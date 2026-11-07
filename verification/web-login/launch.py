# Human-operated launch only; --probe has NO authorization actions and exits.
from pathlib import Path
import subprocess,sys,os,stat,json,hashlib,threading,time,secrets
R=Path(__file__).resolve().parents[2];L=Path(sys.argv[1]).resolve();options=sys.argv[2:];assert len(options)==len(set(options)) and all(v in ['--probe','--diagnostics']for v in options)
probe='--probe'in options;diagnostics='--diagnostics'in options
assert L.parent==R/'.u2-lab' and L.name.startswith(('u2-10a2-','u2-10a2b-','u2-10a2c-','u2-10a2d-','u2-10a2e-','u2-10a2f-'))
assert L.stat().st_uid==os.getuid() and stat.S_IMODE(L.stat().st_mode)==0o700
I=L/'web-install-qualified';found={}
for p in I.rglob('*'):
 if p.is_symlink():assert p.resolve().is_relative_to(I);found[str(p.relative_to(I))]={'symlink':os.readlink(p)}
 elif p.is_file():found[str(p.relative_to(I))]={'sha256':hashlib.sha256(p.read_bytes()).hexdigest()}
assert found==json.loads((L/'WEB-INSTALL-QUALIFIED-MANIFEST.json').read_text()),'Staged files changed; refuse login'
if not probe:
 receipt=json.loads((L/'WEB-LOGIN-SDK-QUALIFICATION.json').read_text())
 assert receipt['status']=='PASS' and receipt['installManifestSHA256']==hashlib.sha256((L/'WEB-INSTALL-QUALIFIED-MANIFEST.json').read_bytes()).hexdigest() and receipt['launcherSHA256']==hashlib.sha256(Path(__file__).read_bytes()).hexdigest(), 'Anonymous qualification required for these exact inputs'
assert not any(p.is_file() and p.name in ['engine','libdrm_client.so','libandroidappmusic.so']for p in I.rglob('*'))
wayland=Path('/run/user')/str(os.getuid())/os.environ.get('WAYLAND_DISPLAY','wayland-0');assert wayland.is_socket()
a=['bwrap','--unshare-user','--unshare-pid','--unshare-ipc','--unshare-uts','--die-with-parent','--new-session','--clearenv','--ro-bind','/usr','/usr','--symlink','usr/bin','/bin','--symlink','usr/lib','/lib','--symlink','usr/lib64','/lib64','--proc','/proc','--dev','/dev','--tmpfs','/tmp','--tmpfs','/run','--tmpfs','/private','--dir','/etc','--dir','/display','--ro-bind',str(wayland),'/display/wayland','--ro-bind',str(I),'/app']
for n in ['/etc/ld.so.cache','/etc/fonts','/etc/ssl/certs','/etc/pki/ca-trust','/etc/pki/tls/certs','/etc/resolv.conf','/etc/hosts','/etc/nsswitch.conf','/etc/passwd','/etc/group','/etc/localtime']:
 if Path(n).exists():a+=['--ro-bind',str(Path(n).resolve()),n]
env={'PATH':'/usr/bin:/bin','HOME':'/private/home','XDG_CONFIG_HOME':'/private/config','XDG_DATA_HOME':'/private/data','XDG_CACHE_HOME':'/private/cache','XDG_RUNTIME_DIR':'/private/run','TMPDIR':'/private/tmp','WAYLAND_DISPLAY':'/display/wayland','XDG_SESSION_TYPE':'wayland','LANG':'en_US.UTF-8','DBUS_SYSTEM_BUS_ADDRESS':'unix:path=/private/run/no-system-bus','ADAM_WEB_LOGIN_ONLY':'1'}
if diagnostics:
 reason_dir=L/('private-reasons-'+time.strftime('%Y%m%d-%H%M%S')+'-'+secrets.token_hex(4));reason_dir.mkdir(mode=0o700)
 a+=['--bind',str(reason_dir),'/diagnostics'];env['ADAM_WEB_LOGIN_DIAGNOSTICS']='1'
 print('Reason codes only: '+str(reason_dir/'reasons.jsonl'),flush=True)
if probe:
 evidence=L/'anonymous-sdk-qualification';evidence.mkdir(mode=0o700);a+=['--bind',str(evidence),'/evidence'];env['ADAM_WEB_LOGIN_PROBE']='1'
for k,v in env.items():a+=['--setenv',k,v]
a+=['--chdir','/private','/app/start-private.sh']
# No console/net/access logs: login URLs/tokens/cookies must not enter evidence.
print('Private Web-only instance. No saved profile, Engine, DRM or audible playback.',flush=True)
print('Close the private window/menu to end; Ctrl+C terminates ONLY this isolated process tree.',flush=True)
p=subprocess.Popen(a,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
# Observe only our descendants' private PID namespaces; no command lines/secrets.
host_ns=os.readlink('/proc/self/ns/pid');namespaces=set();stop=threading.Event()
def observe():
 while not stop.is_set():
  todo=[p.pid];seen=set()
  while todo:
   pid=todo.pop()
   if pid in seen:continue
   seen.add(pid)
   try:
    ns=os.readlink('/proc/'+str(pid)+'/ns/pid')
    if ns!=host_ns:namespaces.add(ns)
    todo.extend(int(n)for n in Path('/proc/'+str(pid)+'/task/'+str(pid)+'/children').read_text().split())
   except (OSError,ValueError):pass
  stop.wait(.05)
monitor=threading.Thread(target=observe,daemon=True);monitor.start()
try:code=p.wait(timeout=55 if probe else None)
except (KeyboardInterrupt,subprocess.TimeoutExpired):
 p.terminate()
 try:code=p.wait(timeout=3)
 except subprocess.TimeoutExpired:p.kill();code=p.wait()
 print('Private namespace ended. This is local cleanup, not remote Apple sign-out.',flush=True)
stop.set();monitor.join(timeout=1)
def remaining():
 live=0
 for proc in Path('/proc').iterdir():
  if not proc.name.isdigit():continue
  try:
   if os.readlink(proc/'ns/pid')in namespaces and (proc/'stat').read_text().rsplit(')',1)[1].split()[0]!='Z':live+=1
  except OSError:pass
 return live
end=time.monotonic()+3
while remaining()and time.monotonic()<end:time.sleep(.05)
if remaining():raise SystemExit('CLEANUP UNCONFIRMED: private processes still alive; no login readiness')
if probe:
 (evidence/'cleanup.json').write_text(json.dumps({'privateNamespacesObserved':len(namespaces),'livePrivateProcessesAfterExit':remaining(),'launcherExitObserved':p.returncode is not None,'tmpfsNamespaceEnded':bool(namespaces)}))
 assert namespaces,'Private process ownership was not observed'
 result=evidence/'probe.json';assert result.exists(),'Anonymous probe failed before safe evidence; no login allowed'
 value=json.loads(result.read_text());assert value['status']=='PASS' and value['engineProcessCount']==0,result.read_text()
 receipt=L/'WEB-LOGIN-SDK-QUALIFICATION.json';assert not receipt.exists(), 'Do not overwrite qualification'
 receipt.write_text(json.dumps({'status':'PASS','scope':'anonymous launch/isolation/cleanup only; real account not verified','installManifestSHA256':hashlib.sha256((L/'WEB-INSTALL-QUALIFIED-MANIFEST.json').read_bytes()).hexdigest(),'launcherSHA256':hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),'realAccountVerified':False},indent=2))
print('Private processes exited; tmpfs profile discarded. No persistent credentials saved by launcher.',flush=True)
raise SystemExit(code)
