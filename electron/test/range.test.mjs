import test from 'node:test';
import assert from 'node:assert/strict';
import { parseByteRange as r } from '../range.mjs';

test('no, malformed, multi-range and empty-spec headers mean a full response', () => {
    for (const h of [undefined, '', 'bytes=abc', 'items=0-5', 'bytes=0-5,10-20', 'bytes=-']) {
        assert.deepEqual(r(h, 100), { kind: 'full' }, String(h));
    }
});

test('closed, open-ended and suffix ranges', () => {
    assert.deepEqual(r('bytes=0-9', 100), { kind: 'range', start: 0, end: 9 });
    assert.deepEqual(r('bytes=90-', 100), { kind: 'range', start: 90, end: 99 });
    assert.deepEqual(r('bytes=-10', 100), { kind: 'range', start: 90, end: 99 });
    assert.deepEqual(r(' Bytes = 5 - 6 ', 100), { kind: 'range', start: 5, end: 6 });
});

test('ends are clamped to the file; oversized suffix returns everything', () => {
    assert.deepEqual(r('bytes=50-9999', 100), { kind: 'range', start: 50, end: 99 });
    assert.deepEqual(r('bytes=-9999', 100), { kind: 'range', start: 0, end: 99 });
});

test('unsatisfiable ranges', () => {
    for (const h of ['bytes=100-', 'bytes=200-300', 'bytes=-0', 'bytes=9-3', 'bytes=99999999999999999999999-']) {
        assert.deepEqual(r(h, 100), { kind: 'unsatisfiable' }, h);
    }
    assert.deepEqual(r('bytes=0-5', 0), { kind: 'unsatisfiable' });
});
