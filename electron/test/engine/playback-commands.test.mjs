import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createPlaybackCommands } from '../../src/engine/playback-commands.js';
const origin='https://127.0.0.1:12345';
const response=(data,status=200)=>new Response(JSON.stringify(data),{status});
function client(override){
 const calls=[]; const owner={sessionId:'a',loadGeneration:1,seekRevision:0,transportRevision:0};
 const bridge=async(url,opt)=>{const type=url.split('/').pop(), body=opt.body&&JSON.parse(opt.body);calls.push({type,body});
 if(type==='time')return response({control:{boot:'boot',epoch:''}});
 if(type==='control')return response({boot:'boot',epoch:'epoch'});
 if(override)return override(type,body);
 return response({command:body.command,ack:{load:'media-committed',intent:'intent-invalidated',seek:'seek-dispatched'}[type]});};
 const p=createPlaybackCommands({origin,scope:()=>owner,enabled:true,orderedFetch:bridge,requestID:()=>String(calls.length)});
 return {p,calls,owner};
}
test('a rejected Intent Barrier must reject before debounced Seek',async()=>{const {p}=client(()=>response({error:'stale'},409));await assert.rejects(p.barrier(),/409/);});
test('ACK must match every issued identity field and stage',async()=>{for(const bad of ['identity','stage']){const {p}=client((type,b)=>response({command:bad==='identity'?{...b.command,sequence:0}:b.command,ack:bad==='stage'?'accepted':'media-committed'}));await assert.rejects(p.request(origin+'/api/v1/vlc/load',{body:'{}'}),/ACK/);}});
test('fixed playback route rejects proxy destinations and methods',async()=>{const {p,calls}=client();for(const url of ['https://example.org/api/v1/vlc/load',origin+'/api/v1/settings',origin+'/api/v1/vlc/load?token=x'])await assert.rejects(p.request(url),/fixed/);await assert.rejects(p.request(origin+'/api/v1/vlc/load',{method:'GET'}),/fixed/);assert.equal(calls.length,0);});
test('ordered mode without Main bridge fails closed',async()=>{const p=createPlaybackCommands({origin,scope:()=>({}),enabled:true});await assert.rejects(p.request(origin+'/api/v1/vlc/load'),/bridge unavailable/);});
test('late ACK cannot survive a newer scope revision',async()=>{let release;const pending=new Promise(r=>release=r);const {p,owner}=client(async(_,b)=>{await pending;return response({command:b.command,ack:'media-committed'});});const work=p.request(origin+'/api/v1/vlc/load',{body:'{}'});await new Promise(r=>setImmediate(r));owner.seekRevision++;release();await assert.rejects(work,/superseded/);});
test('Engine Boot rotation negotiates independently; a revoked epoch is never reclaimed',async()=>{
 let boot='one',epoch='';const commands=[];const owner={sessionId:'a',loadGeneration:1};
 const p=createPlaybackCommands({origin,scope:()=>owner,enabled:true,requestID:()=>String(commands.length),orderedFetch:async(url,opt)=>{
 const t=url.split('/').pop();if(t==='time')return response({control:{boot,epoch}});
 if(t==='control'){epoch='epoch-'+boot;return response({boot,epoch});}
 const b=JSON.parse(opt.body);commands.push(b.command);return response({command:b.command,ack:'media-committed'});
 }});
 await p.request(origin+'/api/v1/vlc/load',{body:'{}'});
 boot='two';epoch='';owner.loadGeneration++;
 await p.request(origin+'/api/v1/vlc/load',{body:'{}'});
 assert.deepEqual(commands.map(c=>c.boot),['one','two']);
 epoch='some-other-client';owner.loadGeneration++;
 await assert.rejects(p.request(origin+'/api/v1/vlc/load',{body:'{}'}),/revoked/);
 assert.equal(commands.length,2);
});
