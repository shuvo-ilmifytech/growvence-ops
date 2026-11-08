package projects

import (
	"context"

	"github.com/google/uuid"

	"github.com/solvinx/opsflow/internal/db"
	"github.com/solvinx/opsflow/internal/policy"
)

// Alerts is what the browser rings about: work that is sitting there unhandled.
// Sales hear about approved updates nobody has sent to the client; whoever works on
// projects hears about cards assigned to them that they have never opened.
type Alerts struct {
	Items        []Alert `json:"items"`
	EverySeconds int     `json:"every_seconds"` // how often to chime while something waits
}

type Alert struct {
	Kind  string `json:"kind"` // "unsent_updates" or "unopened_cards"
	Count int64  `json:"count"`
	Label string `json:"label"`
	Href  string `json:"href"`
}

// ringEvery is the gap between chimes. The browser keeps the timer; this is the rule.
const ringEvery = 30

func (s *Service) Alerts(ctx context.Context, u db.User) (Alerts, error) {
	out := Alerts{Items: []Alert{}, EverySeconds: ringEvery}
	role := policy.Role(u.Role)
	// Nothing rings for the people who don't act on this work.
	if role == policy.HR || role == policy.CEO {
		return out, nil
	}
	row, err := s.st.AlertCounts(ctx, db.AlertCountsParams{IsSales: role == policy.Sales, Viewer: u.ID})
	if err != nil {
		return out, err
	}
	if row.UnsentUpdates > 0 {
		out.Items = append(out.Items, Alert{
			Kind:  "unsent_updates",
			Count: row.UnsentUpdates,
			Label: plural(row.UnsentUpdates, "approved update waiting to be sent to the client", "approved updates waiting to be sent to clients"),
			Href:  "/updates?view=send",
		})
	}
	if row.UnopenedCards > 0 {
		out.Items = append(out.Items, Alert{
			Kind:  "unopened_cards",
			Count: row.UnopenedCards,
			Label: plural(row.UnopenedCards, "task you haven't opened yet", "tasks you haven't opened yet"),
			Href:  "/my-work",
		})
	}
	return out, nil
}

func plural(n int64, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// MarkCardOpened records that this person has seen the card, which stops the ringing.
// Anyone who may read the card may mark it: opening it is the point.
func (s *Service) MarkCardOpened(ctx context.Context, u db.User, cardID uuid.UUID) error {
	if _, err := s.GetCard(ctx, u, cardID); err != nil {
		return err
	}
	return s.st.MarkCardViewed(ctx, db.MarkCardViewedParams{CardID: cardID, UserID: u.ID})
}
