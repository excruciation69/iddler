'use strict';
// Interface building blocks: buttons, toggles, dialogs, menus, tables, QR codes.

// ================================================================ ui atoms

let toastTimer;
function toast(text) {
  document.querySelector('.toast')?.remove();
  const el = h('div.toast', { text });
  document.body.append(el);
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => el.remove(), 2600);
}

function btn(label, onclick, cls = '', icon) {
  return h('button.btn' + (cls ? '.' + cls.split(' ').join('.') : ''), { type: 'button', onclick }, icon && ic(icon), label && h('span', { text: label }));
}

// Changes a button only when its look really changes: rebuilding it every second would
// restart hover effects and make it flicker under the cursor.
function setButton(el, icon, label, cls) {
  const k = icon + '|' + label + '|' + cls;
  if (el.dataset.k === k) return;
  el.dataset.k = k;
  el.className = cls;
  fill(el, ic(icon), h('span', { text: label }));
}

function makeToggle(kind, label, checked, onchange) {
  const inp = h('input', { type: kind === 'radio' ? 'radio' : 'checkbox' });
  inp.checked = !!checked;
  inp.addEventListener('change', () => onchange(inp.checked));
  const mark = kind === 'switch' ? h('span.track') : kind === 'radio' ? h('span.dot') : h('span.box');
  const el = h('label.' + kind, null, inp, mark, label && h('span', { text: label }));
  el.input = inp;
  el.set = v => { inp.checked = !!v; };
  return el;
}
const check = (label, v, fn) => makeToggle('check', label, v, fn);
const toggle = (label, v, fn) => makeToggle('switch', label, v, fn);

function field(props) {
  const area = props.area;
  delete props.area;
  return area ? h('textarea.field', props) : h('input.field', { type: 'text', ...props });
}
function search(placeholder, oninput) {
  const inp = field({ placeholder, oninput: e => oninput(e.target.value.trim().toLowerCase()) });
  return h('div.search', null, ic('search'), inp);
}

// The letter stays underneath, so a missing or slow picture never leaves an empty square.
function avatar(el, s) {
  const letter = (s.name || s.account || '?').slice(0, 1).toUpperCase();
  const src = s.avatar || '';
  if (el.dataset.src === src && el.dataset.l === letter) return el;
  el.dataset.src = src;
  el.dataset.l = letter;
  el.replaceChildren(letter);
  if (src) {
    const img = h('img', { src, alt: '', draggable: 'false' });
    img.onerror = () => img.remove();
    el.append(img);
  }
  return el;
}

function statusOf(s) {
  if (!s) return { led: '', text: '' };
  if (s.status === 'online') {
    if (s.blocked) return { led: 'busy', text: t('st.paused') };
    if (s.playing > 0) return { led: 'idle', text: s.cardsOn && s.cardsLeft ? t('st.cards', s.cardsLeft) : t('st.idling') };
    return { led: 'ok', text: t('st.online') };
  }
  if (s.status === 'connecting' || s.status === 'guard') return { led: 'busy', text: t('st.' + s.status) };
  return { led: '', text: t('st.offline') };
}

function closeLayers() {
  document.querySelectorAll('.overlay, .menu, .sheet, .sheet-ov').forEach(e => e.remove());
  document.querySelector('.me .who.open')?.classList.remove('open');
}

function dialog(title, body, buttons) {
  closeLayers();
  const close = () => { ov.remove(); document.removeEventListener('keydown', onKey); };
  const onKey = e => {
    if (e.key === 'Escape') close();
    if (e.key === 'Enter' && e.target.tagName !== 'TEXTAREA') foot.querySelector('.primary')?.click();
  };
  const foot = h('div.df', null, buttons.map(b => btn(b.label, async () => {
    if ((await b.onclick?.()) !== false) close();
  }, b.cls || '')));
  const box = h('div.dialog.raised', { role: 'dialog' }, h('div.dh', { text: title }), h('div.db', null, body), foot);
  const ov = h('div.overlay', { onmousedown: e => { if (e.target === ov) close(); } }, box);
  document.body.append(ov);
  document.addEventListener('keydown', onKey);
  setTimeout(() => box.querySelector('input, textarea')?.focus(), 30);
  return close;
}
function confirmDialog(title, text, label, fn) {
  dialog(title, [h('p.hint', { text })], [
    { label: t('btn.cancel'), cls: 'ghost' },
    { label, cls: 'danger', onclick: fn },
  ]);
}

function popover(anchor, items) {
  closeLayers();
  const m = h('div.menu.raised', null, items.map(it => it.text
    ? h('div.hint', { style: 'padding:8px 10px', text: it.text })
    : h('button', { type: 'button', onclick: () => { m.remove(); it.onclick(); } }, ic(it.icon), h('span', { text: it.label }))));
  document.body.append(m);
  const r = anchor.getBoundingClientRect();
  const w = m.offsetWidth, hh = m.offsetHeight;
  m.style.left = Math.max(8, Math.min(r.left, innerWidth - w - 8)) + 'px';
  m.style.top = (r.bottom + 6 + hh > innerHeight ? r.top - hh - 6 : r.bottom + 6) + 'px';
  setTimeout(() => {
    const off = e => { if (!m.contains(e.target)) { m.remove(); document.removeEventListener('mousedown', off); } };
    document.addEventListener('mousedown', off);
  });
}

function profileMenu(anchor, id) {
  const sid = S.bots[id]?.steamID;
  popover(anchor, sid ? [
    { icon: 'globe', label: t('ava.browser'), onclick: () => openURL('https://steamcommunity.com/profiles/' + sid) },
    { icon: 'monitor', label: t('ava.steam'), onclick: () => openURL('steam://url/SteamIDPage/' + sid) },
  ] : [{ text: t('ava.noId') }]);
}

// Binds config controls to an account: they save on change and follow engine updates.
function binder(id) {
  const items = [];
  const cfg = () => S.bots[id]?.cfg || {};
  const B = {
    sync() { for (const it of items) it(); },
    switch(path, label) {
      const el = toggle(label, getp(cfg(), path), v => patch(id, mkp(path, v)));
      items.push(() => el.set(getp(cfg(), path)));
      return el;
    },
    check(path, label) {
      const el = check(label, getp(cfg(), path), v => patch(id, mkp(path, v)));
      items.push(() => el.set(getp(cfg(), path)));
      return el;
    },
    radios(path, options) {
      const name = 'r' + Math.random().toString(36).slice(2);
      const els = options.map(([val, label]) => {
        const el = makeToggle('radio', label, getp(cfg(), path) === val, on => on && patch(id, mkp(path, val)));
        el.input.name = name;
        items.push(() => el.set(getp(cfg(), path) === val));
        return el;
      });
      return h('div.col', null, els);
    },
    input(path, props = {}, conv = v => v) {
      const el = field({ ...props, value: getp(cfg(), path) ?? '' });
      el.addEventListener('change', () => {
        const v = conv(el.value.trim());
        if (v === null) { el.value = getp(cfg(), path) ?? ''; return; }
        el.value = v;
        patch(id, mkp(path, v));
      });
      items.push(() => { if (!isFocused(el)) el.value = getp(cfg(), path) ?? ''; });
      return el;
    },
    num(path, min, max) {
      return B.input(path, { class: 'num', inputmode: 'numeric' }, v => {
        const n = parseInt(v, 10);
        return Number.isFinite(n) ? Math.min(max, Math.max(min, n)) : null;
      });
    },
    time(path) { return B.input(path, { class: 'time', maxlength: 5, placeholder: '00:00' }, normTime); },
    lines(path, props = {}) {
      const el = field({ area: true, ...props, value: (getp(cfg(), path) || []).join('\n') });
      el.addEventListener('change', () => patch(id, mkp(path, el.value.split('\n').map(s => s.trim()).filter(Boolean))));
      items.push(() => { if (!isFocused(el)) el.value = (getp(cfg(), path) || []).join('\n'); });
      return el;
    },
    select(path, options) {
      const el = h('select.field', { onchange: () => patch(id, mkp(path, conv(el.value))) },
        options.map(([v, l]) => h('option', { value: String(v), text: l })));
      const conv = v => (typeof options[0][0] === 'number' ? Number(v) : v);
      items.push(() => { if (!isFocused(el)) el.value = String(getp(cfg(), path)); });
      el.value = String(getp(cfg(), path));
      return el;
    },
  };
  return B;
}
// A list you reorder by dragging rows (mouse or finger). Rows slide out of the way as you drag.
function sortable(items, render, onDone) {
  const box = h('div.sortable');
  const rows = items.map((it, i) => { const r = render(it, i); r.dataset.i = i; box.append(r); return r; });
  rows.forEach((row, i) => {
    row.addEventListener('pointerdown', e => {
      if (e.button !== 0) return;
      e.preventDefault();
      const step = row.offsetHeight + 8, startY = e.clientY;
      let to = i;
      row.setPointerCapture(e.pointerId);
      row.classList.add('dragging');
      box.classList.add('active');
      const moveTo = ev => {
        const dy = ev.clientY - startY;
        to = Math.max(0, Math.min(rows.length - 1, Math.round(i + dy / step)));
        row.style.transform = `translateY(${dy}px) scale(1.02)`;
        rows.forEach((o, j) => {
          if (o === row) return;
          const shift = j > i && j <= to ? -step : j < i && j >= to ? step : 0;
          o.style.transform = shift ? `translateY(${shift}px)` : '';
        });
      };
      const end = () => {
        row.removeEventListener('pointermove', moveTo);
        row.removeEventListener('pointerup', end);
        row.removeEventListener('pointercancel', end);
        box.classList.remove('active');
        row.classList.remove('dragging');
        rows.forEach(o => { o.style.transform = ''; });
        if (to !== i) {
          const next = [...items];
          next.splice(to, 0, next.splice(i, 1)[0]);
          onDone(next);
        }
      };
      row.addEventListener('pointermove', moveTo);
      row.addEventListener('pointerup', end);
      row.addEventListener('pointercancel', end);
    });
  });
  return box;
}

function normTime(v) {
  const m = /^(\d{1,2})[:.]?(\d{2})$/.exec(v);
  if (!m || +m[1] > 23 || +m[2] > 59) return null;
  return pad(+m[1]) + ':' + m[2];
}

function group(title, ...kids) { return h('section.group.raised', null, title && h('h3', { text: title }), kids); }
const hint = text => h('p.hint', { text });

// A table whose body only renders visible rows, so libraries with thousands of games stay fast.
function vtable(cols, rowH, renderRow) {
  const head = h('div.thead', null, cols.map(c => h('div', { class: c.cls, text: c.label || '' })));
  const inner = h('div', { style: 'position:relative' });
  const empty = h('div.empty', { hidden: true });
  const body = h('div.tbody', null, inner, empty);
  const el = h('div.table.sunken.grow', null, head, body);
  let items = [], raf = 0;
  const draw = () => {
    raf = 0;
    const top = body.scrollTop, height = body.clientHeight || 600;
    const a = Math.max(0, Math.floor(top / rowH) - 6), z = Math.min(items.length, Math.ceil((top + height) / rowH) + 6);
    inner.style.height = items.length * rowH + 'px';
    fill(inner, ...items.slice(a, z).map((it, i) => {
      const r = renderRow(it);
      r.style.cssText += `;position:absolute;left:0;right:0;top:${(a + i) * rowH}px`;
      return r;
    }));
  };
  body.addEventListener('scroll', () => { if (!raf) raf = requestAnimationFrame(draw); });
  new ResizeObserver(() => draw()).observe(body);
  return {
    el, body,
    set(list, emptyText) {
      items = list;
      empty.hidden = list.length > 0;
      empty.textContent = emptyText || '';
      draw();
    },
  };
}
// Small tables (history lists) render everything.
function table(cols, rows, emptyText) {
  return h('div.table.sunken', null,
    h('div.thead', null, cols.map(c => h('div', { class: c.cls, text: c.label || '' }))),
    h('div.tbody', null, rows.length ? rows : h('div.empty', { text: emptyText })));
}

function gameIcon(url) {
  const el = h('span.gi');
  if (url) el.style.backgroundImage = `url("${url}")`;
  return el;
}

// Styled QR: soft modules and eyes, a badge in the middle. Error correction H keeps it readable;
// the rounding is checked against ZXing, the decoder most wallet and exchange apps use.
function qrSVG(text, { badge = true, ink = '#0b0b0b' } = {}) {
  const q = qrcode(0, badge ? 'H' : 'M');
  q.addData(text);
  q.make();
  const n = q.getModuleCount();
  let hole = badge ? Math.round(n * (badge === true ? 0.22 : badge)) : 0;
  if (hole && hole % 2 !== n % 2) hole++;
  const h0 = (n - hole) / 2;
  const eye = (r, c) => (r < 7 && c < 7) || (r < 7 && c >= n - 7) || (r >= n - 7 && c < 7);
  let dots = '';
  for (let r = 0; r < n; r++) for (let c = 0; c < n; c++) {
    if (!q.isDark(r, c) || eye(r, c)) continue;
    if (hole && r >= h0 - 0.5 && r < h0 + hole - 0.5 && c >= h0 - 0.5 && c < h0 + hole - 0.5) continue;
    dots += `<rect x="${c}" y="${r}" width="1.02" height="1.02"/>`;
  }
  const eyes = [[0, 0], [0, n - 7], [n - 7, 0]].map(([r, c]) =>
    `<rect x="${c + 0.5}" y="${r + 0.5}" width="6" height="6" fill="none" stroke="${ink}" stroke-width="1"/>` +
    `<rect x="${c + 2}" y="${r + 2}" width="3" height="3" fill="${ink}"/>`).join('');
  let mid = '';
  if (hole) {
    const s = hole - 1.2, x = (n - s) / 2;
    // the badge names the network the money goes through, so nobody sends it over the wrong one
    mid = `<g transform="translate(${x} ${x}) scale(${s / 64})" font-family="Play, Arial, sans-serif" text-anchor="middle">
      <rect x="1" y="1" width="62" height="62" fill="#f1efe0"/>
      <rect x="4" y="4" width="56" height="56" fill="#1f8f6a"/>
      <text x="32" y="31" font-size="17" font-weight="800" fill="#fff">USDT</text>
      <text x="32" y="47" font-size="12.5" font-weight="700" fill="#d9f2e8" letter-spacing=".5">TRC20</text></g>`;
  }
  const wrap = h('div');
  wrap.innerHTML = `<svg xmlns="${SVGNS}" viewBox="0 0 ${n} ${n}" shape-rendering="crispEdges"><g fill="${ink}">${dots}</g>${eyes}${mid}</svg>`;
  return wrap.firstChild;
}

// ---------------------------------------------------------------- tooltip
// A single fixed element follows [data-tip] targets. It lives on <body>, so the border of a game
// card or a scrolling panel can never cut it, and it flips below when there is no room above.
(() => {
  const tip = h('div.tip', { role: 'tooltip' });
  let cur = null;
  const hide = () => { cur = null; tip.classList.remove('on'); };
  const show = el => {
    cur = el;
    tip.textContent = el.dataset.tip;
    if (!tip.isConnected) document.body.append(tip);
    const r = el.getBoundingClientRect(), w = tip.offsetWidth, ht = tip.offsetHeight, m = 8;
    const x = Math.min(innerWidth - w - m, Math.max(m, r.left + r.width / 2 - w / 2));
    const y = r.top - ht - m >= m ? r.top - ht - m : r.bottom + m;
    tip.style.transform = `translate3d(${Math.round(x)}px, ${Math.round(y)}px, 0)`;
    tip.classList.add('on');
  };
  document.addEventListener('pointerover', e => {
    const el = e.target.closest?.('[data-tip]');
    if (el === cur) return;
    el && el.dataset.tip ? show(el) : hide();
  });
  document.addEventListener('pointerdown', () => { if (cur) requestAnimationFrame(() => cur && cur.isConnected ? show(cur) : hide()); });
  addEventListener('scroll', hide, true);
  addEventListener('blur', hide);
  // a tip whose target was re-rendered or removed goes away with it
  new MutationObserver(() => { if (cur && !cur.isConnected) hide(); }).observe(document.body, { childList: true, subtree: true });
})();

// ---------------------------------------------------------------- idle
// While the window is hidden or minimised every animation stops, so the app uses no GPU at all.
document.addEventListener('visibilitychange', () => document.documentElement.classList.toggle('idle', document.hidden));
