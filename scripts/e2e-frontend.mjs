// 前端端到端验证:mock DOM,真实 API(localhost:8080),完整走一遍业务流
// 前置:服务已启动(made run)且 DB 可写
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const BASE = 'http://localhost:8080';

// ---- mock DOM ----
const listeners = {};
let lastHTML = '';
globalThis.localStorage = {
  _s: {}, getItem: k => globalThis.localStorage._s[k] || null,
  setItem: (k, v) => { globalThis.localStorage._s[k] = v; },
  removeItem: k => { delete globalThis.localStorage._s[k]; },
};
function fakeEl() {
  return {
    classList: { add() {}, remove() {}, toggle() {}, contains: () => false },
    value: '', set innerHTML(v) { lastHTML = v; }, get innerHTML() { return lastHTML; },
    textContent: '', placeholder: '', style: {}, dataset: {},
    addEventListener() {}, appendChild() {},
  };
}
const els = {};
globalThis.document = {
  getElementById: id => (els[id] || (els[id] = fakeEl())),
  querySelectorAll: () => [],
  addEventListener: (ev, fn) => { listeners[ev] = fn; },
};
globalThis.window = globalThis;
globalThis.alert = m => console.log('  [alert]', m);
globalThis.confirm = () => true;
globalThis.prompt = () => 'e2e测试';

// ---- 真实 fetch 代理 ----
const nativeFetch = globalThis.fetch;
globalThis.fetch = async (url, opts = {}) => {
  const full = url.startsWith('http') ? url : BASE + url;
  const headers = { 'Content-Type': 'application/json', ...(opts.headers || {}) };
  const res = await nativeFetch(full, { ...opts, headers });
  const text = await res.text();
  let data = null;
  try { data = text ? JSON.parse(text) : null; } catch (_) { /* 非 JSON */ }
  if (!res.ok || process.env.DBG) console.log('  [debug fetch]', opts.method || 'GET', url, '→', res.status, String(text).slice(0, 120));
  return { ok: res.ok, status: res.status, json: async () => data, text: async () => text };
};

let failures = 0;
function check(name, cond, extra = '') {
  if (cond) console.log('  ✅ ' + name);
  else { console.log('  ❌ ' + name + (extra ? ' → ' + extra : '')); failures++; }
}

// ---- 加载 app.js ----
console.log('== 加载 app.js ==');
eval(fs.readFileSync(path.join(__dirname, '..', 'web', 'app.js'), 'utf8'));

console.log('== 1. 注册(需邀请码 E2E-1,由外部 psql 插入)==');
const regForm = { preventDefault() {}, target: {} };
globalThis.FormData = class {
  constructor() { this.map = { invite_code: 'E2E-1', username: 'e2e_user', display_name: 'E2E', password: 'password123' }; }
  get(k) { return this.map[k]; }
  entries() { return Object.entries(this.map); }
  [Symbol.iterator]() { return Object.entries(this.map)[Symbol.iterator](); }
};
try {
  await window.Auth.register(regForm);
  const me = await window.api('/me');
  check('注册并验证 token 可用', me && me.user && me.user.username === 'e2e_user', JSON.stringify(me).slice(0, 120));
} catch (e) { check('注册', false, e.message); }

console.log('== 2. 信息流(先发一帖)==');
try {
  // 直接调用 API 发公开帖(模拟编辑器保存的 payload)
  const doc = await window.api('/me/documents', { method: 'POST', body: { title: 'E2E公开帖', content: '内容', visibility: 'public' } });
  check('发公开帖', doc && doc.document && doc.document.id);
  await window.Feed.render();
  check('信息流渲染出帖子', lastHTML.includes('E2E公开帖'), lastHTML.slice(0, 200));
} catch (e) { check('信息流', false, e.message); }

console.log('== 3. 点赞与评论 ==');
try {
  const feed = await window.api('/feed');
  const docId = feed.documents[0].id;
  await window.api('/documents/' + docId + '/reactions', { method: 'POST', body: { emoji: '👍' } });
  const c = await window.api('/documents/' + docId + '/comments', { method: 'POST', body: { content: 'E2E评论' } });
  check('点赞+评论', c && c.comment && c.comment.author);
} catch (e) { check('点赞评论', false, e.message); }

console.log('== 4. 我的空间(目录树)==');
try {
  await window.Space.render();
  const treeOK = typeof window.Space.loadTree === 'function';
  await window.Space.addFolder(); // prompt 返回 'e2e测试'
  check('空间渲染', treeOK);
} catch (e) { check('空间', false, e.message); }

console.log('== 5. 资源库 ==');
try {
  await window.Resources.render();
  const listHTML = document.getElementById('res-list').innerHTML;
  const okEmpty = listHTML.includes('暂无资源');
  const okList = listHTML.includes('类型:链接') || listHTML.includes('类型:文件');
  check('资源库渲染(空态或列表)', okEmpty || okList, listHTML.slice(0, 120));
} catch (e) { check('资源库', false, e.message); }

console.log('== 6. 项目 ==');
try {
  await window.Projects.render();
  check('项目渲染', lastHTML.includes('项目'));
} catch (e) { check('项目', false, e.message); }

console.log('== 7. 标签 ==');
try {
  await window.Tags.render();
  check('标签渲染', lastHTML.includes('标签'));
} catch (e) { check('标签', false, e.message); }

console.log('== 8. 搜索(跨类型)==');
try {
  document.getElementById('search-input').value = 'E2E';
  await window.Search.run();
  check('搜索渲染', lastHTML.includes('E2E公开帖'), lastHTML.slice(0, 200));
} catch (e) { check('搜索', false, e.message); }

console.log(failures === 0 ? '\nE2E 全部通过 ✅' : '\nE2E 失败 ' + failures + ' 项 ❌');
process.exit(failures === 0 ? 0 : 1);
