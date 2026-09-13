// Run with NODE_PATH pointing to a Playwright installation; uses installed Edge.
const {chromium}=require('playwright');
const fs=require('node:fs');
const path=require('node:path');
const assert=require('node:assert/strict');
(async()=>{
 const browser=await chromium.launch({channel:'msedge',headless:true});
 try{
  const page=await browser.newPage({viewport:{width:650,height:480}});
  const errors=[];page.on('pageerror',e=>errors.push(e.message));
  const metrics={cpu_percent:2,memory:{used_bytes:30,total_bytes:100},root_disk:{used_bytes:40,total_bytes:100},network:{received_bytes_per_second:10,sent_bytes_per_second:5}};
  let updates=0;
  await page.route('http://seesize.test/**',async route=>{
   const url=new URL(route.request().url());let data;
   if(url.pathname==='/')return route.fulfill({contentType:'text/html',body:fs.readFileSync(path.join(__dirname,'../internal/hub/web/index.html'),'utf8')});
   if(url.pathname.endsWith('/servers'))data={servers:[{agent_id:'demo',hostname:'demo',online:++updates===1,os:'linux',architecture:'amd64',observed_ip:'127.0.0.1',metrics:{...metrics,cpu_percent:updates}}]};
   else if(url.pathname.endsWith('/metrics'))data={samples:[{collected_at:new Date().toISOString(),metrics}]};
   else data={snapshots:[{root:'/test',at:'2026-09-13T00:00:00Z',complete:true,entries:2,skipped:0,nodes:[{path:'.',bytes:1003},{path:'docs',bytes:1003}]}],changes:[],alerts:[],comparable:false,growth_threshold_bytes:104857600};
   await route.fulfill({json:data});
  });
  await page.goto('http://seesize.test/');
  await page.locator('[data-history-toggle]').click();
  await page.locator('[data-disk-toggle]').click();
  await page.getByText('扫描完整',{exact:true}).waitFor();
  const before=await page.evaluate(()=>{
   window.testPanel=document.querySelector('[data-disk]');window.testCanvas=document.querySelector('canvas');
   window.testButton=document.querySelector('[data-disk-toggle]');testButton.focus();window.scrollTo(0,document.documentElement.scrollHeight);
   return {scroll:window.scrollY,html:testPanel.innerHTML};
  });
  await page.waitForFunction(()=>document.querySelector('.server-head .pill').textContent==='离线');
  const after=await page.evaluate(()=>({scroll:window.scrollY,html:testPanel.innerHTML,panel:testPanel===document.querySelector('[data-disk]'),canvas:testCanvas===document.querySelector('canvas'),focus:document.activeElement===testButton,expanded:!testPanel.hidden}));
  assert.equal(after.panel,true);assert.equal(after.canvas,true);assert.equal(after.focus,true);assert.equal(after.expanded,true);
  assert.equal(after.html,before.html);assert.ok(Math.abs(after.scroll-before.scroll)<=1);assert.deepEqual(errors,[]);
  await page.locator('[data-disk-toggle]').click();assert.equal(await page.locator('[data-disk]').isVisible(),false);
  await page.locator('[data-disk-toggle]').click();assert.equal(await page.locator('[data-disk]').isVisible(),true);
  console.log('PASS: auto-refresh preserves panel, canvas, focus, scroll, and toggle behavior; online status updates.');
 }finally{await browser.close()}
})().catch(e=>{console.error(e);process.exitCode=1});
