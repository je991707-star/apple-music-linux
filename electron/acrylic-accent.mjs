// Acrylic through SetWindowCompositionAttribute, for Windows 10 and Windows 11 before 22H2, where Electron's
// backgroundMaterial does nothing. The call is undocumented but stable since Windows 10 1803.
import { createRequire } from 'module';

const require = createRequire(import.meta.url);
const WCA_ACCENT_POLICY = 19;
let setWca = null;
let policySize = 0;

function init() {
    if (setWca) return;
    const koffi = require('koffi'); // loaded here so other platforms never touch it
    const user32 = koffi.load('user32.dll');
    const policy = koffi.struct('ACCENT_POLICY', { AccentState: 'int', AccentFlags: 'int', GradientColor: 'uint32', AnimationId: 'int' });
    koffi.struct('WINDOWCOMPOSITIONATTRIBDATA', { Attrib: 'int', pvData: koffi.pointer(policy), cbData: 'size_t' });
    setWca = user32.func('int __stdcall SetWindowCompositionAttribute(size_t hwnd, _Inout_ WINDOWCOMPOSITIONATTRIBDATA *data)');
    policySize = koffi.sizeof(policy);
}

// state: 0 off, 3 blur behind, 4 acrylic. The gradient colour is the system's own tint (ABGR); a minimal
// alpha keeps acrylic blurring (alpha 0 turns it off on some builds) and the page overlays supply the tint.
export function setAccent(win, state) {
    init();
    const hwnd = Number(win.getNativeWindowHandle().readBigUInt64LE(0));
    const policy = { AccentState: state, AccentFlags: 0, GradientColor: state === 4 ? 0x01000000 : 0, AnimationId: 0 };
    return setWca(hwnd, { Attrib: WCA_ACCENT_POLICY, pvData: policy, cbData: policySize }) !== 0;
}
