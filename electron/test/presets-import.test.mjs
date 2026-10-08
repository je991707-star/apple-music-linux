import test from 'node:test';
import assert from 'node:assert/strict';
import { sanitizePalette, sanitizePreset } from '../theme-presets.mjs';

test('a well-formed preset passes through, trimmed to known keys', () => {
    const p = sanitizePreset({ name: ' Ocean ', palette: { accent: '#0af', navBg: 'hsla(200, 50%, 20%, 0.7)', appearance: 'light', evil: 'x' } });
    assert.deepEqual(p, { name: 'Ocean', palette: { accent: '#0af', navBg: 'hsla(200, 50%, 20%, 0.7)', appearance: 'light' } });
});

test('non-string and hostile values are dropped, not thrown on later', () => {
    const pal = sanitizePalette({ accent: 123, navBg: { x: 1 }, bgColor: 'red; background:url(http://evil)', navBorder: 'url(http://evil)', accentActive: '#123456' });
    assert.deepEqual(pal, { accentActive: '#123456' });
});

test('unusable input is rejected', () => {
    for (const bad of [null, [], 'x', {}, { name: '', palette: { accent: '#fff' } }, { name: 'n' }, { name: 'n', palette: {} },
        { name: 'n', palette: { accent: 5 } }, { name: 'x'.repeat(65), palette: { accent: '#fff' } },
        { name: 'Apple Music', palette: { accent: '#fff' } }, { name: '__proto__', palette: [] }]) {
        assert.equal(sanitizePreset(bad), null, JSON.stringify(bad));
    }
});

test('overlong colour values are rejected', () => {
    assert.equal(sanitizePalette({ accent: '#' + 'a'.repeat(200) }), null);
});
