import { test } from 'node:test';
import assert from 'node:assert/strict';
import { signOutFlow, SIGN_OUT_TEXT } from '../../src/engine/sign-out.js';

const okFetch = (urls) => async (u) => { urls.push(u); return { ok: true }; };
const failFetch = async () => { throw new Error('down'); };

test('sign-out calls logout, then main without a backstop', async () => {
    const urls = [], got = [];
    const res = await signOutFlow({ engine: 'https://e/', fetchFn: okFetch(urls), clearCredentials: async (o) => { got.push(o); return { ok: true }; } });
    assert.deepEqual(urls, ['https://e/api/v1/drm/logout']);
    assert.deepEqual(got, [{ reload: true, backstop: null }]);
    assert.deepEqual(res, { ok: true, note: '' });
});

test('forget-device uses its route and, when the engine is down, a device backstop', async () => {
    const got = [];
    const res = await signOutFlow({ engine: 'https://e', scope: 'device', fetchFn: failFetch, clearCredentials: async (o) => { got.push(o); return { ok: true }; } });
    assert.deepEqual(got, [{ reload: true, backstop: 'device' }]);
    assert.equal(res.ok, true);
});

test('an unrecorded sign-out is reported, never as success', async () => {
    const res = await signOutFlow({ engine: 'https://e', fetchFn: failFetch, clearCredentials: async () => ({ ok: false, error: 'marker' }) });
    assert.deepEqual(res, { ok: false, message: SIGN_OUT_TEXT.marker });
});

test('credential and durability outcomes have their own text', async () => {
    const urls = [];
    let res = await signOutFlow({ engine: 'https://e', fetchFn: okFetch(urls), clearCredentials: async () => ({ ok: false, error: 'credentials' }) });
    assert.equal(res.message, SIGN_OUT_TEXT.credentials);
    res = await signOutFlow({ engine: 'https://e', fetchFn: failFetch, clearCredentials: async () => ({ ok: true, warning: 'durability' }) });
    assert.deepEqual(res, { ok: true, note: SIGN_OUT_TEXT.durability });
});

test('a 500 from the engine counts as failed', async () => {
    const got = [];
    await signOutFlow({ engine: 'https://e', fetchFn: async () => ({ ok: false }), clearCredentials: async (o) => { got.push(o); return { ok: true }; } });
    assert.equal(got[0].backstop, 'account');
});
