/* LabNexus 前端逻辑(原生 JS,对接全部后端 API) */
'use strict';

// ===== API 封装 =====
let token = localStorage.getItem('ln_token') || '';

async function api(path, { method = 'GET', body, form } = {}) {
  const headers = {};
  if (body) headers['Content-Type'] = 'application/json';
  if (token) headers['Authorization'] = 'Bearer ' + token;
  const res = await fetch('/api' + path, {
    method, headers,
    body: form ? form : (body ? JSON.stringify(body) : undefined),
    credentials: 'same-origin',
  });
  if (res.status === 401 && token && !path.startsWith('/auth/')) {
    // 尝试 refresh 一次
    const r = await fetch('/api/auth/refresh', { method: 'POST', credentials: 'same-origin' });
    if (r.ok) {
      const d = await r.json();
      token = d.access_token;
      localStorage.setItem('ln_token', token);
      return api(path, { method, body, form });
    }
    Auth.logout(true);
    throw new Error('登录已过期,请重新登录');
  }
  const data = await res.json().catch(() => null);
  if (!res.ok) throw new Error((data && data.error && data.error.message) || ('HTTP ' + res.status));
  return data;
}

function errMsg(e) {
  return e && e.message ? e.message : String(e);
}
function showMsg(id, text) {
  const el = document.getElementById(id);
  if (el) el.textContent = text;
}
function esc(s) {
  if (s == null) return '';
  return String(s).replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
}

// ===== 认证 =====
const Auth = {
  switchTab(mode) {
    document.getElementById('login-form').classList.toggle('hidden', mode !== 'login');
    document.getElementById('register-form').classList.toggle('hidden', mode !== 'register');
    document.getElementById('tab-login').classList.toggle('active', mode === 'login');
    document.getElementById('tab-register').classList.toggle('active', mode === 'register');
    showMsg('login-msg', '');
  },
  async login(e) {
    e.preventDefault();
    const fd = new FormData(e.target);
    try {
      const d = await api('/auth/login', { method: 'POST', body: Object.fromEntries(fd) });
      token = d.access_token;
      localStorage.setItem('ln_token', token);
      this.enter();
    } catch (err) { showMsg('login-msg', errMsg(err)); }
  },
  async register(e) {
    e.preventDefault();
    const fd = new FormData(e.target);
    try {
      const d = await api('/auth/register', { method: 'POST', body: Object.fromEntries(fd) });
      token = d.access_token;
      localStorage.setItem('ln_token', token);
      this.enter();
    } catch (err) { showMsg('login-msg', errMsg(err)); }
  },
  async logout(silent) {
    try { await api('/auth/logout', { method: 'POST' }); } catch (_) { /* ignore */ }
    if (!silent) { localStorage.removeItem('ln_token'); token = ''; location.reload(); }
  },
  async enter() {
    document.getElementById('login-view').classList.add('hidden');
    document.getElementById('app-view').classList.remove('hidden');
    try {
      const me = await api('/me');
      document.getElementById('current-user').textContent = me.user.display_name + ' (' + me.user.role + ')';
    } catch (_) { /* ok */ }
    App.nav('feed');
  },
  init() {
    if (token) this.enter();
  },
};

// ===== 视图切换 =====
const App = {
  current: 'feed',
  nav(view) {
    this.current = view;
    document.querySelectorAll('.nav-item').forEach(b => b.classList.toggle('active', b.dataset.view === view));
    const main = document.getElementById('main');
    main.innerHTML = '<div class="empty">加载中…</div>';
    // 箭头函数包裹保留 this(直接取函数引用会导致 this 丢失)
    const fn = {
      feed: () => Feed.render(),
      space: () => Space.render(),
      resources: () => Resources.render(),
      projects: () => Projects.render(),
      tags: () => Tags.render(),
    }[view];
    if (fn) fn();
  },
  init() {
    Auth.init();
  },
};

// ===== 信息流 =====
const Feed = {
  async render() {
    const main = document.getElementById('main');
    main.innerHTML = `
      <div class="toolbar">
        <button class="btn primary" onclick="Editor.open()">✏️ 发帖 / 写笔记</button>
        <button class="btn" onclick="Feed.render()">刷新</button>
      </div>
      <div id="feed-list"></div>`;
    await this.load();
  },
  async load(sort = 'latest') {
    try {
      const d = await api('/feed?sort=' + sort);
      const list = document.getElementById('feed-list');
      if (!list) return;
      await getMyId();
      if (!d.documents.length) { list.innerHTML = '<div class="empty">还没有公开帖子,发第一篇吧!</div>'; return; }
      list.innerHTML = d.documents.map(doc => `
        <div class="card">
          <h3>${esc(doc.title)}</h3>
          <div class="meta">
            <span>👤 ${esc(doc.author ? doc.author.display_name : '?')}</span>
            <span>🕐 ${new Date(doc.created_at).toLocaleString()}</span>
            ${(doc.tags || []).map(t => `<span class="tag-pill">${esc(t.name)}</span>`).join('')}
          </div>
          <div class="content-preview">${esc((doc.content || '').slice(0, 200))}</div>
          <div class="actions">
            <button class="btn small" onclick="Feed.like('${doc.id}')">👍 ${doc.reactions_count || 0}</button>
            <button class="btn small" onclick="Feed.toggleComments('${doc.id}')">💬 ${doc.comments_count || 0}</button>
            <button class="btn small" onclick="Feed.view('${doc.id}')">查看</button>
            ${doc.author_id === getMyId() ? `<button class="btn small" onclick="Editor.edit('${doc.id}')">编辑</button>` : ''}
          </div>
          <div id="comments-${doc.id}" class="hidden"></div>
        </div>`).join('');
    } catch (e) { document.getElementById('feed-list').innerHTML = `<div class="empty">${esc(errMsg(e))}</div>`; }
  },
  async like(docId) {
    try {
      await api('/documents/' + docId + '/reactions', { method: 'POST', body: { emoji: '👍' } });
      this.load();
    } catch (e) { alert(errMsg(e)); }
  },
  async toggleComments(docId) {
    const box = document.getElementById('comments-' + docId);
    if (!box.classList.contains('hidden')) { box.classList.add('hidden'); return; }
    try {
      const d = await api('/documents/' + docId + '/comments');
      box.classList.remove('hidden');
      box.innerHTML = (d.comments || []).map(c => `
        <div class="comment"><span class="author">${esc(c.author ? c.author.display_name : '?')}</span> ${esc(c.content)}
          ${c.author_id === getMyId() ? `<button class="btn small danger" onclick="Feed.delComment('${c.id}','${docId}')">删</button>` : ''}
        </div>`).join('')
        + `<div class="row"><input id="comment-input-${docId}" placeholder="写评论…">
          <button class="btn small primary" onclick="Feed.comment('${docId}')">发送</button></div>`;
    } catch (e) { alert(errMsg(e)); }
  },
  async comment(docId) {
    const input = document.getElementById('comment-input-' + docId);
    if (!input.value.trim()) return;
    try {
      await api('/documents/' + docId + '/comments', { method: 'POST', body: { content: input.value } });
      this.toggleComments(docId);
      this.load();
    } catch (e) { alert(errMsg(e)); }
  },
  async delComment(commentId, docId) {
    try { await api('/comments/' + commentId, { method: 'DELETE' }); this.load(); } catch (e) { alert(errMsg(e)); }
  },
  async view(docId) {
    try {
      const d = await api('/documents/' + docId);
      const main = document.getElementById('main');
      main.innerHTML = `
        <div class="card">
          <button class="btn ghost" onclick="App.nav('feed')">← 返回</button>
          <h2>${esc(d.title)}</h2>
          <div class="meta"><span>👤 ${esc(d.author ? d.author.display_name : '?')}</span>
            <span>${d.visibility === 'public' ? '公开' : '私有'}</span>
            ${(d.tags || []).map(t => `<span class="tag-pill">${esc(t.name)}</span>`).join('')}</div>
          <div class="content-preview" style="white-space:pre-wrap">${esc(d.content)}</div>
        </div>`;
    } catch (e) { alert(errMsg(e)); }
  },
};

// ===== 我的空间 =====
const Space = {
  currentFolder: null,
  folders: [],
  async render() {
    const main = document.getElementById('main');
    main.innerHTML = `
      <div class="toolbar">
        <h2>我的空间</h2>
        <button class="btn primary" onclick="Editor.open()">✏️ 新建文档</button>
        <button class="btn" onclick="Space.render()">刷新</button>
      </div>
      <div class="row">
        <input id="folder-name" placeholder="新目录名称"><button class="btn" onclick="Space.addFolder()">建目录</button>
      </div>
      <div class="row" style="align-items:flex-start">
        <div id="folder-tree" style="min-width:220px;border:1px solid var(--border);border-radius:8px;padding:8px"></div>
        <div id="space-docs" style="flex:1"></div>
      </div>`;
    await this.loadTree();
  },
  async loadTree() {
    try {
      const d = await api('/me/space');
      this.folders = flatten(d.folders || []);
      const tree = document.getElementById('folder-tree');
      tree.innerHTML = `<div class="tree-item ${!this.currentFolder ? 'active' : ''}" onclick="Space.selectFolder(null)">📂 全部文档</div>`
        + renderTree(d.folders || [], 0, this.currentFolder);
      await this.loadDocs();
    } catch (e) { alert(errMsg(e)); }
  },
  selectFolder(id) {
    this.currentFolder = id;
    this.loadTree();
  },
  async addFolder() {
    const name = document.getElementById('folder-name').value.trim();
    if (!name) return;
    try {
      await api('/me/folders', { method: 'POST', body: { name, parent_id: this.currentFolder } });
      document.getElementById('folder-name').value = '';
      this.loadTree();
    } catch (e) { alert(errMsg(e)); }
  },
  async renameFolder(id) {
    const f = this.folders.find(x => x.id === id);
    const name = prompt('新名称', f ? f.name : '');
    if (!name) return;
    try { await api('/me/folders/' + id, { method: 'PATCH', body: { name } }); this.loadTree(); } catch (e) { alert(errMsg(e)); }
  },
  async delFolder(id) {
    if (!confirm('删除该目录?')) return;
    try { await api('/me/folders/' + id, { method: 'DELETE' }); this.loadTree(); } catch (e) { alert(errMsg(e)); }
  },
  async loadDocs() {
    try {
      const q = this.currentFolder ? '?folder_id=' + this.currentFolder : '';
      const d = await api('/me/documents' + q);
      const box = document.getElementById('space-docs');
      if (!(d.documents || []).length) { box.innerHTML = '<div class="empty">该目录下暂无文档</div>'; return; }
      box.innerHTML = d.documents.map(doc => `
        <div class="card">
          <h3>${esc(doc.title)} <span class="tag-pill">${doc.visibility === 'public' ? '公开' : '私有'}</span></h3>
          <div class="meta">🕐 ${new Date(doc.created_at).toLocaleString()} ${(doc.tags || []).map(t => `<span class="tag-pill">${esc(t.name)}</span>`).join('')}</div>
          <div class="actions">
            <button class="btn small" onclick="Editor.edit('${doc.id}')">编辑</button>
            <button class="btn small danger" onclick="Space.delDoc('${doc.id}')">删除</button>
          </div>
        </div>`).join('');
    } catch (e) { alert(errMsg(e)); }
  },
  async delDoc(id) {
    if (!confirm('删除该文档?')) return;
    try { await api('/documents/' + id, { method: 'DELETE' }); this.loadDocs(); } catch (e) { alert(errMsg(e)); }
  },
};

// ===== 编辑器 =====
const Editor = {
  open(folderId) {
    document.getElementById('editor-title').textContent = '新建文档';
    document.getElementById('doc-id').value = '';
    document.getElementById('doc-title').value = '';
    document.getElementById('doc-content').value = '';
    document.getElementById('doc-visibility').value = 'private';
    document.getElementById('doc-tags').value = '';
    document.getElementById('doc-folder').value = folderId || Space.currentFolder || '';
    showMsg('editor-msg', '');
    document.getElementById('editor-modal').classList.remove('hidden');
  },
  async edit(docId) {
    try {
      const d = await api('/documents/' + docId);
      document.getElementById('editor-title').textContent = '编辑文档';
      document.getElementById('doc-id').value = docId;
      document.getElementById('doc-title').value = d.title;
      document.getElementById('doc-content').value = d.content;
      document.getElementById('doc-visibility').value = d.visibility;
      document.getElementById('doc-tags').value = (d.tags || []).map(t => t.id).join(',');
      document.getElementById('doc-folder').value = d.folder_id || '';
      showMsg('editor-msg', '');
      document.getElementById('editor-modal').classList.remove('hidden');
    } catch (e) { alert(errMsg(e)); }
  },
  close() { document.getElementById('editor-modal').classList.add('hidden'); },
  async save() {
    const id = document.getElementById('doc-id').value;
    if (id === 'RESOURCE') {
      // 资源创建:visibility 字段临时充当 type
      const type = document.getElementById('doc-visibility').value;
      const title = document.getElementById('doc-title').value.trim();
      const raw = document.getElementById('doc-content').value.trim();
      const body = { type, title };
      if (type === 'link') body.url = raw;
      else if (type === 'paper') { if (/^10\./.test(raw)) body.doi = raw; else body.arxiv_id = raw; }
      try {
        await api('/resources', { method: 'POST', body });
        this.close();
        Resources.render();
      } catch (e) { showMsg('editor-msg', errMsg(e)); }
      return;
    }
    const payload = {
      title: document.getElementById('doc-title').value,
      content: document.getElementById('doc-content').value,
      visibility: document.getElementById('doc-visibility').value,
    };
    const folder = document.getElementById('doc-folder').value;
    const tags = document.getElementById('doc-tags').value.split(',').map(s => s.trim()).filter(Boolean);
    if (tags.length) payload.tag_ids = tags;
    try {
      if (id) {
        await api('/documents/' + id, { method: 'PATCH', body: payload });
      } else {
        if (folder) payload.folder_id = folder;
        await api('/me/documents', { method: 'POST', body: payload });
      }
      this.close();
      App.nav('feed');
    } catch (e) { showMsg('editor-msg', errMsg(e)); }
  },
};

// ===== 资源库 =====
const Resources = {
  async render() {
    const main = document.getElementById('main');
    main.innerHTML = `
      <div class="toolbar">
        <h2>资源库</h2>
        <select id="res-type" onchange="Resources.load()">
          <option value="">全部类型</option><option value="link">链接</option>
          <option value="paper">文献</option><option value="file">文件</option>
        </select>
        <input id="res-keyword" placeholder="关键词…" onkeydown="if(event.key==='Enter')Resources.load()" style="max-width:200px">
        <button class="btn" onclick="Resources.load()">筛选</button>
        <button class="btn primary" onclick="Resources.showCreate()">＋ 新建资源</button>
        <label class="btn">上传文件<input type="file" style="display:none" onchange="Resources.upload(this)"></label>
      </div>
      <div id="res-list"></div>`;
    await this.load();
  },
  async load() {
    try {
      const type = document.getElementById('res-type').value;
      const kw = document.getElementById('res-keyword').value;
      const q = new URLSearchParams({ type, keyword: kw }).toString();
      const d = await api('/resources?' + q);
      const list = document.getElementById('res-list');
      if (!list) return;
      if (!d.resources.length) { list.innerHTML = '<div class="empty">暂无资源,上传或添加第一个吧</div>'; return; }
      list.innerHTML = d.resources.map(r => `
        <div class="card">
          <h3>${r.type === 'link' ? '🔗' : r.type === 'paper' ? '📄' : '📎'} ${esc(r.title)}</h3>
          <div class="meta">
            <span>类型:${r.type}</span>
            ${r.url ? `<span><a href="${esc(r.url)}" target="_blank">${esc(r.url)}</a></span>` : ''}
            ${r.doi ? `<span>DOI:${esc(r.doi)}</span>` : ''}
            ${r.arxiv_id ? `<span>arXiv:${esc(r.arxiv_id)}</span>` : ''}
            <span>上传:${esc(r.uploader ? r.uploader.display_name : '?')}</span>
            ${(r.tags || []).map(t => `<span class="tag-pill">${esc(t.name)}</span>`).join('')}
          </div>
          ${r.author_id === getMyId() ? '' : ''}
        </div>`).join('');
    } catch (e) { document.getElementById('res-list').innerHTML = `<div class="empty">${esc(errMsg(e))}</div>`; }
  },
  showCreate() {
    const modal = document.getElementById('editor-modal');
    document.getElementById('editor-title').textContent = '新建资源';
    document.getElementById('doc-id').value = 'RESOURCE';
    document.getElementById('doc-title').value = '';
    document.getElementById('doc-content').value = '';
    document.getElementById('doc-visibility').value = 'link';
    document.getElementById('doc-tags').value = '';
    showMsg('editor-msg', '');
    modal.classList.remove('hidden');
    document.getElementById('doc-content').placeholder = 'URL(link) 或 DOI/arXiv(paper)…';
  },
  async upload(input) {
    const file = input.files[0];
    if (!file) return;
    const form = new FormData();
    form.append('file', file);
    try {
      await fetch('/api/resources/upload', { method: 'POST', headers: { 'Authorization': 'Bearer ' + token }, body: form });
      alert('上传成功');
      this.load();
    } catch (e) { alert(errMsg(e)); }
  },
};

// ===== 项目 =====
const Projects = {
  current: null,
  async render() {
    const main = document.getElementById('main');
    main.innerHTML = `
      <div class="toolbar">
        <h2>项目</h2>
        <button class="btn primary" onclick="Projects.create()">＋ 新建项目</button>
        <button class="btn" onclick="Projects.render()">刷新</button>
      </div>
      <div id="proj-list"></div>`;
    try {
      const d = await api('/projects');
      const list = document.getElementById('proj-list');
      if (!d.projects.length) { list.innerHTML = '<div class="empty">暂无项目</div>'; return; }
      list.innerHTML = d.projects.map(p => `
        <div class="card" style="cursor:pointer" onclick="Projects.open('${p.id}')">
          <h3>${esc(p.name)}</h3>
          <div class="meta"><span>负责人:${esc(p.owner ? p.owner.display_name : '?')}</span>
            <span>状态:${p.status}</span><span>🕐 ${new Date(p.created_at).toLocaleString()}</span></div>
          ${p.description ? `<div class="content-preview">${esc(p.description)}</div>` : ''}
        </div>`).join('');
    } catch (e) { document.getElementById('proj-list').innerHTML = `<div class="empty">${esc(errMsg(e))}</div>`; }
  },
  async create() {
    const name = prompt('项目名称');
    if (!name) return;
    try { await api('/projects', { method: 'POST', body: { name, description: '' } }); this.render(); } catch (e) { alert(errMsg(e)); }
  },
  async open(pid) {
    try {
      const d = await api('/projects/' + pid);
      this.current = d.project;
      const p = d.project;
      const main = document.getElementById('main');
      main.innerHTML = `
        <div class="toolbar"><h2>📋 ${esc(p.name)}</h2>
          <button class="btn ghost" onclick="Projects.render()">← 返回</button>
          <button class="btn" onclick="Projects.addMember()">＋ 加成员</button>
          <button class="btn" onclick="Projects.addMilestone()">🏁 里程碑</button>
          <button class="btn primary" onclick="Projects.addTask()">＋ 任务</button>
        </div>
        <div class="card"><div class="meta">
          <span>负责人:${esc(p.owner ? p.owner.display_name : '?')}</span>
          <span>成员:${(p.members || []).map(m => esc(m.user ? m.user.display_name : '?') + '(' + m.role + ')').join(', ')}</span>
        </div>
        <div class="meta">里程碑:${(p.milestones || []).map(m => esc(m.name) + (m.due_date ? ' 截止' + m.due_date : '')).join(' | ') || '无'}</div></div>
        <div class="board">${['todo', 'in_progress', 'blocked', 'done'].map(st => `
          <div class="board-col"><h4>${statusName(st)}</h4>
            ${(p.tasks || []).filter(t => t.status === st).map(t => `
              <div class="task-card">
                <div class="title">${esc(t.title)}</div>
                <div class="due">${t.due_date ? '⏰ ' + t.due_date : ''} ${t.priority === 'high' ? '🔴高' : t.priority === 'low' ? '🟢低' : ''}</div>
                <div class="meta">指派:${esc(t.assignee ? t.assignee.display_name : '未指派')}</div>
                <div class="actions">${transitionBtns(t)}</div>
              </div>`).join('') || '<div class="empty" style="padding:8px">空</div>'}
          </div>`).join('')}
        </div>`;
    } catch (e) { alert(errMsg(e)); }
  },
  async addMember() {
    const uid = prompt('用户ID(从注册用户名查询:先用对方登录看 /api/me)');
    if (!uid) return;
    try { await api('/projects/' + this.current.id + '/members', { method: 'POST', body: { user_id: uid } }); this.open(this.current.id); } catch (e) { alert(errMsg(e)); }
  },
  async addMilestone() {
    const name = prompt('里程碑名称');
    if (!name) return;
    const due = prompt('截止日期(YYYY-MM-DD,可空)') || null;
    try { await api('/projects/' + this.current.id + '/milestones', { method: 'POST', body: { name, due_date: due } }); this.open(this.current.id); } catch (e) { alert(errMsg(e)); }
  },
  async addTask() {
    const title = prompt('任务标题');
    if (!title) return;
    try { await api('/projects/' + this.current.id + '/tasks', { method: 'POST', body: { title, priority: 'medium' } }); this.open(this.current.id); } catch (e) { alert(errMsg(e)); }
  },
  async transition(taskId, status) {
    try { await api('/tasks/' + taskId + '/transition', { method: 'POST', body: { status } }); this.open(this.current.id); } catch (e) { alert(errMsg(e)); }
  },
};

// ===== 标签 =====
const Tags = {
  async render() {
    const main = document.getElementById('main');
    main.innerHTML = `
      <div class="toolbar"><h2>标签</h2>
        <input id="tag-name" placeholder="新标签名"><button class="btn primary" onclick="Tags.create()">创建</button>
      </div>
      <div id="tag-list"></div>`;
    try {
      const d = await api('/tags');
      const list = document.getElementById('tag-list');
      if (!d.tags.length) { list.innerHTML = '<div class="empty">暂无标签</div>'; return; }
      list.innerHTML = d.tags.map(t => `
        <div class="card" style="cursor:pointer" onclick="Tags.contents('${t.id}')">
          <span class="tag-pill" style="background:${esc(t.color)};color:#fff">${esc(t.name)}</span>
          <span class="meta">点击查看内容</span>
        </div>`).join('');
    } catch (e) { document.getElementById('tag-list').innerHTML = `<div class="empty">${esc(errMsg(e))}</div>`; }
  },
  async create() {
    const name = document.getElementById('tag-name').value.trim();
    if (!name) return;
    try { await api('/tags', { method: 'POST', body: { name } }); this.render(); } catch (e) { alert(errMsg(e)); }
  },
  async contents(tagId) {
    try {
      const d = await api('/tags/' + tagId + '/contents');
      const main = document.getElementById('main');
      main.innerHTML = `<button class="btn ghost" onclick="Tags.render()">← 返回</button>`
        + (d.documents || []).map(doc => `
          <div class="card"><h3>${esc(doc.title)}</h3>
            <div class="meta">👤 ${esc(doc.author ? doc.author.display_name : '?')} ${doc.visibility === 'public' ? '公开' : '私有'}</div></div>`).join('')
        || '<div class="empty">该标签下暂无内容</div>';
    } catch (e) { alert(errMsg(e)); }
  },
};

// ===== 搜索 =====
const Search = {
  async run() {
    const q = document.getElementById('search-input').value.trim();
    if (!q) return;
    try {
      const d = await api('/search?q=' + encodeURIComponent(q));
      const main = document.getElementById('main');
      const docs = d.documents || [], ress = d.resources || [], tasks = d.tasks || [];
      main.innerHTML = `<div class="toolbar"><h2>搜索:"${esc(q)}"</h2></div>
        <div class="result-group"><h4>📄 文档 (${docs.length})</h4>
          ${docs.map(x => `<div class="card"><h3>${esc(x.title)}</h3><div class="meta">👤 ${esc(x.author ? x.author.display_name : '?')} ${x.visibility === 'public' ? '公开' : '私有'}</div></div>`).join('') || '<div class="empty">无</div>'}
        </div>
        <div class="result-group"><h4>📚 资源 (${ress.length})</h4>
          ${ress.map(x => `<div class="card"><h3>${esc(x.title)}</h3><div class="meta">类型:${x.type}</div></div>`).join('') || '<div class="empty">无</div>'}
        </div>
        <div class="result-group"><h4>📋 任务 (${tasks.length})</h4>
          ${tasks.map(x => `<div class="card"><h3>${esc(x.title)}</h3><div class="meta">状态:${statusName(x.status)}</div></div>`).join('') || '<div class="empty">无</div>'}
        </div>`;
    } catch (e) { alert(errMsg(e)); }
  },
};

// ===== 工具函数 =====
let myId = null;
async function getMyId() {
  if (myId) return myId;
  try {
    const d = await api('/me');
    myId = d.user.id;
    return myId;
  } catch (_) { return ''; }
}
function flatten(nodes, depth = 0) {
  let out = [];
  (nodes || []).forEach(n => { out.push(n); out = out.concat(flatten(n.children, depth + 1)); });
  return out;
}
function renderTree(nodes, depth, selected) {
  return (nodes || []).map(n => `
    <div>
      <div class="tree-item ${n.id === selected ? 'active' : ''}" style="padding-left:${8 + depth * 16}px"
           onclick="Space.selectFolder('${n.id}')">
        📁 ${esc(n.name)}
        <span>
          <button class="btn small" onclick="event.stopPropagation();Space.renameFolder('${n.id}')">改</button>
          <button class="btn small danger" onclick="event.stopPropagation();Space.delFolder('${n.id}')">删</button>
        </span>
      </div>
      ${n.children && n.children.length ? `<div class="tree-children">${renderTree(n.children, depth + 1, selected)}</div>` : ''}
    </div>`).join('');
}
function statusName(s) {
  return { todo: '待办', in_progress: '进行中', blocked: '受阻', done: '完成', active: '进行中' }[s] || s;
}
function transitionBtns(task) {
  const map = {
    todo: [['in_progress', '▶ 开始']],
    in_progress: [['blocked', '⛔ 受阻'], ['done', '✅ 完成']],
    blocked: [['todo', '↩ 重开'], ['in_progress', '▶ 继续']],
    done: [],
  };
  return (map[task.status] || []).map(([st, label]) =>
    `<button class="btn small" onclick="Projects.transition('${task.id}','${st}')">${label}</button>`).join('');
}

// 全局初始化
window.api = api; // API 封装暴露(e2e 测试与调试用)
window.Auth = Auth; window.App = App; window.Feed = Feed; window.Space = Space;
window.Editor = Editor; window.Resources = Resources; window.Projects = Projects;
window.Tags = Tags; window.Search = Search;
document.addEventListener('DOMContentLoaded', () => App.init());
