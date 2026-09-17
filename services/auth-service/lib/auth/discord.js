const passport = require('passport');
const DiscordStrategy = require('passport-discord').Strategy;
const pool = require('../db');

passport.serializeUser((user, done) => done(null, user.id));
passport.deserializeUser(async (id, done) => {
    const res = await pool.query('SELECT * FROM users WHERE id = $1', [id]);
    done(null, res.rows[0]);
});

passport.use(new DiscordStrategy({
    clientID: process.env.DISCORD_CLIENT_ID,
    clientSecret: process.env.DISCORD_CLIENT_SECRET,
    callbackURL: process.env.DISCORD_REDIRECT_URI,
    scope: ['identify'],
    passReqToCallback: true
}, async (req, accessToken, refreshToken, profile, done) => {
    try {
        if (!req.user) {
            return done(new Error('Sign in with your Sift username first, then link Discord.'));
        }

        const taken = await pool.query(
            `SELECT id FROM users
             WHERE discord_id = $1 AND id <> $2`,
            [profile.id, req.user.id]
        );
        if (taken.rows[0]) {
            const err = new Error('That Discord account is already linked to another Sift user.');
            err.code = 'discord_taken';
            return done(err);
        }

        const res = await pool.query(
            `UPDATE users
             SET discord_id = $1
             WHERE id = $2
             RETURNING *`,
            [profile.id, req.user.id]
        );
        return done(null, res.rows[0]);
    } catch (err) {
        return done(err);
    }
}));
