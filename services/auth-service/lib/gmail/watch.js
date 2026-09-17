const { google } = require('googleapis');
const pool = require('../db');
const { parseKey, encrypt, decrypt } = require('../crypto/tokens');
const { isRelinkError } = require('./errors');
const { notifyDeadGmail } = require('./notify');

function tokenKey() {
    return parseKey(process.env.TOKEN_ENCRYPTION_KEY);
}

async function getGmailClient(userId, email) {
    if (!email) throw new Error('Gmail email is required.');

    const res = await pool.query(
        `SELECT access_token, refresh_token, expires_at, last_history_id
         FROM oauth_credentials WHERE user_id = $1 AND provider = $2 AND email = $3`,
        [userId, 'google', email]
    );
    if (!res.rows[0]) throw new Error('Credentials not found.');

    const creds = res.rows[0];
    const key = tokenKey();
    const accessToken = decrypt(key, creds.access_token);
    const refreshToken = decrypt(key, creds.refresh_token);

    const auth = new google.auth.OAuth2(
        process.env.GOOGLE_CLIENT_ID,
        process.env.GOOGLE_CLIENT_SECRET
    );

    auth.setCredentials({
        access_token: accessToken,
        refresh_token: refreshToken
    });

    const isExpired = new Date(creds.expires_at) <= new Date();

    if (isExpired) {
        console.log('Token expired. Refreshing...');
        const { credentials } = await auth.refreshAccessToken();
        const encAccess = encrypt(key, credentials.access_token);
        const encRefresh = credentials.refresh_token ? encrypt(key, credentials.refresh_token) : null;

        await pool.query(
            `UPDATE oauth_credentials
             SET access_token = $1,
                 expires_at = to_timestamp($2 / 1000.0),
                 refresh_token = COALESCE($3, oauth_credentials.refresh_token),
                 token_invalid_notified_at = NULL
             WHERE user_id = $4 AND provider = 'google' AND email = $5`,
            [encAccess, credentials.expiry_date, encRefresh, userId, email]
        );

        auth.setCredentials(credentials);
        console.log('Token refreshed successfully.');
    }

    return { gmail: google.gmail({ version: 'v1', auth }), lastHistoryId: creds.last_history_id };
}

async function watchInbox(userId, email, options = {}) {
    const { gmail, lastHistoryId } = await getGmailClient(userId, email);

    const res = await gmail.users.watch({
        userId: 'me',
        requestBody: {
            labelIds: ['INBOX'],
            topicName: process.env.GOOGLE_PUBSUB_TOPIC
        }
    });

    const { historyId, expiration } = res.data;
    // First watch only - renews must not rewind historyId (would skip mail).
    const resetHistory = options.resetHistory === true || !lastHistoryId;

    if (resetHistory) {
        await pool.query(
            `UPDATE oauth_credentials
             SET last_history_id = $1, watch_expiration = $2
             WHERE user_id = $3 AND provider = 'google' AND email = $4`,
            [historyId, expiration, userId, email]
        );
    } else {
        await pool.query(
            `UPDATE oauth_credentials
             SET watch_expiration = $1
             WHERE user_id = $2 AND provider = 'google' AND email = $3`,
            [expiration, userId, email]
        );
    }

    if (typeof watchScheduler === 'function') {
        watchScheduler(userId, email, expiration);
    }

    return res.data;
}

async function listGoogleMailboxes(userId) {
    const res = await pool.query(
        `SELECT email FROM oauth_credentials WHERE user_id = $1 AND provider = 'google'`,
        [userId]
    );
    return res.rows.map((row) => row.email);
}

function withTimeout(promise, ms) {
    return Promise.race([
        promise,
        new Promise((_, reject) => {
            setTimeout(() => reject(new Error('timeout')), ms).unref();
        })
    ]);
}

async function probeMailboxStatus(userId, email, hasRefresh) {
    if (!hasRefresh) {
        await notifyDeadGmail(pool, userId, email);
        return 'relink';
    }
    try {
        const { gmail } = await withTimeout(getGmailClient(userId, email), 8000);
        await withTimeout(gmail.users.getProfile({ userId: 'me' }), 8000);
        return 'ok';
    } catch (err) {
        if (isRelinkError(err)) {
            await notifyDeadGmail(pool, userId, email);
            return 'relink';
        }
        console.error(`Gmail probe failed for ${email}: ${err.message}`);
        return 'unknown';
    }
}

async function listGoogleMailboxStatuses(userId) {
    const res = await pool.query(
        `SELECT email,
                (refresh_token IS NOT NULL AND refresh_token <> '') AS has_refresh
         FROM oauth_credentials
         WHERE user_id = $1 AND provider = 'google'
         ORDER BY email`,
        [userId]
    );
    const statuses = [];
    for (const row of res.rows) {
        statuses.push({
            email: row.email,
            status: await probeMailboxStatus(userId, row.email, row.has_refresh)
        });
    }
    return statuses;
}

let watchScheduler = null;

function setWatchScheduler(fn) {
    watchScheduler = fn;
}

module.exports = { getGmailClient, watchInbox, listGoogleMailboxes, listGoogleMailboxStatuses, setWatchScheduler };
