// Every scheme Settings → Links can turn on must be declared by the Linux desktop entries, or the
// system cannot offer Adam as a handler for it.
//   node --test electron/test/link-packaging.test.mjs
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { APPLE_SCHEMES, OWN_SCHEME } from '../deeplink.mjs';

const read = (p) => readFileSync(new URL(p, import.meta.url), 'utf8');
const linuxSchemes = [OWN_SCHEME, ...APPLE_SCHEMES.filter(s => !s.windowsOnly).map(s => s.scheme)];

test('the desktop entries and AppImage config declare every Linux scheme', () => {
    const pkg = JSON.parse(read('../package.json'));
    const targets = {
        'flatpak desktop': read('../../flatpak/io.github.silentone12725.Adam.desktop'),
        'installer desktop': read('../../scripts/installer-header.sh'),
        'AppImage mimeTypes': pkg.build.linux.mimeTypes.join(';') + ';',
    };
    for (const [name, text] of Object.entries(targets)) {
        for (const s of linuxSchemes) assert.ok(text.includes(`x-scheme-handler/${s}`), `${name} is missing ${s}`);
    }
});

test('the Windows installer registers only adam://', () => {
    const pkg = JSON.parse(read('../package.json'));
    const schemes = pkg.build.protocols.flatMap(p => p.schemes);
    assert.deepEqual(schemes, ['adam']);
});
