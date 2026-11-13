'use strict';
// Games: the library, idling, goals and the schedule.

// ---------------------------------------------------------------- hours

// Steam artwork for a game; the card falls back to a coloured tile with the name when it is missing.
const CDN = 'https://cdn.cloudflare.steamstatic.com/steam/apps/';
const capsule = appid => CDN + appid + '/library_600x900.jpg';
const headerArt = appid => CDN + appid + '/header.jpg';
function tint(appid) {
  const hue = (appid * 47) % 360;
  return `linear-gradient(160deg, hsl(${hue} 35% 22%), hsl(${(hue + 40) % 360} 30% 9%))`;
}
function art(el, url, fallbackName, appid) {
  if (el.dataset.src === url) return el;
  el.dataset.src = url;
  el.style.backgroundImage = tint(appid);
  fill(el, fallbackName ? h('div.ph', { text: fallbackName }) : null);
  const img = new Image();
  img.onload = () => { el.style.backgroundImage = `url("${url}")`; fill(el); };
  img.src = url;
  return el;
}

// Games: the library as a grid of capsules, the running session on top, actions docked below.
function HoursSec(id, info) {
  const B = binder(id);
  let q = '', filter = 'all', sort = 'hours';
  const sessText = h('span.mono');
  info.append(sessText);
  S.onSearch = v => { q = v; refill(); };

  // hero
  const heroBg = h('div.bg');
  const kicker = h('div.kicker');
  const heroTitle = h('h2');
  const heroMeta = h('div.meta');
  const strip = h('div.strip');
  const heroBtn = h('button.btn.big', { type: 'button', onclick: () => act('run', { id, module: 'hours', on: !S.bots[id].hoursOn }).catch(() => {}) });
  const hero = h('section.hero', null, heroBg, h('div.actions', null, heroBtn), kicker, heroTitle, heroMeta, strip);

  // library
  const seg = h('div.seg', null, [['all', 'lib.all'], ['sel', 'lib.selected'], ['fav', 'lib.favs']].map(([k, l]) =>
    h('button', { type: 'button', 'data-k': k, class: k === filter ? 'on' : '', text: t(l), onclick: () => { filter = k; [...seg.children].forEach(b => b.classList.toggle('on', b.dataset.k === k)); refill(); } })));
  const sortSel = h('select.field', { style: 'height:30px', onchange: () => { sort = sortSel.value; refill(); } },
    [['hours', 'lib.sortHours'], ['name', 'lib.sortName'], ['goal', 'lib.sortGoal']].map(([v, l]) => h('option', { value: v, text: t(l) })));
  const grid = h('div.games');
  const empty = h('div.empty', { hidden: true });
  grid.addEventListener('click', onGrid);

  // dock
  const selCount = h('b.mono', { style: 'font-size:15px;color:#fff' });
  const custom = h('span.ellipsis', { style: 'color:var(--accent)' });
  const runBtn = h('button.btn.big', { type: 'button', onclick: () => act('run', { id, module: 'hours', on: !S.bots[id].hoursOn }).catch(() => {}) });

  const el = h('div.col', { style: 'gap:18px' },
    hero,
    h('div.lib-head', null, h('h2', { text: t('lib.title') }), seg, h('span.grow'), sortSel,
      btn(t('hours.clear'), () => setSelected([]), 'sm ghost'),
      h('button.btn.sm.icon', { type: 'button', 'data-tip': t('hours.reload'), onclick: () => act('reloadGames', { id }).catch(() => {}) }, ic('refresh')),
      btn(t('hours.addGame'), () => addGameDialog(id), 'sm', 'plus')),
    grid, empty,
    h('div.dock', null,
      h('div.info', null,
        h('div.row', { style: 'min-width:0' }, h('span.dim', { text: t('hours.customTitle') + ':' }), custom, h('a', { href: `#/a/${encodeURIComponent(id)}/profile`, text: t('hours.customSet') })),
        h('div.row.wrap', { style: 'gap:8px' }, B.switch('hours.schedule.on', t('hours.schedule')),
          B.time('hours.schedule.from'), h('span.dim', { text: t('profile.to') }), B.time('hours.schedule.to'))),
      h('div.col', { style: 'align-items:flex-end;gap:6px' }, h('span.dim', null, selCount, ' / 32'), runBtn)));

  const cards = new Map();
  const b0 = () => S.bots[id];
  const sel = () => new Set(b0()?.cfg.hours.selected || []);
  const favs = () => new Set(b0()?.cfg.favorites || []);
  function setSelected(list) {
    b0().cfg.hours.selected = list;
    refill();
    patch(id, { hours: { selected: list } });
  }
  function toggleFav(appid) {
    const f = b0().cfg.favorites || [];
    const next = f.includes(appid) ? f.filter(x => x !== appid) : [...f, appid];
    b0().cfg.favorites = next;
    refill();
    patch(id, { favorites: next });
  }
  function onGrid(e) {
    const a = e.target.closest('[data-act]');
    if (!a) return;
    e.preventDefault();
    const appid = Number(a.closest('.gcard').dataset.id);
    const s = b0().cfg.hours.selected || [];
    if (a.dataset.act === 'pick') {
      if (s.includes(appid)) setSelected(s.filter(x => x !== appid));
      else if (s.length >= 32) toast(t('hours.full'));
      else setSelected([...s, appid]);
    } else if (a.dataset.act === 'goal') goalDialog(id, appid);
    else if (a.dataset.act === 'fav') toggleFav(appid);
    else if (a.dataset.act === 'rm') act('removeGame', { id, appid }).catch(() => {});
  }
  function card(g) {
    const r = { art: h('div.art'), tagL: h('span.tag.l'), tagR: h('span.tag.r'), bar: h('div.bar', { hidden: true }, h('i')) };
    r.favBtn = h('button.btn.icon', { type: 'button', 'data-act': 'fav' }, ic('star'));
    r.el = h('div.gcard', { 'data-id': g.appid }, r.art, r.tagL, r.tagR, r.bar,
      h('button.pick', { type: 'button', 'data-act': 'pick' }, ic('check')),
      h('div.nm', { text: g.name }),
      h('div.acts', null, h('button.btn', { type: 'button', 'data-act': 'goal' }, ic('clock'), h('span', { text: t('lib.goal') })), r.favBtn,
        g.manual ? h('button.btn.icon', { type: 'button', 'data-act': 'rm', title: t('btn.remove') }, ic('trash')) : null));
    art(r.art, capsule(g.appid), '', g.appid);
    return r;
  }
  function paint(r, g, b, s, f) {
    const on = s.has(g.appid), live = (b.playingIDs || []).includes(g.appid);
    const hrs = b.hours?.[g.appid] ?? g.minutes / 60;
    const goal = b.cfg.hours.goals?.[g.appid];
    const k = [on, live, hrs.toFixed(1), goal, f.has(g.appid), S.lang].join('|');
    if (r.k === k) return;
    r.k = k;
    r.el.classList.toggle('on', on);
    r.el.classList.toggle('live', live);
    fill(r.tagL, live ? ic('spin', 'spin') : ic('clock'), hrs.toFixed(1) + (goal ? ' / ' + goal : '') + ' ' + t('u.h'));
    fill(r.tagR, f.has(g.appid) ? h('span.fav', null, ic('star')) : null);
    r.tagR.hidden = !f.has(g.appid);
    r.bar.hidden = !goal;
    if (goal) r.bar.firstChild.style.width = Math.min(100, hrs / goal * 100) + '%';
    r.favBtn.dataset.tip = f.has(g.appid) ? t('lib.unfav') : t('lib.fav');
    r.favBtn.style.color = f.has(g.appid) ? '#ffd257' : '';
  }
  function refill() {
    const b = b0();
    if (!b) return;
    const games = S.games[id] || [];
    const s = sel(), f = favs();
    let list = games.filter(g => (filter === 'all' || (filter === 'sel' ? s.has(g.appid) : f.has(g.appid))) &&
      (!q || g.name.toLowerCase().includes(q) || String(g.appid).startsWith(q)));
    const hrs = g => b.hours?.[g.appid] ?? g.minutes / 60;
    if (sort === 'hours') list = [...list].sort((a, c) => hrs(c) - hrs(a));
    else if (sort === 'goal') list = [...list].sort((a, c) => (b.cfg.hours.goals?.[c.appid] ? 1 : 0) - (b.cfg.hours.goals?.[a.appid] ? 1 : 0) || hrs(c) - hrs(a));
    const want = list.map(g => {
      let r = cards.get(g.appid);
      if (!r) { r = card(g); cards.set(g.appid, r); }
      paint(r, g, b, s, f);
      return r.el;
    });
    if (want.length !== grid.children.length || want.some((e, i) => grid.children[i] !== e)) grid.replaceChildren(...want);
    empty.hidden = list.length > 0;
    empty.textContent = b.gamesLoading && !games.length ? t('hours.loading') : games.length ? t('hours.nothing') : t('hours.empty');
  }
  function heroPaint(b) {
    const games = S.games[id] || [];
    const playing = (b.playingIDs || []).map(a => games.find(g => g.appid === a) || { appid: a, name: String(a) });
    const selected = (b.cfg.hours.selected || []).map(a => games.find(g => g.appid === a)).filter(Boolean);
    const lead = playing[0] || selected[0];
    if (lead) art(heroBg, headerArt(lead.appid), '', lead.appid); else { heroBg.dataset.src = ''; heroBg.style.backgroundImage = ''; }
    const on = b.hoursOn;
    fill(kicker, on ? [ic('spin', 'spin'), t(b.blocked ? 'st.paused' : 'hero.running')] : [ic('clock'), t('hero.ready')]);
    heroTitle.textContent = on ? (b.custom || lead?.name || '') : selected.length ? t('hero.selected', selected.length) : t('hero.pick');
    fill(heroMeta, on ? [h('span', null, h('b', { text: fmtDur(b.sessionMs) })), h('span', { text: t('hero.games', playing.length) })]
      : [h('span.dim', { text: selected.length ? t('hero.readyHint') : t('hero.pickHint') })]);
    const sk = (on ? playing : selected).slice(0, 12).map(g => g.appid).join(',') + '|' + on;
    if (strip.dataset.k !== sk) {
      strip.dataset.k = sk;
      fill(strip, (on ? playing : selected).slice(0, 12).map(g => art(h('div.mini' + (on ? '.live' : ''), { title: g.name }), headerArt(g.appid), '', g.appid)));
    }
    setButton(heroBtn, on ? 'stop' : 'play', on ? t('hours.stop') : t('hours.start'), 'btn big ' + (on ? 'stop' : 'primary'));
    heroBtn.disabled = !on && !selected.length;
  }
  return {
    el,
    update(b) {
      B.sync();
      refill();
      heroPaint(b);
      selCount.textContent = (b.cfg.hours.selected || []).length;
      custom.textContent = b.custom || t('hours.noCustom');
      setButton(runBtn, b.hoursOn ? 'stop' : 'play', b.hoursOn ? t('hours.stop') : t('hours.start'), 'btn big ' + (b.hoursOn ? 'stop' : 'primary'));
      sessText.textContent = b.hoursOn ? fmtDur(b.sessionMs) : '';
    },
    destroy() { S.onSearch = null; },
  };
}

function goalDialog(id, appid) {
  const b = S.bots[id];
  const g = (S.games[id] || []).find(x => x.appid === appid);
  const cur = b.cfg.hours.goals?.[appid];
  const inp = field({ class: 'num', inputmode: 'decimal', value: cur || '' });
  dialog(t('goal.title'), [
    h('div.dim', { text: g?.name || appid }),
    h('div.row', null, h('span', { text: t('goal.stopAt') }), inp, h('span', { text: t('u.h') })),
    hint(t('goal.hint')),
  ], [
    ...(cur ? [{ label: t('goal.remove'), cls: 'ghost danger', onclick: () => patch(id, { hours: { goals: { [appid]: 0 } } }) }] : []),
    { label: t('btn.cancel'), cls: 'ghost' },
    { label: t('btn.save'), cls: 'primary', onclick: () => {
      const v = parseFloat(inp.value.replace(',', '.'));
      if (!(v > 0)) { inp.focus(); return false; }
      patch(id, { hours: { goals: { [appid]: Math.round(v * 10) / 10 } } });
    } },
  ]);
}

function addGameDialog(id) {
  const appid = field({ placeholder: 'AppID', inputmode: 'numeric' });
  const name = field({ placeholder: t('addGame.name') });
  const err = h('div.error');
  dialog(t('addGame.title'), [appid, name, err, hint(t('addGame.hint'))], [
    { label: t('btn.cancel'), cls: 'ghost' },
    { label: t('btn.add'), cls: 'primary', onclick: async () => {
      const n = parseInt((appid.value.match(/\d+/) || [''])[0], 10);
      try { await call('addGame', { id, appid: n || 0, name: name.value }); }
      catch (e) { err.textContent = tr(e.message); return false; }
    } },
  ]);
}
