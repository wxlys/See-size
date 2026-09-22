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
  let acknowledged=false;
  await page.route('http://seesize.test/**',async route=>{
   const url=new URL(route.request().url());let data;
   if(url.pathname.endsWith('/ack')){assert.equal(route.request().headers()['x-seesize-request'],'1');acknowledged=true;return route.fulfill({json:{status:'acknowledged'}});}
   if(url.pathname.endsWith('/events'))return route.fulfill({json:{ack_enabled:true,next_cursor:0,events:[{id:1,root:'/test',path:'docs',delta:2097152,threshold:1048576,from:'2026-09-13T00:00:00Z',to:'2026-09-13T00:01:00Z',acknowledged_at:acknowledged?'2026-09-13T00:02:00Z':null}]}});
   if(url.pathname==='/')return route.fulfill({contentType:'text/html',body:fs.readFileSync(path.join(__dirname,'../internal/hub/web/index.html'),'utf8')});
   if(url.pathname.endsWith('/servers'))data={servers:[{agent_id:'demo',hostname:'demo',online:++updates===1,os:'linux',architecture:'amd64',observed_ip:'127.0.0.1',metrics:{...metrics,cpu_percent:updates}}]};
   else if(url.pathname.endsWith('/trend'))data={first:'2026-09-13T00:00:00Z',last:'2026-09-13T00:05:00Z',step_seconds:url.searchParams.get('range')==='24h'?240:10,points:[{at:'2026-09-13T00:00:00Z',average:[2,30,40,10,5],peak:[4,30,40,12,6]}]};
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
  await page.getByLabel('趋势时间范围').selectOption('24h');
  await page.waitForFunction(()=>document.querySelector('.history-note').textContent.includes('240 秒'));
  assert.equal(await page.getByLabel('趋势时间范围').inputValue(),'24h');
  const chart=page.locator('[data-chart="cpu"]');
  await chart.hover({position:{x:100,y:70}});
  await page.locator('.chart-tooltip:visible').waitFor();
  assert.match(await page.locator('.chart-tooltip:visible').textContent(),/平均：2.0%.*峰值：4.0%/);
  await page.getByRole('button',{name:'告警历史'}).click();
  await page.getByRole('button',{name:'标记已处理'}).click();
  await page.getByText('已处理',{exact:true}).waitFor();
  console.log('PASS: auto-refresh preserves panel, canvas, focus, scroll, and toggle behavior; online status updates.');
 }finally{await browser.close()}
})().catch(e=>{console.error(e);process.exitCode=1});
