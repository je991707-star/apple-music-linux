from pathlib import Path
import os,json,hashlib,socket,sys,subprocess,stat
root=Path(sys.argv[1]).resolve();c=json.loads((root/'launch.json').read_text());install=root/'install';state=root/'state'
assert root==Path(c['root']) and root.parent.name=='u2-4-20261102-run1'
for p in [root,state,state/'home',state/'home/.config',state/'cache',state/'run',state/'tmp',state/'control',state/'logs']:
 s=p.stat();assert p.resolve()==p and s.st_uid==os.getuid() and stat.S_IMODE(s.st_mode)==0o700
assert not any((state/'home/.config').iterdir()),'fresh private profile required'
assert not any((state/'logs').iterdir()),'fresh evidence directory required'
found={}
for p in install.rglob('*'):
 if p.is_symlink():
  assert p.resolve().is_relative_to(install),str(p);found[str(p.relative_to(install))]={'symlink':os.readlink(p)}
 elif p.is_file():found[str(p.relative_to(install))]={'sha256':hashlib.sha256(p.read_bytes()).hexdigest()}
assert found==json.loads((root/'manifest.json').read_text())
assert not any(p.name in ['libdrm_client.so','libandroidappmusic.so','fetch-fairplay-libs.sh'] for p in install.rglob('*'))
for port in range(c['port'],c['port']+4):
 assert port not in [10020,20020,20025,20026]
 with socket.socket() as s:s.bind(('127.0.0.1',port))
wayland=Path('/run/user')/str(os.getuid())/os.environ.get('WAYLAND_DISPLAY','wayland-0');assert wayland.is_socket()
args=['bwrap','--unshare-user','--unshare-pid','--unshare-ipc','--unshare-uts','--die-with-parent','--new-session','--clearenv','--ro-bind','/usr','/usr','--symlink','usr/bin','/bin','--symlink','usr/lib','/lib','--symlink','usr/lib64','/lib64','--proc','/proc','--dev','/dev','--tmpfs','/tmp','--tmpfs','/run','--dir','/etc','--dir','/display','--ro-bind',str(wayland),'/display/wayland','--ro-bind',str(install),'/app','--bind',str(state),'/private','--ro-bind',str(state/'machine-id'),'/etc/machine-id']
for p in ['/etc/ld.so.cache','/etc/fonts','/etc/ssl/certs','/etc/pki/ca-trust','/etc/pki/tls/certs','/etc/resolv.conf','/etc/hosts','/etc/nsswitch.conf','/etc/passwd','/etc/group','/etc/localtime']:
 if Path(p).exists():args+=['--ro-bind',str(Path(p).resolve()),p]
env={'PATH':'/usr/bin:/bin','HOME':'/private/home','XDG_CONFIG_HOME':'/private/home/.config','XDG_CACHE_HOME':'/private/cache','XDG_DATA_HOME':'/private/home/.local/share','XDG_RUNTIME_DIR':'/private/run','TMPDIR':'/private/tmp','WAYLAND_DISPLAY':'/display/wayland','XDG_CURRENT_DESKTOP':'KDE','XDG_SESSION_TYPE':'wayland','LANG':'en_US.UTF-8','DBUS_SYSTEM_BUS_ADDRESS':'unix:path=/private/run/no-system-bus','NO_AT_BRIDGE':'1','ADAM_VLC_ORDERED':'1','ADAM_ENGINE_PORT':str(c['port']),'ADAM_MAIN_CONTROL_DIR':'/private/control','ADAM_ENGINE_BIN':'/app/resources/engine'}
for k,v in env.items():args+=['--setenv',k,v]
args+=['--chdir','/private','/usr/bin/dbus-run-session','--config-file=/app/private-bus.conf','--','/app/start-inside.sh']
(root/'isolation-plan.json').write_text(json.dumps({'hostHomeVisible':False,'hostBusVisible':False,'nativeDRMIncluded':False,'chromiumSandboxRequired':True,'privateBus':True,'port':c['port'],'audioListener':'not implemented; no fallback enabled','keyringQualification':False,'credentialStorage':'anonymous-only profile; no credential save allowed; basic Chromium profile is NOT production credential qualification','network':'shared only for anonymous Apple Music page; Renderer loopback and login hosts blocked by observer'},indent=2))
with (root/'launch.log').open('w') as f:
 try:code=subprocess.run(args,stdout=f,stderr=subprocess.STDOUT,timeout=125).returncode
 except subprocess.TimeoutExpired:code=124
(root/'exit.json').write_text(json.dumps({'exit_code':code}));print(root.name,'exit',code)
raise SystemExit(code)
