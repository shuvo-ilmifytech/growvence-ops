package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/solvinx/opsflow/internal/apperr"
	"github.com/solvinx/opsflow/internal/db"
	"github.com/solvinx/opsflow/internal/projects"
	"github.com/solvinx/opsflow/internal/storage"
	"github.com/solvinx/opsflow/internal/store"
	"github.com/solvinx/opsflow/internal/validate"
)

// Files in chat. The browser asks for an upload ticket, puts the file straight into
// object storage, and then sends the message naming the tickets it used. The message
// is only written once every file is confirmed in storage.

const (
	maxAttachments = 10
	uploadTTL      = time.Hour
	downloadTTL    = 5 * time.Minute
)

var unsafeKeyChars = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

type AttachmentOut struct {
	ID       uuid.UUID `json:"id"`
	Name     string    `json:"name"`
	Size     int64     `json:"size"`
	MimeType string    `json:"mime_type"`
}

type UploadTicket struct {
	UploadID  string            `json:"upload_id"`
	URL       string            `json:"url"`
	Method    string            `json:"method"`
	Headers   map[string]string `json:"headers"`
	ExpiresAt time.Time         `json:"expires_at"`
}

// pendingUpload lives in Redis between the ticket and the message that uses it.
type pendingUpload struct {
	UserID      uuid.UUID `json:"user_id"`
	Key         string    `json:"key"`
	Name        string    `json:"name"`
	Size        int64     `json:"size"`
	ContentType string    `json:"content_type"`
}

func uploadKey(id string) string { return "chatupload:" + id }

// InitUpload validates a file and returns a presigned URL to put it at.
func (s *Service) InitUpload(ctx context.Context, me db.User, in projects.UploadInput) (UploadTicket, error) {
	name, ctype, err := projects.CleanUpload(in, s.maxUpload)
	if err != nil {
		return UploadTicket{}, err
	}
	id := uuid.NewString()
	safe := unsafeKeyChars.ReplaceAllString(name, "_")
	if len(safe) > 100 {
		safe = safe[len(safe)-100:]
	}
	p := pendingUpload{
		UserID: me.ID, Key: fmt.Sprintf("chat/%s/%s/%s", me.ID, id, safe),
		Name: name, Size: in.Size, ContentType: ctype,
	}
	url, headers, err := s.files.PresignPut(ctx, p.Key, ctype, uploadTTL)
	if err != nil {
		return UploadTicket{}, err
	}
	raw, _ := json.Marshal(p)
	if err := s.rdb.Set(ctx, uploadKey(id), raw, uploadTTL).Err(); err != nil {
		return UploadTicket{}, err
	}
	return UploadTicket{UploadID: id, URL: url, Method: "PUT", Headers: headers, ExpiresAt: time.Now().Add(uploadTTL)}, nil
}

// claimUploads takes the sender's pending uploads (each once) and checks that every
// file really is in storage at the size announced.
func (s *Service) claimUploads(ctx context.Context, me db.User, ids []string) ([]pendingUpload, error) {
	if len(ids) > maxAttachments {
		return nil, apperr.Validation(validate.Errors{"attachments": fmt.Sprintf("at most %d files per message", maxAttachments)})
	}
	out := make([]pendingUpload, 0, len(ids))
	fail := func(err error) ([]pendingUpload, error) {
		s.discard(ctx, out)
		return nil, err
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		raw, err := s.rdb.GetDel(ctx, uploadKey(id)).Bytes()
		if errors.Is(err, redis.Nil) {
			return fail(apperr.Validation(validate.Errors{"attachments": "an upload has expired; attach the file again"}))
		} else if err != nil {
			return fail(err)
		}
		var p pendingUpload
		if err := json.Unmarshal(raw, &p); err != nil {
			return fail(err)
		}
		if p.UserID != me.ID {
			return fail(apperr.Validation(validate.Errors{"attachments": "an upload is not yours"}))
		}
		out = append(out, p)
		obj, err := s.files.Stat(ctx, p.Key)
		if errors.Is(err, storage.ErrNotFound) {
			return fail(apperr.Conflict(p.Name + " never reached storage. Attach it again."))
		} else if err != nil {
			return fail(err)
		}
		if obj.Size != p.Size {
			return fail(apperr.Validation(validate.Errors{"attachments": p.Name + " doesn't match the uploaded file"}))
		}
	}
	return out, nil
}

// discard removes uploaded objects that won't be used after all.
func (s *Service) discard(ctx context.Context, ups []pendingUpload) {
	for _, p := range ups {
		_ = s.files.Remove(context.WithoutCancel(ctx), p.Key)
	}
}

// saveAttachments records the claimed uploads against a one-to-one or a group message.
func saveAttachments(ctx context.Context, q *db.Queries, me db.User, message, groupMessage *uuid.UUID, ups []pendingUpload) ([]AttachmentOut, error) {
	out := make([]AttachmentOut, 0, len(ups))
	for _, p := range ups {
		a, err := q.AddChatAttachment(ctx, db.AddChatAttachmentParams{
			MessageID: message, GroupMessageID: groupMessage, UploadedBy: me.ID,
			Name: p.Name, SizeBytes: p.Size, MimeType: p.ContentType, StorageKey: p.Key,
		})
		if err != nil {
			return nil, err
		}
		out = append(out, attachmentOut(a))
	}
	return out, nil
}

func attachmentOut(a db.ChatAttachment) AttachmentOut {
	return AttachmentOut{ID: a.ID, Name: a.Name, Size: a.SizeBytes, MimeType: a.MimeType}
}

// byMessage groups attachments by the message they belong to.
func byMessage(rows []db.ChatAttachment, group bool) map[uuid.UUID][]AttachmentOut {
	out := map[uuid.UUID][]AttachmentOut{}
	for _, a := range rows {
		id := a.MessageID
		if group {
			id = a.GroupMessageID
		}
		if id != nil {
			out[*id] = append(out[*id], attachmentOut(a))
		}
	}
	return out
}

// AttachmentURL is a short-lived download link, for people in the conversation only.
func (s *Service) AttachmentURL(ctx context.Context, me db.User, id uuid.UUID) (string, error) {
	a, err := s.st.ChatAttachmentForViewer(ctx, db.ChatAttachmentForViewerParams{ID: id, Viewer: me.ID})
	if store.IsNotFound(err) {
		return "", apperr.NotFound("File")
	} else if err != nil {
		return "", err
	}
	return s.files.PresignGet(ctx, a.StorageKey, a.Name, downloadTTL)
}

// validBody checks a message's text; it may be empty only when files go with it.
func validBody(body string, files int) error {
	v := validate.Errors{}
	if body == "" && files == 0 {
		v.Add("body", "is required")
	} else if len([]rune(body)) > maxBody {
		v.Add("body", "is too long")
	}
	if !v.OK() {
		return apperr.Validation(v)
	}
	return nil
}
