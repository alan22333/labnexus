// 前端诊断脚本:mock 浏览器环境,加载 app.js,逐步触发各模块,输出错误
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));

// ---- mock 浏览器环境 ----
const listeners = {};
globalThis.localStorage = {
  _s: {}, getItem: k => globalThis.localStorage._s[k] || null,
  setItem: (k, v) => { globalThis.localStorage._s[k] = v; },
  removeItem: k => { delete globalThis.localStorage._s[k]; },
};
function fakeEl() {
  return {
    classList: { add() {}, remove() {}, toggle() {}, contains: () => false },
    value: '', innerHTML: '', textContent: '', placeholder: '',
    style: {}, dataset: {}, checked: false,
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
globalThis.fetch = async (url, opts) => {
  console.log('  [fetch]', opts && opts.method || 'GET', url);
  return { ok: true, status: 200, json: async () => ({}) };
};
globalThis.alert = m => console.log('  [alert]', m);
globalThis.confirm = () => true;
globalThis.prompt = () => '测试';
globalThis.URLSearchParams = URLSearchParams;

console.log('== 1. 加载 app.js ==');
try {
  const code = fs.readFileSync(path.join(__dirname, '..', 'web', 'app.js'), 'utf8');
  eval(code);
  console.log('  顶层执行 OK');
} catch (e) {
  console.log('  ❌ 顶层执行错误:', e.message);
  process.exit(1);
}

console.log('== 2. 触发 DOMContentLoaded(App.init) ==');
try {
  if (listeners.DOMContentLoaded) listeners.DOMContentLoaded();
  console.log('  init OK(无 token,应停留登录页)');
} catch (e) {
  console.log('  ❌ init 错误:', e.message);
}

console.log('== 3. 模拟登录(Auth.login) ==');
try {
  const fakeForm = { preventDefault() {}, target: {} };
  globalThis.FormData = class {
    constructor() { this.map = { username: 'alice', password: 'password123' }; }
    get(k) { return this.map[k]; }
    entries() { return Object.entries(this.map); }
  };
  await window.Auth.login(fakeForm);
  console.log('  登录 OK');
} catch (e) {
  console.log('  ❌ 登录错误:', e.message, '\n', e.stack);
}

console.log('== 4. 触发各视图 render ==');
for (const v of ['feed', 'space', 'resources', 'projects', 'tags']) {
  try {
    const fn = { feed: () => window.Feed.render(), space: () => window.Space.render(), resources: () => window.Resources.render(), projects: () => window.Projects.render(), tags: () => window.Tags.render() }[v];
    await fn();
    console.log('  ' + v + '.render OK');
  } catch (e) {
    console.log('  ❌ ' + v + '.render 错误:', e.message);
  }
}

console.log('== 5. 搜索 ==');
try {
  document.getElementById('search-input').value = '测试';
  await window.Search.run();
  console.log('  search OK');
} catch (e) {
  console.log('  ❌ search 错误:', e.message);
}
console.log('== 诊断完成 ==');
