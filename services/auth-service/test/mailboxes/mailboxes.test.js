const { test } = require('node:test');
const assert = require('node:assert/strict');
const { mailboxesHtml } = require('../../lib/mailboxes');

test('empty', () => {
    assert.match(mailboxesHtml([]), /No Gmail inbox linked yet/);
});

test('ok mailbox has no relink copy', () => {
    const html = mailboxesHtml([{ email: 'a@b.com', status: 'ok' }]);
    assert.match(html, /a@b.com/);
    assert.equal(html.includes('expired'), false);
    assert.equal(html.includes('Relink'), false);
});

test('relink mailbox is explicit', () => {
    const html = mailboxesHtml([{ email: 'a@b.com', status: 'relink' }]);
    assert.match(html, /Google access expired/);
    assert.match(html, /Relink/);
    assert.match(html, /will not be read until you relink/);
});
