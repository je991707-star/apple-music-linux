// Private build only: separate local queue, never replaces/authorizes real MusicKit.
export function createFixedQueue(){
 const durations={'alac-a':4000,'alac-b':4000,'alac-tail':4500,'alac-fail':12000,'alac-fail-short':4000,'alac-delay':4000,'alac-404':4000};
 const audio=document.createElement('audio');audio.hidden=true;document.body.append(audio);
 const listeners=new Map();const host={isAuthorized:false,authorizationStatus:0,storefrontId:'us',developerToken:'',musicUserToken:'',repeatMode:0,shuffleMode:0,nowPlayingItem:null,queue:{items:[],position:0,playbackMode:0},currentPlaybackTime:0,currentPlaybackDuration:0,playbackState:0,
 addEventListener(name,fn){const set=listeners.get(name)||new Set();set.add(fn);listeners.set(name,set)},removeEventListener(name,fn){listeners.get(name)?.delete(fn)},async play(){},pause(){},async seekToTime(){},async setQueue(){throw Error('Fixed local queue only')},
 async changeToMediaAtIndex(i){if(!Number.isInteger(i)||i<0||i>=this.queue.items.length)throw Error('Local index denied');this.queue.position=i;this.nowPlayingItem=this.queue.items[i];for(const fn of listeners.get('nowPlayingItemDidChange')||[])await fn(new Event('nowPlayingItemDidChange'))},async skipToNextItem(){throw Error('Account/station queues unavailable')}};
 function seed(ids){if(!Array.isArray(ids)||ids.length<1||ids.length>3||ids.some(id=>!Object.hasOwn(durations,id)))throw Error('Fixed queue required');host.queue.items=ids.map(id=>({id,type:'songs',playParams:{id,catalogId:id,kind:'song'},attributes:{name:'Offline synthetic '+id,artistName:'Generated local signal',durationInMillis:durations[id],playParams:{id,catalogId:id,kind:'song'}}}));host.queue.position=0;host.nowPlayingItem=null;}
 return {audio,host,seed};
}
