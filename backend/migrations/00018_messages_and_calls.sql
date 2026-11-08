-- +goose Up
-- One-to-one chat between people in the organisation, plus a log of voice calls.
-- Chat is person to person, not project to project: there is no conversation row,
-- a thread is simply every message between two people.

CREATE TABLE messages (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    sender_id    uuid NOT NULL REFERENCES users (id),
    recipient_id uuid NOT NULL REFERENCES users (id),
    body         text NOT NULL CHECK (length(btrim(body)) BETWEEN 1 AND 4000),
    created_at   timestamptz NOT NULL DEFAULT now(),
    read_at      timestamptz,
    CONSTRAINT messages_not_to_self CHECK (sender_id <> recipient_id)
);

-- A thread is read newest-first between one pair, whichever way round they are.
CREATE INDEX messages_thread_idx ON messages
    (least(sender_id, recipient_id), greatest(sender_id, recipient_id), created_at DESC);
CREATE INDEX messages_unread_idx ON messages (recipient_id) WHERE read_at IS NULL;

-- Voice calls in the browser. The conversation itself is peer to peer; this is only
-- the record of who rang whom, so missed calls are visible afterwards.
CREATE TABLE calls (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    caller_id   uuid NOT NULL REFERENCES users (id),
    callee_id   uuid NOT NULL REFERENCES users (id),
    status      text NOT NULL DEFAULT 'ringing'
                CHECK (status IN ('ringing', 'answered', 'declined', 'missed', 'ended', 'failed')),
    started_at  timestamptz NOT NULL DEFAULT now(),
    answered_at timestamptz,
    ended_at    timestamptz,
    CONSTRAINT calls_not_to_self CHECK (caller_id <> callee_id)
);

CREATE INDEX calls_caller_idx ON calls (caller_id, started_at DESC);
CREATE INDEX calls_callee_idx ON calls (callee_id, started_at DESC);
-- Someone can only be in one call at a time.
CREATE UNIQUE INDEX calls_one_live_per_caller ON calls (caller_id) WHERE status IN ('ringing', 'answered');
CREATE UNIQUE INDEX calls_one_live_per_callee ON calls (callee_id) WHERE status IN ('ringing', 'answered');

-- +goose Down
DROP TABLE calls;
DROP TABLE messages;
