package projects

import (
	"context"
	"strings"

	"github.com/solvinx/opsflow/internal/apperr"
	"github.com/solvinx/opsflow/internal/db"
	"github.com/solvinx/opsflow/internal/policy"
	"github.com/solvinx/opsflow/internal/validate"
)

// Search is one box over everything the user can already reach: projects (by name,
// client, client username, order ID or number), board cards, files and people.
// It reuses the list scopes, so it never reveals a project someone can't open.

const searchLimit = 30

type SearchResult struct {
	Results []db.SearchRow `json:"results"`
	Query   string         `json:"query"`
}

func (s *Service) Search(ctx context.Context, u db.User, q string) (SearchResult, error) {
	q = strings.TrimSpace(q)
	if len(q) < 2 {
		return SearchResult{Results: []db.SearchRow{}, Query: q}, nil
	}
	if len(q) > 200 {
		return SearchResult{}, apperr.Validation(validate.Errors{"q": "is too long"})
	}
	sc := scopeFor(u)
	// HR run people, not projects: they search the directory only.
	if policy.Role(u.Role) == policy.HR {
		rows, err := s.st.Search(ctx, db.SearchParams{
			Q: q, ScopeCreator: &u.ID, ScopeAssignee: &u.ID, HideDrafts: true, People: true, Lim: searchLimit,
		})
		return SearchResult{Results: rows, Query: q}, err
	}
	rows, err := s.st.Search(ctx, db.SearchParams{
		Q: q, ScopeCreator: sc.creator, ScopeSales: sc.sales, ScopeAssignee: sc.assignee, ScopeLead: sc.lead,
		HideDrafts: sc.hideDrafts, People: true, Lim: searchLimit,
	})
	if err != nil {
		return SearchResult{}, err
	}
	return SearchResult{Results: rows, Query: q}, nil
}
