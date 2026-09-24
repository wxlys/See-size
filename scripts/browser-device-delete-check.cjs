const {chromium}=require('playwright');
const fs=require('node:fs');
const path=require('node:path');
const assert=require('node:assert/strict');
(async()=>{
 const browser=await chromium.launch({channel:'msedge',headless:true});
 try{
  const page=await browser.newPage();let deleted=false,calls=0;const errors=[];
  page.on('pageerror',e=>errors.push(e.message));
  await page.route('http://seesize.test/**',async route=>{
   const req=route.request(),url=new URL(req.url());
   if(url.pathname==='/')return route.fulfill({contentType:'text/html',body:fs.readFileSync(path.join(__dirname,'../internal/hub/web/index.html'),'utf8')});
   if(req.method()==='DELETE'){
    calls++;assert.equal(url.pathname,'/api/v1/devices/retired');assert.equal(req.headers()['x-seesize-request'],'1');assert.equal(req.postDataJSON().confirm_id,'retired');deleted=true;
    return route.fulfill({json:{status:'deleted'}});
   }
   if(url.pathname==='/api/v1/devices')return route.fulfill({json:{devices:[{agent_id:'active',revoked:false},...deleted?[]:[{agent_id:'retired',revoked:true}]]}});
   return route.fulfill({json:{servers:[]}});
  });
  await page.goto('http://seesize.test/');await page.getByRole('button',{name:'设备管理',exact:true}).click();
  const button=page.getByRole('button',{name:'删除设备及历史'});
  await button.waitFor();assert.equal(await button.count(),1);
  page.once('dialog',d=>d.dismiss());await button.click();assert.equal(calls,0);
  page.once('dialog',d=>d.accept('wrong'));await button.click();await page.getByText('设备 ID 不匹配，未删除',{exact:true}).waitFor();assert.equal(calls,0);
  page.once('dialog',d=>d.accept('retired'));await button.click();await page.waitForFunction(()=>!document.querySelector('#device-list').textContent.includes('retired'));
  assert.equal(calls,1);assert.match(await page.locator('#device-list').textContent(),/active/);assert.deepEqual(errors,[]);
  console.log('PASS: delete only revoked device; cancel and wrong confirmation do not send requests; successful deletion refreshes list.');
 }finally{await browser.close()}
})().catch(e=>{console.error(e);process.exitCode=1});
