// Engine process ownership and lifecycle. Originally from the pull request by Flavio Giacchetti
// (silentone12725/apple-music-linux#1), adapted for Adam: Windows ownership check, Adam naming.
import { readFileSync, statSync } from 'node:fs';
import { execFile } from 'node:child_process';
import path from 'node:path';
import net from 'node:net';
import crypto from 'node:crypto';

// A PID in a lock file (or a process listening on our port) is not proof of
// ownership. Recover only an orphan of this exact executable and data directory.
// Linux: reads /proc.
export function isOwnedOrphan(pid, binary, dataDir, port, fs = { readFileSync, statSync }) {
    if (!Number.isSafeInteger(pid) || pid <= 1 || pid === process.pid) return false;
    try {
        const status = fs.readFileSync(`/proc/${pid}/status`, 'utf8');
        if (!/^PPid:\s+1$/m.test(status)) return false;
        const uid = Number(status.match(/^Uid:\s+(\d+)/m)?.[1]);
        if (uid !== process.getuid()) return false;
        const sameFile = (a, b) => a.dev === b.dev && a.ino === b.ino;
        if (!sameFile(fs.statSync(`/proc/${pid}/exe`), fs.statSync(binary))) return false;
        if (!sameFile(fs.statSync(`/proc/${pid}/cwd`), fs.statSync(dataDir))) return false;
        const args = fs.readFileSync(`/proc/${pid}/cmdline`, 'utf8').split('\0');
        const index = args.indexOf('--api');
        return index > 0 && args[index + 1] === String(port);
    } catch { return false; }
}

// Windows: the image path and command line must be ours. `query` returns
// { exe, cmdline } for a pid, or null when the process is gone.
export function queryWindowsProcess(pid) {
    return new Promise(resolve => {
        const script = `$p = Get-CimInstance Win32_Process -Filter "ProcessId=${Number(pid)}"; ` +
            `if ($p) { ConvertTo-Json -Compress @{ exe = $p.ExecutablePath; cmdline = $p.CommandLine } }`;
        execFile('powershell.exe', ['-NoProfile', '-NonInteractive', '-Command', script],
            { encoding: 'utf8', timeout: 8000, windowsHide: true }, (err, out) => {
                if (err || !out.trim()) return resolve(null);
                try { resolve(JSON.parse(out)); } catch { resolve(null); }
            });
    });
}

export async function isOwnedOrphanWindows(pid, binary, port, query = queryWindowsProcess) {
    if (!Number.isSafeInteger(pid) || pid <= 4 || pid === process.pid) return false;
    const info = await query(pid);
    if (!info?.exe || !info.cmdline) return false;
    if (path.resolve(info.exe).toLowerCase() !== path.resolve(binary).toLowerCase()) return false;
    return new RegExp(`--api\\s+"?${Number(port)}"?(\\s|$)`).test(info.cmdline);
}

export async function recoverOrphanEngine({ lockPath, binary, dataDir, port, log = console.log,
    plat = process.platform, query = queryWindowsProcess }) {
    let pid;
    try {
        const owner = readFileSync(lockPath, 'utf8').trim();
        if (!/^\d+$/.test(owner)) return;
        pid = Number(owner);
    } catch { return; }
    const owned = async () => (plat === 'win32'
        ? isOwnedOrphanWindows(pid, binary, port, query)
        : isOwnedOrphan(pid, binary, dataDir, port));
    if (!(await owned())) return;
    log(`[Adam] Stopping orphan engine (pid ${pid})`);
    // Windows has no SIGTERM: Node maps any signal to TerminateProcess.
    try { process.kill(pid, 'SIGTERM'); } catch { return; }
    for (let i = 0; i < 20; i++) {
        await new Promise(resolve => setTimeout(resolve, 100));
        if (!(await owned())) return;
    }
    // Recheck ownership immediately before escalation; never unlink the lock
    // file, since a replacement inode would allow concurrent session owners.
    if (await owned()) {
        try { process.kill(pid, 'SIGKILL'); } catch {}
    }
}

export function isPortFree(port) {
    return new Promise(resolve => {
        const server = net.createServer();
        server.once('error', () => resolve(false));
        server.listen({ port, host: '127.0.0.1', exclusive: true }, () => {
            server.close(() => resolve(true));
        });
    });
}

// One Main-owned ChildProcess object per spawn. Signals and deadlines are not
// death certificates; only this object's valid exit/close events settle death.
export function createEngineLifecycle({ prepare, spawn, onOutput = () => {}, onStderr = () => {},
    onMessage = () => {}, onError = console.error, log = console.log, isQuitting = () => false,
    onSpawn = () => {}, onReady = () => {}, onRevoke = () => {}, onExit = () => {},
    restartDelay = 1000, killDelay = 800, exitTimeout = killDelay + 800, spawnTimeout = 5000 }) {
    let current = null, starting = null, permanent = false, timer = null, version = 0;
    let blocked = false;
    let backoff = restartDelay, queue = Promise.resolve(), replacing = null;
    const records = new WeakMap();
    const report = (error, owner) => { try { onError(error, owner); } catch {} };
    const cancelRetry = () => { version++; if (timer) clearTimeout(timer); timer = null; };
    const stopped = () => permanent || isQuitting();
    const validEnd = (code, signal) => Number.isInteger(code) || (typeof signal === 'string' && /^SIG[A-Z0-9]+$/.test(signal));
    function invalidate(record, reason) {
        if (!record || record.revoked) return;
        record.revoked = true;
        for (const reject of record.readyWaiters) reject(Error('Engine owner revoked'));
        record.readyWaiters.clear();
        try { onRevoke(record.owner, reason); }
        catch { blocked = true; record.revocationFailed = true; report(Error('Engine authority revocation unconfirmed'), record.owner); }
    }
    function finish(record, code, signal) {
        if (record.exited || !validEnd(code, signal)) return;
        const retry = current === record && !record.revoked && !record.execError && !stopped();
        record.exited = true;
        invalidate(record, record.execError ? 'spawn-failed' : 'engine-exited');
        if (current === record) current = null;
        record.resolveExit({ ownerId: record.owner.id, stage: 'owned-child-exited' });
        try { onExit(record.owner); } catch { blocked = true; report(Error('Engine exit cleanup unconfirmed'), record.owner); }
        if (retry && !blocked) {
            const expected = version;
            backoff = Date.now() - record.startedAt > 30000 ? restartDelay : Math.min(backoff * 2, 30000);
            timer = setTimeout(() => { timer = null; if (!stopped() && version === expected && !current) void start(); }, backoff);
        }
    }
    function start() {
        if (stopped() || blocked) return Promise.resolve();
        if (starting) return starting;
        if (current) return Promise.resolve(current.revoked ? undefined : current.owner);
        if (timer) clearTimeout(timer); timer = null;
        const owner = Object.freeze({ id: crypto.randomUUID() });
        const record = { owner, proc: null, prepared: null, ready: false, revoked: false, exited: false,
            readyWaiters: new Set(), startedAt: Date.now() };
        record.death = new Promise(resolve => { record.resolveExit = resolve; });
        records.set(owner, record); current = record;
        starting = (async () => {
            try {
                record.prepared = await prepare(owner);
                if (stopped() || record.revoked || current !== record) {
                    invalidate(record, 'preparation-cancelled');
                    record.exited = true; record.resolveExit({ ownerId: owner.id, stage: 'never-spawned' });
                    if (current === record) current = null;
                    onExit(owner); return;
                }
                const proc = spawn(owner, record.prepared); record.proc = proc;
                if (!proc || typeof proc.once !== 'function' || typeof proc.kill !== 'function') throw Error('Invalid owned Engine child');
                proc.once('spawn', () => {
                    if (current !== record || record.revoked || record.exited) return;
                    record.ready = true; log('[Adam] Engine spawned');
                    try { onReady(owner, record.prepared); } catch { invalidate(record, 'owner-binding-failed'); }
                });
                proc.on('error', error => {
                    if (record.exited || current !== record) return;
                    record.execError = true; invalidate(record, 'child-error'); report(error, owner);
                    // A signal error on an existing PID is not proof of exit.
                });
                proc.on('exit', (code, signal) => finish(record, code, signal));
                proc.on('close', (code, signal) => finish(record, code, signal));
                const deliver = fn => data => { if (current === record && !record.revoked && !record.exited) fn(data, owner); };
                proc.stdout?.on('data', deliver(onOutput)); proc.stderr?.on('data', deliver(onStderr));
                proc.on('message', deliver(onMessage));
                onSpawn(owner, proc, record.prepared);
                return owner;
            } catch (error) {
                invalidate(record, 'start-failed'); report(error, owner);
                if (!record.proc) {
                    record.exited = true; record.resolveExit({ ownerId: owner.id, stage: 'never-spawned' });
                    if (current === record) current = null;
                    try { onExit(owner); } catch {}
                }
            }
        })().finally(() => { starting = null; });
        return starting;
    }
    const bounded = async (promise, ms, message) => {
        let deadline;
        try { return await Promise.race([promise, new Promise((_, reject) => { deadline = setTimeout(() => reject(Error(message)), ms); })]); }
        finally { clearTimeout(deadline); }
    };
    async function waitReady(owner) {
        const record = records.get(owner);
        if (!record || current !== record || record.revoked || record.exited) throw Error('Engine owner unavailable');
        if (record.ready) return owner;
        // Observe the actual owned spawn event; returning a ChildProcess object
        // does not mean exec succeeded.
        await bounded(new Promise((resolve, reject) => {
            const ready = () => { record.readyWaiters.delete(reject); resolve(); };
            record.readyWaiters.add(reject); record.proc.once('spawn', ready);
            record.death.then(() => { record.readyWaiters.delete(reject); reject(Error('Engine exited before admission')); });
        }), spawnTimeout, 'Engine spawn unconfirmed').catch(error => { invalidate(record, 'spawn-unconfirmed'); throw error; });
        if (!isCurrent(owner)) throw Error('Engine owner revoked');
        return owner;
    }
    async function retire() {
        if (starting) await bounded(starting, exitTimeout, 'Engine preparation exit unconfirmed');
        const record = current;
        if (!record) return { stage: 'no-owned-child' };
        invalidate(record, 'retiring');
        if (record.exited) return record.death;
        if (!record.proc) throw Error('Engine exit unconfirmed');
        const proc = record.proc;
        const signal = name => {
            if (current !== record || record.exited) return;
            // Exit fields prevent signalling a handle whose death may already
            // be queued, but they never substitute for the event ACK.
            if (proc.exitCode != null || proc.signalCode != null) return;
            try { proc.kill(name); } catch {} // retain ownership and wait for evidence
        };
        const escalation = setTimeout(() => signal('SIGKILL'), killDelay);
        try {
            signal('SIGTERM');
            const ack = await bounded(record.death, exitTimeout, 'Engine exit unconfirmed; replacement blocked');
            if (record.revocationFailed) throw Error('Engine authority revocation blocked');
            return ack;
        } finally { clearTimeout(escalation); }
    }
    const enqueue = fn => { const work = queue.then(fn); queue = work.catch(() => {}); return work; };
    function stop() {
        permanent = true; cancelRetry(); invalidate(current, 'stopping');
        return enqueue(retire);
    }
    function suspend() {
        cancelRetry(); invalidate(current, 'cancelled');
        return enqueue(retire);
    }
    function restart() {
        if (stopped() || blocked) return Promise.reject(Error('Engine lifecycle stopped or blocked'));
        if (replacing) return replacing;
        cancelRetry(); const requested = version; invalidate(current, 'reloading');
        const work = enqueue(async () => {
            await retire();
            if (stopped() || blocked || version !== requested) throw Error('Engine replacement cancelled');
            const owner = await start();
            return waitReady(owner);
        });
        replacing = work;
        work.then(() => { if (replacing === work) replacing = null; }, () => { if (replacing === work) replacing = null; });
        return work;
    }
    function reload() {
        if (stopped()) return Promise.resolve();
        const work = restart(); work.catch(error => report(error)); return work;
    }
    function isCurrent(owner) { return !!current && current.owner === owner && current.ready && !current.revoked && !current.exited; }
    return { start, stop, reload, restart, suspend, waitReady, isCurrent,
        isOwned: owner => !!current && current.owner === owner && !current.revoked && !current.exited,
        currentOwner: () => current && isCurrent(current.owner) ? current.owner : null,
        snapshot: () => current ? Object.freeze({ ownerId: current.owner.id, ready: current.ready, revoked: current.revoked, exited: current.exited }) : null };
}
