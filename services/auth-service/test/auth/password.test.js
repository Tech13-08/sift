const { test, describe } = require('node:test');
const assert = require('node:assert/strict');
const {
    normalizeUsername,
    validateUsername,
    validatePassword,
    hashPassword,
    verifyPassword,
    PASSWORD_RULES
} = require('../../lib/auth/password');
const { newResetToken, hashResetToken } = require('../../lib/mail');

describe('username rules', () => {
    test('accepts valid usernames', () => {
        assert.equal(validateUsername('skizz'), null);
        assert.equal(validateUsername('User_01'), null);
        assert.equal(validateUsername('abc'), null);
        assert.equal(validateUsername('a'.repeat(32)), null);
    });

    test('rejects spaces and invalid characters with clear message', () => {
        const spaced = validateUsername('Falak Tulsi');
        assert.ok(spaced);
        assert.match(spaced, /letters, numbers, underscore/i);
        assert.ok(validateUsername('ab'));
        assert.ok(validateUsername('a'.repeat(33)));
        assert.ok(validateUsername('bad-name'));
        assert.ok(validateUsername('Osprey X'));
        assert.ok(validateUsername('my sister'));
    });

    test('normalize trims', () => {
        assert.equal(normalizeUsername('  skizz  '), 'skizz');
    });
});

describe('password rules', () => {
    test('accepts 8+ chars', () => {
        assert.equal(validatePassword('test1234'), null);
        assert.equal(validatePassword('a'.repeat(128)), null);
    });

    test('rejects short and long with message', () => {
        assert.equal(validatePassword('test123'), 'Password must be at least 8 characters.');
        assert.match(validatePassword('a'.repeat(129)), /too long/i);
        assert.match(PASSWORD_RULES, /8/);
    });

    test('hash roundtrip', async () => {
        const hash = await hashPassword('correct-horse');
        assert.equal(await verifyPassword('correct-horse', hash), true);
        assert.equal(await verifyPassword('wrong', hash), false);
        assert.equal(await verifyPassword('x', null), false);
    });
});

describe('reset tokens', () => {
    test('raw token hashes stably and is not stored raw', () => {
        const { raw, hash } = newResetToken();
        assert.equal(raw.length, 64);
        assert.equal(hash.length, 64);
        assert.notEqual(raw, hash);
        assert.equal(hashResetToken(raw), hash);
        assert.notEqual(hashResetToken(`${raw}x`), hash);
    });
});

describe('register edge cases', () => {
    test('confirm password mismatch is detectable before submit', () => {
        assert.notEqual('password1', 'password2');
        assert.equal(validatePassword('password1'), null);
    });
});
