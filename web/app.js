// The interface is a document: the list of what you need, and under each row the price of
// its equivalent in the seven chains. Everything is built with DOM nodes and textContent —
// product names are third-party strings and never become HTML.
'use strict';

const CODE = {
  quota: 'sin cuota', blocked: 'bloqueada', timeout: 'sin respuesta',
  network: 'sin red', parse: 'ilegible', unknown: 'sin respuesta'
};
const LEVEL = { exacta: 'exact', equivalente: 'same', probable: 'maybe', elegida: 'picked' };
const LEVEL_NOTE = {
  exacta: 'mismo código de barras', equivalente: 'mismo producto y tamaño',
  probable: 'no es lo mismo', elegida: 'elegida por ti'
};

const $ = (sel, root = document) => root.querySelector(sel);

function el(tag, props = {}, ...kids) {
  const node = document.createElement(tag);
  for (const [k, v] of Object.entries(props)) {
    if (k === 'class') node.className = v;
    else if (k === 'text') node.textContent = v;
    else if (k.startsWith('data-')) node.setAttribute(k, v);
    else node[k] = v;
  }
  for (const kid of kids) if (kid != null) node.append(kid);
  return node;
}

const money = (cents) => cents == null
  ? '—'
  : (cents / 100).toLocaleString('es-ES', { minimumFractionDigits: 2, maximumFractionDigits: 2 }) + ' €';

const label = (store) => (state.stores.find((s) => s.store === store) || {}).label || store;

let state = null;
let timer = null;

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

function apply(doc) {
  state = doc;
  render();
}

function note(text) {
  $('#status').textContent = text || '';
}

async function act(promise, message) {
  try {
    apply(await promise);
    note(message || '');
  } catch (err) {
    note(err.message);
  }
}

function render() {
  const pending = state.items.some((i) => i.resolving);
  const engine = $('#engine');
  engine.classList.toggle('bad', !state.reachable);
  engine.textContent = !state.reachable
    ? 'motor de catálogo sin conexión'
    : pending ? 'buscando equivalentes…' : `${state.items.length} producto${state.items.length === 1 ? '' : 's'}`;

  renderList();
  renderCarts();
  renderRetry();
  schedule(pending);
}

function schedule(pending) {
  clearTimeout(timer);
  if (pending) timer = setTimeout(refresh, 2500);
}

async function refresh() {
  try {
    apply(await api('GET', '/api/state'));
  } catch (err) {
    $('#engine').classList.add('bad');
    $('#engine').textContent = 'motor de catálogo sin conexión';
  }
}

function renderList() {
  const list = $('#list');
  list.replaceChildren();
  $('#empty').hidden = state.items.length > 0;
  $('#list-meta').textContent = state.items.length
    ? `${state.items.length} producto${state.items.length === 1 ? '' : 's'} · ${state.carts.filter((c) => c.count > 0).length} cadenas con cesta`
    : 'Vacía';

  const tpl = $('#item-tpl');
  for (const item of state.items) {
    const node = tpl.content.firstElementChild.cloneNode(true);
    node.dataset.id = item.id;
    $('.q', node).textContent = item.query;

    const anchor = $('.anchor-line', node);
    if (item.anchor) {
      anchor.append(
        document.createTextNode('Base: '),
        el('b', { text: item.anchor.name }),
        document.createTextNode(` · ${label(item.anchor.store)} · ${money(item.anchor.priceCents)}`)
      );
    } else {
      anchor.textContent = item.resolving ? 'Buscando la base en las cadenas…' : 'Sin base todavía';
    }

    if (item.error) {
      const line = $('.item-note', node);
      line.hidden = false;
      line.textContent = item.error;
    }
    $('.remove', node).addEventListener('click', () =>
      act(api('DELETE', `/api/items/${item.id}`), 'Producto quitado.'));

    const cells = $('.cells', node);
    for (const cell of item.cells) cells.append(renderCell(item, cell));
    list.append(node);
  }
}

function renderCell(item, cell) {
  const level = cell.level;
  const node = el('div', { class: `cell ${LEVEL[level] || level}` });
  node.append(el('span', { class: 'chain', text: label(cell.store) }));

  if (level === 'pending') {
    node.append(el('span', { class: 'note', text: 'buscando…' }));
    return node;
  }
  if (level === 'unavailable') {
    node.append(el('span', { class: 'note', text: CODE[cell.code] || 'no disponible' }));
    return node;
  }
  if (level === 'no' || !cell.product) {
    node.append(el('span', { class: 'note', text: 'no encontrado' }));
  } else {
    const p = cell.product;
    node.append(
      el('span', { class: 'price', text: money(p.priceCents) }),
      el('span', { class: 'pname', text: p.name }),
      el('span', { class: `badge ${LEVEL[level]}`, text: level })
    );
    if (cell.reason) node.append(el('span', { class: 'note', text: cell.reason }));
    const isBase = item.anchor && item.anchor.store === cell.store && item.anchor.id === p.id;
    if (!isBase) {
      node.append(el('button', {
        class: 'alt', type: 'button', text: 'usar como base',
        'data-anchor': `${cell.store}|${p.id}`
      }));
    }
  }

  if (cell.alts && cell.alts.length) {
    const alts = el('div', { class: 'alts' });
    for (const alt of cell.alts) {
      alts.append(el('button', {
        class: 'alt', type: 'button',
        'data-pick': `${cell.store}|${alt.product.id}`,
        title: alt.reason || ''
      }, el('span', { text: alt.product.name }), el('b', { text: money(alt.product.priceCents) })));
    }
    node.append(alts);
  }
  return node;
}

function renderCarts() {
  const wrap = $('#carts');
  wrap.replaceChildren();
  for (const cart of state.carts) {
    const card = el('article', { class: 'cart' });
    card.append(
      el('h3', { text: cart.label }),
      el('p', { class: cart.count ? 'total' : 'total off', text: cart.count ? money(cart.totalCents) : '—' }),
      el('p', { class: 'count', text: `${cart.count} de ${cart.items} productos` })
    );
    const details = el('details');
    details.append(el('summary', { text: 'lista para la tienda' }));
    const ul = el('ul');
    for (const line of cart.lines) {
      ul.append(el('li', {}, el('span', { text: line.name }), el('span', { text: money(line.priceCents) })));
    }
    for (const miss of cart.missing) {
      ul.append(el('li', {}, el('span', { class: 'miss', text: miss }), el('span', { class: 'miss', text: 'falta' })));
    }
    details.append(ul);
    card.append(details);
    wrap.append(card);
  }
}

function renderRetry() {
  const retry = $('#retry');
  const stuck = state.carts.some((c) => c.retry) || state.items.some((i) => i.error);
  retry.hidden = !stuck;
}

// One delegated listener for every row action, so a re-render never loses a handler.
document.addEventListener('click', (event) => {
  const button = event.target.closest('button[data-pick],button[data-anchor]');
  if (!button) return;
  const item = button.closest('.item');
  if (!item) return;
  const [store, id] = (button.dataset.pick || button.dataset.anchor).split('|');
  if (button.dataset.pick) {
    act(api('POST', `/api/items/${item.dataset.id}/pick`, { store, id }), 'Producto fijado en su cadena.');
  } else {
    act(api('POST', `/api/items/${item.dataset.id}/anchor`, { store, id }), 'Base cambiada; equivalencias recalculadas.');
  }
});

$('#add').addEventListener('submit', async (event) => {
  event.preventDefault();
  const input = $('#q');
  const query = input.value.trim();
  if (!query) return;
  input.value = '';
  await act(api('POST', '/api/items', { query }), 'Añadido. Buscando su equivalente en las cadenas…');
  input.focus();
});

$('#retry').addEventListener('click', () =>
  act(api('POST', '/api/retry'), 'Reintentando las cadenas que no respondieron…'));

refresh();
