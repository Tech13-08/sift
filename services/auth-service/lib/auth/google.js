const passport = require('passport');
const GoogleStrategy = require('passport-google-oauth20').Strategy;
const pool = require('../db');
const { parseKey, encrypt } = require('../crypto/tokens');

async function upsertGoogleCredential(userId, profile, accessToken, refreshToken) {
    const email = profile.emails?.[0]?.value;
    if (!email) {
        throw new Error('Google account has no email');
    }

    const taken = await pool.query(
        `SELECT user_id FROM oauth_credentials
         WHERE provider = 'google' AND lower(email) = lower($1) AND user_id <> $2
         LIMIT 1`,
        [email, userId]
    );
    if (taken.rows[0]) {
        const err = new Error('That Gmail inbox is already linked to another Sift user.');
        err.code = 'gmail_taken';
        throw err;
    }

    const key = parseKey(process.env.TOKEN_ENCRYPTION_KEY);
    const encAccess = encrypt(key, accessToken);
    const encRefresh = refreshToken ? encrypt(key, refreshToken) : null;
    try {
        await pool.query(
            `INSERT INTO oauth_credentials (user_id, provider, email, access_token, refresh_token, expires_at)
             VALUES ($1, 'google', $2, $3, $4, NOW() + interval '1 hour')
             ON CONFLICT (user_id, provider, email)
             DO UPDATE SET
                access_token = EXCLUDED.access_token,
                refresh_token = COALESCE(EXCLUDED.refresh_token, oauth_credentials.refresh_token),
                expires_at = EXCLUDED.expires_at,
                token_invalid_notified_at = NULL`,
            [userId, email, encAccess, encRefresh]
        );
    } catch (err) {
        if (err && err.code === '23505') {
            const conflict = new Error('That Gmail inbox is already linked to another Sift user.');
            conflict.code = 'gmail_taken';
            throw conflict;
        }
        throw err;
    }

    try {
        await pool.query(
            `UPDATE users
             SET email = COALESCE(NULLIF(email, ''), $1)
             WHERE id = $2`,
            [email, userId]
        );
    } catch (err) {
        if (err && err.code === '23505') {
            const conflict = new Error('That Gmail inbox is already linked to another Sift user.');
            conflict.code = 'gmail_taken';
            throw conflict;
        }
        throw err;
    }
    return email;
}

passport.use(new GoogleStrategy({
    clientID: process.env.GOOGLE_CLIENT_ID,
    clientSecret: process.env.GOOGLE_CLIENT_SECRET,
    callbackURL: process.env.GOOGLE_REDIRECT_URI,
    passReqToCallback: true
}, async (req, accessToken, refreshToken, profile, done) => {
    try {
        if (!req.user) {
            return done(new Error('Sign in with your Sift username first, then link Gmail.'));
        }
        await upsertGoogleCredential(req.user.id, profile, accessToken, refreshToken);
        const refreshed = await pool.query('SELECT * FROM users WHERE id = $1', [req.user.id]);
        return done(null, refreshed.rows[0] || req.user);
    } catch (err) {
        return done(err);
    }
}));

module.exports = { upsertGoogleCredential };
