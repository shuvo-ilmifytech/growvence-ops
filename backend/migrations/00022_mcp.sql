-- +goose Up
-- AI assistants connect to OpsFlow over MCP (Phase 14) as the person using them. An
-- assistant holds a token that stands for that person: either a personal token they
-- created on their Account page (Claude Code, Codex), or one issued through OAuth when
-- they approved a connector (Claude.ai, ChatGPT). Only hashes of tokens are stored.

-- OAuth clients: registered dynamically (RFC 7591), or known by the HTTPS URL of their
-- client metadata document, which then is the id.
CREATE TABLE oauth_clients (
    id            text PRIMARY KEY,
    kind          text NOT NULL CHECK (kind IN ('registered', 'metadata')),
    name          text NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
    redirect_uris text[] NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),
    refreshed_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE mcp_tokens (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id            uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    kind               text NOT NULL CHECK (kind IN ('personal', 'oauth')),
    -- Personal: what the person called it. OAuth: the client's name, as approved.
    name               text NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
    client_id          text REFERENCES oauth_clients (id) ON DELETE CASCADE,
    access_hash        text NOT NULL UNIQUE,
    access_expires_at  timestamptz,          -- NULL: personal tokens don't expire
    refresh_hash       text UNIQUE,
    refresh_expires_at timestamptz,
    created_at         timestamptz NOT NULL DEFAULT now(),
    last_used_at       timestamptz,
    revoked_at         timestamptz,
    CONSTRAINT mcp_tokens_oauth_has_client CHECK ((kind = 'oauth') = (client_id IS NOT NULL))
);

CREATE INDEX mcp_tokens_user_idx ON mcp_tokens (user_id) WHERE revoked_at IS NULL;

-- +goose Down
DROP TABLE mcp_tokens;
DROP TABLE oauth_clients;
