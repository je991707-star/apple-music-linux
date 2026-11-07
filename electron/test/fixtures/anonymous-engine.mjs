// Anonymous Node child only. No Native runtime, account, network or audio.
import fs from 'node:fs';import path from 'node:path';
const config=fs.readFileSync(path.join(process.cwd(),'config.yaml'),'utf8');
const files=JSON.parse(config.match(/^drm-base-dir: (.+)$/m)[1]);
const mode=process.argv[2];
process.on('SIGTERM',()=>{if(mode==='ignore-term')process.send?.({kind:'term-observed'});else process.exit(0)});
process.on('message',m=>{if(m?.kind==='crash')process.exit(3);if(m?.kind==='exit')process.exit(0)});
process.send?.({kind:'fixture-ready',hasSyntheticAccountFile:fs.existsSync(path.join(files,'MUSIC_TOKEN'))});
setInterval(()=>{},1000);
