// Read-only business smoke against authorized wsrser; credential stays in process memory.
const {chromium}=require('playwright');
const {execFileSync}=require('node:child_process');
const assert=require('node:assert/strict');
(async()=>{
 const token=execFileSync('ssh',['-o','BatchMode=yes','wsrser','cat /home/wsr/seesize/admin.token'],{encoding:'utf8',timeout:15000}).trim();
 const browser=await chromium.launch({channel:'msedge',headless:true});
 try{
  const page=await browser.newPage({viewport:{width:1440,height:1000}});
  const failures=[];page.on('pageerror',()=>failures.push('page script error'));
  const origin='https://8.148.5.169';
  await page.goto(origin+'/login');
  await page.getByLabel('管理凭据').fill(token);
  await page.getByRole('button',{name:'登录',exact:true}).click();
  await page.waitForURL(origin+'/');
  await page.locator('[data-history-toggle="wsrser-main"]').click();
  const panel=page.locator('[data-history="wsrser-main"]');
  for(const range of ['30m','1h','24h']){
   const loaded=page.waitForResponse(r=>r.url().includes('/wsrser-main/trend?range='+range)&&r.status()===200);
   await panel.locator('select').selectOption(range);await loaded;
   await page.waitForFunction(()=>document.querySelector('[data-history="wsrser-main"] .history-note').textContent.includes('实际数据'));
   const cpu=panel.locator('[data-chart="cpu"]');await cpu.scrollIntoViewIfNeeded();
   const box=await cpu.boundingBox();await page.mouse.move(box.x+box.width*.65,box.y+80);
   const tip=panel.locator('.chart-tooltip:visible');await tip.waitFor();
   assert.doesNotMatch(await tip.innerText(),/样本|聚合/);
   const expanded=page.waitForResponse(r=>r.url().includes('/wsrser-main/trend?range='+range)&&r.status()===200);
   await panel.getByRole('button',{name:'放大CPU图表',exact:true}).click();await expanded;
   const dialog=page.locator('dialog');await dialog.waitFor();
   await page.waitForFunction(()=>document.querySelector('dialog .label').textContent.includes('实际数据'));
   assert.ok((await dialog.locator('canvas').boundingBox()).height>300);
   if(range==='24h')assert.equal(await dialog.locator('select').isVisible(),false);
   else{
    const historical=page.waitForResponse(r=>r.url().includes('/wsrser-main/trend?')&&new URL(r.url()).searchParams.has('to')&&r.status()===200);
    await dialog.locator('select').selectOption({index:1});await historical;
    await page.waitForFunction(()=>document.querySelector('dialog .label').textContent.includes('实际数据'));
   }
   await page.keyboard.press('Escape');await dialog.waitFor({state:'detached'});
  }
  // Compare UI tooltip text with the actual live response used for that chart.
  const responsePromise=page.waitForResponse(r=>r.url().includes('/wsrser-main/trend?range=24h')&&r.status()===200);
  await panel.locator('select').selectOption('24h');
  const data=await (await responsePromise).json();
  await page.waitForTimeout(200);
  const target=data.points[Math.floor(data.points.length/2)];
  const start=Date.parse(data.points[0].at),end=Date.parse(data.points.at(-1).at);
  const bytes=value=>{
   if(!value)return '0 B/s';
   const units=['B','KB','MB','GB','TB'];let i=0;
   while(value>=1024&&i<units.length-1){value/=1024;i++}
   return value.toFixed(i<2?0:1)+' '+units[i]+'/s';
  };
  for(const [name,indices] of [['cpu',[0]],['memory',[1]],['disk',[2]],['network',[3,4]]]){
   const canvas=panel.locator(`[data-chart="${name}"]`);await canvas.scrollIntoViewIfNeeded();
   const box=await canvas.boundingBox(),width=Math.max(box.width,220);
   const x=66+(Date.parse(target.at)-start)*(width-78)/(end-start||1);
   await page.mouse.move(box.x+x,box.y+80);
   const tooltip=canvas.locator('..').locator('.chart-tooltip:visible');await tooltip.waitFor();
   const rows=await tooltip.locator('strong').allTextContents();
   assert.deepEqual(rows,indices.flatMap(i=>[target.average[i],target.peak[i]].map(v=>name==='network'?bytes(v):v.toFixed(1)+'%')));
  }
  await page.getByRole('button',{name:'退出登录',exact:true}).click();
  await page.waitForURL('**/login');
  assert.equal((await page.request.get(origin+'/api/v1/servers')).status(),401);
  assert.deepEqual(failures,[]);
  console.log('PASS: live HTTPS login; 30m/1h/24h charts, tooltips, expanded historical windows; all four live tooltips equal API; logout denies access.');
 }finally{await browser.close()}
})().catch(()=>{console.error('Live chart verification failed; inspect the relevant check without printing credentials.');process.exitCode=1});
