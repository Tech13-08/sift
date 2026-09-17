const { test } = require('node:test');
const assert = require('node:assert/strict');
const { deadGmailMessage, publicAppUrl } = require('../../lib/discord');

test('dead gmail message names the mailbox and app url', () => {
    process.env.AUTH_PUBLIC_URL = 'http://localhost:3000/';
    const msg = deadGmailMessage('you@gmail.com');
    assert.match(msg, /you@gmail.com/);
    assert.match(msg, /Relink Gmail: http:\/\/localhost:3000$/);
    assert.equal(publicAppUrl(), 'http://localhost:3000');
});
