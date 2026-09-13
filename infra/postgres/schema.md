# Schema

`init.sql` is the source of truth. Update this mermaid when the schema changes.

Each user has a timezone and a local digest time (default 08:00). The summarizer fires at that civil time in that zone. Custom times are the same two columns on `/`.

```mermaid
erDiagram
    users ||--o{ oauth_credentials : has
    users ||--o{ ingested_messages : receives
    users ||--o{ digests : receives
    users ||--o{ user_mail_rules : sets
    digests ||--o{ ingested_messages : includes

    users {
        uuid id PK
        text discord_id UK
        text username
        text timezone
        time digest_local_time
    }

    oauth_credentials {
        uuid id PK
        uuid user_id FK
        text provider
        text email
        text access_token
        text refresh_token
        timestamptz expires_at
        text last_history_id
        bigint watch_expiration
        timestamptz token_invalid_notified_at
    }

    digests {
        uuid id PK
        uuid user_id FK
        timestamptz window_start
        timestamptz window_end
        text kind
        text status
        text summary
        timestamptz created_at
    }

    ingested_messages {
        uuid id PK
        uuid user_id FK
        text mailbox
        text gmail_message_id
        text from_address
        text subject
        text snippet
        text body_text
        timestamptz internal_date
        timestamptz ingested_at
        uuid digest_id FK
        text kind
        text outcome
        text fact_who
        text fact_what
        text fact_when
        text fact_summary
        vector embedding
        text gmail_thread_id
        bool in_reply_to_me
    }

    user_mail_rules {
        uuid id PK
        uuid user_id FK
        text rule_type
        text pattern
        text instruction
        int color
        timestamptz created_at
    }
```

A partial unique index on `(user_id, window_end)` where `kind = 'scheduled'` prevents a slot from running twice. Manual test digests are not on that index. Digest `kind` is `scheduled` or `manual`. Message `kind` is `notice` (keep) or `promo` (skip), not a taxonomy of email types. `in_reply_to_me` is true when Gmail's thread includes a message you sent — those are always kept. `user_mail_rules.rule_type` is `mute`, `always_show`, `job_filter`, or `instruction`. `instruction` is a digest-prompt fragment produced from a user Discord DM. `color` is an optional Discord embed color the user chose; unset means default grey. Bodies are cleared after 30 days; the rest of the row is deleted after 90. Optional `embedding` (pgvector, 768-d from Ollama `nomic-embed-text`) is used only as a backup for fuzzy insight questions; brand/from matches still win. The daily digest does not use it. Cursors live in `discord_rule_cursors` (keyed by `discord_id`, which maps to `users`).
