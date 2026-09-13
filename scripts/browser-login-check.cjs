// Live login smoke test. Supply SEE_SIZE_TEST_ADMIN_TOKEN and optional SEE_SIZE_TEST_URL.
const {chromium}=require('playwright');
const assert=require('node:assert/strict');
(async()=>{
 const token=process.env.SEE_SIZE_TEST_ADMIN_TOKEN;if(!token)throw new Error('test credential required');
 const browser=await chromium.launch({channel:'msedge',headless:true});
 try{
  const page=await browser.newPage();const base=process.env.SEE_SIZE_TEST_URL||'http://127.0.0.1:18082';
  await page.goto(base);await page.waitForURL('**/login');
  await page.getByLabel('管理凭据').fill(token);await page.getByRole('button',{name:'登录',exact:true}).click();
  await page.waitForURL(base+'/');await page.waitForFunction(()=>document.querySelector('#updated').textContent.startsWith('更新时间'));
  await page.getByRole('button',{name:'设备管理',exact:true}).click();await page.getByText('aliyun-test-18 · 已授权',{exact:false}).waitFor();
  await page.getByRole('button',{name:'退出登录',exact:true}).click();await page.waitForURL('**/login');
  const res=await page.request.get(base+'/api/v1/servers');assert.equal(res.status(),401);
  console.log('PASS: login page, authenticated dashboard/device list, logout and anonymous denial.');
 }finally{await browser.close()}
})().catch(e=>{console.error(e.message);process.exitCode=1});
