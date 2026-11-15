const fs = require('node:fs');
const path = require('node:path');
// 只连接独立 mock 浏览器，不读取或修改真实账号。
const base = process.env.UI_URL || 'http://127.0.0.1:5173';
const cdp = process.env.CDP_URL || 'http://127.0.0.1:9351';
(async () => {
  const pages = await (await fetch(cdp + '/json/list')).json();
  const page = pages.find(p => p.url.startsWith(base));
  if (!page) throw Error('未找到独立模拟测试页');
  const ws = new WebSocket(page.webSocketDebuggerUrl);
  await new Promise(r => ws.onopen = r);
  let n = 0; const pending = new Map();
  const send = (method, params = {}) => new Promise((resolve, reject) => { const id = ++n; pending.set(id, { resolve, reject }); ws.send(JSON.stringify({ id, method, params })); });
  ws.onmessage = e => { const m = JSON.parse(e.data); if (m.id) { const p = pending.get(m.id); pending.delete(m.id); if (p) m.error ? p.reject(Error(JSON.stringify(m.error))) : p.resolve(m.result); } };
  const ev = async expression => { const r = await send('Runtime.evaluate', { expression, awaitPromise: true, returnByValue: true }); if (r.exceptionDetails) throw Error(r.exceptionDetails.exception?.description || JSON.stringify(r.exceptionDetails)); return r.result?.value; };
  const results = [];
  async function test(name, code) {
    try { const value = await ev(`(async()=>{const s=document.querySelector('.win').__vueParentComponent.setupState;const b=s.browser;const pause=ms=>new Promise(r=>setTimeout(r,ms));const check=(v,m)=>{if(!v)throw Error(m)};${code}})()`); results.push({ name, ok: true, value }); console.log('PASS', name); }
    catch (e) { results.push({ name, ok: false, error: e.message }); console.error('FAIL', name, e.message); }
  }
  try {
    await send('Page.enable'); await send('Runtime.enable');
    for (const [index, name, row] of [[0, '账号库', '.gcard'], [1, '此刻', '.queue-item'], [3, '晨白', '.banner']]) {
      await send('Page.navigate', { url: base + '/?mock=1' });
      await new Promise(r => setTimeout(r, 700));
      await ev(`import('/src/shellkit/appearance.ts').then(m=>m.activeShell.value=${index})`);
      await new Promise(r => setTimeout(r, 150));
      await send('Emulation.setDeviceMetricsOverride', { width: 1120, height: 760, deviceScaleFactor: 1, mobile: false });
      await test(name + '启动组入口打开共享管理器并返回', `const entry=document.querySelector('.groups-entry');check(entry&&entry.textContent.includes('启动组'),'缺启动组入口');entry.click();await pause(100);const sheet=document.querySelector('.sheet:not(.embedded)');check(sheet,'入口未打开启动组');const groupState=sheet.__vueParentComponent.setupState;check(groupState.store===s.store,'启动组未使用当前账号数据');check(groupState.store.groups.length===s.store.groups.length,'启动组数据不一致');sheet.querySelector('[aria-label="关闭"]').click();await pause(50);check(!document.querySelector('.sheet'),'关闭后未返回账号页');check(document.querySelector('.groups-entry'),'返回后入口丢失');`);
      await test(name + '搜索、分组、勾选保持', `check(b,'缺账号管理');b.search.value='';b.clearSelection();const a=s.store.accounts[2];s.store.toggleSelect(a.id);b.search.value=a.name;await pause(30);check(b.visibleCount.value===1,'搜索结果错误');b.view.value='tag';await pause(30);check(s.store.selectedIds.includes(a.id),'切视图丢勾选');b.search.value='';b.view.value='flat';await pause(30);check(document.querySelectorAll('${row}').length===s.store.accounts.length,'平铺缺账号');b.clearSelection();`);
      await test(name + '标签身份、组选择、折叠', `await s.store.setTags(s.store.accounts[7].id,['未分组','A|B','引"号']);b.view.value='tag';await pause(30);const real=b.sections.value.find(x=>x.tag==='未分组'),none=b.sections.value.find(x=>x.untagged);check(real&&none&&real.key!==none.key,'同名分组混淆');const sec=b.sections.value.find(x=>x.rows.length>1);b.selectSection(sec);const count=b.selectedCount.value;b.toggleSection(sec);await pause(20);check(b.selectedCount.value===count,'折叠改变勾选');b.toggleSection(sec);b.clearSelection();`);
      await test(name + '账号与标签组排序保存失败回滚', `b.view.value='flat';b.sort.value='custom';const ids=s.store.accounts.map(a=>a.id);await b.moveAccount(b.sections.value[0],ids[0],1);check(s.store.accounts[1].id===ids[0],'账号排序未保存');const before=s.store.accounts.map(a=>a.id);window.__accountMock.reorderError='模拟失败';await b.moveAccount(b.sections.value[0],before[0],1);check(JSON.stringify(s.store.accounts.map(a=>a.id))===JSON.stringify(before),'失败未回滚');check(b.error.value,'失败未反馈');window.__accountMock.reorderError='';b.view.value='tag';const sec=b.sections.value.find(x=>x.tag===b.groupOrder.value[1]);const tag=sec.tag;await b.moveGroup(sec,-1);check(b.groupOrder.value[0]===tag,'标签组排序未保存');b.search.value=s.store.accounts[0].name;await pause(20);check([...document.querySelectorAll('.section-moves button')].every(e=>e.disabled),'搜索时可错移隐藏标签组');b.search.value='';b.view.value='flat';`);
      await test(name + '查看排序不改手动顺序', `const ids=s.store.accounts.map(a=>a.id);for(const sort of ['name','recent','running']){b.sort.value=sort;await pause(10);check(b.sections.value.every(sec=>!sec.draggable),'派生排序可修改');check(JSON.stringify(ids)===JSON.stringify(s.store.accounts.map(a=>a.id)),'查看改变顺序');}b.sort.value='custom';`);
      await test(name + '打码不泄漏名称与头像', `if(!s.store.maskAll)s.store.toggleMask();await pause(50);const phone=window.__mock.accounts.find(a=>a.name==='13800138000');check(phone,'缺手机号样例');check(!document.querySelector('.win').innerText.includes('13800138000'),'文本泄漏');check(![...document.querySelectorAll('.win [title],.win [aria-label]')].some(e=>(e.title+' '+e.getAttribute('aria-label')).includes('13800138000')),'提示泄漏');`);
      await test(name + '标签编辑等待保存与失败保留草稿', `b.view.value='flat';b.search.value='';await pause(30);const host=document.querySelector('.account-tags:has(.edit-tags)');check(host,'缺标签组件');const t=host.__vueParentComponent.setupState;check(t,'无标签状态');t.editing=true;await pause(20);const a=t.account||t.$props?.account||host.__vueParentComponent.props.account;t.drafts[a.id]='失败草稿';window.__accountMock.tagsError='模拟失败';await t.add();check(t.drafts[a.id]==='失败草稿'&&t.errors[a.id],'失败清草稿');window.__accountMock.tagsError='';t.drafts[a.id]='主题测试';await t.add();check(!t.drafts[a.id],'保存成功未清草稿');check(s.store.accounts.find(x=>x.id===a.id).tags.includes('主题测试'),'标签未保存');`);
      await test(name + '标签输入法、上限与重复校验', `const host=document.querySelector('.account-tags:has(.edit-tags)'),t=host.__vueParentComponent.setupState,a=host.__vueParentComponent.props.account;t.editing=true;const original=[...a.tags];window.__accountMock.calls=[];t.drafts[a.id]='输入法';t.enter(new KeyboardEvent('keydown',{key:'Enter',isComposing:true}));await pause(20);check(!window.__accountMock.calls.some(c=>c.path.endsWith('/tags')),'输入法误提交');t.drafts[a.id]='超'.repeat(17);await t.add();check(t.messages[a.id].includes('16'),'超长标签未校验');t.drafts[a.id]=a.tags[0].toUpperCase();await t.add();check(t.messages[a.id].includes('已有'),'重复标签未校验');await s.store.setTags(a.id,Array.from({length:8},(_,i)=>'标签'+i));await pause(20);check(host.querySelector('input').disabled,'8标签未禁止新增');await s.store.setTags(a.id,original);`);
      for (const width of [680, 900, 1120]) {
        await send('Emulation.setDeviceMetricsOverride', { width, height: 760, deviceScaleFactor: 1, mobile: false });
        await test(name + '长名标签无横溢出 ' + width, `b.view.value='flat';b.search.value='';if(s.store.maskAll)s.store.toggleMask();const a=window.__mock.accounts[0];a.name='很长的账号名称'.repeat(12);await s.store.setTags(a.id,['很长的标签文字一','很长的标签文字二']);await pause(1100);const win=document.querySelector('.win');check(win.scrollWidth<=win.clientWidth+2,'主题整体横溢出');const bad=[...document.querySelectorAll('${row},.account-tags,.browser-controls,.account-section-header')].filter(e=>e.clientWidth&&e.scrollWidth>e.clientWidth+2).map(e=>e.className);check(!bad.length,'局部溢出 '+bad.join(','));return {width:win.clientWidth};`);
      }
    }
    await test('跨主题保留账号、标签、顺序和勾选', `const a=s.store.accounts[0];b.clearSelection();s.store.toggleSelect(a.id);await s.store.setTags(a.id,['跨主题标签']);window.__themeExpected={ids:s.store.accounts.map(a=>a.id),id:a.id};const mod=await import('/src/shellkit/appearance.ts');for(const i of [0,1,2,3]){mod.activeShell.value=i;await pause(100);const state=document.querySelector('.win').__vueParentComponent.setupState;check(JSON.stringify(state.store.accounts.map(a=>a.id))===JSON.stringify(window.__themeExpected.ids),'切主题顺序变更');check(state.store.selectedIds.includes(a.id),'切主题丢勾选');check(state.store.accounts.find(x=>x.id===a.id).tags.includes('跨主题标签'),'切主题标签丢失');}check(document.querySelector('.bbar.light'),'晨白批量条未跟随配色');const bar=document.querySelector('.bbar').getBoundingClientRect();check(bar.left>=0&&bar.right<=innerWidth,'批量条超出窗口');`);
    fs.writeFileSync(path.resolve(__dirname, '../../tmp/account-theme-results.json'), JSON.stringify(results, null, 2));
    console.log('TOTAL', results.filter(r => r.ok).length, '/', results.length);
    if (results.some(r => !r.ok)) process.exitCode = 1;
  } finally { ws.close(); }
})().catch(e => { console.error(e); process.exitCode = 1; });
