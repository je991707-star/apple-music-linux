import {test} from 'node:test';import assert from 'node:assert/strict';import fs from 'node:fs';import os from 'node:os';import path from 'node:path';import https from 'node:https';import crypto from 'node:crypto';import {execFileSync} from 'node:child_process';
import {readPlaybackDescriptor,readPinnedPlaybackStatus} from '../playback-main-tls.mjs';
const linuxTest=(name,fn)=>test(name,{skip:process.platform!=='linux'},fn);
function cert(t,san='IP:127.0.0.1'){const dir=fs.mkdtempSync(path.join(os.tmpdir(),'u2-tls-'));fs.chmodSync(dir,0o700);t.after(()=>fs.rmSync(dir,{recursive:true,force:true}));execFileSync('openssl',['req','-x509','-newkey','ec','-pkeyopt','ec_paramgen_curve:P-256','-nodes','-keyout',dir+'/key','-out',dir+'/cert','-days','1','-subj','/CN=U2 offline test','-addext','subjectAltName='+san,'-addext','basicConstraints=critical,CA:TRUE'],{stdio:'ignore'});const certificate=fs.readFileSync(dir+'/cert','utf8'),key=fs.readFileSync(dir+'/key');return{dir,certificate,key,pin:crypto.createHash('sha256').update(new crypto.X509Certificate(certificate).raw).digest('hex')};}
async function server(t,c,fn){const s=https.createServer(c,fn);await new Promise(r=>s.listen(0,'127.0.0.1',r));t.after(()=>new Promise(r=>s.close(r)));return{...c,port:s.address().port};}
const token='offline-synthetic-main-header';
linuxTest('real pinned CA TLS status succeeds; redirects/errors/token reflection never cross IPC',async t=>{const c=cert(t);let mode='ok';const s=await server(t,{cert:c.certificate,key:c.key},(q,r)=>{assert.equal(q.headers['x-api-token'],token);if(mode==='redirect'){r.writeHead(307,{Location:'https://example.org/'+token});r.end();}else if(mode==='reflect')r.end(JSON.stringify({error:token}));else if(mode==='truncated'){r.writeHead(200,{'Content-Length':10000});r.write('{}');r.destroy();}else if(mode==='large')r.end('x'.repeat(17000));else r.end(JSON.stringify({control:{boot:'test'}}));});const trust={...c,port:s.port};assert.equal((await readPinnedPlaybackStatus(trust,token)).status,200);for(mode of ['redirect','reflect','truncated','large'])await assert.rejects(readPinnedPlaybackStatus(trust,token));});
linuxTest('wrong CA or pin is rejected before any HTTP token is sent',async t=>{const a=cert(t),b=cert(t);let received=0;const s=await server(t,{cert:a.certificate,key:a.key},(q,r)=>{received++;r.end('{}');});await assert.rejects(readPinnedPlaybackStatus({...b,port:s.port},token));await assert.rejects(readPinnedPlaybackStatus({...a,pin:b.pin,port:s.port},token));assert.equal(received,0);});
linuxTest('wrong hostname and bounded response wait reject without event-loop blocking',async t=>{const a=cert(t,'DNS:wrong.invalid');let received=0;const s=await server(t,{cert:a.certificate,key:a.key},(q,r)=>{received++;r.end('{}');});await assert.rejects(readPinnedPlaybackStatus({...a,port:s.port},token));assert.equal(received,0);const good=cert(t);const stall=await server(t,{cert:good.certificate,key:good.key},()=>{});let tick=false;setTimeout(()=>tick=true,20);await assert.rejects(readPinnedPlaybackStatus({...good,port:stall.port},token,100));assert.equal(tick,true);});
linuxTest('private descriptor validates nonce/PID/port/pin/cert/file permissions and rejects symlink',t=>{const a=cert(t);const d={...a,boot:'a'.repeat(32),pid:123,port:32200,nonce:'b'.repeat(64)};const file=a.dir+'/playback-boot.json';const save=()=>{fs.writeFileSync(file,JSON.stringify(d),{mode:0o600});fs.chmodSync(file,0o600);};save();const expected={pid:123,port:32200,nonce:d.nonce};assert.equal(readPlaybackDescriptor(a.dir,expected).boot,d.boot);for(const e of [{...expected,pid:124},{...expected,nonce:'stale'},{...expected,port:32201}])assert.throws(()=>readPlaybackDescriptor(a.dir,e));fs.chmodSync(file,0o644);assert.throws(()=>readPlaybackDescriptor(a.dir,expected));fs.unlinkSync(file);fs.symlinkSync(a.dir+'/cert',file);assert.throws(()=>readPlaybackDescriptor(a.dir,expected));});
linuxTest('JSON escaped token reflection is rejected after decoding too',async t=>{
 const c=cert(t);const encoded=[...token].map(ch=>'\\u'+ch.charCodeAt(0).toString(16).padStart(4,'0')).join('');
 const s=await server(t,{cert:c.certificate,key:c.key},(q,r)=>r.end('{"control":{"boot":"test","epoch":"'+encoded+'"}}'));
 await assert.rejects(readPinnedPlaybackStatus({...c,port:s.port},token));
});
linuxTest('a substituted FIFO descriptor is rejected without blocking Main file I/O',async t=>{
 const {spawn}=await import('node:child_process');const dir=fs.mkdtempSync(path.join(os.tmpdir(),'u2-fifo-'));fs.chmodSync(dir,0o700);t.after(()=>fs.rmSync(dir,{recursive:true,force:true}));
 execFileSync('mkfifo',['-m','600',dir+'/playback-boot.json']);
 const moduleURL=new URL('../playback-main-tls.mjs',import.meta.url).href;
 const code=`import{readPlaybackDescriptor}from ${JSON.stringify(moduleURL)};try{readPlaybackDescriptor(${JSON.stringify(dir)},{pid:123,port:33300,nonce:'test'});process.exitCode=2}catch{process.exitCode=0}`;
 const result=await new Promise(resolve=>{
  const child=spawn(process.execPath,['--input-type=module','-e',code],{env:{...process.env,ELECTRON_RUN_AS_NODE:'1'},stdio:'ignore'});let timedOut=false;
  const limit=setTimeout(()=>{timedOut=true;child.kill('SIGKILL')},1000);
  child.once('close',(code,signal)=>{clearTimeout(limit);resolve({code,signal,timedOut,exitObserved:true})});
 });
 assert.equal(result.timedOut,false,JSON.stringify(result));assert.equal(result.code,0);
});
linuxTest('an expired otherwise matching private CA descriptor is rejected',t=>{
 const a=cert(t);
 fs.writeFileSync(a.dir+'/index','');fs.writeFileSync(a.dir+'/serial','01\n');
 fs.writeFileSync(a.dir+'/ca.cnf',`[ca]
default_ca=local
[local]
database=${a.dir}/index
serial=${a.dir}/serial
new_certs_dir=${a.dir}
certificate=${a.dir}/cert
default_md=sha256
default_days=1
policy=policy
x509_extensions=extensions
[policy]
commonName=supplied
[extensions]
basicConstraints=critical,CA:TRUE
keyUsage=digitalSignature,keyCertSign
extendedKeyUsage=serverAuth
subjectAltName=IP:127.0.0.1
`);
 execFileSync('openssl',['req','-new','-key',a.dir+'/key','-subj','/CN=U2 offline test','-out',a.dir+'/request'],{stdio:'ignore'});
 execFileSync('openssl',['ca','-selfsign','-config',a.dir+'/ca.cnf','-in',a.dir+'/request','-keyfile',a.dir+'/key','-startdate','20000101000000Z','-enddate','20000102000000Z','-out',a.dir+'/expired','-batch','-notext'],{stdio:'ignore'});
 const certificate=fs.readFileSync(a.dir+'/expired','utf8'),parsed=new crypto.X509Certificate(certificate);
 assert(parsed.ca);assert(parsed.verify(parsed.publicKey));assert.equal(parsed.checkIP('127.0.0.1'),'127.0.0.1');assert(Date.parse(parsed.validTo)<Date.now());
 const value={certificate,pin:crypto.createHash('sha256').update(parsed.raw).digest('hex'),boot:'a'.repeat(32),nonce:'b'.repeat(64),pid:123,port:33300};
 fs.writeFileSync(a.dir+'/playback-boot.json',JSON.stringify(value),{mode:0o600});
 assert.throws(()=>readPlaybackDescriptor(a.dir,{pid:value.pid,port:value.port,nonce:value.nonce}));
});
