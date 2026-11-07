from pathlib import Path
import re,sys,shutil
R=Path(__file__).resolve().parents[2];O=Path(sys.argv[1]);O.mkdir()
s=(R/'engine/cmd/handlers_vlc.go').read_text();a=list(re.finditer(r'^(func |type ownedVLCSource )',s,re.M));decl=[]
for i,m in enumerate(a):
 d=s[m.start():a[i+1].start()if i+1<len(a)else len(s)]
 if any(x in d.split('\n',1)[0]for x in ['handleVLC','loadVLCCommand','vlcACK','vlcCommandError','ownedVLCSource']):decl.append(d)
(O/'handler.go').write_text('package ordering\nimport("context";"encoding/json";"engine/core/vlc";"engine/internal/playbackcmd";"errors";"fmt";"net/http";"sync")\n'+'\n'.join(decl))
for src,dst in [('vlc_commands.go','commands.go'),('vlcgate.go','gate.go'),('source_retirement.go','retirement.go')]:
 (O/dst).write_text((R/'engine/cmd'/src).read_text().replace('package main','package ordering',1))
for src,dst in [('handler_contracts_test.go.txt','ordering_test.go'),('handler_additional_test.go.txt','additional_test.go')]:
 s=(R/'verification/u2-ordering'/src).read_text()
 if dst=='ordering_test.go':
  s=s.replace('"engine/core/vlc"','"engine/core/vlc"\n"engine/core/playback"')
  s=s.replace('type manager struct{ missing map[string]bool }','type manager struct{ missing map[string]bool; inner *playback.Manager }')
  start=s.index('func (m manager) GetSession');end=s.index('\ntype streaming',start)
  s=s[:start]+'''func(m manager)GetSession(id string)(session,bool){if m.missing[id]{m.inner.Release(id)};v,ok:=m.inner.GetSession(id);if !ok{return session{},false};return session{v.AssetID,v.Codec},true}
func(m manager)AdmitSource(id,boot string,fn func()(*playbackcmd.Ticket,error))(*playbackcmd.Ticket,error){return m.inner.AdmitSource(id,boot,fn)}
func(m manager)RevokeSources(){m.inner.RevokeSources()}
'''+s[end:]
  s=s.replace('type player struct {','type player struct {\n beforeLoad func()',1)
  needle='func (p *player) LoadSourceCommand(src vlc.Source, t *playbackcmd.Ticket, ms int64) error {'
  s=s.replace(needle,needle+'\n if p.beforeLoad!=nil{p.beforeLoad()}\n if !t.Valid(){if src!=nil{src.Abort();src.Close()};return playbackcmd.ErrStale}',1)
  s=s.replace('type APIServer struct {','type APIServer struct {\n sourceRetirements sync.Map',1)
  s=re.sub(r'pm\s+manager', 'pm *manager',s)
 s=re.sub(r'pm:\s*manager\{','pm: &manager{',s)
 # Wrap test constructors, not production functions: provide a real finite Session Manager.
 pos=0
 while True:
  i=s.find('&APIServer{',pos)
  if i<0:break
  b=s.index('{',i);j=b+1;depth=1
  while depth:depth+=(s[j]=='{')-(s[j]=='}');j+=1
  s=s[:i]+'newServer(t,'+s[i:j]+')'+s[j:];pos=j+len('newServer(t,')+1
 (O/dst).write_text(s)
(O/'fixture_test.go').write_text('''package ordering
import("engine/core/playback";"engine/core/vlc";"engine/internal/playbackcmd";"testing")
func newServer(t *testing.T,s *APIServer)*APIServer{if s.pm==nil{s.pm=&manager{}};s.pm.inner=playback.NewOrderingSessionFixture();t.Cleanup(func(){var all []*sourceRetirement;s.sourceRetirements.Range(func(_,v any)bool{all=append(all,v.(*sourceRetirement));return true});s.cancelVLCCommands();if s.vlcPlayer!=nil{s.vlcPlayer.Stop()};for _,v:=range all{<-v.done}});return s}
func observeRetirementSource(_ *APIServer,src vlc.Source)vlc.Source{return src}
func(p *player)StopRetiredLease(l *playbackcmd.Lease)(bool,int){p.mu.Lock();defer p.mu.Unlock();if p.ticket==nil||p.ticket.SourceLease()!=l||l.Active(){return false,0};if p.src!=nil{p.src.Abort();p.src.Close();p.src=nil};p.pos=0;return true,1}
''')
(O/'go.mod').write_text('module engine/orderingprobe\ngo 1.26.0\nrequire engine v0.0.0\nreplace engine => '+str(R/'engine')+'\n')

shutil.copyfile(R/"verification/u2-retirement/authority_gap_test.go.txt",O/"authority_gap_test.go")
