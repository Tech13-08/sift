const pool = require('../db');
const { watchInbox } = require('./watch');

const DEFAULT_RENEW_BEFORE_MS = 24 * 60 * 60 * 1000;
const DEFAULT_RETRY_MS = 60 * 60 * 1000;
const MAX_TIMEOUT_MS = 2147483647;

const timers = new Map();

function envMs(name, fallback) {
    const raw = Number(process.env[name]);
    return Number.isFinite(raw) && raw > 0 ? raw : fallback;
}

function renewBeforeMs() {
    return envMs('WATCH_RENEW_BEFORE_MS', DEFAULT_RENEW_BEFORE_MS);
}

function mailboxKey(userId, email) {
    return `${userId}|${String(email).toLowerCase()}`;
}

function msUntilRenew(watchExpiration, now = Date.now()) {
    if (watchExpiration == null || watchExpiration === '') return 0;
    const exp = Number(watchExpiration);
    if (!Number.isFinite(exp) || exp <= 0) return 0;
    return Math.max(0, exp - renewBeforeMs() - now);
}

function isDue(watchExpiration, now, beforeMs) {
    return msUntilRenew(watchExpiration, now) === 0 || (Number(watchExpiration) - now <= beforeMs);
}

function clearMailboxTimer(key) {
    const existing = timers.get(key);
    if (existing) clearTimeout(existing);
    timers.delete(key);
}

function scheduleWatchRenewal(userId, email, expiration) {
    const key = mailboxKey(userId, email);
    clearMailboxTimer(key);

    let delay = msUntilRenew(expiration);
    if (delay > MAX_TIMEOUT_MS) delay = MAX_TIMEOUT_MS;

    const fireAt = new Date(Date.now() + delay).toISOString();
    if (delay === 0) {
        console.log(`Gmail watch renewal for ${email} is due now`);
    } else {
        console.log(`Gmail watch renewal for ${email} scheduled at ${fireAt}`);
    }

    const timer = setTimeout(() => {
        timers.delete(key);
        renewMailbox(userId, email).catch((err) => {
            console.error(`Gmail watch renewer crashed for ${email}:`, err);
        });
    }, delay);
    if (typeof timer.unref === 'function') timer.unref();
    timers.set(key, timer);
}

async function renewMailbox(userId, email) {
    const res = await pool.query(
        `SELECT watch_expiration, last_history_id
         FROM oauth_credentials
         WHERE user_id = $1 AND provider = 'google' AND email = $2`,
        [userId, email]
    );
    const row = res.rows[0];
    if (!row) return;

    if (msUntilRenew(row.watch_expiration) > 0) {
        scheduleWatchRenewal(userId, email, row.watch_expiration);
        return;
    }

    try {
        const result = await watchInbox(userId, email, {
            resetHistory: !row.last_history_id
        });
        console.log(`Gmail watch renewed for ${email}; expires ${result.expiration}`);
    } catch (err) {
        const retryMs = envMs('WATCH_RENEW_RETRY_MS', DEFAULT_RETRY_MS);
        console.error(`Gmail watch renew failed for ${email}: ${err.message}; retry in ${Math.round(retryMs / 60000)}m`);
        scheduleWatchRenewal(userId, email, Date.now() + retryMs + renewBeforeMs());
    }
}

async function startWatchRenewer() {
    const { setWatchScheduler } = require('./watch');
    setWatchScheduler(scheduleWatchRenewal);

    const res = await pool.query(
        `SELECT user_id, email, watch_expiration
         FROM oauth_credentials
         WHERE provider = 'google'
           AND refresh_token IS NOT NULL
           AND refresh_token <> ''`
    );

    if (!res.rows.length) {
        console.log('Gmail watch renewer: no mailboxes to schedule');
        return;
    }

    for (const row of res.rows) {
        scheduleWatchRenewal(row.user_id, row.email, row.watch_expiration);
    }
}

module.exports = { startWatchRenewer, scheduleWatchRenewal, msUntilRenew, isDue };
