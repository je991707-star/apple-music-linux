import test from 'node:test';
import assert from 'node:assert/strict';
import { mkdtempSync, readFileSync, readdirSync, statSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { writeFileAtomicSync, writeFileAtomic } from '../atomic-write.mjs';

const tmp = () => mkdtempSync(path.join(tmpdir(), 'adam-aw-'));

test('sync write replaces content, applies mode, leaves no temp files', () => {
    const d = tmp(); const f = path.join(d, 'store');
    writeFileAtomicSync(f, 'one'); writeFileAtomicSync(f, 'two');
    assert.equal(readFileSync(f, 'utf8'), 'two');
    if (process.platform !== 'win32') assert.equal(statSync(f).mode & 0o777, 0o600);
    assert.deepEqual(readdirSync(d), ['store']);
    rmSync(d, { recursive: true });
});

test('async write replaces content and leaves no temp files', async () => {
    const d = tmp(); const f = path.join(d, 'store');
    await Promise.all([writeFileAtomic(f, 'a'), writeFileAtomic(f, 'b'), writeFileAtomic(f, 'c')]);
    assert.match(readFileSync(f, 'utf8'), /^[abc]$/); // never a torn mix
    assert.deepEqual(readdirSync(d), ['store']);
    rmSync(d, { recursive: true });
});

test('failure leaves the existing file intact and cleans up', () => {
    const d = tmp(); const f = path.join(d, 'store');
    writeFileAtomicSync(f, 'keep');
    assert.throws(() => writeFileAtomicSync(path.join(d, 'no-such-dir', 'x'), 'x'));
    assert.equal(readFileSync(f, 'utf8'), 'keep');
    assert.deepEqual(readdirSync(d), ['store']);
    rmSync(d, { recursive: true });
});
