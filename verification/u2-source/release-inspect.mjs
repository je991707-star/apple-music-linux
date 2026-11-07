// Production-code archive qualification only; never installs Adam.
import fs from 'node:fs';import path from 'node:path';import crypto from 'node:crypto';import {createRequire} from 'node:module';
const require=createRequire('/var/home/h97m/Projects/adam/electron/package.json'),asar=require('@electron/asar'),esbuild=require('esbuild');
const root=path.resolve(import.meta.dirname,'../..'),lab=path.resolve(process.argv[2]);if(!lab.match(new RegExp('^'+root+'/.u2-lab/u2-(?:5|5a|6|7|8|9)-')))throw Error('New lab required');
const suffix=process.argv[3]||'v1';if(!/^[a-z0-9-]+$/.test(suffix))throw Error('Invalid artifact suffix');
const out=lab+'/production-asar-input-'+suffix;fs.mkdirSync(out);const pkg=JSON.parse(fs.readFileSync(root+'/electron/package.json'));
if(pkg.main!=='main.mjs'||!pkg.build.files.includes('!test/**'))throw Error('Production entry/exclusion changed');
for(const p of fs.readdirSync(root+'/electron',{withFileTypes:true}))if(p.isFile()&&/\.(mjs|cjs|js|css|html|png|ico|svg|json)$/.test(p.name))fs.copyFileSync(root+'/electron/'+p.name,out+'/'+p.name);
for(const n of ['src','assets'])if(fs.existsSync(root+'/electron/'+n))fs.cpSync(root+'/electron/'+n,out+'/'+n,{recursive:true});
for(const [entry,name]of [['engine-playback.js','engine-bundle.js'],['engine-sse.js','engine-sse-bundle.js'],['smart-cache.js','smart-cache-bundle.js']])await esbuild.build({entryPoints:[root+'/electron/src/'+entry],bundle:true,format:'iife',outfile:out+'/'+name,logLevel:'warning'});
fs.copyFileSync(root+'/electron/src/vision-glass.js',out+'/vision-bundle.js');
const archive=lab+'/production-code-'+suffix+'.asar';await asar.createPackage(out,archive);const entries=asar.listPackage(archive),bad=['u2-fixed-synthetic-v1','__u2Controller','createFixedQueue','ADAM_MAIN_FIXTURE_DIR','u2-observer.mjs','Fixed synthetic A','Fixed synthetic B','b-distinct.m4a','alac-a','alac-b','ADAM_MAIN_PRIVATE_OUTPUT','U2_PRIVATE_PULSE_QUALIFICATION','NewPrivatePulseTestPlayer'];
if(entries.some(n=>/\/(test|verification)\//.test(n)||/observer|private-bus|start-inside/.test(n)))throw Error('Test file in archive');
for(const file of entries.filter(n=>/\.(mjs|cjs|js|json)$/.test(n))){const text=asar.extractFile(archive,file.replace(/^\//,'')).toString();if(bad.some(s=>text.includes(s)))throw Error('Test marker in archive '+file)}
for(const file of ['main.mjs','preload.cjs','playback-main-source.mjs'])if(!asar.extractFile(archive,file).equals(fs.readFileSync(root+'/electron/'+file)))throw Error('Production source differs '+file);
const goList=fs.existsSync(lab+'/production-go-list-final.json')?'production-go-list-final.json':'production-go-list.json';
const engineName=fs.existsSync(lab+'/engine-release-no-fixture-final2')?'engine-release-no-fixture-final2':fs.existsSync(lab+'/engine-release-no-fixture-final')?'engine-release-no-fixture-final':'engine-release-no-fixture';
const go=JSON.parse(fs.readFileSync(lab+'/'+goList));if(go.GoFiles.includes('main_source_fixture.go')||!go.IgnoredGoFiles.includes('main_source_fixture.go'))throw Error('Fixture compiled in production');
const engine=fs.readFileSync(lab+'/'+engineName);if(bad.some(s=>engine.includes(Buffer.from(s))))throw Error('Fixture marker in release engine');
const sha=b=>crypto.createHash('sha256').update(b).digest('hex');
fs.writeFileSync(lab+'/release-exclusion-'+suffix+'.json',JSON.stringify({status:'PASS',scope:'Actual production-code ASAR and default no-DRM Engine executable; NOT a complete installer/native-DRM release qualification',entries:entries.length,main:pkg.main,fixtureGoFileIgnored:true,testMarkersExcluded:bad,productionMainByteExact:true,asarSHA256:sha(require('original-fs').readFileSync(archive)),engineSHA256:sha(engine)},null,2));console.log('Production ASAR / executable exclusion PASS');
