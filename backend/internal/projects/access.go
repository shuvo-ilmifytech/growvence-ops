package projects

import (
	"context"
	"net/url"
	"strings"

	"github.com/google/uuid"

	"github.com/solvinx/opsflow/internal/apperr"
	"github.com/solvinx/opsflow/internal/db"
	"github.com/solvinx/opsflow/internal/policy"
	"github.com/solvinx/opsflow/internal/store"
	"github.com/solvinx/opsflow/internal/validate"
	"github.com/solvinx/opsflow/internal/workflow"
)

// Store access: before the team can work on a client's store it needs access to it
// (a Shopify collaborator request, a WordPress login…). Each project records whether
// access has been asked for or given, and the link the client gave.

var StoreAccessStates = []string{"not_requested", "requested", "given"}

var storeAccessLabels = map[string]string{"not_requested": "Not requested", "requested": "Requested", "given": "Access given"}

type StoreAccessInput struct {
	Status string  `json:"status"`
	Link   *string `json:"link"` // the client's access link; empty clears it
}

// canSetStoreAccess: Sales (who talk to the client), and the Ops people on the project
// (its Team Leaders and whoever is working it). The CEO only reads.
func canSetStoreAccess(u db.User, a workflow.Actor, status string) bool {
	if status == string(workflow.Cancelled) {
		return false
	}
	switch policy.Role(u.Role) {
	case policy.Sales, policy.TL:
		return true
	case policy.Member:
		return a.Works()
	}
	return false
}

func (s *Service) SetStoreAccess(ctx context.Context, u db.User, id uuid.UUID, in StoreAccessInput) error {
	v := validate.Errors{}
	v.OneOf("status", in.Status, StoreAccessStates...)
	var link *string
	if in.Link != nil {
		l := strings.TrimSpace(*in.Link)
		if l != "" {
			if len(l) > 1000 {
				v.Add("link", "is too long")
			} else if p, err := url.Parse(l); err != nil || (p.Scheme != "https" && p.Scheme != "http") || p.Host == "" {
				v.Add("link", "must be a web address starting with https://")
			}
			link = &l
		}
	}
	if !v.OK() {
		return apperr.Validation(v)
	}
	return s.st.Tx(ctx, func(q *db.Queries) error {
		p, err := s.load(ctx, q, u, id)
		if err != nil {
			return err
		}
		if !canSetStoreAccess(u, actorFor(u, p.CreatedBy, p.MemberIDs), p.Status) {
			return apperr.Forbidden()
		}
		// A link sent without one keeps the current link: changing only the status is common.
		if in.Link == nil {
			link = p.StoreAccessLink
		}
		if p.StoreAccess == in.Status && deref(p.StoreAccessLink) == deref(link) {
			return nil
		}
		if err := q.SetStoreAccess(ctx, db.SetStoreAccessParams{ID: id, StoreAccess: in.Status, StoreAccessLink: link}); err != nil {
			return err
		}
		if p.StoreAccess != in.Status {
			from, to := storeAccessLabels[p.StoreAccess], storeAccessLabels[in.Status]
			if err := recordChange(ctx, q, id, u.ID, p.MemberIDs, "store_access", &from, &to, false); err != nil {
				return err
			}
			if err := s.notifyProjectActivity(ctx, q, u, p, "set store access to "+strings.ToLower(to)); err != nil {
				return err
			}
		}
		if deref(p.StoreAccessLink) != deref(link) {
			if err := recordChange(ctx, q, id, u.ID, p.MemberIDs, "store_access_link", p.StoreAccessLink, link, false); err != nil {
				return err
			}
		}
		return store.LogActivity(ctx, q, store.Activity{
			Actor: &u.ID, Action: "project.store_access_changed", EntityType: "project", EntityID: &id, ProjectID: &id,
			Metadata: map[string]any{"from": p.StoreAccess, "to": in.Status, "link_changed": deref(p.StoreAccessLink) != deref(link)},
		})
	})
}

