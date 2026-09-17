const RULE_TYPES = new Set(['mute', 'always_show', 'job_filter', 'instruction']);
const rateLimit = require('express-rate-limit');
const { cancelWatchRenewal, cancelAllWatchRenewalsForUser } = require('./gmail/renew');
const {
    USERNAME_RULES,
    PASSWORD_RULES,
    normalizeUsername,
    validateUsername,
    validatePassword,
    hashPassword,
    verifyPassword
} = require('./auth/password');
const { sendMail, publicWebBase, newResetToken, hashResetToken, buildPasswordResetEmail } = require('./mail');
const { recordUserAction } = require('./metrics');

const authLimiter = rateLimit({
    windowMs: 15 * 60 * 1000,
    limit: 30,
    standardHeaders: true,
    legacyHeaders: false,
    message: { error: 'rate_limited' }
});

const resetLimiter = rateLimit({
    windowMs: 15 * 60 * 1000,
    limit: 10,
    standardHeaders: true,
    legacyHeaders: false,
    message: { error: 'rate_limited' }
});

function requireUser(req, res) {
    if (!req.user) {
        res.status(401).json({ error: 'unauthorized' });
        return null;
    }
    return req.user;
}

function destroySession(req, res) {
    return new Promise((resolve, reject) => {
        req.logout((err) => {
            if (err) return reject(err);
            req.session.destroy((destroyErr) => {
                if (destroyErr) return reject(destroyErr);
                res.clearCookie('connect.sid');
                resolve();
            });
        });
    });
}

async function countGoogleMailboxes(pool, userId) {
    const res = await pool.query(
        `SELECT COUNT(*)::int AS n FROM oauth_credentials
         WHERE user_id = $1 AND provider = 'google'`,
        [userId]
    );
    return res.rows[0]?.n || 0;
}

function mountApi(app, { pool, listGoogleMailboxStatuses, isValidTimezone }) {
    app.get('/api/auth-rules', (_req, res) => {
        res.json({
            username: USERNAME_RULES,
            password: PASSWORD_RULES
        });
    });

    app.post('/api/register', authLimiter, async (req, res) => {
        if (req.user) {
            return res.status(400).json({ error: 'already_signed_in' });
        }
        const username = normalizeUsername(req.body?.username);
        const password = String(req.body?.password || '');
        const passwordConfirm = String(req.body?.password_confirm ?? req.body?.passwordConfirm ?? '');
        const userErr = validateUsername(username);
        if (userErr) return res.status(400).json({ error: 'username', message: userErr });
        const passErr = validatePassword(password);
        if (passErr) return res.status(400).json({ error: 'password', message: passErr });
        if (password !== passwordConfirm) {
            return res.status(400).json({
                error: 'password_mismatch',
                message: 'Passwords do not match.'
            });
        }

        try {
            const passwordHash = await hashPassword(password);
            const created = await pool.query(
                `INSERT INTO users (username, password_hash)
                 VALUES ($1, $2)
                 RETURNING *`,
                [username, passwordHash]
            );
            const user = created.rows[0];
            await new Promise((resolve, reject) => {
                req.login(user, (err) => (err ? reject(err) : resolve()));
            });
            recordUserAction('signup');
            res.status(201).json({ ok: true, id: user.id, username: user.username });
        } catch (err) {
            if (err && err.code === '23505') {
                return res.status(409).json({ error: 'username_taken', message: 'That username is taken.' });
            }
            console.error(err);
            res.status(500).json({ error: 'internal', message: 'Could not create account. Try again.' });
        }
    });

    app.post('/api/login', authLimiter, async (req, res) => {
        if (req.user) {
            return res.status(400).json({ error: 'already_signed_in' });
        }
        const username = normalizeUsername(req.body?.username);
        const password = String(req.body?.password || '');
        if (!username || !password) {
            return res.status(400).json({ error: 'credentials', message: 'Username and password are required.' });
        }

        try {
            const found = await pool.query(
                `SELECT * FROM users WHERE lower(username) = lower($1)`,
                [username]
            );
            const user = found.rows[0];
            if (!user || !(await verifyPassword(password, user.password_hash))) {
                return res.status(401).json({ error: 'invalid_credentials', message: 'Wrong username or password.' });
            }
            await new Promise((resolve, reject) => {
                req.login(user, (err) => (err ? reject(err) : resolve()));
            });
            res.json({ ok: true, id: user.id, username: user.username });
        } catch (err) {
            console.error(err);
            res.status(500).json({ error: 'internal', message: 'Could not sign in. Try again.' });
        }
    });

    app.post('/api/password/forgot', resetLimiter, async (req, res) => {
        const username = normalizeUsername(req.body?.username);
        const okBody = {
            ok: true,
            message: 'If that account has a contact email, we sent a reset link.'
        };
        if (!username) {
            return res.json(okBody);
        }
        try {
            const found = await pool.query(
                `SELECT id, username, email FROM users WHERE lower(username) = lower($1)`,
                [username]
            );
            const user = found.rows[0];
            if (!user?.email) {
                return res.json(okBody);
            }
            const { raw, hash } = newResetToken();
            await pool.query(
                `INSERT INTO password_reset_tokens (user_id, token_hash, expires_at)
                 VALUES ($1, $2, NOW() + interval '1 hour')`,
                [user.id, hash]
            );
            const link = `${publicWebBase()}/reset-password?token=${encodeURIComponent(raw)}`;
            const mail = buildPasswordResetEmail({ username: user.username, resetUrl: link });
            await sendMail({
                to: user.email,
                subject: mail.subject,
                text: mail.text,
                html: mail.html,
                attachments: mail.attachments
            });
            res.json(okBody);
        } catch (err) {
            console.error(err);
            res.json(okBody);
        }
    });

    app.post('/api/password/reset', resetLimiter, async (req, res) => {
        const token = String(req.body?.token || '').trim();
        const password = String(req.body?.password || '');
        const passwordConfirm = String(req.body?.password_confirm ?? req.body?.passwordConfirm ?? '');
        if (!token) {
            return res.status(400).json({ error: 'token', message: 'Reset link is missing or invalid.' });
        }
        const passErr = validatePassword(password);
        if (passErr) return res.status(400).json({ error: 'password', message: passErr });
        if (password !== passwordConfirm) {
            return res.status(400).json({ error: 'password_mismatch', message: 'Passwords do not match.' });
        }
        try {
            const hash = hashResetToken(token);
            const found = await pool.query(
                `SELECT id, user_id FROM password_reset_tokens
                 WHERE token_hash = $1
                   AND used_at IS NULL
                   AND expires_at > NOW()`,
                [hash]
            );
            const row = found.rows[0];
            if (!row) {
                return res.status(400).json({
                    error: 'token_invalid',
                    message: 'This reset link is invalid or has expired.'
                });
            }
            const passwordHash = await hashPassword(password);
            await pool.query(`UPDATE users SET password_hash = $1 WHERE id = $2`, [passwordHash, row.user_id]);
            await pool.query(
                `UPDATE password_reset_tokens SET used_at = NOW() WHERE id = $1`,
                [row.id]
            );
            await pool.query(
                `UPDATE password_reset_tokens SET used_at = NOW()
                 WHERE user_id = $1 AND used_at IS NULL AND id <> $2`,
                [row.user_id, row.id]
            );
            res.json({ ok: true });
        } catch (err) {
            console.error(err);
            res.status(500).json({ error: 'internal', message: 'Could not reset password.' });
        }
    });

    app.post('/api/password/change', authLimiter, async (req, res) => {
        const user = requireUser(req, res);
        if (!user) return;
        const currentPassword = String(req.body?.current_password ?? req.body?.currentPassword ?? '');
        const password = String(req.body?.password || '');
        const passwordConfirm = String(req.body?.password_confirm ?? req.body?.passwordConfirm ?? '');
        if (!currentPassword) {
            return res.status(400).json({ error: 'current_password', message: 'Current password is required.' });
        }
        const passErr = validatePassword(password);
        if (passErr) return res.status(400).json({ error: 'password', message: passErr });
        if (password !== passwordConfirm) {
            return res.status(400).json({ error: 'password_mismatch', message: 'Passwords do not match.' });
        }
        try {
            const found = await pool.query(`SELECT password_hash FROM users WHERE id = $1`, [user.id]);
            const row = found.rows[0];
            if (!row || !(await verifyPassword(currentPassword, row.password_hash))) {
                return res.status(401).json({ error: 'invalid_credentials', message: 'Current password is wrong.' });
            }
            const passwordHash = await hashPassword(password);
            await pool.query(`UPDATE users SET password_hash = $1 WHERE id = $2`, [passwordHash, user.id]);
            res.json({ ok: true });
        } catch (err) {
            console.error(err);
            res.status(500).json({ error: 'internal', message: 'Could not change password.' });
        }
    });

    app.post('/api/contact-email', async (req, res) => {
        const user = requireUser(req, res);
        if (!user) return;
        const email = String(req.body?.email || '').trim();
        if (!email) {
            return res.status(400).json({ error: 'email', message: 'Pick a linked inbox email.' });
        }
        try {
            const owned = await pool.query(
                `SELECT email FROM oauth_credentials
                 WHERE user_id = $1 AND provider = 'google' AND lower(email) = lower($2)`,
                [user.id, email]
            );
            if (!owned.rows[0]) {
                return res.status(400).json({
                    error: 'not_linked',
                    message: 'Contact email must be one of your linked Gmail inboxes.'
                });
            }
            await pool.query(`UPDATE users SET email = $1 WHERE id = $2`, [owned.rows[0].email, user.id]);
            recordUserAction('contact_email_changed');
            res.json({ ok: true, contact_email: owned.rows[0].email });
        } catch (err) {
            console.error(err);
            res.status(500).json({ error: 'internal' });
        }
    });

    app.get('/api/me', async (req, res) => {
        const user = requireUser(req, res);
        if (!user) return;

        try {
            const userRes = await pool.query(
                `SELECT id::text AS id, username, email, discord_id, timezone,
                        to_char(digest_local_time, 'HH24:MI') AS digest_local_time
                 FROM users WHERE id = $1`,
                [user.id]
            );
            const row = userRes.rows[0];
            if (!row) {
                return res.status(401).json({ error: 'unauthorized' });
            }

            const gmailRes = await pool.query(
                `SELECT 1 FROM oauth_credentials
                 WHERE user_id = $1 AND provider = 'google'
                 LIMIT 1`,
                [user.id]
            );
            const statuses = await listGoogleMailboxStatuses(user.id);
            const schedule = Boolean(row.timezone && row.digest_local_time);
            const discordLinked = Boolean(row.discord_id);
            const hasMailbox = gmailRes.rows.length > 0;

            res.json({
                id: row.id,
                username: row.username,
                email: row.email,
                contact_email: row.email,
                discord_id: row.discord_id,
                timezone: row.timezone,
                digest_local_time: row.digest_local_time,
                setup: {
                    google: hasMailbox,
                    gmail: hasMailbox,
                    schedule,
                    discord: discordLinked,
                    contact_email: Boolean(row.email)
                },
                mailboxes_need_relink: statuses.some((box) => box.status === 'relink'),
                discord_server_invite: process.env.DISCORD_SERVER_INVITE || 'https://discord.gg/sDzA28WcjP',
                discord_bot_invite: process.env.DISCORD_BOT_INVITE
                    || (process.env.DISCORD_CLIENT_ID
                        ? `https://discord.com/oauth2/authorize?client_id=${process.env.DISCORD_CLIENT_ID}&permissions=0&integration_type=0&scope=applications.commands+bot`
                        : '')
            });
        } catch (err) {
            console.error(err);
            res.status(500).json({ error: 'internal' });
        }
    });

    app.get('/api/mailboxes', async (req, res) => {
        const user = requireUser(req, res);
        if (!user) return;

        try {
            const identity = await pool.query(`SELECT email FROM users WHERE id = $1`, [user.id]);
            const contact = (identity.rows[0]?.email || '').toLowerCase();
            const statuses = await listGoogleMailboxStatuses(user.id);
            res.json({
                mailboxes: statuses.map((box) => ({
                    email: box.email,
                    status: box.status,
                    is_contact: Boolean(contact) && box.email.toLowerCase() === contact
                })),
                contact_email: identity.rows[0]?.email || null
            });
        } catch (err) {
            console.error(err);
            res.status(500).json({ error: 'internal' });
        }
    });

    app.post('/api/settings', async (req, res) => {
        const user = requireUser(req, res);
        if (!user) return;

        const timezone = String(req.body?.timezone || '').trim();
        const digestTime = String(req.body?.digest_local_time || '').trim();

        if (!isValidTimezone(timezone)) {
            return res.status(400).json({ error: 'timezone' });
        }
        if (!/^\d{2}:\d{2}(:\d{2})?$/.test(digestTime)) {
            return res.status(400).json({ error: 'time' });
        }

        try {
            const updated = await pool.query(
                `UPDATE users SET timezone = $1, digest_local_time = $2::time
                 WHERE id = $3
                 RETURNING timezone, to_char(digest_local_time, 'HH24:MI') AS digest_local_time`,
                [timezone, digestTime, user.id]
            );
            const row = updated.rows[0];
            res.json({
                ok: true,
                timezone: row.timezone,
                digest_local_time: row.digest_local_time
            });
        } catch (err) {
            console.error(err);
            res.status(500).json({ error: 'internal' });
        }
    });

    app.get('/api/rules', async (req, res) => {
        const user = requireUser(req, res);
        if (!user) return;

        try {
            const result = await pool.query(
                `SELECT id::text AS id, rule_type, pattern, instruction, color, created_at
                 FROM user_mail_rules
                 WHERE user_id = $1
                 ORDER BY created_at ASC`,
                [user.id]
            );
            res.json({ rules: result.rows });
        } catch (err) {
            console.error(err);
            res.status(500).json({ error: 'internal' });
        }
    });

    app.post('/api/rules', async (req, res) => {
        const user = requireUser(req, res);
        if (!user) return;

        const ruleType = String(req.body?.rule_type || '').trim();
        const pattern = String(req.body?.pattern || '').trim();
        const instruction = req.body?.instruction != null
            ? String(req.body.instruction).trim()
            : null;
        let color = req.body?.color;
        if (color === '' || color === undefined) color = null;
        if (color != null) {
            color = Number(color);
            if (!Number.isInteger(color)) {
                return res.status(400).json({ error: 'color' });
            }
        }

        if (!RULE_TYPES.has(ruleType)) {
            return res.status(400).json({ error: 'rule_type' });
        }
        if (!pattern) {
            return res.status(400).json({ error: 'pattern' });
        }

        try {
            const result = await pool.query(
                `INSERT INTO user_mail_rules (user_id, rule_type, pattern, instruction, color)
                 VALUES ($1, $2, $3, $4, $5)
                 RETURNING id::text AS id, rule_type, pattern, instruction, color, created_at`,
                [user.id, ruleType, pattern, instruction || null, color]
            );
            res.status(201).json(result.rows[0]);
        } catch (err) {
            console.error(err);
            res.status(500).json({ error: 'internal' });
        }
    });

    app.patch('/api/rules/:id', async (req, res) => {
        const user = requireUser(req, res);
        if (!user) return;

        const id = String(req.params.id || '').trim();
        if (!id) return res.status(400).json({ error: 'id' });

        const sets = [];
        const params = [user.id, id];
        const body = req.body || {};

        if (Object.prototype.hasOwnProperty.call(body, 'color')) {
            let color = body.color;
            if (color === '' || color === null) {
                color = null;
            } else {
                color = Number(color);
                if (!Number.isInteger(color)) {
                    return res.status(400).json({ error: 'color' });
                }
            }
            params.push(color);
            sets.push(`color = $${params.length}`);
        }
        if (Object.prototype.hasOwnProperty.call(body, 'pattern')) {
            const pattern = String(body.pattern || '').trim();
            if (!pattern) return res.status(400).json({ error: 'pattern' });
            params.push(pattern);
            sets.push(`pattern = $${params.length}`);
        }
        if (Object.prototype.hasOwnProperty.call(body, 'instruction')) {
            const instruction = body.instruction == null
                ? null
                : String(body.instruction).trim() || null;
            params.push(instruction);
            sets.push(`instruction = $${params.length}`);
        }

        if (!sets.length) {
            return res.status(400).json({ error: 'empty' });
        }

        try {
            const result = await pool.query(
                `UPDATE user_mail_rules
                 SET ${sets.join(', ')}
                 WHERE user_id = $1 AND id = $2::uuid
                 RETURNING id::text AS id, rule_type, pattern, instruction, color, created_at`,
                params
            );
            if (!result.rows[0]) {
                return res.status(404).json({ error: 'not_found' });
            }
            res.json(result.rows[0]);
        } catch (err) {
            console.error(err);
            res.status(500).json({ error: 'internal' });
        }
    });

    app.delete('/api/rules/:id', async (req, res) => {
        const user = requireUser(req, res);
        if (!user) return;

        const id = String(req.params.id || '').trim();
        if (!id) return res.status(400).json({ error: 'id' });

        try {
            const result = await pool.query(
                `DELETE FROM user_mail_rules
                 WHERE user_id = $1 AND id = $2::uuid
                 RETURNING id`,
                [user.id, id]
            );
            if (!result.rows[0]) {
                return res.status(404).json({ error: 'not_found' });
            }
            res.status(204).end();
        } catch (err) {
            console.error(err);
            res.status(500).json({ error: 'internal' });
        }
    });

    app.get('/api/digests', async (req, res) => {
        const user = requireUser(req, res);
        if (!user) return;

        let limit = parseInt(String(req.query.limit || '30'), 10);
        if (!Number.isFinite(limit) || limit < 1) limit = 30;
        if (limit > 100) limit = 100;

        try {
            const result = await pool.query(
                `SELECT id::text AS id, window_start, window_end, kind, status, summary, created_at
                 FROM digests
                 WHERE user_id = $1
                 ORDER BY created_at DESC
                 LIMIT $2`,
                [user.id, limit]
            );
            res.json({ digests: result.rows });
        } catch (err) {
            console.error(err);
            res.status(500).json({ error: 'internal' });
        }
    });

    app.get('/api/digests/:id', async (req, res) => {
        const user = requireUser(req, res);
        if (!user) return;

        const id = String(req.params.id || '').trim();
        if (!id) return res.status(400).json({ error: 'id' });

        try {
            const digestRes = await pool.query(
                `SELECT id::text AS id, window_start, window_end, kind, status, summary, created_at
                 FROM digests
                 WHERE user_id = $1 AND id = $2::uuid`,
                [user.id, id]
            );
            const digest = digestRes.rows[0];
            if (!digest) {
                return res.status(404).json({ error: 'not_found' });
            }

            const messagesRes = await pool.query(
                `SELECT id::text AS id, mailbox, from_address, subject,
                        fact_who, fact_what, fact_when, fact_summary, kind, outcome
                 FROM ingested_messages
                 WHERE user_id = $1 AND digest_id = $2::uuid
                 ORDER BY internal_date ASC NULLS LAST, ingested_at ASC`,
                [user.id, id]
            );

            res.json({ digest, messages: messagesRes.rows });
        } catch (err) {
            console.error(err);
            res.status(500).json({ error: 'internal' });
        }
    });

    app.post('/api/logout', async (req, res, next) => {
        try {
            await destroySession(req, res);
            res.json({ ok: true });
        } catch (err) {
            next(err);
        }
    });

    app.delete('/api/discord', async (req, res) => {
        const user = requireUser(req, res);
        if (!user) return;

        try {
            const prev = await pool.query(
                `SELECT discord_id FROM users WHERE id = $1`,
                [user.id]
            );
            const discordId = prev.rows[0]?.discord_id;
            await pool.query(
                `UPDATE users SET discord_id = NULL WHERE id = $1`,
                [user.id]
            );
            if (discordId) {
                await pool.query(
                    `DELETE FROM discord_rule_cursors WHERE discord_id = $1`,
                    [discordId]
                );
            }
            recordUserAction('discord_unlinked');
            res.json({ ok: true });
        } catch (err) {
            console.error(err);
            res.status(500).json({ error: 'internal' });
        }
    });

    app.delete('/api/mailboxes', async (req, res) => {
        const user = requireUser(req, res);
        if (!user) return;

        const email = String(req.body?.email || req.query?.email || '').trim();
        if (!email) {
            return res.status(400).json({ error: 'email' });
        }

        try {
            const remaining = await countGoogleMailboxes(pool, user.id);
            if (remaining <= 1) {
                return res.status(400).json({
                    error: 'last_mailbox',
                    message: 'Keep at least one linked Gmail - it is your contact email for password resets.'
                });
            }

            const deleted = await pool.query(
                `DELETE FROM oauth_credentials
                 WHERE user_id = $1 AND provider = 'google' AND lower(email) = lower($2)
                 RETURNING email`,
                [user.id, email]
            );
            if (!deleted.rows[0]) {
                return res.status(404).json({ error: 'not_found' });
            }

            cancelWatchRenewal(user.id, deleted.rows[0].email);

            await pool.query(
                `DELETE FROM ingested_messages
                 WHERE user_id = $1 AND lower(mailbox) = lower($2)`,
                [user.id, email]
            );

            const identity = await pool.query(
                `SELECT email FROM users WHERE id = $1`,
                [user.id]
            );
            const identityEmail = identity.rows[0]?.email;
            if (identityEmail && identityEmail.toLowerCase() === email.toLowerCase()) {
                const nextMailbox = await pool.query(
                    `SELECT email FROM oauth_credentials
                     WHERE user_id = $1 AND provider = 'google'
                     ORDER BY email
                     LIMIT 1`,
                    [user.id]
                );
                await pool.query(
                    `UPDATE users SET email = $1 WHERE id = $2`,
                    [nextMailbox.rows[0]?.email || null, user.id]
                );
            }

            recordUserAction('gmail_unlinked');
            res.json({ ok: true, email: deleted.rows[0].email });
        } catch (err) {
            console.error(err);
            res.status(500).json({ error: 'internal' });
        }
    });

    app.delete('/api/account', async (req, res) => {
        const user = requireUser(req, res);
        if (!user) return;

        const confirm = String(req.body?.confirm || '').trim().toLowerCase();
        if (confirm !== 'delete') {
            return res.status(400).json({ error: 'confirm' });
        }

        try {
            const prev = await pool.query(
                `SELECT discord_id FROM users WHERE id = $1`,
                [user.id]
            );
            const discordId = prev.rows[0]?.discord_id;

            cancelAllWatchRenewalsForUser(user.id);

            const deleted = await pool.query(
                `DELETE FROM users WHERE id = $1 RETURNING id`,
                [user.id]
            );
            if (!deleted.rows[0]) {
                return res.status(404).json({ error: 'not_found' });
            }

            if (discordId) {
                await pool.query(
                    `DELETE FROM discord_rule_cursors WHERE discord_id = $1`,
                    [discordId]
                );
            }

            await destroySession(req, res);
            recordUserAction('account_deleted');
            res.json({ ok: true });
        } catch (err) {
            console.error(err);
            res.status(500).json({ error: 'internal' });
        }
    });

    app.post('/api/ask', async (req, res) => {
        const user = requireUser(req, res);
        if (!user) return;

        const text = String(req.body?.text || '').trim();
        if (!text) {
            return res.status(400).json({ error: 'text' });
        }

        const summarizerURL = (process.env.SUMMARIZER_URL || 'http://summarizer-service:8090').replace(/\/$/, '');
        try {
            const upstream = await fetch(`${summarizerURL}/v1/ask`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    user_id: user.id,
                    text
                })
            });
            const raw = await upstream.text();
            let parsed = {};
            try {
                parsed = JSON.parse(raw);
            } catch {
                parsed = { reply: raw };
            }
            if (!upstream.ok) {
                return res.status(502).json({
                    error: parsed.error || `summarizer ${upstream.status}`
                });
            }
            res.json({ reply: parsed.reply || '' });
        } catch (err) {
            console.error(err);
            res.status(502).json({ error: 'summarizer_unavailable' });
        }
    });
}

module.exports = { mountApi };
