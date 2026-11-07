"""Create only a fixed, synthetic 4-second ALAC B in a fresh qualification Lab.
No Runtime rebuild or external media; source A is never modified.
"""
from pathlib import Path
import array,hashlib,json,os,subprocess,sys
root=Path(__file__).resolve().parents[2];lab=Path(sys.argv[1]).resolve()
assert lab.parent==root/'.u2-lab' and lab.name.startswith('u2-5a-')
fixture=lab/'fixtures';source=fixture/'reference.s16le'
sha=lambda p:hashlib.sha256(p.read_bytes()).hexdigest()
assert sha(source)=='4502fb87e6c2651a3c5247310a187ce294f2032b37ad93d04bc3dc679bb50e36'
for name in ['b-distinct.s16le','b-distinct.m4a']:assert not (fixture/name).exists(),'Refuse fixture overwrite'
a=array.array('h');a.frombytes(source.read_bytes());assert len(a)==384000
if sys.byteorder!='little':a.byteswap()
b=array.array('h',(-v if v!=-32768 else 32767 for v in a))
if sys.byteorder!='little':b.byteswap()
ref=fixture/'b-distinct.s16le';media=fixture/'b-distinct.m4a';ref.write_bytes(b.tobytes())
env={k:v for k,v in os.environ.items()if k not in ['LD_LIBRARY_PATH','LD_PRELOAD','VLC_PLUGIN_PATH']}
version=subprocess.check_output(['/usr/bin/ffmpeg','-version'],env=env).decode().splitlines()[0]
subprocess.run(['/usr/bin/ffmpeg','-hide_banner','-nostdin','-v','error','-f','s16le','-ar','48000','-ac','2','-i',str(ref),'-c:a','alac','-movflags','+faststart',str(media)],env=env,check=True,timeout=15)
(lab/'b-generation.json').write_text(json.dumps({'encoder':version,'encoderBinarySHA256':sha(Path('/usr/bin/ffmpeg')),'AReference':sha(source),'BReference':sha(ref),'BMedia':sha(media),'frames':192000,'rate':48000,'channels':2,'bits':16,'formula':'signed sample negation; -32768 maps to 32767','sourceAUnchanged':True},indent=2))
