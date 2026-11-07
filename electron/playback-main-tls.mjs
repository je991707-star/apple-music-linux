import {createOwnSSEDecoder} from './scoped-source-policy.mjs';
import fs from 'node:fs';
import path from 'node:path';
import crypto from 'node:crypto';
import https from 'node:https';
import tls from 'node:tls';
export function privateControlDirectory(dir) {
    if (process.platform !== 'linux' || !path.isAbsolute(dir || '') || fs.realpathSync(dir) !== dir) throw Error('Private control directory required');
    const st=fs.statSync(dir);
    if (!st.isDirectory() || (st.mode & 0o777) !== 0o700 || st.uid !== process.getuid()) throw Error('Private control directory required');
    return dir;
}
export function readPlaybackDescriptor(dir, expected) {
    privateControlDirectory(dir);
    const fd=fs.openSync(path.join(dir,'playback-boot.json'),fs.constants.O_RDONLY|fs.constants.O_NOFOLLOW|fs.constants.O_NONBLOCK);
    try {
        const st=fs.fstatSync(fd);
        if (!st.isFile() || st.uid!==process.getuid() || (st.mode&0o777)!==0o600 || st.size>16384) throw Error('Invalid descriptor');
        const bytes=Buffer.alloc(16385);
        const size=fs.readSync(fd,bytes,0,bytes.length,0);
        if(size>16384)throw Error('Invalid descriptor');
        const value=JSON.parse(bytes.subarray(0,size).toString('utf8'));
        const cert=new crypto.X509Certificate(value.certificate);
        if (value.pid!==expected.pid || value.port!==expected.port || value.nonce!==expected.nonce ||
            !/^[a-f0-9]{32}$/.test(value.boot) || !/^[a-f0-9]{64}$/.test(value.pin) || !cert.ca ||
            !cert.verify(cert.publicKey) || !cert.checkIP('127.0.0.1') ||
            Date.now()<Date.parse(cert.validFrom) || Date.now()>Date.parse(cert.validTo) ||
            crypto.createHash('sha256').update(cert.raw).digest('hex')!==value.pin) throw Error('Invalid descriptor');
        return value;
    } finally { fs.closeSync(fd); }
}
export async function readPinnedPlaybackStatus(trust, token, deadline=4000) {
 const result=await sendPinnedPlaybackOperation(trust,token,{operation:"time"},deadline);
 if(result.status!==200)throw Error("Unconfirmed status");return result;
}
export async function sendPinnedPlaybackOperation(trust, token, query, deadline=4000) {
 const paths={auth:"/api/v1/main/auth",time:"/api/v1/vlc/time",source:"/api/v1/main/source",control:"/api/v1/vlc/control",load:"/api/v1/vlc/load",seek:"/api/v1/vlc/seek",pause:"/api/v1/vlc/pause",resume:"/api/v1/vlc/resume",stop:"/api/v1/vlc/stop",intent:"/api/v1/vlc/intent"};
 if(!Object.hasOwn(paths,query.operation))throw Error("Invalid fixed Main transport");
 const payload=query.body===undefined?undefined:JSON.stringify(query.body);
 if(payload&&Buffer.byteLength(payload)>4096)throw Error("Request limit");
 const headers={"X-Api-Token":token};
 if(query.source)headers["X-Main-Source"]=query.source;
 if(query.issuer)headers["X-Main-Issuer"]=query.issuer;
 if(payload){headers["Content-Type"]="application/json";headers["Content-Length"]=Buffer.byteLength(payload)}
    // Agent does not release the socket to HTTP until BOTH TLS validation and
    // private pin verification succeeded. Tokens cannot reach a replaced port.
    const agent=createPinnedPlaybackAgent(trust);
    try { return await new Promise((resolve,reject)=>{
        const req=https.request({hostname:'127.0.0.1',port:trust.port,path:paths[query.operation],method:payload?'POST':'GET',agent,
            headers,signal:query.signal?AbortSignal.any([AbortSignal.timeout(deadline),query.signal]):AbortSignal.timeout(deadline)},res=>{
            const chunks=[];let bytes=0;
            res.on('data',b=>{bytes+=b.length;if(bytes>16384){req.destroy(Error('Response limit'));return}chunks.push(b);});
            res.once('error',reject);res.once('aborted',()=>reject(Error('Truncated response')));
            res.once('end',()=>{try{
                const text=Buffer.concat(chunks).toString();
                if(text.includes(token))throw Error('Reflected token');
                if(res.statusCode>=300&&res.statusCode<400)throw Error('Redirect forbidden');
                if(res.statusCode>=400){const body={error:'Engine operation rejected',mayHaveApplied:true};if(query.operation==='source'&&query.body?.operation==='volume'){body.backend='unconfirmed';try{const v=JSON.parse(text);if(['native-unidentified','native-pulse','callback-memory','unavailable'].includes(v.backend))body.backend=v.backend;if(v.stage==='output-unsupported'&&v.backend==='callback-memory'&&v.mayHaveApplied===false)body.mayHaveApplied=false;}catch{}}resolve({status:res.statusCode,body});return}
                const data=JSON.parse(text);
                if(JSON.stringify(data).includes(token)||[query.source,query.issuer].filter(Boolean).some(v=>JSON.stringify(data).includes(v)))throw Error('Reflected authority');
                resolve({status:res.statusCode,body:data});
            }catch{reject(Error('Invalid status response'));}});
        });
        req.once('error',reject);req.end(payload);
    }); } finally {agent.destroy();}
}

function createPinnedPlaybackAgent(trust){
    const agent=new https.Agent({maxCachedSessions:0});
    agent.createConnection=(options,done)=>{
        let finished=false;
        const socket=tls.connect({...options,ca:trust.certificate,rejectUnauthorized:true,checkServerIdentity:tls.checkServerIdentity});
        const finish=err=>{if(finished)return;finished=true;done(err,err?undefined:socket);};
        socket.setTimeout(2500,()=>{finish(Error('TLS deadline'));socket.destroy();});
        socket.once('error',finish);
        socket.once('secureConnect',()=>{
            const raw=socket.getPeerCertificate().raw;
            if(!raw||crypto.createHash('sha256').update(raw).digest('hex')!==trust.pin){finish(Error('TLS pin'));socket.destroy();}
            else{socket.setTimeout(0);finish(null);}
        });
    };
return agent;
}

export function openPinnedOwnEvents(trust,token,query,onEvent,onEnd){
 return new Promise((resolve,reject)=>{
  const agent=createPinnedPlaybackAgent(trust);let settled=false,ended=false;
  const finish=()=>{if(ended)return;ended=true;clearTimeout(deadline);agent.destroy();if(settled)onEnd();};
  const req=https.request({hostname:'127.0.0.1',port:trust.port,path:'/api/v1/main/source/events',method:'POST',agent,headers:{'X-Api-Token':token,'X-Main-Source':query.source,'Content-Type':'application/json','Accept':'text/event-stream'}},res=>{
   if(res.statusCode!==200||res.headers['content-type']!=='text/event-stream'){req.destroy();reject(Error('Own SSE rejected'));finish();return}
   clearTimeout(deadline);settled=true;resolve({cancel(){req.destroy();finish()}});
   const decoder=createOwnSSEDecoder(value=>{if(JSON.stringify(value).includes(token)||JSON.stringify(value).includes(query.source))throw Error('Reflected SSE authority');onEvent(value)});
   res.setEncoding('utf8');res.setTimeout(15000,()=>req.destroy());res.on('data',chunk=>{try{decoder(chunk)}catch{req.destroy()}});res.once('error',finish);res.once('end',finish);res.once('aborted',finish);
  });
  const deadline=setTimeout(()=>{req.destroy();reject(Error('Own SSE deadline'));finish()},4000);
  req.once('error',()=>{clearTimeout(deadline);if(!settled)reject(Error('Own SSE unconfirmed'));finish()});req.end(JSON.stringify(query.body));
 });
}
