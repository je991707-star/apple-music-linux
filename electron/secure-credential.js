import {createAccountSheet} from './src/engine/account-sheet.js';
const api=window.adamPrivateInput;
if(api){
 let challenge=null,closed=false,polling=false,mutating=false,revision=0,timer=null;
 // This counter only discards stale Renderer views. It is not an Account Epoch.
 async function mutate(fn){
  if(closed||mutating)return {ok:false,error:'Private input unavailable'};
  mutating=true;revision++;
  try{
   const result=await fn();
   return closed?{ok:false,error:'Private input unavailable'}:result;
  }catch{return {ok:false,error:'Private input unavailable'}}
  finally{mutating=false;revision++}
 }
 const sheet=createAccountSheet({synthetic:true,
  signIn:(email,password)=>mutate(()=>api.credentials(email,password)),
  submitCode:async reply=>{
   const id=challenge?.id;
   if(!id||closed||mutating)return false;
   challenge=null;
   return (await mutate(()=>api.challenge(id,reply)))?.ok===true;
  },
  onCancel:()=>stop(true),
 });
 function stop(cancel=false){
  if(closed)return;
  closed=true;revision++;challenge=null;
  sheet.close();clearInterval(timer);
  if(cancel)return Promise.resolve().then(()=>api.cancel()).catch(()=>{});
 }
 sheet.openSignIn();
 timer=setInterval(async()=>{
  if(closed||polling||mutating)return;
  polling=true;const captured=revision;
  try{
   const v=await api.view();
   if(closed||mutating||revision!==captured)return;
   if(!v.ok){await stop(true);return}
   challenge=v.challenge;
   sheet.onSnapshot({state:{authentication:v.phase==='challenge_required'?'challenging':v.phase},challenge:challenge?{type:'two_factor',id:challenge.id}:null});
  }catch{await stop(true)}
  finally{polling=false}
 },500);
 window.addEventListener('pagehide',()=>stop(),{once:true});
}
