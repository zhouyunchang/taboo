// Real CLI subprocesses and a long-lived MCP stdio process against the Go API.
import assert from 'node:assert/strict';
import {spawn} from 'node:child_process';
import fs from 'node:fs';
import {createInterface} from 'node:readline';

const env = {...process.env};
const external = env.TABOO_AUTH_MODE === 'oauth';
const cli = async (args, overrides = {}, success = true) => {
 const child = spawn(process.execPath, ['apps/cli/taboo.js', ...args], {env:{...env,...overrides}});
 let out='',err=''; child.stdout.on('data',b=>out+=b); child.stderr.on('data',b=>err+=b);
 const code = await new Promise((resolve,reject)=>{child.on('error',reject);child.on('exit',resolve)});
 assert.equal(code===0,success,`CLI ${args[0]} exit=${code} ${err}`);
 assert(!err.includes('consumer-fixture-value') && !err.includes(env.TABOO_CLIENT_SECRET), 'credential in CLI error');
 return out;
};
if (external) {
 await cli(['login','--oauth']);
 assert(!fs.existsSync(env.TABOO_SESSION_FILE), 'external credential persisted');
} else {
 await cli(['login','--client-id',env.TABOO_CLIENT_ID,'--client-secret',env.TABOO_CLIENT_SECRET]);
}
assert((await cli(['list'])).includes('CONSUMER_SECRET'));
assert.equal((await cli(['get','CONSUMER_SECRET'])).trim(),'consumer-fixture-value');
assert((await cli(['run','--',process.execPath,'-e','process.stdout.write(process.env.CONSUMER_SECRET)'])).includes('consumer-fixture-value'));
await cli(['list'],{TABOO_PROJECT_ID:'project-b'},false);
if(external) {
 await cli(['get','CONSUMER_SECRET'],{TABOO_OAUTH_SCOPES:'read'},false);
 await cli(['export'],{TABOO_OAUTH_SCOPES:'read'},false);
 await cli(['list'],{TABOO_ORG:'b'},false);
 await cli(['list'],{TABOO_OAUTH_ISSUER:env.TABOO_TEST_OTHER_ISSUER},false);
 await cli(['list'],{TABOO_CLIENT_SECRET:'invalid'},false);
}
const child=spawn(env.TABOO_TEST_MCP_BIN,[],{env,stdio:['pipe','pipe','pipe']});
let stderr='';child.stderr.on('data',b=>stderr+=b);
const pending=new Map();let sequence=0;
const lines=createInterface({input:child.stdout});
lines.on('line',line=>{const response=JSON.parse(line);const p=pending.get(response.id);if(p){clearTimeout(p.timer);pending.delete(response.id);p.resolve(response)}});
const rpc=(method,params)=>new Promise((resolve,reject)=>{
 const id=++sequence;
 const timer=setTimeout(()=>{pending.delete(id);reject(Error('MCP response timeout'))},10000);
 pending.set(id,{resolve,timer});child.stdin.write(JSON.stringify({jsonrpc:'2.0',id,method,params})+'\n');
});
const tool=(name,args={})=>rpc('tools/call',{name,arguments:args});
try {
 assert.equal((await rpc('initialize',{protocolVersion:'2025-06-18',capabilities:{},clientInfo:{name:'public-fixture',version:'1'}})).result.serverInfo.name,'taboo');
 const list=await tool('secrets.list');assert(!list.result.isError);assert(!JSON.stringify(list).includes('consumer-fixture-value'));
 const masked=await tool('secrets.get',{key:'CONSUMER_SECRET'});assert(!masked.result.isError);assert(JSON.stringify(masked).includes('••••'));assert(!JSON.stringify(masked).includes('consumer-fixture-value'));
 const revealed=await tool('secrets.get',{key:'CONSUMER_SECRET',reveal:true});assert(!revealed.result.isError);assert(JSON.stringify(revealed).includes('consumer-fixture-value'));
 assert.equal((await tool('secrets.list',{env:'prod'})).result.isError,true);
 const revoke=await fetch(env.TABOO_SERVER+env.TABOO_TEST_REVOKE_PATH,{method:'POST',headers:{Authorization:'Bearer '+env.TABOO_TEST_OWNER_TOKEN}});
 assert.equal(revoke.status,200);
 assert.equal((await tool('secrets.list')).result.isError,true,'cached token accepted after local revocation');
 await cli(['list'],{},false);
 assert(!stderr.includes('consumer-fixture-value') && !stderr.includes(env.TABOO_CLIENT_SECRET));
} finally { lines.close();child.kill();for(const p of pending.values())clearTimeout(p.timer); }
console.log('CLI and MCP public consumer contracts passed');
