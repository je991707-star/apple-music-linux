import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { shouldShowOffline, offlineHash } from '../load-failure.mjs';

test('main-frame network failures show the offline page', () => {
    for (const errorCode of [-106, -105, -102, -118, -109, -7, -130, -200, -501]) {
        assert.equal(shouldShowOffline({ errorCode, isMainFrame: true, validatedURL: 'https://music.apple.com/' }), true, String(errorCode));
    }
});

test('aborted loads, sub-frames and the offline page itself are ignored', () => {
    assert.equal(shouldShowOffline({ errorCode: -3, isMainFrame: true, validatedURL: 'https://music.apple.com/' }), false);
    assert.equal(shouldShowOffline({ errorCode: -106, isMainFrame: false, validatedURL: 'https://cdn.example/x.js' }), false);
    assert.equal(shouldShowOffline({ errorCode: -106, isMainFrame: true, validatedURL: 'file:///app/offline.html' }), false);
});

test('messages are friendly and the hash round-trips', () => {
    const q = new URLSearchParams(offlineHash(-106, 'ERR_INTERNET_DISCONNECTED'));
    assert.equal(q.get('m'), 'You are offline');
    assert.equal(q.get('d'), 'ERR_INTERNET_DISCONNECTED (-106)');
    assert.match(new URLSearchParams(offlineHash(-202)).get('m'), /secure connection/);
    assert.match(new URLSearchParams(offlineHash(-999)).get('m'), /could not be loaded/);
});

test('offline.html is self-contained: locked-down CSP, no external resources, retry wired', () => {
    const html = readFileSync(new URL('../offline.html', import.meta.url), 'utf8');
    assert.match(html, /Content-Security-Policy[^>]*default-src 'none'/);
    assert.match(html, /connect-src https:\/\/music\.apple\.com/);
    assert.doesNotMatch(html, /<(link|img|iframe|script)[^>]+\b(src|href)=["']https?:/i);
    assert.match(html, /id="retry"/);
    assert.match(html, /addEventListener\('online'/);
});
