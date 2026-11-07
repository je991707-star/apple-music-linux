// Private observer imports full normal Main; it has no account/login capability.
import {app,BrowserWindow,session} from 'electron/main';
import {verifyFormalNext} from './formal-next-contract.mjs';
import fs from 'node:fs';import cp from 'node:child_process';import {syncBuiltinESMExports} from 'node:module';
const rows=[],events=[];let win,engine,host,finished=false;
const sleep=ms=>new Promise(r=>setTimeout(r,ms));
function note(v){const row={...v,wall:Date.now()};events.push(row);fs.appendFileSync('/private/logs/events.jsonl',JSON.stringify(row)+'\n')}
async function until(fn,ms=12000){const end=Date.now()+ms;while(Date.now()<end){try{const v=await fn();if(v)return v}catch{}await sleep(50)}throw Error('bounded condition deadline')}
async function test(name,fn){try{rows.push({name,status:'PASS',evidence:await fn()})}catch(e){rows.push({name,status:'FAIL',error:e.message});try{fs.writeFileSync('/private/logs/failure-'+name+'.json',JSON.stringify({controller:await ipc('__u2Controller.state()'),engine:await status()},null,2))}catch{}}note(rows.at(-1))}
const spawn=cp.spawn;cp.spawn=function(binary,args,opts){const c=spawn(binary,args,opts);if(binary==='/app/resources/engine'){engine=c;note({event:'actual-engine-spawn',pid:c.pid})}return c};syncBuiltinESMExports();
app.setPath('userData','/private/home/.config/adam/electron-session');app.setPath('sessionData','/private/home/.config/adam/electron-session');app.setPath('crashDumps','/private/crash');app.setAppLogsPath('/private/logs');
app.on('browser-window-created',(_,w)=>{if(!win){win=w;w.webContents.on('did-start-navigation',d=>note({event:'navigation-start',mainFrame:d.isMainFrame,sameDocument:d.isSameDocument}));w.webContents.on('console-message',(_,...args)=>{const message=typeof args[0]==='object'?args[0].message:args[1];if(typeof message==='string'&&message.startsWith('[Adam VLC] ended'))note({event:'formal-controller-terminal',message});else if(typeof message==='string'&&message.startsWith('[Adam Engine]'))note({event:'controller-progress',message:message.slice(0,300)})})}});
app.whenReady().then(()=>{session.fromPartition('persist:apple-music').webRequest.onBeforeRequest((d,cb)=>{let cancel=true;try{const u=new URL(d.url),h=u.hostname;cancel=h==='localhost'||h==='::1'||/^127\./.test(h)||/^(idmsa|appleid|account)\.apple\.com$/.test(h)}catch{}cb({cancel})})});
const main=await import('./main.mjs');host=main.playbackSourceHost;
host.bindFactory({kind:'u2-fixed-synthetic-v1',accept:id=>['alac-a','alac-b','alac-tail','alac-fail','alac-fail-short','alac-delay','alac-404'].includes(id)});
const ipc=code=>win.webContents.executeJavaScript(code);
const approve=id=>host.approve({sender:win.webContents,senderFrame:win.webContents.mainFrame},id);
async function status(){return ipc(`adamBridge.orderedPlayback({operation:'time'})`)}
async function start(id){approve(id);await ipc(`__u2Controller.start([${JSON.stringify(id)}])`);return until(async()=>{const s=await ipc('__u2Controller.state()');return s.sessionId&&s.assetId===id&&s.mode?s:false})}
async function finish(){if(finished)return;finished=true;clearTimeout(watchdog);fs.writeFileSync('/private/logs/results.json',JSON.stringify({scope:'full normal Main/preload/Controller/private local queue, no account',rows},null,2));try{await host.revoke()}catch{}win?.destroy();app.quit()}
const watchdog=setTimeout(()=>{note({event:'observer-deadline'});void finish()},155000);
void app.whenReady().then(async()=>{
 await test('anonymous-normal-ui-sandbox-and-private-state',async()=>{
  await until(()=>win,5000);await until(()=>ipc(`location.origin==='https://music.apple.com'&&document.readyState==='complete'&&typeof __u2Controller==='object'`),55000);
  const s=await ipc(`({origin:location.origin,sandbox:adamBridge.playbackIsolation,authorized:MusicKit.getInstance().isAuthorized,node:typeof require,engine:window._adamEngineURL})`);
  if(s.authorized||s.node!=='undefined'||!s.sandbox.sandboxed||!s.sandbox.contextIsolated||fs.existsSync('/var/home/h97m')||!process.env.DBUS_SESSION_BUS_ADDRESS?.includes('/private/run/bus'))throw Error('isolation contract');
  win.show();fs.writeFileSync('/private/logs/normal-ui.png',(await win.webContents.capturePage()).toPNG());return s;
 });
 if(rows.at(-1).status!=='PASS'){await finish();return}
 await ipc(`window.__u2OwnEvents=[];adamBridge.onOwnPlaybackEvent(v=>{if(window.__u2OwnEvents.length<128)window.__u2OwnEvents.push({...v,observedWall:Date.now()})});true;`);
 await test('Main-approved-normal-controller-ALAC-192000-exact',async()=>{
  const s=await start('alac-a');const r=await until(async()=>{const r=await status();return r.body?.pcm?.full_pcm_exact?r:false},7000);if(r.body.pcm.frames!==192000)throw Error('PCM frames');return {controller:s,native:r.body};
 });
 await test('natural-EOF-next-once',async()=>{await until(()=>events.some(e=>e.event==='formal-controller-terminal'&&e.message.includes('→ _adamNext')),4000);await sleep(400);const count=events.filter(e=>e.event==='formal-controller-terminal'&&e.message.includes('→ _adamNext')).length;if(count!==1)throw Error('duplicate or missing Next');const r=await status();const end=r.body?.pcm?.events?.find(e=>e.event==='MediaPlayerEndReached');const next=events.find(e=>e.event==='formal-controller-terminal'&&e.message.includes('→ _adamNext'));if(!end||next.wall-Date.parse(end.wall)>2000)throw Error('EOF Next exceeded original 2s contract');return {nextRequests:count,nextLatencyMs:next.wall-Date.parse(end.wall)};});
 await test('scoped-SSE-real-minute-renewal-authorized-snapshot',async()=>{
  const started=Date.now(),before=await status(),samples=[];
  const source=await ipc('__u2Controller.state()');
  function processResources(){const stat=fs.readFileSync('/proc/self/status','utf8');return{pid:process.pid,fd:fs.readdirSync('/proc/self/fd').length,rssKiB:Number(stat.match(/VmRSS:\s+(\d+)/)?.[1]??0)}}
  for(let i=0;i<7;i++){samples.push({wall:Date.now(),main:processResources(),engine:(await status()).body?.pcm?.resources,subscriptions:host.subscriptionDiagnostics()});if(i<6)await sleep(11000)}
  const messages=await ipc('window.__u2OwnEvents');const renew=messages.filter(v=>v.resynchronized===true);
  const after=await status(),s=await ipc('__u2Controller.state()');
  if(Date.now()-started<60000||!renew.length||renew.some(v=>v.event?.kind!=='snapshot'||v.event?.identity?.sessionId!==source.sessionId)||s.sessionId!==source.sessionId||after.body.control.sequence!==before.body.control.sequence)throw Error('long renewal identity/order/Native mutation');
  const active=host.subscriptionDiagnostics();if(active.length!==1||active[0].queuePeak>8||active[0].pending>8||active[0].renewals<1||!messages.some(v=>v.observedWall>started+60000&&v.event?.identity?.sessionId===source.sessionId))throw Error('no own events beyond original lease');
  const evidence={elapsedMs:Date.now()-started,sessionId:source.sessionId,samples,renewals:renew,eventSequences:messages.filter(v=>v.event).map(v=>({seq:v.event.seq,wall:v.observedWall,kind:v.event.kind,sessionId:v.event.identity.sessionId})),continuity:'authorized-snapshot, NOT lossless replay',nativeCommandSequenceBefore:before.body.control.sequence,nativeCommandSequenceAfter:after.body.control.sequence};
  fs.writeFileSync('/private/logs/sse-long-run.json',JSON.stringify(evidence,null,2));await ipc('window.__u2OwnEvents=[];true');return evidence;
 });
 await test('terminal-seek-exact-suffix-one-frame',async()=>{
  await ipc('__u2Controller.seek(500)');const r=await until(async()=>{const r=await status();const p=r.body?.pcm;return p?.post_flush_suffix_exact&&p.first_post_flush_pcm_ms>=500?r:false},7000);const p=r.body.pcm;if(p.first_post_flush_pcm_ms-500>4096/48)throw Error('seek precision');return {pcm:p,controller:await ipc('__u2Controller.state()')};
 });
 await test('own-source-touch',async()=>{const s=await ipc('__u2Controller.state()');const r=await ipc(`adamBridge.touchPlaybackSession({sessionId:${JSON.stringify(s.sessionId)}})`);if(r.status!==200)throw Error('touch failed');return {status:r.status};});
 await test('scoped-own-metadata-unknown-sample-and-live-events',async()=>{
  const s=await ipc('__u2Controller.state()');
  const r=await ipc(`adamBridge.orderedPlayback({operation:'metadata',body:{sessionId:${JSON.stringify(s.sessionId)}}})`);
  const v=r.body;if(r.status!==200||v.identity.sessionId!==s.sessionId||v.sampleDurationMs!==null||v.readerError!==null||v.declaredDurationMs!==4000)throw Error('own metadata identity/unknown facts');
  const own=await ipc(`({metadata:window._adamEngine.ownMetadata,connected:window._adamEngine._connected,subscription:window._adamEngine._subscription})`);
  if(!own.connected||!own.subscription||own.metadata?.identity?.sessionId!==s.sessionId)throw Error('own source subscription absent');
  return {metadata:v,subscription:own.subscription,connected:own.connected};
 });
 await test('scoped-fixed-library-artwork-preferences',async()=>{
  const s=await ipc('__u2Controller.state()');const queries=[{operation:'library',body:{sessionId:s.sessionId,page:0,limit:1}},{operation:'artwork',body:{sessionId:s.sessionId}}];const r=await ipc('Promise.all('+JSON.stringify(queries)+'.map(q=>adamBridge.orderedPlayback(q)))');
  if(r[0].status!==200||r[0].body.kind!=='public'||r[0].body.items.length!==1||!r[0].body.hasMore||r[1].status!==200||r[1].body.mime!=='image/png'||!r[1].body.base64.startsWith('iVBOR'))throw Error('fixed library/artwork DTO');
  const preference=await ipc(`(async()=>{await adamBridge.storeWrite('soundCheckEnabled',false);return await adamBridge.storeRead('soundCheckEnabled')})()`);if(preference!==false)throw Error('UI preference');
  return {preference,library:r[0].body,artwork:{mime:r[1].body.mime,bytes:r[1].body.base64.length},accountLibrary:'BLOCKED'};
 });
 await test('scoped-app-volume-native-and-stale-source-denial',async()=>{
  const s=await ipc('__u2Controller.state()');const query=value=>ipc('adamBridge.orderedPlayback('+JSON.stringify({operation:'setVolume',body:{sessionId:s.sessionId,value}})+')');
  const changed=await query(80);const native=await status();const restored=await query(100);note({event:'scoped-volume-native-readback',changed,restored});
  if(changed.status!==200||changed.body.stage!=='app-volume-returned'||changed.body.nativeValue!==80||restored.status!==200||restored.body.nativeValue!==100)throw Error('versioned app volume');
  return {changed:changed.body,restored:restored.body,nativeVolume:native.body.volume,scope:'app Player only, no system volume'};
 });
 await test('scoped-native-unconfirmed-volume-fails-closed',async()=>{const s=await ipc('__u2Controller.state()');const r=await ipc('adamBridge.orderedPlayback('+JSON.stringify({operation:'setVolume',body:{sessionId:s.sessionId,value:80}})+')');if(r.status!==503)throw Error('unconfirmed Native volume became success');return {status:r.status,positiveQualification:'BLOCKED',rollback:false};});
 await test('scoped-authority-negative-operations',async()=>{
  const s=await ipc('__u2Controller.state()');const queries=[{operation:'metadata',body:{sessionId:'foreign'}},{operation:'metadata',body:{sessionId:s.sessionId,url:'file:///etc/passwd'}},{operation:'library',body:{sessionId:s.sessionId,page:99,limit:99}},{operation:'artwork',body:{sessionId:s.sessionId,url:'https://example.test'}},{operation:'setVolume',body:{sessionId:s.sessionId,value:201}},{operation:'prewarm',body:{sessionId:s.sessionId}},{operation:'eventAck',body:{subscriptionId:'foreign',seq:1}}];
  const replies=await ipc('Promise.all('+JSON.stringify(queries)+'.map(q=>adamBridge.orderedPlayback(q)))');if(replies.some(r=>r.status!==403))throw Error('scoped authority bypass');const credentialDenied=await ipc(`adamBridge.storeRead('MUSIC_TOKEN').then(()=>false,()=>true)`);if(!credentialDenied)throw Error('credential-shaped key access');return {denied:replies.length,prewarm:'BLOCKED queue authority unavailable'};
 });
 await test('new-source-handoff-pause-resume',async()=>{
  const oldSubscription=await ipc('window._adamEngine._subscription');
  const next=await start('alac-b');await until(async()=>{const r=await status(),s=await ipc('__u2Controller.state()');return r.body?.state==='playing'&&!s.loading},2000);await ipc('__u2Controller.pause()');await until(async()=>{const r=await status();return r.body?.state==='paused'},2000);await ipc('__u2Controller.resume()');const r=await until(async()=>{const r=await status();return r.body?.pcm?.full_pcm_exact?r:false},7000);if(r.body.pcm.frames!==192000)throw Error('handoff PCM');const closures=await ipc('window.__u2OwnEvents.filter(e=>e.closed)');if(!closures.some(e=>e.subscriptionId===oldSubscription))throw Error('old source subscription not closed');const newSubscription=await ipc('window._adamEngine._subscription');if(!newSubscription||oldSubscription===newSubscription)throw Error('source subscription not rotated');return {controller:next,native:r.body,oldSubscription,newSubscription};
 });
 await test('tail-silence-216000-exact',async()=>{const s=await start('alac-tail');const r=await until(async()=>{const r=await status();return r.body?.pcm?.full_pcm_exact&&r.body.pcm.frames===216000?r:false},7000);return{controller:s,native:r.body};});
 await test('forged-session-and-lease-denied',async()=>{const s=await ipc('__u2Controller.state()');const r=await ipc(`Promise.all([adamBridge.orderedPlayback({operation:'touch',body:{sessionId:'forged'}}),adamBridge.orderedPlayback({operation:'touch',body:{sessionId:${JSON.stringify(s.sessionId)},leaseId:'forged'}})])`);if(r.some(v=>v.status!==403))throw Error('authority bypass');return{denied:r.length};});
 await test('ALAC-A-to-B-formal-next-native-identity',async()=>{
  const startWall=Date.now(),traceStart=(await ipc('__u2Controller.nextCalls()')).length;
  approve('alac-a');await ipc(`__u2Controller.start(['alac-a','alac-b'])`);
  const ac=await until(async()=>{const s=await ipc('__u2Controller.state()');return typeof s.sessionId==='string'&&s.sessionId.length>0&&s.assetId==='alac-a'&&s.mode?s:false});
  let ae=await until(async()=>{const r=await status();return r.body?.sessionId===ac.sessionId&&r.body.pcm?.native?.callbackAttached?r.body:false},3000);
  const aOwner=await ipc('__u2Controller.owner()');approve('alac-b');
  const transitions=[];
  const bc=await until(async()=>{const s=await ipc('__u2Controller.state()');const r=await status();
   if(r.body?.sessionId===ac.sessionId)ae=r.body;
   if(transitions.length<80)transitions.push({wall:Date.now(),sessionId:s.sessionId,assetId:s.assetId,engineSession:r.body?.sessionId??null});
   return typeof s.sessionId==='string'&&s.sessionId.length>0&&s.assetId==='alac-b'&&s.sessionId!==ac.sessionId?s:false;
  },7000);
  const bAcquiredWall=Date.now();
  const be=await until(async()=>{const r=await status(),v=r.body,p=v?.pcm;
   return v?.sessionId===bc.sessionId&&v.control?.sessionId===bc.sessionId&&p?.native?.lease?.sessionId===bc.sessionId&&p.native.callbackAttached&&p.coherent===true&&p.full_pcm_exact&&p.frames===192000&&v.state==='ended'?v:false;
  },7000);
  const aEOF=be.pcm.history.find(h=>h.binding?.sessionId===ac.sessionId&&h.events?.some(e=>e.event==='MediaPlayerEndReached'));
  const nextCalls=(await ipc('__u2Controller.nextCalls()')).slice(traceStart),aNext=nextCalls.filter(c=>c.sessionId===ac.sessionId);
  const denied=await ipc('adamBridge.orderedPlayback('+JSON.stringify({operation:'release',body:{sessionId:ac.sessionId}})+')');
  await ipc('__u2Controller.stalePoll('+JSON.stringify(ac.sessionId)+','+JSON.stringify(ac.pollRevision)+')');
  const stale404=await ipc('__u2Controller.staleReopen('+JSON.stringify(aOwner)+')');await sleep(100);
  const after=await status(),afterController=await ipc('__u2Controller.state()'),afterCalls=(await ipc('__u2Controller.nextCalls()')).slice(traceStart);
  const cleanup={oldReleaseRejected:denied.status===403,old404Rejected:stale404===false,bStillCurrent:after.body?.sessionId===bc.sessionId&&afterController.sessionId===bc.sessionId&&after.body.pcm?.native?.lease?.sessionId===bc.sessionId};
  const evidence={a:{controller:ac,engine:ae},b:{controller:bc,engine:be},aEOF,next:{count:afterCalls.filter(c=>c.sessionId===ac.sessionId).length,sessionId:ac.sessionId,calls:nextCalls},cleanup,expectedBHash:'02e10bff81e3d2d06f4130476d4c30fbe2500a892c66b5df1c67b84231786ed4',transitions,timing:{startWall,bAcquiredWall,bCompletedWall:Date.now(),aNextWall:aNext[0]?.wall,aEOFNativeWall:aEOF?.events?.find(e=>e.event==='MediaPlayerEndReached')?.wall}};
  fs.writeFileSync('/private/logs/formal-next-identity.json',JSON.stringify(evidence,null,2));
  verifyFormalNext(evidence);return evidence;
 });
 await test('source-release-and-replay-denied',async()=>{const s=await ipc('__u2Controller.state()');const r=await ipc('adamBridge.orderedPlayback('+JSON.stringify({operation:'release',body:{sessionId:s.sessionId}})+')');if(r.status!==200)throw Error('release failed');const denied=await ipc(`adamBridge.touchPlaybackSession({sessionId:${JSON.stringify(s.sessionId)}})`);if(denied.status!==403)throw Error('retired touch');return{stage:r.body.stage,replay:denied.status};});
 await test('owned-404-reopen-new-lease-cannot-revive-retired-source',async()=>{
  const old=await start('alac-404');await until(async()=>{const r=await ipc(`adamBridge.touchPlaybackSession({sessionId:${JSON.stringify(old.sessionId)}})`);return r.status===404},4000);
  const reopened=await ipc('__u2Controller.reopen(0.5)');if(reopened!==true)throw Error('reopen not accepted');
  const next=await until(async()=>{const s=await ipc('__u2Controller.state()');return s.sessionId&&s.sessionId!==old.sessionId?s:false},2500);
  if(next.assetId!=='alac-404')throw Error('wrong reopen asset');const denied=await ipc(`adamBridge.touchPlaybackSession({sessionId:${JSON.stringify(old.sessionId)}})`);if(denied.status!==403)throw Error('old source adopted new authority');return {oldSession:old.sessionId,newSession:next.sessionId,retiredTouch:denied.status};
 });
 await test('short-failed-source-preserves-upstream-short-track-policy',async()=>{
  const before=events.filter(e=>e.event==='formal-controller-terminal'&&e.message.includes('→ _adamNext')).length;await start('alac-fail-short');
  const r=await until(async()=>{const r=await status();return r.body?.sourceFailed===true?r:false},3000);
  await until(()=>events.filter(e=>e.event==='formal-controller-terminal'&&e.message.includes('→ _adamNext')).length===before+1,4000);
  const s=await ipc('__u2Controller.state()');if(s.retryCount!==0||r.body.sourceComplete!==false||r.body.eof===true)throw Error('upstream short-track policy changed');return {controller:s,native:r.body};
 });
 await test('source-failure-is-not-clean-EOF-and-retry-bounded',async()=>{
  await start('alac-fail');const r=await until(async()=>{const r=await status();return r.body?.sourceFailed===true?r:false},3000);if(r.body.sourceComplete!==false||r.body.eof===true)throw Error('failure became clean EOF');
  const ownFailure=await until(()=>ipc(`window.__u2OwnEvents.find(v=>v.event?.metadata?.sourceFailed===true&&v.event.identity.sessionId===${JSON.stringify(r.body.sessionId)})`),2000);const s=await until(async()=>{const s=await ipc('__u2Controller.state()');return s.retryCount>0?s:false},5000);if(s.retryCount>2)throw Error('retry limit');return {native:r.body,controller:s,ownFailure};
 });
 await test('document-reload-revokes-approved-intent',async()=>{approve('alac-a');let loaded=false;win.webContents.once('did-finish-load',()=>loaded=true);win.webContents.reload();await until(()=>loaded,30000);const r=await ipc(`adamBridge.orderedPlayback({operation:'create',body:{assetId:'alac-a'}})`);if(r.status!==403)throw Error('approval crossed document');return {status:r.status};});
 await test('Engine-restart-boot-pin-rotation-no-grant-inheritance',async()=>{await until(()=>ipc(`typeof __u2Controller==='object'`),15000);await start('alac-a');const oldSubscription=await ipc('window._adamEngine._subscription');const old=JSON.parse(fs.readFileSync('/private/control/playback-boot.json'));engine.kill('SIGKILL');const next=await until(()=>{const n=JSON.parse(fs.readFileSync('/private/control/playback-boot.json'));return n.boot!==old.boot?n:false},20000);if(next.pin===old.pin||next.nonce===old.nonce)throw Error('trust reused');const r=await ipc(`adamBridge.orderedPlayback({operation:'create',body:{assetId:'alac-a'}})`);if(r.status!==403)throw Error('old approval reused');const closure=await ipc('({closed:window._adamEngine.lastClosure,connected:window._adamEngine._connected,subscription:window._adamEngine._subscription})');if(closure.connected||closure.subscription!==null||!closure.closed||!oldSubscription)throw Error('Engine exit did not close own subscription');return {oldBoot:old.boot,newBoot:next.boot,pinRotated:true,oldSubscription,closure};});
 await test('Renderer-does-not-inherit-Main-transport-or-source-issuer',async()=>{
  const pid=win.webContents.getOSProcessId();const env=fs.readFileSync('/proc/'+pid+'/environ').toString();if(/ADAM_API_TOKEN=|ADAM_MAIN_SOURCE_ISSUER=|ADAM_MAIN_BOOT_NONCE=/.test(env))throw Error('Renderer inherited authority');return {checked:true};
 });
 await finish();
}).catch(e=>{note({event:'observer-error',message:e.message});void finish()});
