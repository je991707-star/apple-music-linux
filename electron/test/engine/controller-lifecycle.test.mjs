import { test } from 'node:test';
import assert from 'node:assert/strict';
import { controller } from '../helpers/vlc-controller.mjs';
for(const raw of [0,-1,45000])test(`terminal ${raw} retains owned playing anchor`,async()=>{const h=controller();h.update(180000);h.update(raw,'ended');h.ended(raw);await h.run(500);assert.equal(h.commands.find(v=>v.op==='load').body.startMs,180000);});
test('backward Seek invalidates older playing position before any new playing sample',async()=>{const h=controller();h.update(180000);await h.seek(45000);await h.run(150);h.ended(0);await h.run(500);assert.equal(h.commands.find(v=>v.op==='load').body.startMs,45000);});
test('new Load invalidates all source/terminal observations',()=>{const h=controller();h.facts({sourceFailed:true,readerEof:true,sourceComplete:true});h.beginLoad();assert.equal(h.diagnostics().sourceFailed,null);assert.equal(h.diagnostics().readerEof,null);});
test('reader EOF and explicit failure remain distinct from unknown completion',async()=>{const h=controller();h.update(180000);h.facts({readerEof:true,sourceFailed:true,sourceComplete:null});h.ended(0);assert.equal(h.snapshot().retries,1);assert.equal(h.diagnostics().sourceComplete,null);});
test('clean reader EOF retains upstream short-track advance policy',()=>{const h=controller();h.update(180000);h.facts({readerEof:true});h.ended(0);assert.equal(h.next(),1);assert.equal(h.snapshot().retries,0);});
test('CDN gate suppresses duplicate Next without starting Retry',()=>{const h=controller();h.cdn(true);h.update(239000);h.ended(0);h.ended(0);assert.equal(h.next(),0);assert.equal(h.snapshot().retries,0);});
test('poll restarts cannot reset two-retry budget',async()=>{const h=controller();for(let n=0;n<3;n++){h.update(180000);h.ended(0);await h.run(500);h.start();}assert.equal(h.snapshot().retries,2);assert.equal(h.commands.filter(v=>v.op==='load').length,2);assert.equal(h.next(),1);});
test('late session touch 404 cannot reopen a new session',async()=>{let finish;const h=controller({fetch:()=>new Promise(r=>finish=r)});const old=h.touch();h.session('b');finish({status:404});await old;assert(!h.events.some(v=>v?.reopen));});
test('owned Playing overrun preserves upstream duration growth',()=>{const h=controller();h.update(300000);assert(h.snapshot().duration>=300);});
test('status body delayed past Seek cannot move UI or expose raw error',async()=>{let finish;const h=controller({fetch:async()=>({ok:true,json:()=>new Promise(r=>finish=r)})});const poll=h.poll();await h.drain();await h.seek(120000);finish({state:'ended',posMs:999999,lengthMs:999999,readerError:'https://private.invalid/secret'});await poll;assert.equal(h.snapshot().position,120000);assert.equal(h.diagnostics().readerError,null);assert.equal(h.next(),0);});
test('Ordered polling follows a successful Seek instead of retaining the old seek revision',async()=>{
 const control={boot:'test-boot',epoch:'',sessionId:'a',loadGeneration:1,sequence:0};
 const h=controller({ordered:true,orderedFetch:async(url,opt)=>{
 const type=url.split('/').pop();let data;
 if(type==='time')data={control:{...control},state:'playing',posMs:800,lengthMs:240000};
 else if(type==='control'){control.epoch='test-epoch';data={...control};}
 else{const command=JSON.parse(opt.body).command;control.sequence=command.sequence;data={command,ack:{load:'media-committed',intent:'intent-invalidated',seek:'seek-dispatched',resume:'native-committed'}[type]};}
 return {ok:true,status:200,json:async()=>data};
 }});
 h.beginLoad();await h.command('load',{sessionId:'a'});
 await h.seek(500);await h.run(150);await h.drain();await h.poll();
 assert.equal(h.snapshot().position,800);
});
