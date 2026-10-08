import test from 'node:test';
import assert from 'node:assert/strict';
import { PLAY_STATE, PLAY_TRANSITIONS, isLegalPlayTransition } from '../../src/engine/playstate.js';

const states = Object.values(PLAY_STATE);

test('every state has an explicit transition entry and only known targets (closed relation)', () => {
    for (const s of states) {
        assert.ok(PLAY_TRANSITIONS[s], `${s} has no entry`);
        for (const t of PLAY_TRANSITIONS[s]) assert.ok(states.includes(t), `${s} → unknown state ${t}`);
    }
    assert.deepEqual(Object.keys(PLAY_TRANSITIONS).sort(), [...states].sort());
});

test('every state is reachable from idle, and idle is reachable from every state (no dead or trapped states)', () => {
    const reach = (from, edges) => { const seen = new Set([from]); const q = [from];
        while (q.length) for (const n of edges(q.shift())) if (!seen.has(n)) { seen.add(n); q.push(n); } return seen; };
    assert.deepEqual([...reach(PLAY_STATE.IDLE, s => PLAY_TRANSITIONS[s])].sort(), [...states].sort());
    for (const s of states) assert.ok(reach(s, x => PLAY_TRANSITIONS[x]).has(PLAY_STATE.IDLE), `${s} cannot get back to idle`);
});

test('any state may fall back to idle (reset/error)', () => {
    for (const s of states) assert.ok(isLegalPlayTransition(s, PLAY_STATE.IDLE), s);
});

test('the design edges are legal and the shortcuts are not', () => {
    const { IDLE, OPENING, CDN_SETTLING, STREAMING, COMPLETE } = PLAY_STATE;
    for (const [a, b] of [[IDLE, OPENING], [OPENING, CDN_SETTLING], [OPENING, STREAMING], [CDN_SETTLING, STREAMING], [STREAMING, COMPLETE]]) {
        assert.ok(isLegalPlayTransition(a, b), `${a} → ${b}`);
    }
    for (const [a, b] of [[IDLE, STREAMING], [IDLE, COMPLETE], [IDLE, CDN_SETTLING], [COMPLETE, STREAMING], [COMPLETE, OPENING], [STREAMING, OPENING], [CDN_SETTLING, OPENING]]) {
        assert.ok(!isLegalPlayTransition(a, b), `${a} → ${b} must be illegal`);
    }
    assert.equal(isLegalPlayTransition('bogus', PLAY_STATE.IDLE), false);
    assert.equal(isLegalPlayTransition('bogus', 'bogus'), false);
});
