const passport = require('passport');
const GoogleStrategy = require('passport-google-oauth20').Strategy;
const pool = require('../db');
const { parseKey, encrypt } = require('../crypto/tokens');

passport.use(new GoogleStrategy({
    clientID: process.env.GOOGLE_CLIENT_ID,
    clientSecret: process.env.GOOGLE_CLIENT_SECRET,
    callbackURL: process.env.GOOGLE_REDIRECT_URI,
    passReqToCallback: true
}, async (req, accessToken, refreshToken, profile, done) => {
    if (!req.user) return done(new Error("No session found. Login with Discord first."));

    try {
        const key = parseKey(process.env.TOKEN_ENCRYPTION_KEY);
        const encAccess = encrypt(key, accessToken);
        const encRefresh = refreshToken ? encrypt(key, refreshToken) : null;
        await pool.query(
            `INSERT INTO oauth_credentials (user_id, provider, email, access_token, refresh_token, expires_at)
             VALUES ($1, $2, $3, $4, $5, NOW() + interval '1 hour')
             ON CONFLICT (user_id, provider, email) 
             DO UPDATE SET 
                access_token = EXCLUDED.access_token, 
                refresh_token = COALESCE(EXCLUDED.refresh_token, oauth_credentials.refresh_token), 
                expires_at = EXCLUDED.expires_at,
                token_invalid_notified_at = NULL`,
            [req.user.id, 'google', profile.emails[0].value, encAccess, encRefresh]
        );
        return done(null, req.user);
    } catch (err) {
        return done(err);
    }
}));