// Remote pages receive explicit UI state, never arbitrary Main store keys.
const own=(o,k)=>Object.hasOwn(o,k);
const plain=o=>o!==null&&typeof o==='object'&&!Array.isArray(o);
const keys=(o,allowed)=>plain(o)&&Object.keys(o).every(k=>allowed.includes(k));
const text=(v,n=1024)=>typeof v==='string'&&v.length<=n;
const num=v=>typeof v==='number'&&Number.isFinite(v)&&v>=0;
const ident=v=>text(v,128)&&/^[a-zA-Z0-9._-]+$/.test(v);
const flag=v=>typeof v==='boolean'||v==='true'||v==='false';
export const UI_STORE_KEYS=Object.freeze(['resumeEnabled','discordEnabled','soundCheckEnabled','hotkeysEnabled','historyEnabled','hotkeyBindings','adam_play_history','adam_resume_state']);
const actions=['playPause','seekForward','seekBack','volUp','volDown','love'];
const historyKeys=['name','artist','artworkUrl','duration','id','catalogId','ts'];
const resumeKeys=['v','containers','containerIdx','itemIdx','shuffle','repeat','adamId','name','artist','artworkUrl','positionSec','durationSec','savedAt'];
function historyEntry(o){return keys(o,historyKeys)&&Object.entries(o).every(([k,v])=>k==='ts'?num(v):k==='artworkUrl'?(v===null||text(v,4096)):text(v,k==='duration'?32:1024))}
function jsonUI(key,value){
 if(!text(value,256*1024)||Buffer.byteLength(value,'utf8')>256*1024)return false;
 let o;try{o=JSON.parse(value)}catch{return false}
 if(key==='hotkeyBindings')return keys(o,actions)&&Object.values(o).every(v=>text(v,32)&&v.length>0);
 if(key==='adam_play_history')return Array.isArray(o)&&o.length<=50&&o.every(historyEntry);
 if(!keys(o,resumeKeys))return false;
 return Object.entries(o).every(([k,v])=>{
  if(k==='containers')return Array.isArray(v)&&v.length<=1000&&v.every(c=>keys(c,['items'])&&Array.isArray(c.items)&&c.items.length<=10000&&c.items.every(ident));
  if(k==='shuffle')return typeof v==='boolean';
  if(['v','containerIdx','itemIdx','repeat','positionSec','durationSec','savedAt'].includes(k))return num(v);
  if(k==='adamId')return ident(v);
  return v===null&&k==='artworkUrl'||text(v,k==='artworkUrl'?4096:1024);
 });
}
export function validUIStoreValue(key,value){return UI_STORE_KEYS.includes(key)&&(key.endsWith('Enabled')?flag(value):jsonUI(key,value))}
export function createPlaybackUIStore({guard,load,changed}){
 function check(key){if(typeof key!=='string'||!UI_STORE_KEYS.includes(key))throw Error('store access denied')}
 async function access(event,key,op,value){
  const scope=guard.capture(event);check(key);
  if(op==='write'&&!validUIStoreValue(key,value))throw Error('invalid UI preference');
  const store=await load();guard.assertCurrent(scope);
  if(op==='read')return own(store,key)&&validUIStoreValue(key,store[key])?store[key]:null;
  if(op==='delete')delete store[key];else store[key]=value;
  changed();
 }
 return{read:(e,k)=>access(e,k,'read'),write:(e,k,v)=>access(e,k,'write',v),delete:(e,k)=>access(e,k,'delete')};
}
