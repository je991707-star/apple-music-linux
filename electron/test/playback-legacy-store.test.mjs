import {test} from 'node:test';
import assert from 'node:assert/strict';
import {createPlaybackUIStore,UI_STORE_KEYS} from '../playback-ui-store.mjs';
const guard={capture:()=>({}),assertCurrent(){}};
const state={}; const store=createPlaybackUIStore({guard,load:async()=>state,changed(){}});
test('Legacy settings retain all five actual UI preference forms',async()=>{for(const key of UI_STORE_KEYS.filter(k=>k.endsWith('Enabled')))for(const value of [true,false,'true','false']){await store.write({},key,value);assert.equal(await store.read({},key),value)}});
test('actual history and resume serialized schemas round trip without credential access',async()=>{
 const values={hotkeyBindings:{playPause:'Space',seekForward:'ArrowRight',seekBack:'ArrowLeft',volUp:'ArrowUp',volDown:'ArrowDown',love:'l'},adam_play_history:[{name:'Synthetic',artist:'Local',artworkUrl:null,duration:'4:00',id:'123',catalogId:'123',ts:1}],adam_resume_state:{v:1,containers:[{items:['123','p.local']}],containerIdx:0,itemIdx:0,shuffle:false,repeat:0,adamId:'123',name:'Synthetic',artist:'Local',artworkUrl:null,positionSec:1,durationSec:4,savedAt:1}};
 for(const [key,data] of Object.entries(values)){const raw=JSON.stringify(data);await store.write({},key,raw);assert.equal(await store.read({},key),raw);await store.delete({},key);assert.equal(await store.read({},key),null)}
});
test('legacy identity data cannot migrate through arbitrary Renderer store keys',async()=>{for(const key of ['drm_password','MUSIC_TOKEN','session_token','auth_cookie','device_secret','refresh_token','library.key','aml_drm_password','adam_drm_password'])await assert.rejects(store.read({},key),/denied/)});
