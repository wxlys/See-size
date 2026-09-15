// NODE_PATH must point to an installed Playwright package. No live server used.
const {chromium}=require('playwright');
const fs=require('node:fs');
const path=require('node:path');
const assert=require('node:assert/strict');
(async()=>{
 const browser=await chromium.launch({channel:'msedge',headless:true});
 try{
  const page=await browser.newPage({viewport:{width:1280,height:800}});
  const errors=[];page.on('pageerror',e=>errors.push(e.message));
  let ids=['a','b'];
  await page.route('http://seesize.test/**',async route=>{
   const url=new URL(route.request().url());
   if(url.pathname==='/')return route.fulfill({contentType:'text/html',body:fs.readFileSync(path.join(__dirname,'../internal/hub/web/index.html'),'utf8')});
   if(url.pathname.endsWith('/servers'))return route.fulfill({json:{servers:ids.map(id=>({agent_id:id,hostname:'server-'+id,online:id==='a',os:'linux',metrics:{cpu_percent:id==='a'?1:90}}))}});
   const id=url.pathname.split('/')[4];
   if(url.pathname.endsWith('/trend')){
    if(id==='a')await new Promise(r=>setTimeout(r,150));
    return route.fulfill({json:{first:'2026-09-15T00:00:00Z',last:'2026-09-15T00:00:00Z',step_seconds:url.searchParams.get('range')==='24h'?240:10,points:[{at:'2026-09-15T00:00:00Z',average:[1,2,3,4,5],peak:[1,2,3,4,5]}]}});
   }
   if(url.pathname.endsWith('/disk'))return route.fulfill({json:{snapshots:[{root:'/fixture-'+id,at:'2026-09-15T00:00:00Z',complete:true,entries:1,skipped:0,nodes:[]}],changes:[],alerts:[],comparable:false}});
   return route.fulfill({json:{events:[],ack_enabled:true}});
  });
  await page.goto('http://seesize.test/');
  const visible=page.locator('#servers > .server:visible');
  await page.getByRole('button',{name:'server-a'}).waitFor();
  assert.equal(await visible.count(),1);
  assert.equal(await visible.locator('h2').textContent(),'server-a');
  await visible.locator('[data-history-toggle]').click();
  await visible.locator('select').selectOption('24h');
  await page.getByRole('button',{name:'server-b'}).click();
  assert.equal(await visible.locator('h2').textContent(),'server-b');
  await visible.locator('[data-disk-toggle]').click();
  await visible.getByText('/fixture-b',{exact:false}).waitFor();
  await page.getByRole('button',{name:'server-a'}).click();
  assert.equal(await visible.locator('select').inputValue(),'24h');
  await page.waitForFunction(()=>document.querySelector('[data-history="a"] .history-note').textContent.includes('240 秒'));
  await page.getByRole('button',{name:'server-b'}).click();
  await page.evaluate(()=>{window.savedCard=document.querySelector('#servers > .server:not([hidden])');});
  // Reordering and offline status must not change the selection or DOM identity.
  ids=['b','a'];await page.evaluate(()=>refresh());
  assert.equal(await visible.locator('h2').textContent(),'server-b');
  assert.equal(await page.evaluate(()=>savedCard===document.querySelector('#servers > .server:not([hidden])')),true);
  assert.equal(await visible.locator('[data-disk]').isVisible(),true);
  await page.setViewportSize({width:390,height:700});
  assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true);
  ids=['a'];await page.evaluate(()=>refresh());
  assert.equal(await visible.locator('h2').textContent(),'server-a');
  ids=[];await page.evaluate(()=>refresh());
  assert.equal(await visible.count(),0);
  assert.equal(await page.locator('.server-choice').count(),0);
  ids=['b'];await page.evaluate(()=>refresh());
  assert.equal(await visible.locator('h2').textContent(),'server-b');
  assert.deepEqual(errors,[]);
  console.log('PASS: sidebar selection, per-server state, refresh identity, removal, empty state, and mobile layout.');
 }finally{await browser.close()}
})().catch(e=>{console.error(e);process.exitCode=1});
