const { escapeHtml } = require('./html');

function mailboxesHtml(statuses) {
    if (!statuses.length) return '<p>No Gmail inbox linked yet.</p>';

    const items = statuses.map((box) => {
        const email = escapeHtml(box.email);
        if (box.status === 'relink') {
            return `<li>${email} — Google access expired. <a href="/auth/google">Relink</a></li>`;
        }
        if (box.status === 'unknown') {
            return `<li>${email} — could not verify Google access</li>`;
        }
        return `<li>${email}</li>`;
    });

    const note = statuses.some((box) => box.status === 'relink')
        ? '<p>Mail from that inbox will not be read until you relink.</p>'
        : '';

    return `<ul>${items.join('')}</ul>${note}`;
}

module.exports = { mailboxesHtml };
