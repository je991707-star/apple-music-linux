"""Check the original runtime manifest without following symlinks as file pins."""
from pathlib import Path
import hashlib,json,os,sys
root=Path('/var/home/h97m/Projects/adam-portable-vlc/.portable-vlc-lab/phase4c-20261029-run1/build-repeat')
manifest=json.loads((root/'outputs-sha256.json').read_text())
results=[]
for name,pin in manifest.items():
 if not name.startswith('runtime-bundle/'):continue
 p=root/name
 actual={'symlink':os.readlink(p)}if p.is_symlink()else {'sha256':hashlib.sha256(p.read_bytes()).hexdigest()}
 results.append({'path':name,'match':actual==pin})
assert len(results)==93, len(results)
assert all(v['match'] for v in results)
Path(sys.argv[1]).write_text(json.dumps({'checked':len(results),'results':results},indent=2)+'\n')
