const fs = require('fs');
const path = require('path');

const viewsDir = path.join(__dirname, '..', 'views');

function escapeHtml(value) {
    return String(value ?? '')
        .replace(/&/g, '&amp;')
        .replace(/</g, '&lt;')
        .replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;');
}

function render(name, vars = {}) {
    let html = fs.readFileSync(path.join(viewsDir, name), 'utf8');
    for (const [key, value] of Object.entries(vars)) {
        html = html.replaceAll(`{{${key}}}`, String(value ?? ''));
    }
    return html;
}

module.exports = { escapeHtml, render };
