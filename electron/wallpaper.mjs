// Finding the desktop wallpaper, which the blur theme draws (blurred) behind the interface.
// Hyprland and KDE have their own lookups in main.mjs; this covers GNOME.
import path from 'path';
import { existsSync } from 'fs';
import { execFile } from 'child_process';

// `gsettings get org.gnome.desktop.background picture-uri` output -> a file path ('' when not a file URI).
export function parseGsettingsUri(out) {
    const s = String(out).trim().replace(/^'|'$/g, '');
    if (!s.startsWith('file://')) return '';
    try { return decodeURIComponent(new URL(s).pathname); } catch { return ''; }
}

function run(cmd, args) {
    return new Promise(resolve => {
        execFile(cmd, args, { encoding: 'utf8', timeout: 3000, windowsHide: true }, (err, stdout) => resolve(err ? '' : stdout));
    });
}

export async function gnomeWallpaperPath(exists = existsSync, runner = run) {
    for (const key of ['picture-uri-dark', 'picture-uri']) {
        const p = parseGsettingsUri(await runner('gsettings', ['get', 'org.gnome.desktop.background', key]));
        if (p && exists(p)) return p;
    }
    return '';
}

// MIME subtype for a data: URL (Windows' cached wallpaper has no extension and is a JPEG).
export function imageMime(file) {
    const ext = path.extname(file).slice(1).toLowerCase();
    if (!ext) return 'jpeg';
    return ext === 'jpg' ? 'jpeg' : ext;
}
