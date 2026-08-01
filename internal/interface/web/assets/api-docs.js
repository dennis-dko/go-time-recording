'use strict';

/**
 * Renders /openapi.json as a readable reference.
 *
 * Written by hand rather than vendoring Swagger UI: the Content-Security-Policy
 * forbids external scripts, and shipping a megabyte of third-party assets would
 * work against the single-binary goal. The specification itself is standard
 * OpenAPI, so anyone preferring Swagger UI can load /openapi.json into it.
 */

const $ = (sel, root = document) => root.querySelector(sel);

/** Builds an element; text always goes through textContent. */
function el(tag, props = {}, ...children) {
  const node = document.createElement(tag);
  for (const [key, value] of Object.entries(props)) {
    if (key === 'class') node.className = value;
    else if (key === 'text') node.textContent = value;
    else if (value !== null && value !== undefined) node.setAttribute(key, value);
  }
  for (const child of children) {
    if (child !== null && child !== undefined) node.append(child);
  }
  return node;
}

const METHODS = ['get', 'post', 'put', 'patch', 'delete'];

async function render() {
  const spec = await (await fetch('/openapi.json')).json();

  document.title = `${spec.info.title} — API`;
  $('#doc-title').textContent = spec.info.title;
  $('#doc-description').textContent = spec.info.description ?? '';

  const body = $('#doc-body');
  const base = spec.servers?.[0]?.url ?? '';

  // Group the operations by tag so related endpoints stay together.
  const byTag = new Map();

  for (const [path, item] of Object.entries(spec.paths)) {
    for (const method of METHODS) {
      const operation = item[method];
      if (!operation) continue;

      const tag = operation.tags?.[0] ?? 'other';
      if (!byTag.has(tag)) byTag.set(tag, []);

      // Path-level parameters apply to every operation underneath.
      const parameters = [...(item.parameters ?? []), ...(operation.parameters ?? [])];
      byTag.get(tag).push({ path, method, operation, parameters });
    }
  }

  // Follow the declared tag order, then anything left over.
  const order = [...(spec.tags ?? []).map((t) => t.name), ...byTag.keys()];
  const seen = new Set();

  for (const tag of order) {
    if (seen.has(tag) || !byTag.has(tag)) continue;
    seen.add(tag);

    const meta = (spec.tags ?? []).find((t) => t.name === tag);
    const card = el('div', { class: 'card' }, el('h2', { text: tag }));

    if (meta?.description) card.append(el('p', { class: 'muted', text: meta.description }));

    for (const entry of byTag.get(tag)) card.append(operationBlock(base, entry));

    body.append(card);
  }

  body.append(securityCard(spec));
}

function operationBlock(base, { path, method, operation, parameters }) {
  const block = el('div', { class: 'op' });

  block.append(el('div', { class: 'op-head' },
    el('span', { class: `method method-${method}`, text: method.toUpperCase() }),
    el('code', { class: 'op-path', text: base + path }),
    el('span', { class: 'op-summary', text: operation.summary ?? '' }),
  ));

  if (operation.description) {
    block.append(el('p', { class: 'muted', text: operation.description }));
  }

  // An operation with `security: []` overrides the global requirement, which
  // is how the public endpoints are marked.
  if (Array.isArray(operation.security) && operation.security.length === 0) {
    block.append(el('p', { class: 'muted plus', text: 'No authentication required.' }));
  }

  if (parameters.length) {
    block.append(el('h3', { text: 'Parameters' }));
    block.append(table(
      ['Name', 'In', 'Required', 'Type', 'Description'],
      parameters.map((p) => [
        p.name,
        p.in,
        p.required ? 'yes' : 'no',
        p.schema?.type ?? '',
        p.description ?? '',
      ]),
    ));
  }

  const schema = operation.requestBody?.content?.['application/json']?.schema;
  if (schema) {
    block.append(el('h3', { text: 'Request body' }));
    block.append(bodyTable(schema));
  }

  block.append(el('h3', { text: 'Responses' }));
  block.append(table(
    ['Status', 'Description'],
    Object.entries(operation.responses ?? {}).map(([code, r]) => [code, r.description ?? '']),
  ));

  return block;
}

/** Renders a request body, resolving a single local $ref. */
function bodyTable(schema) {
  const resolved = schema.$ref ? refName(schema.$ref) : null;

  if (resolved) {
    return el('p', { class: 'muted', text: `See schema: ${resolved}` });
  }

  const required = new Set(schema.required ?? []);

  return table(
    ['Field', 'Type', 'Required', 'Description'],
    Object.entries(schema.properties ?? {}).map(([name, prop]) => [
      name,
      prop.type ?? '',
      required.has(name) ? 'yes' : 'no',
      prop.description ?? '',
    ]),
  );
}

const refName = (ref) => ref.split('/').pop();

function table(headers, rows) {
  const wrap = el('div', { class: 'table-wrap' });
  const head = el('tr', {}, ...headers.map((h) => el('th', { text: h })));
  const bodyRows = rows.length
    ? rows.map((cells) => el('tr', {}, ...cells.map((c) => el('td', { text: String(c) }))))
    : [el('tr', {}, el('td', { class: 'empty', colspan: headers.length, text: 'none' }))];

  wrap.append(el('table', {}, el('thead', {}, head), el('tbody', {}, ...bodyRows)));

  return wrap;
}

function securityCard(spec) {
  const card = el('div', { class: 'card' }, el('h2', { text: 'Authentication' }));

  const schemes = spec.components?.securitySchemes ?? {};

  card.append(table(
    ['Scheme', 'Type', 'Sent as', 'Description'],
    Object.entries(schemes).map(([name, s]) => [
      name,
      s.type,
      s.scheme === 'bearer' ? 'Authorization: Bearer <token>' : `${s.in}: ${s.name ?? ''}`,
      s.description ?? '',
    ]),
  ));

  card.append(el('h3', { text: 'Example' }));
  card.append(el('pre', { class: 'secret', text:
    'curl -H "Authorization: Bearer gtr_xxxxx" \\\n'
    + '     https://your-host/api/v1/timesheets' }));

  return card;
}

document.addEventListener('DOMContentLoaded', () => {
  render().catch((err) => {
    $('#doc-body').append(el('div', { class: 'card' },
      el('p', { class: 'minus', text: `Could not load the specification: ${err.message}` })));
  });
});
