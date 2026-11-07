import fs from 'node:fs';import path from 'node:path';import crypto from 'node:crypto';import {createRequire} from 'node:module';
const require=createRequire('/var/home/h97m/Projects/adam/electron/package.json'),esbuild=require('esbuild'),acorn=require('acorn');
const root=path.resolve(import.meta.dirname,'../..'),lab=process.argv[2];if(!lab||!path.resolve(lab).match(new RegExp('^'+root+'/.u2-lab/u2-(?:5|5a|6|7|8|9)-')))throw Error('New U2-5 lab required');
const original=fs.readFileSync(root+'/electron/src/engine-playback.js','utf8');let source=original;const edits=[];
function replace(from,to){if(source.split(from).length!==2)throw Error('Unexpected source '+from.slice(0,60));source=source.replace(from,to);edits.push(from)}
source="import {createFixedQueue} from 'u2-fixed-queue';\nconst __fixedQueue=createFixedQueue();\nconst __formalNextCalls=[];\n"+source;
replace('function getMKAudio() {\n','function getMKAudio() { return __fixedQueue.audio; /* fixed local queue; not SDK audio */\n');
replace('    const mk = await waitForMusicKit();','    const mk = __fixedQueue.host; // data-only anonymous local queue');
replace('    await waitForLossless(T().losslessWait);\n    if (genStale(myGen)) return;\n\n    try {\n        const sess = await _resolveSession','    // Clear fixed bytes require no DRM capability.\n    if (genStale(myGen)) return;\n\n    try {\n        const sess = await _resolveSession');
replace('    _adamNextRef = _adamNext;', `    _adamNextRef = async (...args) => {__formalNextCalls.push({sessionId:_sessionId,assetId:_currentAssetId,wall:Date.now(),loadGeneration:_vlcLoadGeneration});return _adamNext(...args);};`);
replace('    if (mk.nowPlayingItem) handleTrackChange(mk);',`    window.__u2Controller=Object.freeze({
 start:async(ids)=>{__fixedQueue.seed(ids);_sessionContainers.length=0;_sessionContainers.push({items:[...ids]});_sessionContainerIdx=0;_sessionItemIdx=0;await mk.changeToMediaAtIndex(0)},
 volume:value=>{__fixedQueue.audio.volume=value/100},pause:()=>mk.pause(),resume:()=>mk.play(),seek:ms=>mk.seekToTime(ms/1000),
 stop:async()=>{await _vlcCommands.stop();_resetPlaybackState()},touch:()=>_touchSession(),reopen:pos=>_reopenCurrentTrack(pos),
 nextCalls:()=>__formalNextCalls.slice(),owner:()=>_vlcRetryOwner(),
 stalePoll:(session,generation)=>_vlcPollTick(__fixedQueue.audio,session,generation),staleReopen:owner=>_reopenCurrentTrack(0,owner),
 state:()=>({sessionId:_sessionId,assetId:_currentAssetId,loadGeneration:_vlcLoadGeneration,seekRevision:_vlcSeekRevision,pollRevision:_vlcPollGeneration,retryCount:_vlcRetryCount,position:_vlcPosMs,durationMs:_durationSec*1000,loading:_vlcLoading,paused:_vlcPaused,mode:_vlcMode,retryPending:_vlcRetryTimer!==null,queueIndex:_sessionItemIdx,queue:__fixedQueue.host.queue.items.map(x=>x.id)})
 });
    if (mk.nowPlayingItem) handleTrackChange(mk);`);
function declarations(text){const out=new Map();function walk(n){if(!n||typeof n!=='object')return;if(n.type==='FunctionDeclaration')out.set(n.id.name,text.slice(n.start,n.end));for(const v of Object.values(n))if(Array.isArray(v))v.forEach(walk);else if(v&&typeof v==='object')walk(v)}walk(acorn.parse(text,{ecmaVersion:'latest',sourceType:'module'}));return out}
const before=declarations(original),after=declarations(source),hashes={};for(const name of ['_resolveSession','_vlcRetryFrom','_vlcHandleEnded','_vlcUpdatePosition','_vlcPollTick','_vlcHandleStateChange','installMKSeekInterceptor','_requestVLCTransport','_adamNext','_adamGoto','_setupVLCPath']){if(!before.has(name)||before.get(name)!==after.get(name))throw Error('Controller algorithm changed '+name);hashes[name]=crypto.createHash('sha256').update(before.get(name)).digest('hex')}
await esbuild.build({stdin:{contents:source,sourcefile:'engine-playback.js',resolveDir:root+'/electron/src'},plugins:[{name:'fixed-source',setup(build){build.onResolve({filter:/^u2-fixed-queue$/},()=>({path:root+'/verification/u2-source/fixture-queue.js'}))}}],bundle:true,format:'iife',outfile:lab+'/engine-bundle.js',logLevel:'warning'});
fs.writeFileSync(lab+'/controller-overlay.json',JSON.stringify({wholeFile:true,astExtraction:false,edits,unchangedCriticalHashes:hashes},null,2));
for(const [file,out]of [['engine-sse.js','engine-sse-bundle.js'],['smart-cache.js','smart-cache-bundle.js']])await esbuild.build({entryPoints:[root+'/electron/src/'+file],bundle:true,format:'iife',outfile:lab+'/'+out,logLevel:'warning'});
