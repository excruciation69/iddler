'use strict';
// Inventory manager: browse an inventory, price it, sell single items or every duplicate,
// manage your listings and search the Community Market.

// Steam currency codes → ISO codes for formatting.
const CURRENCIES = { 1: 'USD', 2: 'GBP', 3: 'EUR', 4: 'CHF', 5: 'RUB', 6: 'PLN', 7: 'BRL', 8: 'JPY', 9: 'NOK', 10: 'IDR', 11: 'MYR',
  12: 'PHP', 13: 'SGD', 14: 'THB', 15: 'VND', 16: 'KRW', 17: 'TRY', 18: 'UAH', 19: 'MXN', 20: 'CAD', 21: 'AUD', 22: 'NZD', 23: 'CNY',
  24: 'INR', 25: 'CLP', 26: 'PEN', 27: 'COP', 28: 'ZAR', 29: 'HKD', 30: 'TWD', 31: 'SAR', 32: 'AED', 34: 'ARS', 35: 'ILS', 37: 'KZT',
  38: 'KWD', 39: 'QAR', 40: 'CRC', 41: 'UYU' };
const moneyFmt = {};
function money(cents, cur) {
  const code = CURRENCIES[cur] || 'USD';
  const key = code + S.lang;
  moneyFmt[key] ||= new Intl.NumberFormat(locale(), { style: 'currency', currency: code, minimumFractionDigits: 2 });
  return moneyFmt[key].format((cents || 0) / 100);
}

// Market fees, the same rule the engine uses: Steam 5% + publisher 10%, each at least one cent.
const buyerPays = r => (r <= 0 ? 0 : r + Math.max(1, Math.floor(r * 5 / 100)) + Math.max(1, Math.floor(r * 10 / 100)));
function sellerGets(pay) {
  let r = Math.floor(pay * 100 / 115);
  while (buyerPays(r + 1) <= pay) r++;
  while (r > 0 && buyerPays(r) > pay) r--;
  return r;
}
const toCents = v => Math.round(parseFloat(String(v).replace(',', '.').replace(/[^\d.]/g, '')) * 100) || 0;
const fromCents = c => (c / 100).toFixed(2);

const INV_SOURCES = [
  [753, '6', 'inv.src.steam'], [730, '2', 'Counter-Strike 2'], [440, '2', 'Team Fortress 2'], [570, '2', 'Dota 2'], [252490, '2', 'Rust'],
];
const INV_KINDS = [['', 'inv.kind.all'], ['card', 'inv.kind.card'], ['foil', 'inv.kind.foil'], ['background', 'inv.kind.background'],
  ['emoticon', 'inv.kind.emoticon'], ['booster', 'inv.kind.booster'], ['gems', 'inv.kind.gems']];

function InventorySec(id, info) {
  const B = binder(id);
  const st = { tab: 'items', src: 0, items: [], loading: false, error: '', q: '', kind: '', onlySell: false, onlyDup: false,
    sort: 'name', picked: new Set(), prices: {}, pricing: false, listings: null, lPicked: new Set(), results: null, total: 0, mq: '', mApp: 0 };
  const cur = () => S.bots[id]?.currency || 1;
  const src = () => INV_SOURCES[st.src] || INV_SOURCES[0];

  // header numbers
  const kTotal = h('div.v'), kSell = h('div.v'), kDup = h('div.v'), kValue = h('div.v.acc');
  const strip = h('div.stats-strip.four', null,
    h('div', null, h('div.k', { text: t('inv.k.items') }), kTotal),
    h('div', null, h('div.k', { text: t('inv.k.marketable') }), kSell),
    h('div', null, h('div.k', { text: t('inv.k.dups') }), kDup),
    h('div', null, h('div.k', { text: t('inv.k.value') }), kValue));
  const tabs = h('div.seg');
  const body = h('div.col', { style: 'gap:14px' });
  const job = h('div', { hidden: true });
  const el = h('div.col', { style: 'gap:16px' }, strip, h('div.row.wrap', null, tabs, h('span.grow'),
    btn(t('inv.dups'), dupDialog, 'sm', 'tag'), btn('', () => load(true), 'sm icon', 'refresh')), job, body);

  function setTab(tb) {
    st.tab = tb;
    fill(tabs, [['items', 'inv.tab.items'], ['listings', 'inv.tab.listings'], ['market', 'inv.tab.market']].map(([k, l]) =>
      h('button' + (st.tab === k ? '.on' : ''), { type: 'button', onclick: () => setTab(k) }, t(l))));
    if (tb === 'listings' && !st.listings) loadListings();
    draw();
  }

  async function load(refresh) {
    const [appid, context] = src();
    st.loading = true; st.error = ''; draw();
    try {
      st.items = (await call('inventory', { id, appid, context, refresh })) || [];
      st.picked.clear();
    } catch (e) { st.error = tr(e.message); }
    st.loading = false;
    draw();
  }
  async function loadListings() {
    st.listings = null; draw();
    try { st.listings = (await call('listings', { id })) || []; } catch (e) { st.listings = []; toast(tr(e.message)); }
    st.lPicked.clear();
    draw();
  }
  async function price(g, book) {
    const k = g.appid + '|' + g.hashName;
    if (st.prices[k] && (!book || st.prices[k].highestBuy + st.prices[k].lowestSell > 0)) return st.prices[k];
    const p = await call('itemPrice', { id, appid: g.appid, hash: g.hashName, book }).catch(() => null);
    if (p) st.prices[k] = p;
    return p;
  }
  const priceOf = g => st.prices[g.appid + '|' + g.hashName];

  // fetch prices for what is on screen, one at a time (the market rate-limits bursts)
  async function priceAll() {
    if (st.pricing) { st.pricing = false; return; }
    st.pricing = true; draw();
    for (const g of visible().filter(g => g.marketable && !priceOf(g))) {
      if (!st.pricing || !el.isConnected) break;
      await price(g, false);
      paintTile(g);
      paintStats();
      await new Promise(r => setTimeout(r, 350));
    }
    st.pricing = false;
    draw();
  }

  function visible() {
    let list = st.items;
    if (st.kind) list = list.filter(g => g.kind === st.kind);
    if (st.onlySell) list = list.filter(g => g.marketable);
    if (st.onlyDup) list = list.filter(g => g.count > 1);
    if (st.q) list = list.filter(g => (g.name + ' ' + g.game + ' ' + g.type).toLowerCase().includes(st.q));
    const by = { name: (a, b) => a.name.localeCompare(b.name), count: (a, b) => b.count - a.count,
      price: (a, b) => ((priceOf(b)?.lowest || 0) - (priceOf(a)?.lowest || 0)) };
    return [...list].sort(by[st.sort]);
  }

  function paintStats() {
    const it = st.items;
    kTotal.textContent = it.reduce((n, g) => n + g.count, 0);
    kSell.textContent = it.filter(g => g.marketable).reduce((n, g) => n + g.count, 0);
    kDup.textContent = it.filter(g => g.count > 1).reduce((n, g) => n + g.count - 1, 0);
    const known = it.filter(g => priceOf(g));
    const sum = known.reduce((n, g) => n + (priceOf(g).median || priceOf(g).lowest) * g.count, 0);
    kValue.textContent = known.length ? money(sum, cur()) : '…';
  }

  // ---- tiles
  const tiles = new Map();
  function tile(g) {
    const sel = h('span.cbx', { onclick: e => { e.stopPropagation(); togglePick(g); } }, h('span.box'));
    const pr = h('div.pr');
    const el = h('div.itile', { onclick: () => itemDialog(g) }, sel,
      g.count > 1 ? h('span.cnt', { text: '×' + g.count }) : null,
      h('div.ico', { style: g.color ? `--c:#${g.color}` : '' }, g.icon ? h('img', { src: g.icon, alt: '', loading: 'lazy', decoding: 'async' }) : ic('box')),
      h('div.nm', { text: g.name }), h('div.gm', { text: g.game || g.type }), pr);
    tiles.set(g.key, { el, sel, pr });
    paintTile(g);
    return el;
  }
  function paintTile(g) {
    const r = tiles.get(g.key);
    if (!r) return;
    r.el.classList.toggle('on', st.picked.has(g.key));
    r.el.classList.toggle('off', !g.marketable);
    r.sel.firstChild.classList.toggle('on', st.picked.has(g.key));
    const p = priceOf(g);
    fill(r.pr, !g.marketable ? h('span.faint', { text: t('inv.notMarketable') }) : p ? h('b', { text: money(p.lowest || p.median, cur()) }) : h('span.faint', { text: '–' }));
  }
  function togglePick(g) {
    if (!g.marketable) return toast(t('inv.notMarketable'));
    st.picked.has(g.key) ? st.picked.delete(g.key) : st.picked.add(g.key);
    paintTile(g);
    paintBar();
  }

  // ---- selection bar
  const barText = h('span');
  const bar = h('div.selbar', { hidden: true }, h('span.badge', null, ic('check')), barText, h('span.grow'),
    btn(t('inv.clear'), () => { st.picked.clear(); st.items.forEach(paintTile); paintBar(); }, 'sm ghost'),
    btn(t('inv.sellSel'), () => sellDialog(st.items.filter(g => st.picked.has(g.key))), 'primary', 'tag'));
  function paintBar() {
    const sel = st.items.filter(g => st.picked.has(g.key));
    bar.hidden = !sel.length;
    barText.textContent = t('inv.picked', sel.length, sel.reduce((n, g) => n + g.count, 0));
  }

  // ---- sell dialog: copies and price per item, fees shown live
  async function sellDialog(groups) {
    if (!groups.length) return;
    const b = S.bots[id];
    const mode = b.cfg.market?.mode || 'lowest', under = b.cfg.market?.undercut ?? 1;
    const rows = groups.map(g => ({ g, copies: g.count, pays: 0, inp: null, get: null, cnt: null }));
    const tbody = h('div.sell-rows');
    const total = h('div.sell-total');
    const repaint = () => {
      let pay = 0, get = 0, n = 0;
      for (const r of rows) {
        r.get.textContent = r.pays ? money(sellerGets(r.pays), cur()) : '–';
        if (r.pays >= 3) { pay += r.pays * r.copies; get += sellerGets(r.pays) * r.copies; n += r.copies; }
      }
      fill(total, h('span', { text: t('inv.sellN', n) }), h('span.grow'),
        h('span.dim', { text: t('inv.buyersPay') + ' ' + money(pay, cur()) }), h('b', { text: t('inv.youGet') + ' ' + money(get, cur()) }));
    };
    for (const r of rows) {
      r.inp = field({ class: 'num money', inputmode: 'decimal', placeholder: '0.00' });
      r.inp.addEventListener('input', () => { r.pays = toCents(r.inp.value); repaint(); });
      r.get = h('span.mono');
      r.cnt = field({ class: 'num', inputmode: 'numeric', value: r.copies });
      r.cnt.addEventListener('input', () => { r.copies = Math.max(1, Math.min(r.g.count, parseInt(r.cnt.value, 10) || 1)); repaint(); });
      tbody.append(h('div.sell-row', null,
        h('div.ico.sm', { style: r.g.color ? `--c:#${r.g.color}` : '' }, r.g.icon ? h('img', { src: r.g.icon, alt: '' }) : ic('box')),
        h('div.col', { style: 'gap:2px;min-width:0;flex:1' }, h('b.ellipsis', { text: r.g.name }), h('span.faint.ellipsis', { style: 'font-size:11.5px', text: r.g.game || r.g.type })),
        h('label.col', { style: 'gap:2px' }, h('span.k', { text: t('inv.copies', r.g.count) }), r.cnt),
        h('label.col', { style: 'gap:2px' }, h('span.k', { text: t('inv.price') }), r.inp),
        h('div.col', { style: 'gap:2px;width:90px;text-align:right' }, h('span.k', { text: t('inv.get') }), r.get)));
    }
    const mx = h('div.row.wrap', { style: 'gap:8px' }, h('span.dim', { text: t('inv.autoPrice') }),
      ...[['lowest', 'inv.mode.lowest'], ['median', 'inv.mode.median'], ['buy', 'inv.mode.buy']].map(([m, l]) =>
        btn(t(l), () => autoPrice(m), 'xs' + (m === mode ? ' primary' : ''))));
    async function autoPrice(m) {
      for (const r of rows) {
        const p = await price(r.g, m === 'buy');
        const base = p ? ({ lowest: p.lowest, median: p.median, buy: p.highestBuy }[m] || p.lowest || p.median) : 0;
        r.pays = Math.max(3, base - under);
        if (!base) r.pays = 0;
        r.inp.value = r.pays ? fromCents(r.pays) : '';
        repaint();
      }
    }
    const confirmNote = b.canConfirm && b.cfg.market?.autoConfirm ? t('inv.autoConfirmOn') : b.canConfirm ? t('inv.confirmHere') : t('inv.confirmApp');
    dialog(t('inv.sellTitle'), [mx, tbody, total, h('p.hint', null, ic('info'), ' ', confirmNote)], [
      { label: t('btn.cancel'), cls: 'ghost' },
      { label: t('inv.list'), cls: 'primary', onclick: async () => {
        const orders = rows.filter(r => r.pays >= 3).map(r => ({ appid: r.g.appid, contextid: r.g.contextid, assets: r.g.assets.slice(0, r.copies), pays: r.pays }));
        if (!orders.length) { toast(t('err.priceTooLow')); return false; }
        try { await call('sell', { id, orders }); } catch (e) { toast(tr(e.message)); return false; }
        st.picked.clear();
        draw();
      } },
    ]);
    document.querySelector('.overlay:last-child .dialog')?.classList.add('wide');
    repaint();
    autoPrice(mode);
  }

  // ---- item details
  async function itemDialog(g) {
    const pbox = h('div.pgrid', null, h('div.empty', { text: g.marketable ? t('stats.loading') : t('inv.notMarketable') }));
    dialog(g.name, [
      h('div.item-head', null, h('div.ico.lg', { style: g.color ? `--c:#${g.color}` : '' }, g.icon ? h('img', { src: g.icon.replace('96fx96f', '184fx184f'), alt: '' }) : ic('box')),
        h('div.col', { style: 'gap:6px;min-width:0' }, h('div.dim', { text: g.type }), g.game ? h('div', { text: g.game }) : null,
          h('div.row.wrap', { style: 'gap:6px' }, h('span.chip', { text: '×' + g.count }),
            h('span.chip' + (g.marketable ? '.on' : ''), { text: g.marketable ? t('inv.marketable') : t('inv.notMarketable') }),
            h('span.chip' + (g.tradable ? '.on' : ''), { text: g.tradable ? t('inv.tradable') : t('inv.notTradable') })))),
      pbox,
    ], [
      ...(g.marketable ? [{ label: t('inv.openMarket'), cls: 'ghost', onclick: () => { openURL(`https://steamcommunity.com/market/listings/${g.appid}/${encodeURIComponent(g.hashName)}`); return false; } }] : []),
      { label: t('btn.close'), cls: 'ghost' },
      ...(g.marketable ? [{ label: t('inv.sellThis'), cls: 'primary', onclick: () => { setTimeout(() => sellDialog([g]), 0); } }] : []),
    ]);
    if (!g.marketable) return;
    const p = await price(g, true);
    paintTile(g);
    const cell = (k, v) => h('div', null, h('div.k', { text: t(k) }), h('div.v', { text: v }));
    fill(pbox, p ? [cell('inv.p.lowest', money(p.lowest, cur())), cell('inv.p.median', money(p.median, cur())),
      cell('inv.p.buy', p.highestBuy ? money(p.highestBuy, cur()) : '–'), cell('inv.p.volume', String(p.volume || 0)),
      cell('inv.p.youGet', money(sellerGets(p.lowest || p.median), cur()))] : h('div.empty', { text: t('inv.noPrice') }));
  }

  // ---- sell all duplicates
  function dupDialog() {
    const b = S.bots[id];
    const m = b.cfg.market || {};
    let mode = m.mode || 'lowest';
    const kinds = new Set(src()[0] === 753 ? ['card', 'background', 'emoticon'] : []);
    const under = field({ class: 'num', inputmode: 'numeric', value: m.undercut ?? 1 });
    const min = field({ class: 'num money', inputmode: 'decimal', value: m.min ? fromCents(m.min) : '', placeholder: '–' });
    const max = field({ class: 'num money', inputmode: 'decimal', value: m.max ? fromCents(m.max) : '', placeholder: '–' });
    const n = st.items.filter(g => g.marketable && g.count > 1 && (!kinds.size || kinds.has(g.kind))).reduce((s, g) => s + g.count - 1, 0);
    const modeBox = h('div.col');
    const paintMode = () => fill(modeBox, [['lowest', 'inv.mode.lowest'], ['median', 'inv.mode.median'], ['buy', 'inv.mode.buyFast']].map(([v, l]) => {
      const r = makeToggle('radio', t(l), mode === v, on => { if (on) mode = v; });
      r.input.name = 'dupmode';
      return r;
    }));
    paintMode();
    const kindBox = src()[0] === 753 ? h('div.row.wrap', { style: 'gap:6px' }, INV_KINDS.slice(1).map(([k, l]) =>
      check(t(l), kinds.has(k), on => (on ? kinds.add(k) : kinds.delete(k))))) : null;
    dialog(t('inv.dupsTitle'), [
      hint(t('inv.dupsText', n)),
      kindBox && h('div.label', { text: t('inv.dupsKinds') }), kindBox,
      h('div.label', { text: t('inv.dupsPrice') }), modeBox,
      h('div.trio', null, [[t('inv.undercut'), under], [t('inv.min'), min], [t('inv.max'), max]].map(([l, f]) =>
        h('label.col', { style: 'gap:4px' }, h('span.k', { text: l }), f))),
    ], [
      { label: t('btn.cancel'), cls: 'ghost' },
      { label: t('inv.dupsGo'), cls: 'primary', onclick: async () => {
        const cfg = { mode, undercut: Math.max(0, parseInt(under.value, 10) || 0), min: toCents(min.value), max: toCents(max.value) };
        patch(id, { market: cfg });
        try {
          const [appid, context] = src();
          const count = await call('sellDuplicates', { id, appid, context, mode: cfg.mode, undercut: cfg.undercut, min: cfg.min, max: cfg.max, kinds: [...kinds] });
          toast(count ? t('inv.dupsStarted', count) : t('inv.dupsNone'));
        } catch (e) { toast(tr(e.message)); return false; }
      } },
    ]);
  }

  // ---- listings
  function drawListings() {
    if (st.listings == null) return fill(body, h('div.empty', { text: t('stats.loading') }));
    const ls = st.listings;
    const sum = ls.reduce((n, l) => n + l.receive, 0);
    const pickedN = ls.filter(l => st.lPicked.has(l.id)).length;
    const rows = ls.map(l => h('div.tr' + (st.lPicked.has(l.id) ? '.on' : ''), null,
      h('div.c-cb', null, h('span.cbx', { onclick: () => { st.lPicked.has(l.id) ? st.lPicked.delete(l.id) : st.lPicked.add(l.id); drawListings(); } },
        h('span.box' + (st.lPicked.has(l.id) ? '.on' : '')))),
      h('div.c-n', null, h('span.ico.xs', null, l.icon ? h('img', { src: l.icon, alt: '', loading: 'lazy' }) : ic('box')), h('span.ellipsis', { text: l.name }),
        l.pending ? h('span.chip.warn', { text: t('inv.pending') }) : null),
      h('div.c-s.c-r.mono', { text: money(l.pays, cur()) }),
      h('div.c-s.c-r.mono.acc', { text: money(l.receive, cur()) }),
      h('div.c-d.mono.hide-sm', { text: l.created ? fmtDate(l.created) : '' })));
    fill(body,
      h('div.toolbar', null, h('span.dim', { text: t('inv.listingsSum', ls.length, money(sum, cur())) }), h('span.grow'),
        pickedN ? btn(t('inv.removeSel', pickedN), () => removeListings([...st.lPicked]), 'sm', 'x') : null,
        btn(t('inv.removeAll'), () => confirmDialog(t('inv.removeAll'), t('inv.removeAllText', ls.length), t('inv.removeAll'), () => removeListings([])), 'sm ghost danger', 'trash'),
        btn('', loadListings, 'sm icon', 'refresh')),
      table([{ cls: 'c-cb' }, { cls: 'c-n', label: t('inv.item') }, { cls: 'c-s c-r', label: t('inv.buyerPays') }, { cls: 'c-s c-r', label: t('inv.get') },
        { cls: 'c-d hide-sm', label: t('keys.date') }], rows, t('inv.noListings')));
  }
  async function removeListings(ids) {
    if (!ids.length && !st.listings?.length) return;
    try { await call('removeListings', { id, ids }); toast(t('inv.removing')); } catch (e) { toast(tr(e.message)); }
  }

  // ---- market search
  const mInput = field({ placeholder: t('inv.searchMarket'), class: 'grow' });
  const mApp = h('select.field', { style: 'width:auto', onchange: () => { st.mApp = Number(mApp.value); runSearch(0); } },
    h('option', { value: '0', text: t('inv.allGames') }), INV_SOURCES.map(([a, , l]) => h('option', { value: String(a), text: l.includes('.') ? t(l) : l })));
  mInput.addEventListener('keydown', e => { if (e.key === 'Enter') runSearch(0); });
  async function runSearch(start) {
    st.mq = mInput.value.trim();
    if (!start) { st.results = null; drawMarket(); }
    try {
      const r = await call('marketSearch', { id, query: st.mq, appid: st.mApp, start });
      st.results = start ? [...(st.results || []), ...(r.items || [])] : (r.items || []);
      st.total = r.total || 0;
    } catch (e) { st.results = []; toast(tr(e.message)); }
    drawMarket();
  }
  function drawMarket() {
    const res = st.results;
    const grid = res == null ? h('div.empty', { text: t('stats.loading') }) : !res.length ? h('div.empty', { text: st.mq ? t('inv.nothingFound') : t('inv.searchHint') })
      : h('div.items', null, res.map(r => h('div.itile.market', { onclick: () => openURL(`https://steamcommunity.com/market/listings/${r.appid}/${encodeURIComponent(r.hashName)}`) },
        h('div.ico', null, r.icon ? h('img', { src: r.icon, alt: '', loading: 'lazy', decoding: 'async' }) : ic('box')),
        h('div.nm', { text: r.name }), h('div.gm', { text: r.game }),
        h('div.pr', null, h('b', { text: r.text || money(r.price, cur()) }), h('span.faint', { text: ' · ' + t('inv.offers', r.listings) })))));
    fill(body, h('div.toolbar', null, h('div.search.grow', null, ic('search'), mInput), mApp, btn(t('inv.find'), () => runSearch(0), 'primary', 'search')),
      grid, res && res.length < st.total ? h('div.row', { style: 'justify-content:center' }, btn(t('inv.more'), () => runSearch(res.length), 'sm')) : null);
  }

  // ---- inventory tab
  const srcSel = h('select.field', { onchange: () => { st.src = Number(srcSel.value); st.kind = ''; load(false); } },
    INV_SOURCES.map(([, , l], i) => h('option', { value: String(i), text: l.includes('.') ? t(l) : l })));
  const qBox = search(t('inv.search'), v => { st.q = v; drawItems(); });
  function drawItems() {
    tiles.clear();
    const list = visible();
    const kinds = src()[0] === 753 ? h('div.seg.kinds', null, INV_KINDS.map(([k, l]) =>
      h('button' + (st.kind === k ? '.on' : ''), { type: 'button', onclick: () => { st.kind = k; drawItems(); } }, t(l)))) : null;
    const sortSel = h('select.field', { onchange: () => { st.sort = sortSel.value; drawItems(); } },
      [['name', 'inv.sort.name'], ['count', 'inv.sort.count'], ['price', 'inv.sort.price']].map(([v, l]) => h('option', { value: v, text: t(l) })));
    sortSel.value = st.sort;
    const grid = st.loading ? h('div.items', null, Array.from({ length: 12 }, () => h('div.itile.skel')))
      : st.error ? h('div.empty', { text: st.error })
      : !list.length ? h('div.empty', { text: st.items.length ? t('hours.nothing') : t('inv.empty') })
      : h('div.items', null, list.map(tile));
    fill(body,
      h('div.toolbar', null, srcSel, qBox, sortSel, h('span.grow'),
        btn(st.pricing ? t('inv.stopPrices') : t('inv.prices'), priceAll, 'sm' + (st.pricing ? '' : ' primary'), st.pricing ? 'stop' : 'tag')),
      h('div.toolbar', null, kinds, h('span.grow'),
        check(t('inv.onlySell'), st.onlySell, v => { st.onlySell = v; drawItems(); }),
        check(t('inv.onlyDup'), st.onlyDup, v => { st.onlyDup = v; drawItems(); })),
      grid, bar,
      group(t('inv.settings'),
        h('div.row.wrap', { style: 'gap:18px' }, B.switch('market.autoConfirm', t('inv.autoConfirm'))),
        hint(S.bots[id]?.canConfirm ? t('inv.autoConfirmHint') : t('inv.needMaFile'))));
    paintBar();
  }

  function draw() {
    paintStats();
    if (st.tab === 'items') drawItems();
    else if (st.tab === 'listings') drawListings();
    else { if (st.results == null && !st.mq) st.results = []; drawMarket(); }
  }

  // ---- job progress (selling or removing), live from the engine
  const jm = memo();
  function paintJob(b) {
    const j = b.marketJob;
    const fresh = j && (j.running || Date.now() / 1000 - j.at < 120);
    jm([j, fresh, b.canConfirm, S.lang], () => {
      job.hidden = !fresh;
      if (!fresh) return fill(job);
      const pct = j.total ? (j.done + j.failed) / j.total * 100 : 0;
      fill(job, h('div.jobcard', null,
        h('div.row', null, j.running ? ic('spin', 'spin') : ic('check'),
          h('b', { text: t(j.kind === 'sell' ? 'inv.job.sell' : 'inv.job.remove', j.done, j.total) }),
          j.failed ? h('span.err', { text: t('inv.job.failed', j.failed) }) : null, h('span.grow'),
          j.pending && b.canConfirm ? btn(t('inv.confirmAll', j.pending), async () => {
            try { const n = await call('confirmMarket', { id }); toast(t('inv.confirmed', n)); } catch (e) { toast(tr(e.message)); }
          }, 'sm primary', 'check') : null,
          j.running ? btn(t('btn.stop'), () => act('stopMarket', { id }).catch(() => {}), 'sm ghost', 'stop') : null),
        h('div.bar', null, h('i', { style: `width:${pct}%` })),
        j.pending && !b.canConfirm ? hint(t('inv.confirmAppN', j.pending)) : null,
        j.error && j.failed ? h('div.faint', { style: 'font-size:12px', text: tr(j.error) }) : null));
      if (!j.running && st.wasRunning) {
        if (st.tab === 'listings' || j.kind === 'remove') loadListings();
        if (j.kind === 'sell') load(false);
      }
      st.wasRunning = j.running;
    });
  }

  setTab('items');
  load(false);
  return {
    el,
    update(b) {
      B.sync();
      paintJob(b);
      info.textContent = b.status === 'online' ? '' : t('st.offline');
    },
  };
}
