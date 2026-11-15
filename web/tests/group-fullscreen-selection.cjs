const fs = require('node:fs');
const path = require('node:path');
// 仅用 mock 测试整组选择与窗口成员子集规划，不启动游戏。
(async () => {
  const base = process.env.UI_URL || 'http://127.0.0.1:5173';
  const debuggerUrl = process.env.CDP_URL || 'http://127.0.0.1:9349';
  const pages = await (await fetch(debuggerUrl + '/json/list')).json();
  const page = pages.find((p) => p.url.startsWith(base));
  if (!page) throw Error('找不到独立 mock 测试页');
  const ws = new WebSocket(page.webSocketDebuggerUrl);
  await new Promise((resolve) => { ws.onopen = resolve; });
  let sequence = 0;
  const requests = new Map();
  const send = (method, params = {}) => new Promise((resolve, reject) => {
    const id = ++sequence;
    requests.set(id, { resolve, reject });
    ws.send(JSON.stringify({ id, method, params }));
  });
  ws.onmessage = (e) => {
    const msg = JSON.parse(e.data);
    const request = requests.get(msg.id);
    if (!request) return;
    requests.delete(msg.id);
    msg.error ? request.reject(Error(JSON.stringify(msg.error))) : request.resolve(msg.result);
  };
  const evaluate = async (expression) => {
    const result = await send('Runtime.evaluate', { expression, returnByValue: true, awaitPromise: true });
    if (result.exceptionDetails) throw Error(result.exceptionDetails.exception?.description || JSON.stringify(result.exceptionDetails));
    return result.result?.value;
  };
  const results = [];
  async function test(name, body) {
    const value = await evaluate(`(async()=>{
      const s=document.querySelector('.sheet').__vueParentComponent.setupState;
      const mock=window.__groupMock;
      const pause=ms=>new Promise(resolve=>setTimeout(resolve,ms));
      const check=(value,message)=>{if(!value)throw Error(message)};
      const selected=()=>[...document.querySelectorAll('.layout-options button')].filter(b=>b.getAttribute('aria-pressed')==='true').map(b=>b.querySelector('b').textContent);
      const prepare=async(count,windowCount)=>{
        s.draft.slots=s.store.accounts.slice(0,count).map((a,i)=>({accountKey:a.id,name:a.name,mode:'',fps:0,display:i<windowCount?'windowed':'',disabled:false,x:0,y:0,w:0,h:0,hasRect:false,zOrder:i+1}));
        s.draft.layout='cascade:cols';s.draft.defDisplay='windowed';await pause(0);await s.planner.ensure();
      };
      ${body}
    })()`);
    results.push({ name, ok: true, value });
    console.log('PASS', name, JSON.stringify(value ?? null));
  }
  try {
    await send('Page.enable');
    await send('Page.navigate', { url: base + '/?mock=1' });
    await new Promise((resolve) => setTimeout(resolve, 900));
    await evaluate(`document.querySelector('button[title="启动组"]').click()`);
    await new Promise((resolve) => setTimeout(resolve, 100));
    await test('全屏选择立即高亮，不被旧规划及迟到响应覆盖', `
      await prepare(7,7);mock.planDelay=160;
      [...document.querySelectorAll('.layout-options button')].find(b=>b.textContent.includes('专注全屏')).click();
      await pause(10);check(s.draft.layout==='fullscreen','未保存全屏意图');check(selected().join()==='专注全屏','立即高亮仍是窗口子方案');
      check(!document.querySelector('.canvas'),'全屏选择后窗口成员位置应默认收起');
      await s.planner.ensure();await pause(200);check(s.layoutPlan.mode==='cascade:cols','测试未产生标题子方案');check(selected().join()==='专注全屏','规划返回覆盖全屏选中态');mock.planDelay=0;
      return {selected:selected(),subset:s.layoutPlan.mode,windows:s.windows.length};
    `);
    await test('保存并重开保持全屏，成员窗口覆盖仍保留', `
      mock.calls=[];await s.commit();check(!s.error,'保存失败'+s.error);
      const saved=mock.calls.find(c=>c.path.endsWith('/groups/save'));check(saved.body.layout==='fullscreen','实际保存被窗口方案改写');
      check(saved.body.slots.every(x=>x.display==='windowed'),'成员显式窗口被意外改掉');
      s.loadGroup(s.draft.id);await pause(0);await s.planner.ensure();check(selected().join()==='专注全屏','重开选中态丢失');
      s.showSettings=true;await pause(10);const select=[...document.querySelectorAll('.settings label')].find(e=>e.textContent.includes('排列方案')).querySelector('select');check(select.value==='fullscreen','下拉框与按钮不一致');
      return saved.body.layout;
    `);
    await test('纯全屏无窗口子集不显示缺窗口警告', `
      for(const slot of s.draft.slots)slot.display='';await pause(0);await s.planner.ensure();await pause(10);
      check(s.windows.length===0,'纯全屏仍有窗口');check(selected().join()==='专注全屏','纯全屏高亮错误');
      check(!document.querySelector('.plan-reason')?.textContent.includes('没有需要排列的窗口'),'显示无关窗口警告');
      for(const selector of ['.position .canvas','.position .coords','.position-mode','.position-actions','.plan-environment'])check(!document.querySelector(selector),'纯全屏仍显示位置设置：'+selector);
      check(document.querySelector('.fullscreen-summary'),'缺全屏说明');check(document.querySelector('.fullscreen-summary .layer-actions'),'全屏层级入口丢失');
      check(document.querySelector('.thead').textContent.includes('层级'),'全屏仍显示坐标表头');return s.windows.length;
    `);
    await test('混合成员保留窗口规划，切回窗口方案正常', `
      s.draft.slots[0].display='windowed';s.draft.slots[1].display='windowed';await pause(0);await s.planner.ensure();await pause(10);
      check(s.windows.length===2&&s.layoutPlan.rects.length===2,'窗口子集规划丢失');check(selected().join()==='专注全屏','混合成员覆盖整组选择');
      check(!document.querySelector('.canvas'),'全屏配置默认应收起窗口位置');
      document.querySelector('.window-overrides .textbtn').click();await pause(10);
      check(document.querySelector('.canvas')&&document.querySelectorAll('.wblock').length===2,'窗口成员位置编辑未按子集显示');
      check(document.querySelector('.position .sectionhead h3').textContent==='窗口成员的位置','混合成员位置范围不明确');
      s.chooseLayout('cascade:rows');await pause(0);await s.planner.ensure();await pause(10);
      check(s.draft.layout==='cascade:rows'&&selected().join()==='画面多露出','切回窗口方案失败');return selected();
    `);
    await test('切换全屏只收起设置，不丢已保存坐标与层级', `
      const slot=s.draft.slots[0];Object.assign(slot,{x:120,y:90,w:1024,h:720,hasRect:true,zOrder:5,display:''});
      for(const other of s.draft.slots)other.display='';s.chooseLayout('fullscreen');await pause(0);await s.planner.ensure();await pause(10);
      check(!document.querySelector('.canvas'),'全屏画布未收起');check(slot.hasRect&&slot.x===120&&slot.zOrder===5,'收起位置控件修改了记录');
      s.chooseLayout('cascade:cols');await pause(0);await s.planner.ensure();await pause(10);
      check(document.querySelector('.canvas')&&document.querySelector('.position-mode'),'切回窗口没有恢复位置设置');
      check(slot.hasRect&&slot.x===120&&slot.y===90&&slot.zOrder===5,'切回窗口坐标或层级丢失');return true;
    `);
    const output = path.resolve(__dirname, '../../tmp');
    fs.mkdirSync(output, { recursive: true });
    fs.writeFileSync(path.join(output, 'group-fullscreen-selection-results.json'), JSON.stringify(results, null, 2));
  } finally { ws.close(); }
})().catch((error) => { console.error(error); process.exitCode = 1; });
