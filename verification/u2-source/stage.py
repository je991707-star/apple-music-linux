from pathlib import Path
import shutil,os,json,secrets,sys,hashlib,re
R=Path(__file__).resolve().parents[2]
L=Path(os.environ.get('U2_SOURCE_LAB',str(R/'.u2-lab/u2-5-20261102-run1')))
assert L.resolve()==L and L.parent==R/'.u2-lab' and L.name.startswith(('u2-5-','u2-5a-','u2-6-','u2-7-','u2-8-','u2-9-'))
assert sys.argv[1].replace('-','').isalnum() and '/' not in sys.argv[1]
mode=sys.argv[3] if len(sys.argv)>3 else 'synthetic'
assert mode in ['synthetic','baseline-ordered','baseline-legacy','volume','auto-volume','stability','lifecycle']
root=L/sys.argv[1];port=int(sys.argv[2]);assert 1024<=port<=65532 and port not in [10020,20020,20025,20026];root.mkdir(mode=0o700)
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
engine=L/('engine-release-no-fixture-final' if mode.startswith('baseline-') else 'engine-source-u28' if L.name.startswith(('u2-8-','u2-9-')) else 'engine-source-strict-pulse-final' if L.name.startswith('u2-7-') else 'engine-source-scoped-public' if L.name.startswith('u2-6-') else 'engine-source-no-drm-v8')
shutil.copy2(engine,install/'resources/engine')
rt=Path('/var/home/h97m/Projects/adam-portable-vlc/.portable-vlc-lab/phase4c-20261029-run1/build-repeat/runtime-bundle');shutil.copytree(rt,install/'resources/vlc',symlinks=True)
ff=Path('/var/home/h97m/Projects/adam-portable-vlc/third_party/ffmpeg');shutil.copytree(ff,install/'resources/ffmpeg',symlinks=True)
bundles=L if mode in ['synthetic','volume','auto-volume','stability','lifecycle'] else L/('production-asar-input-current-final' if L.name.startswith('u2-9-') else 'production-asar-input-scoped-exit-final')
for n in ['engine-bundle.js','engine-sse-bundle.js','smart-cache-bundle.js']:shutil.copy2(bundles/n,app/n)
shutil.copytree(L/'fixtures',install/'fixtures',symlinks=False)
shutil.copy2(R/'electron/src/vision-glass.js',app/'vision-bundle.js')
shutil.copy2(R/('verification/u2-source/normal-observer.mjs' if mode=='synthetic' else 'verification/u2-source/lifecycle-observer.mjs' if mode=='lifecycle' else 'verification/u2-source/stability-observer.mjs' if mode=='stability' else 'verification/u2-source/auto-volume-observer.mjs' if mode=='auto-volume' else 'verification/u2-source/volume-observer.mjs' if mode=='volume' else 'verification/u2-source/baseline-observer.mjs'),app/'u2-observer.mjs')
shutil.copy2(R/'verification/u2-source/formal-next-contract.mjs',app/'formal-next-contract.mjs')
shutil.copy2(R/'verification/u2-source/resource-trend.mjs',app/'resource-trend.mjs')
pkg=json.loads((app/'package.json').read_text());pkg['main']='u2-observer.mjs';(app/'package.json').write_text(json.dumps(pkg,indent=2))
(install/'private-bus.conf').write_text('''<!DOCTYPE busconfig PUBLIC "-//freedesktop//DTD D-Bus Bus Configuration 1.0//EN" "http://www.freedesktop.org/standards/dbus/1.0/busconfig.dtd"><busconfig><type>session</type><listen>unix:path=/private/run/bus</listen><auth>EXTERNAL</auth><policy context="default"><allow own="*"/><allow send_destination="*"/><allow receive_sender="*"/></policy></busconfig>''')
# Anonymous Chromium profile only; basic is not a credential-storage qualification.
(install/'start-inside.sh').write_text('''#!/bin/sh
set -eu
exec /app/adam --disable-setuid-sandbox --disable-gpu --password-store=basic --ozone-platform=wayland
''');(install/'start-inside.sh').chmod(0o700)
if mode in ['volume','auto-volume']:
 shutil.copy2(L/'pw.conf',install/'pw.conf')
 (install/'start-inside.sh').write_text("""#!/bin/sh
set -eu
export PIPEWIRE_RUNTIME_DIR=/private/run PIPEWIRE_REMOTE=pipewire-u2 PULSE_SERVER=unix:/private/run/pulse/native ADAM_MAIN_PRIVATE_OUTPUT=pulse
pipewire -c /app/pw.conf > /private/logs/pipewire.log 2>&1 & pw=$!
pipewire-pulse > /private/logs/pulse.log 2>&1 & pulse=$!
wireplumber > /private/logs/wireplumber.log 2>&1 & wp=$!
trap 'kill "$wp" "$pulse" "$pw" 2>/dev/null || true; wait || true' EXIT
n=0;until pactl info > /private/logs/pulse-info.txt 2>&1;do n=$((n+1));[ "$n" -lt 50 ] || exit 3;sleep .1;done
/app/adam --disable-setuid-sandbox --disable-gpu --password-store=basic --ozone-platform=wayland
""")

if mode=='auto-volume':
 p=install/'start-inside.sh';p.write_text(p.read_text().replace('ADAM_MAIN_PRIVATE_OUTPUT=pulse','ADAM_MAIN_PRIVATE_OUTPUT=auto'))

for n in ['home','home/.config','home/.local/share','cache','run','tmp','logs','crash','control']:(state/n).mkdir(parents=True,exist_ok=True,mode=0o700)
(state/'machine-id').write_text(secrets.token_hex(16)+'\n')
(root/'acceptance-plan.json').write_text(json.dumps(re.findall(r"await test\('([^']+)'",(app/'u2-observer.mjs').read_text())))
(root/'launch.json').write_text(json.dumps({'root':str(root),'port':port,'mode':mode}));(root/'launch.json').chmod(0o600)
manifest={}
for p in install.rglob('*'):
 if p.is_symlink():manifest[str(p.relative_to(install))]={'symlink':os.readlink(p)}
 elif p.is_file():manifest[str(p.relative_to(install))]={'sha256':hashlib.sha256(p.read_bytes()).hexdigest()}
(root/'manifest.json').write_text(json.dumps(manifest,indent=2))
assert (app/'main.mjs').read_bytes()==(R/'electron/main.mjs').read_bytes()
assert (app/'preload.cjs').read_bytes()==(R/'electron/preload.cjs').read_bytes()
print(root)
