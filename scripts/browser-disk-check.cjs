// Isolated fixture checks: never triggers a live filesystem scan or upload.
const {chromium}=require('playwright');
const fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict');
(async()=>{
 const browser=await chromium.launch({channel:'msedge',headless:true});
 try{
  const page=await browser.newPage({viewport:{width:1280,height:1000}});
  const errors=[];page.on('pageerror',e=>errors.push(e.message));
  const full={agent_id:'a',root:'/fixture',depth:3,at:'2026-10-21T01:00:00Z',complete:true,entries:100,skipped:0,
   nodes:[{path:'.',bytes:1000},{path:'large',bytes:600},{path:'large/sub',bytes:200},{path:'growing',bytes:300}]};
  let payload={snapshots:[full],reference_snapshot:null,comparable:true,alerts:[],growth_threshold_bytes:104857600,
   changes:[{path:'.',bytes:1000,delta:110},{path:'large',bytes:600,delta:10},{path:'large/sub',bytes:200,delta:5},{path:'growing',bytes:300,delta:150},{path:'deleted',bytes:0,delta:-50}]};
  await page.route('http://seesize.test/**',route=>{
   const url=new URL(route.request().url());
   if(url.pathname==='/')return route.fulfill({contentType:'text/html',body:fs.readFileSync(path.join(__dirname,'../internal/hub/web/index.html'),'utf8')});
   if(url.pathname.endsWith('/servers'))return route.fulfill({json:{servers:[{agent_id:'a',hostname:'disk fixture',online:true,metrics:{}}]}});
   if(url.pathname.endsWith('/disk'))return route.fulfill({json:payload});
   return route.fulfill({json:{events:[]}});
  });
  await page.goto('http://seesize.test/');await page.locator('[data-disk-toggle]').click();
  const panel=page.locator('[data-disk]');await panel.getByText('扫描完整',{exact:true}).waitFor();
  const paths=()=>panel.locator('.disk-row').evaluateAll(rows=>rows.map(r=>r.dataset.directory));
  assert.deepEqual(await paths(),['.','large','growing','deleted']);
  assert.match(await panel.locator('[data-directory="large"]').innerText(),/60.0%/);
  await panel.getByRole('button',{name:'展开 large',exact:true}).click();
  assert.deepEqual(await paths(),['.','large','large/sub','growing','deleted']);
  await panel.getByLabel('目录排序').selectOption('growth');
  assert.deepEqual(await paths(),['.','growing','large','large/sub','deleted']);
  assert.match(await panel.locator('[data-directory="deleted"]').innerText(),/已移除.*|−50 B/);
  await page.evaluate(()=>{window.savedDirectory=document.querySelector('[data-directory="large"]');return loadDisk('a')});
  assert.equal(await page.evaluate(()=>savedDirectory===document.querySelector('[data-directory="large"]')),true);
  payload={...payload,snapshots:[{...full,at:'2026-10-21T01:30:00Z',nodes:full.nodes.map(n=>({...n,bytes:n.bytes+1}))}]};
  await page.evaluate(()=>loadDisk('a'));
  assert.equal(await panel.getByLabel('目录排序').inputValue(),'growth');
  assert.ok((await paths()).includes('large/sub'));
  payload={snapshots:[{...full,complete:false,reason:'entry limit',entries:10,at:'2026-10-21T02:00:00Z',nodes:[{path:'.',bytes:10}]}],reference_snapshot:full,comparable:false,alerts:[],changes:[],growth_threshold_bytes:104857600};
  await page.evaluate(()=>loadDisk('a'));
  await panel.getByText('扫描不完整',{exact:true}).waitFor();
  assert.match(await panel.innerText(),/达到扫描条目上限/);
  assert.equal(await panel.getByLabel('目录结果来源').inputValue(),'reference');
  assert.match(await panel.locator('.disk-source-note').innerText(),/不代表当前占用/);
  assert.ok((await paths()).includes('large'));
  assert.equal(await panel.locator('option[value="growth"]').evaluate(e=>e.disabled),true);
  assert.equal(await panel.locator('.disk-positive,.disk-negative').count(),0);
  if(process.env.SEE_SIZE_DISK_SCREENSHOT){await panel.scrollIntoViewIfNeeded();await page.screenshot({path:process.env.SEE_SIZE_DISK_SCREENSHOT,fullPage:true});}
  await panel.getByLabel('目录结果来源').selectOption('current');
  assert.deepEqual(await paths(),['.']);
  assert.match(await panel.locator('.disk-source-note').innerText(),/仅为已扫描部分/);
  payload.reference_snapshot=null;payload.snapshots[0].reason='time limit or cancellation';
  await page.evaluate(()=>loadDisk('a'));
  assert.match(await panel.innerText(),/达到时间预算或扫描已取消/);
  assert.equal(await panel.locator('[data-disk-source]').count(),0);
  // Same-sized directories with many children: bounded rendering with explicit pagination.
  payload={...payload,snapshots:[{...full,nodes:[{path:'.',bytes:1000},...Array.from({length:600},(_,i)=>({path:'dir-'+i,bytes:1}))]}],comparable:false};
  await page.evaluate(()=>loadDisk('a'));
  assert.equal(await panel.locator('.disk-row').count(),500);
  await panel.getByRole('button',{name:'显示更多目录'}).click();
  assert.equal(await panel.locator('.disk-row').count(),601);
  await page.setViewportSize({width:390,height:800});
  assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true);
  assert.deepEqual(errors,[]);
  console.log('PASS: size/growth order, shares, tree toggles, removed directories, refresh state, incomplete reference, reasons, pagination, mobile.');
 }finally{await browser.close()}
})().catch(e=>{console.error(e);process.exitCode=1});
