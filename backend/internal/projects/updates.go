package projects

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/solvinx/opsflow/internal/apperr"
	"github.com/solvinx/opsflow/internal/db"
	"github.com/solvinx/opsflow/internal/notify"
	"github.com/solvinx/opsflow/internal/policy"
	"github.com/solvinx/opsflow/internal/store"
	"github.com/solvinx/opsflow/internal/updateflow"
	"github.com/solvinx/opsflow/internal/validate"
	"github.com/solvinx/opsflow/internal/workflow"
)

// updateActor: IsAssignee means "one of the project's assigned members". Sales are one
// team, so any Sales user acts for the project's client (see and send approved updates).
func updateActor(u db.User, salesID uuid.UUID, members []uuid.UUID) updateflow.Actor {
	return updateflow.Actor{
		Role:         policy.Role(u.Role),
		IsSalesOwner: policy.Role(u.Role) == policy.Sales,
		IsAssignee:   slices.Contains(members, u.ID),
	}
}

// onRequestTeam marks the actor as on the team a client request is with: everyone there
// reads its updates, even before the leader hands them a card. A project's updates are
// never widened this way — they are the assigned members'.
func onRequestTeam(a updateflow.Actor, d db.UpdateDetail, team *uuid.UUID) updateflow.Actor {
	a.OnTeam = d.ClientRequestID != nil && d.ProjectTeamID != nil && onTeam(team, *d.ProjectTeamID)
	return a
}

func (s *Service) inboxRequiresApproval(ctx context.Context, q *db.Queries) bool {
	row, err := q.GetSetting(ctx, "inbox_requires_approval")
	if err != nil {
		return true // fail safe: require approval
	}
	var b bool
	if json.Unmarshal(row.Value, &b) != nil {
		return true
	}
	return b
}

// loadUpdate returns an update the user may see (404 otherwise, never revealing it exists).
func (s *Service) loadUpdate(ctx context.Context, q *db.Queries, u db.User, id uuid.UUID) (db.UpdateDetail, updateflow.Actor, error) {
	d, err := q.GetUpdateDetail(ctx, id)
	if store.IsNotFound(err) {
		return d, updateflow.Actor{}, apperr.NotFound("Update")
	} else if err != nil {
		return d, updateflow.Actor{}, err
	}
	team, err := viewerTeam(ctx, q, u)
	if err != nil {
		return d, updateflow.Actor{}, err
	}
	a := onRequestTeam(updateActor(u, d.ProjectSalesID, d.ProjectMemberIDs), d, team)
	sees := canView(u, d.ProjectStatus, d.ProjectSalesID, d.ProjectMemberIDs, d.ProjectTeamID, d.ProjectTeamLeadID, d.ProjectTeamHeadID) || a.OnTeam
	if !sees || !updateflow.CanView(a, updateflow.Status(d.Status)) {
		return d, a, apperr.NotFound("Update")
	}
	return d, a, nil
}

// lockUpdate locks the row for a state change and returns the visible detail with it.
func (s *Service) lockUpdate(ctx context.Context, q *db.Queries, u db.User, id uuid.UUID) (db.Update, db.UpdateDetail, updateflow.Actor, error) {
	row, err := q.LockUpdate(ctx, id)
	if store.IsNotFound(err) {
		return row, db.UpdateDetail{}, updateflow.Actor{}, apperr.NotFound("Update")
	} else if err != nil {
		return row, db.UpdateDetail{}, updateflow.Actor{}, err
	}
	d, a, err := s.loadUpdate(ctx, q, u, id)
	return row, d, a, err
}

func (s *Service) flowContext(ctx context.Context, q *db.Queries, row db.Update, d db.UpdateDetail) (updateflow.Context, error) {
	n, err := q.CountUpdateFiles(ctx, &row.ID)
	return updateflow.Context{
		Status: updateflow.Status(row.Status), Destination: updateflow.Destination(row.Destination),
		Attachments: n, ProjectStatus: d.ProjectStatus, InboxRequiresApproval: s.inboxRequiresApproval(ctx, q),
	}, err
}

// state is the mutable part of an update row, written in one go by SetUpdateState.
type state struct {
	db.Update
}

func (st state) write(ctx context.Context, q *db.Queries) error {
	return q.SetUpdateState(ctx, db.SetUpdateStateParams{
		ID: st.ID, Status: st.Status, Destination: st.Destination, SubmittedAt: st.SubmittedAt,
		RevisionNumber: st.RevisionNumber, CheckedBy: st.CheckedBy, CheckedAt: st.CheckedAt,
		AutoApproved: st.AutoApproved, SentBy: st.SentBy, SentAt: st.SentAt,
	})
}

func (s *Service) logUpdate(ctx context.Context, q *db.Queries, u db.User, row db.Update, action string, from, to string, comment string, meta map[string]any) error {
	if from != to {
		var c *string
		if comment != "" {
			c = &comment
		}
		var f *string
		if from != "" {
			f = &from
		}
		if err := q.InsertUpdateStatusHistory(ctx, db.InsertUpdateStatusHistoryParams{
			UpdateID: row.ID, FromStatus: f, ToStatus: to, ChangedBy: u.ID, Comment: c,
		}); err != nil {
			return err
		}
	}
	if err := touch(ctx, q, updateRowContainer(row)); err != nil {
		return err
	}
	if meta == nil {
		meta = map[string]any{}
	}
	meta["number"] = row.Number
	meta["from"], meta["to"] = from, to
	return store.LogActivity(ctx, q, store.Activity{
		Actor: &u.ID, Action: action, EntityType: "update", EntityID: &row.ID,
		ProjectID: row.ProjectID, ClientRequestID: row.ClientRequestID, Metadata: meta,
	})
}

// ── Create & edit ──────────────────────────────────────────

type UpdateInput struct {
	Message     *string `json:"message"`
	Destination *string `json:"destination"`
	LockVersion *int32  `json:"lock_version"`
}

// CreateUpdate starts a draft update on a project.
func (s *Service) CreateUpdate(ctx context.Context, u db.User, projectID uuid.UUID, in UpdateInput) (uuid.UUID, error) {
	return s.createUpdateOn(ctx, u, &projectID, nil, in)
}

// CreateRequestUpdate starts a draft update on a client request. It runs the same flow as
// a project's: the members working it write, the Team Leader checks, Sales post it.
func (s *Service) CreateRequestUpdate(ctx context.Context, u db.User, requestID uuid.UUID, in UpdateInput) (uuid.UUID, error) {
	return s.createUpdateOn(ctx, u, nil, &requestID, in)
}

func (s *Service) createUpdateOn(ctx context.Context, u db.User, projectID, requestID *uuid.UUID, in UpdateInput) (uuid.UUID, error) {
	msg, dest := "", string(updateflow.Inbox)
	if in.Message != nil {
		msg = *in.Message
	}
	if in.Destination != nil {
		dest = *in.Destination
	}
	if !updateflow.ValidDestination(dest) {
		return uuid.Nil, apperr.Validation(validate.Errors{"destination": "must be inbox, order_page or delivery"})
	}
	if err := updateflow.ValidateMessage(msg, false); err != nil {
		return uuid.Nil, err
	}
	if requestID != nil && updateflow.Destination(dest) == updateflow.Delivery {
		return uuid.Nil, apperr.Validation(validate.Errors{
			"destination": "a client request has no order to deliver: send it to the inbox or the order page",
		})
	}
	var id uuid.UUID
	err := s.st.Tx(ctx, func(q *db.Queries) error {
		c, err := s.lockContainer(ctx, q, u, projectID, requestID)
		if err != nil {
			return err
		}
		if err := updateflow.CanCreate(updateActor(u, c.SalesID, c.Members), containerFlowStatus(c)); err != nil {
			return err
		}
		if id, err = q.CreateUpdate(ctx, db.CreateUpdateParams{
			ProjectID: c.projectID(), ClientRequestID: c.requestID(), AuthorID: u.ID, Message: msg, Destination: dest,
		}); err != nil {
			return err
		}
		row, err := q.LockUpdate(ctx, id)
		if err != nil {
			return err
		}
		return s.logUpdate(ctx, q, u, row, "update.created", "", string(updateflow.Draft), "", nil)
	})
	return id, err
}

// EditUpdate: the assignee edits text/destination (approved text goes back for approval);
// the TL may only correct the destination.
func (s *Service) EditUpdate(ctx context.Context, u db.User, id uuid.UUID, in UpdateInput) error {
	if in.Destination != nil && !updateflow.ValidDestination(*in.Destination) {
		return apperr.Validation(validate.Errors{"destination": "must be inbox, order_page or delivery"})
	}
	if in.Message != nil {
		if err := updateflow.ValidateMessage(*in.Message, false); err != nil {
			return err
		}
	}
	return s.st.Tx(ctx, func(q *db.Queries) error {
		row, d, a, err := s.lockUpdate(ctx, q, u, id)
		if err != nil {
			return err
		}
		if in.LockVersion != nil && *in.LockVersion != row.LockVersion {
			return apperr.Conflict("Someone else changed this update. Reload to see the latest version.")
		}
		msg, dest := row.Message, row.Destination
		if in.Message != nil {
			msg = *in.Message
		}
		if in.Destination != nil {
			dest = *in.Destination
		}
		if msg == row.Message && dest == row.Destination {
			return nil
		}
		fc, err := s.flowContext(ctx, q, row, d)
		if err != nil {
			return err
		}
		fc.Destination = updateflow.Destination(dest)

		// A reviewing TL's only edit is correcting where it goes; a TL working the
		// project edits their own update like any assignee.
		if a.Role == policy.TL && !a.IsAssignee {
			if msg != row.Message {
				return apperr.Forbidden()
			}
			if err := updateflow.SetDestination(a, fc); err != nil {
				return err
			}
			if err := q.SetUpdateContent(ctx, db.SetUpdateContentParams{ID: id, Message: msg, Destination: dest}); err != nil {
				return err
			}
			return s.logUpdate(ctx, q, u, row, "update.destination_corrected", row.Status, row.Status, "",
				map[string]any{"destination": [2]string{row.Destination, dest}})
		}

		next, err := updateflow.Edit(a, fc)
		if err != nil {
			return err
		}
		if err := q.SetUpdateContent(ctx, db.SetUpdateContentParams{ID: id, Message: msg, Destination: dest}); err != nil {
			return err
		}
		return s.afterEdit(ctx, q, u, row, next, "update.edited")
	})
}

// afterEdit applies the status an edit lands on (approved -> pending clears the approval).
func (s *Service) afterEdit(ctx context.Context, q *db.Queries, u db.User, before db.Update, next updateflow.Status, action string) error {
	if string(next) != before.Status {
		cur, err := q.LockUpdate(ctx, before.ID)
		if err != nil {
			return err
		}
		st := state{cur}
		st.Status = string(next)
		st.CheckedBy, st.CheckedAt, st.AutoApproved = nil, nil, false
		if err := st.write(ctx, q); err != nil {
			return err
		}
		return s.logUpdate(ctx, q, u, before, action, before.Status, string(next), "Edited after approval", nil)
	}
	return s.logUpdate(ctx, q, u, before, action, before.Status, before.Status, "", nil)
}

// ── Status moves ───────────────────────────────────────────

func (s *Service) SubmitUpdate(ctx context.Context, u db.User, id uuid.UUID) error {
	return s.st.Tx(ctx, func(q *db.Queries) error {
		row, d, a, err := s.lockUpdate(ctx, q, u, id)
		if err != nil {
			return err
		}
		if err := updateflow.ValidateMessage(row.Message, true); err != nil {
			return err
		}
		fc, err := s.flowContext(ctx, q, row, d)
		if err != nil {
			return err
		}
		to, auto, err := updateflow.Submit(a, fc)
		if err != nil {
			return err
		}
		st := state{row}
		now := time.Now()
		st.Status, st.SubmittedAt, st.AutoApproved = string(to), &now, auto
		st.CheckedBy, st.CheckedAt = nil, nil
		if auto {
			st.CheckedAt = &now // approved by setting, not by a person
		}
		// A Delivery records which delivery it is: 0 = original, N = Nth revision.
		st.RevisionNumber = nil
		if row.Destination == string(updateflow.Delivery) && row.ProjectID != nil {
			p, err := q.LockProject(ctx, *row.ProjectID)
			if err != nil {
				return err
			}
			st.RevisionNumber = &p.RevisionCount
		}
		if err := st.write(ctx, q); err != nil {
			return err
		}
		comment := ""
		typ := notify.UpdateSubmitted
		if auto {
			comment = "Approved automatically (Inbox approval is off)"
			typ = notify.UpdateApproved
		}
		if err := s.notifyFresh(ctx, q, u, id, typ, ""); err != nil {
			return err
		}
		if err := s.logUpdate(ctx, q, u, row, "update.submitted", row.Status, string(to), comment, map[string]any{"auto_approved": auto}); err != nil {
			return err
		}
		// A Delivery goes to the TL's check, or straight to Sales when it needs no approval
		// (a Team Leader's own update).
		if row.Destination != string(updateflow.Delivery) || row.ProjectID == nil {
			return nil
		}
		if to == updateflow.Approved {
			return s.advanceDelivery(ctx, q, u, *row.ProjectID, workflow.ReadyToDeliver, fmt.Sprintf("Delivery update #%d is ready", row.Number))
		}
		return s.advanceDelivery(ctx, q, u, *row.ProjectID, workflow.ReadyForReview, fmt.Sprintf("Delivery update #%d sent for the check", row.Number))
	})
}

type ApproveInput struct {
	Destination *string `json:"destination"` // optional correction
}

func (s *Service) ApproveUpdate(ctx context.Context, u db.User, id uuid.UUID, in ApproveInput) error {
	if in.Destination != nil && !updateflow.ValidDestination(*in.Destination) {
		return apperr.Validation(validate.Errors{"destination": "must be inbox, order_page or delivery"})
	}
	return s.st.Tx(ctx, func(q *db.Queries) error {
		row, d, a, err := s.lockUpdate(ctx, q, u, id)
		if err != nil {
			return err
		}
		fc, err := s.flowContext(ctx, q, row, d)
		if err != nil {
			return err
		}
		st := state{row}
		if in.Destination != nil {
			fc.Destination = updateflow.Destination(*in.Destination)
			st.Destination = *in.Destination
		}
		if err := updateflow.Approve(a, fc); err != nil {
			return err
		}
		now := time.Now()
		st.Status, st.CheckedBy, st.CheckedAt, st.AutoApproved = string(updateflow.Approved), &u.ID, &now, false
		if st.Destination == string(updateflow.Delivery) && st.RevisionNumber == nil && row.ProjectID != nil {
			p, err := q.LockProject(ctx, *row.ProjectID)
			if err != nil {
				return err
			}
			st.RevisionNumber = &p.RevisionCount
		} else if st.Destination != string(updateflow.Delivery) {
			st.RevisionNumber = nil
		}
		if err := st.write(ctx, q); err != nil {
			return err
		}
		if err := s.notifyFresh(ctx, q, u, id, notify.UpdateApproved, ""); err != nil {
			return err
		}
		meta := map[string]any{}
		if st.Destination != row.Destination {
			meta["destination"] = [2]string{row.Destination, st.Destination}
		}
		if err := s.logUpdate(ctx, q, u, row, "update.approved", row.Status, st.Status, "", meta); err != nil {
			return err
		}
		if st.Destination != string(updateflow.Delivery) || row.ProjectID == nil {
			return nil
		}
		return s.advanceDelivery(ctx, q, u, *row.ProjectID, workflow.ReadyToDeliver, fmt.Sprintf("Delivery update #%d approved", row.Number))
	})
}

func (s *Service) RequestUpdateChanges(ctx context.Context, u db.User, id uuid.UUID, comment string) error {
	comment = strings.TrimSpace(comment)
	if len(comment) > 5000 {
		return apperr.Validation(validate.Errors{"comment": "is too long"})
	}
	return s.st.Tx(ctx, func(q *db.Queries) error {
		row, d, a, err := s.lockUpdate(ctx, q, u, id)
		if err != nil {
			return err
		}
		fc, err := s.flowContext(ctx, q, row, d)
		if err != nil {
			return err
		}
		if err := updateflow.RequestChanges(a, fc, comment); err != nil {
			return err
		}
		st := state{row}
		st.Status, st.CheckedBy, st.CheckedAt = string(updateflow.ChangesRequested), nil, nil
		if err := st.write(ctx, q); err != nil {
			return err
		}
		// The reason also starts the Ops thread so the discussion continues there.
		if _, err := q.InsertUpdateComment(ctx, db.InsertUpdateCommentParams{
			UpdateID: id, Thread: string(updateflow.OpsThread), AuthorID: u.ID, Body: comment,
		}); err != nil {
			return err
		}
		if err := s.notifyFresh(ctx, q, u, id, notify.UpdateChanges, comment); err != nil {
			return err
		}
		return s.logUpdate(ctx, q, u, row, "update.changes_requested", row.Status, st.Status, comment, nil)
	})
}

// MarkSent records that Sales posted it on the platform. A Delivery completes the project.
func (s *Service) MarkSent(ctx context.Context, u db.User, id uuid.UUID) error {
	return s.st.Tx(ctx, func(q *db.Queries) error {
		row, d, a, err := s.lockUpdate(ctx, q, u, id)
		if err != nil {
			return err
		}
		fc, err := s.flowContext(ctx, q, row, d)
		if err != nil {
			return err
		}
		var p db.Project
		if row.ProjectID != nil {
			if p, err = q.LockProject(ctx, *row.ProjectID); err != nil {
				return err
			}
			fc.ProjectStatus = p.Status
		}
		if err := updateflow.MarkSent(a, fc); err != nil {
			return err
		}
		st := state{row}
		now := time.Now()
		st.Status, st.SentBy, st.SentAt = string(updateflow.Sent), &u.ID, &now
		if err := st.write(ctx, q); err != nil {
			return err
		}
		if err := s.logUpdate(ctx, q, u, row, "update.sent", row.Status, st.Status, "", nil); err != nil {
			return err
		}
		if err := s.notifyFresh(ctx, q, u, id, notify.UpdateSent, ""); err != nil {
			return err
		}
		if row.Destination != string(updateflow.Delivery) || row.ProjectID == nil {
			return nil
		}
		// Sending the Delivery on the marketplace is delivering the project.
		note := fmt.Sprintf("Delivery sent (update #%d)", row.Number)
		if row.RevisionNumber != nil && *row.RevisionNumber > 0 {
			note = fmt.Sprintf("Revision %d delivered (update #%d)", *row.RevisionNumber, row.Number)
		}
		return s.advanceDelivery(ctx, q, u, p.ID, workflow.Delivered, note)
	})
}

// deliveryStep orders the statuses a delivery passes through.
var deliveryStep = map[workflow.Status]int{
	workflow.InProgress: 1, workflow.Blocked: 1, workflow.ReadyForReview: 2, workflow.ReadyToDeliver: 3, workflow.Delivered: 4,
}

// advanceDelivery moves a project along with its Delivery update: submitted → In Review,
// approved → Ready to Deliver, sent → Delivered. It never moves a project back, and leaves
// New, Completed and Cancelled projects alone, so the status buttons and the update can be
// used in any mix.
func (s *Service) advanceDelivery(ctx context.Context, q *db.Queries, u db.User, projectID uuid.UUID, to workflow.Status, note string) error {
	p, err := q.LockProject(ctx, projectID)
	if err != nil {
		return err
	}
	from := workflow.Status(p.Status)
	if step, ok := deliveryStep[from]; !ok || step >= deliveryStep[to] {
		return nil
	}
	if err := q.SetProjectStatus(ctx, db.SetProjectStatusParams{ID: p.ID, Status: string(to)}); err != nil {
		return err
	}
	if err := q.InsertStatusHistory(ctx, db.InsertStatusHistoryParams{
		ProjectID: p.ID, FromStatus: &p.Status, ToStatus: string(to), ChangedBy: u.ID, Comment: &note,
	}); err != nil {
		return err
	}
	members, err := q.ProjectMemberIDs(ctx, p.ID)
	if err != nil {
		return err
	}
	if err := s.notifyTransition(ctx, q, u, lockedProject{Project: p, Members: members}, from, to, note); err != nil {
		return err
	}
	return store.LogActivity(ctx, q, store.Activity{
		Actor: &u.ID, Action: "project.status_changed", EntityType: "project", EntityID: &p.ID, ProjectID: &p.ID,
		Metadata: map[string]any{"from": p.Status, "to": to, "via_update": true},
	})
}

// ── Comments ───────────────────────────────────────────────

func (s *Service) CommentUpdate(ctx context.Context, u db.User, id uuid.UUID, thread, body string) (db.UpdateComment, error) {
	body = strings.TrimSpace(body)
	v := validate.Errors{}
	v.OneOf("thread", thread, string(updateflow.OpsThread), string(updateflow.SalesThread))
	v.Len("body", body, 1, 10000)
	if !v.OK() {
		return db.UpdateComment{}, apperr.Validation(v)
	}
	var c db.UpdateComment
	err := s.st.Tx(ctx, func(q *db.Queries) error {
		row, _, a, err := s.lockUpdate(ctx, q, u, id)
		if err != nil {
			return err
		}
		if !updateflow.CanComment(a, updateflow.Thread(thread)) {
			return apperr.Forbidden()
		}
		if c, err = q.InsertUpdateComment(ctx, db.InsertUpdateCommentParams{UpdateID: id, Thread: thread, AuthorID: u.ID, Body: body}); err != nil {
			return err
		}
		d, err := q.GetUpdateDetail(ctx, id)
		if err != nil {
			return err
		}
		if err := s.notifyUpdateComment(ctx, q, u, d, thread, body); err != nil {
			return err
		}
		return s.logUpdate(ctx, q, u, row, "update.commented", row.Status, row.Status, "", map[string]any{"thread": thread})
	})
	return c, err
}

// ── Attachments ────────────────────────────────────────────

func (s *Service) InitUpdateUpload(ctx context.Context, u db.User, id uuid.UUID, in UploadInput) (UploadTicket, error) {
	name, ctype, err := s.cleanUpload(in)
	if err != nil {
		return UploadTicket{}, err
	}
	var c container
	err = s.st.Tx(ctx, func(q *db.Queries) error {
		row, d, a, err := s.lockUpdate(ctx, q, u, id)
		if err != nil {
			return err
		}
		fc, err := s.flowContext(ctx, q, row, d)
		if err != nil {
			return err
		}
		c = updateRowContainer(row)
		_, err = updateflow.Edit(a, fc)
		return err
	})
	if err != nil {
		return UploadTicket{}, err
	}
	return s.issueTicket(ctx, pendingUpload{
		ProjectID: c.projectID(), RequestID: c.requestID(), UpdateID: &id,
		UserID: u.ID, Name: name, Size: in.Size, ContentType: ctype,
	})
}

func (s *Service) CompleteUpdateUpload(ctx context.Context, u db.User, id uuid.UUID, uploadID string) error {
	p, obj, sum, err := s.takePending(ctx, u, uploadID)
	if err == nil && (p.UpdateID == nil || *p.UpdateID != id) {
		err = apperr.NotFound("Upload")
	}
	if err != nil {
		return err
	}
	err = s.st.Tx(ctx, func(q *db.Queries) error {
		row, d, a, err := s.lockUpdate(ctx, q, u, id)
		if err != nil {
			return err
		}
		fc, err := s.flowContext(ctx, q, row, d)
		if err != nil {
			return err
		}
		next, err := updateflow.Edit(a, fc)
		if err != nil {
			return err
		}
		file, err := q.CreateFile(ctx, db.CreateFileParams{
			ProjectID: row.ProjectID, ClientRequestID: row.ClientRequestID, UpdateID: &id,
			OriginalName: p.Name, UploadedBy: u.ID,
		})
		if err != nil {
			return err
		}
		if _, err := storeVersion(ctx, q, file.ID, p, obj, sum, u.ID); err != nil {
			return err
		}
		return s.afterEdit(ctx, q, u, row, next, "update.attachment_added")
	})
	if err != nil {
		s.discard(ctx, p.Key)
	}
	return err
}

func (s *Service) RemoveUpdateAttachment(ctx context.Context, u db.User, id, fileID uuid.UUID) error {
	return s.st.Tx(ctx, func(q *db.Queries) error {
		row, d, a, err := s.lockUpdate(ctx, q, u, id)
		if err != nil {
			return err
		}
		f, err := q.LockFile(ctx, fileID)
		if store.IsNotFound(err) || (err == nil && (f.UpdateID == nil || *f.UpdateID != id || f.RemovedAt != nil)) {
			return apperr.NotFound("Attachment")
		} else if err != nil {
			return err
		}
		fc, err := s.flowContext(ctx, q, row, d)
		if err != nil {
			return err
		}
		next, err := updateflow.Edit(a, fc)
		if err != nil {
			return err
		}
		if err := q.RemoveFile(ctx, fileID); err != nil {
			return err
		}
		return s.afterEdit(ctx, q, u, row, next, "update.attachment_removed")
	})
}

// ── Read ───────────────────────────────────────────────────

type UpdateOut struct {
	db.UpdateDetail
}

// sanitize hides what a viewer must not learn (Sales never see the Ops thread, not even its size).
func sanitize(d db.UpdateDetail, a updateflow.Actor) UpdateOut {
	if a.Role == policy.Sales {
		d.OpsCommentCount = 0
	}
	return UpdateOut{d}
}

type UpdatePermissions struct {
	CanEdit           bool `json:"can_edit"`
	CanSubmit         bool `json:"can_submit"`
	CanApprove        bool `json:"can_approve"`
	CanRequestChanges bool `json:"can_request_changes"`
	CanSetDestination bool `json:"can_set_destination"`
	CanMarkSent       bool `json:"can_mark_sent"`
	CanCommentOps     bool `json:"can_comment_ops"`
	CanCommentSales   bool `json:"can_comment_sales"`
	CanReadOps        bool `json:"can_read_ops"`
}

type UpdateDetailResult struct {
	Update        UpdateOut                       `json:"update"`
	Permissions   UpdatePermissions               `json:"permissions"`
	Attachments   []db.ListUpdateFilesRow         `json:"attachments"`
	Comments      []db.ListUpdateCommentsRow      `json:"comments"`
	History       []db.ListUpdateStatusHistoryRow `json:"history"`
	NeedsApproval bool                            `json:"needs_approval"` // under current settings
}

func (s *Service) GetUpdate(ctx context.Context, u db.User, id uuid.UUID) (UpdateDetailResult, error) {
	d, a, err := s.loadUpdate(ctx, s.st.Queries, u, id)
	if err != nil {
		return UpdateDetailResult{}, err
	}
	n, err := s.st.CountUpdateFiles(ctx, &id)
	if err != nil {
		return UpdateDetailResult{}, err
	}
	fc := updateflow.Context{
		Status: updateflow.Status(d.Status), Destination: updateflow.Destination(d.Destination),
		Attachments: n, ProjectStatus: d.ProjectStatus, InboxRequiresApproval: s.inboxRequiresApproval(ctx, s.st.Queries),
	}
	ok := func(err error) bool { return err == nil }
	_, editErr := updateflow.Edit(a, fc)
	readable := updateflow.ReadableThreads(a)
	res := UpdateDetailResult{
		Update: sanitize(d, a),
		Permissions: UpdatePermissions{
			CanEdit: ok(editErr),
			CanSubmit: a.Works() &&
				(fc.Status == updateflow.Draft || fc.Status == updateflow.ChangesRequested),
			CanApprove:        a.Role == policy.TL && fc.Status == updateflow.PendingApproval,
			CanRequestChanges: a.Role == policy.TL && fc.Status == updateflow.PendingApproval,
			CanSetDestination: a.Role == policy.TL && (fc.Status == updateflow.PendingApproval || fc.Status == updateflow.Approved),
			CanMarkSent:       ok(updateflow.MarkSent(a, fc)),
			CanCommentOps:     updateflow.CanComment(a, updateflow.OpsThread),
			CanCommentSales:   updateflow.CanComment(a, updateflow.SalesThread),
			CanReadOps:        len(readable) == 2,
		},
		NeedsApproval: fc.Destination != updateflow.Inbox || fc.InboxRequiresApproval,
	}
	if res.Attachments, err = s.st.ListUpdateFiles(ctx, &id); err != nil {
		return res, err
	}
	if res.Comments, err = s.st.ListUpdateComments(ctx, db.ListUpdateCommentsParams{UpdateID: id, Threads: readable}); err != nil {
		return res, err
	}
	res.History, err = s.st.ListUpdateStatusHistory(ctx, id)
	return res, err
}

type UpdateFilter struct {
	ProjectID   *uuid.UUID
	RequestID   *uuid.UUID
	Statuses    []string
	Destination *string
	Q           *string
	Page        int
	PageSize    int
}

type UpdateListResult struct {
	Updates  []UpdateOut      `json:"updates"`
	Total    int64            `json:"total"`
	Counts   map[string]int64 `json:"counts"`
	Page     int              `json:"page"`
	PageSize int              `json:"page_size"`
}

func updateScope(u db.User) (sales, assignee, lead *uuid.UUID, hideDrafts bool) {
	switch policy.Role(u.Role) {
	case policy.Sales:
		return &u.ID, nil, nil, true
	case policy.TL:
		return nil, nil, &u.ID, true
	case policy.Member:
		return nil, &u.ID, nil, false
	case policy.CEO:
		return nil, nil, nil, false
	}
	nobody := uuid.Nil
	return &nobody, &nobody, nil, true
}

func (s *Service) ListUpdates(ctx context.Context, u db.User, f UpdateFilter) (UpdateListResult, error) {
	sales, assignee, lead, hideDrafts := updateScope(u)
	if f.ProjectID != nil {
		if _, err := s.load(ctx, s.st.Queries, u, *f.ProjectID); err != nil {
			return UpdateListResult{}, err
		}
	}
	if f.RequestID != nil {
		if _, err := s.visibleRequest(ctx, s.st.Queries, u, *f.RequestID); err != nil {
			return UpdateListResult{}, err
		}
	}
	rows, err := s.st.ListUpdateDetails(ctx, db.ListUpdateDetailsParams{
		ScopeSales: sales, ScopeAssignee: assignee, ScopeLead: lead, HideDrafts: hideDrafts,
		ProjectID: f.ProjectID, ClientRequestID: f.RequestID, Statuses: f.Statuses, Destination: f.Destination, Q: f.Q,
		Lim: int32(f.PageSize), Off: int32((f.Page - 1) * f.PageSize),
	})
	if err != nil {
		return UpdateListResult{}, err
	}
	counts, err := s.st.CountUpdatesByStatus(ctx, db.CountUpdatesByStatusParams{
		ScopeSales: sales, ScopeAssignee: assignee, ScopeLead: lead, HideDrafts: hideDrafts,
	})
	if err != nil {
		return UpdateListResult{}, err
	}
	team, err := viewerTeam(ctx, s.st.Queries, u)
	if err != nil {
		return UpdateListResult{}, err
	}
	res := UpdateListResult{Updates: make([]UpdateOut, len(rows)), Counts: map[string]int64{}, Page: f.Page, PageSize: f.PageSize}
	for i, r := range rows {
		res.Total = r.Total
		a := onRequestTeam(updateActor(u, r.UpdateDetail.ProjectSalesID, r.UpdateDetail.ProjectMemberIDs), r.UpdateDetail, team)
		res.Updates[i] = sanitize(r.UpdateDetail, a)
	}
	for _, c := range counts {
		res.Counts[c.Status] = c.N
	}
	return res, nil
}
