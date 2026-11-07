from pathlib import Path
import shutil,os,json,secrets,sys,hashlib
R=Path(__file__).resolve().parents[2];L=R/'.u2-lab/u2-4-20261102-run1';root=L/sys.argv[1];port=int(sys.argv[2]);root.mkdir(mode=0o700)
install=root/'install';state=root/'state';install.mkdir(mode=0o700);state.mkdir(mode=0o700)
base=Path('/var/home/h97m/Projects/adam/electron/dist/linux-unpacked')
for p in base.iterdir():
 if p.name=='resources':continue
 if p.is_dir():shutil.copytree(p,install/p.name,symlinks=True)
 elif p.is_file():shutil.copy2(p,install/p.name)
app=install/'resources/app';app.mkdir(parents=True)
for p in (R/'electron').iterdir():
 if p.is_file()and p.suffix in ['.mjs','.cjs','.js','.css','.html','.png','.ico','.svg','.json']:shutil.copy2(p,app/p.name)
for n in ['src','assets']:
 if (R/'electron'/n).is_dir():shutil.copytree(R/'electron'/n,app/n,symlinks=False)
modules=Path('/var/home/h97m/Projects/adam/electron/node_modules');copied=set()
def dependency(n):
 if n in copied:return
 p=modules/n
 if not p.exists():return
 p=p.resolve();to=app/'node_modules'/n;to.parent.mkdir(parents=True,exist_ok=True);shutil.copytree(p,to,symlinks=False);copied.add(n)
 for child in json.loads((p/'package.json').read_text()).get('dependencies',{}):dependency(child)
dependency('mpris-service')
shutil.copy2(L/'engine-normal-v3-no-drm',install/'resources/engine')
rt=Path('/var/home/h97m/Projects/adam-portable-vlc/.portable-vlc-lab/phase4c-20261029-run1/build-repeat/runtime-bundle');shutil.copytree(rt,install/'resources/vlc',symlinks=True)
ff=Path('/var/home/h97m/Projects/adam-portable-vlc/third_party/ffmpeg');shutil.copytree(ff,install/'resources/ffmpeg',symlinks=True)
for n in ['engine-bundle.js','engine-sse-bundle.js','smart-cache-bundle.js']:shutil.copy2(L/n,app/n)
shutil.copy2(R/'electron/src/vision-glass.js',app/'vision-bundle.js')
shutil.copy2(R/'verification/u2-main/normal-observer.mjs',app/'u2-observer.mjs')
pkg=json.loads((app/'package.json').read_text());pkg['main']='u2-observer.mjs';(app/'package.json').write_text(json.dumps(pkg,indent=2))
(install/'private-bus.conf').write_text('''<!DOCTYPE busconfig PUBLIC "-//freedesktop//DTD D-Bus Bus Configuration 1.0//EN" "http://www.freedesktop.org/standards/dbus/1.0/busconfig.dtd"><busconfig><type>session</type><listen>unix:path=/private/run/bus</listen><auth>EXTERNAL</auth><policy context="default"><allow own="*"/><allow send_destination="*"/><allow receive_sender="*"/></policy></busconfig>''')
# Anonymous Chromium profile only; basic is not a credential-storage qualification.
(install/'start-inside.sh').write_text('''#!/bin/sh
set -eu
exec /app/adam --disable-setuid-sandbox --disable-gpu --password-store=basic --ozone-platform=wayland
''');(install/'start-inside.sh').chmod(0o700)
for n in ['home','home/.config','home/.local/share','cache','run','tmp','logs','crash','control']:(state/n).mkdir(parents=True,exist_ok=True,mode=0o700)
(state/'machine-id').write_text(secrets.token_hex(16)+'\n')
(root/'launch.json').write_text(json.dumps({'root':str(root),'port':port}));(root/'launch.json').chmod(0o600)
manifest={}
for p in install.rglob('*'):
 if p.is_symlink():manifest[str(p.relative_to(install))]={'symlink':os.readlink(p)}
 elif p.is_file():manifest[str(p.relative_to(install))]={'sha256':hashlib.sha256(p.read_bytes()).hexdigest()}
(root/'manifest.json').write_text(json.dumps(manifest,indent=2))
assert (app/'main.mjs').read_bytes()==(R/'electron/main.mjs').read_bytes()
assert (app/'preload.cjs').read_bytes()==(R/'electron/preload.cjs').read_bytes()
print(root)
