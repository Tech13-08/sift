const client = require('prom-client');

const registry = new client.Registry();

const userActions = new client.Counter({
    name: 'sift_user_actions_total',
    help: 'Successful Sift user actions (action label only; no PII)',
    labelNames: ['action'],
    registers: [registry]
});

// Pre-register known actions so Grafana panels have series before the first real event.
for (const action of [
    'signup',
    'gmail_linked',
    'gmail_unlinked',
    'discord_linked',
    'discord_unlinked',
    'contact_email_changed',
    'account_deleted'
]) {
    userActions.inc({ action }, 0);
}

/** @param {string} action */
function recordUserAction(action) {
    if (!action) return;
    userActions.inc({ action: String(action) });
}

function metricsHandler(_req, res) {
    res.set('Content-Type', registry.contentType);
    registry
        .metrics()
        .then((body) => res.end(body))
        .catch((err) => {
            console.error(err);
            res.status(500).end('metrics_error');
        });
}

module.exports = { recordUserAction, metricsHandler, registry };
