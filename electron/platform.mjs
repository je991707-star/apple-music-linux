// Cross-platform helpers for main.mjs. Everything platform-dependent that can be a pure
// function takes `plat` explicitly so it is unit-testable from any OS.
import path from 'path';
import os from 'os';
import net from 'net';
import { execFile } from 'child_process';

export const IS_WIN = process.platform === 'win32';
export const IS_LINUX = process.platform === 'linux';

// Directory that holds the `apple-music-linux` config folder. Windows: %APPDATA%. Inside a
// Flatpak the real ~/.config is invisible and XDG_CONFIG_HOME is the app's own directory.
export function configRoot(plat = process.platform, env = process.env, home = os.homedir()) {
    if (plat === 'win32') return env.APPDATA || path.join(home, 'AppData', 'Roaming');
    return env.FLATPAK_ID && env.XDG_CONFIG_HOME ? env.XDG_CONFIG_HOME : path.join(home, '.config');
}

export const engineFileName = (plat = process.platform) => (plat === 'win32' ? 'engine.exe' : 'engine');

// Environment additions that let the engine find its bundled native libraries.
// Linux uses LD_LIBRARY_PATH; Windows resolves DLLs through PATH (and the exe's own folder).
export function engineLibEnv({ plat = process.platform, env = process.env, vlcDir, ffDir, resDir, hasVlc, hasFfmpeg }) {
    const sep = plat === 'win32' ? ';' : ':';
    const out = {};
    if (plat === 'win32') {
        const dirs = [];
        if (hasVlc) dirs.push(vlcDir);
        if (hasFfmpeg) dirs.push(path.join(ffDir, 'bin'));
        if (resDir) dirs.push(resDir);
        if (hasVlc) out.VLC_PLUGIN_PATH = path.join(vlcDir, 'plugins');
        out.PATH = [...dirs, env.PATH || env.Path].filter(Boolean).join(sep);
        return out;
    }
    if (hasVlc) {
        out.LD_LIBRARY_PATH = [vlcDir, path.join(ffDir, 'lib'), env.LD_LIBRARY_PATH].filter(Boolean).join(sep);
        out.VLC_PLUGIN_PATH = path.join(vlcDir, 'plugins');
    }
    if (hasFfmpeg) out.PATH = [path.join(ffDir, 'bin'), env.PATH].filter(Boolean).join(sep);
    return out;
}

// Discord Rich Presence IPC endpoints: named pipes on Windows, Unix sockets elsewhere.
export function discordIpcPaths(plat = process.platform, env = process.env) {
    const paths = [];
    if (plat === 'win32') {
        for (let i = 0; i < 10; i++) paths.push(`\\\\.\\pipe\\discord-ipc-${i}`);
        return paths;
    }
    const bases = [env.XDG_RUNTIME_DIR, env.TMPDIR, env.TMP, env.TEMP, '/tmp'].filter(Boolean);
    const subdirs = ['', 'snap.discord/', 'app/com.discordapp.Discord/', 'app/com.discordapp.DiscordCanary/'];
    for (const base of bases)
        for (const sub of subdirs)
            for (let i = 0; i < 10; i++) paths.push(path.join(base, sub + `discord-ipc-${i}`));
    return paths;
}

// pid listening on a TCP port from `netstat -ano` output (Windows).
export function parseNetstatListeningPid(out, port) {
    for (const line of String(out).split(/\r?\n/)) {
        const f = line.trim().split(/\s+/);
        // TCP  127.0.0.1:20025  0.0.0.0:0  LISTENING  1234
        if (f.length >= 5 && f[0] === 'TCP' && f[3] === 'LISTENING' && f[1].endsWith(':' + port)) {
            const pid = parseInt(f[4], 10);
            if (pid > 0) return pid;
        }
    }
    return 0;
}

// pid listening on a TCP port from `ss -tlnpH` output (Linux).
export function parseSsListeningPid(out) {
    const m = String(out).match(/pid=(\d+)/);
    return m ? parseInt(m[1], 10) : 0;
}

function run(cmd, args, timeout) {
    return new Promise(resolve => {
        execFile(cmd, args, { encoding: 'utf8', timeout, windowsHide: true },
            (err, stdout) => resolve(err ? '' : stdout));
    });
}

export async function listeningPid(port, plat = process.platform) {
    if (plat === 'win32') return parseNetstatListeningPid(await run('netstat', ['-ano', '-p', 'tcp'], 4000), port);
    return parseSsListeningPid(await run('ss', ['-tlnpH', `sport = :${port}`], 2000));
}

// True when nothing accepts connections on the loopback port. Works on every OS.
export function isPortFree(port, host = '127.0.0.1') {
    return new Promise(resolve => {
        const s = net.createServer();
        s.once('error', () => resolve(false));
        s.once('listening', () => s.close(() => resolve(true)));
        s.listen(port, host);
    });
}

// A YAML double-quoted scalar. JSON string syntax is a subset of it, so backslashes in Windows
// paths and any quotes are escaped correctly.
export const yamlStr = (v) => JSON.stringify(String(v));
