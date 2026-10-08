import test from 'node:test';
import assert from 'node:assert/strict';

const calls = [];
let putResolvers = [];
globalThis.window = { _adamEngineURL: 'https://engine.test', addEventListener() {} };
globalThis.document = { cookie: '' };
globalThis.localStorage = { getItem: () => null, setItem() {} };
globalThis.history = { pushState() {}, replaceState() {} };
globalThis.location = { href: 'https://music.apple.com/us/album/x/123' };
globalThis.fetch = (url, opts = {}) => {
    calls.push({ url: String(url), method: opts.method || 'GET', body: opts.body });
    if (opts.method === 'PUT') return new Promise(res => putResolvers.push(job => res({ ok: true, json: async () => ({ jobId: job }) })));
    if (opts.method === 'DELETE') return Promise.resolve({ ok: true });
    return Promise.resolve({ ok: true, json: async () => ({ data: [{ relationships: { tracks: { data: [{ id: '1' }] } } }] }) });
};
await import('../src/smart-cache.js');
const sc = globalThis.window._adamSmartCache;
const reset = () => { calls.length = 0; putResolvers = []; sc._jobs = {}; sc._seq = {}; };
const tick = () => new Promise(r => setImmediate(r));

test('a stale in-flight context is cancelled instead of overwriting the newer job', async () => {
    reset();
    const a = sc.sendContext({ type: 'queue', reason: 'a', slot: 'queue', tracks: [{ id: '1' }], currentIndex: 0 });
    await tick();
    const b = sc.sendContext({ type: 'queue', reason: 'b', slot: 'queue', tracks: [{ id: '2' }], currentIndex: 0 });
    await tick();
    putResolvers[1]('jobB'); await b;       // newer call answers first
    putResolvers[0]('jobA'); await a;       // older call answers late
    assert.equal(sc._jobs.queue, 'jobB', 'the newer job must stay registered');
    assert.ok(calls.some(c => c.method === 'DELETE' && c.url.endsWith('/jobs/jobA')), 'the stale job must be cancelled');
});

test('a huge queue is windowed around the current track and keeps absolute indices', async () => {
    reset();
    const tracks = Array.from({ length: 1000 }, (_, i) => ({ id: String(i + 1) }));
    const p = sc.sendContext({ type: 'queue', reason: 'big', slot: 'queue', tracks, currentIndex: 500 });
    await tick(); putResolvers[0]('j'); await p;
    const body = JSON.parse(calls.find(c => c.method === 'PUT').body);
    assert.equal(body.tracks.length, 200);
    assert.ok(body.tracks.some(t => t.assetId === '501'), 'current track must be in the window');
    const cur = body.tracks.find(t => t.assetId === '501');
    assert.equal(cur.signals.queueDistance, 0, 'distance is measured on the full queue');
    assert.ok(body.tracks.every(t => t.signals.queueDistance <= 210));
});

test('a small queue is sent whole', async () => {
    reset();
    const p = sc.sendContext({ type: 'queue', reason: 's', slot: 'queue', tracks: [{ id: '1' }, { id: '2' }], currentIndex: 1 });
    await tick(); putResolvers[0]('j'); await p;
    assert.equal(JSON.parse(calls.find(c => c.method === 'PUT').body).tracks.length, 2);
});
