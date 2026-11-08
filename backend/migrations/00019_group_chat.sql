-- +goose Up
-- Group chat: a named conversation anyone can start — one for Sales, one for Ops —
-- kept apart from one-to-one messages, because "read" here is per member and per
-- group, not per message.

CREATE TABLE chat_groups (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name       text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 80),
    created_by uuid NOT NULL REFERENCES users (id),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE chat_group_members (
    group_id     uuid NOT NULL REFERENCES chat_groups (id) ON DELETE CASCADE,
    user_id      uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    added_at     timestamptz NOT NULL DEFAULT now(),
    -- Everything written before this moment has been seen by this member.
    last_read_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (group_id, user_id)
);

CREATE INDEX chat_group_members_user_idx ON chat_group_members (user_id);

CREATE TABLE group_messages (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    group_id   uuid NOT NULL REFERENCES chat_groups (id) ON DELETE CASCADE,
    sender_id  uuid NOT NULL REFERENCES users (id),
    body       text NOT NULL CHECK (length(btrim(body)) BETWEEN 1 AND 4000),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX group_messages_thread_idx ON group_messages (group_id, created_at DESC);

-- +goose Down
DROP TABLE group_messages;
DROP TABLE chat_group_members;
DROP TABLE chat_groups;
