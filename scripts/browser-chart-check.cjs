// Isolated browser fixtures; no production requests.
const {chromium}=require('playwright');
const fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict');
(async()=>{
 const browser=await chromium.launch({channel:'msedge',headless:true});
 try{
  const page=await browser.newPage({viewport:{width:1280,height:900}});
  const errors=[],requests=[];page.on('pageerror',e=>errors.push(e.message));
  await page.route('http://seesize.test/**',route=>{
   const url=new URL(route.request().url());
   if(url.pathname==='/')return route.fulfill({contentType:'text/html',body:fs.readFileSync(path.join(__dirname,'../internal/hub/web/index.html'),'utf8')});
   if(url.pathname.endsWith('/servers'))return route.fulfill({json:{servers:[{agent_id:'a',hostname:'fixture',online:true,metrics:{cpu_percent:99}}]}});
   if(url.pathname.endsWith('/trend')){
    requests.push(url);
    return route.fulfill({json:{first:'2026-10-20T00:00:00Z',last:'2026-10-20T00:00:20Z',step_seconds:5,points:[0,10,20].map((s,i)=>({at:new Date(Date.parse('2026-10-20T00:00:00Z')+s*1000).toISOString(),average:[10+i,20,30,1024,2048],peak:[40+i,50,60,4096,8192],network_scope:'eth0'}))}});
   }
   return route.fulfill({json:{}});
  });
  await page.goto('http://seesize.test/');
  await page.locator('[data-history-toggle]').click();
  await page.locator('[data-history] select').selectOption('30m');
  await page.waitForFunction(()=>document.querySelector('.history-note').textContent.includes('实际数据'));
  const cpu=page.locator('canvas[data-chart="cpu"]');
  await cpu.scrollIntoViewIfNeeded();
  const box=await cpu.boundingBox();await page.mouse.move(box.x+box.width/2,box.y+70);
  const tip=page.locator('.chart-tooltip:visible');
  assert.match(await tip.innerText(),/11.0%/);assert.match(await tip.innerText(),/41.0%/);
  assert.doesNotMatch(await tip.innerText(),/样本|聚合/);
  // Count actual drawing connections: 10-second cadence must not become isolated dots in 5-second buckets.
  const segments=await page.evaluate(()=>{
   const c=document.createElement('canvas');c.dataset.chart='cpu';
   const wrap=document.createElement('div');wrap.append(c);document.body.append(wrap);
   const ctx=c.getContext('2d'),original=ctx.lineTo.bind(ctx);let lines=0;ctx.lineTo=(...args)=>{lines++;original(...args)};
   const points=[0,10,20].map(s=>({at:new Date(s*1000).toISOString(),value:20}));
   drawLines(c,points,[{value:s=>s.value,color:'#fff'}],100,5);wrap.remove();return lines;
  });assert.equal(segments,7); // 5 grid lines + 2 connected sample segments.
  await page.getByRole('button',{name:'放大CPU图表',exact:true}).click();
  const dialog=page.locator('dialog');
  await dialog.locator('canvas').waitFor();
  assert.equal(await dialog.locator('select').inputValue(),'');
  assert.equal(requests.at(-1).searchParams.get('range'),'30m');
  assert.ok((await dialog.locator('canvas').boundingBox()).height>300);
  await dialog.locator('select').selectOption({index:1});
  await page.waitForFunction(()=>document.querySelector('dialog .label').textContent.includes('实际数据'));
  assert.ok(requests.at(-1).searchParams.has('to'));
  await page.keyboard.press('Escape');await dialog.waitFor({state:'detached'});
  await page.locator('[data-history] select').selectOption('1h');
  await page.getByRole('button',{name:'放大CPU图表',exact:true}).click();
  assert.ok(await dialog.locator('option').count()>2);
  await page.mouse.click(2,2);await dialog.waitFor({state:'detached'});
  await page.locator('[data-history] select').selectOption('24h');
  await page.getByRole('button',{name:'放大CPU图表',exact:true}).click();
  assert.equal(await dialog.locator('select').isVisible(),false);
  await page.setViewportSize({width:390,height:700});
  assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true);
  assert.deepEqual(errors,[]);
  console.log('PASS: tooltip values, cadence connections, expansion, historical selection, close, mobile.');
 }finally{await browser.close()}
})().catch(e=>{console.error(e);process.exitCode=1});
