'use strict';
// Core: state, translations, DOM helpers, icons and the API client.
// SteamIdler UI. One page, no framework: data arrives over SSE, actions go through /api/call.

const Q = new URLSearchParams(location.search);
const KEY = Q.get('k') || '';
const NATIVE = Q.get('native') === '1';
if (NATIVE) document.documentElement.classList.add('native');

const S = { hub: null, bots: {}, games: {}, logs: [], dict: {}, lang: '', i18n: {}, live: false, attempt: {}, langAt: 0 };
const TRC20 = 'TUWXd4f3g3pSjxEgM7upWtCBdn774dJgPu';
const BINANCE_ID = '446743475';
const GITHUB = 'https://github.com/excruciation69/SteamIdler';
const STEAM_PROFILE = 'https://steamcommunity.com/profiles/76561199171650128/';
const TELEGRAM = 'https://t.me/excruciation69';

// ================================================================ helpers

function t(key, ...args) {
  const s = S.dict[key] ?? key;
  return s.replace(/\{(\d)\}/g, (_, i) => args[i] ?? '');
}
// engine messages carry keys as arguments (err.timeout and the like)
const tr = s => (typeof s === 'string' && S.dict[s] !== undefined) ? t(s) : s;

const SVGNS = 'http://www.w3.org/2000/svg';
function h(tag, props, ...kids) {
  const [name, ...cls] = tag.split('.');
  const el = document.createElement(name || 'div');
  if (cls.length) el.className = cls.join(' ');
  for (const [k, v] of Object.entries(props || {})) {
    if (v == null || v === false) continue;
    if (k === 'class') el.className = (el.className + ' ' + v).trim();
    else if (k === 'text') el.textContent = v;
    else if (k === 'style') el.style.cssText = v;
    else if (k.startsWith('on')) el.addEventListener(k.slice(2), v);
    else if (k === 'value' || k === 'checked' || k === 'disabled') el[k] = v;
    else el.setAttribute(k, v === true ? '' : v);
  }
  for (const kid of kids.flat(3)) {
    if (kid == null || kid === false) continue;
    el.append(kid instanceof Node ? kid : String(kid));
  }
  return el;
}

// Line icons drawn with straight segments only: square ends, sharp corners, no circles.
const ICONS = {
  clock: '<rect x="3.5" y="3.5" width="17" height="17"/><path d="M12 7v5h4.5"/>',
  spin: '<rect x="3.5" y="3.5" width="17" height="17"/><path class="hand" d="M12 12V6.5"/><path class="hand slow" d="M12 12h3.5"/>',
  cards: '<rect x="4" y="7" width="11" height="14"/><path d="M8 7V3h12v14h-5"/>',
  trophy: '<path d="M7 4h10v6l-2.5 3h-5L7 10z"/><path d="M12 13v5M8 21h8M9.5 18h5M17 6h3v3l-3 3M7 6H4v3l3 3"/>',
  key: '<rect x="3" y="12" width="8" height="8"/><path d="M10 13 20 3M16.5 6.5l3 3M13.5 9.5l2 2"/>',
  gift: '<rect x="3" y="8" width="18" height="4"/><path d="M12 8v13M19 12v9H5v-9M12 8 8 4H6v4M12 8l4-4h2v4"/>',
  user: '<rect x="8" y="3.5" width="8" height="8"/><path d="M4 21v-3l3-3h10l3 3v3"/>',
  users: '<rect x="4.5" y="4" width="7" height="7"/><path d="M2 20v-2l3-3h7l3 3v2M15 4h4v7h-4M17.5 15l3 3v2"/>',
  swap: '<path d="M7 3 3 7l4 4M3 7h14M17 21l4-4-4-4M21 17H7"/>',
  chart: '<path d="M4 4v16h16"/><path d="M8 16v-4M12 16V8M16 16v-6"/>',
  id: '<rect x="3" y="5" width="18" height="14"/><rect x="6.5" y="8.5" width="4.5" height="4.5"/><path d="M6 16h6M15 10h3M15 14h3"/>',
  sliders: '<path d="M4 6h9M17 6h3M4 12h3M11 12h9M4 18h11M19 18h1"/><rect x="13" y="4" width="4" height="4"/><rect x="7" y="10" width="4" height="4"/><rect x="15" y="16" width="4" height="4"/>',
  heart: '<path d="M12 20 4 12V7l3-3h3l2 2 2-2h3l3 3v5z"/>',
  plus: '<path d="M12 5v14M5 12h14"/>',
  search: '<rect x="4" y="4" width="11" height="11"/><path d="m15 15 5.5 5.5"/>',
  play: '<path d="M8 5.5v13l10-6.5z"/>',
  stop: '<rect x="6.5" y="6.5" width="11" height="11"/>',
  refresh: '<path d="M20 4v6h-6M4 20v-6h6M19 10l-3-4H8L5 9M5 14l3 4h8l3-3"/>',
  back: '<path d="M15 6l-6 6 6 6"/>',
  next: '<path d="m9 6 6 6-6 6"/>',
  globe: '<rect x="3" y="4" width="18" height="16"/><path d="M3 8.5h18M6 6.2h1M9 6.2h1M12 6.2h1"/>',
  monitor: '<rect x="3" y="4" width="18" height="12"/><path d="M8 20h8M12 16v4"/>',
  copy: '<rect x="8" y="8" width="12" height="12"/><path d="M16 8V4H4v12h4"/>',
  x: '<path d="M6 6l12 12M18 6 6 18"/>',
  up: '<path d="m6 15 6-6 6 6"/>',
  down: '<path d="m6 9 6 6 6-6"/>',
  trash: '<path d="M4 7h16M10 11v6M14 11v6M6 7l1 13h10l1-13M9 7V4h6v3"/>',
  upload: '<path d="M12 15V4M7 9l5-5 5 5M5 20h14"/>',
  logout: '<path d="M15 4h5v16h-5M10 17l5-5-5-5M15 12H4"/>',
  phone: '<rect x="7" y="3" width="10" height="18"/><path d="M11 18h2"/>',
  send: '<path d="M21 3 3 10.5l7 2.5 2.5 7z"/><path d="m10 13 11-10"/>',
  star: '<path d="m12 3 2.8 5.8 6.2.9-4.5 4.4 1 6.2-5.5-2.9-5.5 2.9 1-6.2L3 9.7l6.2-.9z"/>',
  lock: '<rect x="5" y="11" width="14" height="10"/><path d="M8 11V5h8v6"/>',
  unlock: '<rect x="5" y="11" width="14" height="10"/><path d="M8 11V5h8v2"/>',
  shield: '<path d="M12 3 5 6v6l7 9 7-9V6z"/>',
  edit: '<path d="M4 20h4L19 9l-4-4L4 16z"/>',
  check: '<path d="m5 12 5 5 9-10"/>',
  info: '<rect x="3.5" y="3.5" width="17" height="17"/><path d="M12 11v6M12 7.5v.5"/>',
  grip: '<path d="M9 6h.01M15 6h.01M9 12h.01M15 12h.01M9 18h.01M15 18h.01" stroke-width="3"/>',
  more: '<path d="M5 12h.01M12 12h.01M19 12h.01" stroke-width="3.2"/>',
  box: '<path d="M3.5 7.5 12 3.5l8.5 4v9L12 20.5l-8.5-4z"/><path d="M3.5 7.5 12 11.5l8.5-4M12 11.5v9"/>',
  tag: '<path d="M3.5 3.5h8l9 9-8 8-9-9z"/><rect x="7" y="7" width="2.5" height="2.5"/>',
  list: '<path d="M8 6h12M8 12h12M8 18h12M4 6h.01M4 12h.01M4 18h.01" stroke-width="2"/>',
  home: '<rect x="3.5" y="3.5" width="7" height="7"/><rect x="13.5" y="3.5" width="7" height="7"/><rect x="3.5" y="13.5" width="7" height="7"/><rect x="13.5" y="13.5" width="7" height="7"/>',
};
function ic(name, extra) {
  const s = document.createElementNS(SVGNS, 'svg');
  s.setAttribute('viewBox', '0 0 24 24');
  s.setAttribute('class', 'i' + (extra ? ' ' + extra : ''));
  s.innerHTML = ICONS[name] || '';
  return s;
}

const getp = (o, path) => path.split('.').reduce((a, k) => (a == null ? a : a[k]), o);
const mkp = (path, v) => path.split('.').reduceRight((acc, k) => ({ [k]: acc }), v);
function merge(dst, src) {
  for (const [k, v] of Object.entries(src)) {
    if (v && typeof v === 'object' && !Array.isArray(v) && dst[k] && typeof dst[k] === 'object') merge(dst[k], v);
    else dst[k] = v;
  }
}
const pad = n => String(n).padStart(2, '0');
const fmtH = sec => (sec / 3600).toFixed(1);
function fmtDur(ms) {
  const s = Math.floor((ms || 0) / 1000);
  return `${Math.floor(s / 3600)}:${pad(Math.floor(s / 60) % 60)}:${pad(s % 60)}`;
}
const locale = () => (S.lang === 'en' ? 'en-GB' : 'ru-RU');
const fmtDate = u => new Date(u * 1000).toLocaleString(locale(), { day: '2-digit', month: '2-digit', year: '2-digit', hour: '2-digit', minute: '2-digit' });
const fmtTime = u => new Date(u * 1000).toLocaleTimeString(locale(), { hour: '2-digit', minute: '2-digit' });
const fmtDay = u => new Date(u * 1000).toLocaleDateString(locale(), { day: 'numeric', month: 'long', year: 'numeric' });
// replaceChildren would print null and false as text: drop them first
const fill = (el, ...kids) => el.replaceChildren(...kids.flat(3).filter(k => k != null && k !== false));
const isFocused = el => el && document.activeElement === el;
function memo() {
  let last;
  return (key, fn) => { const k = JSON.stringify(key); if (k !== last) { last = k; fn(); } };
}

// ================================================================ api

async function call(name, args = {}) {
  const r = await fetch('/api/call/' + name, {
    method: 'POST',
    headers: { 'X-SteamIdler-Key': KEY, 'Content-Type': 'application/json' },
    body: JSON.stringify(args),
  });
  if (!r.ok) throw new Error('err.network');
  const j = await r.json();
  if (!j.ok) throw new Error(j.error || 'error');
  return j.data;
}
// act: call and show the error as a toast
async function act(name, args) {
  try { return await call(name, args); } catch (e) { toast(tr(e.message)); throw e; }
}
function patch(id, p) {
  const b = S.bots[id];
  if (b) merge(b.cfg, p);
  return act('update', { id, patch: p }).catch(() => {});
}
function patchSettings(p) {
  if (S.hub) merge(S.hub.settings, p);
  return act('settings', { patch: p }).catch(() => {});
}
function openURL(url) {
  // the phone panel opens links on the phone itself, the Mac window asks the engine
  if (NATIVE) act('openExternal', { url }).catch(() => {});
  else window.open(url, '_blank', 'noopener');
}
async function copy(text) {
  try { await navigator.clipboard.writeText(text); }
  catch {
    const ta = h('textarea', { style: 'position:fixed;opacity:0' });
    ta.value = text; document.body.append(ta); ta.select(); document.execCommand('copy'); ta.remove();
  }
  toast(t('copied'));
}
