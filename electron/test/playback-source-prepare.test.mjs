import {test} from 'node:test';import assert from 'node:assert/strict';import fs from 'node:fs';import vm from 'node:vm';import {createRequire}from'node:module';
const require=createRequire(process.env.ADAM_TEST_PARSER_BASE||'/var/home/h97m/Projects/adam/electron/package.json'),acorn=require('acorn');
const source=fs.readFileSync(new URL('../src/engine-playback.js',import.meta.url),'utf8');const ast=acorn.parse(source,{ecmaVersion:'latest',sourceType:'module'});const declaration=ast.body.find(n=>n.type==='FunctionDeclaration'&&n.id.name==='_resolveSession');
test('actual session acquisition adopts confirmed control epoch before new Load ownership is captured',async()=>{
 const calls=[],session={sessionId:'owned',assetId:'fixed'};
 const context=vm.createContext({_generation:7,_currentAssetId:'fixed',_orderedReopenSession:null,_vlcCommands:{enabled:true,prepare:async id=>{calls.push('claim:'+id);return{boot:'boot',epoch:'new-epoch'}}},_vlcObserveControl:c=>calls.push('epoch:'+c.epoch),window:{adamBridge:{orderedPlayback:async()=>({status:200,body:session})}}});
 vm.runInContext(source.slice(declaration.start,declaration.end),context);
 assert.equal(await context._resolveSession({},'fixed','us',{}),session);
 assert.deepEqual(calls,['claim:owned','epoch:new-epoch']);
});
