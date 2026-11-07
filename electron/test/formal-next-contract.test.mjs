import {test} from 'node:test';import assert from 'node:assert/strict';
import {verifyFormalNext} from '../../verification/u2-source/formal-next-contract.mjs';
function valid(){const mk=(id,asset,g,hash,physical)=>({controller:{sessionId:id,assetId:asset,loadGeneration:g},engine:{state:"ended",sessionId:id,sourceGeneration:g,sourceComplete:true,sourceFailed:false,eof:true,control:{boot:'boot',epoch:'epoch'+g,sessionId:id,loadGeneration:g,sequence:g},pcm:{frames:192000,sha256:hash,referenceSHA256:hash,full_pcm_exact:true,resetSerial:g,binding:{sessionId:id,assetId:asset,sourceGeneration:g,leaseId:'lease'+g,commandSequence:g},native:{physicalLoad:physical,physicalMedia:'media'+g,callbackAttached:true,callbackHandle:g,lease:{boot:'boot',sessionId:id,sourceGeneration:g,leaseId:'lease'+g}},coherent:true,events:[{event:'MediaPlayerPlaying',resetSerial:g},{event:'MediaPlayerEndReached',resetSerial:g}]}}});return{a:mk('A','alac-a',1,'a'.repeat(64),1),b:mk('B','alac-b',2,'b'.repeat(64),3),aEOF:{binding:{leaseId:'lease1'},sessionId:'A',resetSerial:1,full_pcm_exact:true,sha256:'a'.repeat(64),frames:192000,events:[{event:'MediaPlayerEndReached',resetSerial:1}]},next:{count:1,sessionId:'A'},cleanup:{bStillCurrent:true,oldReleaseRejected:true,old404Rejected:true},expectedBHash:'b'.repeat(64)}}
test('qualified identities and distinct current PCM are accepted',()=>assert.equal(verifyFormalNext(valid()),true));
for(const [name,change]of [
 ['B not yet acquired',v=>delete v.b.controller.sessionId],['B null',v=>v.b.controller.sessionId=null],['B same as A',v=>v.b.controller.sessionId='A'],
 ['Controller B but Engine A',v=>v.b.engine.sessionId='A'],['Native still A',v=>v.b.engine.pcm.native.lease.sessionId='A'],
 ['sink not reset',v=>v.b.engine.pcm.resetSerial=1],['retained A PCM',v=>v.b.engine.pcm.sha256='a'.repeat(64)],
 ['duplicate A EOF / Next',v=>v.next.count=2],['no A natural EOF',v=>v.aEOF.events=[]],['no B physical commit',v=>v.b.engine.pcm.native.physicalLoad=1],
 ['no B callback',v=>v.b.engine.pcm.native.callbackAttached=false],['old A cleanup erased B',v=>v.cleanup.bStillCurrent=false],
 ['late old release accepted',v=>v.cleanup.oldReleaseRejected=false],['late old 404 accepted',v=>v.cleanup.old404Rejected=false],
 ['wrong source generation',v=>v.b.engine.sourceGeneration=1],['wrong lease',v=>v.b.engine.pcm.native.lease.leaseId='lease1'],
 ['no B natural event',v=>v.b.engine.pcm.events=[]],['incoherent snapshot',v=>v.b.engine.pcm.coherent=false]
])test('strict oracle rejects '+name,()=>{const v=valid();change(v);assert.throws(()=>verifyFormalNext(v))});
test('original q/r null-Session regressions are rejected',async()=>{
 const fs=await import('node:fs');const data=JSON.parse(fs.readFileSync(new URL('./fixtures/formal-next-null-regression.json',import.meta.url)));
 for(const e of data){assert.equal(e.bSession,null);assert.equal(e.bSession!==e.aSession,true);assert.equal(e.pcm.full_pcm_exact,true);assert.throws(()=>verifyFormalNext({a:{controller:{sessionId:e.aSession}},b:{controller:{sessionId:e.bSession}}}),/B Session/)}
});
