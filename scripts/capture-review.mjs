// 最终验收截图：桌面与移动，覆盖登录、密钥、抽屉、审计、设置、对话框
import { chromium } from 'playwright';
const b = await chromium.launch();
const shots = [];
async function page(vp) {
  const ctx = await b.newContext({viewport:vp, deviceScaleFactor:2, locale:'zh-CN', timezoneId:'Asia/Shanghai'});
  return [ctx, await ctx.newPage()];
}
// login
{
  const [ctx,p] = await page({width:1440,height:900});
  await p.goto('http://127.0.0.1:7303/', {waitUntil:'load'}); await p.waitForTimeout(700);
  await p.screenshot({path:'.impeccable/review/login-desktop.jpg'}); shots.push('login-desktop');
  await ctx.close();
  const [ctx2,p2] = await page({width:390,height:844});
  await p2.goto('http://127.0.0.1:7303/', {waitUntil:'load'}); await p2.waitForTimeout(700);
  await p2.screenshot({path:'.impeccable/review/login-mobile.jpg'}); shots.push('login-mobile');
  await ctx2.close();
}
// app desktop
{
  const [ctx,p] = await page({width:1440,height:900});
  const errs=[]; p.on('pageerror',e=>errs.push(e.message));
  await p.goto('http://127.0.0.1:7301/', {waitUntil:'load'}); await p.waitForTimeout(700);
  await p.screenshot({path:'.impeccable/review/desktop.jpg'}); shots.push('desktop');
  const eye = p.getByRole('button',{name:'显示',exact:true}).first(); if (await eye.count()) { await eye.click(); await p.waitForTimeout(400); }
  await p.screenshot({path:'.impeccable/review/desktop-revealed.jpg'}); shots.push('desktop-revealed');
  const key = p.locator('button.secret-key-btn').first(); await key.click(); await p.waitForTimeout(600);
  await p.screenshot({path:'.impeccable/review/desktop-drawer.jpg'}); shots.push('desktop-drawer');
  await p.keyboard.press('Escape'); await p.waitForTimeout(300);
  for (const [n,f] of [['审计日志','desktop-audit'],['机器身份','desktop-identities'],['个人设置','desktop-settings']]) {
    await p.getByRole('button',{name:n,exact:true}).first().click(); await p.waitForTimeout(600);
    await p.screenshot({path:`.impeccable/review/${f}.png`}); shots.push(f);
  }
  await p.getByRole('button',{name:'密钥',exact:true}).first().click(); await p.waitForTimeout(300);
  await p.getByRole('button',{name:'＋ 新建密钥'}).first().click(); await p.waitForTimeout(500);
  await p.screenshot({path:'.impeccable/review/desktop-dialog.jpg'}); shots.push('desktop-dialog');
  console.log('desktop errors', errs);
  await ctx.close();
}
// app mobile
{
  const [ctx,p] = await page({width:390,height:844});
  const errs=[]; p.on('pageerror',e=>errs.push(e.message));
  await p.goto('http://127.0.0.1:7301/', {waitUntil:'load'}); await p.waitForTimeout(700);
  await p.screenshot({path:'.impeccable/review/mobile.jpg', fullPage:true}); shots.push('mobile');
  await p.locator('button.secret-key-btn').first().click(); await p.waitForTimeout(600);
  await p.screenshot({path:'.impeccable/review/mobile-drawer.jpg'}); shots.push('mobile-drawer');
  await p.keyboard.press('Escape'); await p.waitForTimeout(300);
  await p.getByRole('button',{name:'设置',exact:true}).first().click().catch(()=>{});
  await p.waitForTimeout(600);
  await p.screenshot({path:'.impeccable/review/mobile-settings.jpg', fullPage:true}); shots.push('mobile-settings');
  console.log('mobile errors', errs);
  await ctx.close();
}
console.log(shots.join(', '));
await b.close();
