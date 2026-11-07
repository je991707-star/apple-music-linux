import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
    GLASS, clampTo, cleanBlur, cleanGlassOpacity, cleanNavAlpha,
    sidebarOpacityControl, sidebarOpacityValue, artNavAlpha, navColorWithAlphaVar,
} from '../../src/engine/sidebar-glass.js';

test('zero is a real value, not "unset" (a blur of 0 used to become 20)', () => {
    assert.equal(cleanBlur(0), 0);
    assert.equal(cleanBlur('0'), 0);
    assert.equal(cleanGlassOpacity(0), 0);
    assert.equal(cleanNavAlpha(0), 0);
});

test('values are clamped to their range and junk falls back to the default', () => {
    assert.equal(cleanBlur(500), 80);
    assert.equal(cleanBlur(-5), 0);
    assert.equal(cleanBlur(12.6), 13);
    assert.equal(cleanGlassOpacity(9), 0.5);
    assert.equal(cleanNavAlpha(7), 1);
    for (const junk of [undefined, null, NaN, Infinity, '', '  ', 'abc', {}, [], true]) {
        assert.equal(cleanBlur(junk), GLASS.blur.def, String(junk));
        assert.equal(cleanGlassOpacity(junk), GLASS.glassOpacity.def, String(junk));
        assert.equal(cleanNavAlpha(junk), GLASS.navAlpha.def, String(junk));
    }
    assert.equal(clampTo({ def: 1, min: 0, max: 2 }, '1.5'), 1.5);
});

test('Sidebar opacity edits the accent colour in accent and the blur veil everywhere else', () => {
    assert.equal(sidebarOpacityControl('accent').pref, 'themeNavBgAlpha');
    for (const mode of ['blur', 'custom', undefined, 'nonsense']) assert.equal(sidebarOpacityControl(mode).pref, 'glassOpacity', String(mode));
    assert.deepEqual([sidebarOpacityControl('accent').def, sidebarOpacityControl('accent').max], [0.72, 1]);
    assert.deepEqual([sidebarOpacityControl('blur').def, sidebarOpacityControl('blur').max], [0.07, 0.5]);
});

test('the slider shows the value stored for the current theme', () => {
    const prefs = { themeNavBgAlpha: 0.4, glassOpacity: 0.12 };
    assert.equal(sidebarOpacityValue('accent', prefs), 0.4);
    assert.equal(sidebarOpacityValue('blur', prefs), 0.12);
    assert.equal(sidebarOpacityValue('blur', {}), 0.07, 'blur default is the 7% veil, not the accent 72%');
    assert.equal(sidebarOpacityValue('accent', {}), 0.72);
    assert.equal(sidebarOpacityValue('blur', { glassOpacity: 0 }), 0, 'a saved 0 stays 0');
});

test('album-art sidebar alpha follows the same slider', () => {
    assert.equal(artNavAlpha('accent', { themeNavBgAlpha: 0.3 }), 0.3);
    assert.equal(artNavAlpha('blur', {}), 0.6, 'default veil gives the art theme its usual 0.6');
    assert.equal(artNavAlpha('blur', { glassOpacity: 0 }), 0);
    assert.equal(artNavAlpha('blur', { glassOpacity: 0.5 }), 1);
    assert.ok(artNavAlpha('blur', { glassOpacity: 0.14 }) > artNavAlpha('blur', { glassOpacity: 0.07 }));
});

test('navColorWithAlphaVar swaps only the alpha', () => {
    assert.equal(navColorWithAlphaVar('hsla(210, 20%, 13%, 0.6)'), 'hsla(210, 20%, 13%, var(--adam-nav-alpha, 0.6))');
    assert.equal(navColorWithAlphaVar('rgba(10,20,30,0.25)'), 'rgba(10,20,30, var(--adam-nav-alpha, 0.25))');
    assert.equal(navColorWithAlphaVar('#202020'), '#202020');
    assert.equal(navColorWithAlphaVar('hsl(1, 2%, 3%)'), 'hsl(1, 2%, 3%)');
});
