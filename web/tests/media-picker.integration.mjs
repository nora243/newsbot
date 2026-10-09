import { test } from "node:test";
import assert from "node:assert/strict";
import http from "node:http";
import { spawn } from "node:child_process";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";

const delay = ms => new Promise(resolve => setTimeout(resolve, ms));
async function waitFor(fn) { for (let i=0;i<200;i++) { try { if(await fn())return; } catch {} await delay(50); } throw Error("Timed out waiting for UI"); }
async function port() {const s=http.createServer();await new Promise(r=>s.listen(0,"127.0.0.1",r));const p=s.address().port;await new Promise(r=>s.close(r));return p;}

test("media selections stay bounded and do not start video downloads", {skip:process.env.NEWSBOT_BROWSER_TEST!=="1",timeout:60000}, async()=>{
 const token="media-browser-test-token-32-characters";
 const assets=Array.from({length:40},(_,i)=>({id:(i+1).toString(16).padStart(32,"0"),article_id:1,kind:i<8?"video":"image",source_name:"Test source",article_title:"Test article"}));
 const requests=[];const exceptions=[];
 const backend=http.createServer((req,res)=>{
  assert.equal(req.headers.authorization,`Bearer ${token}`);
  const path=new URL(req.url,"http://localhost").pathname;
  if(path.endsWith("/preview")){res.setHeader("Content-Type","image/png");res.end(Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aY1sAAAAASUVORK5CYII=","base64"));return;}
  res.setHeader("Content-Type","application/json");
  if(path==="/api/overview")res.end(JSON.stringify({stats:{sources:1,active_sources:1,articles:1,videos:0,pending_review:0,processing:0,failed:0},ai:{ready:true,provider:"test"}}));
  else if(path==="/api/sources")res.end(JSON.stringify([{id:1,name:"Test source",rss_url:"https://example.com/rss",active:true,language:"en",category:"WORLD",license_notes:"Test rights"}]));
  else if(path==="/api/articles")res.end(JSON.stringify([{id:1,source_id:1,source_name:"Test source",url:"https://example.com/one",title:"Test article",content:"Source content"}]));
  else if(path==="/api/media/discover")res.end(JSON.stringify({assets,warnings:[]}));
  else if(path.endsWith("/file")){res.statusCode=404;res.end(JSON.stringify({error:"Playback unavailable in test"}));}
  else res.end("[]");
 });
 await new Promise(r=>backend.listen(0,"127.0.0.1",r));
 const webPort=await port(),chromePort=await port(),origin=`http://127.0.0.1:${webPort}`;
 const profile=await mkdtemp(join(tmpdir(),"newsbot-media-chrome-"));
 const server=spawn(process.execPath,["node_modules/next/dist/bin/next","start","--hostname","127.0.0.1","--port",String(webPort)],{env:{...process.env,PORT:String(webPort),HOSTNAME:"127.0.0.1",ADMIN_TOKEN:token,WEB_ORIGIN:origin,API_INTERNAL_URL:`http://127.0.0.1:${backend.address().port}`},stdio:"ignore"});
 const chrome=spawn(process.env.CHROME_PATH||"/usr/bin/google-chrome",["--headless=new","--no-sandbox","--disable-dev-shm-usage",`--remote-debugging-port=${chromePort}`,`--user-data-dir=${profile}`,"about:blank"],{stdio:"ignore"});
 let ws;
 try{
  await waitFor(async()=> (await fetch(origin)).ok);
  await waitFor(async()=> (await fetch(`http://127.0.0.1:${chromePort}/json/version`)).ok);
  const tabs=await (await fetch(`http://127.0.0.1:${chromePort}/json`)).json();
  ws=new WebSocket(tabs.find(t=>t.type==="page").webSocketDebuggerUrl);await new Promise((r,j)=>{ws.addEventListener("open",r,{once:true});ws.addEventListener("error",j,{once:true})});
  let sequence=0;const pending=new Map();
  ws.addEventListener("message",event=>{const m=JSON.parse(event.data);if(m.id){const p=pending.get(m.id);if(p){pending.delete(m.id);m.error?p.reject(Error(m.error.message)):p.resolve(m.result)}}else if(m.method==="Runtime.exceptionThrown")exceptions.push(m.params);else if(m.method==="Network.requestWillBeSent")requests.push(m.params.request.url)});
  const send=(method,params={})=>new Promise((resolve,reject)=>{const id=++sequence;pending.set(id,{resolve,reject});ws.send(JSON.stringify({id,method,params}))});
  const evaluate=async expression=>{const r=await send("Runtime.evaluate",{expression,returnByValue:true,awaitPromise:true});if(r.exceptionDetails)throw Error(r.exceptionDetails.exception?.description || r.exceptionDetails.text);return r.result.value;};
  await send("Runtime.enable");await send("Network.enable");await send("Page.enable");
  const login=await fetch(`${origin}/api/session`,{method:"POST",headers:{Origin:origin,"Content-Type":"application/json"},body:JSON.stringify({token})});
  assert.equal(login.status,200);
  const cookie=login.headers.get("set-cookie").split(";")[0].split("=")[1];
  await send("Network.setCookie",{name:"newsbot_session",value:cookie,url:origin,httpOnly:true,sameSite:"Lax"});
  await send("Page.navigate",{url:origin});
  await waitFor(()=>evaluate(`!!document.querySelector('.sidebar')`));
  await waitFor(()=>requests.some(u=>u.includes("/api/backend/articles?")));
  await evaluate(`Array.from(document.querySelectorAll('nav button')).find(b=>b.textContent.includes('Tin đã thu thập')).click()`);
  await waitFor(()=>evaluate(`!!document.querySelector('.article-row input')`));
  await evaluate(`document.querySelector('.article-row input').click()`);
  await evaluate(`Array.from(document.querySelectorAll('button')).find(b=>b.textContent.includes('Chọn ảnh/video')).click()`);
  await waitFor(()=>evaluate(`document.querySelectorAll('.media-card').length===40`));
  assert.equal(await evaluate(`document.querySelectorAll('.media-card video').length`),0);
  for(let i=0;i<30;i++){
   await evaluate(`Array.from(document.querySelectorAll('.media-actions button')).find(b=>b.textContent.includes('Chọn tất cả')).click()`);
   await waitFor(()=>evaluate(`document.querySelector('.media-footer strong').textContent==='24/24'`));
   await evaluate(`document.querySelector('.media-card input').click()`);
   await waitFor(()=>evaluate(`document.querySelector('.media-footer strong').textContent==='23/24'`));
   await evaluate(`document.querySelector('.media-card input').click()`);
   await waitFor(()=>evaluate(`document.querySelector('.media-footer strong').textContent==='24/24'`));
  }
  assert.equal(requests.filter(u=>/\/media\/[a-f0-9]{32}\/file/.test(u)).length,0,"selection downloaded original media");
  assert.equal(await evaluate(`document.querySelectorAll('.media-card video').length`),0);
  await evaluate(`document.querySelector('.media-video-preview').click()`);
  await waitFor(()=>evaluate(`document.querySelectorAll('.media-card video').length===1`));
  assert.equal(exceptions.length,0,"browser JavaScript errors");
  assert.equal(await evaluate(`document.querySelectorAll('.media-card img:not([loading=lazy])').length`),0);
 }catch(e){console.error("Browser exceptions:",JSON.stringify(exceptions));throw e;}finally{
  if(ws)ws.close();chrome.kill("SIGTERM");server.kill("SIGTERM");
  backend.closeAllConnections();await new Promise(r=>backend.close(r));
  await delay(100);await rm(profile,{recursive:true,force:true,maxRetries:5,retryDelay:100});
 }
});
