from pathlib import Path
import os,subprocess,hashlib,json,sys
ROOT=Path('/var/home/h97m/Projects');OUT=ROOT/'adam-u2-ordering/.u2-lab/u2-1-20261102-run1'
def sha(p):return hashlib.sha256(p.read_bytes()).hexdigest()
def run(r,*a):return subprocess.check_output(['git','-C',str(r),*a],env={**os.environ,'GIT_OPTIONAL_LOCKS':'0'})
def snapshot():
 data={}
 for n in ['adam-eof-fix','adam-upstream-150-review','adam','adam-portable-vlc','adam-reader-close','adam-vlc-fix','adam-u2-ordering']:
  r=ROOT/n;files={};tracked=run(r,'ls-files','-z').decode().split('\0')[:-1];untracked=run(r,'ls-files','--others','--exclude-standard','-z').decode().split('\0')[:-1]
  for s in sorted(set(tracked+untracked)):
   if any(p in Path(s).parts for p in ['.eof-lab','.u2-lab','.isolated-lab','.portable-vlc-lab','node_modules','dist','cache','rootfs','__pycache__'])or s.startswith(('third_party/','drm/files/')):continue
   p=r/s
   files[s]={'symlink':os.readlink(p)}if p.is_symlink()else({'sha256':sha(p)}if p.is_file()else{'missing':True})
  data[n]={'head':run(r,'rev-parse','HEAD').decode().strip(),'branch':run(r,'branch','--show-current').decode().strip(),'status':run(r,'status','--porcelain=v1').decode(),'diff':hashlib.sha256(run(r,'diff','--binary','HEAD')).hexdigest(),'index':hashlib.sha256(run(r,'diff','--cached','--binary')).hexdigest(),'untracked':untracked,'files':files}
 data['adam-u2-backup']={p.name:sha(p) for p in (ROOT/'adam-u2-backup').iterdir()if p.is_file()}
 return data
if __name__=='__main__':
 p=OUT/sys.argv[1]
 if p.exists():raise SystemExit('Refuse to overwrite snapshot')
 p.write_text(json.dumps(snapshot(),indent=2));print(p)
