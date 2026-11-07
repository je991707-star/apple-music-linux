import {test} from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import {createRequire} from 'node:module';
import {createAccountSheet} from '../../src/engine/account-sheet.js';

// Fixed, synthetic status only. No auth provider, network, wallet or credential.
test('disabled Engine account sheet never creates password/challenge input or invokes callbacks',()=>{
 let dom=0,auth=0,done=0,cancelled=0;
 const doc=new Proxy({},{get(){dom++;throw Error('disabled credential dialog accessed DOM')}});
 const sheet=createAccountSheet({enabled:false,doc,signIn(){auth++;},submitCode(){auth++;},onDone(){done++;},onCancel(){cancelled++;}});
 sheet.openSignIn();
 for(const snapshot of [
  {state:{authentication:'challenging'},challenge:{type:'two_factor',id:'fixed-test'}},
  {state:{authentication:'logged_in',process:'running'},capabilities:{cbcs:true}},
  {state:{authentication:'failed'}},
 ])sheet.onSnapshot(snapshot);
 sheet.close();sheet.openSignIn();assert.equal(sheet.isOpen(),false);
 assert.deepEqual({dom,auth,done,cancelled},{dom:0,auth:0,done:0,cancelled:0});
});
test('Legacy default sheet retains deferred user-triggered DOM path',()=>{
 const sheet=createAccountSheet({doc:{createElement(){throw Error('user-triggered DOM boundary')}},signIn(){throw Error('must not authenticate')}});
 assert.equal(sheet.isOpen(),false);assert.throws(()=>sheet.openSignIn(),/user-triggered DOM boundary/);
});
const require=createRequire(process.env.ADAM_TEST_PARSER_BASE||import.meta.url);
const {parse}=require('espree');
const source=fs.readFileSync(new URL('../../src/engine-playback.js',import.meta.url),'utf8');
const ast=parse(source,{ecmaVersion:'latest',sourceType:'module'});
const node=ast.body.find(n=>n.type==='FunctionDeclaration'&&n.id.name==='_accountSheet');
const declaration=source.slice(node.start,node.end);
for(const mode of [{bridge:true,flag:false},{bridge:false,flag:true},{bridge:false,flag:false}])test(`production account caller gate bridge=${mode.bridge} flag=${mode.flag}`,()=>{
 let options;
 const context=vm.createContext({_accountSheetInstance:null,window:{adamBridge:{orderedPlaybackEnabled:mode.bridge},_adamVLCOrdered:mode.flag},createAccountSheet:o=>{options=o;return{};}});
 vm.runInContext(declaration+';_accountSheet()',context);
 assert.equal(options.enabled,!mode.bridge&&!mode.flag);
});
test('production Ordered account section offers no unsupported sign-in/logout/device controls',()=>{
 let section;function walk(n){if(!n||typeof n!=='object')return;if(n.type==='FunctionDeclaration'&&n.id.name==='buildAccountSection')section=n;for(const v of Object.values(n))if(Array.isArray(v))v.forEach(walk);else if(v&&typeof v==='object')walk(v)}walk(ast);
 assert(section);const body={},wrap={};const context=vm.createContext({window:{adamBridge:{orderedPlaybackEnabled:true}},makeSection:()=>({body,wrap}),document:{createElement(){throw Error('unqualified account controls rendered')}},makeBtn(){throw Error('unqualified button created')}});
 vm.runInContext(source.slice(section.start,section.end)+';buildAccountSection({capabilities:{cbcs:true}},()=>{})',context);
 assert.match(body.textContent,/unavailable/);assert.match(body.textContent,/not been qualified/);
});
