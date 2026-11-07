# Builds no code, copies only current public application sources and Electron.
from pathlib import Path
import sys,os,json,hashlib,shutil
R=Path(__file__).resolve().parents[2]
L=Path(sys.argv[1]).resolve()
assert L.parent==R/'.u2-lab' and L.name.startswith(('u2-10a2-','u2-10a2b-','u2-10a2c-','u2-10a2d-','u2-10a2e-','u2-10a2f-'))
I=L/'web-install-qualified';I.mkdir(mode=0o700)
base=Path('/var/home/h97m/Projects/adam/electron/dist/linux-unpacked')
for p in base.iterdir():
 if p.name=='resources':continue
 if p.is_dir():shutil.copytree(p,I/p.name,symlinks=True)
 elif p.is_file():shutil.copy2(p,I/p.name)
a=I/'resources/app';a.mkdir(parents=True)
for p in (R/'electron').iterdir():
 if p.is_file()and p.suffix in ['.mjs','.cjs','.js','.json','.css','.html','.png','.ico','.svg']:shutil.copy2(p,a/p.name)
for n in ['src','assets']:
 if (R/'electron'/n).is_dir():shutil.copytree(R/'electron'/n,a/n)
# Actual normal Main/preload, separate opt-in startup branch. No observer/broker.
assert json.loads((a/'package.json').read_text())['main']=='main.mjs'
(I/'web-login-private.json').write_text(json.dumps({'version':1,'webOnly':True}))
(I/'private-bus.conf').write_text('''<!DOCTYPE busconfig PUBLIC "-//freedesktop//DTD D-Bus Bus Configuration 1.0//EN" "http://www.freedesktop.org/standards/dbus/1.0/busconfig.dtd"><busconfig><type>session</type><listen>unix:path=/private/run/bus</listen><auth>EXTERNAL</auth><policy context="default"><allow own="*"/><allow send_destination="*"/><allow receive_sender="*"/></policy></busconfig>''')
(I/'start-private.sh').write_text('''#!/bin/sh
set -eu
umask 077
ulimit -c 0
mkdir -p /private/home /private/config /private/data /private/cache /private/run /private/tmp /private/session /private/crash
exec dbus-run-session --config-file=/app/private-bus.conf -- /app/adam --disable-setuid-sandbox --disable-gpu --ozone-platform=wayland
''');(I/'start-private.sh').chmod(0o700)
manifest={}
for p in I.rglob('*'):
 if p.is_symlink():assert p.resolve().is_relative_to(I);manifest[str(p.relative_to(I))]={'symlink':os.readlink(p)}
 elif p.is_file():manifest[str(p.relative_to(I))]={'sha256':hashlib.sha256(p.read_bytes()).hexdigest()}
assert not any(p.is_file() and p.name in ['engine','libdrm_client.so','libandroidappmusic.so','u2-observer.mjs'] for p in I.rglob('*'))
(L/'WEB-INSTALL-QUALIFIED-MANIFEST.json').write_text(json.dumps(manifest,indent=2))
print('Private source tree staged; no Engine/DRM/runtime/profile/account copied')
