// Windows acrylic window background (the "blur" theme on Windows).
//
// The window gets a DWM system backdrop (Electron's backgroundMaterial), the page is transparent so the
// material shows through, and two plain overlay layers in the page add the tint colour and the luminosity
// layer on top. Theme (system/light/dark) drives the material through nativeTheme.themeSource.
export const ACRYLIC_MATERIALS = Object.freeze(['acrylic', 'mica', 'tabbed', 'auto', 'none']);
// Height of the strip the page reserves for dragging; matches the caption height Windows draws its buttons in.
export const ACRYLIC_TITLEBAR_HEIGHT = 32;
export const ACRYLIC_THEMES = Object.freeze(['system', 'light', 'dark']);
// Dark by default because Adam's interface is dark-styled; System and Light stay available. The tint is black
// at 0.7: measured on Windows 11 the bare dark acrylic is still mid-grey (it picks up the wallpaper), a black
// tint is what darkens it, and a light tint would only make it lighter. The page strip drops from ~61 to ~18
// (of 255) and still blurs.
export const ACRYLIC_DEFAULTS = Object.freeze({
    material: 'acrylic', theme: 'dark', tintColor: '#000000', tintOpacity: 0.7, lumOpacity: 0,
});

// Windows 11 22H2 (build 22621) is the first release where Electron's backgroundMaterial works. Windows 10
// (and Windows 11 before 22H2) draw acrylic through SetWindowCompositionAttribute instead, which can only do
// acrylic and plain blur. 1803 (build 17134) is the first build with the acrylic accent.
const MATERIAL_BUILD = 22621;
const ACRYLIC_ACCENT_BUILD = 17134;
const BUILD_RE = /^10\.0\.(\d+)/;

// Which Windows this is, from os.release() ("10.0.<build>"): 'win11', 'win10', or null for anything else.
export function windowsVersion(release, plat = process.platform) {
    const m = plat === 'win32' ? BUILD_RE.exec(String(release)) : null;
    return m ? (Number(m[1]) >= 22000 ? 'win11' : 'win10') : null;
}

// How the blur is drawn here. 'material': DWM system backdrop. 'accent': SetWindowCompositionAttribute, with
// the accent state to use (4 acrylic, 3 plain blur on builds older than 1803). null: not Windows 10 or 11.
export function acrylicBackend(release, plat = process.platform) {
    if (!windowsVersion(release, plat)) return null;
    const build = Number(BUILD_RE.exec(String(release))[1]);
    if (build >= MATERIAL_BUILD) return { kind: 'material', materials: ACRYLIC_MATERIALS };
    return { kind: 'accent', accentState: build >= ACRYLIC_ACCENT_BUILD ? 4 : 3, materials: ['acrylic', 'none'] };
}

const HEX_COLOR = /^#[0-9a-fA-F]{6}$/;
const unit = (v, fallback) => (typeof v === 'number' && Number.isFinite(v) ? Math.min(1, Math.max(0, v)) : fallback);

// Anything stored or received from the page becomes a valid settings object; bad fields fall back to the default.
// `materials` is what this Windows can draw (see acrylicBackend).
export function normalizeAcrylic(raw, materials = ACRYLIC_MATERIALS) {
    const r = raw && typeof raw === 'object' ? raw : {};
    const d = ACRYLIC_DEFAULTS;
    return {
        material: materials.includes(r.material) ? r.material : d.material,
        theme: ACRYLIC_THEMES.includes(r.theme) ? r.theme : d.theme,
        tintColor: typeof r.tintColor === 'string' && HEX_COLOR.test(r.tintColor) ? r.tintColor.toLowerCase() : d.tintColor,
        tintOpacity: unit(r.tintOpacity, d.tintOpacity),
        lumOpacity: unit(r.lumOpacity, d.lumOpacity),
    };
}

// The two overlay layers sit behind the interface (z-index -1, like the other background layers) and are
// hidden in the accent and custom-CSS themes, which paint their own opaque background. The luminosity layer
// is the theme's base colour; "none" turns it fully opaque so the window is a plain solid colour.
export const ACRYLIC_CSS = `
:root { --adam-acrylic-lum: #202020; --adam-acrylic-lum-opacity: 0; --adam-acrylic-tint: rgba(0,0,0,0); }
@media (prefers-color-scheme: light) { :root { --adam-acrylic-lum: #f3f3f3; } }
#_adamAcrylicLum, #_adamAcrylicTint { position: fixed; inset: 0; pointer-events: none; z-index: -1; }
#_adamAcrylicLum { background: var(--adam-acrylic-lum); opacity: var(--adam-acrylic-lum-opacity); }
/* The window has no native title bar (the material has to reach the top edge), so the page provides the drag
   area, stopping short of the window buttons that Windows draws over its right end. */
#_adamAcrylicDrag { position: fixed; top: 0; left: env(titlebar-area-x, 0); width: env(titlebar-area-width, calc(100% - 140px));
    height: env(titlebar-area-height, ${ACRYLIC_TITLEBAR_HEIGHT}px); -webkit-app-region: drag; z-index: 2147483000; }
#_adamAcrylicTint { background: var(--adam-acrylic-tint); }
html[data-adam-acrylic-material="none"] #_adamAcrylicLum { opacity: 1 !important; }
body[data-adam-mode="accent"] #_adamAcrylicLum, body[data-adam-mode="accent"] #_adamAcrylicTint,
body[data-adam-mode="custom"] #_adamAcrylicLum, body[data-adam-mode="custom"] #_adamAcrylicTint { display: none !important; }
`;

// Page script that creates the layers on first use and updates them afterwards (safe to run any number of times).
export function acrylicOverlayScript(settings) {
    const s = normalizeAcrylic(settings);
    const n = parseInt(s.tintColor.slice(1), 16);
    const tint = `rgba(${(n >> 16) & 255},${(n >> 8) & 255},${n & 255},${s.tintOpacity})`;
    return `(function(){
        if (!document.body) return;
        if (!document.getElementById('_adamAcrylicStyle')) {
            const st = document.createElement('style'); st.id = '_adamAcrylicStyle';
            st.textContent = ${JSON.stringify(ACRYLIC_CSS)};
            document.documentElement.appendChild(st);
        }
        if (!document.getElementById('_adamAcrylicLum')) {
            const lum = document.createElement('div'); lum.id = '_adamAcrylicLum';
            const tint = document.createElement('div'); tint.id = '_adamAcrylicTint';
            document.body.prepend(tint); document.body.prepend(lum);
        }
        if (!document.getElementById('_adamAcrylicDrag')) {
            const drag = document.createElement('div'); drag.id = '_adamAcrylicDrag';
            document.body.appendChild(drag);
        }
        const r = document.documentElement;
        r.style.setProperty('--adam-acrylic-lum-opacity', ${JSON.stringify(String(s.lumOpacity))});
        r.style.setProperty('--adam-acrylic-tint', ${JSON.stringify(tint)});
        r.setAttribute('data-adam-acrylic-material', ${JSON.stringify(s.material)});
    })()`;
}
