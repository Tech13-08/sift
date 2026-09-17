const crypto = require('crypto');
const fs = require('fs');
const path = require('path');
const brand = require('./branding.generated.json');

const LOGO_CID = 'sift-logo@branding';
const LOGO_PNG = path.join(__dirname, 'sift-logo-email.png');

// Password-reset mail via SMTP (Resend in prod). No SMTP → log the link instead.
async function sendMail({ to, subject, text, html, attachments }) {
    const from = process.env.SMTP_FROM || process.env.MAIL_FROM || `${brand.product.name} <noreply@sift.local>`;
    const smtpUrl = process.env.SMTP_URL || '';
    const host = process.env.SMTP_HOST || '';

    if (!smtpUrl && !host) {
        console.warn(`[mail] SMTP not configured - logging outbound mail to=${to} subject=${subject}\n${text}`);
        return { ok: true, logged: true };
    }

    let nodemailer;
    try {
        nodemailer = require('nodemailer');
    } catch {
        console.error('[mail] nodemailer not installed; logging mail instead');
        console.warn(`[mail] to=${to} subject=${subject}\n${text}`);
        return { ok: true, logged: true };
    }

    const transporter = smtpUrl
        ? nodemailer.createTransport(smtpUrl)
        : nodemailer.createTransport({
            host,
            port: Number(process.env.SMTP_PORT || 587),
            secure: String(process.env.SMTP_SECURE || '').toLowerCase() === 'true',
            auth: process.env.SMTP_USER
                ? { user: process.env.SMTP_USER, pass: process.env.SMTP_PASS || '' }
                : undefined
        });

    await transporter.sendMail({
        from,
        to,
        subject,
        text,
        html: html || text,
        attachments: attachments || undefined
    });
    return { ok: true, logged: false };
}

function publicWebBase() {
    return (process.env.WEB_URL || process.env.AUTH_PUBLIC_URL || brand.product.url_fallback || 'http://localhost:3010').replace(/\/$/, '');
}

function newResetToken() {
    const raw = crypto.randomBytes(32).toString('hex');
    const hash = crypto.createHash('sha256').update(raw).digest('hex');
    return { raw, hash };
}

function hashResetToken(raw) {
    return crypto.createHash('sha256').update(String(raw || '')).digest('hex');
}

function escapeHtml(s) {
    return String(s || '')
        .replace(/&/g, '&amp;')
        .replace(/</g, '&lt;')
        .replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;');
}

// CID PNG logo (Gmail strips SVG). Colors from branding.generated.json.
function buildPasswordResetEmail({ username, resetUrl }) {
    const base = publicWebBase();
    const safeName = escapeHtml(username);
    const safeUrl = escapeHtml(resetUrl);
    const year = new Date().getUTCFullYear();
    const e = brand.email;
    const name = brand.product.name;
    const w = brand.logo.width || 40;
    const h = brand.logo.height || 31;
    const logoSrc = fs.existsSync(LOGO_PNG) ? `cid:${LOGO_CID}` : `${base}/sift-logo-email.png`;
    const logoImg = `<img src="${logoSrc}" width="${w}" height="${h}" alt="${escapeHtml(name)}" style="display:block;border:0;outline:none;text-decoration:none;" />`;

    const text = [
        `Hi ${username},`,
        '',
        `We received a request to reset your ${name} password.`,
        'Open this link within 1 hour to choose a new password:',
        resetUrl,
        '',
        'If you did not ask for this, you can ignore this email - your password will stay the same.',
        '',
        `- ${name}`,
        base
    ].join('\n');

    const html = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <meta name="color-scheme" content="light" />
  <title>Reset your ${escapeHtml(name)} password</title>
</head>
<body style="margin:0;padding:0;background:${e.page_bg};font-family:Georgia,'Times New Roman',serif;">
  <table role="presentation" width="100%" cellspacing="0" cellpadding="0" style="background:${e.page_bg};padding:32px 16px;">
    <tr>
      <td align="center">
        <table role="presentation" width="100%" cellspacing="0" cellpadding="0" style="max-width:520px;background:${e.card_bg};border-radius:12px;overflow:hidden;border:1px solid ${e.card_border};">
          <tr>
            <td style="padding:28px 32px 12px;background:${e.header_bg};">
              <table role="presentation" cellspacing="0" cellpadding="0">
                <tr>
                  <td style="vertical-align:middle;padding-right:12px;">
                    ${logoImg}
                  </td>
                  <td style="vertical-align:middle;">
                    <div style="font-family:Georgia,serif;font-size:28px;line-height:1;color:${e.mark};letter-spacing:-0.02em;">${escapeHtml(name)}</div>
                  </td>
                </tr>
              </table>
            </td>
          </tr>
          <tr>
            <td style="padding:28px 32px 8px;">
              <h1 style="margin:0 0 12px;font-family:system-ui,-apple-system,Segoe UI,sans-serif;font-size:22px;line-height:1.3;color:${e.title};font-weight:600;">
                Reset your password
              </h1>
              <p style="margin:0 0 16px;font-family:system-ui,-apple-system,Segoe UI,sans-serif;font-size:15px;line-height:1.55;color:${e.body};">
                Hi <strong style="color:${e.title};">${safeName}</strong>, we received a request to reset the password for your ${escapeHtml(name)} account.
              </p>
              <p style="margin:0 0 24px;font-family:system-ui,-apple-system,Segoe UI,sans-serif;font-size:15px;line-height:1.55;color:${e.body};">
                This link expires in <strong style="color:${e.title};">1 hour</strong>. If you didn’t request a reset, you can ignore this email.
              </p>
              <table role="presentation" cellspacing="0" cellpadding="0" style="margin:0 0 28px;">
                <tr>
                  <td style="border-radius:8px;background:${e.cta_bg};">
                    <a href="${safeUrl}" style="display:inline-block;padding:12px 22px;font-family:system-ui,-apple-system,Segoe UI,sans-serif;font-size:15px;font-weight:600;color:${e.cta_text};text-decoration:none;">
                      Choose a new password
                    </a>
                  </td>
                </tr>
              </table>
              <p style="margin:0 0 8px;font-family:system-ui,-apple-system,Segoe UI,sans-serif;font-size:12px;line-height:1.5;color:${e.muted};">
                Button not working? Paste this link into your browser:
              </p>
              <p style="margin:0;font-family:ui-monospace,Menlo,Consolas,monospace;font-size:11px;line-height:1.5;color:${e.link_fallback};word-break:break-all;">
                ${safeUrl}
              </p>
            </td>
          </tr>
          <tr>
            <td style="padding:20px 32px 28px;border-top:1px solid ${e.card_border};">
              <p style="margin:0;font-family:system-ui,-apple-system,Segoe UI,sans-serif;font-size:12px;line-height:1.5;color:${e.footer};">
                © ${year} ${escapeHtml(name)} · <a href="${escapeHtml(base)}" style="color:${e.footer};">${escapeHtml(base.replace(/^https?:\/\//, ''))}</a>
              </p>
            </td>
          </tr>
        </table>
      </td>
    </tr>
  </table>
</body>
</html>`;

    const attachments = [];
    if (fs.existsSync(LOGO_PNG)) {
        attachments.push({
            filename: 'sift-logo.png',
            path: LOGO_PNG,
            cid: LOGO_CID,
            contentType: 'image/png',
            contentDisposition: 'inline'
        });
    }

    return {
        subject: `Reset your ${name} password`,
        text,
        html,
        attachments
    };
}

module.exports = {
    sendMail,
    publicWebBase,
    newResetToken,
    hashResetToken,
    buildPasswordResetEmail,
    escapeHtml
};
