// Pure cross-platform helpers (platform.mjs) and Windows integration helpers (win-integration.mjs).
//   node --test electron/test/platform.test.mjs
import { test } from 'node:test';
import assert from 'node:assert/strict';
import zlib from 'node:zlib';
import {
    configRoot, engineFileName, engineLibEnv, discordIpcPaths,
    parseNetstatListeningPid, parseSsListeningPid, isPortFree, yamlStr, migrateLegacyConfig, migrateStoreKeys,
} from '../platform.mjs';
import { makeIconPng, parseCmdArg, mediaSessionScript } from '../win-integration.mjs';

test('configRoot: %APPDATA% on Windows, ~/.config elsewhere, Flatpak override', () => {
    assert.equal(configRoot('win32', { APPDATA: 'C:\\Users\\a\\AppData\\Roaming' }, 'C:\\Users\\a'), 'C:\\Users\\a\\AppData\\Roaming');
    assert.match(configRoot('win32', {}, '/h'), /AppData[\\/]Roaming$/);
    assert.equal(configRoot('linux', {}, '/home/a'), '/home/a/.config');
    assert.equal(configRoot('linux', { FLATPAK_ID: 'x', XDG_CONFIG_HOME: '/flat' }, '/home/a'), '/flat');
    assert.equal(configRoot('linux', { XDG_CONFIG_HOME: '/flat' }, '/home/a'), '/home/a/.config');
});

test('engineFileName', () => {
    assert.equal(engineFileName('win32'), 'engine.exe');
    assert.equal(engineFileName('linux'), 'engine');
});

test('engineLibEnv: Linux uses LD_LIBRARY_PATH, Windows prepends PATH', () => {
    const lin = engineLibEnv({ plat: 'linux', env: { PATH: '/usr/bin', LD_LIBRARY_PATH: '/old' },
        vlcDir: '/r/vlc', ffDir: '/r/ff', resDir: '/r', hasVlc: true, hasFfmpeg: true });
    assert.equal(lin.LD_LIBRARY_PATH, '/r/vlc:/r/ff/lib:/old');
    assert.equal(lin.VLC_PLUGIN_PATH, '/r/vlc/plugins');
    assert.equal(lin.PATH, '/r/ff/bin:/usr/bin');
    const win = engineLibEnv({ plat: 'win32', env: { PATH: 'C:\\Windows' },
        vlcDir: 'C:\\r\\vlc', ffDir: 'C:\\r\\ff', resDir: 'C:\\r', hasVlc: true, hasFfmpeg: true });
    assert.ok(win.PATH.startsWith('C:\\r\\vlc;'));
    assert.ok(win.PATH.endsWith(';C:\\Windows'));
    assert.ok(win.PATH.includes(';C:\\r;'));
    assert.equal(win.LD_LIBRARY_PATH, undefined);
    assert.deepEqual(engineLibEnv({ plat: 'linux', env: {}, hasVlc: false, hasFfmpeg: false }), {});
});

test('discordIpcPaths: named pipes on Windows, sockets elsewhere', () => {
    const w = discordIpcPaths('win32', {});
    assert.equal(w.length, 10);
    assert.equal(w[0], '\\\\.\\pipe\\discord-ipc-0');
    const l = discordIpcPaths('linux', { XDG_RUNTIME_DIR: '/run/user/1000' });
    assert.ok(l.includes('/run/user/1000/discord-ipc-0'));
    assert.ok(l.includes('/run/user/1000/snap.discord/discord-ipc-3'));
});

test('parseNetstatListeningPid picks the LISTENING row for the port', () => {
    const out = [
        '  Proto  Local Address          Foreign Address        State           PID',
        '  TCP    0.0.0.0:135            0.0.0.0:0              LISTENING       1032',
        '  TCP    127.0.0.1:20025        0.0.0.0:0              LISTENING       4242',
        '  TCP    127.0.0.1:20025        127.0.0.1:50000        ESTABLISHED     4242',
        '  TCP    127.0.0.1:120025       0.0.0.0:0              LISTENING       7',
    ].join('\r\n');
    assert.equal(parseNetstatListeningPid(out, 20025), 4242);
    assert.equal(parseNetstatListeningPid(out, 9999), 0);
    assert.equal(parseNetstatListeningPid('', 20025), 0);
});

test('parseSsListeningPid', () => {
    assert.equal(parseSsListeningPid('LISTEN 0 4096 127.0.0.1:20025 0.0.0.0:* users:(("engine",pid=777,fd=7))'), 777);
    assert.equal(parseSsListeningPid(''), 0);
});

test('isPortFree reflects a real listener', async () => {
    const net = await import('node:net');
    const srv = net.createServer();
    await new Promise(r => srv.listen(0, '127.0.0.1', r));
    const { port } = srv.address();
    assert.equal(await isPortFree(port), false);
    await new Promise(r => srv.close(r));
    assert.equal(await isPortFree(port), true);
});

test('makeIconPng produces a decodable RGBA PNG with the glyph drawn', () => {
    for (const shape of ['play', 'pause', 'prev', 'next']) {
        const png = makeIconPng(shape);
        assert.deepEqual([...png.subarray(0, 8)], [0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]);
        assert.equal(png.readUInt32BE(16), 32);                    // IHDR width
        assert.equal(png[25], 6);                                  // colour type RGBA
        const idatLen = png.readUInt32BE(33);
        const raw = zlib.inflateSync(png.subarray(41, 41 + idatLen));
        assert.equal(raw.length, (32 * 4 + 1) * 32);
        let lit = 0;
        for (let i = 0; i < raw.length; i++) if (i % 129 !== 0 && raw[i] === 255) lit++;
        assert.ok(lit > 100, `${shape} should have visible pixels`);
    }
    assert.notDeepEqual(makeIconPng('play'), makeIconPng('pause'));
});

test('parseCmdArg accepts only known transport commands', () => {
    assert.equal(parseCmdArg(['app.exe', '--adam-cmd=playpause']), 'playpause');
    assert.equal(parseCmdArg(['--hidden', '--adam-cmd=next']), 'next');
    assert.equal(parseCmdArg(['--adam-cmd=rm -rf']), null);
    assert.equal(parseCmdArg(['--adam-cmd=evil']), null);
    assert.equal(parseCmdArg(undefined), null);
});

test('mediaSessionScript is valid JS and embeds state as data, not code', () => {
    const evil = { metadata: { title: '"); alert(1); ("', artist: 'a', album: 'b', artUrl: '' }, status: 'Playing', positionMs: 1, durationMs: 2 };
    const src = mediaSessionScript(evil);
    assert.doesNotThrow(() => new Function(src));
    assert.ok(src.includes(JSON.stringify(evil)));
});

test('yamlStr escapes Windows paths so the engine config stays valid YAML', () => {
    assert.equal(yamlStr('C:\\Users\\a\\AppData'), '"C:\\\\Users\\\\a\\\\AppData"');
    assert.equal(yamlStr('/home/a/.config/x'), '"/home/a/.config/x"');
    assert.equal(yamlStr('a"b'), '"a\\"b"');
});

import { mkdtempSync, mkdirSync, writeFileSync, readFileSync as rf, existsSync } from 'node:fs';
import { tmpdir } from 'node:os';
import path from 'node:path';

function tmp() { return mkdtempSync(path.join(tmpdir(), 'adam-mig-')); }
function put(p, c) { mkdirSync(path.dirname(p), { recursive: true }); writeFileSync(p, c); }

test('migrateLegacyConfig moves the old config folder and renames aml- files', () => {
    const root = tmp();
    put(path.join(root, 'apple-music-linux', 'electron-prefs.json'), '{"a":1}');
    put(path.join(root, 'apple-music-linux', 'aml-store.enc'), 'enc');
    put(path.join(root, 'apple-music-linux', 'drm', 'files', 'MUSIC_TOKEN'), 'tok');
    put(path.join(root, 'aml', 'eq.json'), 'eq');
    const dir = migrateLegacyConfig(root, 'adam');
    assert.equal(dir, path.join(root, 'adam'));
    assert.equal(rf(path.join(dir, 'electron-prefs.json'), 'utf8'), '{"a":1}');
    assert.equal(rf(path.join(dir, 'adam-store.enc'), 'utf8'), 'enc');
    assert.equal(rf(path.join(dir, 'drm', 'files', 'MUSIC_TOKEN'), 'utf8'), 'tok');
    assert.equal(rf(path.join(dir, 'eq.json'), 'utf8'), 'eq');
    assert.ok(!existsSync(path.join(root, 'apple-music-linux')));
    assert.ok(!existsSync(path.join(root, 'aml')));
});

test('migrateLegacyConfig never overwrites existing new-name data and is idempotent', () => {
    const root = tmp();
    put(path.join(root, 'adam', 'electron-prefs.json'), 'new');
    put(path.join(root, 'adam', 'adam-store.enc'), 'newstore');
    put(path.join(root, 'apple-music-linux', 'electron-prefs.json'), 'old');
    put(path.join(root, 'apple-music-linux', 'extra.txt'), 'x');
    put(path.join(root, 'adam', 'aml-store.enc'), 'stale');
    migrateLegacyConfig(root, 'adam');
    migrateLegacyConfig(root, 'adam');
    assert.equal(rf(path.join(root, 'adam', 'electron-prefs.json'), 'utf8'), 'new');
    assert.equal(rf(path.join(root, 'adam', 'adam-store.enc'), 'utf8'), 'newstore');
    assert.equal(rf(path.join(root, 'adam', 'extra.txt'), 'utf8'), 'x');
    assert.equal(rf(path.join(root, 'apple-music-linux', 'electron-prefs.json'), 'utf8'), 'old');
});

test('migrateLegacyConfig on a fresh machine just returns the new path', () => {
    const root = tmp();
    assert.equal(migrateLegacyConfig(root, 'adam'), path.join(root, 'adam'));
});

test('migrateStoreKeys renames aml_ keys without clobbering', () => {
    const st = { aml_resume_state: { t: 1 }, aml_play_counts: { a: 1 }, adam_play_counts: { b: 2 }, other: 3 };
    assert.equal(migrateStoreKeys(st), true);
    assert.deepEqual(st, { adam_resume_state: { t: 1 }, adam_play_counts: { b: 2 }, other: 3 });
    assert.equal(migrateStoreKeys(st), false);
});

import { resolveThemeMode } from '../theme-mode.mjs';

test('signed-out users always get the plain blur theme, whatever was saved', () => {
    for (const themeMode of [undefined, 'accent', 'blur', 'custom']) {
        for (const isHyprland of [true, false]) {
            assert.equal(resolveThemeMode({ signedIn: false, themeMode, isHyprland }), 'blur', `${themeMode}/${isHyprland}`);
        }
    }
});

test('signed-in users get their saved theme, with the existing defaults and fallbacks', () => {
    assert.equal(resolveThemeMode({ signedIn: true, themeMode: 'accent', isHyprland: true }), 'accent');
    assert.equal(resolveThemeMode({ signedIn: true, themeMode: 'custom', isHyprland: false }), 'custom');
    assert.equal(resolveThemeMode({ signedIn: true, themeMode: undefined, isHyprland: false }), 'accent');
    assert.equal(resolveThemeMode({ signedIn: true, themeMode: undefined, isHyprland: true }), 'blur');
    assert.equal(resolveThemeMode({ signedIn: true, themeMode: 'blur', isHyprland: true }), 'blur');
    assert.equal(resolveThemeMode({ signedIn: true, themeMode: 'blur', isHyprland: false }), 'blur', 'blur works on every desktop');
});

import { hasSessionBus, mprisRetryDelay } from '../platform.mjs';

test('hasSessionBus needs an actual bus, not just environment variables', () => {
    const none = () => false, all = () => true;
    assert.equal(hasSessionBus({}, all), false, 'no variables at all');
    assert.equal(hasSessionBus({ XDG_RUNTIME_DIR: '/run/user/1000' }, none), false, 'runtime dir without a bus socket');
    assert.equal(hasSessionBus({ XDG_RUNTIME_DIR: '/run/user/1000' }, p => p === '/run/user/1000/bus'), true);
    assert.equal(hasSessionBus({ DBUS_SESSION_BUS_ADDRESS: 'unix:path=/run/user/1000/bus' }, none), false, 'address points at a missing socket');
    assert.equal(hasSessionBus({ DBUS_SESSION_BUS_ADDRESS: 'unix:path=/run/user/1000/bus' }, all), true);
    assert.equal(hasSessionBus({ DBUS_SESSION_BUS_ADDRESS: 'unix:abstract=/tmp/dbus-x,guid=1' }, none), true, 'abstract sockets cannot be checked');
    assert.equal(hasSessionBus({ DBUS_SESSION_BUS_ADDRESS: 'unix:path=/a/b;unix:path=/c/d' }, p => p === '/c/d'), true, 'any listed transport will do');
    assert.equal(hasSessionBus({ DBUS_SESSION_BUS_ADDRESS: 'unix:path=/run/user/1000/bus,guid=abc' }, p => p === '/run/user/1000/bus'), true, 'trailing parameters');
});

test('mprisRetryDelay doubles from 2 s up to a 60 s ceiling', () => {
    assert.deepEqual([1, 2, 3, 4, 5, 6, 7, 20].map(mprisRetryDelay), [2000, 4000, 8000, 16000, 32000, 60000, 60000, 60000]);
    assert.equal(mprisRetryDelay(0), 2000);
});
