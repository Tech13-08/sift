const bcrypt = require('bcryptjs');

const USERNAME_RE = /^[a-zA-Z0-9_]{3,32}$/;
const BCRYPT_ROUNDS = 12;

function normalizeUsername(raw) {
    return String(raw || '').trim();
}

function validateUsername(username) {
    if (!USERNAME_RE.test(username)) {
        return 'Username must be 3-32 characters: letters, numbers, underscore.';
    }
    return null;
}

function validatePassword(password) {
    const p = String(password || '');
    if (p.length < 8) {
        return 'Password must be at least 8 characters.';
    }
    if (p.length > 128) {
        return 'Password is too long (max 128 characters).';
    }
    return null;
}

const USERNAME_RULES = '3-32 characters; letters, numbers, and underscores only (no spaces).';
const PASSWORD_RULES = 'At least 8 characters (max 128).';

async function hashPassword(password) {
    return bcrypt.hash(String(password), BCRYPT_ROUNDS);
}

async function verifyPassword(password, passwordHash) {
    if (!passwordHash) return false;
    return bcrypt.compare(String(password), passwordHash);
}

module.exports = {
    USERNAME_RE,
    USERNAME_RULES,
    PASSWORD_RULES,
    normalizeUsername,
    validateUsername,
    validatePassword,
    hashPassword,
    verifyPassword
};
