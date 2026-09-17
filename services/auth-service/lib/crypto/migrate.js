const { encrypt, decrypt, isCiphertext } = require('./tokens');

async function ensureOauthColumns(pool) {
    await pool.query(`
        ALTER TABLE oauth_credentials
        ADD COLUMN IF NOT EXISTS token_invalid_notified_at TIMESTAMPTZ
    `);

    // One Gmail address → one Sift user (matches Discord 1:1). Drop older duplicate rows first.
    await pool.query(`
        DELETE FROM oauth_credentials AS a
        USING oauth_credentials AS b
        WHERE a.provider = 'google'
          AND b.provider = 'google'
          AND lower(a.email) = lower(b.email)
          AND a.ctid <> b.ctid
          AND (
            COALESCE(a.watch_expiration, 0) < COALESCE(b.watch_expiration, 0)
            OR (
              COALESCE(a.watch_expiration, 0) = COALESCE(b.watch_expiration, 0)
              AND a.id::text > b.id::text
            )
          )
    `);
    await pool.query(`
        CREATE UNIQUE INDEX IF NOT EXISTS oauth_google_email_unique
        ON oauth_credentials (lower(email))
        WHERE provider = 'google'
    `);
}

async function ensureUserIdentityColumns(pool) {
    await pool.query(`ALTER TABLE users ADD COLUMN IF NOT EXISTS email TEXT`);
    await pool.query(`ALTER TABLE users ADD COLUMN IF NOT EXISTS password_hash TEXT`);
    await pool.query(`ALTER TABLE users ALTER COLUMN discord_id DROP NOT NULL`);
    await pool.query(`
        UPDATE users u
        SET email = c.email
        FROM (
            SELECT DISTINCT ON (user_id) user_id, email
            FROM oauth_credentials
            WHERE provider = 'google'
            ORDER BY user_id, email
        ) c
        WHERE u.id = c.user_id AND (u.email IS NULL OR u.email = '')
    `);
    await pool.query(`
        CREATE UNIQUE INDEX IF NOT EXISTS users_email_unique
        ON users (lower(email))
        WHERE email IS NOT NULL AND email <> ''
    `);
    await pool.query(`
        CREATE UNIQUE INDEX IF NOT EXISTS users_username_unique
        ON users (lower(username))
    `);
    await pool.query(`
        CREATE TABLE IF NOT EXISTS password_reset_tokens (
            id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
            user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
            token_hash TEXT NOT NULL UNIQUE,
            expires_at TIMESTAMPTZ NOT NULL,
            used_at TIMESTAMPTZ,
            created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
        )
    `);
    await pool.query(`
        CREATE INDEX IF NOT EXISTS password_reset_tokens_user_idx
        ON password_reset_tokens (user_id)
    `);
}

async function migratePlaintextTokens(pool, key) {
    const res = await pool.query(
        `SELECT user_id, provider, email, access_token, refresh_token
         FROM oauth_credentials`
    );
    let rewritten = 0;
    for (const row of res.rows) {
        if (isCiphertext(row.access_token) && (row.refresh_token == null || isCiphertext(row.refresh_token))) {
            continue;
        }
        let accessPlain;
        let refreshPlain;
        try {
            accessPlain = decrypt(key, row.access_token);
            refreshPlain = row.refresh_token == null ? null : decrypt(key, row.refresh_token);
        } catch (err) {
            console.error(`skip token migrate for ${row.email}: ${err.message}`);
            continue;
        }
        const accessEnc = encrypt(key, accessPlain);
        const refreshEnc = refreshPlain == null ? null : encrypt(key, refreshPlain);
        if (accessEnc === row.access_token && refreshEnc === row.refresh_token) {
            continue;
        }
        await pool.query(
            `UPDATE oauth_credentials
             SET access_token = $1, refresh_token = $2
             WHERE user_id = $3 AND provider = $4 AND email = $5`,
            [accessEnc, refreshEnc, row.user_id, row.provider, row.email]
        );
        rewritten += 1;
    }
    if (rewritten) {
        console.log(`Encrypted ${rewritten} oauth_credentials row(s) at rest`);
    }
}

async function claimDeadGmailNotice(pool, userId, email) {
    const res = await pool.query(
        `UPDATE oauth_credentials AS c
         SET token_invalid_notified_at = NOW()
         FROM users u
         WHERE c.user_id = u.id
           AND c.user_id = $1
           AND c.provider = 'google'
           AND lower(c.email) = lower($2)
           AND c.token_invalid_notified_at IS NULL
         RETURNING u.discord_id, c.email`,
        [userId, email]
    );
    return res.rows[0] || null;
}

async function unclaimDeadGmailNotice(pool, userId, email) {
    await pool.query(
        `UPDATE oauth_credentials
         SET token_invalid_notified_at = NULL
         WHERE user_id = $1 AND provider = 'google' AND lower(email) = lower($2)
           AND token_invalid_notified_at IS NOT NULL`,
        [userId, email]
    );
}

function alreadyEncrypted(value) {
    return isCiphertext(value);
}

module.exports = {
    ensureOauthColumns,
    ensureUserIdentityColumns,
    migratePlaintextTokens,
    claimDeadGmailNotice,
    unclaimDeadGmailNotice,
    alreadyEncrypted
};
