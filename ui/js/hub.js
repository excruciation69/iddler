'use strict';
// Overview: every account at a glance, the event feed and adding accounts.

// ================================================================ hub

function HubPage() {
  const cardsEl = h('div.cards');
  const feed = h('div.feed');
  const kpis = h('div.kpis');
  const sub = h('span.sub');
  const startAll = btn(t('hub.startAll'), () => act('startAll').catch(() => {}), 'sm', 'play');
  const stopAll = btn(t('hub.stopAll'), () => act('stopAll').catch(() => {}), 'sm', 'stop');
  const el = h('div.page', null, h('div.scroll', null,
    h('div.ptitle', null, h('h1', { text: t('hub.title') }), sub,
      h('div.actions', null, startAll, stopAll, btn(t('hub.add'), addAccountDialog, 'sm primary', 'plus'))),
    groupInvite(),
    cardsEl,
    h('div.split', null,
      group(t('hub.events'), feed),
      group(t('hub.week'), kpis))));

  const cards = new Map();
  const addCard = h('button.addcard', { type: 'button', onclick: addAccountDialog },
    h('span.plus', null, ic('plus')), h('span', { text: t('hub.add') }));
  const feedMemo = memo();

  function card(id) {
    const r = {};
    r.ava = h('div.ava', { title: t('ava.title'), onclick: e => { e.stopPropagation(); profileMenu(r.ava, id); } });
    r.name = h('div.nm.ellipsis');
    r.led = h('span.led');
    r.st = h('span.ellipsis');
    r.today = h('b'); r.week = h('b'); r.left = h('b');
    r.run = h('button.btn.sm', { type: 'button', onclick: e => { e.stopPropagation(); quickRun(id); } });
    r.el = h('article.acard.raised', { onclick: () => go(`#/a/${encodeURIComponent(id)}/hours`) },
      h('div.head', null, r.ava, h('div', { style: 'min-width:0;flex:1' }, r.name, h('div.st', null, r.led, r.st))),
      h('div.stats', null,
        h('div.sunken', null, r.today, h('span', { text: t('k.today') })),
        h('div.sunken', null, r.week, h('span', { text: t('k.week') })),
        h('div.sunken', null, r.left, h('span', { text: t('k.cardsLeft') }))),
      h('div.row', null, h('span.grow'), r.run, btn(t('btn.open'), null, 'sm ghost')));
    return r;
  }

  function update() {
    const accs = S.hub.accounts || [];
    const running = accs.filter(a => a.hoursOn || a.cardsOn).length;
    sub.textContent = t('hub.count', accs.length, running);
    startAll.disabled = !accs.some(a => a.status === 'online' && !(a.hoursOn || a.cardsOn));
    stopAll.disabled = running === 0;
    const seen = new Set();
    for (const s of accs) {
      seen.add(s.id);
      let r = cards.get(s.id);
      if (!r) { r = card(s.id); cards.set(s.id, r); }
      avatar(r.ava, s);
      r.name.textContent = s.name || s.account;
      const st = statusOf(s);
      r.led.className = 'led ' + st.led;
      r.st.textContent = st.text;
      fill(r.today, fmtH(s.todaySec), h('small', { text: t('u.h') }));
      fill(r.week, fmtH(s.weekSec), h('small', { text: t('u.h') }));
      r.left.textContent = s.cardsLeft ?? 0;
      const on = s.hoursOn || s.cardsOn;
      setButton(r.run, s.status !== 'online' ? 'logout' : on ? 'stop' : 'play',
        s.status !== 'online' ? t('btn.signIn') : on ? t('btn.stop') : t('btn.start'), 'btn sm' + (s.status === 'online' && !on ? ' primary' : ''));
    }
    for (const [id, r] of cards) if (!seen.has(id)) { r.el.remove(); cards.delete(id); }
    const want = [...accs.map(s => cards.get(s.id).el), addCard];
    if (want.some((e, i) => cardsEl.children[i] !== e) || cardsEl.children.length !== want.length) fill(cardsEl, ...want);

    feedMemo([S.logs.length && S.logs[S.logs.length - 1].id, S.lang, accs.map(a => a.name)], () => {
      const last = S.logs.slice(-40).reverse();
      fill(feed, ...(last.length ? last.map(logLine) : [h('div.hint', { text: t('hub.noEvents') })]));
    });

    let keys = 0, free = 0, drops = 0;
    for (const b of Object.values(S.bots)) {
      keys += (b.keys || []).filter(k => k.result === 'ok').length;
      free += (b.claimed || []).filter(c => c.kind === 'promo').length;
      drops += (b.drops || []).reduce((n, d) => n + d.count, 0);
    }
    const week = accs.reduce((n, a) => n + a.weekSec, 0), today = accs.reduce((n, a) => n + a.todaySec, 0);
    fill(kpis, 
      kpi(fmtH(week) + ' ' + t('u.h'), t('hub.hours')), kpi(fmtH(today) + ' ' + t('u.h'), t('k.today')),
      kpi(keys, t('hub.keys')), kpi(free, t('hub.free')));
  }
  const kpi = (v, l) => h('div.kpi.sunken', null, h('b', { text: v }), h('span', { text: l }));
  return { el, update };
}

const STEAM_GROUP = 'https://steamcommunity.com/groups/iddle2003';
// A one-time invitation to the Steam group. Joining is the user's own click.
function groupInvite() {
  if (S.hub.settings.inviteHidden) return null;
  const close = () => { patchSettings({ inviteHidden: true }); el.remove(); };
  const el = h('div.invite.raised', null, h('span.inv-ic', null, ic('users')),
    h('div.grow', { style: 'min-width:0' }, h('b', { text: t('group.title') }), h('div.dim', { text: t('group.text') })),
    btn(t('group.open'), () => { openURL(STEAM_GROUP); close(); }, 'sm primary'),
    h('button.btn.icon.ghost', { type: 'button', title: t('btn.close'), onclick: close }, ic('x')));
  return el;
}

function logLine(l) {
  const s = S.hub?.accounts?.find(a => a.id === l.account);
  return h('div.ln', null,
    h('span.ts', { text: fmtTime(l.at) }),
    h('span.src', { text: s ? (s.name || s.account) : 'SteamIdler' }),
    h('span', { class: l.kind === 'err' ? 'err' : l.kind === 'warn' ? 'warn' : l.kind === 'ok' ? 'ok' : '', text: t(l.key, ...(l.args || []).map(tr)) }));
}

async function quickRun(id) {
  const s = S.hub.accounts.find(a => a.id === id);
  if (!s) return;
  if (s.status !== 'online') return go(`#/a/${encodeURIComponent(id)}/hours`);
  if (s.hoursOn || s.cardsOn) {
    if (s.hoursOn) await act('run', { id, module: 'hours', on: false }).catch(() => {});
    if (s.cardsOn) await act('run', { id, module: 'cards', on: false }).catch(() => {});
    return;
  }
  act('run', { id, module: 'hours', on: true }).catch(e => {
    if (e.message === 'err.noGames') go(`#/a/${encodeURIComponent(id)}/hours`);
  });
}

function addAccountDialog() {
  const acc = field({ placeholder: t('login.account'), autocomplete: 'username', autocapitalize: 'off', spellcheck: 'false' });
  const pw = field({ type: 'password', placeholder: t('login.password'), autocomplete: 'current-password' });
  const rem = check(t('login.remember'), true, () => {});
  const err = h('div.error');
  const pwBox = h('div.col', { style: 'gap:12px' },
    h('div.label', { text: t('login.account') }), acc,
    h('div.label', { text: t('login.password') }), pw, rem, err, hint(t('login.hint')));

  // QR: Steam mobile app → shield → scan. The code refreshes itself until it is approved.
  const qrBox = h('div.qrlogin', { hidden: true });
  let qrOn = false, qrURL = '', closeDlg = () => {};
  const qrShow = (url, note) => {
    if (url && url !== qrURL) { qrURL = url; fill(qrBox, h('div.qr', null, qrSVG(url, { badge: false })), hint(t('login.qrHint'))); }
    if (note) fill(qrBox, h('div.empty', { text: note }), btn(t('login.qrAgain'), qrStart, 'sm', 'refresh'));
  };
  async function qrStart() {
    qrURL = '';
    fill(qrBox, h('div.empty', { text: t('conn.text') }));
    try { qrShow((await call('qrStart')).url); } catch (e) { return qrShow('', tr(e.message)); }
    qrOn = true;
    while (qrOn && qrBox.isConnected) {
      await new Promise(r => setTimeout(r, 1500));
      if (!qrOn || !qrBox.isConnected) break;
      const s = await call('qrState').catch(() => null);
      if (!s) continue;
      if (s.error) { qrOn = false; return qrShow('', tr(s.error)); }
      if (s.done && s.account) {
        qrOn = false;
        closeDlg();
        return go(`#/a/${encodeURIComponent(s.account)}/hours`);
      }
      qrShow(s.url);
    }
    if (qrOn) call('qrCancel').catch(() => {});
    qrOn = false;
  }

  const tabs = h('div.seg.full');
  const setTab = tab => {
    fill(tabs, [['pw', 'login.byPassword'], ['qr', 'login.byQR']].map(([k, l]) =>
      h('button' + (tab === k ? '.on' : ''), { type: 'button', onclick: () => setTab(k) }, t(l))));
    pwBox.hidden = tab !== 'pw';
    qrBox.hidden = tab !== 'qr';
    const submit = document.querySelector('.overlay:last-child .df .primary');
    if (submit) submit.hidden = tab !== 'pw';
    if (tab === 'qr' && !qrOn) qrStart();
    if (tab === 'pw' && qrOn) { qrOn = false; call('qrCancel').catch(() => {}); }
  };
  closeDlg = dialog(t('login.title'), [tabs, pwBox, qrBox], [
    { label: t('btn.cancel'), cls: 'ghost', onclick: () => { if (qrOn) { qrOn = false; call('qrCancel').catch(() => {}); } } },
    { label: t('login.submit'), cls: 'primary', onclick: async () => {
      if (!pwBox.hidden) {
        err.textContent = '';
        try {
          const id = await call('addAccount', { account: acc.value.trim(), password: pw.value, remember: rem.input.checked });
          S.attempt[id] = Math.floor(Date.now() / 1000) - 1;
          go(`#/a/${encodeURIComponent(id)}/hours`);
        } catch (e) { err.textContent = tr(e.message); return false; }
      }
    } },
  ]);
  setTab('pw');
}
