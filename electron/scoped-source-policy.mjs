const plain=v=>v!==null&&Object.getPrototypeOf(v)===Object.prototype;
const finiteOrNull=v=>v===null||typeof v==='number'&&Number.isFinite(v)&&v>=0;
const nullableBool=v=>v===null||typeof v==='boolean';
function validIdentity(v){return plain(v)&&Object.keys(v).every(k=>['boot','epoch','sessionId','assetId','sourceGeneration','leaseId','ownershipRevision','documentGeneration','loadGeneration','commandSequence'].includes(k))&&['boot','epoch','sessionId','assetId','leaseId'].every(k=>typeof v[k]==='string'&&v[k].length<=128)&&['sourceGeneration','ownershipRevision','documentGeneration','loadGeneration','commandSequence'].every(k=>Number.isSafeInteger(v[k])&&v[k]>=0);}
export function sameSourceIdentity(a,b){return a&&b&&['boot','epoch','sessionId','assetId','sourceGeneration','leaseId','ownershipRevision','documentGeneration'].every(k=>a[k]===b[k])}
export function validateOwnMetadata(value,expected){
 const keys=['identity','codec','declaredDurationMs','sampleDurationMs','sourceComplete','sourceFailed','sourceAborted','readerEof','readerError','state','positionMs','vlcLengthMs'];
 if(!plain(value)||Object.keys(value).some(k=>!keys.includes(k))||!validIdentity(value.identity)||!sameSourceIdentity(value.identity,expected)||!['alac','aac','atmos',''].includes(value.codec)||!finiteOrNull(value.declaredDurationMs)||!finiteOrNull(value.sampleDurationMs)||!finiteOrNull(value.positionMs)||!finiteOrNull(value.vlcLengthMs)||!['unloaded','nothing','opening','buffering','playing','paused','stopped','ended','error'].includes(value.state)||!['sourceComplete','sourceFailed','sourceAborted','readerEof'].every(k=>nullableBool(value[k]))||value.readerError!==null)throw Error('Invalid own metadata');
 return value;
}
const kinds=new Set(['snapshot','playback-state','playback-status','native-terminal','source-lifecycle','volume','command-load','command-seek','command-stop','command-pause','command-resume','command-intent']);
export function validateOwnEvent(value,expected,last){
 if(!plain(value)||Object.keys(value).some(k=>!['seq','kind','identity','metadata'].includes(k))||!Number.isSafeInteger(value.seq)||value.seq<=last||!kinds.has(value.kind)||!validIdentity(value.identity)||!sameSourceIdentity(value.identity,expected)||Buffer.byteLength(JSON.stringify(value))>8192)throw Error('Invalid scoped event');
 if(value.metadata!==null)validateOwnMetadata(value.metadata,expected);
 return value;
}
export function validateFixedArtwork(v){
 if(!plain(v)||Object.keys(v).some(k=>!['mime','base64'].includes(k))||v.mime!=='image/png'||typeof v.base64!=='string'||v.base64.length>87384||!/^[A-Za-z0-9+/]*={0,2}$/.test(v.base64))throw Error('Invalid fixed artwork');
 const bytes=Buffer.from(v.base64,'base64');if(bytes.length>65536||bytes.length<24||!bytes.subarray(0,8).equals(Buffer.from([137,80,78,71,13,10,26,10])))throw Error('Invalid PNG artwork');return {mime:v.mime,base64:v.base64};
}
export function validatePublicLibrary(v){
 if(!plain(v)||Object.keys(v).some(k=>!['kind','page','items','hasMore'].includes(k))||v.kind!=='public'||!Number.isSafeInteger(v.page)||v.page<0||!Array.isArray(v.items)||v.items.length>2||typeof v.hasMore!=='boolean'||v.items.some(i=>!plain(i)||Object.keys(i).some(k=>!['id','title','durationMs'].includes(k))||typeof i.id!=='string'||!/[a-zA-Z0-9]/.test(i.id)||!/^[a-zA-Z0-9._:-]{1,128}$/.test(i.id)||typeof i.title!=='string'||i.title.length>128||!finiteOrNull(i.durationMs)))throw Error('Unqualified library DTO');return v;
}
// Incremental, bounded SSE parser. No global event names, replay or arbitrary JSON.
export function createOwnSSEDecoder(consume){let pending='';return chunk=>{
 pending+=chunk;if(Buffer.byteLength(pending)>16384)throw Error('SSE buffer limit');
 let end;while((end=pending.indexOf('\n\n'))>=0){const frame=pending.slice(0,end);pending=pending.slice(end+2);if(Buffer.byteLength(frame)>8192)throw Error('SSE frame limit');const lines=frame.split('\n');if(lines.length!==2||lines[0]!=='event: own'||!lines[1].startsWith('data: '))throw Error('Unexpected SSE fields');consume(JSON.parse(lines[1].slice(6)));}
};}
