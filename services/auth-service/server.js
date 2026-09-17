require('dotenv').config();
const crypto = require('crypto');
const express = require('express');
const session = require('express-session');
const passport = require('passport');
const { createClient } = require('redis');
const { RedisStore } = require('connect-redis');

require('./lib/auth/discord');
require('./lib/auth/google');

const { watchInbox, listGoogleMailboxes, listGoogleMailboxStatuses } = require('./lib/gmail/watch');
const { startWatchRenewer } = require('./lib/gmail/renew');
const { escapeHtml, render } = require('./lib/html');
const { mailboxesHtml } = require('./lib/mailboxes');
const { parseKey } = require('./lib/crypto/tokens');
const { ensureOauthColumns, ensureUserIdentityColumns, migratePlaintextTokens } = require('./lib/crypto/migrate');
const { mountApi } = require('./lib/api');
const { metricsHandler, recordUserAction } = require('./lib/metrics');
const pool = require('./lib/db');

const app = express();

const isProduction = process.env.NODE_ENV === 'production';
const webOrigin = (process.env.WEB_URL || '').replace(/\/$/, '');
const sessionSecret = process.env.SESSION_SECRET || (isProduction ? '' : crypto.randomBytes(32).toString('hex'));

function afterAuthRedirect() {
    return webOrigin ? `${webOrigin}/app` : '/';
}

function authFailureRedirect() {
    return webOrigin ? `${webOrigin}/?error=auth` : '/';
}

function linkTakenRedirect(kind) {
    if (!webOrigin) {
        return kind === 'gmail_taken' ? '/app/inboxes?error=gmail_taken' : '/app?error=discord_taken';
    }
    if (kind === 'gmail_taken') {
        return `${webOrigin}/app/inboxes?error=gmail_taken`;
    }
    return `${webOrigin}/app?error=discord_taken`;
}

/** Passport authenticate for account linking with clear "already taken" redirects. */
function authenticateLink(strategy, takenCode) {
    return (req, res, next) => {
        passport.authenticate(strategy, (err, user) => {
            if (err) {
                if (err.code === takenCode || /already linked/i.test(err.message || '')) {
                    return res.redirect(linkTakenRedirect(takenCode));
                }
                console.error(`${strategy} link failed:`, err);
                return res.redirect(authFailureRedirect());
            }
            if (!user) {
                return res.redirect(authFailureRedirect());
            }
            req.logIn(user, (loginErr) => {
                if (loginErr) return next(loginErr);
                next();
            });
        })(req, res, next);
    };
}

if (!sessionSecret) {
    throw new Error('SESSION_SECRET must be set in production');
}

if (!process.env.SESSION_SECRET && !isProduction) {
    console.warn('SESSION_SECRET not set; using ephemeral development secret');
}

function describeDatabaseTarget(url) {
    if (!url) return 'MISSING';
    try {
        const parsed = new URL(url);
        const db = parsed.pathname.replace(/^\//, '') || '(none)';
        const port = parsed.port || (parsed.protocol === 'postgres:' || parsed.protocol === 'postgresql:' ? '5432' : '');
        const host = port ? `${parsed.hostname}:${port}` : parsed.hostname;
        return `${host}/${db}`;
    } catch {
        return 'SET (unparseable)';
    }
}

function redisUrl() {
    const addr = process.env.REDIS_ADDR || 'localhost:6379';
    if (addr.startsWith('redis://') || addr.startsWith('rediss://')) return addr;
    return `redis://${addr}`;
}

async function start() {
    const tokenKey = parseKey(process.env.TOKEN_ENCRYPTION_KEY);
    await ensureOauthColumns(pool);
    await ensureUserIdentityColumns(pool);
    await migratePlaintextTokens(pool, tokenKey);

    const redisClient = createClient({ url: redisUrl() });
    redisClient.on('error', (err) => console.error('Redis session store:', err));
    await redisClient.connect();

    // Public URL is https (tunnel/ingress): trust proxy + Secure cookies.
    if (webOrigin) {
        app.set('trust proxy', 1);
    }
    if (webOrigin.startsWith('https://')) {
        // Traefik may send X-Forwarded-Proto: http; force https so Secure cookies stick.
        app.use((req, _res, next) => {
            req.headers['x-forwarded-proto'] = 'https';
            next();
        });
    }

    app.use(session({
        store: new RedisStore({ client: redisClient, prefix: 'sift:sess:' }),
        secret: sessionSecret,
        resave: false,
        saveUninitialized: false,
        cookie: {
            httpOnly: true,
            sameSite: 'lax',
            secure: webOrigin.startsWith('https://')
        }
    }));

    app.use(passport.initialize());
    app.use(passport.session());
    app.use(express.urlencoded({ extended: false, limit: '64kb' }));
    app.use(express.json({ limit: '256kb' }));

    if (webOrigin) {
        app.use('/api', (req, res, next) => {
            res.setHeader('Access-Control-Allow-Origin', webOrigin);
            res.setHeader('Access-Control-Allow-Credentials', 'true');
            res.setHeader('Access-Control-Allow-Headers', 'Content-Type');
            res.setHeader('Access-Control-Allow-Methods', 'GET,POST,PATCH,DELETE,OPTIONS');
            if (req.method === 'OPTIONS') {
                return res.sendStatus(204);
            }
            next();
        });
    }

    mountApi(app, { pool, listGoogleMailboxStatuses, isValidTimezone });

    app.get('/metrics', metricsHandler);

    app.get('/healthz', async (req, res) => {
        try {
            await pool.query('SELECT 1');
            await redisClient.ping();
            res.status(200).send('ok');
        } catch (err) {
            res.status(503).send('unavailable');
        }
    });

    app.get('/', async (req, res) => {
        if (webOrigin) {
            return res.redirect(req.user ? `${webOrigin}/app` : `${webOrigin}/`);
        }

        if (!req.user) {
            return res.send(render('login.html'));
        }

        try {
            const userRes = await pool.query(
                `SELECT username, timezone, to_char(digest_local_time, 'HH24:MI') AS digest_local_time
                 FROM users WHERE id = $1`,
                [req.user.id]
            );
            const row = userRes.rows[0];
            if (!row) {
                return res.status(401).send(render('login.html'));
            }

            const statuses = await listGoogleMailboxStatuses(req.user.id);
            const mailboxes = mailboxesHtml(statuses);

            let flash = '';
            if (req.query.error === 'timezone') flash = '<p>Invalid timezone.</p>';
            if (req.query.error === 'time') flash = '<p>Invalid time.</p>';

            res.send(render('home.html', {
                username: escapeHtml(row.username),
                timezone: escapeHtml(row.timezone),
                digest_time: escapeHtml(row.digest_local_time),
                mailboxes,
                flash
            }));
        } catch (err) {
            console.error(err);
            res.status(500).send('Failed to load home');
        }
    });

    app.post('/settings', async (req, res) => {
        if (!req.user) return res.redirect('/');

        const timezone = String(req.body.timezone || '').trim();
        const digestTime = String(req.body.digest_local_time || '').trim();

        if (!isValidTimezone(timezone)) {
            return res.redirect('/?error=timezone');
        }
        if (!/^\d{2}:\d{2}(:\d{2})?$/.test(digestTime)) {
            return res.redirect('/?error=time');
        }

        try {
            await pool.query(
                `UPDATE users SET timezone = $1, digest_local_time = $2::time WHERE id = $3`,
                [timezone, digestTime, req.user.id]
            );
            res.redirect('/');
        } catch (err) {
            console.error(err);
            res.status(500).send('Failed to save settings');
        }
    });

    app.get('/logout', (req, res, next) => {
        req.logout((err) => {
            if (err) return next(err);
            req.session.destroy(() => {
                res.redirect(webOrigin || '/');
            });
        });
    });

    app.get('/auth/discord', (req, res, next) => {
        if (!req.user) {
            return res.redirect(webOrigin ? `${webOrigin}/?error=login_first` : '/?error=login_first');
        }
        passport.authenticate('discord')(req, res, next);
    });

    app.get('/auth/discord/callback', authenticateLink('discord', 'discord_taken'), (req, res) => {
        recordUserAction('discord_linked');
        res.redirect(afterAuthRedirect());
    });

    app.get('/auth/google', (req, res, next) => {
        if (!req.user) {
            return res.redirect(webOrigin ? `${webOrigin}/?error=login_first` : '/?error=login_first');
        }
        passport.authenticate('google', {
            scope: ['profile', 'email', 'https://www.googleapis.com/auth/gmail.readonly'],
            accessType: 'offline',
            prompt: 'select_account consent'
        })(req, res, next);
    });

    app.get('/auth/google/callback', authenticateLink('google', 'gmail_taken'), async (req, res) => {
        recordUserAction('gmail_linked');
        try {
            const emails = await listGoogleMailboxes(req.user.id);
            for (const email of emails) {
                await watchInbox(req.user.id, email, { resetHistory: false });
            }
            res.redirect(webOrigin ? `${webOrigin}/app/inboxes` : afterAuthRedirect());
        } catch (err) {
            console.error(err);
            res.status(500).send('Linked but Watch failed.');
        }
    });

    app.get('/test-refresh', async (req, res) => {
        if (isProduction) {
            return res.status(404).end();
        }
        if (!req.user) return res.status(401).send('Log in first');
        try {
            const emails = await listGoogleMailboxes(req.user.id);
            if (!emails.length) return res.status(400).json({ message: 'No Gmail account linked' });
            const data = [];
            for (const email of emails) {
                data.push(await watchInbox(req.user.id, email, { resetHistory: false }));
            }
            res.json({ message: 'Watch refreshed', data });
        } catch (err) {
            console.error(err);
            res.status(500).send(err.message);
        }
    });

    const PORT = process.env.PORT || 3000;
    const HOST = process.env.HOST || '0.0.0.0';

    app.listen(PORT, HOST, () => {
        console.log(`Auth service listening on http://${HOST}:${PORT}`);
        console.log('Database target:', describeDatabaseTarget(process.env.DATABASE_URL));
        console.log('Session store: redis');
        startWatchRenewer().catch((err) => {
            console.error('Gmail watch renewer failed to start:', err);
        });
    });
}

function isValidTimezone(name) {
    if (!name) return false;
    try {
        return Intl.supportedValuesOf('timeZone').includes(name);
    } catch {
        try {
            new Intl.DateTimeFormat('en-US', { timeZone: name }).format();
            return true;
        } catch {
            return false;
        }
    }
}

start().catch((err) => {
    console.error('Auth service failed to start:', err);
    process.exit(1);
});
