// Keeps the config directory from growing without bound and removes what earlier versions
// left behind. Deletion is rule-based and conservative:
//   * only paths named below are ever removed (never a glob over the whole directory),
//   * anything live (credentials, prefs, DRM session, identity) is on a protected list,
//   * stale profile data must have been untouched for STALE_DAYS,
//   * nothing outside the config directory is touched, and symlinks are removed, not followed.
import { lstatSync, readdirSync, rmSync, writeFileSync, readFileSync, mkdirSync } from 'fs';
import path from 'path';

const DAY = 24 * 3600 * 1000;
const MB = 1024 * 1024;
export const STALE_DAYS = 14;

// Live state. A rule that would touch one of these (or anything inside) is dropped.
export const PROTECTED = [
    'adam-store.enc', 'electron-prefs.json', 'eq.json', 'mv-dec.key', 'hrtf.wav',
    'adamplay', 'drm/files', 'engine-data', 'electron-session',
];

// Written by the hybris/Android-wrapper era or by earlier app names; nothing reads them now.
export const OBSOLETE = [
    'wrapper', 'wrapper-data', 'webview-data',
    'drm/rootfs', 'drm/hybris-linker', 'drm/drm-rootless', 'drm/drm-native',
];

// Chromium profile entries that sit at the config root from before userData moved to
// electron-session/. Removed only once untouched for STALE_DAYS.
export const STALE_ROOT_PROFILE = [
    'Cache', 'Code Cache', 'GPUCache', 'GPUPersistentCache', 'DawnGraphiteCache', 'DawnWebGPUCache',
    'Crashpad', 'Dictionaries', 'DIPS', 'DIPS-wal', 'DIPS-shm', 'Local Storage', 'Session Storage',
    'Network Persistent State', 'Partitions', 'Preferences', 'Local State', 'Shared Dictionary',
    'SharedStorage', 'SharedStorage-wal', 'Trust Tokens', 'Trust Tokens-journal', 'blob_storage',
    'DevToolsActivePort', 'SingletonLock', 'SingletonCookie', 'SingletonSocket',
];

// Regenerable caches inside the live profile, capped. Over the cap they are deleted at startup,
// before Chromium opens them, and rebuilt on demand.
export const CACHE_CAPS = [
    { rel: 'electron-session/Partitions/apple-music/Cache',      maxBytes: 400 * MB },
    { rel: 'electron-session/Partitions/apple-music/Code Cache', maxBytes: 100 * MB },
    { rel: 'electron-session/Cache',                             maxBytes: 100 * MB },
    { rel: 'electron-session/Code Cache',                        maxBytes: 50 * MB },
];

// Crashed atomic writes leave these behind; they are never read again.
const CHROMIUM_TEMP = /^\.org\.chromium\.Chromium\./;
const TEMP_MAX_AGE = 1 * DAY;
const CRASH_DUMP_MAX_AGE = 7 * DAY;

function lstat(p) { try { return lstatSync(p); } catch { return null; } }

// Total size and newest mtime of a file or tree (symlinks are not followed).
export function measure(p) {
    const st = lstat(p);
    if (!st) return { bytes: 0, newest: 0 };
    let bytes = st.isFile() ? st.size : 0, newest = st.mtimeMs;
    if (st.isDirectory()) {
        let names = [];
        try { names = readdirSync(p); } catch { return { bytes, newest }; }
        for (const n of names) {
            const m = measure(path.join(p, n));
            bytes += m.bytes; if (m.newest > newest) newest = m.newest;
        }
    }
    return { bytes, newest };
}

const inside = (root, p) => { const r = path.relative(root, p); return r !== '' && !r.startsWith('..') && !path.isAbsolute(r); };
const overlaps = (a, b) => a === b || a.startsWith(b + path.sep) || b.startsWith(a + path.sep);

// Decide what to delete. Pure with respect to the filesystem (it only reads).
export function planCleanup(root, now = Date.now()) {
    const plan = [];
    const protectedAbs = PROTECTED.map(r => path.join(root, r));
    const add = (rel, reason, { allowInsideProtected = false } = {}) => {
        const abs = path.join(root, rel);
        if (!inside(root, abs) || !lstat(abs)) return;
        // Protected entries are never deleted; a cache cap may sit *inside* electron-session.
        if (protectedAbs.some(pr => (allowInsideProtected ? abs === pr : overlaps(abs, pr)))) return;
        plan.push({ path: abs, reason, bytes: measure(abs).bytes });
    };

    for (const rel of OBSOLETE) add(rel, 'obsolete');

    for (const rel of STALE_ROOT_PROFILE) {
        const abs = path.join(root, rel);
        if (!lstat(abs)) continue;
        const { newest } = measure(abs);
        if (now - newest >= STALE_DAYS * DAY) add(rel, `stale profile data (untouched ${Math.floor((now - newest) / DAY)}d)`);
    }

    for (const { rel, maxBytes } of CACHE_CAPS) {
        const abs = path.join(root, rel);
        if (!lstat(abs)) continue;
        const { bytes } = measure(abs);
        if (bytes > maxBytes) add(rel, `cache over cap (${Math.round(bytes / MB)}MB > ${Math.round(maxBytes / MB)}MB)`, { allowInsideProtected: true });
    }

    // Leftover temp files from interrupted writes, in the profile root and each partition.
    const tempDirs = ['electron-session'];
    try {
        for (const part of readdirSync(path.join(root, 'electron-session', 'Partitions'))) tempDirs.push(path.join('electron-session', 'Partitions', part));
    } catch { /* no partitions yet */ }
    for (const d of tempDirs) {
        let names = [];
        try { names = readdirSync(path.join(root, d)); } catch { continue; }
        for (const n of names) {
            if (!CHROMIUM_TEMP.test(n)) continue;
            const abs = path.join(root, d, n), st = lstat(abs);
            if (st && now - st.mtimeMs >= TEMP_MAX_AGE) {
                plan.push({ path: abs, reason: 'leftover temp file', bytes: st.size });
            }
        }
    }

    // Old crash dumps.
    const done = path.join(root, 'electron-session', 'Crashpad', 'completed');
    try {
        for (const n of readdirSync(done)) {
            const abs = path.join(done, n), st = lstat(abs);
            if (st && now - st.mtimeMs >= CRASH_DUMP_MAX_AGE) plan.push({ path: abs, reason: 'old crash dump', bytes: st.size });
        }
    } catch { /* none */ }

    return plan;
}

// Run the plan. Returns { removed, bytes, errors }.
export function runCleanup(root, { now = Date.now(), dryRun = false, log = () => {} } = {}) {
    const plan = planCleanup(root, now);
    let removed = 0, bytes = 0, errors = 0;
    for (const item of plan) {
        if (!inside(root, item.path)) continue; // defence in depth
        log(`${dryRun ? 'would remove' : 'removing'} ${path.relative(root, item.path)} (${Math.round(item.bytes / 1024)}KB): ${item.reason}`);
        if (dryRun) { removed++; bytes += item.bytes; continue; }
        try { rmSync(item.path, { recursive: true, force: true }); removed++; bytes += item.bytes; }
        catch (e) { errors++; log(`could not remove ${item.path}: ${e.message}`); }
    }
    return { removed, bytes, errors, plan };
}

// Run at most once per `everyMs` so startup does not walk large cache trees every launch.
export function runCleanupThrottled(root, { everyMs = DAY, now = Date.now(), log } = {}) {
    const stamp = path.join(root, '.cleanup-stamp');
    try { if (now - Number(readFileSync(stamp, 'utf8')) < everyMs) return null; } catch { /* first run */ }
    const res = runCleanup(root, { now, log });
    try { mkdirSync(root, { recursive: true }); writeFileSync(stamp, String(now)); } catch { /* ignore */ }
    return res;
}

// Files in the per-user cache directory (~/.cache/adam) that no current code reads: the plaintext
// library cache the encrypted one replaced, WebKit-era caches, and research graph databases that
// tools wrote into the app cache by mistake (regenerable with the scripts in .sentrux/).
export const OBSOLETE_CACHE = ['library.json', 'WebKitCache', 'CacheStorage', 'apk-graph.db', 'win-graph.db'];

export function cleanupCache(cacheRoot, { dryRun = false, log = () => {} } = {}) {
    let removed = 0, bytes = 0, errors = 0;
    for (const rel of OBSOLETE_CACHE) {
        const abs = path.join(cacheRoot, rel);
        if (!inside(cacheRoot, abs) || !lstat(abs)) continue;
        const size = measure(abs).bytes;
        log(`${dryRun ? 'would remove' : 'removing'} cache/${rel} (${Math.round(size / 1024)}KB): obsolete`);
        if (dryRun) { removed++; bytes += size; continue; }
        try { rmSync(abs, { recursive: true, force: true }); removed++; bytes += size; }
        catch (e) { errors++; log(`could not remove ${abs}: ${e.message}`); }
    }
    return { removed, bytes, errors };
}
