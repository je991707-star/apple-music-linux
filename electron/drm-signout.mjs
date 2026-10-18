// Main-process side of a DRM sign-out: the backstop request for the engine and
// the order of the steps (record, forget credentials, reload). The engine owns
// every session file; this module only ever writes a new, uniquely named request
// file (logout-pending.backstop-<nonce>) that the engine completes on start.
import * as nodeFs from 'fs';
import path from 'path';
import { randomBytes as nodeRandomBytes } from 'crypto';

/** Accepts only the two sign-out scopes the engine knows; anything else is null. */
export function validBackstop(v) {
    return v === 'account' || v === 'device' ? v : null;
}

/**
 * Durably write a backstop request. The rename is the commit point: before it
 * nothing is visible; after it the engine will see the request.
 * @returns {{ committed: boolean, durable: boolean, error?: string }}
 */
export function writeBackstop(dir, scope, { fs = nodeFs, randomBytes = nodeRandomBytes, platform = process.platform } = {}) {
    const nonce = randomBytes(16).toString('hex');
    const final = path.join(dir, `logout-pending.backstop-${nonce}`);
    const tmp = `${final}.tmp`;
    try {
        fs.mkdirSync(dir, { recursive: true, mode: 0o700 });
        const fd = fs.openSync(tmp, 'wx', 0o600);
        try {
            fs.writeSync(fd, scope);
            fs.fsyncSync(fd);
        } finally {
            fs.closeSync(fd);
        }
        fs.renameSync(tmp, final);
    } catch (e) {
        try { fs.unlinkSync(tmp); } catch {}
        return { committed: false, durable: false, error: e.message };
    }
    // Windows cannot open a directory to fsync it: there directory-entry durability
    // is not guaranteed (Adam has no DRM sign-in on Windows).
    if (platform === 'win32') return { committed: true, durable: false };
    try {
        const dfd = fs.openSync(dir, 'r');
        try { fs.fsyncSync(dfd); } finally { fs.closeSync(dfd); }
        return { committed: true, durable: true };
    } catch (e) {
        return { committed: true, durable: false, error: e.message };
    }
}

/**
 * The fixed order of a sign-out's main-process steps.
 * 1. backstop set (the engine route failed) → record it first; if it could not be
 *    committed, stop: no credentials removed, no reload, report 'marker'.
 * 2. forget the stored Apple credentials; failure → 'credentials', no reload.
 * 3. reload the engine (only for a sign-out, reload !== false).
 * A backstop committed but not crash-durable still proceeds: the restarted engine
 * finds the visible request at once ('durability' warning).
 */
export async function clearCredentialsFlow({ reload = true, backstop = null, record, deleteCredentials, reloadEngine }) {
    const scope = reload ? validBackstop(backstop) : null;
    let warning;
    if (scope) {
        const r = record(scope);
        if (!r.committed) return { ok: false, error: 'marker' };
        if (!r.durable) warning = 'durability';
    }
    if (!(await deleteCredentials())) return { ok: false, error: 'credentials' };
    if (reload) reloadEngine();
    return warning ? { ok: true, warning } : { ok: true };
}
