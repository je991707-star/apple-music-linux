// Standalone visual check for the liquid-glass addon: a frameless transparent window with
// the native panel behind it. Run with `electron glass-demo.mjs` (Windows).
import { app, BrowserWindow, screen } from 'electron';
import path from 'path';
import { fileURLToPath } from 'url';
import { createRequire } from 'module';
import { glassModuleDirs, loadGlassModule, createGlassController } from './glass.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));
const require = createRequire(import.meta.url);

app.whenReady().then(() => {
    const glass = loadGlassModule({
        dirs: glassModuleDirs({ resourcesPath: process.resourcesPath, devDir: path.join(here, '..', 'native', 'liquid-glass') }),
        load: (e) => require(e), log: (m) => console.log('[glass]', m),
    });
    console.log('[glass] module loaded:', !!glass);
    const win = new BrowserWindow({ x: 40, y: 20, width: 760, height: 230, alwaysOnTop: true, frame: false, transparent: true, hasShadow: false,
        backgroundColor: '#00000000', resizable: true, webPreferences: { contextIsolation: true } });
    win.loadURL('data:text/html,' + encodeURIComponent(`<body style="margin:0;font:600 28px Segoe UI;color:#fff;
        background:rgba(0,0,0,.08);height:100vh;display:flex;align-items:center;justify-content:center;border-radius:14px;
        box-shadow:inset 0 0 0 1px rgba(255,255,255,.2)">Adam liquid glass</body>`));
    const ctl = glass && createGlassController({ glass, win, screen, contentProtection: false, log: (m) => console.log('[glass]', m) });
    console.log('[glass] started:', ctl ? ctl.start() : false);
    win.on('closed', () => { try { ctl?.stop(); glass?.shutdown?.(); } catch {} app.quit(); });
});
