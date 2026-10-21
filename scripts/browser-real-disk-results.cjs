// Actual scan uploads and live HTTPS UI; no intercepted responses or synthetic API data.
const {chromium}=require('playwright');
const {execFileSync}=require('node:child_process');
const fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict');
(async()=>{
 const output=process.env.SEE_SIZE_DISK_RESULTS_DIR;if(!output)throw new Error('Set a new screenshot output directory');
 fs.mkdirSync(output);
 const token=execFileSync('ssh',['-o','BatchMode=yes','wsrser','cat /home/wsr/seesize/admin.token'],{encoding:'utf8',timeout:15000}).trim();
 const browser=await chromium.launch({channel:'msedge',headless:true});
 try{
  const page=await browser.newPage({viewport:{width:1440,height:1100}}),origin='https://8.148.5.169';
  const errors=[];page.on('pageerror',()=>errors.push('page script error'));
  await page.goto(origin+'/login');await page.getByLabel('管理凭据').fill(token);
  await page.getByRole('button',{name:'登录',exact:true}).click();await page.waitForURL(origin+'/');
  async function panelFor(id){
   await page.locator('#server-nav .server-choice').filter({hasText:id}).click();
   const panel=page.locator(`[data-disk="${id}"]`);
   if(!await panel.isVisible())await page.locator(`[data-disk-toggle="${id}"]`).click();
   await panel.locator('.disk-tree').waitFor();return panel;
  }
  const successID='disk-scan-success-20261021';
  const success=await panelFor(successID);
  const data=await (await page.request.get(origin+'/api/v1/servers/'+successID+'/disk')).json();
  assert.ok(data.comparable&&data.snapshots[0].complete);
  const changes=new Map(data.changes.map(c=>[c.path,c.delta]));
  assert.equal(changes.get('app-logs'),128*1024*1024);
  assert.equal(changes.get('cache'),-3*1024*1024);
  assert.equal(changes.get('archive'),-2*1024*1024);
  await success.getByRole('button',{name:'展开 app-logs',exact:true}).click();
  assert.ok(await success.locator('[data-directory="app-logs/service"]').isVisible());
  assert.match(await success.locator('[data-directory="archive"]').innerText(),/已移除/);
  await success.screenshot({path:path.join(output,'success-size.png')});
  await success.getByLabel('目录排序').selectOption('growth');
  await success.screenshot({path:path.join(output,'success-growth.png')});
  const card=success.locator('..');await card.locator('[data-event-toggle]').click();
  await card.getByText('待处理',{exact:true}).first().waitFor();
  const events=await (await page.request.get(origin+'/api/v1/servers/'+successID+'/events')).json();
  assert.equal(events.events.length,2);
  await card.locator('[data-events]').screenshot({path:path.join(output,'success-events.png')});
  for(const [scenario,reason] of [['entry-limit',/达到扫描条目上限/],['soft-timeout',/达到时间预算或扫描已取消/],['permission-denied',/不可读取|无权限/]]){
   execFileSync('ssh',['-o','BatchMode=yes','wsrser','python3 /home/wsr/seesize/restore-check-disk-validation-20261021/disk-validation-failure-case.py '+scenario],{encoding:'utf8',timeout:20000});
   const result=await panelFor('disk-scan-incomplete-20261021');await page.evaluate(()=>loadDisk('disk-scan-incomplete-20261021'));
   await page.waitForFunction(text=>document.querySelector('[data-disk="disk-scan-incomplete-20261021"]').textContent.includes(text),scenario==='entry-limit'?'达到扫描条目上限':scenario==='soft-timeout'?'达到时间预算或扫描已取消':'不可读取');
   assert.match(await result.innerText(),reason);
   assert.equal(await result.getByLabel('目录结果来源').inputValue(),'reference');
   assert.equal(await result.locator('.disk-positive,.disk-negative').count(),0);
   await result.screenshot({path:path.join(output,'failure-'+scenario+'.png')});
  }
  const partial=await panelFor('disk-scan-incomplete-20261021');
  assert.match(await partial.innerText(),/扫描不完整/);
  assert.match(await partial.innerText(),/不可读取|无权限/);
  assert.equal(await partial.getByLabel('目录结果来源').inputValue(),'reference');
  assert.match(await partial.locator('.disk-source-note').innerText(),/不代表当前占用/);
  assert.equal(await partial.locator('.disk-positive,.disk-negative').count(),0);
  await partial.screenshot({path:path.join(output,'partial-reference.png')});
  await partial.getByLabel('目录结果来源').selectOption('current');
  assert.match(await partial.locator('.disk-source-note').innerText(),/仅为已扫描部分/);
  await partial.screenshot({path:path.join(output,'partial-current.png')});
  const logPanel=await panelFor('disk-scan-varlog-20261021');
  assert.match(await logPanel.innerText(),/\/var\/log/);
  await logPanel.screenshot({path:path.join(output,'real-varlog.png')});
  await page.setViewportSize({width:390,height:850});
  assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true);
  await logPanel.screenshot({path:path.join(output,'real-varlog-mobile.png')});
  await page.getByRole('button',{name:'退出登录',exact:true}).click();await page.waitForURL('**/login');
  assert.deepEqual(errors,[]);
  console.log('PASS: actual uploaded snapshots, positive/negative/deleted changes, growth events, reference and partial views, /var/log and mobile. Screenshots: '+output);
 }finally{await browser.close()}
})().catch(()=>{console.error('Real disk UI check failed; inspect the step without printing credentials.');process.exitCode=1});
