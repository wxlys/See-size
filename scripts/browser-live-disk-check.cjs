// Live HTTPS page + read-only API; directory interactions use intercepted fixtures.
// No disk snapshot is uploaded and no server-side scan is started.
const {chromium}=require('playwright');
const {execFileSync}=require('node:child_process');
const assert=require('node:assert/strict');
(async()=>{
 const token=execFileSync('ssh',['-o','BatchMode=yes','wsrser','cat /home/wsr/seesize/admin.token'],{encoding:'utf8',timeout:15000}).trim();
 const browser=await chromium.launch({channel:'msedge',headless:true});
 try{
  const page=await browser.newPage({viewport:{width:1280,height:1000}}),origin='https://8.148.5.169';
  const errors=[];page.on('pageerror',()=>errors.push('page script error'));
  await page.goto(origin+'/login');
  await page.getByLabel('管理凭据').fill(token);await page.getByRole('button',{name:'登录',exact:true}).click();
  await page.waitForURL(origin+'/');
  await page.locator('[data-disk-toggle="wsrser-main"]').waitFor();
  const real=await page.request.get(origin+'/api/v1/servers/wsrser-main/disk');assert.equal(real.status(),200);
  const actual=await real.json();assert.ok(Object.hasOwn(actual,'reference_snapshot'));
  await page.locator('[data-disk-toggle="wsrser-main"]').click();
  const panel=page.locator('[data-disk="wsrser-main"]');
  if(!actual.snapshots.length)await panel.getByText('暂无目录快照。请在该服务器上手动运行目录扫描。',{exact:true}).waitFor();
  const full={agent_id:'wsrser-main',root:'/browser-fixture-only',depth:3,at:'2026-10-21T01:00:00Z',complete:true,entries:4,skipped:0,nodes:[{path:'.',bytes:1000},{path:'large',bytes:600},{path:'large/sub',bytes:100},{path:'growing',bytes:300}]};
  let fixture={snapshots:[full],reference_snapshot:null,comparable:true,changes:[{path:'large',bytes:600,delta:10},{path:'growing',bytes:300,delta:100}],alerts:[],growth_threshold_bytes:104857600};
  await page.route('**/api/v1/servers/wsrser-main/disk',r=>r.fulfill({json:fixture}));
  await page.evaluate(()=>loadDisk('wsrser-main'));
  await panel.getByText('扫描完整',{exact:true}).waitFor();
  await panel.getByRole('button',{name:'展开 large',exact:true}).click();
  assert.ok(await panel.locator('[data-directory="large/sub"]').isVisible());
  await panel.getByLabel('目录排序').selectOption('growth');
  assert.deepEqual(await panel.locator('.disk-row').evaluateAll(rows=>rows.map(r=>r.dataset.directory)),['.','growing','large','large/sub']);
  assert.match(await panel.locator('[data-directory="large"]').innerText(),/60.0%/);
  fixture={...fixture,snapshots:[{...full,complete:false,reason:'entry limit',at:'2026-10-21T02:00:00Z',nodes:[{path:'.',bytes:10}]}],reference_snapshot:full,comparable:false,changes:[]};
  await page.evaluate(()=>loadDisk('wsrser-main'));
  await panel.getByText('扫描不完整',{exact:true}).waitFor();
  assert.match(await panel.innerText(),/达到扫描条目上限/);
  assert.equal(await panel.getByLabel('目录结果来源').inputValue(),'reference');
  assert.match(await panel.locator('.disk-source-note').innerText(),/不代表当前占用/);
  assert.equal(await panel.locator('.disk-positive,.disk-negative').count(),0);
  await panel.getByLabel('目录结果来源').selectOption('current');
  assert.equal(await panel.locator('.disk-row').count(),1);
  await page.unroute('**/api/v1/servers/wsrser-main/disk');
  await page.getByRole('button',{name:'退出登录',exact:true}).click();await page.waitForURL('**/login');
  assert.equal((await page.request.get(origin+'/api/v1/servers/wsrser-main/disk')).status(),401);
  assert.deepEqual(errors,[]);
  console.log('PASS: live HTTPS page/login/disk API/logout; served directory UI sorting, tree, shares and partial references verified with browser-only fixtures. Real snapshots: '+actual.snapshots.length);
 }finally{await browser.close()}
})().catch(()=>{console.error('Live disk verification failed; inspect the check without printing credentials.');process.exitCode=1});
