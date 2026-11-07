import test from 'node:test';
import assert from 'node:assert/strict';

// engine-sse.js starts a connection at import time and publishes window._adamEngine.
globalThis.window = { _adamEngineURL: 'https://127.0.0.1:1' };
globalThis.fetch = () => new Promise(() => {}); // never answers: no network side effects
await import('../src/engine-sse.js');
const sse = globalThis.window._adamEngine;

test('waitFor removes its listener when it times out', async () => {
    await assert.rejects(sse.waitFor('never.fires', 20), /timed out/);
    assert.equal(sse._listeners.get('never.fires')?.size ?? 0, 0, 'timed-out waitFor left a listener behind');
});

test('waitFor removes its listener once resolved', async () => {
    const p = sse.waitFor('hello', 1000);
    sse._dispatch('hello', { ok: 1 });
    assert.deepEqual(await p, { ok: 1 });
    assert.equal(sse._listeners.get('hello')?.size ?? 0, 0);
});

test('a stream that connects and drops straight away keeps backing off', () => {
    sse._retryDelay = 8000;
    sse._noteStreamEnded(300);          // lived 300 ms: not a healthy connection
    assert.equal(sse._retryDelay, 8000, 'delay must not reset after a connection that died at once');
    sse._noteStreamEnded(60_000);       // lived a minute: healthy, start over
    assert.equal(sse._retryDelay, 1000);
});

test.after(() => { clearTimeout(sse._retryTimer); sse._ctrl?.abort(); });

// ── SSE wire-format automaton ────────────────────────────────────────────────
const enc = new TextEncoder();
function streamOf(chunks) {
    let i = 0;
    return new ReadableStream({ pull(c) { i < chunks.length ? c.enqueue(typeof chunks[i] === 'string' ? enc.encode(chunks[i++]) : chunks[i++]) : c.close(); } });
}
async function parse(chunks) {
    const events = [];
    const orig = sse._dispatch;
    sse._dispatch = (type, data) => events.push([type, data]);
    sse._lastEventId = '';
    try { await sse._parseStream(streamOf(chunks)); } finally { sse._dispatch = orig; }
    return events;
}

const WIRE = 'id: 7\nevent: drm\ndata: {"a":1}\n\n:comment\ndata: line1\ndata: line2\n\nevent: x\r\ndata: crlf\r\n\r\ndata\n\nretry: 4000\ndata: tail\n\n';

test('events: types, multi-line data, CRLF, JSON parsing, comments ignored', async () => {
    const ev = await parse([WIRE]);
    assert.deepEqual(ev, [['drm', { a: 1 }], ['message', 'line1\nline2'], ['x', 'crlf'], ['message', ''], ['message', 'tail']]);
    // ['message', ''] is the bare "data" line: the spec appends "" + LF to the buffer, so the event does fire.
    assert.equal(sse._lastEventId, '7');
    assert.equal(sse._retryDelay, 4000);
});

test('chunk-boundary invariance: the same bytes split anywhere give the same events', async () => {
    const bytes = enc.encode(WIRE);
    const whole = await parse([bytes]);
    let seed = 12345; const rnd = (n) => (seed = (seed * 1103515245 + 12345) & 0x7fffffff) % n;
    for (let run = 0; run < 300; run++) {
        const cuts = [...new Set(Array.from({ length: 1 + rnd(8) }, () => rnd(bytes.length)))].sort((a, b) => a - b);
        const parts = []; let prev = 0;
        for (const c of [...cuts, bytes.length]) { parts.push(bytes.slice(prev, c)); prev = c; }
        assert.deepEqual(await parse(parts), whole, `split at ${cuts}`);
    }
    // multi-byte UTF-8 split in the middle of a character must survive too
    const utf = enc.encode('data: héllo ✓\n\n');
    for (let c = 1; c < utf.length; c++) {
        assert.deepEqual(await parse([utf.slice(0, c), utf.slice(c)]), [['message', 'héllo ✓']], `utf8 split at ${c}`);
    }
});

test('an event is only dispatched at its blank line: a truncated tail is dropped, not half-delivered', async () => {
    assert.deepEqual(await parse(['data: complete\n\ndata: cut off']), [['message', 'complete']]);
    assert.deepEqual(await parse(['event: only-a-type\n\n']), [], 'no data lines means no event');
});

test('parser state resets between events', async () => {
    const ev = await parse(['event: a\nid: 1\ndata: 1\n\ndata: 2\n\n']);
    assert.deepEqual(ev, [['a', 1], ['message', 2]], 'the event type must not leak into the next event');
});
