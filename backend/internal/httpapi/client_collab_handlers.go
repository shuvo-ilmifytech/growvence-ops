package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"

	"github.com/solvinx/opsflow/internal/db"
	"github.com/solvinx/opsflow/internal/notify"
	"github.com/solvinx/opsflow/internal/policy"
	"github.com/solvinx/opsflow/internal/projects"
	"github.com/solvinx/opsflow/internal/storage"
)

// Client collaboration (Phase 4): ClickUp-style work with the client.
// Business clients (CL-0001…) link to projects; tasks can be shared with the
// client (visible_to_client), discussed in shared comments, signed off through
// approvals, and supported by a per-project message thread + shared files.
//
// Two different links guard the two sides: the team works on project_clients
// (the business link), the client portal checks client_project_access (the
// login link). A client sees a project only when the login link exists.

// canViewClients: who may open the Clients page.
func canViewClients(u db.User) bool {
	r := roleOf(u)
	return isSystemAdmin(u) || r == policy.Sales || r == policy.TL || r == policy.HR
}

// canManageClients: who may add or edit a client.
func canManageClients(u db.User) bool {
	return u.IsAdmin || roleOf(u) == policy.Sales
}

// canWorkCollab: team-side project work (comments, messages, files, approvals).
// Members work the tasks, Sales/TL run them, the CEO only reads (readOnlyGuard
// blocks their writes, like any other project operation).
func canWorkCollab(u db.User) bool {
	r := roleOf(u)
	return isSystemAdmin(u) || r == policy.Sales || r == policy.TL || r == policy.Member
}

// canManageCollab: linking clients, toggling visibility, asking for approval.
func canManageCollab(u db.User) bool {
	r := roleOf(u)
	return u.IsAdmin || r == policy.Sales || r == policy.TL
}

// collabCard resolves a card inside a project for the collaboration endpoints.
type collabCard struct {
	ID              uuid.UUID
	ProjectID       *uuid.UUID
	VisibleToClient bool
	Title           string
	ColumnKey       string
}

func (s *Server) collabCard(ctx context.Context, pid, cardID uuid.UUID) (collabCard, error) {
	var c collabCard
	err := s.store.Pool.QueryRow(ctx, `
		SELECT id, project_id, visible_to_client, title, column_key
		FROM project_cards WHERE id = $1 AND removed_at IS NULL`, cardID).
		Scan(&c.ID, &c.ProjectID, &c.VisibleToClient, &c.Title, &c.ColumnKey)
	if err != nil {
		if err == pgx.ErrNoRows {
			return c, errNotFound("Card not found")
		}
		return c, err
	}
	if c.ProjectID == nil || *c.ProjectID != pid {
		return c, errNotFound("Card not found")
	}
	return c, nil
}

// ── Clients ──────────────────────────────────────────────────

type clientInput struct {
	CompanyName   string `json:"company_name"`
	ContactPerson string `json:"contact_person"`
	Email         string `json:"email"`
	Phone         string `json:"phone"`
	Address       string `json:"address"`
	Type          string `json:"type"`
	Status        string `json:"status"`
}

func (s *Server) listClients(c fiber.Ctx) error {
	if !canViewClients(currentUser(c)) {
		return errForbidden()
	}
	rows, err := s.store.Queries.ListClients(c.Context())
	if err != nil {
		return err
	}
	if rows == nil {
		rows = []db.ListClientsRow{}
	}
	return c.JSON(fiber.Map{"clients": rows})
}

func (s *Server) createClient(c fiber.Ctx) error {
	u := currentUser(c)
	if !canManageClients(u) {
		return errForbidden()
	}
	var in clientInput
	if err := decodeJSON(c, &in); err != nil {
		return err
	}
	in.CompanyName = strings.TrimSpace(in.CompanyName)
	if len(in.CompanyName) < 1 || len(in.CompanyName) > 200 {
		return errBadRequest("Company name is required (max 200 characters)")
	}
	in.ContactPerson = strings.TrimSpace(in.ContactPerson)
	in.Email = strings.TrimSpace(in.Email)
	in.Phone = strings.TrimSpace(in.Phone)
	in.Address = strings.TrimSpace(in.Address)
	typ := strings.TrimSpace(in.Type)
	if typ == "" {
		typ = "B2B"
	}
	if typ != "B2B" && typ != "Individual" {
		return errBadRequest("Type must be B2B or Individual")
	}
	row, err := s.store.Queries.CreateClient(c.Context(), db.CreateClientParams{
		CompanyName: in.CompanyName, ContactPerson: strPtr(in.ContactPerson),
		Email: strPtr(in.Email), Phone: strPtr(in.Phone), Address: strPtr(in.Address),
		Type: typ, CreatedBy: &u.ID,
	})
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"id": row.ID, "client_code": row.ClientCode})
}

func (s *Server) getClient(c fiber.Ctx) error {
	if !canViewClients(currentUser(c)) {
		return errForbidden()
	}
	id, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	cl, err := s.store.Queries.GetClient(c.Context(), id)
	if err != nil {
		return errNotFound("Client not found")
	}
	contacts, err := s.store.Queries.ListClientContacts(c.Context(), id)
	if err != nil {
		return err
	}
	if contacts == nil {
		contacts = []db.ClientContact{}
	}
	projs, err := s.store.Queries.ListClientProjects(c.Context(), id)
	if err != nil {
		return err
	}
	if projs == nil {
		projs = []db.ListClientProjectsRow{}
	}
	return c.JSON(fiber.Map{"client": cl, "contacts": contacts, "projects": projs})
}

func (s *Server) updateClient(c fiber.Ctx) error {
	u := currentUser(c)
	if !canManageClients(u) {
		return errForbidden()
	}
	id, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	var in clientInput
	if err := decodeJSON(c, &in); err != nil {
		return err
	}
	in.CompanyName = strings.TrimSpace(in.CompanyName)
	if len(in.CompanyName) < 1 || len(in.CompanyName) > 200 {
		return errBadRequest("Company name is required (max 200 characters)")
	}
	in.ContactPerson = strings.TrimSpace(in.ContactPerson)
	in.Email = strings.TrimSpace(in.Email)
	in.Phone = strings.TrimSpace(in.Phone)
	in.Address = strings.TrimSpace(in.Address)
	typ := strings.TrimSpace(in.Type)
	if typ != "B2B" && typ != "Individual" {
		return errBadRequest("Type must be B2B or Individual")
	}
	status := strings.TrimSpace(in.Status)
	if status != "active" && status != "inactive" {
		return errBadRequest("Status must be active or inactive")
	}
	if _, err := s.store.Queries.GetClient(c.Context(), id); err != nil {
		return errNotFound("Client not found")
	}
	if err := s.store.Queries.UpdateClient(c.Context(), db.UpdateClientParams{
		ID: id, CompanyName: in.CompanyName, ContactPerson: strPtr(in.ContactPerson),
		Email: strPtr(in.Email), Phone: strPtr(in.Phone), Address: strPtr(in.Address),
		Type: typ, Status: status,
	}); err != nil {
		return err
	}
	return c.JSON(fiber.Map{"ok": true})
}

type contactInput struct {
	Name      string `json:"name"`
	Email     string `json:"email"`
	Phone     string `json:"phone"`
	Role      string `json:"role"`
	IsPrimary bool   `json:"is_primary"`
}

func (s *Server) addClientContact(c fiber.Ctx) error {
	u := currentUser(c)
	if !canManageClients(u) {
		return errForbidden()
	}
	id, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	var in contactInput
	if err := decodeJSON(c, &in); err != nil {
		return err
	}
	in.Name = strings.TrimSpace(in.Name)
	if len(in.Name) < 1 || len(in.Name) > 120 {
		return errBadRequest("Contact name is required (max 120 characters)")
	}
	in.Email = strings.TrimSpace(in.Email)
	in.Phone = strings.TrimSpace(in.Phone)
	in.Role = strings.TrimSpace(in.Role)
	if _, err := s.store.Queries.GetClient(c.Context(), id); err != nil {
		return errNotFound("Client not found")
	}
	ctx := c.Context()
	tx, err := s.store.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	q := db.New(tx)
	if in.IsPrimary {
		if _, err := tx.Exec(ctx, `UPDATE client_contacts SET is_primary = false WHERE client_id = $1`, id); err != nil {
			return err
		}
	}
	cid, err := q.AddClientContact(ctx, db.AddClientContactParams{
		ClientID: id, Name: in.Name, Email: strPtr(in.Email), Phone: strPtr(in.Phone),
		Role: strPtr(in.Role), IsPrimary: in.IsPrimary,
	})
	if err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"id": cid})
}

func (s *Server) deleteClientContact(c fiber.Ctx) error {
	if !canManageClients(currentUser(c)) {
		return errForbidden()
	}
	id, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	cid, err := paramUUID(c, "cid")
	if err != nil {
		return err
	}
	ct, err := s.store.Pool.Exec(c.Context(), `DELETE FROM client_contacts WHERE id = $1 AND client_id = $2`, cid, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return errNotFound("Contact not found")
	}
	return c.JSON(fiber.Map{"ok": true})
}

// ── Project ↔ client links ───────────────────────────────────

func (s *Server) projectCollab(c fiber.Ctx) error {
	u := currentUser(c)
	if !canWorkCollab(u) {
		return errForbidden()
	}
	pid, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	if _, err := s.projectRow(c, pid); err != nil {
		return err
	}
	clients, err := s.store.Queries.ListProjectClients(c.Context(), pid)
	if err != nil {
		return err
	}
	if clients == nil {
		clients = []db.ListProjectClientsRow{}
	}
	messages, err := s.store.Queries.ListClientMessages(c.Context(), pid)
	if err != nil {
		return err
	}
	if messages == nil {
		messages = []db.ListClientMessagesRow{}
	}
	files, err := s.store.Queries.ListClientFiles(c.Context(), pid)
	if err != nil {
		return err
	}
	if files == nil {
		files = []db.ListClientFilesRow{}
	}
	return c.JSON(fiber.Map{"clients": clients, "messages": messages, "files": files})
}

// projectRow verifies the project exists (light check: the collaboration
// endpoints are supplementary to the project's own permission model).
func (s *Server) projectRow(c fiber.Ctx, pid uuid.UUID) (string, error) {
	var name string
	err := s.store.Pool.QueryRow(c.Context(), `SELECT name FROM projects WHERE id = $1`, pid).Scan(&name)
	if err != nil {
		if err == pgx.ErrNoRows {
			return "", errNotFound("Project not found")
		}
		return "", err
	}
	return name, nil
}

type linkClientInput struct {
	ClientID uuid.UUID `json:"client_id"`
}

func (s *Server) linkClient(c fiber.Ctx) error {
	if !canManageCollab(currentUser(c)) {
		return errForbidden()
	}
	pid, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	if _, err := s.projectRow(c, pid); err != nil {
		return err
	}
	var in linkClientInput
	if err := decodeJSON(c, &in); err != nil {
		return err
	}
	if in.ClientID == uuid.Nil {
		return errBadRequest("client_id is required")
	}
	if _, err := s.store.Queries.GetClient(c.Context(), in.ClientID); err != nil {
		return errNotFound("Client not found")
	}
	n, err := s.store.Queries.ProjectHasClient(c.Context(), db.ProjectHasClientParams{ProjectID: pid, ClientID: in.ClientID})
	if err != nil {
		return err
	}
	if n > 0 {
		return errConflict("Client is already linked to this project")
	}
	u := currentUser(c)
	if _, err := s.store.Queries.LinkClientToProject(c.Context(), db.LinkClientToProjectParams{
		ProjectID: pid, ClientID: in.ClientID, CreatedBy: &u.ID,
	}); err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"ok": true})
}

func (s *Server) unlinkClient(c fiber.Ctx) error {
	if !canManageCollab(currentUser(c)) {
		return errForbidden()
	}
	pid, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	cid, err := paramUUID(c, "cid")
	if err != nil {
		return err
	}
	ct, err := s.store.Pool.Exec(c.Context(), `DELETE FROM project_clients WHERE project_id = $1 AND client_id = $2`, pid, cid)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return errNotFound("Client is not linked to this project")
	}
	return c.JSON(fiber.Map{"ok": true})
}

// ── Task comments ────────────────────────────────────────────

func (s *Server) listTaskComments(c fiber.Ctx) error {
	u := currentUser(c)
	pid, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	cardID, err := paramUUID(c, "cardId")
	if err != nil {
		return err
	}
	if roleOf(u) == policy.Client {
		if !s.clientHasAccess(c, u.ID, pid) {
			return errForbidden()
		}
		if _, err := s.collabCard(c.Context(), pid, cardID); err != nil {
			return err
		}
		// Clients only ever see the shared side of the thread.
		rows, err := s.store.Queries.ListTaskCommentsForClient(c.Context(), cardID)
		if err != nil {
			return err
		}
		if rows == nil {
			rows = []db.ListTaskCommentsForClientRow{}
		}
		return c.JSON(fiber.Map{"comments": rows})
	}
	if !canWorkCollab(u) {
		return errForbidden()
	}
	if _, err := s.collabCard(c.Context(), pid, cardID); err != nil {
		return err
	}
	rows, err := s.store.Queries.ListTaskComments(c.Context(), cardID)
	if err != nil {
		return err
	}
	if rows == nil {
		rows = []db.ListTaskCommentsRow{}
	}
	return c.JSON(fiber.Map{"comments": rows})
}

type taskCommentInput struct {
	Body       string `json:"body"`
	IsInternal bool   `json:"is_internal"`
}

func (s *Server) addTaskComment(c fiber.Ctx) error {
	u := currentUser(c)
	pid, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	cardID, err := paramUUID(c, "cardId")
	if err != nil {
		return err
	}
	isClient := roleOf(u) == policy.Client
	if isClient {
		if !s.clientHasAccess(c, u.ID, pid) {
			return errForbidden()
		}
	} else if !canWorkCollab(u) {
		return errForbidden()
	}
	card, err := s.collabCard(c.Context(), pid, cardID)
	if err != nil {
		return err
	}
	if isClient && !card.VisibleToClient {
		return errForbidden()
	}
	var in taskCommentInput
	if err := decodeJSON(c, &in); err != nil {
		return err
	}
	body := strings.TrimSpace(in.Body)
	if len(body) < 1 || len(body) > 5000 {
		return errBadRequest("Comment must be 1-5000 characters")
	}
	isInternal := in.IsInternal && !isClient // a client can never write an internal comment
	row, err := s.store.Queries.AddTaskComment(c.Context(), db.AddTaskCommentParams{
		CardID: cardID, AuthorID: &u.ID, Body: body, IsInternal: isInternal,
	})
	if err != nil {
		return err
	}
	ctx := c.Context()
	preview := body
	if len(preview) > 200 {
		preview = preview[:197] + "..."
	}
	if isClient {
		_ = s.notifyCollabTeam(ctx, pid, u.ID, notify.CollabComment,
			"Client commented on "+card.Title, preview,
			"/projects/"+pid.String()+"?tab=board&card="+cardID.String())
	} else if !isInternal {
		_ = s.notifyCollabClients(ctx, pid, u.ID, notify.CollabComment,
			"New comment on "+card.Title, preview, "/client/"+pid.String())
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"id": row.ID, "created_at": row.CreatedAt})
}

// ── Visibility + approvals ───────────────────────────────────

type visibleInput struct {
	Visible bool `json:"visible"`
}

func (s *Server) setCardVisible(c fiber.Ctx) error {
	if !canManageCollab(currentUser(c)) {
		return errForbidden()
	}
	pid, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	cardID, err := paramUUID(c, "cardId")
	if err != nil {
		return err
	}
	if _, err := s.collabCard(c.Context(), pid, cardID); err != nil {
		return err
	}
	var in visibleInput
	if err := decodeJSON(c, &in); err != nil {
		return err
	}
	if err := s.store.Queries.SetCardVisibleToClient(c.Context(), db.SetCardVisibleToClientParams{
		ID: cardID, VisibleToClient: in.Visible,
	}); err != nil {
		return err
	}
	return c.JSON(fiber.Map{"ok": true, "visible_to_client": in.Visible})
}

// listTaskApprovals returns a card's approval history (newest first).
func (s *Server) listTaskApprovals(c fiber.Ctx) error {
	if !canWorkCollab(currentUser(c)) {
		return errForbidden()
	}
	pid, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	cardID, err := paramUUID(c, "cardId")
	if err != nil {
		return err
	}
	if _, err := s.collabCard(c.Context(), pid, cardID); err != nil {
		return err
	}
	rows, err := s.store.Queries.ListTaskApprovals(c.Context(), cardID)
	if err != nil {
		return err
	}
	if rows == nil {
		rows = []db.ListTaskApprovalsRow{}
	}
	return c.JSON(fiber.Map{"approvals": rows})
}

func (s *Server) requestTaskApproval(c fiber.Ctx) error {
	u := currentUser(c)
	if !canManageCollab(u) {
		return errForbidden()
	}
	pid, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	cardID, err := paramUUID(c, "cardId")
	if err != nil {
		return err
	}
	card, err := s.collabCard(c.Context(), pid, cardID)
	if err != nil {
		return err
	}
	if !card.VisibleToClient {
		return errBadRequest("Make the task visible to the client before asking for approval")
	}
	if _, err := s.store.Queries.GetPendingApproval(c.Context(), cardID); err == nil {
		return errConflict("An approval is already pending for this task")
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	id, err := s.store.Queries.RequestTaskApproval(c.Context(), db.RequestTaskApprovalParams{
		CardID: cardID, RequestedBy: &u.ID,
	})
	if err != nil {
		return err
	}
	_ = s.notifyCollabClients(c.Context(), pid, u.ID, notify.CollabApprovalAsked,
		"Approval requested: "+card.Title,
		"Please review this task and approve it or request changes.",
		"/client/"+pid.String())
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"id": id})
}

type approvalReviewInput struct {
	Status string `json:"status"` // approved | changes_requested
	Note   string `json:"note"`
}

func (s *Server) reviewTaskApproval(c fiber.Ctx) error {
	u := currentUser(c)
	pid, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	cardID, err := paramUUID(c, "cardId")
	if err != nil {
		return err
	}
	if !s.clientHasAccess(c, u.ID, pid) {
		return errForbidden()
	}
	card, err := s.collabCard(c.Context(), pid, cardID)
	if err != nil {
		return err
	}
	if !card.VisibleToClient {
		return errForbidden()
	}
	var in approvalReviewInput
	if err := decodeJSON(c, &in); err != nil {
		return err
	}
	in.Status = strings.TrimSpace(in.Status)
	if in.Status != "approved" && in.Status != "changes_requested" {
		return errBadRequest("Status must be approved or changes_requested")
	}
	note := strings.TrimSpace(in.Note)
	if in.Status == "changes_requested" && note == "" {
		return errBadRequest("Tell the team what needs to change")
	}
	if len(note) > 2000 {
		return errBadRequest("Note must be at most 2000 characters")
	}
	// Lock the pending row: two reviewers can't both decide it.
	ctx := c.Context()
	tx, err := s.store.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var apID uuid.UUID
	err = tx.QueryRow(ctx, `
		SELECT id FROM task_approvals WHERE card_id = $1 AND status = 'pending' FOR UPDATE`, cardID).Scan(&apID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return errNotFound("No approval is pending for this task")
		}
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE task_approvals
		SET status = $2, reviewed_by = $3, reviewed_at = now(), review_note = $4
		WHERE id = $1`, apID, in.Status, u.ID, note); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	if in.Status == "approved" {
		_ = s.notifyCollabTeam(ctx, pid, u.ID, notify.CollabApproved,
			"Client approved: "+card.Title, "The client signed off this task.",
			"/projects/"+pid.String()+"?tab=board&card="+cardID.String())
	} else {
		_ = s.notifyCollabTeam(ctx, pid, u.ID, notify.CollabChanges,
			"Client requested changes: "+card.Title, note,
			"/projects/"+pid.String()+"?tab=board&card="+cardID.String())
	}
	return c.JSON(fiber.Map{"ok": true, "status": in.Status})
}

// ── Messages ─────────────────────────────────────────────────

func (s *Server) listClientMessages(c fiber.Ctx) error {
	u := currentUser(c)
	pid, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	if roleOf(u) == policy.Client {
		if !s.clientHasAccess(c, u.ID, pid) {
			return errForbidden()
		}
	} else if !canWorkCollab(u) {
		return errForbidden()
	}
	rows, err := s.store.Queries.ListClientMessages(c.Context(), pid)
	if err != nil {
		return err
	}
	if rows == nil {
		rows = []db.ListClientMessagesRow{}
	}
	return c.JSON(fiber.Map{"messages": rows})
}

type messageInput struct {
	Body string `json:"body"`
}

func (s *Server) addClientMessage(c fiber.Ctx) error {
	u := currentUser(c)
	pid, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	isClient := roleOf(u) == policy.Client
	if isClient {
		if !s.clientHasAccess(c, u.ID, pid) {
			return errForbidden()
		}
	} else if !canWorkCollab(u) {
		return errForbidden()
	}
	var in messageInput
	if err := decodeJSON(c, &in); err != nil {
		return err
	}
	body := strings.TrimSpace(in.Body)
	if len(body) < 1 || len(body) > 5000 {
		return errBadRequest("Message must be 1-5000 characters")
	}
	row, err := s.store.Queries.AddClientMessage(c.Context(), db.AddClientMessageParams{
		ProjectID: pid, AuthorID: &u.ID, Body: body,
	})
	if err != nil {
		return err
	}
	ctx := c.Context()
	preview := body
	if len(preview) > 200 {
		preview = preview[:197] + "..."
	}
	if isClient {
		_ = s.notifyCollabTeam(ctx, pid, u.ID, notify.CollabMessage,
			"New message from the client", preview,
			"/projects/"+pid.String()+"?tab=client")
	} else {
		_ = s.notifyCollabClients(ctx, pid, u.ID, notify.CollabMessage,
			"New message from our team", preview, "/client/"+pid.String())
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"id": row.ID, "created_at": row.CreatedAt})
}

// ── Files ────────────────────────────────────────────────────

func (s *Server) listClientFiles(c fiber.Ctx) error {
	u := currentUser(c)
	pid, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	if roleOf(u) == policy.Client {
		if !s.clientHasAccess(c, u.ID, pid) {
			return errForbidden()
		}
	} else if !canWorkCollab(u) {
		return errForbidden()
	}
	rows, err := s.store.Queries.ListClientFiles(c.Context(), pid)
	if err != nil {
		return err
	}
	if rows == nil {
		rows = []db.ListClientFilesRow{}
	}
	return c.JSON(fiber.Map{"files": rows})
}

type shareFileInput struct {
	FileName string    `json:"file_name"`
	FileURL  string    `json:"file_url"` // /api/files/versions/<uuid>/download
	FileSize int64     `json:"file_size"`
	ClientID *uuid.UUID `json:"client_id"`
}

// addClientFile shares an already-uploaded project file with the client: the
// file stays in the project's Files section, the client sees it here.
func (s *Server) addClientFile(c fiber.Ctx) error {
	u := currentUser(c)
	if !canManageCollab(u) {
		return errForbidden()
	}
	pid, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	if _, err := s.projectRow(c, pid); err != nil {
		return err
	}
	var in shareFileInput
	if err := decodeJSON(c, &in); err != nil {
		return err
	}
	name := strings.TrimSpace(in.FileName)
	if len(name) < 1 || len(name) > 255 {
		return errBadRequest("File name is required (max 255 characters)")
	}
	versionID, ok := parseFileVersionURL(in.FileURL)
	if !ok {
		return errBadRequest("file_url must be a project file download link")
	}
	if in.FileSize < 0 {
		return errBadRequest("File size must be 0 or more")
	}
	// The version must belong to a live file on this project.
	var okVersion bool
	err = s.store.Pool.QueryRow(c.Context(), `
		SELECT EXISTS(SELECT 1 FROM file_versions fv JOIN files f ON f.id = fv.file_id
		              WHERE fv.id = $1 AND f.project_id = $2 AND f.removed_at IS NULL)`,
		versionID, pid).Scan(&okVersion)
	if err != nil {
		return err
	}
	if !okVersion {
		return errNotFound("File not found on this project")
	}
	if in.ClientID != nil {
		n, err := s.store.Queries.ProjectHasClient(c.Context(), db.ProjectHasClientParams{ProjectID: pid, ClientID: *in.ClientID})
		if err != nil {
			return err
		}
		if n == 0 {
			return errBadRequest("Client is not linked to this project")
		}
	}
	id, err := s.store.Queries.AddClientFile(c.Context(), db.AddClientFileParams{
		ProjectID: pid, ClientID: in.ClientID, UploadedBy: &u.ID,
		FileName: name, FileUrl: in.FileURL, FileSize: in.FileSize, IsClientUpload: false,
	})
	if err != nil {
		return err
	}
	_ = s.notifyCollabClients(c.Context(), pid, u.ID, notify.CollabFile,
		"New file shared: "+name, "Our team shared a file on your project.", "/client/"+pid.String())
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"id": id})
}

// parseFileVersionURL pulls the version id out of /api/files/versions/<uuid>/download.
func parseFileVersionURL(raw string) (uuid.UUID, bool) {
	raw = strings.TrimSpace(raw)
	const prefix = "/api/files/versions/"
	if !strings.HasPrefix(raw, prefix) || !strings.HasSuffix(raw, "/download") {
		return uuid.Nil, false
	}
	id, err := uuid.Parse(raw[len(prefix) : len(raw)-len("/download")])
	if err != nil {
		return uuid.Nil, false
	}
	return id, true
}

// ── Client-file uploads (presigned S3, same shape as project uploads) ──

const collabUploadTTL = 15 * time.Minute

type collabPendingUpload struct {
	ProjectID   uuid.UUID `json:"project_id"`
	UserID      uuid.UUID `json:"user_id"`
	Key         string    `json:"key"`
	Name        string    `json:"name"`
	Size        int64     `json:"size"`
	ContentType string    `json:"content_type"`
}

func collabUploadKey(id string) string { return "collab-upload:" + id }

func safeUploadName(name string) string {
	var b strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	safe := b.String()
	if len(safe) > 100 {
		safe = safe[len(safe)-100:]
	}
	return safe
}

type clientFileUploadInput struct {
	Filename    string `json:"filename"`
	Size        int64  `json:"size"`
	ContentType string `json:"content_type"`
}

func (s *Server) initClientFileUpload(c fiber.Ctx) error {
	u := currentUser(c)
	pid, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	if roleOf(u) == policy.Client {
		if !s.clientHasAccess(c, u.ID, pid) {
			return errForbidden()
		}
	} else if !canWorkCollab(u) {
		return errForbidden()
	}
	var in clientFileUploadInput
	if err := decodeJSON(c, &in); err != nil {
		return err
	}
	name, ctype, err := projects.CleanUpload(projects.UploadInput{
		Filename: in.Filename, Size: in.Size, ContentType: in.ContentType,
	}, s.cfg.MaxUploadSize)
	if err != nil {
		return err
	}
	uploadID := uuid.NewString()
	key := fmt.Sprintf("projects/%s/client-files/%s/%s", pid, uploadID, safeUploadName(name))
	url, headers, err := s.files.PresignPut(c.Context(), key, ctype, collabUploadTTL)
	if err != nil {
		return err
	}
	raw, _ := json.Marshal(collabPendingUpload{
		ProjectID: pid, UserID: u.ID, Key: key, Name: name, Size: in.Size, ContentType: ctype,
	})
	if err := s.rdb.Set(c.Context(), collabUploadKey(uploadID), raw, collabUploadTTL).Err(); err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"upload_id": uploadID, "url": url, "method": "PUT", "headers": headers,
		"expires_at": time.Now().Add(collabUploadTTL),
	})
}

func (s *Server) completeClientFileUpload(c fiber.Ctx) error {
	u := currentUser(c)
	pid, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	uploadID, err := paramUUID(c, "upload_id")
	if err != nil {
		return err
	}
	isClient := roleOf(u) == policy.Client
	if isClient {
		if !s.clientHasAccess(c, u.ID, pid) {
			return errForbidden()
		}
	} else if !canWorkCollab(u) {
		return errForbidden()
	}
	raw, err := s.rdb.Get(c.Context(), collabUploadKey(uploadID.String())).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return errNotFound("Upload expired or unknown")
		}
		return err
	}
	var p collabPendingUpload
	if err := json.Unmarshal(raw, &p); err != nil {
		return err
	}
	if p.ProjectID != pid || p.UserID != u.ID {
		return errForbidden()
	}
	if _, err := s.files.Stat(c.Context(), p.Key); err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return errBadRequest("Upload not finished yet")
		}
		return err
	}
	_ = s.rdb.Del(c.Context(), collabUploadKey(uploadID.String()))
	// Attribute the file to the project's first linked business client, if any.
	var clientID *uuid.UUID
	_ = s.store.Pool.QueryRow(c.Context(), `
		SELECT client_id FROM project_clients WHERE project_id = $1 ORDER BY created_at LIMIT 1`, pid).
		Scan(&clientID)
	id, err := s.store.Queries.AddClientFile(c.Context(), db.AddClientFileParams{
		ProjectID: pid, ClientID: clientID, UploadedBy: &u.ID,
		FileName: p.Name, FileUrl: p.Key, FileSize: p.Size, IsClientUpload: isClient,
	})
	if err != nil {
		return err
	}
	if isClient {
		_ = s.notifyCollabTeam(c.Context(), pid, u.ID, notify.CollabFile,
			"Client uploaded a file: "+p.Name, "The client shared a file on this project.",
			"/projects/"+pid.String()+"?tab=client")
	} else {
		_ = s.notifyCollabClients(c.Context(), pid, u.ID, notify.CollabFile,
			"New file shared: "+p.Name, "Our team shared a file on your project.", "/client/"+pid.String())
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"id": id})
}

// downloadClientFile redirects to a short-lived presigned URL. Shared project
// files resolve to their storage key first, so a client never needs the
// project's own download endpoint.
func (s *Server) downloadClientFile(c fiber.Ctx) error {
	u := currentUser(c)
	id, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	var pid uuid.UUID
	var fileName, fileURL string
	err = s.store.Pool.QueryRow(c.Context(), `
		SELECT project_id, file_name, file_url FROM client_files WHERE id = $1`, id).
		Scan(&pid, &fileName, &fileURL)
	if err != nil {
		if err == pgx.ErrNoRows {
			return errNotFound("File not found")
		}
		return err
	}
	if roleOf(u) == policy.Client {
		if !s.clientHasAccess(c, u.ID, pid) {
			return errForbidden()
		}
	} else if !canWorkCollab(u) {
		return errForbidden()
	}
	key := fileURL
	if versionID, ok := parseFileVersionURL(fileURL); ok {
		err := s.store.Pool.QueryRow(c.Context(), `
			SELECT storage_key FROM file_versions WHERE id = $1`, versionID).Scan(&key)
		if err != nil {
			return errNotFound("File not found")
		}
	}
	url, err := s.files.PresignGet(c.Context(), key, fileName, 15*time.Minute)
	if err != nil {
		return err
	}
	c.Set(fiber.HeaderCacheControl, "no-store")
	return c.Redirect().Status(fiber.StatusFound).To(url)
}

// ── Client portal: one project's collaboration ───────────────

func (s *Server) clientCollab(c fiber.Ctx) error {
	u := currentUser(c)
	pid, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	if !s.clientHasAccess(c, u.ID, pid) {
		return errForbidden()
	}
	cards, err := s.store.Queries.ListVisibleCardsForClient(c.Context(), pid)
	if err != nil {
		return err
	}
	if cards == nil {
		cards = []db.ListVisibleCardsForClientRow{}
	}
	messages, err := s.store.Queries.ListClientMessages(c.Context(), pid)
	if err != nil {
		return err
	}
	if messages == nil {
		messages = []db.ListClientMessagesRow{}
	}
	files, err := s.store.Queries.ListClientFiles(c.Context(), pid)
	if err != nil {
		return err
	}
	if files == nil {
		files = []db.ListClientFilesRow{}
	}
	return c.JSON(fiber.Map{"cards": cards, "messages": messages, "files": files})
}

// ── Notify helpers ───────────────────────────────────────────

func (s *Server) notifyCollabClients(ctx context.Context, pid uuid.UUID, actor uuid.UUID, typ, title, body, link string) error {
	users, err := s.store.Queries.ListProjectClientUsers(ctx, pid)
	if err != nil {
		return err
	}
	recipients := notify.Recipients{}
	for _, cu := range users {
		recipients = recipients.Add(cu.ID)
	}
	if len(recipients) == 0 {
		return nil
	}
	return notify.Send(ctx, s.store.Queries, recipients, actor, notify.Notification{
		Type: typ, ProjectID: &pid, Title: title, Body: body, Link: link,
	})
}

func (s *Server) notifyCollabTeam(ctx context.Context, pid uuid.UUID, actor uuid.UUID, typ, title, body, link string) error {
	rows, err := s.store.Pool.Query(ctx, `
		SELECT DISTINCT u.id
		FROM users u
		LEFT JOIN project_members pm ON pm.user_id = u.id AND pm.project_id = $1
		LEFT JOIN team_members tm ON tm.user_id = u.id
		LEFT JOIN teams t ON t.id = tm.team_id AND t.lead_id IS NOT NULL
		WHERE u.is_active
		  AND (pm.user_id IS NOT NULL
		       OR u.id IN (SELECT lead_id FROM teams WHERE lead_id IS NOT NULL)
		       OR u.is_admin)
		  AND u.role <> 'client'`,
		pid)
	if err != nil {
		return err
	}
	defer rows.Close()
	recipients := notify.Recipients{}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			continue
		}
		recipients = recipients.Add(id)
	}
	if len(recipients) == 0 {
		return nil
	}
	return notify.Send(ctx, s.store.Queries, recipients, actor, notify.Notification{
		Type: typ, ProjectID: &pid, Title: title, Body: body, Link: link,
	})
}
