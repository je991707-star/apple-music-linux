import {test} from 'node:test';import assert from 'node:assert/strict';import {createPlaybackCommands} from '../src/engine/playback-commands.js';
test('new private source needs a fresh control claim rather than inheriting old grant epoch',async()=>{
 let session='a',generation=1,claims=0,epoch='';const reply=body=>({ok:true,status:200,json:async()=>body});
 const commands=createPlaybackCommands({enabled:true,origin:'https://127.0.0.1:34725',scope:()=>({sessionId:session,loadGeneration:generation}),fetch(){throw Error('raw transport denied')},orderedFetch:async(url,options)=>{
 const type=new URL(url).pathname.split('/').pop(),body=options.body?JSON.parse(options.body):{};
 if(type==='time')return reply({control:{boot:'boot',epoch}});
 if(type==='control'){claims++;epoch='epoch-'+claims;return reply({boot:'boot',epoch,sessionId:body.sessionId})}
 return reply({ack:'media-committed',command:body.command});
 }});
 await commands.request('https://127.0.0.1:34725/api/v1/vlc/load',{body:JSON.stringify({sessionId:session})});
 session='b';generation++;await commands.request('https://127.0.0.1:34725/api/v1/vlc/load',{body:JSON.stringify({sessionId:session})});
 assert.equal(claims,2);
});
