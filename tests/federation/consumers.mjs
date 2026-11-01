// Real CLI subprocesses and a long-lived MCP stdio process against the Go API.
import assert from 'node:assert/strict';
import {spawn} from 'node:child_process';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import {createInterface} from 'node:readline';

const env = {...process.env};
const external = env.TABOO_AUTH_MODE === 'oauth';
let lastCliErr = '';
const cli = async (args, overrides = {}, success = true) => {
 const {cwd, ...envOverrides} = overrides;
 const child = spawn(process.execPath, [path.resolve('apps/cli/taboo.js'), ...args], {env:{...env,...envOverrides}, cwd});
 let out='',err=''; child.stdout.on('data',b=>out+=b); child.stderr.on('data',b=>err+=b);
 const code = await new Promise((resolve,reject)=>{child.on('error',reject);child.on('exit',resolve)});
 lastCliErr = err;
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
const writeArgs=['set','CONSUMER_SECRET','consumer-fixture-value','--idempotency-key','cli-consumer-update','--expected-version','1'];
assert((await cli(writeArgs)).includes('(v2)'));
assert((await cli(writeArgs)).includes('(v2)'));
await cli(['set','CONSUMER_SECRET','consumer-fixture-value','--idempotency-key','cli-stale-version','--expected-version','1'],{},false);
assert((await cli(['list'])).includes('CONSUMER_SECRET'));
assert.equal((await cli(['get','CONSUMER_SECRET'])).trim(),'consumer-fixture-value');
await cli(['set','CLI_REF_TARGET','resolved-target','--idempotency-key','cli-ref-target','--expected-version','0']);
await cli(['set','CLI_REF_SRC','${dev:CLI_REF_TARGET}','--idempotency-key','cli-ref-src','--expected-version','0']);
assert.equal((await cli(['get','CLI_REF_SRC'])).trim(),'resolved-target');
assert.equal((await cli(['get','CLI_REF_SRC','--raw'])).trim(),'${dev:CLI_REF_TARGET}');
assert((await cli(['run','--',process.execPath,'-e','process.stdout.write(process.env.CLI_REF_SRC||"")'])).includes('resolved-target'));
const imported = path.join(os.tmpdir(), `taboo-import-${process.pid}.env`);
fs.writeFileSync(imported, 'CLI_IMPORT=from-cli\n');
assert((await cli(['import', imported, '--preview'])).includes('preview:'));
assert((await cli(['import', imported, '--strategy', 'skip', '--idempotency-key', 'cli-import'])).includes('created=1'));
fs.unlinkSync(imported);
const scanDir = fs.mkdtempSync(path.join(os.tmpdir(), 'taboo-scan-'));
const leaked = 'wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY';
fs.writeFileSync(path.join(scanDir, 'leak.env'), `aws_secret_access_key=${leaked}\n`);
await cli(['scan','--upload','--org','a'], {cwd: scanDir}, false);
const reports = await fetch(`${env.TABOO_SERVER}/api/v1/orgs/a/scan-reports`, {headers:{Authorization:`Bearer ${env.TABOO_TEST_OWNER_TOKEN}`}});
const reportList = await reports.json();
assert.equal(reports.status, 200);
let sawRule = false;
for (const report of reportList.reports) {
  const detail = await (await fetch(`${env.TABOO_SERVER}/api/v1/orgs/a/scan-reports/${report.id}`, {headers:{Authorization:`Bearer ${env.TABOO_TEST_OWNER_TOKEN}`}})).text();
  assert.equal(detail.includes(leaked), false, 'matched secret text was stored');
  if (detail.includes('aws-secret-access-key')) sawRule = true;
}
assert.equal(sawRule, true, `uploaded finding missing\nstderr=${lastCliErr}\nreports=${JSON.stringify(reportList)}`);
fs.rmSync(scanDir, {recursive:true, force:true});
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
