const {chromium}=require('playwright');
const fs=require('node:fs');
const path=require('node:path');
const assert=require('node:assert/strict');
(async()=>{
 const browser=await chromium.launch({channel:'msedge',headless:true});
 try{
  const page=await browser.newPage();const errors=[];page.on('pageerror',e=>errors.push(e.message));
  let data={configured:false};let fail=false;
  await page.route('http://seesize.test/**',route=>{
   const url=new URL(route.request().url());
   if(url.pathname==='/')return route.fulfill({contentType:'text/html',body:fs.readFileSync(path.join(__dirname,'../internal/hub/web/index.html'),'utf8')});
   if(url.pathname==='/api/v1/backup-status')return route.fulfill(fail?{status:503,json:{error:'unavailable'}}:{json:data});
   return route.fulfill({json:{servers:[]}});
  });
  await page.goto('http://seesize.test/');await page.getByRole('button',{name:'备份状态',exact:true}).click();
  await page.waitForFunction(()=>document.querySelector('#backup-result').textContent.includes('尚未配置'));
  data={configured:true,files:7,bytes:1024,status_readable:false};await page.evaluate(()=>loadBackup());
  assert.match(await page.locator('#backup-result').textContent(),/7 份.*未知/);
  data.status_readable=true;data.latest={state:'success',started_at:'2026-09-25T00:00:00Z',finished_at:'2026-09-25T00:01:00Z',keep:7,file:'test.db'};
  await page.evaluate(()=>loadBackup());assert.match(await page.locator('#backup-result').textContent(),/最近任务：成功/);
  data.latest.state='failed';await page.evaluate(()=>loadBackup());assert.match(await page.locator('#backup-result').textContent(),/最近任务：失败/);
  fail=true;await page.evaluate(()=>loadBackup());assert.match(await page.locator('#backup-result').textContent(),/读取失败/);
  assert.deepEqual(errors,[]);console.log('PASS: unconfigured, unknown with old files, success, failed and unreadable backup states.');
 }finally{await browser.close()}
})().catch(e=>{console.error(e);process.exitCode=1});
