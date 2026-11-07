// The sidebar's glass controls (Settings -> Display: Glass blur and Sidebar opacity). Shared by the page and
// main.mjs so both sides agree on ranges, defaults and which stored value a slider edits.
//
// The sidebar background is, per theme:
//   accent  the palette's sidebar colour at themeNavBgAlpha (default 72%)
//   blur / custom CSS  a white veil over the blur at glassOpacity (default 7%)
// so "Sidebar opacity" edits a different value, on a different scale, in each. It used to write one pref while
// driving the other's CSS variable, and a blur of 0 became 20 (0 || 20).

export const GLASS = Object.freeze({
    blur: Object.freeze({ def: 20, min: 0, max: 80 }),
    glassOpacity: Object.freeze({ def: 0.07, min: 0, max: 0.5 }),
    navAlpha: Object.freeze({ def: 0.72, min: 0, max: 1 }),
    // Album-art theming colours the sidebar with this alpha when the veil is at its default.
    artNavAlpha: 0.6,
});

// A finite number inside [min, max], else the default. Strings are accepted (prefs are JSON), 0 is a real value.
export function clampTo({ def, min, max }, v) {
    const n = typeof v === 'number' ? v : typeof v === 'string' && v.trim() !== '' ? Number(v) : NaN;
    return Number.isFinite(n) ? Math.min(max, Math.max(min, n)) : def;
}
const hundredths = (n) => Math.round(n * 100) / 100;
export const cleanBlur = (v) => Math.round(clampTo(GLASS.blur, v));
export const cleanGlassOpacity = (v) => hundredths(clampTo(GLASS.glassOpacity, v));
export const cleanNavAlpha = (v) => hundredths(clampTo(GLASS.navAlpha, v));

/** What the Sidebar opacity slider edits in a theme: its pref, range and default. */
export function sidebarOpacityControl(mode) {
    return mode === 'accent'
        ? { pref: 'themeNavBgAlpha', ...GLASS.navAlpha, clean: cleanNavAlpha, hint: 'How opaque the sidebar background is' }
        : { pref: 'glassOpacity', ...GLASS.glassOpacity, clean: cleanGlassOpacity, hint: 'How strong the light veil over the sidebar blur is' };
}

/** The sidebar opacity currently stored for a theme. */
export function sidebarOpacityValue(mode, prefs) {
    const c = sidebarOpacityControl(mode);
    return c.clean(prefs?.[c.pref]);
}

/** Alpha for album-art theming's sidebar colour (the --adam-nav-alpha variable), following the same slider. */
export function artNavAlpha(mode, prefs) {
    if (mode === 'accent') return cleanNavAlpha(prefs?.themeNavBgAlpha);
    const veil = cleanGlassOpacity(prefs?.glassOpacity);
    return Math.min(1, hundredths(GLASS.artNavAlpha * veil / GLASS.glassOpacity.def));
}

/** The same colour with its alpha replaced by var(--adam-nav-alpha, fallback); other colour forms are left alone. */
export function navColorWithAlphaVar(color) {
    const m = /^(hsla|rgba)\((.*),\s*([\d.]+)\s*\)\s*$/i.exec(String(color).trim());
    return m ? `${m[1]}(${m[2]}, var(--adam-nav-alpha, ${m[3]}))` : color;
}
