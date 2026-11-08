-- +goose Up
-- Files sent in chat, one-to-one or in a group. They belong to the message, not to a
-- project, so they are kept apart from `files`. The browser uploads straight to object
-- storage first; the message is written once every upload is confirmed.
-- A message may now be only a file, so its text may be empty.
ALTER TABLE messages DROP CONSTRAINT messages_body_check;
ALTER TABLE messages ADD CONSTRAINT messages_body_check CHECK (length(btrim(body)) <= 4000);
ALTER TABLE group_messages DROP CONSTRAINT group_messages_body_check;
ALTER TABLE group_messages ADD CONSTRAINT group_messages_body_check CHECK (length(btrim(body)) <= 4000);

CREATE TABLE chat_attachments (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    message_id       uuid REFERENCES messages (id) ON DELETE CASCADE,
    group_message_id uuid REFERENCES group_messages (id) ON DELETE CASCADE,
    uploaded_by      uuid NOT NULL REFERENCES users (id),
    name             text NOT NULL CHECK (length(name) BETWEEN 1 AND 255),
    size_bytes       bigint NOT NULL CHECK (size_bytes > 0),
    mime_type        text NOT NULL,
    storage_key      text NOT NULL UNIQUE,
    created_at       timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT chat_attachments_one_message CHECK (num_nonnulls(message_id, group_message_id) = 1)
);

CREATE INDEX chat_attachments_message_idx ON chat_attachments (message_id) WHERE message_id IS NOT NULL;
CREATE INDEX chat_attachments_group_message_idx ON chat_attachments (group_message_id) WHERE group_message_id IS NOT NULL;

-- +goose Down
DROP TABLE chat_attachments;
DELETE FROM group_messages WHERE btrim(body) = '';
DELETE FROM messages WHERE btrim(body) = '';
ALTER TABLE group_messages DROP CONSTRAINT group_messages_body_check;
ALTER TABLE group_messages ADD CONSTRAINT group_messages_body_check CHECK (length(btrim(body)) BETWEEN 1 AND 4000);
ALTER TABLE messages DROP CONSTRAINT messages_body_check;
ALTER TABLE messages ADD CONSTRAINT messages_body_check CHECK (length(btrim(body)) BETWEEN 1 AND 4000);
