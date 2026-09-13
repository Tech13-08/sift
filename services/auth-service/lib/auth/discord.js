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
    scope: ['identify']
}, async (accessToken, refreshToken, profile, done) => {
    try {
        const res = await pool.query(
            `INSERT INTO users (discord_id, username) 
             VALUES ($1, $2) 
             ON CONFLICT (discord_id) DO UPDATE SET username = $2 
             RETURNING *`,
            [profile.id, profile.username]
        );
        return done(null, res.rows[0]);
    } catch (err) {
        return done(err);
    }
}));