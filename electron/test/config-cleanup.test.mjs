import test from 'node:test';
import assert from 'node:assert/strict';
import { mkdtempSync, mkdirSync, writeFileSync, existsSync, utimesSync, symlinkSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { cleanupCache, planCleanup, runCleanup, runCleanupThrottled, PROTECTED, STALE_DAYS } from '../config-cleanup.mjs';

const DAY = 86400e3, NOW = Date.UTC(2026, 9, 9);
const mk = () => mkdtempSync(path.join(tmpdir(), 'adam-cc-'));
function file(root, rel, { size = 10, ageDays = 0 } = {}) {
    const p = path.join(root, rel);
    mkdirSync(path.dirname(p), { recursive: true });
    writeFileSync(p, Buffer.alloc(size));
    const t = new Date(NOW - ageDays * DAY);
    // Directory mtimes count as activity too, so age every ancestor below the root as well.
    for (let d = p; d !== root && d.startsWith(root); d = path.dirname(d)) {
        // never make a directory look older than a newer sibling file already inside it
        if (d === p || ageDays >= 0) utimesSync(d, t, t);
    }
    return p;
}

test('live state is never removed, however old', () => {
    const r = mk();
    for (const rel of ['adam-store.enc', 'electron-prefs.json', 'eq.json', 'mv-dec.key', 'adamplay/device_private_key',
        'adamplay/device_client_id_blob', 'drm/files/MUSIC_TOKEN', 'drm/files/mpl_db/x', 'engine-data/config.yaml',
        'electron-session/Partitions/apple-music/Cookies']) file(r, rel, { ageDays: 400 });
    runCleanup(r, { now: NOW });
    for (const rel of ['adam-store.enc', 'electron-prefs.json', 'eq.json', 'mv-dec.key', 'adamplay/device_private_key',
        'adamplay/device_client_id_blob', 'drm/files/MUSIC_TOKEN', 'drm/files/mpl_db/x', 'engine-data/config.yaml',
        'electron-session/Partitions/apple-music/Cookies']) assert.ok(existsSync(path.join(r, rel)), rel);
    rmSync(r, { recursive: true });
});

test('obsolete wrapper-era directories are removed', () => {
    const r = mk();
    file(r, 'wrapper/rootfs/data/x'); file(r, 'wrapper-data/rootfs/y'); file(r, 'webview-data/z');
    file(r, 'drm/rootfs/system/a'); file(r, 'drm/hybris-linker/b'); file(r, 'drm/files/MUSIC_TOKEN');
    const res = runCleanup(r, { now: NOW });
    for (const rel of ['wrapper', 'wrapper-data', 'webview-data', 'drm/rootfs', 'drm/hybris-linker']) assert.ok(!existsSync(path.join(r, rel)), rel);
    assert.ok(existsSync(path.join(r, 'drm/files/MUSIC_TOKEN')));
    assert.equal(res.errors, 0);
    rmSync(r, { recursive: true });
});

test('stale root profile data goes only after it has been untouched long enough', () => {
    const r = mk();
    file(r, 'Partitions/apple-music/Cache/f', { ageDays: STALE_DAYS + 1 });
    file(r, 'GPUCache/g', { ageDays: STALE_DAYS - 1 });
    file(r, 'Local State', { ageDays: STALE_DAYS + 30 });
    runCleanup(r, { now: NOW });
    assert.ok(!existsSync(path.join(r, 'Partitions')));
    assert.ok(!existsSync(path.join(r, 'Local State')));
    assert.ok(existsSync(path.join(r, 'GPUCache')), 'recently used data must stay');
    rmSync(r, { recursive: true });
});

test('a directory with any recent file inside is not stale', () => {
    const r = mk();
    file(r, 'Partitions/old', { ageDays: 100 });
    file(r, 'Partitions/new', { ageDays: 1 });
    runCleanup(r, { now: NOW });
    assert.ok(existsSync(path.join(r, 'Partitions/old')));
    rmSync(r, { recursive: true });
});

test('oversized caches are cleared, in-budget ones are kept', () => {
    const r = mk();
    file(r, 'electron-session/Partitions/apple-music/Cache/big', { size: 401 * 1024 * 1024 });
    file(r, 'electron-session/Partitions/apple-music/Code Cache/small', { size: 1024 });
    runCleanup(r, { now: NOW });
    assert.ok(!existsSync(path.join(r, 'electron-session/Partitions/apple-music/Cache')));
    assert.ok(existsSync(path.join(r, 'electron-session/Partitions/apple-music/Code Cache/small')));
    assert.ok(existsSync(path.join(r, 'electron-session')), 'the profile itself is protected');
    rmSync(r, { recursive: true });
});

test('interrupted-write temp files and old crash dumps are removed; fresh ones are kept', () => {
    const r = mk();
    const base = 'electron-session/Partitions/apple-music/';
    file(r, base + '.org.chromium.Chromium.old', { ageDays: 3 });
    file(r, base + '.org.chromium.Chromium.fresh', { ageDays: 0 });
    file(r, base + 'Cookies');
    file(r, 'electron-session/Crashpad/completed/old.dmp', { ageDays: 30 });
    file(r, 'electron-session/Crashpad/completed/new.dmp', { ageDays: 1 });
    runCleanup(r, { now: NOW });
    assert.ok(!existsSync(path.join(r, base + '.org.chromium.Chromium.old')));
    assert.ok(existsSync(path.join(r, base + '.org.chromium.Chromium.fresh')));
    assert.ok(existsSync(path.join(r, base + 'Cookies')));
    assert.ok(!existsSync(path.join(r, 'electron-session/Crashpad/completed/old.dmp')));
    assert.ok(existsSync(path.join(r, 'electron-session/Crashpad/completed/new.dmp')));
    rmSync(r, { recursive: true });
});

test('symlinks are removed, never followed', () => {
    const r = mk(), outside = mk();
    file(outside, 'keep.txt');
    symlinkSync(outside, path.join(r, 'wrapper'));
    runCleanup(r, { now: NOW });
    assert.ok(!existsSync(path.join(r, 'wrapper')));
    assert.ok(existsSync(path.join(outside, 'keep.txt')), 'target outside the config dir must survive');
    rmSync(r, { recursive: true }); rmSync(outside, { recursive: true });
});

test('dry run reports but deletes nothing', () => {
    const r = mk();
    file(r, 'wrapper/x', { size: 2048 });
    const res = runCleanup(r, { now: NOW, dryRun: true });
    assert.equal(res.removed, 1);
    assert.ok(existsSync(path.join(r, 'wrapper/x')));
    rmSync(r, { recursive: true });
});

test('throttled runs at most once per interval', () => {
    const r = mk();
    file(r, 'wrapper/x');
    assert.ok(runCleanupThrottled(r, { now: NOW }));
    file(r, 'wrapper/again');
    assert.equal(runCleanupThrottled(r, { now: NOW + 1000 }), null);
    assert.ok(existsSync(path.join(r, 'wrapper/again')));
    assert.ok(runCleanupThrottled(r, { now: NOW + 2 * DAY }));
    assert.ok(!existsSync(path.join(r, 'wrapper')));
    rmSync(r, { recursive: true });
});

test('no rule can reach a protected path', () => {
    const r = mk();
    for (const p of PROTECTED) file(r, p === 'electron-session' ? 'electron-session/keep' : p, { ageDays: 999 });
    const plan = planCleanup(r, NOW);
    for (const item of plan) for (const p of PROTECTED) {
        assert.notEqual(item.path, path.join(r, p), `${p} is planned for deletion`);
    }
    rmSync(r, { recursive: true });
});

test('cache cleanup removes only the listed obsolete entries', () => {
    const r = mk();
    file(r, 'library.json'); file(r, 'WebKitCache/a/b'); file(r, 'apk-graph.db', { size: 100 });
    file(r, 'library.enc'); file(r, 'library.key'); file(r, 'playback/1-alac.m4a'); file(r, 'autoeq/index.json');
    const res = cleanupCache(r);
    assert.equal(res.removed, 3);
    for (const keep of ['library.enc', 'library.key', 'playback/1-alac.m4a', 'autoeq/index.json']) assert.ok(existsSync(path.join(r, keep)), keep);
    for (const gone of ['library.json', 'WebKitCache', 'apk-graph.db']) assert.ok(!existsSync(path.join(r, gone)), gone);
    rmSync(r, { recursive: true });
});
