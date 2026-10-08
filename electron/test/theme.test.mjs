// Theme presets, migration and wallpaper lookup.
//   node --test electron/test/theme.test.mjs
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { BUILTIN_PRESETS, DEFAULT_PRESET, defaultPalette, migrateThemePrefs, THEME_VERSION } from '../theme-presets.mjs';
import { parseRegWallpaper, parseGsettingsUri, windowsWallpaperPath, gnomeWallpaperPath, imageMime } from '../wallpaper.mjs';

test('the one built-in preset is a fixed neutral palette with red only as the accent', () => {
    assert.deepEqual(Object.keys(BUILTIN_PRESETS), ['Apple Music']);
    assert.equal(DEFAULT_PRESET, 'Apple Music');
    const p = defaultPalette();
    assert.equal(p.accent, '#fc3c44');
    assert.equal(p.bgColor, '#000000');
    assert.equal(p.navBg, '#303030');
    assert.equal(p.appearance, 'dark');
    assert.notEqual(defaultPalette(), p, 'every call returns a fresh object');
});

test('migrateThemePrefs drops the old generated palette once and keeps everything else', () => {
    const prefs = { themeMode: 'accent', themePalette: { accent: '#00ff00', navBg: 'hsla(120,80%,14%,0.72)' }, themeAppearance: 'dark', themePresets: [{ name: 'mine' }], zoomFactor: 1.1 };
    assert.equal(migrateThemePrefs(prefs), true);
    assert.equal(prefs.themePalette, undefined);
    assert.equal(prefs.themeAppearance, undefined);
    assert.equal(prefs.themeVersion, THEME_VERSION);
    assert.equal(prefs.themeMode, 'accent');
    assert.deepEqual(prefs.themePresets, [{ name: 'mine' }]);
    assert.equal(prefs.zoomFactor, 1.1);
    // a palette chosen after the migration is never touched again
    prefs.themePalette = { accent: '#123456' };
    assert.equal(migrateThemePrefs(prefs), false);
    assert.deepEqual(prefs.themePalette, { accent: '#123456' });
});

test('parseRegWallpaper reads the value from reg query output', () => {
    const out = '\r\nHKEY_CURRENT_USER\\Control Panel\\Desktop\r\n    WallPaper    REG_SZ    C:\\Windows\\Web\\Wallpaper\\Windows\\img0.jpg\r\n\r\n';
    assert.equal(parseRegWallpaper(out), 'C:\\Windows\\Web\\Wallpaper\\Windows\\img0.jpg');
    assert.equal(parseRegWallpaper('    WallPaper    REG_SZ    \r\n'), '');
    assert.equal(parseRegWallpaper(''), '');
    assert.equal(parseRegWallpaper('    WallPaper    REG_EXPAND_SZ    %SystemRoot%\\a.jpg'), '%SystemRoot%\\a.jpg');
});

test('parseGsettingsUri accepts file URIs only', () => {
    assert.equal(parseGsettingsUri("'file:///home/a/Pictures/my%20wall.jpg'\n"), '/home/a/Pictures/my wall.jpg');
    assert.equal(parseGsettingsUri("'none'"), '');
    assert.equal(parseGsettingsUri("'https://example.com/x.jpg'"), '');
    assert.equal(parseGsettingsUri(''), '');
});

test('windowsWallpaperPath falls back to the transcoded wallpaper when the registry value is empty', async () => {
    const env = { APPDATA: 'C:\\Users\\a\\AppData\\Roaming' };
    const cached = 'C:\\Users\\a\\AppData\\Roaming/Microsoft/Windows/Themes/TranscodedWallpaper';
    const seen = new Set();
    const exists = (p) => seen.has(p.replace(/\\/g, '/'));
    seen.add('C:/Users/a/AppData/Roaming/Microsoft/Windows/Themes/TranscodedWallpaper');
    const empty = async () => '    WallPaper    REG_SZ    \r\n';
    assert.match(await windowsWallpaperPath(env, exists, empty), /TranscodedWallpaper$/);
    seen.add('C:/pics/real.jpg');
    const set = async () => '    WallPaper    REG_SZ    C:\\pics\\real.jpg\r\n';
    assert.equal(await windowsWallpaperPath(env, exists, set), 'C:\\pics\\real.jpg');
    assert.equal(await windowsWallpaperPath({ APPDATA: 'X' }, () => false, empty), '');
});

test('gnomeWallpaperPath prefers the dark variant and needs an existing file', async () => {
    const calls = [];
    const runner = async (_c, args) => { calls.push(args[2]); return args[2] === 'picture-uri-dark' ? "'file:///w/dark.png'" : "'file:///w/light.png'"; };
    assert.equal(await gnomeWallpaperPath(() => true, runner), '/w/dark.png');
    assert.equal(await gnomeWallpaperPath((p) => p === '/w/light.png', runner), '/w/light.png');
    assert.equal(await gnomeWallpaperPath(() => false, runner), '');
});

test('imageMime handles the extensionless Windows cache and jpg', () => {
    assert.equal(imageMime('C:\\x\\TranscodedWallpaper'), 'jpeg');
    assert.equal(imageMime('/a/b.JPG'), 'jpeg');
    assert.equal(imageMime('/a/b.png'), 'png');
    assert.equal(imageMime('/a/b.bmp'), 'bmp');
});
