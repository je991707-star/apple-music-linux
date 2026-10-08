// Pure cross-platform helpers (platform.mjs) and Windows integration helpers (win-integration.mjs).
//   node --test electron/test/platform.test.mjs
import { test } from 'node:test';
import assert from 'node:assert/strict';
import zlib from 'node:zlib';
import {
    configRoot, engineFileName, engineLibEnv, discordIpcPaths,
    parseNetstatListeningPid, parseSsListeningPid, isPortFree, yamlStr,
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
    assert.equal(parseCmdArg(['app.exe', '--aml-cmd=playpause']), 'playpause');
    assert.equal(parseCmdArg(['--hidden', '--aml-cmd=next']), 'next');
    assert.equal(parseCmdArg(['--aml-cmd=rm -rf']), null);
    assert.equal(parseCmdArg(['--aml-cmd=evil']), null);
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
