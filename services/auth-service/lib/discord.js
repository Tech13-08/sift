const DEFAULT_API = 'https://discord.com/api/v10';

function publicAppUrl() {
    return String(process.env.AUTH_PUBLIC_URL || 'http://localhost:3000').replace(/\/$/, '');
}

function deadGmailMessage(email) {
    return `Sift cannot read mail for ${email}. Google access expired. Relink Gmail: ${publicAppUrl()}`;
}

async function sendDiscordDM(discordID, content) {
    const token = String(process.env.DISCORD_BOT_TOKEN || '').trim().replace(/^['"]|['"]$/g, '');
    if (!token) {
        throw new Error('DISCORD_BOT_TOKEN is unset');
    }
    if (!discordID) {
        throw new Error('missing discord id');
    }
    const text = String(content || '').trim();
    if (!text) return;

    const channel = await discordDo('POST', '/users/@me/channels', token, { recipient_id: discordID });
    if (!channel.id) {
        throw new Error(`discord dm channel: ${channel.message || 'missing id'}`);
    }
    const posted = await discordDo('POST', `/channels/${channel.id}/messages`, token, { content: text });
    if (!posted.id) {
        throw new Error(`discord message: ${posted.message || 'missing id'}`);
    }
}

async function discordDo(method, path, token, body) {
    const resp = await fetch(`${DEFAULT_API}${path}`, {
        method,
        headers: {
            Authorization: `Bot ${token}`,
            'Content-Type': 'application/json',
            'User-Agent': 'Sift (https://github.com/sift, 0.1)'
        },
        body: JSON.stringify(body),
        signal: AbortSignal.timeout(20000)
    });
    const raw = await resp.text();
    let parsed = {};
    try {
        parsed = raw ? JSON.parse(raw) : {};
    } catch {
        parsed = { message: raw };
    }
    if (!resp.ok) {
        throw new Error(`discord HTTP ${resp.status}: ${String(parsed.message || raw).slice(0, 200)}`);
    }
    return parsed;
}

module.exports = { sendDiscordDM, deadGmailMessage, publicAppUrl };
