"""Replay the 14 unchanged recording-player contracts against current handlers.
This is NOT Native or Main-principal qualification; those use actual engine/cmd.
The earlier qualified test fixtures are read-only inputs, hashes are recorded.
"""
from pathlib import Path
import sys,re,hashlib,json,shutil
R=Path(__file__).resolve().parents[2];O=Path(sys.argv[1]).resolve()
assert O.parent.parent==R/'.u2-lab' and O.parent.name.startswith(('u2-6-','u2-7-','u2-8-','u2-9-'))
O.mkdir()
B=R/'.u2-lab/u2-5-20261102-run1/handler-qualified-final'
inputs={}
for name in ['ordering_test.go','additional_test.go','authority_gap_test.go','fixture_test.go','go.mod']:
 p=B/name;inputs[str(p.relative_to(R))]=hashlib.sha256(p.read_bytes()).hexdigest();shutil.copy2(p,O/name)
s=(O/'ordering_test.go').read_text().replace('type APIServer struct {','type APIServer struct {\nmainSources scopedRegistry',1);(O/'ordering_test.go').write_text(s)
s=(R/'engine/cmd/handlers_vlc.go').read_text();starts=list(re.finditer(r'^(func |type ownedVLCSource )',s,re.M));decl=[]
for i,m in enumerate(starts):
 d=s[m.start():starts[i+1].start()if i+1<len(starts)else len(s)]
 if any(v in d.split('\n',1)[0]for v in ['handleVLC','loadVLCCommand','vlcACK','vlcCommandError','ownedVLCSource']):decl.append(d)
(O/'handler.go').write_text('package ordering\nimport("os";"context";"encoding/json";"engine/core/vlc";"engine/internal/playbackcmd";"errors";"fmt";"net/http";"sync")\n'+'\n'.join(decl))
for src,dst in [('vlc_commands.go','commands.go'),('vlcgate.go','gate.go'),('source_retirement.go','retirement.go')]:
 (O/dst).write_text((R/'engine/cmd'/src).read_text().replace('package main','package ordering',1))
# Main grant implementations are intentionally NOT substituted with successful
# authority: earlier fixture's Main admission always rejects. These types make
# the observer-only ACK branch compile; it is inactive in these Legacy tests.
(O/'scoped_test.go').write_text('''package ordering
import("sync";"engine/internal/playbackcmd")
type scopedRegistry struct{mu sync.Mutex;grants map[string]*scopedGrant}
type scopedGrant struct{events *scopedEvents;lease *playbackcmd.Lease}
type scopedEvents struct{}
func(*scopedEvents)push(any,string,any){}
func(*scopedGrant)ownIdentity()any{return nil}
''')
(O/'extraction.json').write_text(json.dumps({'currentHandlerSHA256':hashlib.sha256(s.encode()).hexdigest(),'fixtureInputs':inputs,'recordingOnly':True,'mainGrantAdmission':'always denied; not qualified here','expectedContracts':14},indent=2))
