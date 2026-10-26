// Windows acrylic window settings (acrylic.mjs): validation and the page overlay script.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { ACRYLIC_DEFAULTS, ACRYLIC_MATERIALS, normalizeAcrylic, acrylicOverlayScript, windowsVersion, acrylicBackend } from '../acrylic.mjs';

test('defaults: dark theme (Adam is dark-styled) with a black tint, which is what makes acrylic dark', () => {
    assert.deepEqual(ACRYLIC_DEFAULTS, { material: 'acrylic', theme: 'dark', tintColor: '#000000', tintOpacity: 0.7, lumOpacity: 0 });
    assert.deepEqual(normalizeAcrylic(undefined), ACRYLIC_DEFAULTS);
    assert.deepEqual(normalizeAcrylic(null), ACRYLIC_DEFAULTS);
    assert.deepEqual(normalizeAcrylic('acrylic'), ACRYLIC_DEFAULTS);
});

test('every listed material and theme is accepted, anything else falls back', () => {
    for (const material of ACRYLIC_MATERIALS) assert.equal(normalizeAcrylic({ material }).material, material);
    for (const theme of ['system', 'light', 'dark']) assert.equal(normalizeAcrylic({ theme }).theme, theme);
    assert.equal(normalizeAcrylic({ material: 'aero' }).material, 'acrylic');
    assert.equal(normalizeAcrylic({ theme: 'sepia' }).theme, 'dark');
});

test('a Windows that cannot draw a material falls back to acrylic', () => {
    assert.equal(normalizeAcrylic({ material: 'mica' }, ['acrylic', 'none']).material, 'acrylic');
    assert.equal(normalizeAcrylic({ material: 'none' }, ['acrylic', 'none']).material, 'none');
});

test('windowsVersion accepts Windows 10 and Windows 11 only', () => {
    assert.equal(windowsVersion('10.0.26200', 'win32'), 'win11');
    assert.equal(windowsVersion('10.0.22000', 'win32'), 'win11');
    assert.equal(windowsVersion('10.0.21996', 'win32'), 'win10');
    assert.equal(windowsVersion('10.0.19045', 'win32'), 'win10');
    for (const old of ['6.3.9600', '6.1.7601', '5.1.2600', '', undefined]) assert.equal(windowsVersion(old, 'win32'), null);
    assert.equal(windowsVersion('10.0.26200', 'linux'), null);
    assert.equal(windowsVersion('10.0.26200', 'darwin'), null);
});

test('acrylicBackend: DWM material from 22H2, the acrylic accent on Windows 10 and early Windows 11', () => {
    const win11 = acrylicBackend('10.0.26200', 'win32');
    assert.equal(win11.kind, 'material');
    assert.deepEqual(win11.materials, ACRYLIC_MATERIALS);
    assert.equal(acrylicBackend('10.0.22621', 'win32').kind, 'material');
    const early11 = acrylicBackend('10.0.22000', 'win32');
    assert.deepEqual([early11.kind, early11.accentState, early11.materials], ['accent', 4, ['acrylic', 'none']]);
    assert.equal(acrylicBackend('10.0.19045', 'win32').accentState, 4);
    assert.equal(acrylicBackend('10.0.17134', 'win32').accentState, 4);
    assert.equal(acrylicBackend('10.0.17133', 'win32').accentState, 3, 'before 1803 only plain blur exists');
    assert.equal(acrylicBackend('6.1.7601', 'win32'), null);
    assert.equal(acrylicBackend('10.0.26200', 'linux'), null);
});

test('opacities are clamped to 0..1 and non-numbers fall back', () => {
    assert.equal(normalizeAcrylic({ tintOpacity: 5 }).tintOpacity, 1);
    assert.equal(normalizeAcrylic({ lumOpacity: -2 }).lumOpacity, 0);
    assert.equal(normalizeAcrylic({ tintOpacity: 0.37 }).tintOpacity, 0.37);
    for (const bad of ['0.5', NaN, Infinity, null, {}]) assert.equal(normalizeAcrylic({ tintOpacity: bad }).tintOpacity, ACRYLIC_DEFAULTS.tintOpacity);
});

test('tint colour must be #rrggbb; it is lower-cased', () => {
    assert.equal(normalizeAcrylic({ tintColor: '#AABBCC' }).tintColor, '#aabbcc');
    for (const bad of ['red', '#fff', '#12345g', 'rgb(1,2,3)', '#aabbcc;}</style>', 123])
        assert.equal(normalizeAcrylic({ tintColor: bad }).tintColor, '#000000');
});

function fakeDom() {
    const byId = new Map(), bodyOrder = [], appended = [], props = {}, attrs = {};
    const document = {
        body: { prepend(el) { bodyOrder.unshift(el); byId.set(el.id, el); }, appendChild(el) { appended.push(el); byId.set(el.id, el); } },
        documentElement: {
            style: { setProperty: (k, v) => { props[k] = v; } },
            setAttribute: (k, v) => { attrs[k] = v; },
            appendChild(el) { byId.set(el.id, el); },
        },
        getElementById: (id) => byId.get(id) || null,
        createElement: (tag) => ({ tag, id: '', textContent: '' }),
    };
    return { document, props, attrs, bodyOrder, byId, appended };
}
const run = (dom, settings, opts) => new Function('document', `return ${acrylicOverlayScript(settings, opts)}`)(dom.document);

test('overlay script creates the style and both layers, luminosity below the tint', () => {
    const dom = fakeDom();
    run(dom, { tintColor: '#102030', tintOpacity: 0.5, lumOpacity: 0.25, material: 'mica' });
    assert.deepEqual(dom.bodyOrder.map(e => e.id), ['_adamAcrylicLum', '_adamAcrylicTint']);
    assert.ok(dom.byId.has('_adamAcrylicDrag'), 'the page provides the drag area the native title bar used to');
    assert.match(dom.byId.get('_adamAcrylicStyle').textContent, /#_adamAcrylicLum/);
    assert.equal(dom.props['--adam-acrylic-tint'], 'rgba(16,32,48,0.5)');
    assert.equal(dom.props['--adam-acrylic-lum-opacity'], '0.25');
    assert.equal(dom.attrs['data-adam-acrylic-material'], 'mica');
});

test('a window with its own drag area (the mini player) gets the layers but no drag strip', () => {
    const dom = fakeDom();
    run(dom, {}, { drag: false });
    assert.deepEqual(dom.bodyOrder.map(e => e.id), ['_adamAcrylicLum', '_adamAcrylicTint']);
    assert.equal(dom.byId.has('_adamAcrylicDrag'), false);
    assert.equal(dom.attrs['data-adam-acrylic-material'], 'acrylic', 'the page keys its acrylic styling off this');
});

test('running the overlay script again updates in place instead of adding layers', () => {
    const dom = fakeDom();
    run(dom, {});
    run(dom, { tintOpacity: 1, tintColor: '#ffffff' });
    assert.equal(dom.bodyOrder.length, 2);
    assert.equal([...dom.appended].filter(e => e.id === '_adamAcrylicDrag').length, 1);
    assert.equal(dom.props['--adam-acrylic-tint'], 'rgba(255,255,255,1)');
});

test('the overlay script cannot be steered by hostile settings', () => {
    const dom = fakeDom();
    run(dom, { tintColor: "#000000');alert(1);//", material: "x'});alert(1);//", tintOpacity: '1);alert(1)' });
    assert.equal(dom.props['--adam-acrylic-tint'], 'rgba(0,0,0,0.7)');
    assert.equal(dom.attrs['data-adam-acrylic-material'], 'acrylic');
});

test('without a body the script does nothing', () => {
    const dom = fakeDom();
    dom.document.body = null;
    run(dom, {});
    assert.equal(dom.byId.size, 0);
});
