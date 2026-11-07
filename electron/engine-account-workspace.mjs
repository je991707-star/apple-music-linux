import fs from 'node:fs';
import path from 'node:path';
import crypto from 'node:crypto';

// Main-only Linux workspaces. No user-supplied relative path or account data is
// accepted. FD-relative opens stay in the pinned directory across name swaps.
export function createAccountWorkspaceStore(parent) {
    const records = new WeakMap();
    let root = null, closed = false, live = 0;
    const fail = () => { throw Error('Account workspace unavailable'); };
    const anchor = fd => `/proc/self/fd/${fd}`;
    const flags = fs.constants.O_RDONLY | fs.constants.O_DIRECTORY | fs.constants.O_NOFOLLOW;
    function directory(file, expected) {
        if (fs.realpathSync(file) !== file) fail();
        const st = fs.lstatSync(file);
        if (!st.isDirectory() || st.uid !== process.getuid() || (st.mode & 0o777) !== 0o700 ||
            (expected && (st.dev !== expected.dev || st.ino !== expected.ino))) fail();
        return st;
    }
    function pin(file) {
        directory(file); const fd = fs.openSync(file, flags);
        try { const st = fs.fstatSync(fd); directory(file, st); return { file, fd, dev: st.dev, ino: st.ino }; }
        catch (error) { fs.closeSync(fd); throw error; }
    }
    function rootDirectory() {
        if (closed || process.platform !== 'linux' || !path.isAbsolute(parent || '') || path.resolve(parent) !== parent) fail();
        if (root) { directory(parent, root.parent); directory(root.file, root); return root; }
        const p = pin(parent);
        try {
            const name = path.basename(fs.mkdtempSync(`${anchor(p.fd)}/account-workspaces-`));
            root = { ...pin(path.join(parent, name)), parent: p };
            return root;
        } catch (error) { fs.closeSync(p.fd); throw error; }
    }
    function checkPinned(p) {
        const st = fs.fstatSync(p.fd);
        if (st.dev !== p.dev || st.ino !== p.ino) fail();
        directory(p.file, p);
    }
    function scan(fd, depth = 0, budget = { n: 0 }) {
        if (depth > 8) fail();
        for (const name of fs.readdirSync(anchor(fd))) {
            if (++budget.n > 1024) fail();
            const file = `${anchor(fd)}/${name}`, st = fs.lstatSync(file);
            if (st.uid !== process.getuid()) fail();
            if (st.isDirectory()) {
                if ((st.mode & 0o777) !== 0o700) fail();
                const sub = fs.openSync(file, flags);
                try { scan(sub, depth + 1, budget); } finally { fs.closeSync(sub); }
            } else if (!st.isFile() || st.nlink !== 1 || (st.mode & 0o777) !== 0o600) fail();
        }
    }
    function assert(handle) {
        try {
            const record = records.get(handle);
            if (!record || record.revoked || record.closed) fail();
            rootDirectory(); for (const p of record.dirs) checkPinned(p);
            scan(record.files.fd);
            if (record.config) {
                const fd = fs.openSync(`${anchor(record.engine.fd)}/config.yaml`, fs.constants.O_RDONLY | fs.constants.O_NOFOLLOW | fs.constants.O_NONBLOCK);
                try { const st = fs.fstatSync(fd); if (!st.isFile() || st.nlink !== 1 || st.uid !== process.getuid() || (st.mode & 0o777) !== 0o600 || st.ino !== record.config.ino || st.dev !== record.config.dev || st.size !== record.config.size) fail(); }
                finally { fs.closeSync(fd); }
            }
            return record;
        } catch { fail(); }
    }
    function create() {
        try {
            const r = rootDirectory(), id = crypto.randomUUID();
            fs.mkdirSync(`${anchor(r.fd)}/${id}`, { mode: 0o700 });
            const base = pin(path.join(r.file, id));
            const made = [base];
            try {
                for (const name of ['engine', 'files', 'home', 'config', 'cache', 'data', 'android-data', 'identity']) {
                    fs.mkdirSync(`${anchor(base.fd)}/${name}`, { mode: 0o700 });
                    made.push(pin(path.join(base.file, name)));
                }
                const handle = Object.freeze({ id });
                records.set(handle, { dirs: made, engine: made[1], files: made[2], home: made[3], configDir: made[4], cache: made[5], data: made[6], android: made[7], identity: made[8], revoked: false, closed: false });
                assert(handle); live++; return handle;
            } catch (error) { for (const p of made) fs.closeSync(p.fd); throw error; }
        } catch { fail(); }
    }
    function paths(handle) { const r = assert(handle); return Object.freeze({ engineDir: r.engine.file, filesDir: r.files.file }); }
    function writeConfig(handle, port, runtimeLib64Dir = '') {
        if (typeof runtimeLib64Dir !== 'string' || (runtimeLib64Dir && (!path.isAbsolute(runtimeLib64Dir) || path.resolve(runtimeLib64Dir) !== runtimeLib64Dir))) fail();
        if (!Number.isInteger(port) || port < 1024 || port > 65532) fail();
        const r = assert(handle);
        const contents = ['storefront: us', 'media-user-token: ""', 'authorization-token: ""',
            `decrypt-m3u8-port: 127.0.0.1:${port + 2}`, `get-m3u8-port: 127.0.0.1:${port + 3}`,
            'get-m3u8-from-device: true', 'get-m3u8-mode: hires', 'aac-type: aac-lc', 'alac-max: 192000',
            `drm-base-dir: ${JSON.stringify(r.files.file)}`,
            ...(runtimeLib64Dir ? [`drm-runtime-lib64-dir: ${JSON.stringify(runtimeLib64Dir)}`] : [])].join('\n') + '\n';
        try {
            const fd = fs.openSync(`${anchor(r.engine.fd)}/config.yaml`, fs.constants.O_WRONLY | fs.constants.O_CREAT | fs.constants.O_EXCL | fs.constants.O_NOFOLLOW, 0o600);
            try { fs.writeFileSync(fd, contents); r.config = fs.fstatSync(fd); } finally { fs.closeSync(fd); }
            assert(handle);
        } catch { fail(); }
    }
    return { create, assert, paths, writeConfig,
        environment(handle, runtimeLib64Dir = '') {
            const r = assert(handle);
            if (typeof runtimeLib64Dir !== 'string' || (runtimeLib64Dir && (!path.isAbsolute(runtimeLib64Dir) || path.resolve(runtimeLib64Dir) !== runtimeLib64Dir))) fail();
            return Object.freeze({ HOME: r.home.file, XDG_CONFIG_HOME: r.configDir.file,
                XDG_CACHE_HOME: r.cache.file, XDG_DATA_HOME: r.data.file,
                ANDROID_DATA: r.android.file, ADAM_ADAMPLAY_DIR: r.identity.file,
                ...(runtimeLib64Dir ? { ANDROID_ROOT: path.dirname(runtimeLib64Dir) } : {}) });
        },
        revoke(handle) { const r = records.get(handle); if (r) r.revoked = true; },
        release(handle) { const r = records.get(handle); if (!r || r.closed) return; r.revoked = true; r.closed = true; live--; for (const p of r.dirs) fs.closeSync(p.fd); },
        close() { if (live !== 0) fail(); closed = true; if (root) { fs.closeSync(root.fd); fs.closeSync(root.parent.fd); root = null; } } };
}
