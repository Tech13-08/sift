const crypto = require('crypto');

const PREFIX = 'enc:v1:';
const NONCE_SIZE = 12;
const TAG_SIZE = 16;

function parseKey(raw) {
    const hexKey = String(raw || '').trim();
    if (!hexKey) {
        throw new Error('TOKEN_ENCRYPTION_KEY is required');
    }
    if (!/^[0-9a-fA-F]{64}$/.test(hexKey)) {
        throw new Error('TOKEN_ENCRYPTION_KEY must be 64 hex characters');
    }
    return Buffer.from(hexKey, 'hex');
}

function isCiphertext(stored) {
    return String(stored || '').startsWith(PREFIX);
}

function encrypt(key, plaintext) {
    if (plaintext == null || plaintext === '') return plaintext == null ? plaintext : '';
    if (isCiphertext(plaintext)) return plaintext;
    const nonce = crypto.randomBytes(NONCE_SIZE);
    return encryptWithNonce(key, nonce, plaintext);
}

function encryptWithNonce(key, nonce, plaintext) {
    const cipher = crypto.createCipheriv('aes-256-gcm', key, nonce);
    const encrypted = Buffer.concat([cipher.update(String(plaintext), 'utf8'), cipher.final()]);
    const tag = cipher.getAuthTag();
    return PREFIX + Buffer.concat([nonce, encrypted, tag]).toString('base64');
}

function decrypt(key, stored) {
    if (stored == null || stored === '') return stored == null ? stored : '';
    if (!isCiphertext(stored)) return stored;
    const raw = Buffer.from(stored.slice(PREFIX.length), 'base64');
    if (raw.length < NONCE_SIZE + TAG_SIZE) {
        throw new Error('token ciphertext too short');
    }
    const nonce = raw.subarray(0, NONCE_SIZE);
    const tag = raw.subarray(raw.length - TAG_SIZE);
    const data = raw.subarray(NONCE_SIZE, raw.length - TAG_SIZE);
    const decipher = crypto.createDecipheriv('aes-256-gcm', key, nonce);
    decipher.setAuthTag(tag);
    return Buffer.concat([decipher.update(data), decipher.final()]).toString('utf8');
}

module.exports = { PREFIX, parseKey, isCiphertext, encrypt, encryptWithNonce, decrypt };
