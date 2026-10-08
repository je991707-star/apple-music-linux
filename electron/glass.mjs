// Liquid glass on Windows: a native panel (native/liquid-glass, DXGI Desktop Duplication + D3D11 +
// DirectComposition) draws the refracted desktop in a window pinned directly under Adam's transparent,
// frameless Electron window, which draws the interface on top. This module loads the addon and keeps the
// panel in step with the window.
import path from 'path';
import { existsSync } from 'fs';

// Visual parameters. Lengths in DIPs are scaled to physical pixels by the display's scale factor.
export const GLASS_PARAMS = Object.freeze({
    cornerRadiusDip: 14,
    blurSigma: 8,
    displacementScale: 60,
    aberrationIntensity: 1.5,
    saturation: 1.35,
});

// Folders that may hold the built addon, best first: next to the packaged app, then the repo checkout.
export function glassModuleDirs({ resourcesPath, devDir }) {
    return [path.join(resourcesPath, 'liquid-glass'), devDir];
}

// The addon, or null when it is missing, cannot load, or this system does not support it.
export function loadGlassModule({ dirs, load, exists = existsSync, log = () => {} }) {
    for (const dir of dirs) {
        const entry = path.join(dir, 'index.js');
        if (!exists(entry)) continue;
        try {
            const mod = load(entry);
            if (mod && mod.isSupported()) return mod;
            log(`liquid glass: ${dir} loaded but is not supported on this system`);
        } catch (e) {
            log(`liquid glass: could not load ${dir}: ${e.message}`);
        }
    }
    return null;
}

const toInt = Math.round;

// Options for createPanel from a rectangle already in screen physical pixels.
export function panelOptions(rect, scale, params = GLASS_PARAMS) {
    return {
        x: toInt(rect.x), y: toInt(rect.y), width: Math.max(1, toInt(rect.width)), height: Math.max(1, toInt(rect.height)),
        dpr: scale,
        cornerRadius: toInt(params.cornerRadiusDip * scale),
        blurSigma: params.blurSigma,
        displacementScale: params.displacementScale,
        aberrationIntensity: params.aberrationIntensity,
        saturation: params.saturation,
    };
}

const FOLLOW_EVENTS = ['move', 'moved', 'resize', 'resized', 'maximize', 'unmaximize', 'enter-full-screen', 'leave-full-screen'];
const VISIBILITY_EVENTS = ['minimize', 'restore', 'show', 'hide'];

// Keeps one panel under `win`. `screen` is Electron's screen module (or a stand-in with the same calls).
export function createGlassController({ glass, win, screen, params = GLASS_PARAMS, contentProtection = true, log = () => {} }) {
    let panel = null;
    let shown = false;
    const listeners = [];

    const physicalRect = () => {
        const b = win.getBounds();
        return screen.dipToScreenRect ? screen.dipToScreenRect(win, b) : b;
    };
    const scale = () => (screen.getDisplayMatching ? screen.getDisplayMatching(win.getBounds()).scaleFactor : 1) || 1;
    const usable = () => !win.isDestroyed() && win.isVisible() && !win.isMinimized();

    function show() { if (!shown && panel) { panel.show(150); shown = true; } }
    function hide() { if (shown && panel) { panel.hide(0); shown = false; } }

    function sync() {
        if (!panel || win.isDestroyed()) return;
        if (!usable()) { hide(); return; }
        const s = scale();
        const o = panelOptions(physicalRect(), s, params);
        panel.setBounds({ x: o.x, y: o.y, width: o.width, height: o.height });
        panel.setParams({ cornerRadius: o.cornerRadius, blurSigma: o.blurSigma, displacementScale: o.displacementScale,
            aberrationIntensity: o.aberrationIntensity, saturation: o.saturation });
        show();
    }

    function on(emitter, event, fn) { emitter.on(event, fn); listeners.push([emitter, event, fn]); }

    return {
        get panel() { return panel; },

        start() {
            if (panel) return true;
            const s = scale();
            panel = glass.createPanel({ ...panelOptions(physicalRect(), s, params), anchorWindow: win });
            if (!panel) { log('liquid glass: createPanel returned null'); return false; }
            // Keep Adam's own window out of the DXGI capture so the glass doesn't refract the UI on top of itself.
            // Disabled in the demo so the window stays visible in screen capture tools.
            if (contentProtection) win.setContentProtection(true);
            for (const e of FOLLOW_EVENTS) on(win, e, sync);
            for (const e of VISIBILITY_EVENTS) on(win, e, sync);
            if (screen.on) on(screen, 'display-metrics-changed', sync);
            on(win, 'closed', () => this.stop());
            if (usable()) show();
            return true;
        },

        sync,

        stop() {
            for (const [emitter, event, fn] of listeners.splice(0)) emitter.removeListener(event, fn);
            if (panel) {
                try { panel.destroy(); } catch (e) { log(`liquid glass: destroy failed: ${e.message}`); }
                panel = null;
                shown = false;
            }
            if (!win.isDestroyed()) win.setContentProtection(false);
        },
    };
}
