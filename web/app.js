// La interfaz se construye con nodos y textContent: los nombres de producto son cadenas de
// terceros y nunca llegan a ser HTML.
'use strict';

const CHAIN_COLOUR = {
  dia: '#E30613', mercadona: '#009660', mas: '#C9C400', aldi: '#1D3587',
  lidl: '#0050AA', carrefour: '#295BA7', alcampo: '#E30613'
};
const CODE = {
  quota: 'sin cuota', blocked: 'bloqueada', timeout: 'sin respuesta',
  network: 'sin red', parse: 'ilegible', unknown: 'sin respuesta'
};
const LEVEL_CLASS = { exacta: 'exact', equivalente: 'same', probable: 'maybe', elegida: 'picked' };

const $ = (sel, root = document) => root.querySelector(sel);

function el(tag, props = {}, ...kids) {
  const node = document.createElement(tag);
  for (const [k, v] of Object.entries(props)) {
    if (k === 'class') node.className = v;
    else if (k === 'text') node.textContent = v;
    else if (k === 'style') node.setAttribute('style', v);
    else if (k.startsWith('data-') || k.startsWith('aria-')) node.setAttribute(k, v);
    else node[k] = v;
  }
  for (const kid of kids) if (kid != null) node.append(kid);
  return node;
}

const money = (cents) => cents == null
  ? '—'
  : (cents / 100).toLocaleString('es-ES', { minimumFractionDigits: 2, maximumFractionDigits: 2 }) + ' €';

const unitMoney = (cents, unit) => {
  if (cents == null) return '';
  const u = unit === 'ud' ? 'ud' : unit === 'kg' ? 'kg' : unit === 'L' ? 'L' : unit || '';
  return `${money(cents)}/${u}`;
};

const label = (store) => (state.stores.find((s) => s.store === store) || {}).label || store;
const sectionLabel = (key) => (state.sections.find((s) => s.key === key) || {}).label || key;

const dot = (store) => el('i', { class: 'dot', style: `--chain:${CHAIN_COLOUR[store] || '#999'}` });

// The seven chain marks are the chain's name in the basket column. They live in one SVG sprite
// so each row is a <use> of local markup and the page makes no request per logo.
let logosReady = false;

async function loadLogos() {
  try {
    const res = await fetch('/logos.svg');
    if (!res.ok) return;
    const doc = new DOMParser().parseFromString(await res.text(), 'image/svg+xml');
    if (!doc.documentElement || doc.querySelector('parsererror')) return;
    document.body.prepend(document.importNode(doc.documentElement, true));
    logosReady = Boolean(document.getElementById('logo-dia'));
  } catch {
    logosReady = false;
  }
}

// Each mark is cropped to its own drawing (measured with getBBox in the shared 106 x 30 space),
// because the official vectors sit inside that box with very different aspect ratios: without the
// crop, DIA looked tiny and Mercadona stretched. The viewport is fixed, so the marks fit it and
// stay optically balanced.
// Each mark is cropped to its own drawing (measured with getBBox in the shared 106 x 30 space) and
// sized by hand: the official vectors have very different proportions — Mercadona is a 8,8:1
// wordmark and DIA almost square — so one fixed box made DIA tiny and Mercadona unreadable. The
// height is what is matched optically; the width follows the brand's own proportion.
const LOGO_SIZE = {
  alcampo: { w: 74, h: 18 },
  aldi: { w: 54, h: 18 },
  carrefour: { w: 104, h: 13 },
  dia: { w: 35, h: 20 },
  lidl: { w: 61, h: 17 },
  mas: { w: 60, h: 19 },
  mercadona: { w: 110, h: 13 }
};

function chainMark(store, fallbackName) {
  const size = LOGO_SIZE[store];
  if (!logosReady || !size) {
    if (fallbackName) return el('span', { class: 'logo-fallback', text: fallbackName });
    return dot(store);
  }
  const box = el('span', { class: 'cart-logo' });
  const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
  svg.setAttribute('class', 'logo');
  svg.setAttribute('width', String(size.w));
  svg.setAttribute('height', String(size.h));
  svg.setAttribute('aria-hidden', 'true');
  const use = document.createElementNS('http://www.w3.org/2000/svg', 'use');
  use.setAttribute('href', `#logo-${store}`);
  use.setAttribute('width', '100%');
  use.setAttribute('height', '100%');
  svg.append(use);
  box.append(svg);
  return box;
}

let state = null;
let timer = null;
let timerOpen = null;
let shopStore = null;
const openKeys = new Set();

async function api(method, path, body) {
  const res = await fetch(path, {
    method,
    headers: body ? { 'Content-Type': 'application/json' } : {},
    body: body ? JSON.stringify(body) : undefined
  });
  if (!res.ok) {
    const detail = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(detail.error || res.statusText);
  }
  return res.status === 204 ? null : res.json();
}

function note(text, alert = false) {
  const node = $('#status');
  node.textContent = text || '';
  node.classList.toggle('alert', Boolean(alert));
}

function apply(doc) {
  state = doc;
  render();
}

async function act(promise, message) {
  try {
    apply(await promise);
    note(message || '');
  } catch (err) {
    note(err.message, true);
  }
}

// ---------- tus supermercados ----------

function renderStores() {
  const host = $('#stores');
  host.replaceChildren();
  const chosen = state.stores.filter((s) => s.selected);
  $('#stores-count').textContent = `(${chosen.length} de ${state.stores.length})`;
  for (const store of state.stores) {
    const chip = el('button', {
      class: 'store-chip', type: 'button', 'data-store': store.store,
      'aria-pressed': String(store.selected), 'aria-label': store.label
    });
    chip.append(el('span', { class: 'check', text: '✓', 'aria-hidden': 'true' }));
    chip.append(chainMark(store.store, store.label));
    chip.addEventListener('click', () => toggleStore(store.store));
    host.append(chip);
  }
}

async function toggleStore(store) {
  const chosen = state.stores.filter((s) => s.selected).map((s) => s.store);
  const next = chosen.includes(store) ? chosen.filter((s) => s !== store) : [...chosen, store];
  if (!next.length) {
    note('Deja al menos un supermercado en la comparativa.', true);
    return;
  }
  await act(api('POST', '/api/stores', { chains: next }),
    next.length < chosen.length ? 'Fuera de la comparativa.' : 'Añadido; se pedirá en los productos que falten.');
}

function render() {
  const pending = state.items.some((i) => i.resolving);
  const engine = $('#engine');
  engine.classList.toggle('bad', !state.reachable);
  engine.textContent = !state.reachable
    ? 'motor de catálogo sin conexión'
    : pending ? 'buscando equivalentes…' : 'al día';

  const confirmed = state.carts.filter((c) => c.count > 0).length;
  const today = new Date().toLocaleDateString('es-ES', { day: '2-digit', month: 'long', year: 'numeric' });
  $('#meta').textContent = `${state.items.length} producto${state.items.length === 1 ? '' : 's'} · ` +
    `${confirmed}/${state.carts.length} cadenas con cesta · ${today}`;

  renderStores();
  renderList();
  renderCarts();
  if (!shopStore) {
    const wanted = new URL(location.href).searchParams.get('tienda');
    if (wanted && state.carts.some((c) => c.store === wanted)) openShop(wanted);
  }
  $('#retry').hidden = !state.carts.some((c) => c.retry) && !state.items.some((i) => i.error);
  $('#recompute').hidden = !state.items.some((i) => i.anchor && i.cells.some((c) => c.alts && c.alts.length));
  if (shopStore) renderShop(shopStore);

  clearTimeout(timer);
  if (pending) timer = setTimeout(refresh, 2500);
}

async function refresh() {
  try {
    apply(await api('GET', '/api/state'));
    return state;
  } catch {
    $('#engine').classList.add('bad');
    $('#engine').textContent = 'motor de catálogo sin conexión';
  }
}

// ---------- lista ----------

function renderList() {
  const host = $('#sections');
  host.querySelectorAll('.more[open]').forEach((d) => d.dataset.key && openKeys.add(d.dataset.key));
  host.replaceChildren();
  $('#empty').hidden = state.items.length > 0;
  $('#list-count').textContent = state.items.length ? `(${state.items.length})` : '';

  const order = state.sections.map((s) => s.key);
  for (const key of order) {
    const items = state.items.filter((i) => (i.section || 'otros') === key);
    if (!items.length) continue;
    const block = el('section', { class: 'section-block' });
    block.append(el('h3', { class: 'section-h' }, document.createTextNode(sectionLabel(key)),
      el('span', { class: 'count', text: `(${items.length})` })));
    const list = el('ol', { class: 'list' });
    for (const item of items) list.append(renderItem(item));
    block.append(list);
    host.append(block);
  }
}

function renderItem(item) {
  const node = $('#item-tpl').content.firstElementChild.cloneNode(true);
  node.dataset.id = item.id;
  $('.q', node).textContent = item.query;
  $('.qty-n', node).textContent = item.quantity;

  const select = $('.section', node);
  for (const s of state.sections) {
    const opt = el('option', { value: s.key, text: s.label });
    if (s.key === (item.section || 'otros')) opt.selected = true;
    select.append(opt);
  }

  const base = $('.base', node);
  if (item.anchor) {
    base.append(document.createTextNode('Base: '),
      el('b', { text: item.anchor.name }),
      document.createTextNode(` · ${label(item.anchor.store)} · ${money(item.anchor.priceCents)}`));
    if (item.anchor.unitPriceCents != null) {
      base.append(document.createTextNode(` (${unitMoney(item.anchor.unitPriceCents, item.anchor.unit)})`));
    }
  } else {
    base.textContent = item.resolving ? 'Buscando la base en las cadenas…' : 'Sin base todavía';
  }

  if (item.error) {
    const line = $('.item-note', node);
    line.hidden = false;
    line.textContent = item.error;
  }

  const cells = $('.cells', node);
  cells.style.setProperty('--chains', String((item.cells || []).length || 1));
  for (const cell of item.cells || []) cells.append(renderCell(item, cell));
  return node;
}

function renderCell(item, cell) {
  const level = cell.level;
  const node = el('li', { class: `cell ${level}` });
  node.append(el('span', { class: 'chain' }, dot(cell.store), document.createTextNode(label(cell.store))));

  if (level === 'pending') {
    node.append(el('span', { class: 'pending', text: 'buscando…' }));
    return node;
  }
  if (level === 'unavailable') {
    node.append(el('span', { class: 'off', text: CODE[cell.code] || 'no disponible' }));
    return node;
  }
  if (level === 'no' || !cell.product) {
    node.append(el('span', { class: 'none', text: 'no encontrado' }));
    return node;
  }

  const p = cell.product;
  node.append(el('span', { class: 'price', text: money(p.priceCents) }));
  if (p.unitPriceCents != null) node.append(el('span', { class: 'unit', text: unitMoney(p.unitPriceCents, p.unit) }));
  node.append(el('span', { class: 'pname', text: p.name }));
  node.append(el('span', { class: `tag ${LEVEL_CLASS[level] || ''}`, text: level }));
  if (cell.reason) node.append(el('span', { class: 'reason', text: cell.reason }));

  const isBase = item.anchor && item.anchor.store === cell.store && item.anchor.id === p.id;
  const extra = (cell.alts || []).length + (isBase ? 0 : 1);
  if (extra > 0) {
    const key = `${item.id}|${cell.store}`;
    const more = el('details', { class: 'more', 'data-key': key });
    more.open = openKeys.has(key);
    more.addEventListener('toggle', () => (more.open ? openKeys.add(key) : openKeys.delete(key)));
    more.append(el('summary', { text: extra === 1 ? '1 opción' : `${extra} opciones` }));
    const alts = el('div', { class: 'alts' });
    if (!isBase) {
      alts.append(el('button', {
        class: 'alt', type: 'button', text: 'usar este como base',
        'data-anchor': `${cell.store}|${p.id}`
      }));
    }
    for (const alt of cell.alts || []) {
      alts.append(el('button', {
        class: 'alt', type: 'button', title: alt.reason || '',
        'data-pick': `${cell.store}|${alt.product.id}`
      }, el('span', { text: alt.product.name }), el('b', { text: money(alt.product.priceCents) })));
    }
    more.append(alts);
    node.append(more);
  }
  return node;
}

// ---------- cestas ----------

function renderCarts() {
  const host = $('#carts');
  host.replaceChildren();
  for (const cart of state.carts) {
    const empty = cart.count === 0;
    const row = el('button', {
      class: `cart${empty ? ' empty' : ''}`, type: 'button', 'data-shop': cart.store,
      'aria-label': `${cart.label}: ${empty ? 'sin productos confirmados' : money(cart.totalCents)}, ${cart.count} de ${cart.items} productos`
    });
    row.append(
      el('span', { class: 'cart-name' }, chainMark(cart.store)),
      el('span', { class: 'cart-total', text: empty ? '—' : money(cart.totalCents) }),
      el('span', { class: 'cart-count', text: `${cart.count}/${cart.items}` })
    );
    host.append(row);
  }
}

// ---------- selector de producto ----------

function renderPicker(doc) {
  const host = $('#picker-chains');
  host.replaceChildren();
  $('.picker-title', $('#picker')).textContent = `${doc.results.reduce((n, c) => n + c.products.length, 0)} productos para «${doc.query}»`;
  for (const chain of doc.results) {
    const block = el('div', { class: 'picker-chain' });
    const left = el('div');
    left.append(el('span', { class: 'picker-chain-name' }, dot(chain.store), document.createTextNode(chain.label)));
    block.append(left);
    if (chain.code) {
      const right = el('div');
      right.append(el('span', { class: 'picker-chain-state', text: CODE[chain.code] || chain.code }));
      block.append(right);
      host.append(block);
      continue;
    }
    const list = el('div', { class: 'picker-products' });
    if (!chain.products.length) {
      list.append(el('span', { class: 'picker-chain-state', text: 'no está en su catálogo' }));
    }
    for (const product of chain.products) {
      const b = el('button', { class: 'pick', type: 'button', title: product.name });
      b.append(el('span', { text: product.name }));
      const price = product.unitPriceCents != null
        ? `${money(product.priceCents)} · ${unitMoney(product.unitPriceCents, product.unit)}`
        : money(product.priceCents);
      b.append(el('b', { text: price }));
      b.addEventListener('click', () => addChosen(product));
      list.append(b);
    }
    block.append(list);
    host.append(block);
  }
  $('#picker').hidden = false;
}

function closePicker() {
  $('#picker').hidden = true;
  $('#picker-chains').replaceChildren();
}

async function addChosen(product) {
  const term = $('#q').value.trim() || product.name;
  closePicker();
  $('#q').value = '';
  await act(api('POST', '/api/items', { query: term, anchor: product }),
    'Añadido con ese producto como base. Buscando sus equivalentes…');
  $('#q').focus();
}

async function search(term) {
  const button = $('.btn.primary', $('#add'));
  button.disabled = true;
  note('Buscando en las siete cadenas…');
  try {
    renderPicker(await api('POST', '/api/search', { query: term }));
    note('');
  } catch (err) {
    note(err.message, true);
  } finally {
    button.disabled = false;
  }
}

// ---------- modo compra ----------

function renderShop(store) {
  const cart = state.carts.find((c) => c.store === store);
  if (!cart) return;
  $('#shop-h').textContent = cart.label;
  const count = cart.lines.reduce((n, l) => n + l.quantity, 0);
  $('#shop-meta').textContent = `${count} de ${cart.items} productos · ${cart.count ? money(cart.totalCents) : 'sin total confirmado'}`;

  const order = state.sections.map((s) => s.key);
  const lines = [...cart.lines].sort((a, b) => order.indexOf(a.section) - order.indexOf(b.section));
  const list = $('#shop-list');
  list.replaceChildren();
  for (const line of lines) {
    const id = `chk-${Math.random().toString(36).slice(2, 8)}`;
    const box = el('input', { type: 'checkbox', id });
    const name = el('label', { class: 'shop-name', htmlFor: id },
      document.createTextNode(line.name),
      el('small', {
        text: [sectionLabel(line.section), line.quantity > 1 ? `${line.quantity} × ${money(line.priceCents)}` : '',
          line.unitPriceCents != null ? unitMoney(line.unitPriceCents, line.unit) : '']
          .filter(Boolean).join(' · ')
      }));
    list.append(el('li', {}, box, name, el('span', { class: 'shop-price', text: money(line.priceCents * line.quantity) })));
  }
  $('#shop-missing').textContent = cart.missing.length
    ? `No encontrado en ${cart.label}: ${cart.missing.join(', ')}.`
    : 'Está todo lo de la lista.';
}

function openShop(store) {
  shopStore = store;
  const url = new URL(location.href);
  url.searchParams.set('tienda', store);
  history.replaceState(null, '', url);
  renderShop(store);
  const shop = $('#shop');
  shop.hidden = false;
  for (const sel of ['header.masthead', 'main', 'footer']) {
    const node = $(sel);
    if (node) node.inert = true;
  }
  document.body.style.overflow = 'hidden';
  shop.focus();
}

function closeShop() {
  shopStore = null;
  const url = new URL(location.href);
  url.searchParams.delete('tienda');
  history.replaceState(null, '', url);
  $('#shop').hidden = true;
  for (const sel of ['header.masthead', 'main', 'footer']) {
    const node = $(sel);
    if (node) node.inert = false;
  }
  document.body.style.overflow = '';
  $('#q').focus();
}

function shopText() {
  const cart = state.carts.find((c) => c.store === shopStore);
  if (!cart) return '';
  const order = state.sections.map((s) => s.key);
  const lines = [...cart.lines].sort((a, b) => order.indexOf(a.section) - order.indexOf(b.section));
  const body = lines.map((l) => `- ${l.quantity > 1 ? l.quantity + ' × ' : ''}${l.name} — ${money(l.priceCents * l.quantity)}`);
  const out = [`Lista para ${cart.label}${cart.count ? ' — ' + money(cart.totalCents) : ''}`, ...body];
  if (cart.missing.length) out.push(`Falta: ${cart.missing.join(', ')}`);
  return out.join('\n');
}

async function copyShop() {
  const text = shopText();
  const button = $('#shop-copy');
  try {
    await navigator.clipboard.writeText(text);
  } catch {
    const area = el('textarea', { value: text });
    document.body.append(area);
    area.select();
    document.execCommand('copy');
    area.remove();
  }
  button.textContent = 'Copiada';
  setTimeout(() => { button.textContent = 'Copiar'; }, 1600);
}

// ---------- eventos ----------

document.addEventListener('click', (event) => {
  const shopButton = event.target.closest('button[data-shop]');
  if (shopButton) {
    openShop(shopButton.dataset.shop);
    return;
  }
  const action = event.target.closest('button[data-pick],button[data-anchor]');
  if (action) {
    const item = action.closest('.item');
    const [store, id] = (action.dataset.pick || action.dataset.anchor).split('|');
    if (action.dataset.pick) {
      act(api('POST', `/api/items/${item.dataset.id}/pick`, { store, id }), 'Producto fijado en esa cadena.');
    } else {
      act(api('POST', `/api/items/${item.dataset.id}/anchor`, { store, id }), 'Base cambiada; equivalencias recalculadas sin gastar cuota.');
    }
    return;
  }
  const step = event.target.closest('.step');
  if (step) {
    const item = step.closest('.item');
    const current = Number($('.qty-n', item).textContent) || 1;
    const next = Math.min(99, Math.max(1, current + Number(step.dataset.step)));
    if (next !== current) {
      act(api('PATCH', `/api/items/${item.dataset.id}`, { quantity: next }), '');
    }
    return;
  }
  const remove = event.target.closest('.remove');
  if (remove) {
    const item = remove.closest('.item');
    act(api('DELETE', `/api/items/${item.dataset.id}`), 'Producto quitado.');
  }
});

document.addEventListener('change', (event) => {
  const select = event.target.closest('.section');
  if (!select) return;
  const item = select.closest('.item');
  act(api('PATCH', `/api/items/${item.dataset.id}`, { section: select.value }), '');
});

$('#add').addEventListener('submit', async (event) => {
  event.preventDefault();
  const term = $('#q').value.trim();
  if (!term) return;
  await search(term);
});

$('#picker-cancel').addEventListener('click', closePicker);
$('#recompute').addEventListener('click', () =>
  act(api('POST', '/api/recompute'), 'Equivalencias recalculadas con las reglas actuales, sin preguntar a las cadenas.'));
$('#retry').addEventListener('click', () =>
  act(api('POST', '/api/retry'), 'Reintentando las cadenas que no respondieron…'));
$('#shop-close').addEventListener('click', closeShop);
$('#shop-copy').addEventListener('click', copyShop);
$('#shop-print').addEventListener('click', () => window.print());

document.addEventListener('keydown', (event) => {
  if (event.key !== 'Escape') return;
  if (shopStore) closeShop();
  else if (!$('#picker').hidden) closePicker();
});

async function boot() {
  await loadLogos();
  await refresh();
  const deepSearch = new URL(location.href).searchParams.get('buscar');
  if (deepSearch) {
    $('#q').value = deepSearch;
    await search(deepSearch);
  }
}

boot();
