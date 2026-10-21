const fs = require('node:fs');
const path = require('node:path');
const http = require('node:http');
const {execFileSync} = require('node:child_process');
const {chromium} = require('playwright');
const fixture = require('./fixture.cjs');
(async()=>{
  const out=path.join(__dirname,'out'); fs.mkdirSync(out,{recursive:true});
  const sources = {before:execFileSync('git',['show','32e0017d7cf953e0289421c97f4c16ced354421c:web/index.html'],{encoding:'utf8'}),after:fs.readFileSync(path.join(__dirname,'../index.html'),'utf8')};
  if(process.argv[2]) sources.customization=fs.readFileSync(process.argv[2],'utf8');
  const server=http.createServer((req,res)=>{
    const html=sources[new URL(req.url,'http://localhost').pathname.slice(1)] || sources.after;
    res.setHeader('Content-Type','text/html; charset=utf-8');res.end(html.replace('(function boot() {',fixture+'\n(function boot() {').replace('startDemo(); kick(); return;',"window.__test.load(); kick(); return;"));
  });
  await new Promise(r=>server.listen(0,'127.0.0.1',r));
  let browser;
  try {
    browser=await chromium.launch({...(process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH?{executablePath:process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH}:{})});
    const page=await browser.newPage({viewport:{width:1280,height:900},colorScheme:'light'});
    await page.addInitScript(()=>{Date.now=()=>1792548000000;});
    for(const name of Object.keys(sources)) {
      await page.goto(`http://127.0.0.1:${server.address().port}/${name}?demo=1`);
      await page.waitForFunction(()=>window.__lr?.nodes>0);
      if(name==='after') await page.locator('[data-d="ltr"]').click();
      await page.waitForTimeout(1000);
      await page.screenshot({path:path.join(out,`${name}.png`),fullPage:true});
    }
  } finally {if (browser) await browser.close(); await new Promise(r=>server.close(r));}
})().catch(e=>{console.error(e);process.exitCode=1;});
