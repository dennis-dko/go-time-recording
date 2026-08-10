'use strict';

/**
 * Single-page UI for the time recording API.
 *
 * Plain DOM APIs on purpose: the assets are embedded in the Go binary, so
 * keeping the UI build-step-free means `go build` is the only build there is.
 *
 * Permission checks here only decide what to *show*. Every rule is enforced
 * again by the server, so hiding a control is convenience, never security.
 */

const API = '/api/v1';

/** Cached lookups so tables can show names instead of raw ids. */
const cache = { users: [], projects: [], roles: [], permissions: [] };

/** The signed-in user, their permissions, and whether auth is on at all. */
let me = { user: null, permissions: [], authEnabled: false };

// ---------------------------------------------------------------- transport

/**
 * Reads a cookie the server left for this page.
 *
 * Only non-HttpOnly cookies are visible here, which is exactly the CSRF token
 * and never the session token.
 */
function readCookie(name) {
  const prefix = `${name}=`;

  for (const part of document.cookie.split(';')) {
    const entry = part.trim();
    if (entry.startsWith(prefix)) return decodeURIComponent(entry.slice(prefix.length));
  }

  return '';
}

/** Requests that cannot change anything need no token. */
const SAFE_METHODS = new Set(['GET', 'HEAD', 'OPTIONS']);

/**
 * How long a read may hang before this side gives up on it.
 *
 * A request that never comes back holds the in-flight counter above zero for as
 * long as the connection lives, and nothing recovers from that on its own: the
 * loading strip stays up, and every screen that was waiting on the answer waits
 * for ever. The browser does eventually drop a dead connection, but "eventually"
 * is minutes of a page that looks broken.
 *
 * Generous on purpose. This is meant to catch a server that has stopped
 * answering, not a query that is merely slow - the slowest real read here is a
 * long period of entries, which is seconds.
 */
const READ_TIMEOUT_MS = 60000;

/**
 * Calls the API and unwraps GoFr's {data, error} envelope.
 *
 * State-changing calls echo the CSRF cookie back in a header. Another site can
 * make the browser send the session cookie, but it can neither read this cookie
 * nor set a custom header, so the echo is what proves the call came from here.
 *
 * @throws {Error} with the server-provided message on a non-2xx response.
 */
/**
 * The loading strip, driven by how many requests are in flight.
 *
 * Counted rather than toggled: three requests starting together and finishing
 * apart would otherwise switch it off while two were still running.
 *
 * Nothing appears for a request that finishes quickly. A strip that flashes for
 * 40ms is noise, and the screen it flashes over is already answering - so it
 * waits, and only says something when there is something to wait for.
 */
const progress = { inFlight: 0, showTimer: null, hideTimer: null };

/** How long a request may take before it is worth mentioning. */
const PROGRESS_DELAY_MS = 140;

/**
 * How long the strip takes to run to the end and fade once nothing is waiting.
 *
 * Named rather than written into the one setTimeout that uses it, because a test
 * has to wait longer than this to see the strip actually go - and a test carrying
 * its own copy of the number starts blaming the wrong thing the day this changes.
 * It matches the transition under .progress.done span in app.css.
 */
const PROGRESS_FADE_MS = 460;

function progressStart() {
  progress.inFlight += 1;

  if (progress.inFlight > 1 || progress.showTimer) return;

  clearTimeout(progress.hideTimer);

  progress.showTimer = setTimeout(() => {
    progress.showTimer = null;

    const bar = $('#progress');
    if (!bar) return;

    // Removed and re-added so the creep animation restarts rather than
    // continuing from where the last request left it.
    bar.classList.remove('done');
    bar.hidden = false;
    void bar.offsetWidth;
  }, PROGRESS_DELAY_MS);
}

function progressDone() {
  progress.inFlight = Math.max(0, progress.inFlight - 1);

  if (progress.inFlight > 0) return;

  // Nothing is waiting any more, so a pending "show it" is dropped: this is the
  // common case, a request that finished before it was worth mentioning.
  if (progress.showTimer) {
    clearTimeout(progress.showTimer);
    progress.showTimer = null;
  }

  // Deliberately not a `return` above. The strip could still be on screen from an
  // earlier request, and then dropping the show timer is only half the job:
  //
  //   a slow request is shown, finishes, and starts fading out;
  //   inside that fade another request starts, which cancels it;
  //   that one finishes quickly, before it was itself worth showing.
  //
  // The fade had been cancelled and nothing re-armed it, so the strip sat there
  // with hidden still false - invisible, because the inner span had already faded,
  // but present, and the counter out of step with the screen. It stayed that way
  // until some later request outlived the delay and put the strip up again, which
  // on a quiet screen is a long time. A browser test caught it as "showing with no
  // request in flight", which is exactly what it was.
  const bar = $('#progress');
  if (!bar || bar.hidden) return;

  bar.classList.add('done');

  // Belt and braces. Under the only traffic there is - api(), which pairs these two
  // - progressStart has already cleared this on the way up from zero, so there is
  // nothing live to cancel here. Nothing enforces that pairing though, and a stray
  // timer would hide a strip that a later request has just put up.
  clearTimeout(progress.hideTimer);

  progress.hideTimer = setTimeout(() => {
    // Only if nothing started again while it was fading out.
    if (progress.inFlight === 0) bar.hidden = true;
  }, PROGRESS_FADE_MS);
}

async function api(path, options = {}) {
  const method = (options.method ?? 'GET').toUpperCase();
  const headers = { 'Content-Type': 'application/json', ...(options.headers ?? {}) };

  if (!SAFE_METHODS.has(method)) {
    headers['X-CSRF-Token'] = readCookie('gtr_csrf');
  }

  // Reads only, and this is the whole reason the distinction is drawn here.
  //
  // Aborting in the browser does not stop the server: it finishes the work either
  // way. Giving up on a write would therefore report a failure for something that
  // succeeded - and somebody would do it again, which for an import means writing
  // every row twice. A read costs nothing but a second attempt.
  //
  // A caller that brought its own signal keeps it: it has a reason of its own for
  // wanting to cancel, and two things aborting one request cannot both be right.
  const giveUp = SAFE_METHODS.has(method) && !options.signal ? new AbortController() : null;
  const countdown = giveUp ? setTimeout(() => giveUp.abort(), READ_TIMEOUT_MS) : null;

  progressStart();

  let res;

  try {
    res = await fetch(API + path, {
      ...options,
      headers,
      signal: giveUp ? giveUp.signal : options.signal,
      // Without this a cross-origin deployment would drop the cookies entirely.
      credentials: 'same-origin',
    });
  } catch (err) {
    // Only our own abort is reported as a timeout. A caller's signal firing is
    // their business, and "the server did not answer" would be a lie about it.
    if (giveUp?.signal.aborted) {
      throw new Error(t('msg.tooSlow',
        'The server did not answer in time. Please try again.'));
    }

    throw err;
  } finally {
    // Cleared whatever happened, so a fast request does not leave a timer holding
    // a controller it has no further use for.
    clearTimeout(countdown);

    // In a finally, so a refused connection does not leave the strip running for
    // as long as the page is open.
    progressDone();
  }

  let body = null;
  try {
    body = await res.json();
  } catch {
    // A 204 or an error page has no JSON body; fall through to the status.
  }

  if (!res.ok) {
    const err = new Error(errorMessage(body) || `${t('msg.error', 'Error')} ${res.status}`);

    // A 503 from maintenance mode is not a failure of this request, it is the
    // state of the installation. Showing the banner here means every screen
    // reports it the same way rather than each one growing its own handling.
    if (res.status === 503 && body?.error?.maintenance) {
      const banner = $('#maintenance-banner');
      if (banner) {
        banner.textContent = err.message;
        banner.hidden = false;
      }
    }
    // The status alongside the message, so a caller can tell "you are not
    // signed in" from "that did not work" without parsing prose. The log
    // viewer's poller needs exactly that distinction: on a 401 it has to stop
    // rather than keep asking every few seconds forever.
    err.status = res.status;
    throw err;
  }

  return body ? body.data : null;
}

/** Pulls a readable message out of GoFr's error shape. */
/**
 * The message to show for a failed request, in the reader's language where the
 * server said which rule was broken.
 *
 * The server writes its refusals in English at the point the rule is enforced,
 * which is right for the log and wrong for the person who tripped over it. Errors
 * that name themselves carry a code and the values the sentence interpolated, so
 * the sentence is looked up here and the values put back in German word order.
 * Anything without a code falls back to the server's own wording, which is what
 * every error did before.
 */
function errorMessage(body) {
  const err = body && body.error;
  if (!err) return '';
  if (typeof err === 'string') return err;

  if (err.code) {
    const translated = t(`err.${err.code}`, err.message ?? '');
    if (translated) return fillIn(translated, err.values);
  }

  // Before the message, which for these is GoFr's "'1' invalid parameter(s):
  // dailyTargetHours" - a count nobody asked for and a column name rather than the
  // label above the field.
  if (Array.isArray(err.param) && err.param.length) {
    const named = err.param.map((field) => t(`field.${field}`, field));

    return `${t('msg.invalidFields', 'Invalid field(s)')}: ${named.join(', ')}`;
  }

  if (err.message) return err.message;

  return JSON.stringify(err);
}

/**
 * Puts {0}, {1} ... back into a translated sentence.
 *
 * A fractional number is shown to two places, which is how hours are written
 * everywhere else on screen - a refusal saying 6.5 next to a table saying 6.50
 * would read as two different figures.
 */
function fillIn(text, values) {
  if (!Array.isArray(values) || !values.length) return text;

  return text.replace(/\{(\d+)\}/g, (whole, index) => {
    const value = values[Number(index)];
    if (value === undefined || value === null) return whole;
    if (typeof value !== 'number') return String(value);

    return Number.isInteger(value) ? String(value) : value.toFixed(2);
  });
}

// -------------------------------------------------------------------- utils

const $ = (sel, root = document) => root.querySelector(sel);
const $$ = (sel, root = document) => [...root.querySelectorAll(sel)];

/** Whether the signed-in user holds at least one of the given permissions. */
function can(...permissions) {
  if (!me.authEnabled) return true;
  return permissions.some((p) => me.permissions.includes(p));
}

function toast(message, kind = 'ok') {
  const stack = $('#toast');
  if (!stack) return;

  const note = el('div', { class: `toast-note ${kind}` });

  // Errors are announced assertively and successes politely: an error is worth
  // interrupting whatever a screen reader was saying, and "Saved" is not.
  note.setAttribute('role', kind === 'error' ? 'alert' : 'status');

  note.append(el('span', { class: 'toast-text', text: message }));

  const dismiss = el('button', {
    class: 'toast-close',
    type: 'button',
    'aria-label': t('action.dismiss', 'Dismiss'),
    text: '×',
    onclick: () => note.remove(),
  });

  note.append(dismiss);
  stack.append(note);

  // Stacked rather than replaced. A single slot meant two failures in a row
  // showed only the second, which is the case where the first one mattered.
  // Bounded, because a loop of failures should not fill the screen.
  while (stack.children.length > TOAST_LIMIT) stack.firstElementChild.remove();

  // Long enough to read what is there. A fixed five seconds is fine for "Saved"
  // and not for a sentence explaining why a directory refused a bind.
  const linger = Math.min(TOAST_MAX_MS, TOAST_MIN_MS + message.length * TOAST_MS_PER_CHAR);

  setTimeout(() => note.remove(), linger);
}

/** How many notices may be on screen, and how long each one stays. */
const TOAST_LIMIT = 4;
const TOAST_MIN_MS = 4000;
const TOAST_MAX_MS = 20000;
const TOAST_MS_PER_CHAR = 60;

/** Builds an element; text is assigned via textContent, never innerHTML. */
function el(tag, props = {}, ...children) {
  const node = document.createElement(tag);
  for (const [key, value] of Object.entries(props)) {
    if (key === 'class') node.className = value;
    else if (key === 'text') node.textContent = value;
    else if (key === 'checked') node.checked = value;
    else if (key.startsWith('on')) node.addEventListener(key.slice(2), value);
    else if (value !== null && value !== undefined) node.setAttribute(key, value);
  }
  for (const child of children) {
    if (child !== null && child !== undefined) node.append(child);
  }
  return node;
}

function statusBadge(status) {
  // The class keeps the raw status so the colour rules still match; only the
  // label is translated.
  return el('span', { class: `status status-${status}`, text: t(`status.${status}`, status) });
}

/**
 * A date in the reader's own language.
 *
 * Was hard-coded to d.m.Y, which is right in German and wrong everywhere else -
 * an English reader saw 03.08.2026 and had to work out which number was the
 * month. Intl knows every locale's order, and the date is built in UTC so a zone
 * behind the line cannot shift it to the previous day: the string is a calendar
 * day, not a moment.
 */
/** The built-in mark: a stopwatch, drawn rather than fetched. */
const DEFAULT_FAVICON =
  "data:image/svg+xml,<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 16 16'>"
  + "<text y='14' font-size='14'>&#9201;</text></svg>";

/**
 * Points the browser tab at the instance's logo.
 *
 * The logo is stored as a data URI, so it needs no request and cannot be blocked
 * by the Content-Security-Policy - which allows data: images for exactly this
 * reason. Anything else is refused rather than set: a logo that is a link to
 * somewhere else would make the tab icon a request to that somewhere.
 */
function applyFavicon(logo) {
  const link = document.querySelector('link[rel="icon"]');
  if (!link) return;

  const usable = typeof logo === 'string' && logo.startsWith('data:image/');

  link.href = usable ? logo : DEFAULT_FAVICON;
}

function fmtDate(iso) {
  if (!iso) return '–';

  const [y, m, d] = iso.split('-').map(Number);
  const at = new Date(Date.UTC(y, m - 1, d));

  if (Number.isNaN(at.getTime())) return iso;

  return new Intl.DateTimeFormat(activeLanguage(), {
    day: '2-digit', month: '2-digit', year: 'numeric', timeZone: 'UTC',
  }).format(at);
}

const fmtHours = (n) => `${n.toFixed(2)} h`;

/** Renders a signed balance, coloured as a credit or a debt. */
function balanceCell(value) {
  const sign = value > 0 ? '+' : '';
  return el('td', {
    class: `num ${value > 0 ? 'plus' : value < 0 ? 'minus' : ''}`,
    text: `${sign}${value.toFixed(2)} h`,
  });
}

const projectName = (id) => cache.projects.find((p) => p.id === id)?.name ?? `#${id}`;

/** Replaces a tbody with rows, or a single "empty" row when there are none. */
function fillTable(tbody, rows, columnCount, emptyText) {
  // Before the rows go in, while they can still be read for delete buttons.
  const picking = prepareBulkDelete(tbody, rows);

  tbody.replaceChildren();

  if (rows.length === 0) {
    tbody.append(el('tr', {},
      el('td', { class: 'empty', colspan: columnCount + (picking ? 1 : 0), text: emptyText })));
  } else {
    tbody.append(...rows);
  }

  // Only now, with the new rows in the document. The bar reads how many checkboxes
  // are ticked out of the table, so refreshing it before this line counted the rows
  // of the previous render: delete two of three, and the bar stayed up saying
  // "3 selected" over a table where nothing was ticked at all.
  if (picking) refreshBulkBar(tbody.closest('table'));
}

/**
 * The bar belonging to one table.
 *
 * Found by the table's own id rather than by position, so a card that grows a
 * second table later cannot end up driving the wrong bar.
 */
function bulkBarOf(table) {
  const card = table.parentElement && table.parentElement.parentElement;

  return card ? card.querySelector(`.bulk-bar[data-for="${table.id}"]`) : null;
}

/**
 * Gives a table a leading column of checkboxes, when any of its rows offers
 * deletion, plus the bar that acts on them.
 *
 * Driven entirely by what the rows contain, so there is no list anywhere of which
 * tables have this. A table of rows nobody may delete - a report, an overtime
 * balance, the preview of a file about to be imported - is left exactly as it was,
 * and a table that grows a delete button later gets this without being told. The
 * calendar day gets it, without being mentioned anywhere, because its rows are time
 * entries and carry the same delete button the list does.
 *
 * Returns whether the column is there, because the empty row's colspan has to
 * know.
 */
function prepareBulkDelete(tbody, rows) {
  const table = tbody.closest('table');
  if (!table) return false;

  // Which rows carry a deletion, and what it is. Every .danger button is
  // considered, not just the first: a row may offer more than one dangerous
  // thing, and only one of them is the deletion.
  const deletions = new Map();
  for (const row of rows) {
    if (!row.querySelectorAll) continue;

    for (const button of row.querySelectorAll('button.danger')) {
      if (button.deletes) {
        deletions.set(row, button.deletes);
        break;
      }
    }
  }

  const head = table.tHead && table.tHead.rows[0];
  const column = head && head.querySelector('th.pick');

  if (deletions.size === 0) {
    // Nothing deletable this time round - a filter that matched only other
    // people's entries, say. The column goes rather than standing there with
    // nothing in it.
    if (column) column.remove();

    const bar = bulkBarOf(table);
    if (bar) bar.remove();

    return false;
  }

  for (const row of rows) {
    const deletes = deletions.get(row);

    // A row nobody may delete still gets the cell, so the columns line up. It
    // simply has nothing in it to tick.
    const cell = el('td', { class: 'pick' });

    if (deletes) {
      const box = el('input', {
        type: 'checkbox',
        class: 'row-pick',
        'aria-label': `${t('bulk.pick', 'Select')}: ${deletes.label}`,
        onchange: () => refreshBulkBar(table),
      });

      box.deletes = deletes;
      cell.append(box);
    }

    row.prepend(cell);
  }

  if (!column && head) {
    head.prepend(el('th', { class: 'pick' }, el('input', {
      type: 'checkbox',
      class: 'pick-all',
      'aria-label': t('bulk.pickAll', 'Select all'),
      onchange: (event) => {
        for (const box of table.querySelectorAll('tbody .row-pick')) {
          box.checked = event.target.checked;
        }

        refreshBulkBar(table);
      },
    })));
  }

  ensureBulkBar(table);

  // The bar is left to fillTable to refresh, once the rows are actually in the
  // document. The rows are new, so nothing is ticked and the bar hides itself:
  // carrying a selection across a reload would be worse than losing it, because
  // the rows underneath it need not be the same rows.
  return true;
}

/** The bar that says how many rows are ticked and offers to delete them. */
function ensureBulkBar(table) {
  const existing = bulkBarOf(table);
  if (existing) return existing;

  const wrap = table.parentElement;
  if (!wrap || !wrap.parentElement) return null;

  const bar = el('div', { class: 'bulk-bar', 'data-for': table.id },
    el('span', { class: 'bulk-count' }),
    el('button', {
      class: 'danger',
      text: t('bulk.delete', 'Delete selected'),
      onclick: () => runBulkDelete(table),
    }));

  // Above the table rather than below it: in a long table the bar would be off
  // the screen, and the rows being ticked start at the top.
  wrap.parentElement.insertBefore(bar, wrap);

  return bar;
}

function refreshBulkBar(table) {
  const bar = bulkBarOf(table);
  if (!bar) return;

  const boxes = [...table.querySelectorAll('tbody .row-pick')];
  const picked = boxes.filter((box) => box.checked);

  bar.classList.toggle('shown', picked.length > 0);
  bar.querySelector('.bulk-count').textContent = t('bulk.count', '{n} selected')
    .replace('{n}', String(picked.length));

  const all = table.querySelector('thead .pick-all');
  if (all) {
    all.checked = boxes.length > 0 && picked.length === boxes.length;

    // Some but not all: neither ticked nor empty is the truth, and the browser
    // draws that third state itself.
    all.indeterminate = picked.length > 0 && picked.length < boxes.length;
  }
}

/**
 * Deletes every ticked row, one after another.
 *
 * Sequentially, and through the ordinary single-row endpoint. There is no bulk
 * endpoint on purpose: each row goes through the same rules, ownership check and
 * audit entry as a single deletion, so a batch can never delete something the
 * reader could not have deleted on its own. Firing them all at once would race
 * for the same rows and, on SQLite, mostly queue behind each other anyway.
 *
 * A refusal partway does not stop the rest. Eight of ten deleted, with the two
 * refusals named, is more use than eight silently undone.
 */
async function runBulkDelete(table) {
  const picked = [...table.querySelectorAll('tbody .row-pick')]
    .filter((box) => box.checked)
    .map((box) => box.deletes);

  if (picked.length === 0) return;

  const shown = picked.slice(0, 5).map((item) => item.label);
  if (picked.length > shown.length) shown.push('…');

  const ok = await confirmDialog({
    title: t('bulk.title', 'Delete the selected rows?'),
    text: t('bulk.text', 'This cannot be undone.'),
    detail: shown.join(', '),
    confirmLabel: t('bulk.delete', 'Delete selected'),
  });

  if (!ok) return;

  const refused = [];
  let done = 0;

  for (const item of picked) {
    try {
      await api(item.path, { method: 'DELETE' });
      done += 1;
    } catch (err) {
      refused.push(`${item.label}: ${err.message}`);
    }
  }

  if (refused.length > 0) {
    toast(`${t('bulk.refused', '{n} could not be deleted').replace('{n}', String(refused.length))}`
      + ` – ${refused[0]}`, 'error');
  } else {
    toast(t('bulk.done', '{n} deleted').replace('{n}', String(done)), 'ok');
  }

  // One reload for the batch, along the row's own reload path: whatever a single
  // deletion refreshes is what a batch of them refreshes.
  if (picked[0].after) await picked[0].after();
}

/**
 * The "delete" button of a single row - and with it the row's ticket to being
 * deleted in a batch.
 *
 * Every table built this button at its own call site, which was fine while
 * deletion was one row at a time. Selecting several needs to know which rows may
 * be deleted at all, and that question is already answered precisely here: this
 * button exists only where the row builder decided the reader is allowed to
 * delete that row. Deriving the checkbox from the button means the two cannot
 * disagree - no second copy of "is this allowed?", no checkbox offering a
 * deletion the server would refuse.
 *
 * `ask` replaces the confirmation for a row whose single deletion asks something
 * extra; the batch still takes the plain path. `text` is for a table that calls
 * it something else, like revoking a token.
 *
 * The details ride on the node as a property rather than an attribute, so
 * `after` can stay a function.
 */
function deleteButton({ label, path, message, after, ask, text }) {
  const button = el('button', {
    class: 'link danger',
    text: text || t('action.delete', 'delete'),
    onclick: ask || (() => removeAfterConfirm(label, path, message, after)),
  });

  button.deletes = { label, path, message, after };

  return button;
}

/** Fills a <select> with options, preserving the current selection if possible. */
function fillSelect(select, items, { placeholder, labelKey = 'name', valueKey = 'id' } = {}) {
  if (!select) return;
  const previous = select.value;
  select.replaceChildren();
  if (placeholder) select.append(el('option', { value: '', text: placeholder }));
  for (const item of items) {
    select.append(el('option', { value: String(item[valueKey]), text: item[labelKey] }));
  }
  if (previous && [...select.options].some((o) => o.value === previous)) {
    select.value = previous;
  }
}

/**
 * What a role is for, in the reader's language.
 *
 * The description lives in the database, in English, because an administrator writes
 * their own there. The three that ship with the application are translated instead,
 * keyed on the name - a custom role falls through to whatever was typed, which is the
 * only sensible answer for words this application has never seen.
 */
function roleDescription(role) {
  return t(`role.desc.${role.name}`, role.description || '');
}

/**
 * The roles as something to choose from, each with what it is for.
 *
 * A dropdown of bare names asks somebody to guess: "employee-admin" against
 * "employee" is a difference you can only infer, and the difference is whether that
 * person can administer the installation. The list on the Roles screen has always
 * shown the descriptions; the place where the choice is actually made did not.
 */
function roleChoices() {
  return cache.roles.map((role) => {
    const purpose = roleDescription(role);

    return { name: role.name, label: purpose ? `${role.name} — ${purpose}` : role.name };
  });
}

/** Reads a form into a plain object, dropping empty optional fields. */
function formData(form) {
  const out = {};
  for (const [key, raw] of new FormData(form).entries()) {
    const value = typeof raw === 'string' ? raw.trim() : raw;
    if (value !== '') out[key] = value;
  }
  return out;
}

/**
 * Gives every password field a reveal toggle.
 *
 * Done in script rather than markup so any password field added later is
 * covered automatically. Runs once; already-enhanced fields are skipped.
 */
function enhancePasswordFields(root = document) {
  for (const input of $$('input[type="password"]', root)) {
    if (input.parentElement?.classList.contains('pw-wrap')) continue;

    const wrap = el('span', { class: 'pw-wrap' });
    input.replaceWith(wrap);
    wrap.append(input);

    const button = el('button', {
      type: 'button',
      class: 'pw-toggle',
      'aria-label': t('pw.show', 'Show password'),
      'aria-pressed': 'false',
    });

    button.append(eyeIcon(false));

    button.addEventListener('click', () => {
      const revealed = input.type === 'text';
      input.type = revealed ? 'password' : 'text';

      button.setAttribute('aria-pressed', String(!revealed));
      button.setAttribute('aria-label', revealed
        ? t('pw.show', 'Show password')
        : t('pw.hide', 'Hide password'));

      button.replaceChildren(eyeIcon(!revealed));

      // Keep the caret where the user left it; changing `type` moves it to
      // the end in some browsers.
      const caret = input.value.length;
      input.focus();
      input.setSelectionRange(caret, caret);
    });

    wrap.append(button);
  }
}

/** Builds the eye icon, struck through when the password is visible. */
function eyeIcon(revealed) {
  const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
  svg.setAttribute('viewBox', '0 0 24 24');
  svg.setAttribute('fill', 'none');
  svg.setAttribute('stroke', 'currentColor');
  svg.setAttribute('stroke-width', '2');
  svg.setAttribute('stroke-linecap', 'round');
  svg.setAttribute('stroke-linejoin', 'round');
  svg.setAttribute('aria-hidden', 'true');

  const paths = ['M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z', 'M12 9a3 3 0 100 6 3 3 0 000-6z'];
  if (revealed) paths.push('M3 3l18 18');

  for (const d of paths) {
    const path = document.createElementNS('http://www.w3.org/2000/svg', 'path');
    path.setAttribute('d', d);
    svg.append(path);
  }

  return svg;
}

/** Shows or hides every element that declares a data-perm requirement. */
function applyPermissionVisibility(root = document) {
  for (const node of $$('[data-perm]', root)) {
    node.hidden = !can(...node.dataset.perm.split(','));
  }
}

// ---------------------------------------------------------------------- i18n

/**
 * Translations for the interface.
 *
 * German is the source language and lives in the markup itself, so the `de`
 * dictionary is intentionally empty: switching back to German restores the
 * original text nodes. Every user-visible string has a key here, including the
 * ones this script generates at run time.
 */
const TRANSLATIONS = {
  // English is the source language: it lives in the markup and in the
  // fallbacks passed to t(), so a key missing from a translation below
  // degrades to English rather than to a language nobody asked for.
  en: {},

  de: {
    // Entries that read the same in both languages are still listed, so the
    // dictionary stays a complete translation of the markup rather than
    // something a reader has to diff against it.
    'admin.baseDn': 'Base DN',
    'admin.bindDn': 'Bind DN (optional)',
    'admin.dbHost': 'Host',
    'admin.dbPort': 'Port',
    'admin.logo': 'Logo (PNG/SVG, max. 256 KB)',
    'field.name': 'Name',
    'field.optional': 'optional',
    'field.start': 'Start',
    'field.status': 'Status',
    'totp.code': 'Code',

    'setup.title': 'Einrichtung',
    'setup.progress': 'Schritt {n} von {total} — {done} erledigt',
    'setup.next': 'Weiter',
    'setup.back': 'Zurück',
    'setup.skip': 'Diesen Schritt überspringen',
    'setup.openSettings': 'Stattdessen die Einstellungen öffnen',
    'setup.later': 'Später abschließen',
    'setup.finish': 'Fertigstellen',
    'setup.done': 'Einrichtung abgeschlossen.',
    'setup.stillRequired': 'Diese Schritte müssen zuerst erledigt werden; bis dahin erscheint der Assistent immer wieder.',
    'setup.password.title': 'Administrator-Passwort ändern',
    'setup.password.text': 'Dieses Konto hat noch das Passwort aus der Dokumentation, das jeder nachlesen kann. Bis zur Änderung lässt sich nichts anderes nutzen.',
    'setup.timezone.title': 'Zeitzone wählen',
    'setup.timezone.text': 'Sie bestimmt, auf welchen Kalendertag eine Buchung fällt. Ohne Angabe gilt UTC, was Abendbuchungen für den Großteil der Welt auf den falschen Tag legt.',
    'tour.card.title': 'Geführte Tour',
    'tour.card.text': 'Ein kurzer Rundgang durch die Bereiche der Anwendung. Wird bei der ersten Anmeldung einmal gezeigt; jederzeit erneut startbar.',
    'tour.card.start': 'Tour anzeigen',
    'tour.count': 'Schritt {n} von {total}',
    'tour.next': 'Weiter',
    'tour.back': 'Zurück',
    'tour.end': 'Tour überspringen',
    'tour.finish': 'Fertig',
    'tour.timer.title': 'Die Stoppuhr',
    'tour.timer.text': 'Beim Anfangen starten, beim Aufhören stoppen – die gemessene Zeit wird gebucht. Sie läuft weiter, wenn du den Browser schließt, denn sie läuft auf dem Server und nicht in diesem Tab.',
    'tour.stats.title': 'Deine eigenen Zahlen',
    'tour.stats.text': 'Stunden pro Tag und pro Projekt, über einen Zeitraum deiner Wahl. Nur deine eigenen – und niemand sonst sieht sie.',
    'tour.report.title': 'Projektberichte',
    'tour.report.text': 'Was du auf einem Projekt in einem Zeitraum gebucht hast. Deine eigenen Stunden – es gibt keine Aufschlüsselung nach Personen, denn niemand sieht, was jemand anderes erfasst hat.',
    'tour.tokens.title': 'Token und zweiter Faktor',
    'tour.tokens.text': 'Mit einem persönlichen Token kann ein Skript in deinem Namen buchen – mit genau den Rechten, die deine Rolle im Moment der Nutzung hat. Die Zwei-Faktor-Anmeldung liegt ebenfalls hier, mit einem Code zum Scannen.',
    'tour.nav.title': 'Alles liegt hier oben',
    'tour.nav.text': 'Diese Reiter sind die ganze Anwendung. Sichtbar ist nur, was deine Rolle erlaubt — bei manchen ist diese Leiste also kürzer als bei anderen.',
    'tour.book.title': 'Zeit buchen',
    'tour.book.text': 'Datum wählen, Stunden eintragen, fertig. Ein Projekt ist optional — Zeiten lassen sich jetzt erfassen und später einsortieren.',
    'tour.entries.title': 'Deine Einträge',
    'tour.entries.text': 'Alles Gebuchte, filterbar nach Projekt. Es bleibt deins zum Ändern – es gibt niemanden, dem du es vorlegen müsstest.',
    'tour.calendar.title': 'Der Monat auf einen Blick',
    'tour.calendar.text': 'Welche Tage Stunden haben und wie viele. Ein Klick auf einen Tag zeigt, was dahintersteckt – und ein Klick auf einen dieser Einträge öffnet ihn zum Korrigieren.',
    'tour.overtime.title': 'Dein Überstundensaldo',
    'tour.overtime.text': 'Gebuchte Stunden gegen dein Tagesziel. Nur Tage mit Buchungen zählen, damit Wochenenden und freie Tage sich nicht stillschweigend als Minus aufsummieren.',
    'tour.projects.title': 'Projekte, auch eigene',
    'tour.projects.text': 'Gemeinsame Projekte werden zentral angelegt. Du kannst zusätzlich private anlegen, die nur du siehst, um einen Tag aufzuteilen, wenn kein gemeinsames Projekt passt.',
    'tour.account.title': 'Mein Konto',
    'tour.account.text': 'Tagesziel, Zeitzone, Zwei-Faktor-Authentifizierung und API-Token. Das Tagesziel ist der Maßstab für den Überstundensaldo.',
    'tour.admin.title': 'Einstellungen',
    'tour.admin.text': 'Nur für dich als eingebauten Administrator: Darstellung, Datenbank, Verzeichnis, Zeitzone und die Betriebsgrenzwerte.',
    'tour.theme.title': 'Darstellung und Sprache',
    'tour.theme.text': 'Hell oder dunkel — automatisch richtet sich nach der Tageszeit. Die Sprache folgt dem Browser, bis du eine wählst. Das war alles, viel Erfolg.',

    'setup.decideFirst': 'Bitte diesen Schritt zuerst erledigen.',
    'setup.branding.title': 'Installation benennen',
    'setup.branding.text': 'Der Titel erscheint im Browser-Tab und in der Kopfzeile. Optional, aber genau das unterscheidet auf einen Blick eine Testinstanz von der echten.',
    'setup.directory.title': 'Verzeichnis anbinden (optional)',
    'setup.directory.text': 'Mit angebundenem LDAP oder Active Directory melden sich alle mit ihrem vorhandenen Konto an, und hier werden keine Passwörter gespeichert. Überspringen, um Konten lokal zu verwalten.',

    'ops.title': 'Betrieb und Grenzwerte',
    'ops.hint': 'Leer lassen, um den Wert aus der Konfigurationsdatei zu behalten. Änderungen wirken innerhalb weniger Sekunden, ohne Neustart.',
    'ops.sessionLifetime': 'Sitzungsdauer (Stunden)',
    'ops.maxDailyHours': 'Maximale Stunden pro Tag (systemweit)',
    'ops.rateLimit': 'Ratenbegrenzung (Anfragen)',
    'ops.rateWindow': 'Zeitfenster der Ratenbegrenzung (Sekunden)',
    'ops.deleteRatio': 'Verzeichnis-Abgleich: Löschgrenze (0–1)',
    'ops.reset': 'Alle Werte auf die Konfigurationsdatei zurücksetzen',
    'ops.saved': 'Grenzwerte gespeichert',
    'ops.reset.done': 'Alle Werte folgen wieder der Konfigurationsdatei',
    'ops.effective': 'Aktuell wirksam',
    'ops.sessionShort': 'Sitzung',
    'ops.maxShort': 'max./Tag',
    'ops.rateShort': 'Rate',
    'ops.ratioShort': 'Löschgrenze',

    'tel.title': 'Protokoll, Metriken und Traces',
    'tel.logLevel': 'Protokollstufe',
    'tel.activeLog': 'Protokollstufe',
    'tel.hint': 'Wird gespeichert und beim nächsten Start der Anwendung übernommen. Im laufenden Betrieb ist nichts davon umschaltbar: die Protokollstufe wird beim Start gelesen, der Metrik-Port beim Start gebunden und der Trace-Exporter beim Start gebaut. Ein Feld, das der Konfigurationsdatei folgt, behält seinen Wert von dort.',
    'tel.warn': 'Der Metrik-Port fragt nicht nach einer Anmeldung, ist nicht durch TLS geschützt und liefert neben den Metriken auch Go-Profiling-Endpunkte — wo er erreichbar ist, ist es auch ein Heap-Dump. Nur für die eigene Überwachung freigeben.',
    'tel.metrics': 'Metrik-Endpunkt',
    'tel.metricsOff': 'Nicht ausliefern',
    'tel.exporter': 'Trace-Exporter',
    'tel.tracesOff': 'Nirgendwohin exportieren',
    'tel.follow': 'Der Konfigurationsdatei folgen',
    'tel.url': 'Collector als host:port, ohne http://',
    'tel.ratio': 'Anteil aufgezeichneter Traces (0–1)',
    'tel.reset': 'Alles auf die Konfigurationsdatei zurücksetzen',
    'tel.resetDone': 'Metriken und Traces folgen wieder der Konfigurationsdatei',
    'tel.activeMetrics': 'Metriken',
    'tel.activeMetricsOff': 'werden nicht ausgeliefert',
    'tel.activeTraces': 'Traces',
    'tel.activeTracesOff': 'werden nicht exportiert',

    'password.reveal': 'Passwort anzeigen',
    'password.hide': 'Passwort verbergen',

    'restart.title': 'Neustart',
    'restart.unsupported.noExecve': 'Ein Neustart aus der Anwendung heraus ist unter Windows nicht möglich: dafür wird execve gebraucht, das es dort nicht gibt. Gespeicherte Einstellungen werden wirksam, sobald die Anwendung so neu gestartet wird, wie sie gestartet wurde.',
    'restart.unsupported.executableUnknown': 'Ein Neustart aus der Anwendung heraus ist nicht möglich: die laufende Programmdatei lässt sich nicht auffinden. Gespeicherte Einstellungen werden wirksam, sobald die Anwendung so neu gestartet wird, wie sie gestartet wurde.',
    'restart.hint': 'Einige Einstellungen werden nur beim Start der Anwendung gelesen. Diese sind gespeichert und warten:',
    'restart.now': 'Jetzt neu starten',
    'restart.confirm': 'Anwendung neu starten? Wer gerade darin arbeitet, muss die Seite neu laden.',
    'restart.waiting': 'Neustart läuft',
    'restart.waitingHint': 'Es wird gewartet, bis die Anwendung wieder erreichbar ist …',
    'restart.done': 'Die Anwendung wurde neu gestartet, die Einstellungen sind jetzt wirksam.',
    'restart.failed': 'Der Neustart konnte nicht gestartet werden',
    'restart.slow': 'Die Anwendung antwortet noch nicht. Möglicherweise startet sie noch — bitte die Seite gleich neu laden.',
    'restart.none': 'nichts',

    'user.directoryAccount': 'aus dem Verzeichnis',
    'user.directoryHint': 'Wird im LDAP verwaltet. Das Passwort liegt dort, und das Entfernen des Eintrags dort entfernt auch dieses Konto.',

    'passkey.title': 'Passkeys',
    'passkey.hint': 'Mit Fingerabdruck, Gesicht oder Geräte-PIN anmelden, statt ein Passwort einzutippen. Der Schlüssel verlässt das Gerät nie, kann also weder abgephisht noch aus einem Datenleck gelesen werden. Dein Passwort funktioniert weiterhin.',
    'passkey.name': 'Name für dieses Gerät',
    'passkey.namePlaceholder': 'Arbeitslaptop',
    'passkey.add': 'Passkey hinzufügen',
    'passkey.added': 'Hinzugefügt',
    'passkey.lastUsed': 'Zuletzt benutzt',
    'passkey.never': 'nie',
    'passkey.empty': 'Noch keine Passkeys. Füge einen hinzu, um dich ohne Passwort anzumelden.',
    'passkey.signIn': 'Mit Passkey anmelden',
    'passkey.added.done': 'Passkey hinzugefügt',
    'passkey.removed': 'Passkey entfernt',
    'passkey.cancelled': 'Der Passkey wurde nicht verwendet.',
    'passkey.err.notAllowed': 'Die Abfrage wurde abgebrochen oder lief ab. Es wurde nichts geändert.',
    'passkey.err.already': 'Dieses Gerät hat für dieses Konto schon einen Passkey.',
    'passkey.err.unsupported': 'Dieses Gerät kann keinen Passkey der Art erstellen, die diese Installation verlangt.',
    'passkey.err.insecure': 'Ein Passkey braucht HTTPS, und die Adresse in der Leiste muss die sein, für die er angelegt wurde.',
    'passkey.err.aborted': 'Die Abfrage wurde geschlossen, bevor etwas geschehen ist.',
    'passkey.failed': 'Der Passkey wurde nicht akzeptiert.',

    'tz.title': 'Zeitzone',
    'tz.hint': 'Bestimmt, auf welchen Kalendertag eine Buchung fällt — für alle, die unter „Mein Konto" keine eigene gesetzt haben.',
    'tz.myTitle': 'Meine Zeitzone',
    'tz.myHint': 'Bestimmt, auf welchen Kalendertag deine Buchungen fallen. Lass es auf „Systemeinstellung folgen", außer du arbeitest aus einem anderen Land.',
    'tz.timezone': 'Zeitzone',
    'tz.inherit': 'Systemeinstellung folgen',
    'tz.saved': 'Zeitzone gespeichert',

    'app.theme': 'Darstellung',
    'theme.auto': 'Automatisch (nach Tageszeit)',
    'theme.light': 'Hell',
    'theme.dark': 'Dunkel',

    'action.archive': 'archivieren',
    'action.book': 'Buchen',
    'action.calculate': 'Berechnen',
    'action.cancelEdit': 'Bearbeiten abbrechen',
    'action.complete': 'abschließen',
    'action.create': 'Anlegen',
    'action.delete': 'löschen',
    'bulk.pick': 'Auswählen',
    'bulk.pickAll': 'Alle auswählen',
    'bulk.count': '{n} ausgewählt',
    'bulk.delete': 'Ausgewählte löschen',
    'bulk.title': 'Die ausgewählten Zeilen löschen?',
    'bulk.text': 'Das lässt sich nicht rückgängig machen.',
    'bulk.done': '{n} gelöscht',
    'bulk.refused': '{n} konnten nicht gelöscht werden',
    'sheet.projects.text': 'Exportiert die Projekte und Kategorien, die Sie sehen. Ein Import '
      + 'ordnet Zeilen über den Namen zu: ein vorhandener Name wird aktualisiert, ein neuer '
      + 'angelegt. Als Kategorie markierte Zeilen werden Ihre eigenen privaten. Eine leere '
      + 'Zelle lässt den Wert unverändert, es geht also nichts verloren, wenn eine Spalte '
      + 'fehlt.',
    'sheet.projects.file': 'projekte',
    'sheet.projects.done': '{0} Zeilen geschrieben.',
    'sheet.users.text': 'Exportiert alle Konten: Name, E-Mail, Rolle und ob das Kennwort '
      + 'im Verzeichnis liegt. Ein Import ändert Name und Rolle, zugeordnet über die '
      + 'E-Mail-Adresse, und legt keine Konten an – ein neues braucht ein Kennwort, und das '
      + 'gehört nicht in eine Tabelle. Arbeitszeiten und Zeitzonen stehen nicht darin: sie '
      + 'gehören der Person, die sie unter „Mein Konto“ selbst setzt.',
    'sheet.users.file': 'benutzer',
    'sheet.users.done': '{0} Zeilen geschrieben.',
    'action.edit': 'bearbeiten',
    'action.evaluate': 'Auswerten',
    'action.new': 'Neu',
    'action.save': 'Speichern',
    'action.dismiss': 'Schließen',
    'action.cancel': 'Abbrechen',
    'stats.title': 'Meine Stunden',
    'stats.perDay': 'Stunden pro Tag',
    'stats.perProject': 'Stunden pro Projekt',
    'stats.total': 'Gesamt',
    'stats.noProject': 'kein Projekt',
    'stats.deletedProject': 'gelöschtes Projekt',
    'stats.empty': 'In diesem Zeitraum wurde nichts erfasst.',
    'timer.title': 'Stoppuhr',
    'timer.hint': 'Erfasst die tatsächlich gemessene Zeit, sekundengenau — der Eintrag landet auf dem Tag, an dem die Uhr gestartet wurde, in Ihrer eigenen Zeitzone.',
    'timer.start': 'Starten',
    'timer.stop': 'Stoppen und buchen',
    'timer.discard': 'Verwerfen',
    'timer.started': 'Die Uhr läuft',
    'timer.discarded': 'Die Uhr wurde verworfen',
    'timer.discardTitle': 'Gemessene Zeit verwerfen?',
    'timer.discardText': 'Die Uhr wird angehalten und nichts erfasst. Das kann nicht rückgängig gemacht werden.',
    'confirm.deleteTitle': 'Endgültig löschen?',
    'confirm.deleteText': 'wird gelöscht. Das kann nicht rückgängig gemacht werden.',
    'sync.confirmTitle': 'Abgleich ausführen?',
    'admin.activeConnection': 'Aktuell verbunden über',
    'admin.banner': 'Banner-Text (leer = ausgeblendet)',
    'admin.bindPassword': 'Bind-Passwort',
    'admin.branding': 'Erscheinungsbild',
    'admin.brandingHint': 'Titel, Banner, Logo und Fußzeile dieser Installation.',
    'admin.company': 'Firma',
    'admin.companyUrl': 'Firmen-Website',
    'admin.database': 'Datenbank-Anbindung',
    'admin.databaseHint': 'Die Verbindung wird gespeichert und beim nächsten Start der Anwendung verwendet. Ein Wechsel im laufenden Betrieb wäre nicht gefahrlos möglich.',
    'admin.dbName': 'Datenbank / Dateiname',
    'admin.dbPassword': 'Passwort',
    'admin.dbSsl': 'SSL-Modus',
    'admin.dbUser': 'Benutzer',
    'admin.defaultRole': 'Standardrolle für neue Benutzer',
    'admin.dialect': 'Typ',
    'admin.footer': 'Fußzeile',
    'admin.idAttr': 'Eindeutiges ID-Attribut (entryUUID, Active Directory: objectGUID)',
    'admin.keepStored': 'unverändert lassen',
    'admin.ldap': 'LDAP-Anbindung',
    'admin.ldapEnabled': 'Aktiviert',
    'admin.ldapHint': 'Bei aktivierter Anbindung wird das Passwort gegen das Verzeichnis geprüft. Unbekannte Benutzer werden beim ersten erfolgreichen Login lokal angelegt.',
    'admin.legal': 'Rechtlicher Hinweis',
    'admin.logoClear': 'Logo entfernen',
    'admin.logoTooBig': 'Das Logo darf höchstens 256 KB groß sein.',
    'admin.mailAttr': 'E-Mail-Attribut',
    'admin.nameAttr': 'Namens-Attribut',
    'admin.restartNeeded': 'Gespeichert. Wird beim nächsten Start übernommen.',
    'admin.saved': 'Einstellungen gespeichert',
    'admin.skipVerify': 'Zertifikat nicht prüfen (unsicher)',
    'admin.testConnection': 'Verbindung testen',
    'admin.testing': 'Verbindung wird geprüft …',
    'admin.title': 'Titel (Browser-Tab und Kopfzeile)',
    'admin.userFilter': 'Benutzer-Filter (%s = Anmeldename)',
    'app.language': 'Sprache',
    'banner.password': 'Das Initialpasswort ist noch aktiv. Bitte unter „Mein Konto" ändern — bis dahin bleibt die übrige Anwendung gesperrt.',
    'cal.close': 'schließen',
    'cal.clickToEdit': 'Zum Bearbeiten auf einen Eintrag klicken.',
    'cal.monthTotal': 'Gesamt im Monat',
    'cal.months': 'Januar,Februar,März,April,Mai,Juni,Juli,August,September,Oktober,November,Dezember',
    'cal.title': 'Kalender',
    'cal.today': 'Heute',
    'cal.weekdays': 'Mo,Di,Mi,Do,Fr,Sa,So',
    'err.adminHasNoPasskey': 'Der eingebaute Administrator meldet sich mit Kennwort an, damit sich eine Installation nie durch ein verlorenes Gerät aussperrt.',
    'err.adminRoleMustAdminister': 'Der eingebaute Administrator kann nicht in die Rolle „{0}“ wechseln, ihr fehlt „{1}“.',
    'err.adminUndeletable': 'Der eingebaute Administrator kann nicht gelöscht werden.',
    'err.archiveNeedsCompleted': 'Ein Projekt kann erst archiviert werden, wenn sein Status „{0}“ ist.',
    'err.attemptExpired': 'Dieser Versuch ist abgelaufen. Bitte erneut versuchen.',
    'err.bodyNotJSON': 'Die Anfrage enthält kein gültiges JSON.',
    'err.credentialUnreadable': 'Der Anmeldeschlüssel konnte nicht gelesen werden.',
    'err.dateFormat': 'Das Datum „{0}“ muss YYYY-MM-DD oder RFC 3339 sein.',
    'err.deletionNeedsConfirming': '„{0}“ hat {1} erfasste Zeiteinträge. Sie würden mit dem Konto gelöscht und sind nicht wiederherstellbar – zum Fortfahren bitte bestätigen.',
    'err.emailTaken': 'Es gibt bereits einen Benutzer mit der E-Mail-Adresse „{0}“.',
    'err.entryAlreadyOnProject': 'Der Zeiteintrag ist bereits auf Projekt „{0}“ gebucht.',
    'err.initialPasswordPending': 'Das Anfangskennwort muss geändert werden, bevor die Anwendung genutzt werden kann.',
    'err.invalidCredentials': 'E-Mail-Adresse oder Kennwort ist falsch.',
    'err.invalidToken': 'Ungültiges Token.',
    'err.logoNotInline': 'Das Logo muss ein eingebettetes Bild sein (data:image/…).',
    'err.logoTooLarge': 'Das Logo muss kleiner als {0} KB sein.',
    'err.mustChangePasswordFirst': 'Das Konto muss zuerst sein Anfangskennwort ändern.',
    'err.noAuthNoPassword': 'Diese Instanz läuft ohne Anmeldung, es gibt also kein Kennwort zu ändern.',
    'err.noDirectory': 'Es ist kein Verzeichnis konfiguriert.',
    'err.noSession': 'Keine Sitzung.',
    'err.noTimerRunning': 'Es läuft keine Stoppuhr.',
    'err.overDailyLimit': '{0} h würden am {2} zusammen {1} h ergeben und damit das Tagesmaximum von {3} h überschreiten.',
    'err.passkeyKnown': 'Dieser Anmeldeschlüssel ist bereits registriert.',
    'err.passkeyRejected': 'Der Anmeldeschlüssel wurde nicht akzeptiert.',
    'err.passkeyUnverified': 'Der Anmeldeschlüssel konnte nicht geprüft werden.',
    'err.passkeyWrongSession': 'Diese Registrierung gehört zu einer anderen Anmeldung.',
    'err.passwordTooShort': 'Das Kennwort muss mindestens {0} Zeichen lang sein.',
    'err.passwordUnchanged': 'Das neue Kennwort muss sich vom aktuellen unterscheiden.',
    'err.projectClosedForBooking': 'Projekt „{0}“ ist {1} und nimmt keine Zeiteinträge mehr an.',
    'err.projectIsBeingTimed': 'Bei {0} Person(en) läuft gerade eine Stoppuhr auf dieses Projekt. Es kann gelöscht werden, sobald sie gestoppt haben.',
    'err.projectHasEntries': 'Das Projekt hat noch {0} Zeiteinträge und kann nicht gelöscht werden.',
    'err.rangeInverted': '„bis“ darf nicht vor „von“ liegen.',
    'err.roleNameTaken': 'Es gibt bereits eine Rolle namens „{0}“.',
    'err.roleStillAssigned': 'Rolle „{0}“ ist noch {1} Benutzer(n) zugewiesen.',
    'err.sessionExpired': 'Die Sitzung ist abgelaufen.',
    'err.systemRoleUndeletable': 'Die Systemrolle „{0}“ kann nicht gelöscht werden.',
    'err.systemRoleUnrenamable': 'Die Systemrolle „{0}“ kann nicht umbenannt werden.',
    'err.systemRoleRightsFixed': 'Die Rechte der Systemrolle „{0}“ lassen sich nicht ändern – weder entziehen noch hinzufügen. Wer hier arbeiten und zusätzlich verwalten soll, bekommt die Rolle „employee-admin“.',
    'err.roleGrantsNothing': 'Eine Rolle muss mindestens ein Recht gewähren.',
    'err.targetOverMaximum': 'Das Tagesziel ({0} h) darf das Tagesmaximum ({1} h) nicht überschreiten.',
    'err.timerTooLong': 'Die Stoppuhr läuft seit {0} Stunden, mehr als ein Eintrag aufnehmen kann. Bitte von Hand buchen und die Stoppuhr verwerfen.',
    'err.timerTooShort': 'Die Stoppuhr läuft kürzer als die kleinste buchbare Dauer. Bitte stattdessen verwerfen.',
    'err.tooManyTokens': 'Höchstens {0} Token pro Benutzer. Bitte zuerst eines widerrufen.',
    'err.twoFactorAlreadyOn': 'Die Zwei-Faktor-Anmeldung ist bereits aktiv.',
    'err.twoFactorCodeInvalid': 'Der Zwei-Faktor-Code ist nicht gültig.',
    'err.twoFactorNotOn': 'Die Zwei-Faktor-Anmeldung ist nicht aktiv.',
    'err.twoFactorNotStarted': 'Bitte zuerst die Zwei-Faktor-Einrichtung starten.',
    'err.twoFactorRequired': 'Ein Zwei-Faktor-Code ist erforderlich.',
    'err.unknownPermissions': 'Unbekannte Rechte: {0}',
    'err.wrongCurrentPassword': 'Das aktuelle Kennwort ist nicht korrekt.',
    'field.action': 'Aktion',
    'err.importEmpty': 'Die Datei enthält nichts zu importieren.',
    'err.importHasRejectedRows': '{0} von {1} Zeilen können nicht importiert werden. Es wurde nichts geschrieben.',
    'err.noFileUploaded': 'Es wurde keine Datei übermittelt.',
    'msg.tooSlow': 'Der Server hat nicht rechtzeitig geantwortet. Bitte erneut versuchen.',
    'err.notAWorkbook': 'Das ist keine lesbare .xlsx-Datei.',
    'err.wrongWorkbook': 'Diese Datei enthält etwas anderes – vermutlich der Export einer '
      + 'anderen Tabelle.',
    'err.importStoppedAtRow': 'Abgebrochen bei Zeile {0}. {1} Zeilen wurden geschrieben; '
      + 'die Datei kann erneut importiert werden, um den Rest nachzuholen.',
    'err.uploadUnreadable': 'Die übermittelte Datei konnte nicht gelesen werden.',
    'field.actor': 'Aufrufer',
    'wb.allReady': 'Alle {0} Zeilen können importiert werden.',
    'wb.choose': 'Datei wählen…',
    'wb.chosen': 'Zum Prüfen auf „Datei prüfen“ klicken.',
    'wb.empty': 'Die Datei enthält keine Einträge.',
    'wb.export': 'Als .xlsx exportieren',
    'wb.exported': 'Export gespeichert',
    'wb.filename': 'zeiteintraege',
    'wb.import': 'Importieren',
    'wb.imported': 'Die Datei wurde importiert',
    'wb.importedCount': '{0} Einträge angelegt.',
    'wb.noFile': 'Bitte zuerst eine Datei wählen.',
    'wb.preview': 'Datei prüfen',
    'wb.problem': 'Problem',
    'wb.row': 'Zeile',
    'wb.someRejected': '{0} von {1} Zeilen können importiert werden. Solange eine Zeile abgelehnt wird, wird nichts geschrieben.',
    'wb.text': 'Exportiert, was die Filter oben zeigen. Ein Import legt Einträge an; vorhandene werden nie geändert oder ersetzt.',
    'wb.title': 'Tabelle',
    'welcome.back': 'Willkommen zurück',
    'welcome.backName': 'Willkommen zurück, {0}',
    'welcome.hello': 'Willkommen, {0}',
    'welcome.helloPlain': 'Willkommen',
    'welcome.point.account': 'Zeitzone, Sprache und einen zweiten Faktor stellst du unter „Mein Konto“ ein.',
    'welcome.point.target': 'Dein eigenes Tagessoll und Tagesmaximum stellst du unter „Mein Konto“ ein.',
    'welcome.point.administer': 'Lege die Menschen an, die hier arbeiten, und entscheide, was jede und jeder darf.',
    'welcome.point.book': 'Stunden von Hand buchen – oder eine Stoppuhr laufen lassen, die es für dich tut.',
    'welcome.point.see': 'Den Monat im Kalender sehen und die eigenen Zahlen als Diagramme.',
    'welcome.skip': 'Später',
    'welcome.begin': 'Los geht’s',
    'welcome.text': 'Hier wird deine Arbeitszeit erfasst. Ein kurzer Rundgang dauert etwa eine Minute; du kannst ihn jederzeit unter „Mein Konto“ erneut starten.',
    'welcome.timerRunning': 'Es läuft noch eine Stoppuhr. Auf diesem Bildschirm stoppen, um die Zeit zu buchen.',
    'welcome.todayHours': 'Heute bislang {0} h gebucht.',
    'welcome.todayNothing': 'Heute noch nichts gebucht.',
    'welcome.tour': 'Rundgang starten',
    'field.baseDn': 'Basis-DN',
    'field.code': 'Code',
    'field.companyUrl': 'Firmen-Adresse',
    'field.dailyTargetHours': 'Soll/Tag',
    'field.defaultRole': 'Standardrolle',
    'field.durationHours': 'Stunden',
    'field.endDate': 'Ende',
    'field.expiresInDays': 'Läuft ab in (Tagen)',
    'field.host': 'Host',
    'field.id': 'Kennung',
    'field.language': 'Sprache',
    'field.ldapSyncMaxDeleteRatio': 'Höchstanteil gelöschter Konten',
    'field.logLevel': 'Protokollstufe',
    'field.maxDailyHours': 'Max/Tag',
    'field.port': 'Port',
    'field.projectId': 'Projekt',
    'field.rateLimit': 'Anfragegrenze',
    'field.rateLimitWindowSeconds': 'Zeitfenster der Anfragegrenze (Sekunden)',
    'field.sessionLifetimeHours': 'Sitzungsdauer (Stunden)',
    'field.startDate': 'Start',
    'field.syncSchedule': 'Zeitplan',
    'field.timezone': 'Zeitzone',
    'field.traceExporter': 'Trace-Exporter',
    'field.tracerRatio': 'Trace-Anteil',
    'field.tracerUrl': 'Trace-Adresse',
    'field.userFilter': 'Benutzerfilter',
    'field.userId': 'Benutzer',
    'field.date': 'Datum',
    'field.default': 'Standard',
    'field.description': 'Beschreibung',
    'field.email': 'E-Mail',
    'field.end': 'Ende',
    'field.from': 'Von',
    'field.hours': 'Stunden',
    'field.maxPerDay': 'Max/Tag',
    'field.password': 'Passwort',
    'field.period': 'Zeitraum',
    'field.project': 'Projekt',
    'field.projectOptional': 'Projekt (optional)',
    'field.role': 'Rolle',
    'field.targetPerDay': 'Soll/Tag',
    'field.to': 'Bis',
    'field.user': 'Benutzer',
    'filter.allProjects': 'Alle Projekte',
    'footer.versionTitle': 'Laufende Version dieser Installation',
    'log.clear': 'Ansicht leeren',
    'log.delay': 'Aktualisierung alle (s)',
    'log.dropped': 'Ältere Zeilen wurden aus dem Puffer verworfen und sind nicht mehr abrufbar.',
    'log.failed': 'Das Protokoll konnte nicht gelesen werden',
    'log.follow': 'Mitlaufen',
    'log.hint': 'Was dieser Prozess geschrieben hat, das Neueste unten. Hier landet nur, was die Protokollstufe zulässt – ein Level darunter anzuhaken zeigt deshalb nichts. Die Stufe steht oben unter „Protokoll, Metriken und Traces" und wirkt ab dem nächsten Start. Nur im Speicher gehalten: nach einem Neustart ist die Ansicht leer, und sie ersetzt keine Protokollsammlung.',
    'log.manual': 'Automatische Aktualisierung ist aus. Für Mitlaufen eine Sekundenzahl eintragen.',
    'log.pause': 'Anhalten',
    'log.paused': 'Angehalten.',
    'log.resume': 'Fortsetzen',
    'log.search': 'Suche',
    'log.signedOut': 'Nicht mehr angemeldet; das Protokoll wird nicht weiter aktualisiert.',
    'log.title': 'Live-Protokoll',
    'log.unavailable': 'In diesem Prozess ist keine Protokollerfassung aktiv, es gibt daher nichts anzuzeigen.',
    'log.upTo': 'Bis Zeile',
    'login.email': 'E-Mail',
    'login.failed': 'E-Mail-Adresse oder Passwort ist nicht korrekt.',
    'login.hint': 'Bitte mit E-Mail-Adresse und Passwort anmelden.',
    'login.password': 'Passwort',
    'login.submit': 'Anmelden',
    'login.title': 'Anmelden',
    'login.totp': 'Code der Authenticator-App',
    'login.totpNeeded': 'Bitte den Code aus der Authenticator-App eingeben.',
    'msg.booked': 'Zeit gebucht',
    'msg.entryDeleted': 'Eintrag gelöscht',
    'msg.entrySaved': 'Eintrag gespeichert',
    'maint.confirm': 'Installation außer Betrieb nehmen? Alle außer diesem Konto werden abgewiesen.',
    'maint.default': 'Diese Installation ist wegen Wartungsarbeiten vorübergehend nicht verfügbar.',
    'maint.enabled': 'Außer Betrieb',
    'maint.hint': 'Weist alle anderen mit einem Hinweis ab, während die Installation weiterläuft. Vor dem Wiederherstellen oder Verschieben der Datenbank zu benutzen: in diesem Zeitraum erfasste Zeiten sind verloren, sobald der Stand zurückgespielt wird, und wer sie erfasst hat, erfährt es nicht.',
    'maint.message': 'Hinweis für alle anderen',
    'maint.messagePlaceholder': 'Ab 14:00 wieder erreichbar',
    'maint.offSaved': 'Die Installation ist wieder in Betrieb.',
    'maint.onSaved': 'Die Installation ist jetzt außer Betrieb.',
    'maint.title': 'Wartungsmodus',
    'maint.who': 'Solange er aktiv ist, kann nur dieser eingebaute Administrator arbeiten. Alle anderen, auch weitere Administratoren, werden abgewiesen — genau das ist der Zweck.',
    'msg.error': 'Fehler',
    'msg.initFailed': 'Initialisierung fehlgeschlagen',
    'msg.loadFailed': 'Konnte nicht alles laden',
    'msg.invalidFields': 'Ungültige Felder',
    'msg.passwordChanged': 'Passwort geändert. Bitte neu anmelden.',
    'msg.projectArchived': 'Projekt archiviert',
    'msg.projectCompleted': 'Projekt abgeschlossen',
    'msg.projectCreated': 'Projekt angelegt',
    'msg.projectDeleted': 'Projekt gelöscht',
    'msg.roleChanged': 'Rolle geändert',
    'msg.roleCreated': 'Rolle angelegt',
    'msg.roleDeleted': 'Rolle gelöscht',
    'msg.roleSaved': 'Rolle gespeichert',
    'msg.userCreated': 'Benutzer angelegt',
    'msg.userDeleted': 'Benutzer gelöscht',
    'msg.workingTimesSaved': 'Arbeitszeiten gespeichert',
    'nav.admin': 'Einstellungen',
    'nav.calendar': 'Kalender',
    'nav.logout': 'Abmelden',
    'nav.overtime': 'Überstunden',
    'nav.projects': 'Projekte',
    'nav.report': 'Auswertung',
    'nav.roles': 'Rollen',
    'nav.settings': 'Mein Konto',
    'nav.timesheets': 'Zeiteinträge',
    'nav.users': 'Benutzer',
    'ot.balance': 'Saldo',
    'ot.booked': 'Gebucht',
    'ot.empty': 'Keine Buchungen in diesem Zeitraum.',
    'ot.meta': '{0} · Soll {1}/Tag · gebucht {2} von {3}',
    'ot.target': 'Soll',
    'project.create': 'Projekt anlegen',
    'project.hint': 'Ihre Projekte sind Ihre: nur Sie sehen sie, und nur Sie buchen darauf. Zwei Personen an derselben Sache haben je ein eigenes.',
    'project.empty': 'Noch keine Projekte angelegt.',
    'project.open': 'offen',
    'pw.hide': 'Passwort verbergen',
    'pw.show': 'Passwort anzeigen',
    'report.result': 'Ergebnis',
    'report.total': '{0} h gesamt',
    'report.title': 'Projektauswertung',
    'role.create': 'Rolle anlegen',
    'role.edit': 'Rolle „{0}“ bearbeiten',
    'role.rightsFixed': 'Die Rechte einer Systemrolle lassen sich nicht ändern. Wer hier arbeiten und zusätzlich verwalten soll, bekommt die Rolle „employee-admin“.',
    'role.empty': 'Keine Rollen vorhanden.',
    'role.permissions': 'Berechtigungen',
    'role.rights': 'Rechte',
    'role.systemRole': 'Systemrolle',
    'role.desc.admin': 'Verwaltet die Installation, ihre Konten und deren Rollen – und erfasst selbst keine Zeit.',
    'role.desc.employee': 'Verwaltet die eigenen Zeiten, Projekte und den eigenen Kalender.',
    'role.desc.employee-admin': 'Beides: arbeitet hier und verwaltet zusätzlich die Installation.',
    'settings.changePassword': 'Passwort ändern',
    'settings.currentPassword': 'Aktuelles Passwort',
    'settings.maxHours': 'Maximale Stunden pro Tag',
    'settings.newPassword': 'Neues Passwort',
    'settings.targetHours': 'Soll-Stunden pro Tag',
    'settings.workingTimes': 'Meine Arbeitszeiten',
    'settings.workingTimesHint': 'Das Tagessoll ist die Grundlage der Überstunden. Das Tagesmaximum begrenzt, wie viel an einem Tag gebucht werden darf.',
    'sync.aborted': 'Abgebrochen',
    'sync.confirm': 'ACHTUNG: {n} Konto/Konten und {h} Zeiteinträge werden unwiderruflich gelöscht. Fortfahren?',
    'sync.created': 'Angelegt',
    'sync.deleted': 'Gelöscht',
    'sync.directoryUsers': 'Im Verzeichnis',
    'sync.entries': 'Zeiteinträge',
    'sync.schedule': 'Automatisch ausführen (Cron, fünf Felder — leer heißt nur von Hand)',
    'sync.scheduleHint': 'Standardmäßig leer, und das sollte es bleiben, bis eine Vorschau gelesen wurde: ein automatischer Lauf löscht, ohne dass jemand hinsieht. Wird beim nächsten Start übernommen — der Zeitplan wird beim Start der Anwendung gebaut.',
    'sync.scheduleStored': 'Gespeichert',
    'sync.scheduleManual': 'Läuft nur, wenn der Knopf unten gedrückt wird.',
    'sync.scheduleShort': 'Verzeichnis-Zeitplan',
    'sync.hint': 'Gleicht die Benutzer mit dem Verzeichnis ab. Das LDAP wird ausschließlich gelesen und nie verändert.',
    'sync.localExternal': 'Lokal aus dem Verzeichnis',
    'sync.none': 'Keine Konten fehlen im Verzeichnis.',
    'sync.preview': 'Vorschau',
    'sync.run': 'Abgleich ausführen',
    'sync.running': 'Wird geprüft …',
    'status.active': 'aktiv',
    'status.archived': 'archiviert',
    'status.completed': 'abgeschlossen',
    'sync.title': 'Verzeichnis-Abgleich',
    'sync.warning': 'Achtung: Konten, die im Verzeichnis fehlen, werden hier gelöscht — mitsamt ihren erfassten Zeiten, eigenen Projekten und Tokens. Das lässt sich nicht rückgängig machen. Bitte immer zuerst die Vorschau ansehen.',
    'sync.wouldCreate': 'Würden angelegt',
    'sync.wouldDelete': 'Würden gelöscht',
    'token.copyNow': 'Jetzt kopieren — dieser Wert wird nicht wieder angezeigt:',
    'token.create': 'Token erstellen',
    'token.created': 'Erstellt',
    'token.docs': 'API-Dokumentation ↗',
    'token.empty': 'Noch keine Tokens angelegt.',
    'token.expires': 'Gültig für (Tage, 0 = unbegrenzt)',
    'token.expiresAt': 'Läuft ab',
    'token.hint': 'Ein Token ersetzt bei API-Aufrufen Ihr Passwort und hat immer genau die Rechte Ihrer aktuellen Rolle. Der Wert wird nur einmal angezeigt.',
    'token.lastUsed': 'Zuletzt genutzt',
    'token.name': 'Bezeichnung',
    'token.never': 'unbegrenzt',
    'token.prefix': 'Präfix',
    'token.revoke': 'widerrufen',
    'token.revoked': 'Token widerrufen',
    'token.title': 'API-Tokens',
    'token.unused': 'nie',
    'totp.confirm': 'Bestätigen',
    'totp.disable': 'Deaktivieren',
    'totp.disabled': 'Zwei-Faktor-Authentifizierung deaktiviert',
    'totp.enable': 'Aktivieren',
    'totp.enabled': 'Zwei-Faktor-Authentifizierung aktiviert',
    'totp.instructions': 'Diesen Code mit der Authenticator-App scannen oder den Schlüssel von Hand eintragen und den angezeigten Code bestätigen:',
    'totp.manual': 'Schlüssel stattdessen von Hand eintragen',
    'totp.off': 'Zwei-Faktor-Authentifizierung ist nicht aktiviert.',
    'totp.qrAlt': 'QR-Code für die Authenticator-App',
    'totp.on': 'Zwei-Faktor-Authentifizierung ist aktiviert.',
    'totp.title': 'Zwei-Faktor-Authentifizierung',
    'ts.book': 'Zeit buchen',
    'ts.empty': 'Keine Einträge für diesen Filter.',
    'ts.edit': 'Eintrag bearbeiten',
    'ts.entries': 'Einträge',
    'ts.noProject': 'ohne Projekt',
    'user.create': 'Benutzer anlegen',
    'user.empty': 'Noch keine Benutzer angelegt.',
    'user.initialPassword': 'leer = Initialpasswort',
    'user.deleteConfirm': 'Trotzdem löschen? Die erfassten Zeiten sind danach unwiederbringlich verloren.',
    'user.systemAccount': 'Systemkonto',
  },
};

/**
 * Applies the active language.
 *
 * Elements carry their English text in the markup, so a key with no
 * translation - including every key when English itself is selected - leaves
 * the English in place. That is what makes English the fallback rather than
 * merely one more translation.
 */
function applyLanguage(language) {
  const dict = TRANSLATIONS[language] ?? {};
  document.documentElement.lang = language;

  for (const node of $$('[data-i18n]')) {
    const translated = dict[node.dataset.i18n];
    if (translated === undefined) {
      // Restore the English source from the copy taken on first run.
      if (node.dataset.i18nSource !== undefined) {
        setLeadingText(node, node.dataset.i18nSource);
      }

      continue;
    }

    if (node.dataset.i18nSource === undefined) {
      node.dataset.i18nSource = leadingText(node);
    }

    setLeadingText(node, translated);
  }

  for (const node of $$('[data-i18n-placeholder]')) {
    const translated = dict[node.dataset.i18nPlaceholder];
    if (node.dataset.i18nPlaceholderSource === undefined) {
      node.dataset.i18nPlaceholderSource = node.placeholder;
    }

    node.placeholder = translated ?? node.dataset.i18nPlaceholderSource;
  }

  for (const node of $$('[data-i18n-aria]')) {
    const translated = dict[node.dataset.i18nAria];
    if (translated) node.setAttribute('aria-label', translated);
  }
}

/**
 * Reads the label text of a node, ignoring any nested elements.
 *
 * Labels wrap their input, so the text lives in the first text node; using
 * textContent would pull the field's own content in as well.
 */
function leadingText(node) {
  const first = node.firstChild;

  return first && first.nodeType === Node.TEXT_NODE ? first.nodeValue : node.textContent;
}

function setLeadingText(node, value) {
  const first = node.firstChild;

  if (first && first.nodeType === Node.TEXT_NODE) first.nodeValue = value;
  else node.textContent = value;
}

/**
 * The language to render in.
 *
 * A signed-in user's stored choice wins. Before anyone has signed in - which
 * is exactly the sign-in screen - the browser's own preference decides, so a
 * first-time visitor is not greeted in a language they may not read.
 */
function activeLanguage() {
  if (me.user?.language) return me.user.language;

  return detectBrowserLanguage();
}

/** The language shown when nothing better is known. */
const FALLBACK_LANGUAGE = 'en';

/**
 * Picks the best supported language from the browser's preference list.
 *
 * Only the primary subtag is compared, so "en-GB" and "en-US" both match "en".
 * A browser asking for something this application does not speak gets English,
 * which is the language the markup itself is written in.
 */
function detectBrowserLanguage() {
  const supported = Object.keys(TRANSLATIONS);
  const preferences = navigator.languages?.length
    ? navigator.languages
    : [navigator.language ?? FALLBACK_LANGUAGE];

  for (const preference of preferences) {
    const primary = String(preference).toLowerCase().split('-')[0];
    if (supported.includes(primary)) return primary;
  }

  return FALLBACK_LANGUAGE;
}

/** Translates one key for use in code-generated text. */
function t(key, fallback) {
  return TRANSLATIONS[activeLanguage()]?.[key] ?? fallback;
}

/**
 * Remembers, per account and per browser, that the defaults have been offered.
 *
 * An empty stored zone means "follow the instance", which is a real choice - so
 * adopting the browser's zone every time the page loaded would make that choice
 * impossible to keep. The marker is what makes it a one-time suggestion rather
 * than a standing override.
 */
function adoptionMarker(userID) {
  return `gtr.adopted.${userID}`;
}

/**
 * Writes the browser's zone and language into the account, once.
 *
 * The browser knows two things the server cannot: which zone the person is
 * actually in, and which language they read. Until now the language was detected
 * for the current page and thrown away on every load, and the zone was not
 * detected at all - so somebody in Vancouver saw their evening bookings land on
 * the instance's tomorrow until they found the setting.
 *
 * Two deliberate limits. It happens once per account per browser, so "follow the
 * instance setting" stays choosable. And the zone is only written when it differs
 * from the instance's, because writing the same value would take that choice away
 * to no effect at all.
 *
 * Returns whether anything was written, so the caller can read the account back.
 */
async function adoptBrowserDefaults() {
  if (!me.user) return false;

  const marker = adoptionMarker(me.user.id);

  try {
    if (window.localStorage.getItem(marker)) return false;
  } catch {
    // Private browsing, or storage switched off. Suggesting once per load is
    // worse than never suggesting, so this stops here.
    return false;
  }

  let adopted = false;

  // The zone, when the browser's differs from what this account currently
  // resolves to. effectiveTimezone is the instance's while nothing is stored.
  const zone = guessTimezone();
  if (!me.user.timezone && zone && zone !== me.user.effectiveTimezone) {
    try {
      await api('/me/timezone', { method: 'PUT', body: JSON.stringify({ timezone: zone }) });
      adopted = true;
    } catch {
      // A zone this server does not know is not worth a message: the account
      // keeps following the instance, which is what it did before.
    }
  }

  // The language, when the browser asks for one this interface actually speaks.
  const language = detectBrowserLanguage();
  if (!me.user.language && language) {
    try {
      await api('/me/language', { method: 'PUT', body: JSON.stringify({ language }) });
      adopted = true;
    } catch {
      // Same reasoning: it keeps rendering in the detected language for this
      // session, it is simply not remembered.
    }
  }

  try {
    window.localStorage.setItem(marker, '1');
  } catch {
    // Nothing to do. Worst case it is suggested again on the next load, and the
    // conditions above make that a no-op.
  }

  return adopted;
}

// -------------------------------------------------------------------- views

async function loadMe() {
  me = await api('/me');
  me.permissions = me.permissions ?? [];

  const who = $('#who');
  if (me.authEnabled) {
    who.replaceChildren(
      el('strong', { text: me.user.name }),
      el('span', { text: ` · ${me.user.role || 'ohne Rolle'}` }),
    );
  } else {
    who.replaceChildren(el('span', { text: 'Authentifizierung deaktiviert' }));
  }

  $("#password-banner").hidden = !me.user.mustChangePassword;
  $("#logout").hidden = !me.authEnabled;

  applyLanguage(activeLanguage());
  applyPermissionVisibility();

  // Here rather than in loadAdmin, which is where it used to be: that runs at the end
  // of a chain of requests, so a single refusal earlier in the chain left the tab
  // hidden and the Settings screen unreachable. While the initial password still
  // stands the server refuses most of the API, which is exactly when an administrator
  // is most likely to be looking for it.
  //
  // Not a data-perm, because it is not a permission: the Settings screen belongs to
  // the built-in account rather than to a right somebody can be granted.
  $('#tab-admin').hidden = !isSystemAdmin();
  renderTOTPState();
}

async function loadUsers() {
  // Everyone can see themselves, but listing all users is a permission.
  if (!can('users:read')) {
    cache.users = me.user ? [me.user] : [];
  } else {
    cache.users = (await api('/users'))?.items ?? [];
  }

  // There is nobody to pick.
  //
  // Four controls used to ask which person: the booking form, the entry filter, the
  // calendar and the overtime form. They were built when a role could hold
  // timesheets:read:all, and they never worked as they read - the built-in
  // administrator administers accounts and does not read what people recorded in
  // them, so it was offered every colleague and every choice but itself came back
  // 403. The filter was worse: "All users" quietly showed only your own entries,
  // because the server pins the scope and the label did not know.
  //
  // Narrowing them to whoever the caller may ask about left every one of them
  // holding a single name - a dropdown with one entry, asking a question with one
  // answer. So they are gone, and with them the two rights they existed to use.
  // Whose time it is is not a choice any more, it is who is signed in.
  if (!can('users:read')) return;

  const rows = cache.users.map((u) => {
    const actions = el('td', { class: 'actions' });

    if (can('users:delete') && !u.isSystem) {
      actions.append(deleteButton({
        label: `${u.name} <${u.email}>`,
        path: `/users/${u.id}`,
        message: t('msg.userDeleted', 'User deleted'),
        after: refreshAll,
        // Deleting one account asks something extra - whether to keep its
        // entries or purge them - so the row keeps its own dialog. A batch takes
        // the plain deletion, the answer that keeps the history.
        ask: () => deleteStaffMember(u),
      }));
    }

    if (u.isSystem) actions.append(el('span', { class: 'muted', text: t('user.systemAccount', 'System account') }));

    // A directory account shown as an ordinary one invites two useless
    // actions: setting a password that is never checked, and a deletion the
    // next synchronisation undoes.
    if (u.isExternal) {
      actions.append(el('span', {
        class: 'muted',
        text: t('user.directoryAccount', 'from the directory'),
        title: t('user.directoryHint',
          'Managed in LDAP. The password lives there, and removing the entry there removes this account.'),
      }));
    }

    const roleCell = el('td', {});

    // The built-in account's role is shown as text rather than as a control that
    // cannot be used. A disabled dropdown still looks like something to click,
    // and the answer to clicking it is "no" - which is worse than not offering it.
    // Its role is fixed anyway: it must keep one that can still administer.
    if (u.isSystem) {
      roleCell.append(el('span', { text: u.role }));
    } else if (can('users:write') && can('roles:read')) {
      // Changing a role is a select rather than a form: it is the one field
      // that is changed on its own often enough to deserve it.
      const select = el('select', {
        onchange: (e) => patch(`/users/${u.id}/role`, { role: e.target.value },
          t('msg.roleChanged', 'Role changed'), refreshAll),
      });
      fillSelect(select, roleChoices(), { labelKey: 'label', valueKey: 'name' });
      select.value = u.role;
      roleCell.append(select);
    } else {
      roleCell.textContent = u.role || '–';
    }

    return el('tr', { class: me.user && u.id === me.user.id ? 'self' : '' },
      el('td', { text: u.name }),
      el('td', { text: u.email }),
      roleCell,
      el('td', { class: 'num', text: u.dailyTargetHours ? u.dailyTargetHours.toFixed(1) : t('field.default', 'default') }),
      el('td', { class: 'num', text: u.maxDailyHours ? u.maxDailyHours.toFixed(1) : t('field.default', 'default') }),
      actions,
    );
  });

  fillTable($('#table-users tbody'), rows, 6, t('user.empty', 'No users yet.'));
}

async function loadRoles() {
  if (!can('roles:read')) return;

  cache.roles = (await api('/roles'))?.items ?? [];
  cache.permissions = (await api('/permissions'))?.permissions ?? [];

  fillSelect($('#form-user select[name=role]'), roleChoices(),
    { labelKey: 'label', valueKey: 'name' });
  renderPermissionCheckboxes();

  const rows = cache.roles.map((role) => {
    const actions = el('td', { class: 'actions' });

    if (can('roles:write')) {
      actions.append(el('button', {
        class: 'link',
        text: t('action.edit', 'edit'),
        onclick: () => editRole(role),
      }));

      if (!role.isSystem) {
        actions.append(deleteButton({
          label: `${t('field.role', 'Role')} "${role.name}"`,
          path: `/roles/${role.id}`,
          message: t('msg.roleDeleted', 'Role deleted'),
          after: refreshAll,
        }));
      }
    }

    if (role.isSystem) actions.append(el('span', { class: 'muted', text: t('role.systemRole', 'System role') }));

    return el('tr', {},
      el('td', { text: role.name }),
      el('td', { text: roleDescription(role) || '–' }),
      el('td', { class: 'num', text: String(role.permissions.length) }),
      actions,
    );
  });

  fillTable($('#table-roles tbody'), rows, 4, t('role.empty', 'No roles available.'));
}

/**
 * The rights to tick, and whether they can be ticked at all.
 *
 * fixed is for a system role. Its permissions cannot be changed - neither given nor
 * taken - because that role belongs to the built-in account, which configures the
 * installation and records no time; a right added here would hand it a working day from
 * the screen that administers roles.
 *
 * Shown as unavailable rather than left clickable and refused on save. The name of a
 * system role has always been read-only in this form for the same reason: a control
 * that looks usable and answers "no" is worse than one that says so first.
 */
function renderPermissionCheckboxes(selected = [], { fixed = false } = {}) {
  const list = $('#permission-list');
  list.replaceChildren();

  for (const permission of cache.permissions) {
    list.append(el('label', { class: fixed ? 'muted' : '' },
      el('input', {
        type: 'checkbox',
        name: 'permissions',
        value: permission,
        checked: selected.includes(permission),
        disabled: fixed,
      }),
      el('span', { text: permission }),
    ));
  }

  // Why they cannot be ticked, next to them rather than in a notice after the fact.
  const note = $('#role-fixed-note');

  if (note) note.hidden = !fixed;
}

function editRole(role) {
  const form = $('#form-role');
  form.elements.id.value = String(role.id);
  form.elements.name.value = role.name;
  form.elements.description.value = role.description ?? '';
  form.elements.name.readOnly = role.isSystem;
  renderPermissionCheckboxes(role.permissions, { fixed: role.isSystem });
  $('#role-form-title').textContent = t('role.edit', 'Edit role')
    .replace('{0}', role.name);
  switchView('roles');
}

function resetRoleForm() {
  const form = $('#form-role');
  form.reset();
  form.elements.id.value = '';
  form.elements.name.readOnly = false;
  renderPermissionCheckboxes();
  $('#role-form-title').textContent = t('role.create', 'Create role');
}

async function loadProjects() {
  if (!can('projects:read')) return;

  cache.projects = (await api('/projects'))?.items ?? [];

  const bookable = cache.projects.filter((p) => p.status === 'active');
  // A blank first option is what lets time be booked without a project.
  fillSelect($('#timer-project'), bookable,
    { placeholder: t('field.projectOptional', 'Project (optional)') });
  fillSelect($('#form-timesheet select[name=projectId]'), bookable,
    { placeholder: t('ts.noProject', 'no project') });
  fillSelect($('#form-report select[name=projectId]'), cache.projects);
  fillSelect($('#filter-ts-project'), cache.projects,
    { placeholder: t('filter.allProjects', 'All projects') });

  const rows = cache.projects.map((p) => {
    const actions = el('td', { class: 'actions' });

    // Every project belongs to somebody, and the only ones anybody is handed are
    // their own - so this is true of every row here. It stays because it is the
    // question the delete button is really asking: you may remove your own way of
    // organising your hours whatever your project permissions say.
    const mine = me.user && p.ownerId === me.user.id;

    if (can('projects:write') && p.status === 'active') {
      actions.append(el('button', {
        class: 'link',
        text: t('action.complete', 'complete'),
        onclick: () => patch(`/projects/${p.id}`, { status: 'completed' }, t('msg.projectCompleted', 'Project completed'), refreshAll),
      }));
    }

    if (can('projects:archive') && p.status === 'completed') {
      actions.append(el('button', {
        class: 'link',
        text: t('action.archive', 'archive'),
        onclick: () => post(`/projects/${p.id}/archive`, null, t('msg.projectArchived', 'Project archived'), refreshAll),
      }));
    }

    if (can('projects:delete') || mine) {
      actions.append(deleteButton({
        label: `${t('field.project', 'Project')} "${p.name}"`,
        path: `/projects/${p.id}`,
        message: t('msg.projectDeleted', 'Project deleted'),
        after: refreshAll,
      }));
    }

    const period = `${fmtDate(p.startDate)} – ${p.endDate ? fmtDate(p.endDate) : t('project.open', 'open')}`;

    const name = el('td', { text: p.name });
    // No "this one is yours" shade: they all are, and a table shaded from top to
    // bottom says nothing that an unshaded one does not.
    return el('tr', {},
      name,
      // A project needs no period, so the column stays quiet when there is none:
      // it is one person's way of organising their hours, not a plan.
      el('td', { class: p.startDate ? '' : 'empty', text: p.startDate ? period : '–' }),
      el('td', { text: p.description ?? '–' }),
      el('td', {}, statusBadge(p.status)),
      actions,
    );
  });

  fillTable($('#table-projects tbody'), rows, 5, t('project.empty', 'No projects yet.'));
}

/**
 * Reloads everything that shows time entries.
 *
 * The list and the calendar render the same records from two requests, so a
 * change that refreshes only one of them leaves the other showing yesterday -
 * which is what booking an entry and switching to the calendar used to do.
 */
/**
 * Whether this caller may change this entry's figures.
 *
 * An approved entry is refused by the API however it is reached, so offering it
 * would only produce a conflict; the answer lives here so the list, the calendar
 * and the row click cannot disagree about it.
 */
function mayEditTimesheet(entry) {
  const mine = me.user && entry.userId === me.user.id;

  return Boolean(mine) && can('timesheets:write:own');
}

/**
 * Opens an existing entry in the booking form.
 *
 * The API has taken a full update since the beginning, but nothing in the
 * interface offered one: the only way to correct a typed figure was to delete the
 * entry and type it again. The form doubles as the editor rather than being
 * duplicated, so the field rules - step="any", the 24 hour ceiling, the
 * description bound - hold for a correction exactly as they do for a new booking.
 */
function editTimesheet(entry) {
  const form = $('#form-timesheet');
  if (!form) return;

  form.elements.id.value = String(entry.id);
  form.elements.projectId.value = entry.projectId ? String(entry.projectId) : '';
  form.elements.date.value = entry.date;
  form.elements.durationHours.value = String(entry.durationHours);
  form.elements.description.value = entry.description ?? '';

  $('#timesheet-form-title').textContent = t('ts.edit', 'Edit entry');
  $('#timesheet-submit').textContent = t('action.save', 'Save');
  $('#timesheet-cancel').hidden = false;

  switchView('timesheets');
  form.scrollIntoView({ block: 'nearest' });
  form.elements.durationHours.focus();
}

/** Puts the form back to booking a new entry. */
function resetTimesheetForm() {
  const form = $('#form-timesheet');
  if (!form) return;

  form.elements.id.value = '';
  form.elements.durationHours.value = '';
  form.elements.description.value = '';

  $('#timesheet-form-title').textContent = t('ts.book', 'Book time');
  $('#timesheet-submit').textContent = t('action.book', 'Book');
  $('#timesheet-cancel').hidden = true;
}

/**
 * The actions one time entry offers, as a table cell.
 *
 * Shared between the list and the calendar's day view, which render the same
 * records: two copies of these rules would be two places for "who may approve
 * this" to be answered differently, and the copy nobody was looking at would be
 * the wrong one.
 *
 * What is offered depends on the status and on the caller, because the API refuses
 * the rest anyway - the API is the authority here and this only avoids offering
 * what it would turn down.
 */
function timesheetActions(entry) {
  const actions = el('td', { class: 'actions' });
  const mayEdit = mayEditTimesheet(entry);

  if (mayEdit) {
    actions.append(el('button', {
      class: 'link',
      text: t('action.edit', 'edit'),
      onclick: () => editTimesheet(entry),
    }));
  }

  if (mayEdit) {
    actions.append(deleteButton({
      label: `${fmtDate(entry.date)}, ${fmtHours(entry.durationHours)}`,
      path: `/timesheets/${entry.id}`,
      message: t('msg.entryDeleted', 'Entry deleted'),
      after: reloadTimeViews,
    }));
  }

  return actions;
}

async function reloadTimeViews() {
  await loadTimesheets();
  await loadCalendar();
}

async function loadTimesheets() {
  if (!can('timesheets:read:own')) return;

  const params = new URLSearchParams();
  const projectId = $('#filter-ts-project').value;
  if (projectId) params.set('projectId', projectId);

  const suffix = params.toString() ? `?${params}` : '';
  const entries = (await api(`/timesheets${suffix}`))?.items ?? [];

  // Named `entry` rather than `t`, which would shadow the translation helper.
  // No user column and no "this row is yours" highlight: every row is, so a column
  // repeating one name and a shade behind every line say nothing.
  const rows = entries.map((entry) => {
    const actions = timesheetActions(entry);

    return el('tr', {},
      el('td', { text: fmtDate(entry.date) }),
      el('td', {
        class: entry.projectId ? '' : 'empty',
        text: entry.projectId ? projectName(entry.projectId) : t('ts.noProject', 'no project'),
      }),
      el('td', { class: 'num', text: entry.durationHours.toFixed(2) }),
      el('td', { text: entry.description ?? '–' }),
      actions,
    );
  });

  fillTable($('#table-timesheets tbody'), rows, 5, t('ts.empty', 'No entries for this filter.'));
}

// ----------------------------------------------------------------- calendar

/** The month the calendar is showing, as a Date on the first of that month. */
let calendarMonth = new Date(new Date().getFullYear(), new Date().getMonth(), 1);

const ISO_DAY = (d) => `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;

/**
 * The calendar day it is right now, in the zone that applies to this user.
 *
 * Not `new Date().toISOString().slice(0, 10)`, which is the day in UTC: for
 * anyone east of Greenwich that is yesterday for part of every evening, and for
 * anyone west it is tomorrow for part of every morning. Booking hours against
 * the wrong day is the kind of error nobody notices until a month-end total is
 * short.
 *
 * Intl is used rather than date arithmetic because it is the only thing in the
 * platform that knows about daylight saving.
 */
/** The date this code last filled in, so a date the user typed is recognisable. */
let autofilledDate = '';

/**
 * Points the booking form at today, leaving a date the user chose alone.
 *
 * Overwriting their choice would be worse than a stale default: someone
 * catching up on last week's hours would silently book them against today.
 */
function resetBookingDate() {
  const field = $('#form-timesheet')?.elements.date;
  if (!field) return;

  if (field.value === '' || field.value === autofilledDate) {
    autofilledDate = todayISO();
    field.value = autofilledDate;
  }
}

function todayISO() {
  const timeZone = me.user?.effectiveTimezone;

  if (!timeZone) {
    // Before sign-in there is no zone to apply, so the browser's own is the
    // best guess available - and still better than UTC.
    return ISO_DAY(new Date());
  }

  try {
    // en-CA renders as YYYY-MM-DD, which is the format the API expects.
    return new Intl.DateTimeFormat('en-CA', {
      timeZone, year: 'numeric', month: '2-digit', day: '2-digit',
    }).format(new Date());
  } catch {
    // A zone the browser does not know must not break booking entirely.
    return ISO_DAY(new Date());
  }
}

/**
 * Renders the month grid with the hours booked on each day.
 *
 * The whole month is fetched in one request and grouped client-side; asking
 * per day would be dozens of round trips for one screen.
 */
async function loadCalendar() {
  if (!can('timesheets:read:own')) return;

  const first = calendarMonth;
  const last = new Date(first.getFullYear(), first.getMonth() + 1, 0);

  const params = new URLSearchParams({ from: ISO_DAY(first), to: ISO_DAY(last) });

  const entries = (await api(`/timesheets?${params}`))?.items ?? [];

  const byDay = new Map();
  for (const entry of entries) {
    if (!byDay.has(entry.date)) byDay.set(entry.date, []);
    byDay.get(entry.date).push(entry);
  }

  renderCalendarGrid(first, last, byDay);
}

function renderCalendarGrid(first, last, byDay) {
  const monthNames = t('cal.months', 'January,February,March,April,May,June,July,August,September,October,November,December').split(',');
  $('#calendar-title').textContent = `${monthNames[first.getMonth()]} ${first.getFullYear()}`;

  const weekdays = t('cal.weekdays', 'Mon,Tue,Wed,Thu,Fri,Sat,Sun').split(',');
  $('#calendar-weekdays').replaceChildren(...weekdays.map((d) => el('span', { text: d })));

  // Weeks start on Monday, so Sunday (0) becomes the last column.
  const leading = (first.getDay() + 6) % 7;
  const cells = [];

  for (let i = 0; i < leading; i += 1) {
    cells.push(el('div', { class: 'cal-day outside' }));
  }

  const todayIso = todayISO();
  const maxHours = Math.max(8, ...[...byDay.values()].map(
    (list) => list.reduce((sum, e) => sum + e.durationHours, 0)));

  let monthTotal = 0;

  for (let d = 1; d <= last.getDate(); d += 1) {
    const date = new Date(first.getFullYear(), first.getMonth(), d);
    const iso = ISO_DAY(date);
    const dayEntries = byDay.get(iso) ?? [];
    const hours = dayEntries.reduce((sum, e) => sum + e.durationHours, 0);
    monthTotal += hours;

    const classes = ['cal-day'];
    if (dayEntries.length) classes.push('has-entries');
    if (iso === todayIso) classes.push('today');

    const cell = el(dayEntries.length ? 'button' : 'div', {
      class: classes.join(' '),
      ...(dayEntries.length ? { type: 'button' } : {}),
    }, el('span', { class: 'cal-daynum', text: String(d) }));

    if (hours > 0) {
      cell.append(el('span', { class: 'cal-hours', text: fmtHours(hours) }));

      const names = [...new Set(dayEntries.map(
        (e) => (e.projectId ? projectName(e.projectId) : t('ts.noProject', 'no project'))))];
      cell.append(el('span', { class: 'cal-projects', text: names.join(', ') }));

      const bar = el('div', { class: 'cal-bar' });
      bar.style.width = `${Math.min(100, (hours / maxHours) * 100)}%`;
      cell.append(bar);

      cell.addEventListener('click', () => showCalendarDay(iso, dayEntries));
    }

    cells.push(cell);
  }

  $('#calendar-days').replaceChildren(...cells);
  $('#calendar-summary').textContent =
    `${t('cal.monthTotal', 'Month total')}: ${fmtHours(monthTotal)}`;
  $('#calendar-day-card').hidden = true;
}

/** Shows the entries behind one day. */
function showCalendarDay(iso, entries) {
  $('#calendar-day-title').textContent = fmtDate(iso);

  const rows = entries.map((entry) => {
    const row = el('tr', {},
      el('td', { text: entry.projectId ? projectName(entry.projectId) : t('ts.noProject', 'no project') }),
      el('td', { class: 'num', text: entry.durationHours.toFixed(2) }),
      el('td', { text: entry.description ?? '–' }),
      timesheetActions(entry),
    );

    if (mayEditTimesheet(entry)) {
      row.classList.add('clickable');
      row.tabIndex = 0;
      row.title = t('cal.clickToEdit', 'Click an entry to edit it.');

      // Anywhere on the row, except on the buttons it carries - a click on
      // "delete" must not also open the entry behind the confirmation.
      row.addEventListener('click', (e) => {
        if (e.target.closest('button')) return;

        editTimesheet(entry);
      });

      // Reachable without a mouse, since a row is not a control the browser
      // would have given a keyboard behaviour to.
      row.addEventListener('keydown', (e) => {
        if (e.key !== 'Enter' && e.key !== ' ') return;

        e.preventDefault();
        editTimesheet(entry);
      });
    }

    return row;
  });

  fillTable($('#table-calendar-day tbody'), rows, 4, t('ot.empty', 'No bookings in this period.'));
  $('#calendar-day-card').hidden = false;
}

function wireCalendar() {
  const shift = (months) => {
    calendarMonth = new Date(calendarMonth.getFullYear(), calendarMonth.getMonth() + months, 1);
    mutate(loadCalendar, null, null);
  };

  $('#calendar-prev').addEventListener('click', () => shift(-1));
  $('#calendar-next').addEventListener('click', () => shift(1));
  $('#calendar-today').addEventListener('click', () => {
    const now = new Date();
    calendarMonth = new Date(now.getFullYear(), now.getMonth(), 1);
    mutate(loadCalendar, null, null);
  });
  $('#calendar-day-close').addEventListener('click', () => { $('#calendar-day-card').hidden = true; });
}

function fillSettingsForm() {
  if (!me.user) return;
  const form = $('#form-working-times');
  form.elements.dailyTargetHours.value = me.user.dailyTargetHours || '';
  form.elements.maxDailyHours.value = me.user.maxDailyHours || '';
  fillMyTimezone();
}

// --------------------------------------------------------------- API tokens

async function loadTokens() {
  const tokens = (await api('/me/tokens'))?.items ?? [];

  const rows = tokens.map((token) => el('tr', { class: token.expired ? 'empty' : '' },
    el('td', { text: token.name }),
    el('td', {}, el('code', { text: `${token.prefix}…` })),
    el('td', { text: fmtDate(token.createdAt) }),
    el('td', { text: token.expiresAt ? fmtDate(token.expiresAt) : t('token.never', 'unlimited') }),
    el('td', { text: token.lastUsedAt ? fmtDate(token.lastUsedAt) : t('token.unused', 'never') }),
    el('td', { class: 'actions' }, deleteButton({
      // Called revoking rather than deleting, because that is what it means for
      // a token - but it is the same request, so it joins the batch too.
      text: t('token.revoke', 'revoke'),
      label: `${t('token.name', 'Label')} "${token.name}"`,
      path: `/me/tokens/${token.id}`,
      message: t('token.revoked', 'Token revoked'),
      after: loadTokens,
    })),
  ));

  fillTable($('#table-tokens tbody'), rows, 6, t('token.empty', 'No tokens yet.'));
}

function wireTokens() {
  $('#form-token').addEventListener('submit', (e) => {
    e.preventDefault();

    const raw = formData(e.target);
    const body = { name: raw.name, expiresInDays: Number(raw.expiresInDays ?? 0) };

    mutate(async () => {
      const created = await api('/me/tokens', { method: 'POST', body: JSON.stringify(body) });

      // The secret exists only in this response, so it is shown until the
      // user navigates away rather than in a toast that disappears.
      $('#token-secret-value').textContent = created.secret;
      $('#token-secret').hidden = false;
      e.target.reset();
      await loadTokens();
    }, null, null);
  });
}

// ------------------------------------------------------------ administration

/** Whether the caller may open the administration screen. */
const isSystemAdmin = () => !me.authEnabled || (me.user?.isSystem ?? false);

/**
 * Applies the instance branding.
 *
 * Fetched separately from /me because the sign-in screen must show the title
 * and logo before anyone has authenticated.
 */
async function loadBranding() {
  const branding = await api('/branding');

  document.title = branding.title || 'Time Recording';
  $('#app-title').textContent = branding.title || 'Time Recording';

  for (const holder of ['#brand-logo', '#login-logo']) {
    const img = $(holder);
    if (!img) continue;

    img.src = branding.logo || '';
    img.hidden = !branding.logo;
    img.alt = branding.title || '';
  }

  // The announcement banner is separate from the "change your password" one.
  const banner = $('#instance-banner');
  banner.textContent = branding.banner ?? '';
  banner.hidden = !branding.banner;

  $('#footer-text').textContent = branding.footerText ?? '';
  $('#footer-legal').textContent = branding.legalNotice ?? '';

  const company = $('#footer-company');
  company.textContent = branding.companyName ?? '';
  company.hidden = !branding.companyName;
  if (branding.companyUrl) company.href = branding.companyUrl;
  else company.removeAttribute('href');

  // The build serving this page. Shown even when nothing else is configured,
  // which is why the footer is no longer hidden when the branding is empty:
  // "which version is actually running" is the first question of every support
  // conversation, and guessing it from a container tag is not an answer.
  // The instance's own logo in the browser tab, where somebody with several
  // installations open tells them apart. Falls back to the built-in mark, so an
  // instance with no logo looks as it always did.
  applyFavicon(branding.logo);

  const versionEl = $('#footer-version');
  if (versionEl) {
    // The platform beside the version, as "v1.0 (windows)". The same version is
    // published for four of them and they do not all behave alike - restarting
    // from the interface works on Linux and cannot on Windows - so the version
    // alone does not say what somebody is looking at.
    const version = branding.version || '';
    const platform = branding.os || '';

    versionEl.textContent = version && platform ? `${version} (${platform})` : version;
    versionEl.title = version
      ? t('footer.versionTitle', 'Running version of this installation')
      : '';
  }

  return branding;
}

async function loadAdmin() {
  // The tab is shown as soon as who is signed in is known, which is not here - see
  // refreshMe. This only decides whether there is anything to load.
  if (!isSystemAdmin()) return;

  await loadMaintenance();

  const branding = await api('/branding');
  const form = $('#form-branding');
  for (const field of ['title', 'banner', 'companyName', 'companyUrl', 'footerText', 'legalNotice']) {
    form.elements[field].value = branding[field] ?? '';
  }

  setLogoPreview(branding.logo ?? '');

  await loadOperational();

  const timezone = await api('/settings/timezone');
  const instanceSelect = $('#instance-timezone');

  fillTimezoneSelect(instanceSelect, timezone.timezone ?? 'UTC');
  showTimeIn(instanceSelect, $('#instance-timezone-now'), 'UTC');

  const ds = await api('/settings/datasource');
  const dsForm = $('#form-datasource');
  for (const field of ['dialect', 'name', 'host', 'port', 'user', 'sslMode']) {
    dsForm.elements[field].value = ds[field] ?? '';
  }

  $('#datasource-active').textContent =
    `${t('admin.activeConnection', 'Currently connected via')}: ${ds.active}`;

  await loadTelemetry();
  await loadRestart();

  const ldap = await api('/settings/ldap');
  const ldapForm = $('#form-ldap');
  const ldapFields = [
    'host', 'baseDn', 'bindDn', 'userFilter',
    'nameAttribute', 'emailAttribute', 'idAttribute',
  ];
  for (const field of ldapFields) {
    ldapForm.elements[field].value = ldap[field] ?? '';
  }
  ldapForm.elements.port.value = ldap.port || 389;
  for (const flag of ['enabled', 'startTls', 'useTls', 'skipVerify']) {
    ldapForm.elements[flag].checked = Boolean(ldap[flag]);
  }

  fillSelect(ldapForm.elements.defaultRole, roleChoices(),
    { labelKey: 'label', valueKey: 'name' });
  ldapForm.elements.defaultRole.value = ldap.defaultRole ?? 'employee';

  const schedule = $('#form-sync-schedule');
  if (schedule) {
    schedule.elements.syncSchedule.value = ldap.syncSchedule ?? '';

    // What this process is actually scheduled to do, which is not the stored
    // value until the next start - and the restart card is what says so.
    $('#sync-schedule-active').textContent = ldap.syncSchedule
      ? `${t('sync.scheduleStored', 'Saved')}: ${ldap.syncSchedule}`
      : t('sync.scheduleManual', 'Runs only when the button below is pressed.');
  }
}

/** The logo travels as a data URI, so it needs no upload endpoint. */
let pendingLogo = '';

function setLogoPreview(dataURI) {
  pendingLogo = dataURI;

  const preview = $('#logo-preview');
  preview.src = dataURI || '';
  preview.hidden = !dataURI;
}

function wireAdmin() {
  $('#logo-file').addEventListener('change', (e) => {
    const file = e.target.files?.[0];
    if (!file) return;

    // 256 KB matches the server-side limit; checking here gives a better
    // message than a rejected request would.
    if (file.size > 256 * 1024) {
      toast(t('admin.logoTooBig', 'The logo must be smaller than 256 KB.'), 'error');
      e.target.value = '';

      return;
    }

    const reader = new FileReader();
    reader.onload = () => setLogoPreview(String(reader.result));
    reader.readAsDataURL(file);
  });

  $('#logo-clear').addEventListener('click', () => {
    setLogoPreview('');
    $('#logo-file').value = '';
  });

  $('#form-branding').addEventListener('submit', (e) => {
    e.preventDefault();
    const body = { ...formData(e.target), logo: pendingLogo };
    mutate(() => api('/settings/branding', { method: 'PUT', body: JSON.stringify(body) }),
      t('admin.saved', 'Settings saved'),
      async () => { await loadBranding(); await loadAdmin(); });
  });

  $('#form-datasource').addEventListener('submit', (e) => {
    e.preventDefault();
    const body = formData(e.target);
    mutate(async () => {
      const result = await api('/settings/datasource', { method: 'PUT', body: JSON.stringify(body) });
      toast(result.message ?? t('admin.restartNeeded', 'Saved. Applied on the next start.'), 'ok');
    }, null, loadAdmin); // loadAdmin ends with loadRestart, so the card follows.
  });

  $('#form-ldap').addEventListener('submit', (e) => {
    e.preventDefault();
    mutate(() => api('/settings/ldap', { method: 'PUT', body: JSON.stringify(ldapPayload()) }),
      t('admin.saved', 'Settings saved'),
      loadAdmin);
  });

  $('#form-sync-schedule').addEventListener('submit', (e) => {
    e.preventDefault();
    mutate(
      () => api('/settings/ldap', { method: 'PUT', body: JSON.stringify(ldapPayload()) }),
      t('admin.restartNeeded', 'Saved. Applied on the next start.'),
      // The restart card too: the schedule is the one directory setting that
      // waits, because a scheduler is built while the application starts.
      async () => { await loadAdmin(); });
  });

  $('#datasource-test').addEventListener('click', () => {
    const result = $('#datasource-test-result');
    result.textContent = t('admin.testing', 'Testing the connection …');
    result.className = 'muted';

    mutate(async () => {
      const outcome = await api('/settings/datasource/test',
        { method: 'POST', body: JSON.stringify(formData($('#form-datasource'))) });

      result.textContent = outcome.message;
      result.className = outcome.ok ? 'muted plus' : 'muted minus';
    }, null, null);
  });

  $('#ldap-test').addEventListener('click', () => {
    const result = $('#ldap-test-result');
    result.textContent = t('admin.testing', 'Testing the connection …');

    mutate(async () => {
      const outcome = await api('/settings/ldap/test',
        { method: 'POST', body: JSON.stringify(ldapPayload()) });

      result.textContent = outcome.message;
      result.className = outcome.ok ? 'muted plus' : 'muted minus';
    }, null, null);
  });
}

/**
 * Runs a directory reconciliation, or previews one.
 *
 * The real run asks for confirmation naming the exact number of accounts and
 * recorded entries at stake, because it cannot be undone.
 */
function wireDirectorySync() {
  const status = $('#sync-status');
  const result = $('#sync-result');

  const show = (report) => {
    const rows = report.candidates.map((c) => el('tr', {},
      el('td', { text: c.name }),
      el('td', { text: c.email }),
      el('td', { class: `num ${c.timesheets > 0 ? 'minus' : ''}`, text: String(c.timesheets) }),
    ));

    fillTable($('#table-sync tbody'), rows, 3, t('sync.none', 'No accounts are missing from the directory.'));
    result.hidden = false;

    if (report.aborted) {
      status.textContent = `${t('sync.aborted', 'Aborted')}: ${report.aborted}`;
      status.className = 'muted minus';

      return;
    }

    const parts = [
      `${t('sync.directoryUsers', 'In the directory')}: ${report.directoryUsers}`,
      `${t('sync.localExternal', 'Local, from the directory')}: ${report.localExternal}`,
    ];

    if (report.dryRun) {
      parts.push(`${t('sync.wouldDelete', 'Would be deleted')}: ${report.candidates.length}`);
      parts.push(`${t('sync.wouldCreate', 'Would be created')}: ${report.created.length}`);
    } else {
      parts.push(`${t('sync.deleted', 'Deleted')}: ${report.deleted.length}`);
      parts.push(`${t('sync.created', 'Created')}: ${report.created.length}`);
    }

    status.textContent = parts.join(' · ');
    status.className = 'muted';
  };

  $('#sync-preview').addEventListener('click', () => {
    status.textContent = t('sync.running', 'Checking …');
    status.className = 'muted';

    // Cleared on the way out, not only on the way through: a failure raises its
    // own notice, and "Checking …" left standing underneath it says the check is
    // still running when it has already finished badly.
    mutate(
      async () => show(await api('/settings/ldap/sync/preview', { method: 'POST' })),
      null,
      null,
    ).finally(() => {
      if (status.textContent === t('sync.running', 'Checking …')) status.textContent = '';
    });
  });

  $('#sync-run').addEventListener('click', () => {
    mutate(async () => {
      // Preview first, so the confirmation can state the real damage instead
      // of a vague warning.
      const preview = await api('/settings/ldap/sync/preview', { method: 'POST' });
      show(preview);

      if (preview.aborted) return;

      const hours = preview.candidates.reduce((sum, c) => sum + c.timesheets, 0);

      if (preview.candidates.length > 0) {
        const question = t('sync.confirm', 'WARNING: {n} account(s) and {h} time entries will be deleted irreversibly. Continue?')
          .replace('{n}', String(preview.candidates.length))
          .replace('{h}', String(hours));

        const proceed = await confirmDialog({
          title: t('sync.confirmTitle', 'Run the synchronisation?'),
          text: question,
          confirmLabel: t('sync.run', 'Run synchronisation'),
        });

        if (!proceed) return;
      }

      show(await api('/settings/ldap/sync', { method: 'POST' }));
      await refreshAll();
    }, null, null);
  });
}

/** Reads the LDAP form, including its checkboxes and numeric port. */
function ldapPayload() {
  const form = $('#form-ldap');
  const body = {
    host: form.elements.host.value.trim(),
    port: Number(form.elements.port.value || 389),
    baseDn: form.elements.baseDn.value.trim(),
    bindDn: form.elements.bindDn.value.trim(),
    bindPassword: form.elements.bindPassword.value,
    userFilter: form.elements.userFilter.value.trim(),
    nameAttribute: form.elements.nameAttribute.value.trim(),
    emailAttribute: form.elements.emailAttribute.value.trim(),
    idAttribute: form.elements.idAttribute.value.trim(),
    defaultRole: form.elements.defaultRole.value,
  };

  for (const flag of ['enabled', 'startTls', 'useTls', 'skipVerify']) {
    body[flag] = form.elements[flag].checked;
  }

  // The schedule lives in its own card, next to the buttons that run the thing
  // it schedules, but it is stored with the rest of the directory settings and
  // travels on the same request - so it has to be in every payload or saving the
  // connection would clear it.
  const schedule = $('#form-sync-schedule');
  body.syncSchedule = schedule ? schedule.elements.syncSchedule.value.trim() : '';

  return body;
}

// ------------------------------------------------------------- mutations

/** Wraps a mutating call so every failure surfaces as a toast, not a crash. */
async function mutate(fn, successMessage, after) {
  try {
    // Handed to `after`, so a caller that needs what the call answered does not
    // have to make the call again or smuggle it out through a closure. Every
    // existing caller ignores it, which is what makes this safe to add.
    const result = await fn();
    if (successMessage) toast(successMessage, 'ok');
    if (after) await after(result);
  } catch (err) {
    toast(err.message, 'error');
  }
}

const post = (path, body, msg, after) => mutate(
  () => api(path, { method: 'POST', body: body ? JSON.stringify(body) : undefined }), msg, after);

const patch = (path, body, msg, after) => mutate(
  () => api(path, { method: 'PUT', body: JSON.stringify(body) }), msg, after);

const remove = (path, msg, after) => mutate(
  () => api(path, { method: 'DELETE' }), msg, after);

/**
 * Asks a yes-or-no question, and resolves to the answer.
 *
 * Replaces window.confirm, which the browser draws itself: an unstyled box
 * naming the origin, unreadable in a dark theme, and impossible to translate
 * beyond the message inside it. It also blocks the whole page, which is why the
 * old maintenance-mode call had to put the checkbox back by hand afterwards.
 *
 * The markup is built rather than written into index.html, because a question
 * differs only in its words - and reusing .overlay and .overlay-card means this
 * needs no new styling at all.
 */
function confirmDialog({ title, text, detail, confirmLabel, danger = true }) {
  return new Promise((resolve) => {
    // What had focus before, so it can be given back: a dialog that leaves
    // focus on nothing strands anybody using a keyboard.
    const previous = document.activeElement;

    const card = el('div', {
      class: 'overlay-card confirm-card',
      role: 'dialog',
      'aria-modal': 'true',
    });

    card.append(el('h2', { text: title }));
    card.append(el('p', { class: 'muted', text }));

    // The server's own words, when there are any - the count of what would be
    // destroyed usually lives there rather than in the question.
    if (detail) card.append(el('p', { class: 'muted minus', text: detail }));

    const overlay = el('div', { class: 'overlay confirm-overlay' }, card);

    const close = (answer) => {
      document.removeEventListener('keydown', onKey);
      overlay.remove();

      if (previous instanceof HTMLElement) previous.focus();

      resolve(answer);
    };

    // Escape cancels, which is what a dialog with a destructive option should
    // do with an ambiguous keypress.
    const onKey = (event) => {
      if (event.key === 'Escape') close(false);
    };

    const cancel = el('button', {
      class: 'secondary',
      type: 'button',
      text: t('action.cancel', 'Cancel'),
      onclick: () => close(false),
    });

    const proceed = el('button', {
      class: danger ? 'danger' : '',
      type: 'button',
      text: confirmLabel,
      onclick: () => close(true),
    });

    card.append(el('div', { class: 'row confirm-actions' }, cancel, proceed));

    document.addEventListener('keydown', onKey);
    document.body.append(overlay);

    // Cancel takes the focus, not the destructive button: a stray Enter should
    // not delete anything.
    cancel.focus();
  });
}

/**
 * The question asked before something is deleted for good.
 *
 * One wording for all of them, with the thing being deleted named, so the
 * question reads the same wherever it comes from - and so that five delete
 * buttons which asked nothing at all now ask the same thing.
 */
function confirmDelete(what) {
  return confirmDialog({
    title: t('confirm.deleteTitle', 'Delete for good?'),
    text: `${what} ${t('confirm.deleteText', 'will be deleted. This cannot be undone.')}`,
    confirmLabel: t('action.delete', 'delete'),
  });
}

/** remove() with the question in front of it. */
const removeAfterConfirm = async (what, path, msg, after) => {
  if (await confirmDelete(what)) await remove(path, msg, after);
};

/**
 * Deletes a user, asking first when it would destroy recorded time.
 *
 * The server refuses that deletion rather than performing it, and its refusal
 * carries the number of entries. So the question put to the administrator has
 * the real number in it rather than a general warning: "42 entries" is a
 * different decision from "some data may be lost", and only the server knows
 * which it is.
 *
 * An account with nothing recorded is deleted straight away. Asking about
 * something with no consequence trains people to click through the dialog that
 * does have one.
 */
async function deleteStaffMember(user) {
  const done = t('msg.userDeleted', 'Staff member deleted');

  try {
    await api(`/users/${user.id}`, { method: 'DELETE' });
    toast(done, 'ok');
    await refreshAll();

    return;
  } catch (err) {
    if (err.status !== 409) {
      toast(err.message, 'error');

      return;
    }

    // The refusal explains what is attached, so it becomes the question - the
    // server's own sentence as the detail, because it carries the count, and
    // "42 entries" is a different decision from "some data may be lost".
    const proceed = await confirmDialog({
      title: t('confirm.deleteTitle', 'Delete for good?'),
      text: t('user.deleteConfirm', 'Delete anyway? The recorded time cannot be recovered.'),
      detail: err.message,
      confirmLabel: t('action.delete', 'delete'),
    });

    if (!proceed) return;
  }

  mutate(
    () => api(`/users/${user.id}?purge=true`, { method: 'DELETE' }),
    done,
    refreshAll);
}

// ------------------------------------------------------------------ sign-in

/** Shows the sign-in overlay and hides the application behind it. */
function showLogin(message) {
  const screen = $('#login-screen');
  screen.hidden = false;
  // The check is over, whatever it concluded: show the form rather than the
  // spinner that stood in for it.
  screen.classList.remove('checking');

  const error = $('#login-error');
  error.textContent = message ?? '';
  error.hidden = !message;
}

function hideLogin() {
  $('#login-screen').classList.remove('checking');
  $('#login-screen').hidden = true;
  $('#login-error').hidden = true;
  $('#login-totp-field').hidden = true;
  $('#form-login').reset();
}

async function submitLogin(e) {
  e.preventDefault();

  const form = e.target;
  const body = {
    email: form.elements.email.value.trim(),
    password: form.elements.password.value,
    totp: form.elements.totp.value.trim(),
  };

  // Only the sign-in call itself decides whether the credentials were wrong.
  //
  // Wrapping the loading that follows in the same catch made a correct password
  // look like a wrong one: with the initial password still in place the server
  // refuses the rest of the API, refreshAll threw, and the handler put the
  // sign-in screen back with "email address or password is not correct" - on
  // the one account that had just authenticated successfully. There was no way
  // through to the screen that would have fixed it.
  let result;

  try {
    result = await api('/auth/login', { method: 'POST', body: JSON.stringify(body) });
  } catch {
    // The server deliberately does not say which part was wrong, so neither
    // does the interface.
    showLogin(t('login.failed', 'Email address or password is not correct.'));
    form.elements.password.value = '';

    return;
  }

  // The password was right but the account has a second factor: ask for the
  // code instead of reporting a failed sign-in.
  if (result.totpRequired) {
    $('#login-totp-field').hidden = false;
    $('#login-error').textContent = t('login.totpNeeded', 'Please enter the code from your authenticator app.');
    $('#login-error').hidden = false;
    form.elements.totp.focus();

    return;
  }

  hideLogin();

  try {
    await refreshAll();
    switchView(firstVisibleView());

    // Here as well as on a page load, or the greeting would only ever appear to
    // somebody who reloaded after signing in - which is nobody's first sign-in.
    await greetAfterSignIn();
  } catch (err) {
    // Signed in, but something behind it would not load. Staying on the
    // application with an explanation beats being thrown back to a sign-in
    // screen that will accept the same password and do this again.
    toast(`${t('msg.loadFailed', 'Could not load everything')}: ${err.message}`, 'error');
  }
}

async function doLogout() {
  try {
    await api('/auth/logout', { method: 'POST' });
  } catch {
    // Even a failed call should drop the client back to the sign-in screen.
  }

  // Before the state is cleared: the poller checks isSystemAdmin(), and a
  // timer left running would keep asking for the log with no session and paint
  // the screen with authentication failures.
  stopLogPolling();

  me = { user: null, permissions: [], authEnabled: true };
  showLogin();
}

// ------------------------------------------------------------- two-factor

function renderTOTPState() {
  const enabled = me.user?.totpEnabled ?? false;

  $('#totp-state').textContent = enabled
    ? t('totp.on', 'Two-factor authentication is enabled.')
    : t('totp.off', 'Two-factor authentication is not enabled.');

  $('#totp-begin').hidden = enabled;
  $('#totp-disable').hidden = !enabled;
  $('#totp-confirm').hidden = true;
  $('#totp-setup').hidden = true;

  // The code encodes the secret, so it must not survive the enrolment it belongs
  // to - neither on screen nor in the markup.
  const qr = $('#totp-qr');
  qr.hidden = true;
  qr.removeAttribute('src');
  $('#totp-secret').textContent = '';
  $('#totp-uri').textContent = '';
  // Disabling also needs a current code, so the field stays visible for it.
  $('#totp-code-field').hidden = !enabled;
}

function wireTOTP() {
  $('#totp-begin').addEventListener('click', () => mutate(async () => {
    const setup = await api('/me/totp', { method: 'POST' });
    $('#totp-secret').textContent = setup.secret;
    $('#totp-uri').textContent = setup.uri;

    // The picture is the ordinary way in; the typed key stays folded away behind
    // it for a machine with no camera, and for when a code will not scan.
    const qr = $('#totp-qr');
    qr.hidden = !setup.qr;
    qr.alt = setup.qr ? t('totp.qrAlt', 'QR code for your authenticator app') : '';
    if (setup.qr) qr.src = setup.qr;

    // Open on the key when there is no picture, or the screen would offer nothing.
    $('#totp-manual').open = !setup.qr;

    $('#totp-setup').hidden = false;
    $('#totp-code-field').hidden = false;
    $('#totp-confirm').hidden = false;
    $('#totp-begin').hidden = true;
  }, null, null));

  $('#totp-confirm').addEventListener('click', () => {
    const code = $('#totp-code').value.trim();
    mutate(() => api('/me/totp', { method: 'PUT', body: JSON.stringify({ code }) }),
      t('totp.enabled', 'Two-factor authentication enabled'),
      async () => { $('#totp-code').value = ''; await refreshAll(); });
  });

  $('#totp-disable').addEventListener('click', () => {
    const code = $('#totp-code').value.trim();
    mutate(() => api(`/me/totp?code=${encodeURIComponent(code)}`, { method: 'DELETE' }),
      t('totp.disabled', 'Two-factor authentication disabled'),
      async () => { $('#totp-code').value = ''; await refreshAll(); });
  });
}

async function loadLanguages() {
  const languages = (await api('/languages'))?.languages ?? [FALLBACK_LANGUAGE];
  const picker = $('#language-picker');

  picker.replaceChildren();
  for (const language of languages) {
    picker.append(el('option', { value: language, text: language.toUpperCase() }));
  }

  // Nobody signed in yet means the picker should show what is actually on
  // screen, which is whatever the browser preference resolved to.
  picker.value = me.user?.language ?? activeLanguage();
}

// ------------------------------------------------------------- guided tour

/**
 * The tour, in the order someone meets the application.
 *
 * Each step names a real selector rather than describing a control in prose,
 * so a step whose target has been removed can be detected and dropped instead
 * of pointing at nothing. `view` switches to the tab first, because explaining
 * the calendar while the timesheet is on screen helps nobody.
 *
 * `permission` keeps the tour honest about what this person can actually do:
 * an employee has no Roles tab, and a tour that mentions one is a tour that
 * lies.
 */
const TOUR_STEPS = [
  {
    target: '#tabs',
    view: null,
    title: () => t('tour.nav.title', 'Everything lives up here'),
    text: () => t('tour.nav.text',
      'These tabs are the whole application. You only see the ones your role allows, '
      + 'so this bar is shorter for some people than for others.'),
  },
  {
    target: '#timer-card',
    view: 'timesheets',
    permission: 'timesheets:write:own',
    title: () => t('tour.timer.title', 'The stopwatch'),
    text: () => t('tour.timer.text',
      'Start it when you start, stop it when you are done, and it books the time it '
      + 'measured. It keeps running if you close the browser, because it runs on the '
      + 'server rather than in this tab.'),
  },
  {
    target: '#form-timesheet',
    view: 'timesheets',
    permission: 'timesheets:write:own',
    title: () => t('tour.book.title', 'Booking time'),
    text: () => t('tour.book.text',
      'Pick a date, enter the hours, done. A project is optional — hours can be recorded '
      + 'now and sorted later.'),
  },
  {
    target: '#table-timesheets',
    view: 'timesheets',
    permission: 'timesheets:read:own',
    title: () => t('tour.entries.title', 'Your entries'),
    text: () => t('tour.entries.text',
      'Everything you booked, filterable by project. It stays yours to change: there '
      + 'is nobody to submit it to.'),
  },
  {
    target: '#calendar-days',
    view: 'calendar',
    permission: 'timesheets:read:own',
    title: () => t('tour.calendar.title', 'The month at a glance'),
    text: () => t('tour.calendar.text',
      'Which days have hours on them, and how many. Click a day to see what is behind '
      + 'it, and click one of those entries to correct it.'),
  },
  {
    target: '#chart-days',
    view: 'overtime',
    permission: 'timesheets:read:own',
    title: () => t('tour.stats.title', 'Your own figures'),
    text: () => t('tour.stats.text',
      'Hours per day and per project, over any period you choose. Yours alone, and '
      + 'nobody else sees them.'),
  },
  {
    target: '#form-overtime',
    view: 'overtime',
    permission: 'timesheets:read:own',
    title: () => t('tour.overtime.title', 'Your overtime balance'),
    text: () => t('tour.overtime.text',
      'Booked hours against your daily target. Only days with bookings count, so weekends '
      + 'and time off do not quietly pile up as a deficit.'),
  },
  {
    target: '#form-project',
    view: 'projects',
    permission: 'projects:write',
    title: () => t('tour.projects.title', 'Your projects'),
    text: () => t('tour.projects.text',
      'Somewhere to put your hours, and yours alone: only you see them and only you book '
      + 'on them. Two people on the same job keep a project each.'),
  },
  {
    target: '#form-report',
    view: 'report',
    permission: 'reports:read:own',
    title: () => t('tour.report.title', 'Project reports'),
    text: () => t('tour.report.text',
      'What you have booked on one project over a period. Your own hours: there is no '
      + 'breakdown per colleague, because nobody sees what anybody else recorded.'),
  },
  {
    target: '#token-card',
    view: 'settings',
    title: () => t('tour.tokens.title', 'Tokens and a second factor'),
    text: () => t('tour.tokens.text',
      'A personal token lets a script book time as you, and carries exactly the rights '
      + 'your role has at the time it is used. Two-factor authentication is here as '
      + 'well, with a code to scan.'),
  },
  {
    target: '#form-working-times',
    view: 'settings',
    title: () => t('tour.account.title', 'My account'),
    text: () => t('tour.account.text',
      'Your daily target, timezone, two-factor authentication and API tokens. The daily '
      + 'target is what the overtime balance is measured against.'),
  },
  {
    target: '#tab-admin',
    view: null,
    adminOnly: true,
    title: () => t('tour.admin.title', 'Settings'),
    text: () => t('tour.admin.text',
      'Yours alone as the built-in administrator: appearance, database, directory, timezone '
      + 'and the operational limits.'),
  },
  {
    target: '#theme-picker',
    view: null,
    title: () => t('tour.theme.title', 'Appearance and language'),
    text: () => t('tour.theme.text',
      'Light or dark — automatic follows the time of day. The language follows your browser '
      + 'until you choose one. That is everything; enjoy.'),
  },
];

/** Where we are in the tour, and the steps that apply to this person. */
let tour = { steps: [], index: 0, active: false };

/** Drops steps this person cannot reach, so the tour never points at nothing. */
function applicableTourSteps() {
  return TOUR_STEPS.filter((step) => {
    if (step.adminOnly && !isSystemAdmin()) return false;
    if (step.permission && !can(...step.permission.split(','))) return false;

    // A target that is not in the document, or is hidden for this person, is
    // not something to explain.
    const node = $(step.target);

    return node && node.offsetParent !== null;
  });
}

/** Positions the spotlight and the bubble around one element. */
function placeTour(node) {
  const rect = node.getBoundingClientRect();
  const pad = 6;
  const spotlight = $('#tour-spotlight');
  const bubble = $('#tour-bubble');

  // Page coordinates, not viewport ones: both elements are absolutely
  // positioned in the document, so they stay put while it scrolls.
  const top = rect.top + window.scrollY - pad;
  const left = rect.left + window.scrollX - pad;

  spotlight.style.top = `${top}px`;
  spotlight.style.left = `${left}px`;
  spotlight.style.width = `${rect.width + pad * 2}px`;
  spotlight.style.height = `${rect.height + pad * 2}px`;
  spotlight.hidden = false;

  bubble.hidden = false;

  // Below the target where there is room, above it otherwise, and never off
  // the right edge on a narrow screen.
  const bubbleRect = bubble.getBoundingClientRect();
  const below = rect.bottom + 14;
  const fitsBelow = below + bubbleRect.height < window.innerHeight;

  bubble.style.top = fitsBelow
    ? `${below + window.scrollY}px`
    : `${rect.top + window.scrollY - bubbleRect.height - 14}px`;

  const maxLeft = window.scrollX + window.innerWidth - bubbleRect.width - 12;
  bubble.style.left = `${Math.max(window.scrollX + 12, Math.min(left, maxLeft))}px`;
}

async function renderTourStep() {
  const step = tour.steps[tour.index];

  if (step.view) switchView(step.view);

  const node = $(step.target);
  if (!node) {
    // The target went away between building the list and getting here.
    await advanceTour();

    return;
  }

  node.scrollIntoView({ block: 'center', behavior: 'smooth' });

  $('#tour-count').textContent = t('tour.count', 'Step {n} of {total}')
    .replace('{n}', String(tour.index + 1))
    .replace('{total}', String(tour.steps.length));
  $('#tour-title').textContent = step.title();
  $('#tour-text').textContent = step.text();

  $('#tour-back').disabled = tour.index === 0;
  $('#tour-next').textContent = tour.index === tour.steps.length - 1
    ? t('tour.finish', 'Finish')
    : t('tour.next', 'Next');

  // After the layout has settled from the view switch and the scroll, or the
  // rectangle measured would be the one from before it moved.
  requestAnimationFrame(() => requestAnimationFrame(() => placeTour(node)));
}

async function advanceTour() {
  if (tour.index >= tour.steps.length - 1) {
    await endTour();

    return;
  }

  tour.index += 1;
  await renderTourStep();
}

/** Starts the tour, from the beginning. */
async function startTour() {
  tour.steps = applicableTourSteps();

  if (tour.steps.length === 0) return;

  tour.index = 0;
  tour.active = true;
  await renderTourStep();
}

/**
 * Ends the tour and records it as seen.
 *
 * Skipping counts as seen: someone who dismissed it made a decision, and
 * greeting them with it again on the next sign-in would override that.
 */
async function endTour() {
  tour.active = false;
  $('#tour-spotlight').hidden = true;
  $('#tour-bubble').hidden = true;

  await recordTourSeen();
}

/** Notes that the introduction has been offered, so it is offered once. */
async function recordTourSeen() {
  if (!me.user || me.user.tourSeen) return;

  try {
    await api('/me/tour', { method: 'PUT', body: JSON.stringify({ seen: true }) });
    me.user.tourSeen = true;
  } catch {
    // Worst case it is offered again next time, which is a small annoyance
    // and not worth an error message over.
  }
}

function wireTour() {
  $('#tour-next').addEventListener('click', advanceTour);

  $('#tour-back').addEventListener('click', async () => {
    if (tour.index > 0) {
      tour.index -= 1;
      await renderTourStep();
    }
  });

  $('#tour-end').addEventListener('click', endTour);

  // Escape is what people press to get out of a modal, and a tour that traps
  // someone is worse than no tour.
  document.addEventListener('keydown', (e) => {
    if (tour.active && e.key === 'Escape') endTour();
  });

  // The highlight is drawn from a measured rectangle, so it has to be redrawn
  // when the layout changes underneath it.
  for (const event of ['resize', 'scroll']) {
    window.addEventListener(event, () => {
      if (!tour.active) return;

      const node = $(tour.steps[tour.index].target);
      if (node) placeTour(node);
    }, { passive: true });
  }

  $('#tour-restart').addEventListener('click', startTour);
}

/**
 * The greeting, whichever kind applies to this person.
 *
 * One place rather than two calls at each site: a first sign-in is greeted and
 * offered the walk through, anybody who has been here before gets the short one,
 * and never both.
 */
async function greetAfterSignIn() {
  await maybeWelcome();
  await maybeWelcomeBack();
}

/**
 * Whether this person has already been greeted during this visit, marking them as
 * greeted if not.
 *
 * Per tab and gone when the tab is: exactly the lifetime of "this visit". Shared by
 * both greetings so they cannot both land - somebody who declined the walk through
 * and then reloaded was being welcomed back to a screen they had just arrived at.
 *
 * Private browsing can refuse storage outright. Then a greeting shows again on the
 * next load, which is a small annoyance and not worth failing over.
 */
function greetedThisVisit() {
  if (!me.user) return true;

  const key = `gtr_greeted_${me.user.id}`;

  try {
    if (sessionStorage.getItem(key)) return true;

    sessionStorage.setItem(key, '1');
  } catch {
    return false;
  }

  return false;
}

/**
 * Greets somebody on their first sign-in, and offers the walk through.
 *
 * Never while the setup wizard is up: being walked through the application by
 * two things at once is worse than either alone, and the wizard is the one
 * that has to happen first.
 *
 * Not for the built-in administrator either. That account arrives at the setup
 * wizard, which is its own introduction and covers the screens it actually uses;
 * a tour of booking time and reading an overtime balance would be a tour of
 * somebody else's job. The card under My account still starts it on request.
 */
async function maybeWelcome() {
  if (!me.user || me.user.tourSeen) return;
  if (!$('#setup-wizard').hidden) return;
  if (me.user.mustChangePassword) return;
  if (isSystemAdmin()) return;
  if (greetedThisVisit()) return;

  showWelcome();
}

/** Fills in the greeting and puts it up. */
function showWelcome() {
  const overlay = $('#welcome-overlay');
  if (!overlay) return;

  $('#welcome-title').textContent = me.user?.name
    ? t('welcome.hello', 'Welcome, {0}').replace('{0}', me.user.name)
    : t('welcome.helloPlain', 'Welcome');

  $('#welcome-text').textContent = t('welcome.text',
    'This is where your working time is recorded. A short walk through it takes '
    + 'about a minute, and you can start it again at any time under My account.');

  // What the application is for, in the order somebody meets it. Only the points
  // this person can actually act on: a list that promises something somebody cannot
  // do is worse than a shorter list.
  //
  // There was a fourth, about reviewing and approving what your people submit,
  // asked of a permission the application had already stopped defining - so can()
  // answered no for everybody, and yes for everybody with authentication switched
  // off, where it greeted the first user with a job that does not exist.
  const points = [
    can('timesheets:write:own')
      && t('welcome.point.book', 'Book hours by hand, or run a stopwatch and let it book them.'),
    can('timesheets:read:own')
      && t('welcome.point.see', 'See your month in a calendar, and your own figures as charts.'),
    // The daily target is the one part of "My account" that only somebody who works
    // here has, so it is named separately. The greeting used to promise it to
    // everybody, including the account that records no time at all.
    can('settings:write:own')
      && t('welcome.point.target', 'Set your own daily target and ceiling under My account.'),
    can('users:write')
      && t('welcome.point.administer',
        'Add the people who work here, and decide what each of them may do.'),
    t('welcome.point.account',
      'Choose your timezone, your language and a second factor under My account.'),
  ].filter(Boolean);

  $('#welcome-points').replaceChildren(...points.map((text) => el('li', { text })));

  // Every step of the walk points at a control, and a step whose control this person
  // cannot reach is dropped - so for an account that only administers there is nothing
  // left to walk through. Offering it anyway would open an empty tour, which is a
  // worse first impression than not offering one.
  const walkable = applicableTourSteps().length > 0;
  const tourButton = $('#welcome-tour');
  const skipButton = $('#welcome-skip');

  tourButton.hidden = !walkable;

  // With nothing to show, the remaining button is the only way out, so it stops being
  // the quiet second choice and says what it does.
  skipButton.classList.toggle('secondary', walkable);
  setLeadingText(skipButton, walkable
    ? t('welcome.skip', 'Not now')
    : t('welcome.begin', 'Get started'));

  overlay.hidden = false;
  (walkable ? tourButton : skipButton).focus();
}

/** Takes the greeting down, and records that it has been seen. */
async function dismissWelcome({ thenTour }) {
  $('#welcome-overlay').hidden = true;

  if (thenTour) {
    await startTour();

    return;
  }

  // Declining counts as seen, the same way skipping the tour does: somebody who
  // said "not now" made a decision, and asking again next time overrides it.
  await recordTourSeen();
}

function wireWelcome() {
  $('#welcome-tour').addEventListener('click', () => dismissWelcome({ thenTour: true }));
  $('#welcome-skip').addEventListener('click', () => dismissWelcome({ thenTour: false }));

  // Escape is what people press to get out of a dialog.
  document.addEventListener('keydown', (e) => {
    if (e.key === 'Escape' && !$('#welcome-overlay').hidden) {
      dismissWelcome({ thenTour: false });
    }
  });

  $('#welcome-back-close').addEventListener('click', () => {
    $('#welcome-back').hidden = true;
  });
}

/**
 * Greets somebody who has been here before, once per browser session.
 *
 * Per visit rather than per page load: a reload is not an arrival, and a greeting
 * that reappears every time is something to close rather than read. Says what they
 * would otherwise have to go and look up - what today already has on it, and
 * whether they left a stopwatch running.
 */
async function maybeWelcomeBack() {
  const card = $('#welcome-back');
  if (!card || !me.user || !me.user.tourSeen) return;
  if (!$('#setup-wizard').hidden || me.user.mustChangePassword) return;
  if (!can('timesheets:read:own')) return;
  if (greetedThisVisit()) return;

  $('#welcome-back-title').textContent = me.user.name
    ? t('welcome.backName', 'Welcome back, {0}').replace('{0}', me.user.name)
    : t('welcome.back', 'Welcome back');

  $('#welcome-back-text').textContent = await welcomeBackDetail();
  card.hidden = false;
}

/** What today looks like, in one sentence. */
async function welcomeBackDetail() {
  const today = todayISO();

  try {
    const running = await api('/me/timer');

    if (running?.running) {
      return t('welcome.timerRunning',
        'A stopwatch is still running. Stop it on this screen to book the time.');
    }
  } catch {
    // Not worth a word: the greeting is a courtesy, and the screen below it
    // reports anything that is actually wrong.
  }

  try {
    const entries = (await api(`/timesheets?from=${today}&to=${today}`))?.items ?? [];
    const mine = entries.filter((entry) => entry.userId === me.user.id);
    const hours = mine.reduce((sum, entry) => sum + entry.durationHours, 0);

    if (hours > 0) {
      return t('welcome.todayHours', '{0} h booked today so far.')
        .replace('{0}', hours.toFixed(2));
    }

    return t('welcome.todayNothing', 'Nothing booked today yet.');
  } catch {
    return '';
  }
}

// ------------------------------------------------------------ setup wizard

/**
 * What each step says and does.
 *
 * The server decides which steps exist and which are done; this only supplies
 * the words and the form. Keeping the two apart means a step cannot quietly
 * complete because the interface thinks it should have.
 *
 * `fields` renders inside the wizard for the things worth settling on the spot.
 * `tab` sends the administrator to the real screen for the rest - duplicating
 * the LDAP form here would mean two forms to keep in step, and the second one
 * always drifts.
 */
const SETUP_STEPS = {
  password: {
    title: () => t('setup.password.title', 'Change the administrator password'),
    text: () => t('setup.password.text',
      'This account still has the password from the documentation, which anyone can look up. '
      + 'Nothing else can be used until it is changed.'),
    fields: () => [
      passwordField('currentPassword', t('settings.currentPassword', 'Current password'), 'changeme123'),
      passwordField('newPassword', t('settings.newPassword', 'New password'), ''),
    ],
    submit: async (values) => {
      await api('/me/password', {
        method: 'PUT',
        body: JSON.stringify({
          currentPassword: values.currentPassword,
          newPassword: values.newPassword,
        }),
      });

      // The session survives now: the server ends the other devices and keeps
      // this one, so the wizard carries straight on to its next step instead of
      // dropping the person at a sign-in screen half way through setting up.
      return {};
    },
  },

  timezone: {
    title: () => t('setup.timezone.title', 'Choose the timezone'),
    text: () => t('setup.timezone.text',
      'This decides which calendar day a booking falls on. Left unset it is UTC, '
      + 'which puts evening bookings on the wrong day for most of the world.'),
    fields: () => {
      const select = el('select', { name: 'timezone' });
      fillTimezoneSelect(select, guessTimezone());

      return [el('label', { text: t('tz.timezone', 'Timezone') }, select)];
    },
    submit: async (values) => {
      await api('/settings/timezone', {
        method: 'PUT', body: JSON.stringify({ timezone: values.timezone }),
      });
    },
  },

  branding: {
    title: () => t('setup.branding.title', 'Name this installation'),
    text: () => t('setup.branding.text',
      'The title appears in the browser tab and the header. Optional, but it is what tells '
      + 'a test instance apart from the real one at a glance.'),
    fields: () => [
      el('label', { text: t('admin.title', 'Title (browser tab and header)') },
        el('input', { type: 'text', name: 'title', maxlength: '80' })),
    ],
    submit: async (values) => {
      if (!values.title?.trim()) return;

      const branding = await api('/branding');
      await api('/settings/branding', {
        method: 'PUT', body: JSON.stringify({ ...branding, title: values.title.trim() }),
      });
      await loadBranding();
    },
  },

  directory: {
    title: () => t('setup.directory.title', 'Connect a directory (optional)'),
    text: () => t('setup.directory.text',
      'With LDAP or Active Directory connected, people sign in with the account they already have '
      + 'and no passwords are kept here. Skip this to manage accounts locally.'),
    tab: 'admin',
  },
};

/** A labelled password field for the wizard's own forms. */
function passwordField(name, label, placeholder) {
  return el('label', { text: label },
    el('input', { type: 'password', name, placeholder, autocomplete: 'off' }));
}

/** The browser's own zone, as the opening suggestion. */
function guessTimezone() {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC';
  } catch {
    return 'UTC';
  }
}

/** Server state, where in it we are, and what this process is running on. */
let setup = { state: null, index: 0 };

/** Loads the wizard state and shows it if anything is outstanding. */
async function loadSetup() {
  if (!isSystemAdmin()) {
    $('#setup-wizard').hidden = true;

    return;
  }

  try {
    setup.state = await api('/setup');
  } catch {
    // Not fatal: an installation is usable without the wizard, and failing
    // here would block the interface over a hint.
    $('#setup-wizard').hidden = true;

    return;
  }

  if (!shouldShowSetup(setup.state)) {
    $('#setup-wizard').hidden = true;

    return;
  }

  // Open on the first thing still to do rather than always at step one, so
  // coming back does not mean clicking through what is already settled.
  setup.index = Math.max(0, setup.state.steps.findIndex((s) => !s.done));
  renderSetup();
  $('#setup-wizard').hidden = false;
}

/**
 * Mirrors the server's rule: dismissing the wizard settles the optional steps,
 * not the required ones. Anything required and undone brings it back.
 */
function shouldShowSetup(state) {
  const outstanding = state.steps.some((s) => s.required && !s.done);

  return !state.completed || outstanding;
}

function renderSetup() {
  const { state, index } = setup;
  const step = state.steps[index];
  const definition = SETUP_STEPS[step.id];

  const done = state.steps.filter((s) => s.done).length;
  $('#setup-progress').textContent = t('setup.progress', 'Step {n} of {total} — {done} done')
    .replace('{n}', String(index + 1))
    .replace('{total}', String(state.steps.length))
    .replace('{done}', String(done));

  $('#setup-steps').replaceChildren(...state.steps.map((s, i) => el('li', {
    class: [s.done ? 'done' : '', s.required ? 'required' : '', i === index ? 'current' : '']
      .filter(Boolean).join(' '),
    text: SETUP_STEPS[s.id]?.title() ?? s.id,
  })));

  $('#setup-step-title').textContent = definition.title();
  $('#setup-step-text').textContent = definition.text();
  $('#setup-error').hidden = true;

  const fields = el('div', {});
  if (!step.done && definition.fields) fields.append(...definition.fields());
  $('#setup-step-fields').replaceChildren(fields);

  // The wizard builds its fields as it goes, so the one-off pass at start-up has
  // already been and gone by the time a password field of its own exists.
  enhancePasswordFields($('#setup-step-fields'));

  $('#setup-back').disabled = index === 0;

  const last = index === state.steps.length - 1;
  $('#setup-next').textContent = last
    ? t('setup.finish', 'Finish')
    : t('setup.next', 'Next');

  // A required step cannot be skipped; that is what "required" means here.
  $('#setup-skip').hidden = step.required && !step.done;
  $('#setup-skip').textContent = definition.tab
    ? t('setup.openSettings', 'Open Settings instead')
    : t('setup.skip', 'Skip this step');
}

function setupError(message) {
  const box = $('#setup-error');
  box.textContent = message;
  box.hidden = false;
}

/** Reads the wizard's own fields. */
function setupValues() {
  const values = {};
  for (const input of $$('#setup-step-fields input, #setup-step-fields select')) {
    values[input.name] = input.value;
  }

  return values;
}

async function advanceSetup() {
  const step = setup.state.steps[setup.index];
  const definition = SETUP_STEPS[step.id];

  // A required step with no form of its own - the database question - would
  // otherwise be walked straight past by the Next button, which is exactly the
  // skipping this ordering exists to prevent.
  if (step.required && !step.done && !definition.submit) {
    setupError(t('setup.decideFirst', 'Please settle this step before continuing.'));

    return;
  }

  if (!step.done && definition.submit) {
    try {
      // No step signs the person out any more. The password step used to: the
      // server ended every session on a change, so the wizard had nowhere to go
      // but the sign-in screen, half way through setting the installation up. It
      // keeps this session now, so the password step advances like every other.
      await definition.submit(setupValues());
    } catch (err) {
      setupError(err.message);

      return;
    }
  }

  await nextSetupStep();
}

/** Re-reads the state, so a step completed elsewhere is reflected too. */
async function nextSetupStep() {
  const wasLast = setup.index >= setup.state.steps.length - 1;
  const current = setup.state.steps[setup.index];

  setup.state = await api('/setup');

  // The server is the judge of whether the step took. A submit that returned
  // without error but left the step outstanding - a blank title, a connection
  // that was not actually saved - must not be walked past.
  const after = setup.state.steps[setup.index];

  if (current.required && !after.done) {
    setupError(t('setup.decideFirst', 'Please settle this step before continuing.'));
    renderSetup();

    return;
  }

  if (wasLast) {
    await finishSetup();

    return;
  }

  setup.index += 1;
  renderSetup();
}

async function finishSetup() {
  try {
    await api('/setup/complete', { method: 'POST' });
  } catch (err) {
    setupError(err.message);

    return;
  }

  $('#setup-wizard').hidden = true;
  toast(t('setup.done', 'Setup complete.'), 'ok');
  await refreshAll();
}

function wireSetup() {
  $('#setup-next').addEventListener('click', advanceSetup);

  $('#setup-back').addEventListener('click', () => {
    if (setup.index > 0) {
      setup.index -= 1;
      renderSetup();
    }
  });

  $('#setup-skip').addEventListener('click', async () => {
    const step = setup.state.steps[setup.index];
    const definition = SETUP_STEPS[step.id];

    if (definition.tab) {
      // Sends them to the real screen rather than a copy of it, and gets out
      // of the way so they can use it.
      $('#setup-wizard').hidden = true;
      switchView(definition.tab);

      return;
    }

    await nextSetupStep();
  });

  $('#setup-dismiss').addEventListener('click', async () => {
    const outstanding = setup.state.steps.filter((s) => s.required && !s.done);

    if (outstanding.length > 0) {
      setupError(t('setup.stillRequired',
        'These steps have to be settled first; the wizard will keep coming back until they are.'));

      return;
    }

    await finishSetup();
  });
}

// ------------------------------------------------------- operation & limits

/** The fields of the operational form, in the order they appear. */
const OPERATIONAL_FIELDS = [
  'sessionLifetimeHours', 'maxDailyHours', 'rateLimit',
  'rateLimitWindowSeconds', 'ldapSyncMaxDeleteRatio',
];

/**
 * Fills the operational form.
 *
 * An overridden value goes in the field; anything still following the
 * configuration file leaves the field empty and shows that file's value as the
 * placeholder. Prefilling every field instead would turn "not set here" into a
 * deliberate-looking choice the moment anyone pressed Save.
 */
function fillOperationalForm(data) {
  const form = $('#form-operational');

  for (const field of OPERATIONAL_FIELDS) {
    const input = form.elements[field];

    // A field this list names and the markup does not is a mistake, but it must
    // not be a silent one that takes the rest of the screen with it: this loop
    // runs while the administration screen is being built, and a throw here left
    // the telemetry and directory cards empty with nothing said about why.
    if (!input) {
      console.warn(`operational field ${field} is not in the form`);

      continue;
    }

    const override = data.configured?.[field];

    input.value = override ?? '';
    input.placeholder = String(data.defaults?.[field] ?? '');
  }

  const effective = data.effective ?? {};
  $('#operational-effective').textContent = `${t('ops.effective', 'Currently in force')}: `
    + `${t('ops.sessionShort', 'session')} ${effective.sessionLifetimeHours} h, `
    + `${t('ops.maxShort', 'max/day')} ${effective.maxDailyHours} h, `
    + `${t('ops.rateShort', 'rate')} ${effective.rateLimit}/${effective.rateLimitWindowSeconds} s, `
    + `${t('ops.ratioShort', 'delete limit')} ${effective.ldapSyncMaxDeleteRatio}`;
}

/** Reads the form, omitting empty fields so they keep following the file. */
function operationalPayload() {
  const form = $('#form-operational');
  const body = {};

  for (const field of OPERATIONAL_FIELDS) {
    const input = form.elements[field];
    if (!input) continue;

    const raw = input.value.trim();
    if (raw !== '') body[field] = Number(raw);
  }

  return body;
}

function wireOperational() {
  $('#form-operational').addEventListener('submit', (e) => {
    e.preventDefault();
    mutate(
      () => api('/settings/operational', {
        method: 'PUT', body: JSON.stringify(operationalPayload()),
      }),
      t('ops.saved', 'Limits saved'),
      loadOperational);
  });

  $('#operational-reset').addEventListener('click', () => {
    mutate(
      () => api('/settings/operational', { method: 'PUT', body: JSON.stringify({}) }),
      t('ops.reset.done', 'All values follow the configuration file again'),
      loadOperational);
  });
}

async function loadOperational() {
  fillOperationalForm(await api('/settings/operational'));
}

// ----------------------------------------------------------------- restart

/**
 * What each pending setting is called on screen.
 *
 * The server names the setting and leaves the wording here, so the list is
 * translated like everything else rather than arriving as a sentence in one
 * language.
 */
function pendingLabel(setting) {
  switch (setting) {
    case 'logLevel': return t('tel.logLevel', 'Log level');
    case 'metrics': return t('tel.metrics', 'Metrics endpoint');
    case 'traceExporter': return t('tel.exporter', 'Trace exporter');
    case 'tracerUrl': return t('tel.url', 'Collector');
    case 'database': return t('admin.database', 'Database connection');
    case 'directorySchedule': return t('sync.scheduleShort', 'Directory schedule');
    default: return setting;
  }
}

/** What an empty value reads as - "" would look like a rendering fault. */
function pendingValue(value) {
  return value === '' ? t('restart.none', 'none') : value;
}

/**
 * Shows what is waiting for a restart, and offers one.
 *
 * The comparison is the server's: it knows what this process started with and
 * what is stored, and working it out again here would be a second place for the
 * answer to be wrong in.
 */
async function loadRestart() {
  const card = $('#restart-card');
  if (!card) return;

  const state = await api('/settings/restart');
  restartStartedAt = state.startedAt ?? '';

  const pending = state.pending ?? [];
  // Normally the card is only there when something is waiting for a restart.
  // Where restarting is not possible at all it stays, whether anything is pending
  // or not: that is a standing property of this installation, and finding it out
  // by pressing a button that never appears is not finding it out.
  card.hidden = pending.length === 0 && state.supported;

  const list = $('#restart-pending');
  list.replaceChildren(...pending.map((change) => el('li', {},
    el('strong', { text: pendingLabel(change.setting) }),
    el('span', { class: 'from', text: `: ${pendingValue(change.running)} → ` }),
    el('strong', { text: pendingValue(change.stored) }))));

  // Offered only where pressing it would actually work. Where it would not, the
  // reason is shown instead of a button that fails on click.
  // The hint promises a list of saved changes and the list follows it, so both go
  // when there are none. Where restarting is impossible the card is on screen
  // permanently, and a standing "these are saved and waiting:" above nothing reads
  // as a rendering fault rather than as the explanation it sits above.
  const waiting = pending.length > 0;
  $('#restart-hint').hidden = !waiting;
  $('#restart-pending').hidden = !waiting;

  $('#restart-now').hidden = !state.supported;
  $('#restart-unsupported').hidden = state.supported;

  // Translated here rather than shown as it arrives: the server writes this in
  // English at the point the limitation is decided, which is right for a log and
  // wrong for the person reading the screen.
  //
  // Keyed on the code rather than on one sentence for every refusal. There is more
  // than one: Windows has no execve, and on unix the running binary can fail to be
  // located. A single translated sentence would tell a Linux reader they are on
  // Windows and throw away the diagnostic that named the real fault. The server's
  // own wording is the fallback, so a reason nobody has translated still says
  // something true.
  $('#restart-unsupported').textContent = state.supported
    ? ''
    : t(`restart.unsupported.${state.reasonCode || 'other'}`, state.reason ?? '');
}

/** The identity of the running process, to tell a restart from a hiccup. */
let restartStartedAt = '';

/** How long to wait for the application to come back before giving up on it. */
const RESTART_TIMEOUT_MS = 60000;

/**
 * Waits for a different process to answer.
 *
 * Polling for "does it respond" is not enough: replacing the process image takes
 * milliseconds, and a poll that misses that gap would report success without
 * anything having happened. The start time changing is what proves it.
 */
async function waitForRestart(previousStartedAt) {
  const deadline = Date.now() + RESTART_TIMEOUT_MS;

  while (Date.now() < deadline) {
    await new Promise((resolve) => { setTimeout(resolve, 1000); });

    try {
      const state = await api('/settings/restart');
      if (state.startedAt && state.startedAt !== previousStartedAt) return true;
    } catch {
      // Expected while it is down: the connection is refused, or the session
      // has not been read back out of the database yet.
    }
  }

  return false;
}

function wireRestart() {
  const button = $('#restart-now');
  if (!button) return;

  button.addEventListener('click', async () => {
    const proceed = await confirmDialog({
      title: t('restart.title', 'Restart'),
      text: t('restart.confirm',
        'Restart the application? Anyone working in it will have to reload the page.'),
      confirmLabel: t('restart.now', 'Restart now'),
    });

    if (!proceed) return;

    const overlay = $('#restart-overlay');
    const status = $('#restart-status');
    const previous = restartStartedAt;

    overlay.hidden = false;
    status.textContent = t('restart.waitingHint', 'Waiting for the application to come back …');

    try {
      await api('/settings/restart', { method: 'POST' });
    } catch (err) {
      overlay.hidden = true;
      toast(`${t('restart.failed', 'The restart could not be started')}: ${err.message}`, 'error');

      return;
    }

    if (await waitForRestart(previous)) {
      overlay.hidden = true;
      toast(t('restart.done', 'The application has restarted and the settings are in force.'), 'ok');
      await refreshAll();

      return;
    }

    // Not an error as such: it may still be coming back. Saying that is more
    // use than a spinner that never stops.
    overlay.hidden = true;
    toast(t('restart.slow',
      'The application has not answered yet. It may still be starting — reload the page in a moment.'),
    'error');
  });
}

// ------------------------------------------------------ revealing a password

/**
 * Draws the eye, with a slash that is shown only while the password is readable.
 *
 * Built here rather than written into the markup because the button itself is,
 * and inline rather than fetched because the Content-Security-Policy allows no
 * external origin at all. createElementNS, not createElement: an <svg> built in
 * the HTML namespace parses without complaint and renders nothing.
 */
function passwordToggleIcon() {
  const ns = 'http://www.w3.org/2000/svg';
  const svg = document.createElementNS(ns, 'svg');

  svg.setAttribute('viewBox', '0 0 24 24');
  svg.setAttribute('width', '18');
  svg.setAttribute('height', '18');
  svg.setAttribute('fill', 'none');
  svg.setAttribute('stroke', 'currentColor');
  svg.setAttribute('stroke-width', '2');
  svg.setAttribute('stroke-linecap', 'round');
  // The button already carries the label; the drawing would only repeat it.
  svg.setAttribute('aria-hidden', 'true');

  const outline = document.createElementNS(ns, 'path');
  outline.setAttribute('d', 'M2 12s3.6-6.5 10-6.5S22 12 22 12s-3.6 6.5-10 6.5S2 12 2 12z');

  const pupil = document.createElementNS(ns, 'circle');
  pupil.setAttribute('cx', '12');
  pupil.setAttribute('cy', '12');
  pupil.setAttribute('r', '3');

  const slash = document.createElementNS(ns, 'path');
  slash.setAttribute('d', 'M3 3l18 18');
  slash.setAttribute('class', 'password-slash');

  svg.append(outline, pupil, slash);

  return svg;
}

/**
 * Labels the button for what pressing it will do.
 *
 * The key is kept on the element as well as applied, so switching language
 * re-labels it through applyLanguage instead of leaving the last language's
 * word on a button nobody looks at twice.
 */
function labelPasswordToggle(button, revealed) {
  const key = revealed ? 'password.hide' : 'password.reveal';
  const label = revealed
    ? t('password.hide', 'Hide the password')
    : t('password.reveal', 'Show the password');

  button.dataset.i18nAria = key;
  button.setAttribute('aria-label', label);
  button.title = label;
  button.setAttribute('aria-pressed', String(revealed));
  button.classList.toggle('revealed', revealed);
}

/**
 * Gives every password field a button that reveals what was typed.
 *
 * Every field, found by selector rather than listed, so the next one added gets
 * this without anyone remembering to ask. It matters most where the value is not
 * a password being recalled but one being transcribed - a database or bind
 * password copied from somewhere else, where a silent typo is answered minutes
 * later by "connection refused" and nothing on screen says which character was
 * wrong.
 *
 * The field goes back to being hidden when the form is submitted or the page is
 * left, because the browser is where it stays visible otherwise: nothing here
 * re-renders these inputs.
 */
function wirePasswordReveal(root = document) {
  for (const input of root.querySelectorAll('input[type="password"]')) {
    // Running twice would nest a second wrapper and a second button.
    if (input.parentElement?.classList.contains('password-field')) continue;

    const field = document.createElement('span');
    field.className = 'password-field';

    input.replaceWith(field);
    field.append(input);

    const button = document.createElement('button');
    // Not a submit: inside a form, a button with no type submits it, so the
    // first attempt to look at a password would send the form instead.
    button.type = 'button';
    button.className = 'password-toggle';
    button.append(passwordToggleIcon());
    labelPasswordToggle(button, false);

    button.addEventListener('click', () => {
      const revealed = input.type === 'password';
      input.type = revealed ? 'text' : 'password';
      labelPasswordToggle(button, revealed);

      // The caret goes back where it was: the type change moves focus off the
      // field in some browsers, and continuing to type is the normal next act.
      input.focus();
    });

    // Submitting is the end of looking at it. Left revealed, the value would
    // still be on screen behind whatever the save put there.
    input.form?.addEventListener('submit', () => {
      input.type = 'password';
      labelPasswordToggle(button, false);
    });

    field.append(button);
  }
}

// ------------------------------------------------------------------ charts

/**
 * Draws a bar chart, by hand.
 *
 * No chart library, and not for want of one: the Content-Security-Policy allows
 * no external origin at all and the assets are embedded, so anything fetched from
 * a CDN would simply be blocked. That leaves SVG built in the page - and
 * createElementNS rather than createElement, because an <svg> built in the HTML
 * namespace parses without complaint and renders nothing.
 *
 * Horizontal bars with the label beside each one, which is what survives a long
 * project name and a narrow phone. A vertical chart with rotated labels reads
 * badly at both ends.
 */
const SVG_NS = 'http://www.w3.org/2000/svg';

function svg(tag, attributes = {}) {
  const node = document.createElementNS(SVG_NS, tag);

  for (const [name, value] of Object.entries(attributes)) {
    node.setAttribute(name, String(value));
  }

  return node;
}

/**
 * Renders bars into a container.
 *
 * bars is [{label, value, title}]. The scale runs from zero to the largest value,
 * because a bar chart that starts anywhere else exaggerates every difference on
 * it - and these are hours, where twice as long should look twice as long.
 */
function drawBarChart(container, bars, formatValue) {
  container.replaceChildren();

  if (bars.length === 0) return;

  const rowHeight = 22;
  const gap = 4;
  const labelWidth = 118;
  const valueWidth = 62;
  const width = 640;
  const height = bars.length * (rowHeight + gap);
  const trackWidth = width - labelWidth - valueWidth;

  const chart = svg('svg', {
    viewBox: `0 0 ${width} ${height}`,
    // Scales to its container's width while keeping the row height readable.
    width: '100%',
    height,
    role: 'img',
  });

  const largest = Math.max(...bars.map((bar) => bar.value), 0);

  bars.forEach((bar, index) => {
    const y = index * (rowHeight + gap);

    const label = svg('text', {
      x: 0,
      y: y + rowHeight * 0.72,
      class: 'chart-label',
    });
    label.textContent = bar.label;
    chart.append(label);

    // The track, so an empty day is still a row rather than nothing at all.
    chart.append(svg('rect', {
      x: labelWidth, y, width: trackWidth, height: rowHeight, rx: 4, class: 'chart-track',
    }));

    if (bar.value > 0 && largest > 0) {
      // Zero-based, and at least a sliver wide so a very small value is visible
      // rather than looking like nothing was recorded.
      const barWidth = Math.max(2, (bar.value / largest) * trackWidth);

      const rect = svg('rect', {
        x: labelWidth, y, width: barWidth, height: rowHeight, rx: 4, class: 'chart-bar',
      });

      // The exact figure on hover, since the bar is a comparison and not a
      // readout.
      const title = svg('title');
      title.textContent = bar.title ?? `${bar.label}: ${formatValue(bar.value)}`;
      rect.append(title);

      chart.append(rect);
    }

    const value = svg('text', {
      x: width,
      y: y + rowHeight * 0.72,
      class: 'chart-value',
      'text-anchor': 'end',
    });
    value.textContent = formatValue(bar.value);
    chart.append(value);
  });

  container.append(chart);
}

/** The default range: the current month, in whatever zone applies to the user. */
function defaultStatisticsRange() {
  const today = todayISO();
  const firstOfMonth = `${today.slice(0, 7)}-01`;

  return { from: firstOfMonth, to: today };
}

async function loadStatistics() {
  const card = $('#statistics-card');
  if (!card || !can('timesheets:read:own')) return;

  const range = defaultStatisticsRange();

  if (!$('#statistics-from').value) $('#statistics-from').value = range.from;
  if (!$('#statistics-to').value) $('#statistics-to').value = range.to;

  const params = new URLSearchParams({
    from: $('#statistics-from').value,
    to: $('#statistics-to').value,
  });

  const stats = await api(`/me/statistics?${params}`);

  $('#statistics-total').textContent =
    `${t('stats.total', 'Total')}: ${fmtHours(stats.totalHours ?? 0)}`;

  const days = stats.days ?? [];
  const projects = stats.projects ?? [];

  // Empty days are drawn as empty rows on purpose - a chart of only the days that
  // have entries shows a full week where there were two working days.
  drawBarChart($('#chart-days'),
    days.map((day) => ({ label: day.date, value: day.hours })),
    fmtHours);

  drawBarChart($('#chart-projects'),
    projects.map((project) => ({
      // A project with no name is one that has been deleted since; the hours it
      // holds still count, so it is shown rather than dropped.
      label: project.projectId
        ? (project.name || t('stats.deletedProject', 'deleted project'))
        : t('stats.noProject', 'no project'),
      value: project.hours,
    })),
    fmtHours);

  $('#statistics-empty').hidden = (stats.totalHours ?? 0) > 0;
}

/**
 * The spreadsheet card: exporting what is on screen, and importing a file.
 *
 * The import is deliberately two steps. A file assembled by hand is wrong more
 * often than it is right, and the first thing somebody needs is to be shown what
 * their file would do - which rows would be written, which would not, and why -
 * before anything is.
 */

/** The filters the entry list is showing, so the export matches the screen. */
function timesheetFilterQuery() {
  const params = new URLSearchParams();

  const projectId = $('#filter-ts-project')?.value;

  if (projectId) params.set('projectId', projectId);

  return params.toString() ? `?${params}` : '';
}

/**
 * Downloads the export.
 *
 * Through fetch and a blob rather than by pointing the browser at the URL: the
 * request needs the session cookie and a sensible filename, and the server cannot
 * set Content-Disposition - GoFr owns the response headers. Naming it here is
 * better anyway, because this is where the period being looked at is known.
 */
async function exportWorkbook() {
  await downloadSheet(`/timesheets/export${timesheetFilterQuery()}`,
    t('wb.filename', 'time-entries'));
}

/**
 * Fetches an export and saves it under a readable name.
 *
 * The language travels with the request, so the headings in the file are the ones
 * on the screen of whoever asked for it. It is a query parameter rather than the
 * account's setting: the file is about to be opened by the person looking at this
 * screen, in the language they are reading it in.
 */
async function downloadSheet(path, name) {
  const separator = path.includes('?') ? '&' : '?';
  const url = `${API}${path}${separator}lang=${encodeURIComponent(activeLanguage())}`;

  const res = await fetch(url, { credentials: 'same-origin' });

  if (!res.ok) {
    let body = null;
    try {
      body = await res.json();
    } catch {
      // An error page rather than JSON; the status carries the meaning.
    }

    throw new Error(errorMessage(body) || `${t('msg.error', 'Error')} ${res.status}`);
  }

  const blob = await res.blob();
  const objectURL = URL.createObjectURL(blob);

  const link = el('a', { href: objectURL, download: `${name}-${todayISO()}.xlsx` });
  document.body.append(link);
  link.click();
  link.remove();

  // Released once the browser has taken it; a blob left behind holds the whole
  // file in memory for as long as the page is open.
  URL.revokeObjectURL(objectURL);
}

/** Sends the chosen file, either to look or to write. */
async function sendWorkbook(dryRun) {
  const input = $('#wb-file');
  const file = input?.files?.[0];

  if (!file) {
    throw new Error(t('wb.noFile', 'Choose a file first.'));
  }

  const body = new FormData();
  body.append('file', file);
  body.append('dryRun', dryRun ? 'true' : 'false');

  // No Content-Type of our own: the browser has to set it, because only it knows
  // the multipart boundary it generated.
  const res = await fetch(`${API}/timesheets/import`, {
    method: 'POST',
    credentials: 'same-origin',
    headers: { 'X-CSRF-Token': readCookie('gtr_csrf') },
    body,
  });

  let payload = null;
  try {
    payload = await res.json();
  } catch {
    // Falls through to the status below.
  }

  if (!res.ok) {
    const err = new Error(errorMessage(payload) || `${t('msg.error', 'Error')} ${res.status}`);
    err.status = res.status;
    throw err;
  }

  return payload?.data ?? null;
}

/** Shows what the file would do, row by row. */
function renderWorkbookPreview(result) {
  const rows = (result?.rows ?? []).map((row) => el('tr', { class: row.problem ? 'rejected' : '' },
    el('td', { class: 'num', text: String(row.row) }),
    el('td', { text: row.date ? fmtDate(row.date) : '–' }),
    el('td', { text: row.user || '–' }),
    el('td', { text: row.project || t('ts.noProject', 'no project') }),
    el('td', { class: 'num', text: row.hours ? row.hours.toFixed(2) : '–' }),
    el('td', { text: row.description || '–' }),
    el('td', { class: row.problem ? 'minus' : 'muted', text: row.problem || '✓' }),
  ));

  fillTable($('#table-workbook tbody'), rows, 7, t('wb.empty', 'The file has no entries in it.'));
  $('#wb-preview-wrap').hidden = false;

  const writable = result?.writable ?? 0;
  const rejected = result?.rejected ?? 0;

  $('#wb-summary').textContent = rejected > 0
    ? t('wb.someRejected', '{0} of {1} rows can be imported. Nothing is written while any row is refused.')
      .replace('{0}', String(writable)).replace('{1}', String(writable + rejected))
    : t('wb.allReady', 'All {0} rows can be imported.').replace('{0}', String(writable));

  // The import button only where it would do something: offering it for a file
  // that would be refused is offering a failure.
  $('#wb-import').hidden = rejected > 0 || writable === 0;
}

/** Puts the card back to its resting state. */
function resetWorkbookCard() {
  const input = $('#wb-file');
  if (input) input.value = '';

  $('#wb-preview').hidden = true;
  $('#wb-import').hidden = true;
  $('#wb-clear').hidden = true;
  $('#wb-preview-wrap').hidden = true;
  $('#wb-summary').textContent = '';
  $('#table-workbook tbody').replaceChildren();
}

function wireWorkbook() {
  const card = $('#workbook-card');
  if (!card) return;

  $('#wb-export').addEventListener('click', () => mutate(
    exportWorkbook, t('wb.exported', 'Export saved'), null));

  $('#wb-file').addEventListener('change', () => {
    const chosen = Boolean($('#wb-file').files?.length);

    $('#wb-preview').hidden = !chosen;
    $('#wb-clear').hidden = !chosen;
    $('#wb-import').hidden = true;
    $('#wb-preview-wrap').hidden = true;
    $('#wb-summary').textContent = chosen
      ? t('wb.chosen', 'Check the file to see what it would do.')
      : '';
  });

  $('#wb-preview').addEventListener('click', () => mutate(
    () => sendWorkbook(true), null,
    (result) => renderWorkbookPreview(result)));

  $('#wb-import').addEventListener('click', () => mutate(
    () => sendWorkbook(false),
    t('wb.imported', 'The file was imported'),
    async (result) => {
      resetWorkbookCard();
      await reloadTimeViews();

      if (result?.imported) {
        toast(t('wb.importedCount', '{0} entries created.')
          .replace('{0}', String(result.imported)), 'ok');
      }
    }));

  $('#wb-clear').addEventListener('click', resetWorkbookCard);
}

// ------------------------------------------- export and import, table by table

/**
 * The tables that go in and out as a spreadsheet, beside the time entries.
 *
 * Each one exports what its own tab shows and imports into its own table, which is
 * the whole point: a single pair of buttons somewhere general would have meant
 * guessing which table somebody had in mind.
 *
 * Three tables are deliberately absent, for three different reasons. A token's
 * secret exists once, at the moment it is created, and is not in the database to
 * export. A passkey is bound to the device holding it and means nothing anywhere
 * else. A role is a set of permissions, and one spreadsheet cell holding
 * "projects:read,projects:write,..." is a list that has to be exactly right - a typo
 * in it removes a right silently rather than failing, where the role screen shows
 * every permission there is as a checkbox. None of the three is a table a
 * spreadsheet can carry honestly.
 */
const SHEET_CARDS = [
  {
    key: 'projects',
    path: '/projects',
    view: '#view-projects',
    read: 'projects:read',
    write: 'projects:write',
    reload: () => refreshAll(),
  },
  {
    key: 'users',
    path: '/users',
    view: '#view-users',
    read: 'users:read',
    write: 'users:write',
    reload: () => refreshAll(),
  },
];

/**
 * Builds the export/import card for one table.
 *
 * Built here rather than written into index.html three times over. The markup is
 * identical for every table - the only differences are the words and the endpoint -
 * and three copies of it in the page is three places to fix the next time one of
 * these buttons needs to change.
 *
 * The preview is driven by the answer: the server sends its column headings, already
 * translated, so this renders a file of projects and a file of people with the same
 * code and needs to know nothing about either.
 */
function buildSheetCard(spec) {
  const summary = el('p', { class: 'muted' });
  const headRow = el('tr');
  const body = el('tbody');

  const wrap = el('div', { class: 'table-wrap' },
    el('table', {}, el('thead', {}, headRow), body));
  wrap.hidden = true;

  const file = el('input', {
    type: 'file',
    accept: '.xlsx,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
  });

  const check = el('button', {
    type: 'button', class: 'secondary',
    'data-i18n': 'wb.preview', text: 'Check the file',
  });

  const write = el('button', { type: 'button', 'data-i18n': 'wb.import', text: 'Import' });

  const cancel = el('button', {
    type: 'button', class: 'link', 'data-i18n': 'action.cancelEdit', text: 'cancel',
  });

  for (const button of [check, write, cancel]) button.hidden = true;

  const reset = () => {
    file.value = '';

    for (const button of [check, write, cancel]) button.hidden = true;

    wrap.hidden = true;
    summary.textContent = '';
    headRow.replaceChildren();
    body.replaceChildren();
  };

  const send = async (dryRun) => {
    const chosen = file.files?.[0];
    if (!chosen) throw new Error(t('wb.noFile', 'Choose a file first.'));

    const form = new FormData();
    form.append('file', chosen);
    form.append('dryRun', dryRun ? 'true' : 'false');

    // No Content-Type of our own: only the browser knows the multipart boundary
    // it generated.
    const res = await fetch(`${API}${spec.path}/import`
      + `?lang=${encodeURIComponent(activeLanguage())}`, {
      method: 'POST',
      credentials: 'same-origin',
      headers: { 'X-CSRF-Token': readCookie('gtr_csrf') },
      body: form,
    });

    let payload = null;
    try {
      payload = await res.json();
    } catch {
      // Falls through to the status below.
    }

    if (!res.ok) {
      const failure = new Error(errorMessage(payload)
        || `${t('msg.error', 'Error')} ${res.status}`);
      failure.status = res.status;

      throw failure;
    }

    return payload?.data ?? null;
  };

  const preview = (result) => {
    const columns = result?.columns ?? [];

    headRow.replaceChildren(
      el('th', { text: t('wb.row', 'Row') }),
      ...columns.map((column) => el('th', { text: column })),
      el('th', { text: t('wb.problem', 'Problem') }),
    );

    const rows = (result?.rows ?? []).map((row) => el('tr',
      { class: row.problem ? 'rejected' : '' },
      el('td', { class: 'num', text: String(row.row) }),
      // Padded to the column count, because a row the file could not be read at
      // all has no cells and would otherwise leave the table ragged.
      ...columns.map((_, i) => el('td', { text: (row.cells ?? [])[i] || '–' })),
      el('td', { class: row.problem ? 'minus' : 'muted', text: row.problem || '✓' }),
    ));

    fillTable(body, rows, columns.length + 2, t('wb.empty', 'The file has no entries in it.'));
    wrap.hidden = false;

    const writable = result?.writable ?? 0;
    const rejected = result?.rejected ?? 0;

    summary.textContent = rejected > 0
      ? t('wb.someRejected',
        '{0} of {1} rows can be imported. Nothing is written while any row is refused.')
        .replace('{0}', String(writable)).replace('{1}', String(writable + rejected))
      : t('wb.allReady', 'All {0} rows can be imported.').replace('{0}', String(writable));

    // The import button only where it would do something: offering it for a file
    // that would be refused is offering a failure.
    write.hidden = rejected > 0 || writable === 0;
  };

  const exportButton = el('button', {
    type: 'button', class: 'secondary',
    'data-i18n': 'wb.export', text: 'Export as .xlsx',
    onclick: () => mutate(
      () => downloadSheet(`${spec.path}/export`, t(`sheet.${spec.key}.file`, spec.key)),
      t('wb.exported', 'Export saved'), null),
  });

  const picker = el('label', { class: 'file-pick', 'data-perm': spec.write },
    el('span', { 'data-i18n': 'wb.choose', text: 'Choose a file…' }), file);

  file.addEventListener('change', () => {
    const chosen = Boolean(file.files?.length);

    check.hidden = !chosen;
    cancel.hidden = !chosen;
    write.hidden = true;
    wrap.hidden = true;
    summary.textContent = chosen ? t('wb.chosen', 'Check the file to see what it would do.') : '';
  });

  check.addEventListener('click', () => mutate(() => send(true), null, preview));

  write.addEventListener('click', () => mutate(() => send(false),
    t('wb.imported', 'The file was imported'),
    async (result) => {
      reset();
      await spec.reload();

      if (result?.imported) {
        toast(t(`sheet.${spec.key}.done`, '{0} rows written.')
          .replace('{0}', String(result.imported)), 'ok');
      }
    }));

  cancel.addEventListener('click', reset);

  return el('div', { class: 'card', 'data-perm': spec.read },
    el('h2', { 'data-i18n': 'wb.title', text: 'Spreadsheet' }),
    el('p', {
      class: 'muted',
      'data-i18n': `sheet.${spec.key}.text`,
      text: SHEET_TEXTS[spec.key],
    }),
    el('div', { class: 'row' }, exportButton, picker, check, write, cancel),
    summary,
    wrap);
}

/**
 * What each card says it does, in English, which is the source language.
 *
 * Here rather than in the markup because the card is built rather than written, and
 * the paragraph is the one part of it that differs per table - each import behaves
 * differently enough that a shared sentence would be wrong for at least one of
 * them.
 */
const SHEET_TEXTS = {
  projects: 'Exports the projects and categories you can see. An import matches rows '
    + 'by name: a name that already exists is updated, a new one is created. Rows '
    + 'marked as a category become your own private ones. An empty cell leaves that '
    + 'value alone, so nothing is lost by a column somebody deleted.',
  users: 'Exports every account: name, email, role, and whether its password lives in '
    + 'the directory. An import changes the name and the role, matched on the email '
    + 'address, and does not create accounts - a new one needs a password, which does '
    + 'not belong in a spreadsheet. Working times and time zones are not here: they '
    + 'belong to whoever they are about, who sets them under My account.',
};

function wireSheetCards() {
  for (const spec of SHEET_CARDS) {
    const view = $(spec.view);
    if (!view) continue;

    view.append(buildSheetCard(spec));
  }
}

function wireStatistics() {
  const button = $('#statistics-load');
  if (!button) return;

  button.addEventListener('click', () => mutate(loadStatistics, null, null));
}

// ------------------------------------------------------------------ stopwatch

/** The running clock, or null. Kept so the display can tick without asking. */
let runningTimer = null;

/** The interval that ticks the display, so it can be stopped when nothing runs. */
let timerTick = null;

/**
 * Renders the clock's state.
 *
 * The elapsed time is counted here from the start instant rather than polled,
 * because a request per second to be told the same thing is a request per second.
 * The server sends its own elapsed figure too, and that is what the entry will
 * record - so a browser with a wrong clock shows a slightly wrong number here and
 * still books the right one.
 */
function renderTimer() {
  const card = $('#timer-card');
  if (!card) return;

  const running = runningTimer !== null;

  $('#timer-start').hidden = running;
  $('#timer-stop').hidden = !running;
  $('#timer-discard').hidden = !running;
  $('#timer-project').disabled = running;
  $('#timer-description').disabled = running;

  if (!running) {
    $('#timer-elapsed').textContent = '';

    if (timerTick) {
      clearInterval(timerTick);
      timerTick = null;
    }

    return;
  }

  const started = new Date(runningTimer.startedAt).getTime();

  const paint = () => {
    const seconds = Math.max(0, Math.floor((Date.now() - started) / 1000));
    const hh = String(Math.floor(seconds / 3600)).padStart(2, '0');
    const mm = String(Math.floor((seconds % 3600) / 60)).padStart(2, '0');
    const ss = String(seconds % 60).padStart(2, '0');

    $('#timer-elapsed').textContent = `${hh}:${mm}:${ss}`;
  };

  paint();

  if (!timerTick) timerTick = setInterval(paint, 1000);
}

async function loadTimer() {
  if (!can('timesheets:write:own')) return;

  const state = await api('/me/timer');
  runningTimer = state.running ? state : null;

  // The project it was started with, so stopping books what was chosen - the
  // select is disabled while it runs, and this is what it shows.
  if (runningTimer?.projectId) {
    $('#timer-project').value = String(runningTimer.projectId);
  }

  if (runningTimer?.description) {
    $('#timer-description').value = runningTimer.description;
  }

  renderTimer();
}

function wireTimer() {
  const start = $('#timer-start');
  if (!start) return;

  start.addEventListener('click', () => {
    const projectId = $('#timer-project').value;
    const description = $('#timer-description').value.trim();

    const body = {};
    if (projectId) body.projectId = Number(projectId);
    if (description) body.description = description;

    mutate(
      () => api('/me/timer', { method: 'POST', body: JSON.stringify(body) }),
      t('timer.started', 'The clock is running'),
      loadTimer);
  });

  $('#timer-stop').addEventListener('click', () => {
    mutate(
      () => api('/me/timer/stop', { method: 'POST' }),
      t('msg.booked', 'Time booked'),
      // Both time views as well: a booking has just appeared in each of them.
      async () => { await loadTimer(); await reloadTimeViews(); });
  });

  $('#timer-discard').addEventListener('click', async () => {
    const proceed = await confirmDialog({
      title: t('timer.discardTitle', 'Discard the measured time?'),
      text: t('timer.discardText',
        'The clock is stopped and nothing is recorded. This cannot be undone.'),
      confirmLabel: t('timer.discard', 'Discard'),
    });

    if (!proceed) return;

    mutate(
      () => api('/me/timer', { method: 'DELETE' }),
      t('timer.discarded', 'The clock was discarded'),
      loadTimer);
  });
}

// ------------------------------------------------------- metrics and tracing

/**
 * The value that means "administered off", as opposed to the empty value, which
 * means "nothing is administered here and the configuration file decides".
 *
 * Two distinct things that a single empty option would conflate, and the
 * difference is what the server stores: an empty exporter it has been told about
 * overrides a configured one, while an absent field leaves it alone.
 */
const TELEMETRY_OFF = 'off';

/** Fills the metrics and tracing form from the server's answer. */
function fillTelemetryForm(data) {
  const form = $('#form-telemetry');
  const configured = data.configured ?? {};

  form.elements.logLevel.value = configured.logLevel ?? '';
  form.elements.metricsOff.value = configured.metricsOff ? TELEMETRY_OFF : '';

  // Three states in one control: absent, administered off, or an exporter.
  if (configured.traceExporter === undefined || configured.traceExporter === null) {
    form.elements.traceExporter.value = '';
  } else {
    form.elements.traceExporter.value = configured.traceExporter || TELEMETRY_OFF;
  }

  form.elements.tracerUrl.value = configured.tracerUrl ?? '';
  form.elements.tracerRatio.value = configured.tracerRatio ?? '';

  $('#telemetry-active').textContent = describeActiveTelemetry(data.active ?? {});
}

/**
 * Says what this process is actually serving and exporting.
 *
 * Worth a sentence of its own rather than leaving the form to imply it: until the
 * next restart the stored settings and the running ones disagree, and the
 * metrics URL is the one thing on this screen somebody wants to copy.
 */
function describeActiveTelemetry(active) {
  const metrics = active.metricsServed
    // The host comes from the browser rather than the server, which cannot know
    // the name this installation is reached under. The port is a different one,
    // so it has to be spelled out in full to be usable.
    ? `${window.location.protocol}//${window.location.hostname}:${active.metricsPort}${active.metricsPath}`
    : t('tel.activeMetricsOff', 'not served');

  const traces = active.traceExporter
    ? `${active.traceExporter} → ${active.tracerUrl} (${active.tracerRatio})`
    : t('tel.activeTracesOff', 'not exported');

  return `${t('tel.activeLog', 'Log level')}: ${active.logLevel} · `
    + `${t('tel.activeMetrics', 'Metrics')}: ${metrics} · `
    + `${t('tel.activeTraces', 'Traces')}: ${traces}`;
}

/**
 * Reads the form, omitting anything left following the configuration file.
 *
 * An omitted field is not the same as an empty one here, which is why this is
 * assembled by hand rather than by formData: sending tracerUrl: "" would store a
 * deliberate blank where the intent was to leave the file's value in place.
 */
function telemetryPayload() {
  const form = $('#form-telemetry');
  const body = {};

  const level = form.elements.logLevel.value;
  if (level !== '') body.logLevel = level;

  if (form.elements.metricsOff.value === TELEMETRY_OFF) body.metricsOff = true;

  const exporter = form.elements.traceExporter.value;
  if (exporter === TELEMETRY_OFF) body.traceExporter = '';
  else if (exporter !== '') body.traceExporter = exporter;

  const url = form.elements.tracerUrl.value.trim();
  if (url !== '') body.tracerUrl = url;

  const ratio = form.elements.tracerRatio.value.trim();
  if (ratio !== '') body.tracerRatio = Number(ratio);

  return body;
}

function wireTelemetry() {
  $('#form-telemetry').addEventListener('submit', (e) => {
    e.preventDefault();
    mutate(
      () => api('/settings/telemetry', {
        method: 'PUT', body: JSON.stringify(telemetryPayload()),
      }),
      t('admin.restartNeeded', 'Saved. Applied on the next start.'),
      // The restart card too, so what was just saved appears in the list of
      // what is waiting rather than only in a toast that fades.
      afterTelemetrySaved);
  });

  $('#telemetry-reset').addEventListener('click', () => {
    mutate(
      () => api('/settings/telemetry', { method: 'PUT', body: JSON.stringify({}) }),
      t('tel.resetDone', 'Metrics and tracing follow the configuration file again'),
      afterTelemetrySaved);
  });
}

async function loadTelemetry() {
  fillTelemetryForm(await api('/settings/telemetry'));
}

/** Both cards, because saving one of these changes what the other has to say. */
async function afterTelemetrySaved() {
  await loadTelemetry();
  await loadRestart();
}

// ---------------------------------------------------------------- passkeys

/**
 * WebAuthn speaks ArrayBuffers; JSON speaks strings. Everything crossing that
 * boundary is base64url, and getting one of these backwards produces a
 * signature that verifies against nothing, with no useful error - so they live
 * here, once, rather than inline at four call sites.
 */
function b64urlToBytes(value) {
  const padded = value.replace(/-/g, '+').replace(/_/g, '/')
    .padEnd(value.length + (4 - value.length % 4) % 4, '=');
  const binary = atob(padded);
  const bytes = new Uint8Array(binary.length);

  for (let i = 0; i < binary.length; i += 1) bytes[i] = binary.charCodeAt(i);

  return bytes.buffer;
}

function bytesToB64url(buffer) {
  const bytes = new Uint8Array(buffer);
  let binary = '';

  for (const byte of bytes) binary += String.fromCharCode(byte);

  return btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
}

/** Whether this browser and this connection can do passkeys at all. */
let passkeysAvailable = false;

/**
 * Asks the server whether to offer passkeys.
 *
 * Two conditions have to hold: the browser has to implement WebAuthn, and the
 * connection has to be a secure context. Chrome does not define
 * PublicKeyCredential at all over plain HTTP, so the first check covers the
 * second - but the server knows why, and can be asked before showing a button
 * that could only fail.
 */
async function loadPasskeySupport() {
  if (!window.PublicKeyCredential) {
    passkeysAvailable = false;

    return;
  }

  try {
    passkeysAvailable = Boolean((await api('/auth/passkey'))?.available);
  } catch {
    passkeysAvailable = false;
  }

  $('#login-passkey').hidden = !passkeysAvailable;
}

/** Registers a new credential for the signed-in user. */
/**
 * A refused passkey, in the reader's language.
 *
 * navigator.credentials rejects with a DOMException whose message the browser writes
 * itself, in its own wording and usually in English: "The request is not allowed by the
 * user agent or the platform in the current context, possibly because the user denied
 * permission." That went straight into a notice, so a German reader got an English
 * sentence - and a long one that names neither what failed nor what to do next.
 *
 * The name is the part worth reading. It is fixed by the specification, so it can be
 * translated where the message cannot, and it distinguishes the cases that need
 * different answers: a prompt somebody dismissed, a device that already holds a passkey
 * for this account, and a page served over plain HTTP, which cannot work at all.
 *
 * Anything unrecognised keeps the browser's own sentence. It is in the wrong language,
 * and it is still better than "something went wrong".
 */
function passkeyProblem(err) {
  switch (err?.name) {
    case 'NotAllowedError':
      return t('passkey.err.notAllowed',
        'The prompt was dismissed, or it timed out. Nothing was changed.');
    case 'InvalidStateError':
      return t('passkey.err.already',
        'This device already holds a passkey for this account.');
    case 'NotSupportedError':
      return t('passkey.err.unsupported',
        'This device cannot make the kind of passkey this installation asks for.');
    case 'SecurityError':
      return t('passkey.err.insecure',
        'A passkey needs HTTPS, and the address in the bar has to be the one it was '
        + 'made for.');
    case 'AbortError':
      return t('passkey.err.aborted', 'The prompt closed before anything was done.');
    default:
      return err?.message || t('passkey.failed', 'The passkey was not accepted.');
  }
}

async function registerPasskey(name) {
  const started = await api('/me/passkeys/register', { method: 'POST' });
  const options = started.options.publicKey;

  // The server sends base64url because JSON cannot hold bytes; the browser
  // wants the bytes back.
  options.challenge = b64urlToBytes(options.challenge);
  options.user.id = b64urlToBytes(options.user.id);

  for (const credential of options.excludeCredentials ?? []) {
    credential.id = b64urlToBytes(credential.id);
  }

  let created;

  try {
    created = await navigator.credentials.create({ publicKey: options });
  } catch (err) {
    throw new Error(passkeyProblem(err));
  }

  if (!created) throw new Error(t('passkey.cancelled', 'The passkey was not created.'));

  return api('/me/passkeys/register', {
    method: 'PUT',
    body: JSON.stringify({
      token: started.token,
      name,
      credential: {
        id: created.id,
        rawId: bytesToB64url(created.rawId),
        type: created.type,
        response: {
          clientDataJSON: bytesToB64url(created.response.clientDataJSON),
          attestationObject: bytesToB64url(created.response.attestationObject),
        },
        // What the device is: a phone, a security key, the laptop itself. The
        // server keeps it so the next prompt can ask for the right thing.
        transports: created.response.getTransports?.() ?? [],
      },
    }),
  });
}

/**
 * Signs in with a passkey, without a username.
 *
 * The browser offers whichever credentials it holds for this site, and the one
 * that signs names its own owner - so there is nothing to type. That is the
 * point: a password never typed cannot be phished, reused, or read out of
 * somebody else's breach.
 */
async function signInWithPasskey() {
  const started = await api('/auth/passkey/login', { method: 'POST' });
  const options = started.options.publicKey;

  options.challenge = b64urlToBytes(options.challenge);

  for (const credential of options.allowCredentials ?? []) {
    credential.id = b64urlToBytes(credential.id);
  }

  let assertion;

  try {
    assertion = await navigator.credentials.get({ publicKey: options });
  } catch (err) {
    throw new Error(passkeyProblem(err));
  }

  if (!assertion) throw new Error(t('passkey.cancelled', 'The passkey was not used.'));

  return api('/auth/passkey/login', {
    method: 'PUT',
    body: JSON.stringify({
      token: started.token,
      credential: {
        id: assertion.id,
        rawId: bytesToB64url(assertion.rawId),
        type: assertion.type,
        response: {
          clientDataJSON: bytesToB64url(assertion.response.clientDataJSON),
          authenticatorData: bytesToB64url(assertion.response.authenticatorData),
          signature: bytesToB64url(assertion.response.signature),
          userHandle: assertion.response.userHandle
            ? bytesToB64url(assertion.response.userHandle)
            : null,
        },
      },
    }),
  });
}

async function loadPasskeys() {
  const card = $('#passkey-card');

  // The built-in administrator keeps its password on purpose, so it is never
  // offered a passkey it might then come to rely on.
  card.hidden = !passkeysAvailable || !me.user || me.user.isSystem;

  if (card.hidden) return;

  const passkeys = (await api('/me/passkeys'))?.items ?? [];

  const rows = passkeys.map((passkey) => el('tr', {},
    el('td', { text: passkey.name }),
    el('td', { text: fmtDate(passkey.createdAt) }),
    el('td', {
      text: passkey.lastUsedAt ? fmtDate(passkey.lastUsedAt) : t('passkey.never', 'never'),
    }),
    el('td', { class: 'actions' }, deleteButton({
      label: `${t('passkey.name', 'Name')} "${passkey.name}"`,
      path: `/me/passkeys/${passkey.id}`,
      message: t('passkey.removed', 'Passkey removed'),
      after: loadPasskeys,
    })),
  ));

  fillTable($('#table-passkeys tbody'), rows, 4,
    t('passkey.empty', 'No passkeys yet. Add one to sign in without a password.'));
}

function wirePasskeys() {
  $('#form-passkey').addEventListener('submit', (e) => {
    e.preventDefault();

    const field = e.target.elements.name;

    mutate(() => registerPasskey(field.value.trim()),
      t('passkey.added.done', 'Passkey added'),
      async () => { field.value = ''; await loadPasskeys(); });
  });

  $('#login-passkey').addEventListener('click', async () => {
    try {
      await signInWithPasskey();
    } catch (err) {
      // A cancelled prompt and a rejected credential are indistinguishable
      // from here, so the message covers both without guessing.
      showLogin(err.message || t('passkey.failed', 'The passkey was not accepted.'));

      return;
    }

    hideLogin();

    try {
      await refreshAll();
      switchView(firstVisibleView());
      await greetAfterSignIn();
    } catch (err) {
      toast(`${t('msg.loadFailed', 'Could not load everything')}: ${err.message}`, 'error');
    }
  });
}

// --------------------------------------------------------------- live log

/**
 * State of the log viewer.
 *
 * `since` is the sequence number the server last reported, not the last line
 * rendered. The filtering happens on the server, so a filter that matches
 * nothing still advances the cursor - asking from the last *rendered* line
 * would re-scan the whole buffer on every poll.
 */
const logView = {
  since: 0,
  timer: null,
  polling: false,
  paused: false,
  levels: null,
  // A default that is useful rather than complete: DEBUG carries every SQL
  // statement the process runs, which buries everything else.
  quiet: new Set(['DEBUG']),
};

const LOG_DEFAULT_DELAY = 3;
const LOG_MAX_LINES = 2000;

function wireLogViewer() {
  const search = $('#log-search');
  const delay = $('#log-delay');
  const pause = $('#log-pause');

  if (!search || !delay || !pause) return;

  // A filter change means the server has to re-scan from the start of its
  // buffer, so the cursor and the view both reset.
  const restart = () => {
    logView.since = 0;
    $('#log-output').replaceChildren();
    schedulePoll({ immediate: true });
  };

  search.addEventListener('input', debounce(restart, 300));
  $('#log-levels').addEventListener('change', restart);

  delay.addEventListener('change', () => schedulePoll({ immediate: true }));

  pause.addEventListener('click', () => {
    logView.paused = !logView.paused;
    renderPauseButton();
    schedulePoll({ immediate: !logView.paused });
  });

  $('#log-clear').addEventListener('click', () => {
    // The view only. The server's buffer is not the viewer's to discard, and an
    // administrator clearing their screen must not destroy evidence for the
    // next person to look.
    $('#log-output').replaceChildren();
    $('#log-warning').hidden = true;
  });

  renderPauseButton();
}

/** Waits until the user stops typing, so each keystroke is not a request. */
function debounce(fn, ms) {
  let handle = null;

  return (...args) => {
    clearTimeout(handle);
    handle = setTimeout(() => fn(...args), ms);
  };
}

function renderPauseButton() {
  $('#log-pause').textContent = logView.paused
    ? t('log.resume', 'Resume')
    : t('log.pause', 'Pause');
}

/** The levels currently ticked. */
function selectedLogLevels() {
  return $$('#log-levels input:checked').map((input) => input.value);
}

/** The refresh interval in seconds; 0 means do not poll. */
function logDelaySeconds() {
  const value = Number.parseInt($('#log-delay').value, 10);
  if (!Number.isFinite(value) || value < 0) return LOG_DEFAULT_DELAY;

  return Math.min(value, 300);
}

/**
 * Restarts the polling timer for the current settings.
 *
 * Called on every change rather than adjusting a running timer, because
 * "refresh every n seconds" has to mean the new n immediately - not after the
 * old interval has elapsed one more time.
 */
function schedulePoll({ immediate = false } = {}) {
  clearTimeout(logView.timer);
  logView.timer = null;

  if (immediate) void pollLog();

  if (logView.paused) {
    setLogStatus(t('log.paused', 'Paused.'));
    return;
  }

  const seconds = logDelaySeconds();
  if (seconds === 0) {
    setLogStatus(t('log.manual', 'Refreshing is off. Set a number of seconds to follow along.'));
    return;
  }

  logView.timer = setTimeout(() => {
    void pollLog().finally(() => {
      // Chained rather than an interval: a slow or hanging request must not
      // pile up behind itself.
      if (logViewerActive()) schedulePoll();
    });
  }, seconds * 1000);
}

/** Stops polling. Called when the screen is left or the session ends. */
function stopLogPolling() {
  clearTimeout(logView.timer);
  logView.timer = null;
}

/**
 * Whether the log should be polled at all.
 *
 * Only while its own screen is on top: an administrator who moved on to book
 * time has no use for a request every three seconds, and the endpoint is not
 * free - it reads a mutex-guarded buffer.
 */
function logViewerActive() {
  const card = $('#log-card');
  if (!card) return false;

  // The sign-in screen being up means there is no session to poll with. Without
  // this the poller keeps asking through a password change - which ends every
  // session - and paints the screen with authentication failures.
  return isSystemAdmin() && !$('#view-admin').hidden && $('#login-screen').hidden;
}

function setLogStatus(text) {
  const status = $('#log-status');
  if (status) status.textContent = text;
}

async function pollLog() {
  if (!logViewerActive() || logView.polling) return;

  logView.polling = true;

  try {
    const query = new URLSearchParams({ since: String(logView.since), limit: '500' });

    const levels = selectedLogLevels();
    // Every level ticked is the same request as none, and sending none keeps
    // the server from filtering at all.
    if (levels.length > 0 && levels.length < (logView.levels?.length ?? 0)) {
      query.set('levels', levels.join(','));
    }

    const search = $('#log-search').value.trim();
    if (search) query.set('search', search);

    const page = await api(`/admin/logs?${query}`);

    if (logView.levels === null) buildLogLevelFilters(page.levels ?? []);

    if (!page.available) {
      setLogStatus(t('log.unavailable',
        'Log capture is not installed in this process, so there is nothing to show.'));
      return;
    }

    logView.since = page.lastSeq ?? logView.since;

    appendLogLines(page.records ?? []);

    const warning = $('#log-warning');
    if (page.dropped > 0) {
      warning.textContent = t('log.dropped',
        'Older lines have been discarded from the buffer and cannot be recovered.');
      warning.hidden = false;
    }

    setLogStatus(logView.paused
      ? t('log.paused', 'Paused.')
      : `${t('log.upTo', 'Up to line')} ${logView.since}`);
  } catch (err) {
    if (err.status === 401 || err.status === 403) {
      // The session ended - a password change does that, and so does an
      // expiry. Retrying would produce one failure per interval for as long as
      // the tab stays open. Signing in again restarts it, because switchView
      // does.
      stopLogPolling();
      setLogStatus(t('log.signedOut', 'Not signed in any more; the log stopped updating.'));

      return;
    }

    // Any other failure leaves it running: a viewer that gives up on one bad
    // request is useless during exactly the incident it exists for.
    setLogStatus(`${t('log.failed', 'Could not read the log')}: ${err.message}`);
  } finally {
    logView.polling = false;
  }
}

/** Builds the level chips from what the server says it can emit. */
function buildLogLevelFilters(levels) {
  logView.levels = levels;

  const holder = $('#log-levels');
  holder.replaceChildren();

  for (const level of levels) {
    const label = document.createElement('label');
    const input = document.createElement('input');

    input.type = 'checkbox';
    input.value = level;
    input.checked = !logView.quiet.has(level);

    label.append(input, document.createTextNode(level));
    holder.append(label);
  }
}

function appendLogLines(records) {
  if (records.length === 0) return;

  const output = $('#log-output');

  // Whether to scroll afterwards has to be decided before anything is added,
  // because appending changes scrollHeight.
  const follow = $('#log-follow').checked && atLogBottom(output);

  const batch = document.createDocumentFragment();

  for (const record of records) {
    const line = document.createElement('div');
    line.className = 'log-line';
    line.dataset.level = record.level;

    const time = document.createElement('span');
    time.className = 'log-time';
    time.textContent = formatLogTime(record.time);

    const level = document.createElement('span');
    level.className = 'log-level';
    level.textContent = record.level;

    const message = document.createElement('span');
    message.className = 'log-message';
    // textContent, not innerHTML: these are log lines, and a log line is
    // attacker-influenced text - a failed sign-in carries whatever address was
    // typed into it.
    message.textContent = record.message;

    if (record.traceId) line.title = `trace ${record.traceId}`;

    line.append(time, level, message);
    batch.append(line);
  }

  output.append(batch);

  // The view is not the buffer: keeping every line since the screen opened
  // would grow without limit in a long incident.
  while (output.childElementCount > LOG_MAX_LINES) output.firstElementChild.remove();

  if (follow) output.scrollTop = output.scrollHeight;
}

/**
 * Whether the view is scrolled to the end.
 *
 * Scrolling up is a deliberate act - reading something. Following would yank
 * the reader back down on the next poll, so it only applies while they are
 * already at the bottom.
 */
function atLogBottom(output) {
  return output.scrollHeight - output.scrollTop - output.clientHeight < 40;
}

function formatLogTime(iso) {
  const at = new Date(iso);
  if (Number.isNaN(at.getTime())) return '';

  // The viewer's own zone, which is the one they are comparing against a
  // clock on the wall while working out what happened when.
  return at.toLocaleTimeString(undefined, { hour12: false });
}

// -------------------------------------------------------- maintenance mode

/**
 * Renders the notice, and reports whether the installation is out of service.
 *
 * Read on its own rather than as part of the branding, because it has to work on
 * the sign-in screen - where there is no session and most of the API is refused.
 */
async function loadMaintenance() {
  const banner = $('#maintenance-banner');
  if (!banner) return false;

  let state = { enabled: false, message: '' };

  try {
    state = await api('/maintenance');
  } catch {
    // Not fatal. An installation whose settings cannot be read has a bigger
    // problem than a missing banner, and the request that needs them will say so.
    banner.hidden = true;

    return false;
  }

  const notice = state.enabled
    ? (state.message || t('maint.default', 'This installation is temporarily unavailable for maintenance.'))
    : '';

  banner.textContent = notice;
  banner.hidden = !state.enabled;

  // And again inside the sign-in screen, which covers the banner above: it is
  // fixed over the whole viewport with an opaque background, so somebody who is
  // not signed in would never see the one at the top of the page.
  const onLogin = $('#login-maintenance');
  if (onLogin) {
    onLogin.textContent = notice;
    onLogin.hidden = !state.enabled;
  }

  // The form, for the administrator who is looking at it.
  const form = $('#form-maintenance');
  if (form) {
    form.elements.enabled.checked = Boolean(state.enabled);
    form.elements.message.value = state.message ?? '';
  }

  return Boolean(state.enabled);
}

function wireMaintenance() {
  const form = $('#form-maintenance');
  if (!form) return;

  // async, because the question is now a dialog that resolves rather than a
  // browser box that blocks the page.
  form.addEventListener('submit', async (event) => {
    event.preventDefault();

    const enabled = form.elements.enabled.checked;

    // Asked about only when switching it on. Turning it off needs no
    // confirmation: that is the direction that ends an outage, and a dialog in
    // front of it is a dialog between somebody and fixing their installation.
    if (enabled) {
      const proceed = await confirmDialog({
        title: t('maint.title', 'Maintenance mode'),
        text: t('maint.confirm',
          'Turn this installation out of service? Everyone except this account will be turned away.'),
        confirmLabel: t('maint.enabled', 'Out of service'),
      });

      if (!proceed) {
        // Put back, because the checkbox has already been ticked by the click
        // that opened this question.
        form.elements.enabled.checked = false;

        return;
      }
    }

    mutate(
      () => api('/settings/maintenance', {
        method: 'PUT',
        body: JSON.stringify({ enabled, message: form.elements.message.value }),
      }),
      enabled
        ? t('maint.onSaved', 'The installation is now out of service.')
        : t('maint.offSaved', 'The installation is back in service.'),
      loadMaintenance);
  });
}

// ---------------------------------------------------------------- timezone

/**
 * A short list to fall back on where the browser cannot enumerate zones.
 *
 * Deliberately not a curated "important zones" list, which would be a
 * permanent maintenance burden and would still be wrong for someone: it exists
 * only so the picker is never empty, and the server accepts any valid name.
 */
const FALLBACK_TIMEZONES = [
  'UTC', 'Europe/London', 'Europe/Berlin', 'Europe/Zurich', 'Europe/Vienna',
  'Europe/Moscow', 'America/New_York', 'America/Chicago', 'America/Denver',
  'America/Los_Angeles', 'America/Sao_Paulo', 'Asia/Dubai', 'Asia/Kolkata',
  'Asia/Shanghai', 'Asia/Tokyo', 'Australia/Sydney', 'Pacific/Auckland',
];

/** Every zone this browser knows, so the picker matches the tz database. */
function availableTimezones() {
  try {
    const supported = Intl.supportedValuesOf?.('timeZone');
    if (supported?.length) return supported;
  } catch {
    // Older browsers have no supportedValuesOf; the fallback covers them.
  }

  return FALLBACK_TIMEZONES;
}

/** The current time in a zone, so a choice can be checked at a glance. */
function timeIn(timeZone) {
  try {
    return new Intl.DateTimeFormat(activeLanguage(), {
      timeZone, dateStyle: 'medium', timeStyle: 'short',
    }).format(new Date());
  } catch {
    return '';
  }
}

/**
 * Fills a zone picker.
 *
 * inheritLabel, when given, adds an empty option meaning "follow the instance
 * setting". That option is the point of the personal picker: without it every
 * account would be pinned to a zone the moment someone opened the screen, and
 * changing the instance zone would then move nobody.
 */
function fillTimezoneSelect(select, selected, inheritLabel) {
  const zones = availableTimezones();

  select.replaceChildren();

  if (inheritLabel !== undefined) {
    select.append(el('option', { value: '', text: inheritLabel }));
  }

  for (const zone of zones) {
    select.append(el('option', { value: zone, text: zone }));
  }

  // A stored zone this browser does not list would otherwise be silently
  // dropped, and saving the form would change it without anyone asking.
  if (selected && !zones.includes(selected)) {
    select.append(el('option', { value: selected, text: selected }));
  }

  select.value = selected ?? '';
}

/** Shows what time it currently is in the picked zone. */
function showTimeIn(select, output, fallbackZone) {
  const zone = select.value || fallbackZone;
  const now = timeIn(zone);

  output.textContent = now ? `${zone} — ${now}` : '';
}

/** Loads the personal zone into "My account". */
function fillMyTimezone() {
  const select = $('#my-timezone');
  const inherit = `${t('tz.inherit', 'Follow the instance setting')}`;
  const effective = me.user?.effectiveTimezone ?? 'UTC';

  fillTimezoneSelect(select, me.user?.timezone ?? '', `${inherit} (${effective})`);
  showTimeIn(select, $('#my-timezone-now'), effective);
}

function wireTimezones() {
  const mine = $('#my-timezone');
  const myNow = $('#my-timezone-now');

  mine.addEventListener('change', () => showTimeIn(mine, myNow,
    me.user?.effectiveTimezone ?? 'UTC'));

  $('#form-my-timezone').addEventListener('submit', (e) => {
    e.preventDefault();
    mutate(
      () => api('/me/timezone', {
        method: 'PUT', body: JSON.stringify({ timezone: mine.value }),
      }),
      t('tz.saved', 'Timezone saved'),
      refreshAll);
  });

  const instance = $('#instance-timezone');
  const instanceNow = $('#instance-timezone-now');

  instance.addEventListener('change', () => showTimeIn(instance, instanceNow, 'UTC'));

  $('#form-timezone').addEventListener('submit', (e) => {
    e.preventDefault();
    mutate(
      () => api('/settings/timezone', {
        method: 'PUT', body: JSON.stringify({ timezone: instance.value }),
      }),
      t('tz.saved', 'Timezone saved'),
      refreshAll);
  });
}

// ------------------------------------------------------------------- theme

/**
 * How often "automatic" re-checks the clock.
 *
 * A dashboard left open across the evening should follow the day rather than
 * stay bright until someone reloads it. A minute is far more often than it can
 * matter and costs nothing.
 */
const THEME_RECHECK_MS = 60_000;

/** Stores the choice and applies it, keeping theme.js as the single authority. */
function setThemePreference(preference) {
  const theme = window.gtrTheme;

  document.documentElement.dataset.themePreference = preference;
  document.documentElement.dataset.theme = theme.resolve(preference);

  try {
    if (preference === 'auto') localStorage.removeItem(theme.STORAGE_KEY);
    else localStorage.setItem(theme.STORAGE_KEY, preference);
  } catch {
    // Storage can be refused; the choice still holds for this page.
  }
}

/** Wires the appearance picker and keeps "automatic" honest as the day passes. */
function wireTheme() {
  const picker = $('#theme-picker');
  const theme = window.gtrTheme;

  picker.value = theme.stored();
  picker.addEventListener('change', (e) => setThemePreference(e.target.value));

  setInterval(() => {
    if (document.documentElement.dataset.themePreference !== 'auto') return;

    const now = theme.resolve('auto');
    if (now !== document.documentElement.dataset.theme) {
      document.documentElement.dataset.theme = now;
    }
  }, THEME_RECHECK_MS);
}

// --------------------------------------------------------------- bootstrap

function switchView(name) {
  $$('.tab').forEach((tab) => tab.setAttribute('aria-current', String(tab.dataset.view === name)));
  $$('.view').forEach((view) => { view.hidden = view.id !== `view-${name}`; });

  // In the address bar, so a reload comes back to the same screen and a link to
  // one can be sent to somebody. replaceState rather than assigning to
  // location.hash: assigning pushes an entry, and Back would then walk through
  // every tab that was looked at instead of leaving the page.
  if (currentHashView() !== name) {
    history.replaceState(null, '', `#${name}`);
  }

  // The log viewer polls, so it follows the screen it lives on rather than
  // running for as long as the tab is open.
  if (logViewerActive()) schedulePoll({ immediate: true });
  else stopLogPolling();

  // An enrolment in progress does not survive leaving the screen. The panel holds a
  // shared secret and the QR code that encodes it, and neither has any business
  // sitting on a screen somebody has walked away from. Starting again is what the
  // Enable button does anyway.
  if (name !== 'settings' && $('#totp-setup') && !$('#totp-setup').hidden) {
    renderTOTPState();
  }
}

/** The view named in the address bar, if it names one. */
function currentHashView() {
  return decodeURIComponent(window.location.hash.replace(/^#/, ''));
}

/**
 * The view to open: the one in the address bar when it is real and permitted,
 * otherwise the first tab this user may see.
 *
 * Checked against the tabs rather than trusted, because the hash is whatever was
 * typed or bookmarked - including a tab this user is not allowed, or one that
 * stopped existing between releases.
 */
function startingView() {
  const wanted = currentHashView();
  const permitted = $$('.tab').some((tab) => !tab.hidden && tab.dataset.view === wanted);

  return permitted ? wanted : firstVisibleView();
}

/** Picks the first tab the user is actually allowed to see. */
function firstVisibleView() {
  const tab = $$('.tab').find((candidate) => !candidate.hidden);
  return tab ? tab.dataset.view : 'settings';
}

async function refreshAll() {
  await loadMe();

  // Before the booking date is worked out, because that depends on the zone -
  // and on a first sign-in the zone is the thing being adopted.
  if (await adoptBrowserDefaults()) await loadMe();

  // Only now is the applicable zone known, so the booking date is set here
  // rather than at start-up, where it would still be the browser's guess.
  resetBookingDate();

  // Before every other loader, because the server blocks the rest of the API
  // while the initial password stands - and that is exactly when the wizard is
  // most needed. /setup deliberately stays reachable through that block.
  await loadSetup();

  await loadLanguages();

  // With the initial password still in place the server refuses everything
  // below, so asking would only produce a screenful of errors. What is left -
  // the banner, My account, the wizard - is exactly what is needed to get past
  // it, and My account is where that happens.
  if (me.user?.mustChangePassword) {
    fillSettingsForm();
    switchView('settings');

    return;
  }

  // Roles first: the user table renders a role picker from them.
  await loadRoles();
  await Promise.all([loadUsers(), loadProjects()]);
  await loadTimesheets();
  // After users and projects, so the calendar can resolve names.
  await loadCalendar();
  await loadTimer();
  await loadStatistics();
  await loadAdmin();
  await loadTokens();
  await loadPasskeys();
  fillSettingsForm();
}

function wireForms() {
  $('#form-user').addEventListener('submit', (e) => {
    e.preventDefault();
    // No working times here. A new account starts on the instance default under
    // Settings and its owner changes it under My account - a daily target is a time
    // figure, and administering an installation is not the same job as recording time
    // in it.
    const body = formData(e.target);

    mutate(() => api('/users', { method: 'POST', body: JSON.stringify(body) }),
      t('msg.userCreated', 'Staff member created'),
      async () => { e.target.reset(); await refreshAll(); });
  });

  $('#form-role').addEventListener('submit', (e) => {
    e.preventDefault();
    const form = e.target;
    const id = form.elements.id.value;
    const permissions = $$('input[name=permissions]:checked', form).map((i) => i.value);
    const body = {
      name: form.elements.name.value.trim(),
      description: form.elements.description.value.trim(),
      permissions,
    };

    const request = id
      ? api(`/roles/${id}`, { method: 'PUT', body: JSON.stringify(body) })
      : api('/roles', { method: 'POST', body: JSON.stringify(body) });

    mutate(() => request, id ? t('msg.roleSaved', 'Role saved') : t('msg.roleCreated', 'Role created'),
      async () => { resetRoleForm(); await refreshAll(); });
  });

  $('#role-reset').addEventListener('click', resetRoleForm);

  $('#form-project').addEventListener('submit', (e) => {
    e.preventDefault();
    const body = formData(e.target);
    mutate(() => api('/projects', { method: 'POST', body: JSON.stringify(body) }),
      t('msg.projectCreated', 'Project created'),
      async () => { e.target.reset(); await refreshAll(); });
  });

  $('#form-timesheet').addEventListener('submit', (e) => {
    e.preventDefault();
    const { id, ...raw } = formData(e.target);
    const body = {
      ...raw,
      projectId: Number(raw.projectId),
      durationHours: Number(raw.durationHours),
    };

    // The same form books and corrects; the id decides which.
    const editing = Boolean(id);
    const path = editing ? `/timesheets/${id}` : '/timesheets';
    const method = editing ? 'PUT' : 'POST';

    mutate(() => api(path, { method, body: JSON.stringify(body) }),
      editing ? t('msg.entrySaved', 'Entry saved') : t('msg.booked', 'Time booked'),
      async () => {
        // Keeps the project and the date, so booking several entries in a row stays
        // quick.
        resetTimesheetForm();
        await reloadTimeViews();
      });
  });

  $('#timesheet-cancel').addEventListener('click', resetTimesheetForm);

  $('#form-report').addEventListener('submit', (e) => {
    e.preventDefault();
    const { projectId, from, to } = formData(e.target);
    const params = new URLSearchParams();
    if (from) params.set('from', from);
    if (to) params.set('to', to);
    const suffix = params.toString() ? `?${params}` : '';

    mutate(async () => {
      const report = await api(`/projects/${projectId}/report${suffix}`);

      // One row, because the total covers the reader's own hours and nobody else's.
      // The column used to name the person, which is now always the same person.
      const rows = (report.entries ?? []).map((entry) => el('tr', {},
        el('td', { text: `${fmtDate(report.from)} – ${fmtDate(report.to)}` }),
        el('td', { class: 'num', text: entry.hours.toFixed(2) }),
      ));

      fillTable($('#table-report tbody'), rows, 2, t('ot.empty', 'No bookings in this period.'));
      $('#report-total').textContent = t('report.total', '{0} h in total')
        .replace('{0}', report.totalHours.toFixed(2));
      $('#report-result').hidden = false;
    }, null, null);
  });

  $('#form-overtime').addEventListener('submit', (e) => {
    e.preventDefault();
    const { from, to } = formData(e.target);
    const params = new URLSearchParams();
    if (from) params.set('from', from);
    if (to) params.set('to', to);
    const suffix = params.toString() ? `?${params}` : '';

    mutate(async () => {
      // Your own balance. The id is still in the path because the endpoint takes
      // one, and any other id is refused rather than answered.
      const balance = await api(`/users/${me.user.id}/overtime${suffix}`);
      const rows = (balance.days ?? []).map((d) => el('tr', {},
        el('td', { text: fmtDate(d.date) }),
        el('td', { class: 'num', text: fmtHours(d.booked) }),
        el('td', { class: 'num', text: fmtHours(d.target) }),
        balanceCell(d.balance),
      ));
      fillTable($('#table-overtime tbody'), rows, 4, t('ot.empty', 'No bookings in this period.'));

      const total = balance.totalBalance;
      const pill = $('#overtime-total');
      pill.textContent = `${total > 0 ? '+' : ''}${total.toFixed(2)} h`;
      pill.className = `pill ${total > 0 ? 'plus' : total < 0 ? 'minus' : ''}`;
      $('#overtime-meta').textContent = t('ot.meta', '{0} · target {1}/day · booked {2} of {3}')
        .replace('{0}', balance.userName)
        .replace('{1}', fmtHours(balance.dailyTarget))
        .replace('{2}', fmtHours(balance.totalBooked))
        .replace('{3}', fmtHours(balance.totalTarget));
      $('#overtime-result').hidden = false;

    }, null, null);
  });

  $('#form-working-times').addEventListener('submit', (e) => {
    e.preventDefault();
    const form = e.target;
    // Empty means "use the instance default", which the API expresses as 0.
    const body = {
      dailyTargetHours: Number(form.elements.dailyTargetHours.value || 0),
      maxDailyHours: Number(form.elements.maxDailyHours.value || 0),
    };
    mutate(() => api(`/users/${me.user.id}/working-times`,
      { method: 'PUT', body: JSON.stringify(body) }),
      t('msg.workingTimesSaved', 'Working hours saved'),
      refreshAll);
  });

  $('#form-password').addEventListener('submit', (e) => {
    e.preventDefault();
    const body = formData(e.target);
    mutate(() => api('/me/password', { method: 'PUT', body: JSON.stringify(body) }),
      t('msg.passwordChanged',
        'Password changed. Your other devices have been signed out.'),
      async () => {
        e.target.reset();

        // Deliberately not signed out. The server ends this account's other
        // sessions and keeps this one, because this is the device that just
        // proved it knew the old password - so there is nothing to sign back
        // into, and dropping somebody at a sign-in screen achieves only a second
        // sign-in.
        //
        // Refreshed, though. Until the change the server refuses most of the API
        // and the interface knows it; the flag that says so lives on the account,
        // and this screen is still holding the copy it was given at sign-in. The
        // re-login used to do this refresh as a side effect.
        await refreshAll();
      });
  });

  $('#filter-ts-project').addEventListener('change',
    () => mutate(loadTimesheets, null, null));

  $('#tabs').addEventListener('click', (e) => {
    const tab = e.target.closest('.tab');
    if (tab) switchView(tab.dataset.view);
  });

  // Following a link to #calendar, or editing the address bar, moves the screen
  // rather than leaving the address disagreeing with what is shown.
  window.addEventListener('hashchange', () => {
    // Only once there is a session; before that the sign-in screen is the whole
    // interface and switching underneath it would change what appears after it.
    if (!$('#login-screen').hidden) return;

    switchView(startingView());
  });
}

async function init() {
  // The sign-in form is wired first and on its own: if anything below fails,
  // the user must still be able to sign in rather than face a form whose
  // submit handler was never attached, which would silently reload the page.
  $('#form-login').addEventListener('submit', submitLogin);
  enhancePasswordFields();

  // Appearance is a device setting and needs no session, so the picker works on
  // the sign-in screen too.
  wireTheme();

  // Branding is public, so the sign-in screen already carries the instance's
  // own title and logo. A failure here must not block signing in.
  try {
    await loadBranding();
  } catch {
    // Falls back to the built-in title.
  }

  // Applied before the first render so the sign-in screen already speaks the
  // browser language; a signed-in user overrides it in loadMe.
  applyLanguage(activeLanguage());

  // Before the sign-in screen is shown, so its passkey button appears with it
  // rather than popping in afterwards.
  await loadPasskeySupport();
  await loadMaintenance();

  try {
    wireForms();
    wireTOTP();
    wireCalendar();
    wireAdmin();
    wireTokens();
    wireDirectorySync();
    wireTimezones();
    wireOperational();
    wireTelemetry();
    wireRestart();
    wireTimer();
    wireStatistics();
    wireWorkbook();
    wireSheetCards();
    // After the forms are wired, so a submit handler registered here runs
    // beside theirs rather than instead of one.
    wirePasswordReveal();
    wireSetup();
    wireTour();
    wireWelcome();
    wirePasskeys();
    wireLogViewer();
    wireMaintenance();
    $('#logout').addEventListener('click', doLogout);
    $('#language-picker').addEventListener('change', (e) => mutate(
      () => api('/me/language', { method: 'PUT', body: JSON.stringify({ language: e.target.value }) }),
      null,
      refreshAll));

    resetBookingDate();
  } catch (err) {
    toast(`${t('msg.initFailed', 'Initialisation failed')}: ${err.message}`, 'error');
  }

  try {
    await refreshAll();
    hideLogin();
    switchView(startingView());

    // After the first view is up, so the tour highlights something that is
    // actually on screen.
    await greetAfterSignIn();
  } catch {
    // No usable session: the sign-in screen is the whole interface until
    // there is one.
    showLogin();
  }
}

document.addEventListener('DOMContentLoaded', init);
