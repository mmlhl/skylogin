// 在纯内存状态与假网络中验证保存竞态，不启动界面、不读取真实账号。
import { build } from "esbuild";
const code = `
import { nextTick } from "vue";
import { api } from "../src/api/client";
import { state, refresh } from "../src/composables/useAppState";
import { createRealStore } from "../src/realStore";
const equal = (a,b,msg) => { if (JSON.stringify(a)!==JSON.stringify(b)) throw new Error(msg+": "+JSON.stringify(a)); };
const wait = ms => new Promise(resolve => setTimeout(resolve,ms));
const rows = [{id:"a",name:"甲",channel:"huawei",accountId:"a",tags:["原标签"]},{id:"b",name:"乙",channel:"netease",accountId:"b",tags:[]}];
let server = {accounts:rows,settings:{maskTitles:true,accountTagOrder:[]},game:"旧状态"};
let nextReadDelay = 0, saveDelay=0, fail="", inFlight=0, maxInFlight=0;
globalThis.fetch = async (path,init) => {
 if(path==="/api/state") {const snapshot=JSON.stringify(server);const delay=nextReadDelay;nextReadDelay=0;await wait(delay);return new Response(snapshot);}
 const body=init.body?JSON.parse(init.body):{};inFlight++;maxInFlight=Math.max(maxInFlight,inFlight);const error=fail;await wait(saveDelay);inFlight--;
 if(error)return new Response(JSON.stringify({ok:false,message:error}));
 if(path.includes("/select")){const id=new URL(path,"http://test").searchParams.get("id");server.accounts.forEach(a=>a.current=a.id===id);return new Response(JSON.stringify({ok:true}));}
 if(path.endsWith("/tags")){server.accounts.find(a=>a.id===body.id).tags=["VIP"];return new Response(JSON.stringify({ok:true,account:{id:body.id,tags:["VIP"]}}));}
 if(path.endsWith("/tag-order")){server.settings.accountTagOrder=["VIP","未分组"];return new Response(JSON.stringify({ok:true,tagOrder:server.settings.accountTagOrder}));}
 const byId=new Map(server.accounts.map(a=>[a.id,a]));server.accounts=[...new Set(body.ids)].map(id=>byId.get(id)).filter(Boolean);for(const a of rows)if(!server.accounts.includes(a))server.accounts.push(a);
 return new Response(JSON.stringify({ok:true,ids:server.accounts.map(a=>a.id)}));
};
await refresh();const store=createRealStore({});
nextReadDelay=65;const staleRuntime=refresh();server.game="新运行状态";await refresh();await staleRuntime;equal(state.game,"新运行状态","旧poll不能覆盖其它运行字段");
nextReadDelay=90;const oldPoll=refresh();saveDelay=20;
const saved=store.setTags("a",[" VIP ","vip"]);await wait(5);await refresh();
equal(store.accounts.find(a=>a.id==="a").tags,[" VIP ","vip"],"保存期间轮询不应覆盖草稿");
if(!await saved)throw new Error("规范化标签保存失败");
equal(store.accounts.find(a=>a.id==="a").tags,["VIP"],"响应后应立即采用规范化标签");
await refresh();await oldPoll;await nextTick();
equal(store.accounts.find(a=>a.id==="a").tags,["VIP"],"确认后的旧轮询不应回跳");
fail="预期失败";const toastBefore=store.toasts.length;const failed=store.setTags("a",["失败草稿"]);await wait(5);if(await failed)throw new Error("失败应返回false");
equal(store.accounts.find(a=>a.id==="a").tags,["VIP"],"失败应回滚");equal(store.toasts.length,toastBefore+1,"错误只提示一次");
fail="";nextReadDelay=90;const oldOrderPoll=refresh();const sorted=store.reorder(["b","b","a"]);await wait(5);await refresh();await nextTick();
equal(store.accounts.map(a=>a.id),["b","a"],"排序期间旧轮询不能覆盖排序");if(!await sorted)throw new Error("排序保存失败");await refresh();await oldOrderPoll;await nextTick();equal(store.accounts.map(a=>a.id),["b","a"],"排序规范结果不能被旧poll覆盖");
fail="排序失败";if(await store.reorder(["a","b"]))throw new Error("排序失败返回值错误");equal(store.accounts.map(a=>a.id),["b","a"],"排序失败应回滚");fail="";
const one=store.reorder(["a","b"]),two=store.reorder(["b","a"]);if(!(await one)||!(await two))throw new Error("连续排序失败");equal(maxInFlight,1,"保存必须串行");equal(store.accounts.map(a=>a.id),["b","a"],"较早响应不能盖过最后操作");
if(!await store.setTagOrder([" VIP ","vip","未分组"]))throw new Error("分组顺序保存失败");equal(store.tagOrder,["VIP","未分组"],"应立即显示规范化分组顺序");
fail="分组失败";if(await store.setTagOrder(["不同"]))throw new Error("分组失败返回值错误");equal(store.tagOrder,["VIP","未分组"],"分组顺序失败应回滚");
fail="";store.select("b");equal(store.current?.id,"b","选择应立即显示");await wait(2);store.select("a");equal(store.current?.id,"a","快速选择以最后点击优先");await wait(55);equal(store.current?.id,"a","旧选择响应不能覆盖最后点击");
nextReadDelay=80;const oldSelectionPoll=refresh();store.select("b");await wait(30);await refresh();await oldSelectionPoll;await nextTick();equal(store.current?.id,"b","选择不能被旧poll覆盖");
fail="选择失败";store.select("a");await wait(30);equal(store.current?.id,"b","选择失败回到最后成功账号");
fail="";await store.setTags("a",["VIP"]);server.accounts.find(a=>a.id==="a").tags=["服务端二次修改"];await refresh();await nextTick();equal(store.accounts.find(a=>a.id==="a").tags,["服务端二次修改"],"保存结束后新观测应接纳服务端标签变更");
await store.reorder(["b","a"]);const c={id:"c",name:"丙",channel:"huawei",accountId:"c",tags:[]};server.accounts=[c,...server.accounts.slice().reverse()];await refresh();await nextTick();equal(store.accounts.map(a=>a.id),["c","a","b"],"新观测应接纳新增账号及服务端新顺序");
await store.setTagOrder(["VIP","未分组"]);server.settings.accountTagOrder=["服务端分组"];await refresh();await nextTick();equal(store.tagOrder,["服务端分组"],"新观测应接纳服务端分组顺序");
await store.setTags("a",["VIP"]);server.accounts=server.accounts.filter(a=>a.id!=="a");await refresh();await nextTick();if(store.accounts.some(a=>a.id==="a"))throw new Error("被删除账号应移除");server.accounts.push({...rows[0],tags:["重新添加"]});await refresh();await nextTick();equal(store.accounts.find(a=>a.id==="a").tags,["重新添加"],"删除账号的标签覆盖应释放");
store.select("a");await wait(30);server.accounts.forEach(a=>a.current=a.id==="c");await refresh();await nextTick();equal(store.current?.id,"c","新观测应接纳其它入口切换当前账号");
console.log("PASS: normalized saves, stale polls, rollback, serialized saves, selection, trusted future state releases overlays");
`;
const result = await build({stdin:{contents:code,resolveDir:new URL('.',import.meta.url).pathname.replace(/^\/(\w:)/,'$1'),sourcefile:'account-save-test.ts',loader:'ts'},bundle:true,platform:'node',format:'esm',write:false});
await import(`data:text/javascript;base64,${Buffer.from(result.outputFiles[0].text).toString('base64')}`);
