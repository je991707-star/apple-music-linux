"""Fresh source-extracted handler test module; replaces only preparation/native boundaries."""
from pathlib import Path
import sys,re,shutil,json,hashlib
R=Path(__file__).resolve().parents[2];out=Path(sys.argv[1]);out.mkdir()
alltext=(R/'engine/cmd/handlers_vlc.go').read_text();decl=[]
a=list(re.finditer(r'^(func |type ownedVLCSource )',alltext,re.M))
for i,m in enumerate(a):
 d=alltext[m.start():a[i+1].start()if i+1<len(a)else len(alltext)]
 if any(x in d.split('\n',1)[0] for x in ['handleVLC','loadVLCCommand','vlcACK','vlcCommandError','ownedVLCSource']):decl.append(d)
(out/'handler.go').write_text('package ordering\nimport("context";"encoding/json";"engine/core/vlc";"engine/internal/playbackcmd";"errors";"fmt";"net/http";"sync")\n'+'\n'.join(decl))
for src,dest in [('vlc_commands.go','commands.go'),('vlcgate.go','gate.go')]:
 (out/dest).write_text((R/'engine/cmd'/src).read_text().replace('package main','package ordering',1))
for src,dest in [('handler_contracts_test.go.txt','ordering_test.go'),('handler_additional_test.go.txt','additional_test.go')]:
 shutil.copyfile(R/'verification/u2-ordering'/src,out/dest)
(out/'go.mod').write_text('module engine/orderingprobe\ngo 1.26.0\nrequire engine v0.0.0\nreplace engine => '+str(R/'engine')+'\n')
(out/'manifest.json').write_text(json.dumps({'source_sha256':hashlib.sha256(alltext.encode()).hexdigest(),'boundary':'Controlled prepare and recording Native player; no DRM/Source Authorizer stub. Finite synthetic Session catalogue (a,b).','functions':len(decl)},indent=2))
print(out)
