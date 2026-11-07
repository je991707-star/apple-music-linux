// Actual extracted production Controller -> fixed ordered protocol -> real Go
// handlers / Session Manager / StreamingReader / libVLC, isolated PCM sink.
import assert from 'node:assert/strict';
import { controller } from '../../electron/test/helpers/vlc-controller.mjs';
const [name,origin,a,b]=process.argv.slice(2);
const requests=[];
let stopSession=a;
let hold=false,retireBefore=false,release,held;
const heldReady=new Promise(r=>held=r),late=new Promise(r=>release=r);
const orderedFetch=async(url,options={})=>{
 if(retireBefore&&url.endsWith('/load')){retireBefore=false;await fetch(origin+'/fixture/retire',{method:'POST'});hold=true;}
 const response=await fetch(url,{...options,headers:{...options.headers,'X-Api-Token':'u2-local-fixture-client-only'}});
 const type=url.split('/').pop(),body=options.body&&JSON.parse(options.body);
 requests.push({type,status:response.status,command:body?.command});
 if(hold&&type==='load'){hold=false;held();await late;}
 return response;
};
const h=controller({ordered:true,orderedFetch,origin});h.session(a,12000);h.beginLoad();
assert.equal((await h.command('load',{sessionId:a})).status,200);
const stats=async()=>{const r=await fetch(origin+'/fixture/stats');return r.json();};
const wait=async(fn)=>{const end=Date.now()+7000;while(Date.now()<end){const s=await stats();if(fn(s))return s;await new Promise(r=>setTimeout(r,10));}throw new Error('native evidence deadline');};
await wait(s=>s.pcm.frames>0);
const endRetry=()=>{h.update(3000);h.facts({sourceFailed:true,readerEof:false});h.ended(0,'error');assert.equal(h.snapshot().retries,1);};
if(name==='retry-seek'||name==='retry-pause'){
 endRetry();
 if(name==='retry-seek'){await h.seek(500);await h.run(150);}else{await h.pause();await h.drain();}
 await h.run(500);
 assert.equal(requests.filter(v=>v.type==='load').length,1);
 const s=await stats();assert.equal(s.control.sessionId,a);
 if(name==='retry-seek')assert.equal(s.control.sequence,requests.filter(v=>v.type==='resume').at(-1)?.command.sequence??requests.filter(v=>v.type==='seek').at(-1).command.sequence);
 else assert.equal(s.control.sequence,requests.filter(v=>v.type==='pause').at(-1).command.sequence);
}
if(name==='preparing-retry-seek'||name==='preparing-retry-pause'){
 await fetch(origin+'/fixture/hold-preparation',{method:'POST'});
 endRetry();const old=h.run(500);
 assert.equal((await fetch(origin+'/fixture/preparing')).status,204);
 if(name==='preparing-retry-seek'){await h.seek(500);await h.run(150);}else await h.pause();
 await fetch(origin+'/fixture/release-preparation',{method:'POST'});await old;
 const s=await stats();assert.equal(s.control.sessionId,a);assert.equal(s.native.physicalLoad,1);
 assert.equal(s.control.sequence,requests.filter(v=>['seek','resume','pause'].includes(v.type)).at(-1).command.sequence);
}
if(name==='late-retry-retire-swap'||name==='old-404-after-swap'){
 endRetry();if(name==='old-404-after-swap')retireBefore=true;else hold=true;const old=h.run(500);await heldReady;
 await fetch(origin+'/fixture/retire',{method:'POST'});
 h.session(b,12000);h.beginLoad();assert.equal((await h.command('load',{sessionId:b})).status,200);
 const position=h.snapshot().position;release();await old;
 assert.equal(h.snapshot().position,position);assert.equal(h.events.filter(v=>v?.reopen).length,0);
 assert.equal((await stats()).control.sessionId,b);
 if(name==='old-404-after-swap')assert(requests.some(v=>v.type==='load'&&v.status===404));
}
if(name==='source-failure-ended'){
 await fetch(origin+'/fixture/fail',{method:'POST'});
 await wait(s=>s.pcm.events.some(e=>e.event==='MediaPlayerEndReached'));
 const raw=await (await orderedFetch(origin+'/api/v1/vlc/time',{method:'GET'})).json();
 assert.equal(raw.eof,false);
 h.update(3000);h.facts({readerEof:raw.eof,sourceFailed:true,sourceComplete:false});h.ended(raw.posMs,raw.state);
 assert.equal(h.snapshot().retries,1);assert.equal(h.next(),0);
}
if(name==='duplicate-terminal'){
 await fetch(origin+'/fixture/complete',{method:'POST'});
 await wait(s=>s.pcm.full_pcm_exact===true);
 const s=await wait(s=>s.pcm.events.some(e=>e.event==='MediaPlayerEndReached'));
 assert.equal(s.pcm.frames,192000);
 h.session(a,4000);h.update(3990);h.facts({readerEof:true,sourceComplete:null});
 h.ended(0);h.ended(0);assert.equal(h.next(),1);
}
if(name==='terminal-reload'){
 await fetch(origin+'/fixture/complete',{method:'POST'});
 await wait(s=>s.pcm.events.some(e=>e.event==='MediaPlayerEndReached'));
 await h.seek(500);await h.run(150);await h.drain();
 const s=await wait(s=>s.pcm.post_flush_suffix_exact===true);
 assert(Math.abs(s.pcm.first_post_flush_pcm_ms-500)<=4096/48);
 assert.equal(s.pcm.post_flush_contiguous,true);
 assert.equal(s.control.sessionId,a);
}
if(name==='engine-boot-rotation'){
 const old=requests.find(v=>v.type==='load').command;
 const renewed=await (await fetch(origin+'/fixture/restart',{method:'POST'})).json();
 const rejected=await orderedFetch(origin+'/api/v1/vlc/load',{method:'POST',body:JSON.stringify({sessionId:a,command:old})});
 // A discarded old Session may be 404 before the Boot gate; it never mutates the new player.
 assert([404,409].includes(rejected.status));
 h.session(renewed.sessionId,12000);h.beginLoad();stopSession=renewed.sessionId;
 assert.equal((await h.command('load',{sessionId:stopSession})).status,200);
 await wait(s=>s.pcm.frames>0);
 const latest=requests.filter(v=>v.type==='load'&&v.status===200).at(-1).command;
 assert.notEqual(latest.boot,old.boot);assert.notEqual(latest.epoch,old.epoch);
}
console.log(JSON.stringify({case:name,status:'PASS',controller:h.diagnostics(),evidence:await stats(),requests}));
await h.command('stop',{sessionId:['late-retry-retire-swap','old-404-after-swap'].includes(name)?b:stopSession});
