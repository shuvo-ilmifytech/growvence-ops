package projects

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/solvinx/opsflow/internal/apperr"
	"github.com/solvinx/opsflow/internal/db"
	"github.com/solvinx/opsflow/internal/storage"
	"github.com/solvinx/opsflow/internal/store"
	"github.com/solvinx/opsflow/internal/validate"
	"github.com/solvinx/opsflow/internal/workflow"
)

const (
	uploadTTL   = time.Hour
	downloadTTL = 5 * time.Minute
)

// Executables and scripts are refused outright; everything else downloads as an attachment.
var blockedExt = map[string]bool{
	".exe": true, ".msi": true, ".bat": true, ".cmd": true, ".com": true, ".scr": true,
	".ps1": true, ".vbs": true, ".dll": true, ".sh": true, ".apk": true, ".app": true,
}

var unsafeKeyChars = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

type UploadInput struct {
	Filename      string     `json:"filename"`
	Size          int64      `json:"size"`
	ContentType   string     `json:"content_type"`
	ReplaceFileID *uuid.UUID `json:"replace_file_id"` // set to upload a new version of an existing file
}

type UploadTicket struct {
	UploadID  string            `json:"upload_id"`
	URL       string            `json:"url"`
	Method    string            `json:"method"`
	Headers   map[string]string `json:"headers"`
	ExpiresAt time.Time         `json:"expires_at"`
}

// pendingUpload lives in Redis between the presign and the confirm.
type pendingUpload struct {
	// Exactly one of the two: the file's container (a project, or a client request).
	ProjectID   *uuid.UUID `json:"project_id"`
	RequestID   *uuid.UUID `json:"client_request_id"`
	UserID      uuid.UUID  `json:"user_id"`
	FileID      *uuid.UUID `json:"file_id"`
	UpdateID    *uuid.UUID `json:"update_id"` // set for update attachments
	CardID      *uuid.UUID `json:"card_id"`   // set for card attachments
	Key         string     `json:"key"`
	Name        string     `json:"name"`
	Size        int64      `json:"size"`
	ContentType string     `json:"content_type"`
}

func uploadKey(id string) string { return "upload:" + id }

// lockForUpload locks the project and checks the user may change its files: the Sales
// owner or anyone assigned to it.
func (s *Service) lockForUpload(ctx context.Context, q *db.Queries, u db.User, projectID uuid.UUID) (lockedProject, error) {
	row, err := lockVisible(ctx, q, u, projectID)
	if err != nil {
		return row, err
	}
	st := workflow.Status(row.Status)
	a := actorFor(u, row.CreatedBy, row.Members)
	if !workflow.CanManageFiles(st, a) {
		if st.Terminal() && (a.IsOwner || a.IsAssignee) {
			return row, apperr.Conflict("Files can't change once a project is " + st.Label())
		}
		return row, apperr.Forbidden()
	}
	return row, nil
}

// cleanUpload validates a requested upload and normalises its name and type.
func (s *Service) cleanUpload(in UploadInput) (name, ctype string, err error) {
	return CleanUpload(in, s.maxUpload)
}

// CleanUpload validates a requested upload against the size limit and normalises its
// name and type. Chat attachments use it too, so both follow the same rules.
func CleanUpload(in UploadInput, maxUpload int64) (name, ctype string, err error) {
	name = strings.TrimSpace(path.Base(strings.ReplaceAll(in.Filename, `\`, "/")))
	ctype = strings.TrimSpace(in.ContentType)
	if ctype == "" {
		ctype = "application/octet-stream"
	}
	v := validate.Errors{}
	if name == "" || name == "." || name == "/" {
		v.Add("filename", "is required")
	}
	v.MaxLen("filename", name, 255)
	if blockedExt[strings.ToLower(path.Ext(name))] {
		v.Add("filename", "is a type of file that can't be uploaded")
	}
	if in.Size <= 0 {
		v.Add("size", "must be greater than zero")
	} else if in.Size > maxUpload {
		v.Add("size", fmt.Sprintf("must be at most %d MB", maxUpload>>20))
	}
	if _, _, err := mime.ParseMediaType(ctype); err != nil || len(ctype) > 255 {
		v.Add("content_type", "is not a valid media type")
	}
	if !v.OK() {
		return "", "", apperr.Validation(v)
	}
	return name, ctype, nil
}

// issueTicket presigns the PUT and remembers the pending upload for the confirm step.
func (s *Service) issueTicket(ctx context.Context, p pendingUpload) (UploadTicket, error) {
	uploadID := uuid.NewString()
	safe := unsafeKeyChars.ReplaceAllString(p.Name, "_")
	if len(safe) > 100 {
		safe = safe[len(safe)-100:]
	}
	scope := "requests/" + deref(p.RequestID).String()
	if p.ProjectID != nil {
		scope = "projects/" + p.ProjectID.String()
	}
	if p.UpdateID != nil {
		scope += "/updates/" + p.UpdateID.String()
	}
	if p.CardID != nil {
		scope += "/cards/" + p.CardID.String()
	}
	p.Key = fmt.Sprintf("%s/%s/%s", scope, uploadID, safe)
	url, headers, err := s.files.PresignPut(ctx, p.Key, p.ContentType, uploadTTL)
	if err != nil {
		return UploadTicket{}, err
	}
	raw, _ := json.Marshal(p)
	if err := s.rdb.Set(ctx, uploadKey(uploadID), raw, uploadTTL).Err(); err != nil {
		return UploadTicket{}, err
	}
	return UploadTicket{UploadID: uploadID, URL: url, Method: "PUT", Headers: headers, ExpiresAt: time.Now().Add(uploadTTL)}, nil
}

// InitUpload validates a project file and returns a presigned URL the browser uploads to directly.
func (s *Service) InitUpload(ctx context.Context, u db.User, projectID uuid.UUID, in UploadInput) (UploadTicket, error) {
	name, ctype, err := s.cleanUpload(in)
	if err != nil {
		return UploadTicket{}, err
	}
	// Check permissions now (fail fast); they are checked again on confirm.
	err = s.st.Tx(ctx, func(q *db.Queries) error {
		if _, err := s.lockForUpload(ctx, q, u, projectID); err != nil {
			return err
		}
		if in.ReplaceFileID != nil {
			_, err := s.fileOnProject(ctx, q, *in.ReplaceFileID, projectID)
			return err
		}
		return nil
	})
	if err != nil {
		return UploadTicket{}, err
	}
	return s.issueTicket(ctx, pendingUpload{
		ProjectID: &projectID, UserID: u.ID, FileID: in.ReplaceFileID, Name: name, Size: in.Size, ContentType: ctype,
	})
}

func (s *Service) fileOnProject(ctx context.Context, q *db.Queries, fileID, projectID uuid.UUID) (db.File, error) {
	f, err := q.LockFile(ctx, fileID)
	if store.IsNotFound(err) || (err == nil && (deref(f.ProjectID) != projectID || f.UpdateID != nil || f.RemovedAt != nil)) {
		return f, apperr.NotFound("File")
	}
	return f, err
}

// takePending claims a pending upload (once) and verifies the stored object.
func (s *Service) takePending(ctx context.Context, u db.User, uploadID string) (pendingUpload, storage.Object, string, error) {
	var p pendingUpload
	raw, err := s.rdb.GetDel(ctx, uploadKey(uploadID)).Bytes()
	if errors.Is(err, redis.Nil) {
		return p, storage.Object{}, "", apperr.NotFound("Upload")
	} else if err != nil {
		return p, storage.Object{}, "", err
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return p, storage.Object{}, "", err
	}
	if p.UserID != u.ID {
		return p, storage.Object{}, "", apperr.NotFound("Upload")
	}
	obj, err := s.files.Stat(ctx, p.Key)
	if errors.Is(err, storage.ErrNotFound) {
		return p, obj, "", apperr.Conflict("The file never reached storage. Please upload it again.")
	} else if err != nil {
		return p, obj, "", err
	}
	if obj.Size != p.Size {
		s.discard(ctx, p.Key)
		return p, obj, "", apperr.Validation(validate.Errors{"size": "doesn't match the uploaded file"})
	}
	sum, err := s.files.SHA256(ctx, p.Key)
	return p, obj, sum, err
}

func (s *Service) discard(ctx context.Context, key string) {
	_ = s.files.Remove(context.WithoutCancel(ctx), key)
}

// CompleteUpload verifies the stored object and records it as a new file or a new version.
func (s *Service) CompleteUpload(ctx context.Context, u db.User, projectID uuid.UUID, uploadID string) error {
	p, obj, sum, err := s.takePending(ctx, u, uploadID)
	if err == nil && (deref(p.ProjectID) != projectID || p.UpdateID != nil) {
		err = apperr.NotFound("Upload")
	}
	if err != nil {
		return err
	}

	err = s.st.Tx(ctx, func(q *db.Queries) error {
		row, err := s.lockForUpload(ctx, q, u, projectID)
		if err != nil {
			return err
		}
		var file db.File
		field := "file_added"
		var oldName *string
		if p.FileID != nil {
			if file, err = s.fileOnProject(ctx, q, *p.FileID, projectID); err != nil {
				return err
			}
			field, oldName = "file_replaced", &file.OriginalName
		} else if file, err = q.CreateFile(ctx, db.CreateFileParams{
			ProjectID: &projectID, OriginalName: p.Name, UploadedBy: u.ID,
		}); err != nil {
			return err
		}
		next, err := storeVersion(ctx, q, file.ID, p, obj, sum, u.ID)
		if err != nil {
			return err
		}
		if err := q.TouchProject(ctx, projectID); err != nil {
			return err
		}
		if workflow.Status(row.Status) != workflow.Draft {
			label := fmt.Sprintf("%s (v%d)", p.Name, next)
			if err := recordChange(ctx, q, projectID, u.ID, row.Members, field, oldName, &label, false); err != nil {
				return err
			}
			line := "added file " + p.Name
			if field == "file_replaced" {
				line = fmt.Sprintf("uploaded version %d of %s", next, p.Name)
			}
			if err := s.notifyFileChange(ctx, q, u, projectID, line); err != nil {
				return err
			}
		}
		return store.LogActivity(ctx, q, store.Activity{
			Actor: &u.ID, Action: "project." + field, EntityType: "file", EntityID: &file.ID, ProjectID: &projectID,
			Metadata: map[string]any{"name": p.Name, "version": next, "size": obj.Size, "sha256": sum},
		})
	})
	if err != nil {
		s.discard(ctx, p.Key)
	}
	return err
}

// storeVersion appends a version to a file and makes it current.
func storeVersion(ctx context.Context, q *db.Queries, fileID uuid.UUID, p pendingUpload, obj storage.Object, sum string, by uuid.UUID) (int32, error) {
	next, err := q.NextFileVersionNo(ctx, fileID)
	if err != nil {
		return 0, err
	}
	ver, err := q.InsertFileVersion(ctx, db.InsertFileVersionParams{
		FileID: fileID, VersionNo: next, StorageKey: p.Key, SizeBytes: obj.Size,
		MimeType: p.ContentType, Sha256: sum, UploadedBy: by,
	})
	if err != nil {
		return 0, err
	}
	// The file takes the newest upload's name, so replacing "v1.pdf" with "v2.pdf" reads naturally.
	return next, q.SetFileCurrentVersion(ctx, db.SetFileCurrentVersionParams{ID: fileID, CurrentVersionID: &ver.ID, OriginalName: p.Name})
}

// RemoveFile hides a file from the project. Its versions stay in storage and history.
func (s *Service) RemoveFile(ctx context.Context, u db.User, projectID, fileID uuid.UUID) error {
	return s.st.Tx(ctx, func(q *db.Queries) error {
		row, err := s.lockForUpload(ctx, q, u, projectID)
		if err != nil {
			return err
		}
		f, err := s.fileOnProject(ctx, q, fileID, projectID)
		if err != nil {
			return err
		}
		// Assignees remove only what they uploaded; the client's brief stays unless Sales removes it.
		if f.UploadedBy != u.ID && !workflow.CanRemoveAnyFile(workflow.Status(row.Status), actorFor(u, row.CreatedBy, row.Members)) {
			return apperr.Forbidden()
		}
		if err := q.RemoveFile(ctx, fileID); err != nil {
			return err
		}
		if err := q.TouchProject(ctx, projectID); err != nil {
			return err
		}
		if workflow.Status(row.Status) != workflow.Draft {
			if err := recordChange(ctx, q, projectID, u.ID, row.Members, "file_removed", &f.OriginalName, nil, false); err != nil {
				return err
			}
			if err := s.notifyFileChange(ctx, q, u, projectID, "removed file "+f.OriginalName); err != nil {
				return err
			}
		}
		return store.LogActivity(ctx, q, store.Activity{
			Actor: &u.ID, Action: "project.file_removed", EntityType: "file", EntityID: &fileID, ProjectID: &projectID,
			Metadata: map[string]any{"name": f.OriginalName},
		})
	})
}

// DownloadURL returns a short-lived link for any version of a file the user can see.
func (s *Service) DownloadURL(ctx context.Context, u db.User, versionID uuid.UUID) (string, error) {
	v, err := s.st.GetFileVersion(ctx, versionID)
	if store.IsNotFound(err) {
		return "", apperr.NotFound("File")
	} else if err != nil {
		return "", err
	}
	if v.RemovedAt != nil {
		return "", apperr.NotFound("File")
	}
	if v.UpdateID != nil {
		// Attachments follow the update's visibility (Sales only once approved).
		if _, _, err := s.loadUpdate(ctx, s.st.Queries, u, *v.UpdateID); err != nil {
			return "", apperr.NotFound("File")
		}
	} else if v.CardID != nil {
		// Card attachments follow the card (Sales: client requests only).
		if _, err := s.GetCard(ctx, u, *v.CardID); err != nil {
			return "", apperr.NotFound("File")
		}
	} else if _, err := s.load(ctx, s.st.Queries, u, deref(v.ProjectID)); err != nil {
		return "", apperr.NotFound("File")
	}
	name := v.OriginalName
	if ext := path.Ext(name); v.VersionNo > 0 {
		name = fmt.Sprintf("%s (v%d)%s", strings.TrimSuffix(name, ext), v.VersionNo, ext)
	}
	return s.files.PresignGet(ctx, v.StorageKey, name, downloadTTL)
}
