-- +goose Up
-- "Unopened task": a card assigned to someone who has never opened it.
-- One row per person per card, written the first time they open it.
CREATE TABLE card_views (
    card_id   uuid NOT NULL REFERENCES project_cards (id) ON DELETE CASCADE,
    user_id   uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    viewed_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (card_id, user_id)
);

-- Cards that already exist count as seen: nobody should be rung about yesterday's work
-- the moment this ships. Only cards assigned from now on can be "unopened".
INSERT INTO card_views (card_id, user_id)
SELECT ca.card_id, ca.user_id FROM card_assignees ca;

-- +goose Down
DROP TABLE card_views;
