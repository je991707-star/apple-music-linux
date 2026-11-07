# Legacy/ordered recording Handler contracts only. New Main authority is tested
# separately in the real cmd package with real PM/Lease/cgo (never these stubs).
from pathlib import Path
import sys,runpy
root=Path(__file__).resolve().parents[2]
runpy.run_path(str(root/'verification/u2-retirement/extract-handlers.py'),run_name='__main__')
out=Path(sys.argv[1]);p=out/'handler.go';p.write_text(p.read_text().replace('import("context"','import("os";"context"'))
p=out/'fixture_test.go';p.write_text(p.read_text()+'''
var errMainSourceGone = playbackcmd.ErrStale
func(s *APIServer)admitMainVLC(*http.Request,*playbackcmd.Identity,string,string)(*playbackcmd.Ticket,error){return nil,playbackcmd.ErrStale}
func(s *APIServer)claimMainVLC(*http.Request,string,string,string)(playbackcmd.Snapshot,error){return playbackcmd.Snapshot{},playbackcmd.ErrStale}
func(m *manager)Release(id string){m.inner.Release(id)}
func prepareMainSourceDiagnostics(*APIServer,string){}
func disposeMainSourceDiagnostics(*APIServer,string){}
''');p.write_text(p.read_text().replace('import("engine/core/playback"','import("net/http";"engine/core/playback"'))
(out/'EXTRACTION-SCOPE.txt').write_text('Real production mutation functions. Recording Player only. Main authority helpers fail closed and are not exercised here; see real cmd main_source_fixture_test.go.\n')

p=out/"ordering_test.go";s=p.read_text().replace("type session struct{ AssetID, Codec string }","type session struct{ AssetID, Codec, ID string }").replace("return session{v.AssetID,v.Codec},true","return session{v.AssetID,v.Codec,v.ID},true");p.write_text(s)
