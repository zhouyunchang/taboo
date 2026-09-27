// Exercises the shipped image with public disposable fixture material only.
import {execFileSync} from 'node:child_process';
import assert from 'node:assert/strict';
const image=process.env.TABOO_TEST_IMAGE||'taboo:standard-federation-test';
const stem=`taboo-recovery-${process.pid}`, volume=stem+'-data', backup=stem+'-backup';
const port=process.env.TABOO_TEST_CONTAINER_PORT||'17110';
const base=`http://127.0.0.1:${port}`;
const docker=(...args)=>execFileSync('docker',args,{encoding:'utf8',stdio:['pipe','pipe','pipe']}).trim();
const pause=ms=>new Promise(r=>setTimeout(r,ms));
let token,pid;
async function request(path,method='GET',body){
 const res=await fetch(base+'/api/v1'+path,{method,headers:{...(token?{Authorization:`Bearer ${token}`} :{}),...(body?{'Content-Type':'application/json'}:{})},body:body?JSON.stringify(body):undefined});
 const data=await res.json();assert.ok(res.ok,`${method} ${path}: ${res.status}`);return data;
}
async function start(vol,selectedImage=image){
 docker('run','-d','--name',stem,'-p',`127.0.0.1:${port}:7100`,'-v',`${vol}:/data`,'-e','TABOO_JWT_SECRET=public-fixture-session-signing-key',selectedImage);
 for(let i=0;i<60;i++){try{if((await fetch(base+'/api/v1/readyz')).ok)return;}catch{} await pause(500);}
 throw new Error('container not ready');
}
function stop(){docker('stop',stem);docker('rm',stem);}
try{
 docker('volume','create',volume);docker('volume','create',backup);
 await start(volume,process.env.TABOO_TEST_LEGACY_IMAGE||image);
 const account=await request('/auth/register','POST',{email:'recovery@example.test',password:'fixture-password',name:'Recovery fixture'});token=account.tokens.access;
 const me=await request('/me');pid=(await request(`/orgs/${me.orgs[0].slug}/projects`)).projects[0].id;
 assert.equal((await request(`/projects/${pid}/secrets?env=dev`,'POST',{key:'RECOVER',value:'public-recovery-fixture',expectedVersion:0})).version,1);
 stop();
 // Quiesced volume snapshot includes DB, root material and durable export files.
 docker('run','--rm','--user','0','--entrypoint','sh','-v',`${volume}:/source:ro`,'-v',`${backup}:/backup`,image,'-c','cp -a /source/. /backup/');
 await start(volume);assert.equal((await request(`/projects/${pid}/secrets/RECOVER?env=dev`)).value,'public-recovery-fixture');stop();
 await start(backup);const restored=await request(`/projects/${pid}/secrets/RECOVER?env=dev`);assert.equal(restored.value,'public-recovery-fixture');assert.equal(restored.version,1);stop();
 const verify=JSON.parse(docker('run','--rm','--entrypoint','taboo-migrate','-v',`${backup}:/data`,image,'verify','-data-dir','/data'));assert.equal(verify.verified,true);
 assert.throws(()=>docker('run','--rm','--entrypoint','taboo-migrate','-e',`TABOO_MASTER_KEY=${'01'.repeat(32)}`,'-v',`${backup}:/data`,image,'verify','-data-dir','/data'));
 docker('run','--rm','--entrypoint','sh','-v',`${backup}:/data`,image,'-c','rm /data/master.key');
 assert.throws(()=>docker('run','--rm','-e','TABOO_JWT_SECRET=fixture','-v',`${backup}:/data`,image,'--check'));
 assert.equal(docker('run','--rm','--entrypoint','sh','-v',`${backup}:/data`,image,'-c','test ! -f /data/master.key && echo absent'),'absent');
 console.log('Container fresh install, restart, quiesced restore, decrypt verification and missing/wrong root rejection passed.');
}finally{
 try{docker('rm','-f',stem);}catch{}
 for(const v of [volume,backup]){try{docker('volume','rm',v);}catch{}}
}
