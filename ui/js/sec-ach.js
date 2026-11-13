'use strict';
// Achievements.

// ---------------------------------------------------------------- achievements

function AchSec(id, info) {
  const B = binder(id);
  let appid = 0, list = [], picked = new Set(), loading = false, error = '';
  const progress = h('span.dim');
  info.append(progress);
  const pick = h('select.field', { style: 'min-width:240px;max-width:100%', onchange: () => { appid = Number(pick.value); load(); } });
  const tbl = h('div.col');
  const job = h('div.row.wrap');
  const unlockBtn = btn('', () => apply(true), 'primary', 'unlock');
  const lockBtn = btn('', () => apply(false), '', 'lock');
  // hero: the chosen game, how much is open, and the countdown to the next unlock when a job runs
  const heroBg = h('div.bg'), kicker = h('div.kicker'), heroTitle = h('h2'), heroMeta = h('div.meta');
  const heroBar = h('div.bar.thick', { style: 'max-width:420px' }, h('i'));
  const nextCard = h('div.side-card', { hidden: true });
  const hero = h('section.hero', null, heroBg, kicker, heroTitle, heroMeta, heroBar, nextCard);
  let jobRef = null;
  function heroPaint() {
    const name = pick.selectedOptions[0]?.value !== '0' ? pick.selectedOptions[0]?.text : '';
    const j = jobRef;
    const showId = appid || j?.appid || 0;
    if (showId) art(heroBg, headerArt(showId), '', showId); else { heroBg.dataset.src = ''; heroBg.style.backgroundImage = ''; }
    fill(kicker, j ? [ic('spin', 'spin'), t('hero.unlocking')] : [ic('trophy'), t('mod.ach')]);
    heroTitle.textContent = name || j?.game || t('ach.pick');
    const done = list.filter(a => a.unlocked).length;
    fill(heroMeta, list.length ? [h('span', null, h('b', { text: done + ' / ' + list.length }), ' ', t('ach.openShort'))] : []);
    heroBar.hidden = !list.length;
    heroBar.firstChild.style.width = (list.length ? done / list.length * 100 : 0) + '%';
    nextCard.hidden = !j;
    if (j) {
      const nextId = j.pending[0];
      const a = list.find(x => x.id === nextId);
      const left = Math.max(0, (j.nextAt || 0) * 1000 - Date.now());
      fill(nextCard, h('div.t', { text: t('ach.next') }),
        h('div.row', { style: 'margin:8px 0 4px' }, a ? gameIcon(a.icon) : ic('trophy'), h('span.ellipsis', { style: 'font-weight:700;color:#fff', text: a?.name || nextId || '' })),
        h('div.cd', { text: j.nextAt ? fmtDur(left).replace(/^0:/, '') : '…' }),
        h('div.row', { style: 'justify-content:space-between' }, h('span.dim', { text: t('ach.jobLeft', j.pending.length) }),
          btn(t('btn.cancel'), () => act('achCancel', { id }).catch(() => {}), 'xs ghost')));
    }
  }
  const el = h('div.col', { style: 'gap:14px' },
    hero,
    h('div.toolbar', null, pick,
      btn(t('ach.selectAll'), () => { picked = new Set(list.filter(a => !a.protected).map(a => a.id)); draw(); }, 'sm ghost'),
      btn(t('ach.selectNone'), () => { picked.clear(); draw(); }, 'sm ghost')),
    h('div.split', null,
      group(t('ach.how'), B.radios('ach.human', [[true, t('ach.human')], [false, t('ach.all')]]),
        h('div.row.wrap.mini', { style: 'gap:8px' }, h('span.dim', { text: t('ach.delay') }), B.num('ach.minDelay', 1, 720), h('span.dim', { text: '–' }),
          B.num('ach.maxDelay', 1, 1440), h('span.dim', { text: t('u.min') }))),
      group(t('ach.when'),
        h('div.row.wrap.mini', { style: 'gap:8px' }, B.switch('ach.window.on', t('ach.window')), B.time('ach.window.from'), h('span.dim', { text: '–' }), B.time('ach.window.to')),
        B.check('ach.idleWhile', t('ach.idleWhile'))),
      group(t('ach.games'), h('div', null, (() => {
        const c = B.check('ach.showVAC', t('ach.showVAC'));
        c.input.addEventListener('change', () => setTimeout(loadGames, 300));
        return c;
      })()))),
    tbl, job,
    h('div.raised', { style: 'padding:12px 14px' }, h('div.bottom', null, h('span.grow'), lockBtn, unlockBtn)));

  async function loadGames() {
    try {
      const games = (await call('achGames', { id })) || [];
      fill(pick, h('option', { value: '0', text: t('ach.pick') }), ...games.map(g => h('option', { value: String(g.appid), text: g.name })));
      pick.value = String(appid);
      if (pick.value !== String(appid)) { appid = 0; pick.value = '0'; }
    } catch (e) { toast(tr(e.message)); }
  }
  async function load(silent) {
    if (!appid) { list = []; draw(); return; }
    if (!silent) { loading = true; error = ''; list = []; picked.clear(); draw(); }
    const want = appid;
    try {
      const res = (await call('achList', { id, appid, lang: S.lang })) || [];
      if (want !== appid) return;
      list = res;
    } catch (e) { if (!silent) error = tr(e.message); }
    loading = false;
    draw();
  }
  function counts() {
    const sel = list.filter(a => picked.has(a.id) && !a.protected);
    return { unlock: sel.filter(a => !a.unlocked).map(a => a.id), lock: sel.filter(a => a.unlocked).map(a => a.id) };
  }
  async function apply(unlock) {
    const c = counts(), ids = unlock ? c.unlock : c.lock;
    if (!ids.length) return toast(t('ach.nothing'));
    try {
      await call('achSet', { id, appid, ids, unlock, human: S.bots[id].cfg.ach.human });
      picked.clear();
      setTimeout(() => load(true), 1500);
    } catch (e) { toast(tr(e.message)); }
  }
  function draw() {
    heroPaint();
    const done = list.filter(a => a.unlocked).length;
    progress.textContent = list.length ? t('ach.progress', done, list.length) : '';
    const c = counts();
    unlockBtn.lastChild.textContent = t('ach.unlock', c.unlock.length);
    lockBtn.lastChild.textContent = t('ach.lock', c.lock.length);
    unlockBtn.disabled = !c.unlock.length;
    lockBtn.disabled = !c.lock.length;
    const empty = error || (loading ? t('ach.loading') : appid ? t('ach.none') : t('ach.hint'));
    fill(tbl, table([
      { cls: 'c-cb' }, { cls: 'c-n', label: t('ach.name') }, { cls: 'c-s c-r hide-sm', label: t('ach.players') }, { cls: 'c-h', label: t('ach.status') },
    ], loading ? [] : list.map(a => {
      const on = picked.has(a.id);
      return h('div.tr.tall' + (on ? '.on' : ''), null,
        h('div.c-cb', null, a.protected ? h('span.faint', null, ic('lock')) : h('span.cbx', { onclick: () => { on ? picked.delete(a.id) : picked.add(a.id); draw(); } }, h('span.box' + (on ? '.on' : '')))),
        h('div.c-n', null, gameIcon(a.icon), h('div', { style: 'min-width:0' },
          h('div.ellipsis', { text: a.name || a.id }),
          h('div.ellipsis.faint', { style: 'font-size:12px', text: a.desc || (a.hidden ? t('ach.hidden') : '') }))),
        h('div.c-s.c-r.mono.hide-sm', { text: a.percent ? a.percent.toFixed(1) + '%' : '' }),
        h('div.c-h', null, a.protected ? h('span.chip', { text: t('ach.protected') })
          : a.unlocked ? h('span.chip.ok', { text: t('ach.open') }) : h('span.chip', { text: t('ach.closed') })));
    }), empty));
  }
  loadGames();
  draw();
  let lastDone = -1;
  return {
    el,
    update(b) {
      B.sync();
      const j = b.achJob;
      jobRef = j;
      heroPaint();
      if (j && j.appid === appid && j.done !== lastDone) { lastDone = j.done; load(true); }
    },
  };
}
