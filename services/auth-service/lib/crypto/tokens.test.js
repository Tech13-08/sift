const { test } = require('node:test');
const assert = require('node:assert/strict');
const { parseKey, encrypt, encryptWithNonce, decrypt, isCiphertext } = require('./tokens');

const key = parseKey('ab'.repeat(32));

test('parseKey rejects bad input', () => {
    assert.throws(() => parseKey(''));
    assert.throws(() => parseKey('abcd'));
});

test('encrypt roundtrip', () => {
    const plain = 'ya29.a0TEST-refresh';
    const enc = encrypt(key, plain);
    assert.equal(isCiphertext(enc), true);
    assert.equal(enc.includes(plain), false);
    assert.equal(decrypt(key, enc), plain);
    assert.equal(encrypt(key, enc), enc);
});

test('plaintext passthrough', () => {
    assert.equal(decrypt(key, '1//legacy-refresh'), '1//legacy-refresh');
    assert.equal(encrypt(key, ''), '');
});

test('wrong key fails', () => {
    const other = parseKey('cd'.repeat(32));
    const enc = encrypt(key, 'secret');
    assert.throws(() => decrypt(other, enc));
});

test('node and go share nonce layout', () => {
    const interopKey = Buffer.from('000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f', 'hex');
    const nonce = Buffer.alloc(12, 0x11);
    const enc = encryptWithNonce(interopKey, nonce, 'hello');
    assert.equal(enc, 'enc:v1:ERERERERERERERER6AX409doC5AV8KR4KqGBoUVhPTJW');
    assert.equal(decrypt(interopKey, enc), 'hello');
});
