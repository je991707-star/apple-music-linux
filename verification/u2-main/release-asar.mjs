// Explicit local production-ASAR qualification artifact, not an installer.
import fs from 'node:fs';import path from 'node:path';import {createRequire} from 'node:module';import crypto from 'node:crypto';
const require=createRequire('/var/home/h97m/Projects/adam/electron/package.json');const asar=require('@electron/asar');
const root=process.cwd(),lab=root+'/.u2-lab/u2-4-20261102-run1',suffix=process.argv[2]||'v3',out=lab+'/release-asar-input-'+suffix;fs.mkdirSync(out);
const pkg=JSON.parse(fs.readFileSync(root+'/electron/package.json'));if(!pkg.build.files.includes('!test/**'))throw Error('tests not excluded');
for(const entry of fs.readdirSync(root+'/electron',{withFileTypes:true})){
 if(entry.isFile()&&/\.(mjs|cjs|js|css|html|png|ico|svg|json)$/.test(entry.name))fs.copyFileSync(root+'/electron/'+entry.name,out+'/'+entry.name);
}
for(const name of ['src','assets'])if(fs.existsSync(root+'/electron/'+name))fs.cpSync(root+'/electron/'+name,out+'/'+name,{recursive:true});
for(const name of ['engine-bundle.js','engine-sse-bundle.js','smart-cache-bundle.js'])fs.copyFileSync(lab+'/'+name,out+'/'+name);
fs.copyFileSync(root+'/electron/src/vision-glass.js',out+'/vision-bundle.js');
await asar.createPackage(out,lab+'/production-code-'+suffix+'.asar');
const entries=asar.listPackage(lab+'/production-code-'+suffix+'.asar');
if(entries.some(n=>/\/(test|verification)\//.test(n)||/observer|private-bus|start-inside/.test(n)))throw Error('private test code in ASAR');
for(const marker of ['offline-synthetic-key','offline-synthetic-main-header','u2-local-fixture-client-only']){
 for(const file of entries.filter(n=>/\.(mjs|cjs|js|json)$/.test(n)))if(asar.extractFile(lab+'/production-code-'+suffix+'.asar',file.replace(/^\//,'')).toString().includes(marker))throw Error('synthetic key in ASAR');
}
const main=asar.extractFile(lab+'/production-code-'+suffix+'.asar','main.mjs');if(!main.equals(fs.readFileSync(root+'/electron/main.mjs')))throw Error('Main archive differs');
fs.writeFileSync(lab+'/release-asar-results.json',JSON.stringify({scope:'actual production-code ASAR; not full electron-builder installer; optional dependencies not packaged in this inspection artifact',entries:entries.length,mainEntry:pkg.main,mainHash:crypto.createHash('sha256').update(main).digest('hex'),testCodeExcluded:true,syntheticStringsExcluded:true,asarHash:crypto.createHash('sha256').update(require('original-fs').readFileSync(lab+'/production-code-'+suffix+'.asar')).digest('hex')},null,2));
console.log('production code ASAR checked',entries.length);
