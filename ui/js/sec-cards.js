'use strict';
// Card farming.

// ---------------------------------------------------------------- cards

// Card farming: the current game up top, the session counters, then the queue as capsules.
function CardsSec(id, info) {
  const B = binder(id);
  const leftText = h('span.mono');
  info.append(h('span', { text: t('cards.left') }), leftText);
  const heroBg = h('div.bg');
  const kicker = h('div.kicker');
  const heroTitle = h('h2');
  const heroMeta = h('div.meta');
  const runBtn = h('button.btn.big', { type: 'button', onclick: () => act('run', { id, module: 'cards', on: !S.bots[id].cardsOn }).catch(() => {}) });
  const settingsBtn = h('button.btn.big.icon', { type: 'button', 'data-tip': t('cards.settings'), onclick: () => opts.hidden = !opts.hidden }, ic('sliders'));
  const hero = h('section.hero', null, heroBg, h('div.actions', null, settingsBtn, runBtn), kicker, heroTitle, heroMeta);
  // black list: never farm; white list: farm only these. Both are edited from the queue below.
  const lists = h('div.col', { style: 'gap:10px' });
  const setList = (key, appid, on) => {
    const cur = new Set(S.bots[id].cfg.cards[key] || []);
    on ? cur.add(appid) : cur.delete(appid);
    patch(id, { cards: { [key]: [...cur] } });
    onData();
  };
  const nameOf = (b, appid) => [...(b.cards || []), ...(b.cardsSkipped || [])].find(c => c.appid === appid)?.name
    || (S.games[id] || []).find(g => g.appid === appid)?.name || String(appid);
  const ml = memo();
  function paintLists(b) {
    const c = b.cfg.cards;
    ml([c.blacklist, c.whitelist, b.cardsSkipped, S.lang], () => {
      const chips = (key, cls) => (c[key] || []).map(a => h('span.chip' + cls, null, nameOf(b, a),
        h('span.x', { onclick: () => setList(key, a, false) }, ic('x'))));
      fill(lists,
        h('div.label', { text: t('cards.only') }), (c.whitelist || []).length ? h('div.row.wrap', { style: 'gap:6px' }, chips('whitelist', '.on')) : hint(t('cards.onlyNone')),
        h('div.label', { text: t('cards.skip') }), (c.blacklist || []).length ? h('div.row.wrap', { style: 'gap:6px' }, chips('blacklist', '')) : hint(t('cards.skipNone')));
    });
  }
  const opts = h('div.grid2', { hidden: true },
    group(t('cards.mode'), B.radios('cards.mode', [['one', t('cards.one')], ['many', t('cards.many')]])),
    group(t('cards.limit'), B.check('cards.twoHour', t('cards.twoHour'))),
    group(t('cards.after'), B.switch('cards.thenHours', t('cards.thenHours')), hint(t('cards.thenHoursHint'))),
    group(t('cards.lists'), lists));
  const vDropped = h('div.v.acc'), vLeft = h('div.v'), prText = h('span'), prBar = h('div.bar.thick', null, h('i'));
  const statsStrip = h('div.stats-strip', null,
    h('div', null, h('div.k', { text: t('cards.dropped') }), vDropped),
    h('div', null, h('div.k', { text: t('cards.toDrop') }), vLeft),
    h('div.pr', null, h('div.row', null, h('span.dim', { text: t('cards.state') }), prText), prBar));
  const next = h('div.games');
  const drops = h('div.feed');
  const el = h('div.col', { style: 'gap:18px' }, hero, opts, statsStrip,
    h('h3.sect', { text: t('cards.upNext') }), next,
    group(t('cards.recent'), drops));
  const m1 = memo(), m2 = memo();
  let base = null; // drops left when this page first saw farming running, to count this session
  return {
    el,
    update(b) {
      B.sync();
      const cards = b.cards || [];
      const total = cards.reduce((n, c) => n + c.dropsLeft, 0);
      leftText.textContent = total;
      const current = cards.find(c => (b.playingIDs || []).includes(c.appid));
      if (b.cardsOn && base == null) base = total;
      if (!b.cardsOn) base = null;
      const sessionDrops = (b.drops || []).filter(d => d.at * 1000 >= Date.now() - (b.sessionMs || 0)).reduce((n, d) => n + d.count, 0);
      const lead = current || cards[0];
      if (lead) art(heroBg, headerArt(lead.appid), '', lead.appid);
      fill(kicker, b.cardsOn ? [ic('spin', 'spin'), t('hero.farming')] : [ic('cards'), t('mod.cards')]);
      heroTitle.textContent = current ? current.name : b.cardsOn ? t('cards.checking') : cards.length ? t('cards.readyN', cards.length) : t('cards.idleTitle');
      fill(heroMeta, !current && !cards.length && !b.cardsOn ? [h('span.dim', { text: t('cards.hint') })] : current
        ? [h('span', null, h('b', { text: current.dropsLeft }), ' ', t('cards.dropsLeft')), h('div.drops', null, Array.from({ length: Math.min(current.dropsLeft, 12) }, () => h('i'))),
          b.cfg.cards.twoHour && current.hours < 2 ? h('span.chip.on', { text: t('cards.waiting2h') }) : null]
        : [h('span.dim', { text: b.cardsAt ? t('cards.checked', fmtTime(b.cardsAt), cards.length) : '' })]);
      setButton(runBtn, b.cardsOn ? 'stop' : 'play', b.cardsOn ? t('cards.stop') : t('cards.start'), 'btn big ' + (b.cardsOn ? 'stop' : 'primary'));
      vDropped.textContent = sessionDrops;
      vLeft.textContent = total;
      const all = total + sessionDrops;
      prText.textContent = t('cards.ofN', sessionDrops, all);
      prBar.firstChild.style.width = (all ? sessionDrops / all * 100 : 0) + '%';
      paintLists(b);
      m1([cards, b.playingIDs, b.cardsAt, b.cardsOn, S.lang], () => {
        const queue = cards.filter(c => c !== current);
        fill(next, queue.length ? queue.map(c => {
          const live = (b.playingIDs || []).includes(c.appid);
          return h('div.gcard' + (live ? '.live' : ''), null, art(h('div.art'), capsule(c.appid), '', c.appid),
            h('span.tag.l', null, live ? ic('spin', 'spin') : ic('clock'), c.hours.toFixed(1) + ' ' + t('u.h')),
            h('span.tag.r', null, ic('cards'), String(c.dropsLeft)),
            h('div.nm', { text: c.name }),
            h('div.acts', null,
              h('button.btn', { type: 'button', 'data-tip': t('cards.onlyTip'), onclick: () => setList('whitelist', c.appid, true) }, ic('star'), h('span', { text: t('cards.onlyBtn') })),
              h('button.btn.icon', { type: 'button', 'data-tip': t('cards.skipTip'), onclick: () => setList('blacklist', c.appid, true) }, ic('x'))));
        }) : h('div.empty', { style: 'grid-column:1/-1', text: !b.cardsAt ? (b.cardsOn ? t('cards.checking') : t('cards.hint')) : t('cards.none') }));
      });
      m2([b.drops, S.lang], () => {
        const d = b.drops || [];
        fill(drops, ...(d.length ? d.map(x => h('div.ln', null, h('span.ts', { text: fmtTime(x.at) }),
          h('span.src', { text: '+' + x.count }), h('span', { text: x.game }))) : [hint(t('cards.noDrops'))]));
      });
    },
  };
}
function appIcon(id, appid) { return (S.games[id] || []).find(g => g.appid === appid)?.icon || ''; }
