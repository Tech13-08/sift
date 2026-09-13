const { claimDeadGmailNotice, unclaimDeadGmailNotice } = require('../crypto/migrate');
const { sendDiscordDM, deadGmailMessage } = require('../discord');

async function notifyDeadGmail(pool, userId, email) {
    const claimed = await claimDeadGmailNotice(pool, userId, email);
    if (!claimed) return;
    try {
        await sendDiscordDM(claimed.discord_id, deadGmailMessage(claimed.email));
        console.log(`dead gmail discord dm sent mailbox=${claimed.email}`);
    } catch (err) {
        console.error(`dead gmail discord dm failed for ${claimed.email}: ${err.message}`);
        await unclaimDeadGmailNotice(pool, userId, claimed.email);
    }
}

module.exports = { notifyDeadGmail };
