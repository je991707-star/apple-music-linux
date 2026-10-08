import test from 'node:test';
import assert from 'node:assert/strict';
import { createTransportController, createNavigationQueue, withNavigationDeadline } from '../../src/engine/transport.js';

const tick = () => new Promise(r => setImmediate(r));

function harness() {
    const sent = [];
    let scope = 'A';
    let gate = null;
    const ctl = createTransportController({
        scope: () => scope,
        send: async paused => { if (gate) await gate; sent.push(paused); },
    });
    return { ctl, sent, setScope: s => { scope = s; }, hold: () => { let o; gate = new Promise(r => { o = r; }); return () => { gate = null; o(); }; } };
}

test('commands reach the engine in the order issued', async () => {
    const h = harness();
    h.ctl.request(true); h.ctl.request(false); h.ctl.request(true);
    await h.ctl.request(true);
    assert.deepEqual(h.sent, [true, false, true]);
});

test('a repeat of the desired state is deduplicated', async () => {
    const h = harness();
    await h.ctl.request(true);
    await h.ctl.request(true);
    assert.deepEqual(h.sent, [true]);
});

test('queued commands for an old session never reach its replacement', async () => {
    const h = harness();
    const release = h.hold();
    const first = h.ctl.request(true);
    await tick(); // first is now in flight for session A
    const second = h.ctl.request(false);
    h.setScope('B');
    release();
    await Promise.all([first, second]);
    // the in-flight one was already sent for A; the queued one must be dropped
    assert.deepEqual(h.sent, [true]);
});

test('a command issued after the session changed is dropped before sending', async () => {
    const h = harness();
    const p = h.ctl.request(true);
    h.setScope('B');
    await p;
    assert.deepEqual(h.sent, []);
});

test('reset invalidates queued sends and clears desired', async () => {
    const h = harness();
    const release = h.hold();
    h.ctl.request(true);
    const queued = h.ctl.request(false);
    h.ctl.reset(false);
    release();
    await queued;
    assert.equal(h.ctl.desired, false);
    assert.ok(!h.sent.includes(false), 'command queued before reset must not be sent');
});

test('pending is true only while a send is outstanding', async () => {
    const h = harness();
    const release = h.hold();
    const p = h.ctl.request(true);
    await tick();
    assert.equal(h.ctl.pending, true);
    release();
    await p;
    assert.equal(h.ctl.pending, false);
});

test('a failed send does not poison later commands', async () => {
    let fail = true;
    const sent = [];
    const ctl = createTransportController({
        scope: () => 's',
        send: async p => { if (fail) { fail = false; throw new Error('boom'); } sent.push(p); },
    });
    await assert.rejects(ctl.request(true), /boom/);
    ctl.reset();
    await ctl.request(true);
    assert.deepEqual(sent, [true]);
});

test('navigation runs serially, exposes the latest cursor, and survives rejection', async () => {
    const order = [];
    const q = createNavigationQueue(async (ci, ii) => {
        order.push(`start${ii}`);
        await tick();
        if (ii === 1) throw new Error('nav fail');
        order.push(`end${ii}`);
    });
    const a = q.goto(0, 0);
    const b = q.goto(0, 1).catch(() => {});
    const c = q.goto(0, 2);
    assert.deepEqual(q.cursor, { ci: 0, ii: 2 });
    await Promise.all([a, b, c]);
    assert.deepEqual(order, ['start0', 'end0', 'start1', 'start2', 'end2']);
    assert.equal(q.cursor, null);
});

test('withNavigationDeadline rejects slow operations and passes fast ones', async () => {
    assert.equal(await withNavigationDeadline(Promise.resolve(7), 50), 7);
    await assert.rejects(withNavigationDeadline(new Promise(() => {}), 10), /timed out/);
});
