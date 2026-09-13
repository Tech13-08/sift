const { test } = require('node:test');
const assert = require('node:assert/strict');
const { isRelinkError } = require('./errors');

test('invalid_grant is a relink error', () => {
    assert.equal(isRelinkError(new Error('auth: cannot fetch token: 400 invalid_grant')), true);
    assert.equal(isRelinkError({ response: { data: { error: 'invalid_grant' } } }), true);
    assert.equal(isRelinkError({ code: 401, message: 'Unauthorized' }), true);
});

test('timeout is not a relink error', () => {
    assert.equal(isRelinkError(new Error('timeout')), false);
    assert.equal(isRelinkError(new Error('network down')), false);
});
