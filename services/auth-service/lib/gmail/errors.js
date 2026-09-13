function isRelinkError(err) {
    const msg = String(err && err.message ? err.message : err);
    const body = err && err.response && err.response.data;
    if (body && (body.error === 'invalid_grant' || body.error === 'invalid_rapt')) return true;
    const code = err && (err.code || err.status || (err.response && err.response.status));
    if (code === 401 || code === 403 || code === '401' || code === '403') return true;
    return /invalid_grant|invalid_rapt|Token has been expired or revoked|unauthorized/i.test(msg);
}

module.exports = { isRelinkError };
