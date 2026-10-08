// Liquid glass controller (glass.mjs) with stand-ins for the native addon and Electron.
//   node --test electron/test/glass.test.mjs
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { EventEmitter } from 'node:events';
import path from 'node:path';
import { GLASS_PARAMS, glassModuleDirs, loadGlassModule, panelOptions, createGlassController } from '../glass.mjs';

function fakeWin(bounds = { x: 100, y: 50, width: 800, height: 600 }) {
    const w = new EventEmitter();
    Object.assign(w, { bounds, visible: true, minimized: false, destroyed: false, protection: null });
    w.getBounds = () => w.bounds;
    w.isVisible = () => w.visible;
    w.isMinimized = () => w.minimized;
    w.isDestroyed = () => w.destroyed;
    w.setContentProtection = (v) => { w.protection = v; };
    return w;
}
function fakeScreen(scaleFactor = 1.5) {
    const s = new EventEmitter();
    s.dipToScreenRect = (_w, r) => ({ x: r.x * scaleFactor, y: r.y * scaleFactor, width: r.width * scaleFactor, height: r.height * scaleFactor });
    s.getDisplayMatching = () => ({ scaleFactor });
    return s;
}
function fakeGlass() {
    const calls = [];
    const panel = {
        show: (ms) => calls.push(['show', ms]), hide: (ms) => calls.push(['hide', ms]),
        setBounds: (b) => calls.push(['setBounds', b]), setParams: (p) => calls.push(['setParams', p]),
        destroy: () => calls.push(['destroy']),
    };
    return { calls, createPanel: (o) => { calls.push(['createPanel', o]); return panel; }, isSupported: () => true };
}

test('panelOptions converts to whole physical pixels and scales the corner radius', () => {
    const o = panelOptions({ x: 150.4, y: 75.6, width: 1200.2, height: 900.7 }, 1.5);
    assert.deepEqual([o.x, o.y, o.width, o.height], [150, 76, 1200, 901]);
    assert.equal(o.dpr, 1.5);
    assert.equal(o.cornerRadius, 21);
    assert.equal(o.blurSigma, GLASS_PARAMS.blurSigma);
    assert.equal(panelOptions({ x: 0, y: 0, width: 0, height: 0 }, 1).width, 1);
});

test('glassModuleDirs prefers the packaged location', () => {
    const d = glassModuleDirs({ resourcesPath: '/r', devDir: '/repo/native/liquid-glass' });
    assert.deepEqual(d, [path.join('/r', 'liquid-glass'), '/repo/native/liquid-glass']);
});

test('loadGlassModule returns the first supported module and skips broken or unsupported ones', () => {
    const logs = [];
    const mods = { '/a/index.js': { isSupported: () => false }, '/b/index.js': 'throw', '/c/index.js': { isSupported: () => true, tag: 'c' } };
    const load = (p) => { if (mods[p] === 'throw') throw new Error('boom'); return mods[p]; };
    const exists = (p) => p in mods;
    const m = loadGlassModule({ dirs: ['/a', '/b', '/c', '/d'].map(p => path.resolve(p)), load: (p) => load(p.replace(/^[A-Za-z]:/, '').replace(/\\/g, '/')), exists: (p) => exists(p.replace(/^[A-Za-z]:/, '').replace(/\\/g, '/')), log: (l) => logs.push(l) });
    assert.equal(m.tag, 'c');
    assert.equal(logs.length, 2);
    assert.equal(loadGlassModule({ dirs: ['/nowhere'], load, exists: () => false }), null);
});

test('start creates a panel under the window in physical pixels and protects the window from capture', () => {
    const glass = fakeGlass(), win = fakeWin(), screen = fakeScreen(1.5);
    const c = createGlassController({ glass, win, screen });
    assert.equal(c.start(), true);
    const [, opts] = glass.calls.find(x => x[0] === 'createPanel');
    assert.deepEqual([opts.x, opts.y, opts.width, opts.height], [150, 75, 1200, 900]);
    assert.equal(opts.anchorWindow, win);
    assert.equal(win.protection, true);
    assert.deepEqual(glass.calls.find(x => x[0] === 'show'), ['show', 150]);
});

test('the panel follows the window and hides while it is minimised or hidden', () => {
    const glass = fakeGlass(), win = fakeWin(), screen = fakeScreen(1);
    const c = createGlassController({ glass, win, screen });
    c.start();
    glass.calls.length = 0;
    win.bounds = { x: 300, y: 200, width: 640, height: 480 };
    win.emit('move');
    assert.deepEqual(glass.calls.find(x => x[0] === 'setBounds'), ['setBounds', { x: 300, y: 200, width: 640, height: 480 }]);
    glass.calls.length = 0;
    win.minimized = true; win.emit('minimize');
    assert.deepEqual(glass.calls, [['hide', 0]]);
    glass.calls.length = 0;
    win.minimized = false; win.emit('restore');
    assert.ok(glass.calls.some(x => x[0] === 'show'));
    glass.calls.length = 0;
    win.visible = false; win.emit('hide');
    assert.deepEqual(glass.calls, [['hide', 0]]);
});

test('a display scale change re-applies the corner radius', () => {
    const glass = fakeGlass(), win = fakeWin(), screen = fakeScreen(1);
    const c = createGlassController({ glass, win, screen });
    c.start();
    glass.calls.length = 0;
    screen.getDisplayMatching = () => ({ scaleFactor: 2 });
    screen.dipToScreenRect = (_w, r) => ({ x: r.x * 2, y: r.y * 2, width: r.width * 2, height: r.height * 2 });
    screen.emit('display-metrics-changed');
    const [, p] = glass.calls.find(x => x[0] === 'setParams');
    assert.equal(p.cornerRadius, 28);
});

test('stop destroys the panel, removes every listener and lifts capture protection', () => {
    const glass = fakeGlass(), win = fakeWin(), screen = fakeScreen(1);
    const c = createGlassController({ glass, win, screen });
    c.start();
    c.stop();
    assert.ok(glass.calls.some(x => x[0] === 'destroy'));
    assert.equal(win.protection, false);
    assert.equal(win.listenerCount('move'), 0);
    assert.equal(screen.listenerCount('display-metrics-changed'), 0);
    glass.calls.length = 0;
    win.emit('move');
    assert.deepEqual(glass.calls, [], 'no calls after stop');
});

test('closing the window stops the controller; an unsupported addon makes start() fail cleanly', () => {
    const glass = fakeGlass(), win = fakeWin(), screen = fakeScreen(1);
    const c = createGlassController({ glass, win, screen });
    c.start();
    win.destroyed = true; win.emit('closed');
    assert.ok(glass.calls.some(x => x[0] === 'destroy'));
    const none = createGlassController({ glass: { createPanel: () => null }, win: fakeWin(), screen: fakeScreen(1) });
    assert.equal(none.start(), false);
});
