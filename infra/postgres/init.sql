CREATE EXTENSION IF NOT EXISTS pgcrypto;
CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE IF NOT EXISTS users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email TEXT UNIQUE,
    password_hash TEXT,
    discord_id TEXT UNIQUE,
    username TEXT NOT NULL,
    timezone TEXT NOT NULL DEFAULT 'UTC',
    digest_local_time TIME NOT NULL DEFAULT '08:00'
);

CREATE UNIQUE INDEX IF NOT EXISTS users_username_unique
    ON users (lower(username));

CREATE TABLE IF NOT EXISTS oauth_credentials (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID REFERENCES users(id) ON DELETE CASCADE,
    provider TEXT NOT NULL,
    email TEXT NOT NULL,
    access_token TEXT NOT NULL,
    refresh_token TEXT,
    expires_at TIMESTAMP WITH TIME ZONE NOT NULL,
    last_history_id TEXT,
    watch_expiration BIGINT,
    token_invalid_notified_at TIMESTAMPTZ,
    UNIQUE(user_id, provider, email)
);

-- One Gmail address across all users (Discord-style 1:1 binding).
CREATE UNIQUE INDEX IF NOT EXISTS oauth_google_email_unique
    ON oauth_credentials (lower(email))
    WHERE provider = 'google';

CREATE TABLE IF NOT EXISTS digests (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    window_start TIMESTAMPTZ NOT NULL,
    window_end TIMESTAMPTZ NOT NULL,
    kind TEXT NOT NULL DEFAULT 'scheduled',
    status TEXT NOT NULL DEFAULT 'completed',
    summary TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS digests_user_scheduled_window
    ON digests (user_id, window_end)
    WHERE kind = 'scheduled';

CREATE TABLE IF NOT EXISTS ingested_messages (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    mailbox TEXT NOT NULL,
    gmail_message_id TEXT NOT NULL,
    from_address TEXT,
    subject TEXT,
    snippet TEXT,
    body_text TEXT,
    internal_date TIMESTAMPTZ,
    ingested_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    digest_id UUID REFERENCES digests(id) ON DELETE SET NULL,
    kind TEXT,
    outcome TEXT,
    fact_who TEXT,
    fact_what TEXT,
    fact_when TEXT,
    fact_summary TEXT,
    embedding vector(768),
    gmail_thread_id TEXT,
    in_reply_to_me BOOLEAN NOT NULL DEFAULT false,
    UNIQUE (mailbox, gmail_message_id)
);

CREATE INDEX IF NOT EXISTS ingested_messages_pending_idx
    ON ingested_messages (user_id, ingested_at)
    WHERE digest_id IS NULL;

CREATE TABLE IF NOT EXISTS user_mail_rules (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    rule_type TEXT NOT NULL,
    pattern TEXT NOT NULL,
    instruction TEXT,
    color INTEGER,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS user_mail_rules_user_idx
    ON user_mail_rules (user_id);

CREATE INDEX IF NOT EXISTS ingested_messages_ingested_at_idx
    ON ingested_messages (ingested_at);

CREATE INDEX IF NOT EXISTS ingested_messages_embedding_idx
    ON ingested_messages
    USING hnsw (embedding vector_cosine_ops);

CREATE TABLE IF NOT EXISTS discord_rule_cursors (
    discord_id TEXT PRIMARY KEY,
    last_message_id TEXT NOT NULL
);

-- Password reset (hashed token; raw token only sent by email).
CREATE TABLE IF NOT EXISTS password_reset_tokens (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL UNIQUE,
    expires_at TIMESTAMPTZ NOT NULL,
    used_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS password_reset_tokens_user_idx
    ON password_reset_tokens (user_id);
