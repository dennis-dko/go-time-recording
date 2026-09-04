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

/**
 * The branding as it was last read.
 *
 * Kept because the configured texts can refer to it - a footer saying "{instance}
 * {version}" is drawn from here - and because a language change redraws those
 * texts without asking the server again.
 */
let lastBranding = {};

/**
 * The version this document came from, read once and never moved.
 *
 * Not lastBranding.version, which follows the process the page is talking to -
 * so after an update it becomes the new version, and a comparison against it
 * would say nothing changed at the one moment everything did.
 *
 * What it answers is a different question: which build wrote the script, the
 * stylesheet and the markup this tab is running. Nothing can change that but a
 * load, which is exactly why it is worth knowing.
 */
let loadedVersion = null;

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

/**
 * What a refusal means beyond its message.
 *
 * Lifted out of api() so the requests that cannot go through it share it. Three
 * cannot: the download needs the response as a blob, and the two imports need the
 * browser to set a multipart boundary only it knows. Each had written out the
 * part it needed and stopped at the message, so all three missed the two answers
 * that are about the installation rather than about the request - a session that
 * has ended, and maintenance mode - and an upload, the longest-running thing here
 * and so the likeliest to be the request that finds the session gone, showed a
 * red toast on a screen that still looked signed in.
 *
 * The caller throws it. This builds it, and does what the status obliges on the
 * way, which is why it is not simply a constructor.
 */
function refusalFrom(res, body) {
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

    // And whoever may not be here while it lasts goes back to the sign-in
    // screen, where the same sentence is waiting.
    //
    // Everything an ordinary account can reach is refused for as long as
    // maintenance is on, so what they had was a signed-in screen where every
    // card was an error and every click another one - the interface insisting
    // they were working while nothing worked. Two clicks after an
    // administrator flipped the switch, and no way to tell it was deliberate.
    //
    // Told to the server rather than only forgotten here: an account signed
    // out by this should have to sign in again afterwards, and a session left
    // standing would let the screen come back by itself the moment maintenance
    // ended.
    //
    // Not for whoever may administer the installation - they are the ones who
    // end it, and the request that brought this back was some other 503.
    if (me.user && !can('settings:manage') && !endingTheSession) {
      endingTheSession = true;

      try {
        void endTheSessionQuietly();
        handBackTheScreen(err.message);
      } finally {
        endingTheSession = false;
      }
    }
  }
  // The status alongside the message, so a caller can tell "you are not
  // signed in" from "that did not work" without parsing prose. The log
  // viewer's poller needs exactly that distinction: on a 401 it has to stop
  // rather than keep asking every few seconds forever.
  err.status = res.status;

  // The refusal itself, kept whole beside the sentence made from it. The
  // sentence is what a person reads; the object holds what could not be turned
  // into one - the words of whatever actually failed, and the reference that
  // finds the matching log line. A caller that has somewhere to fold that away
  // wants it, and every caller that does not can go on using err.message.
  err.refusal = body?.error ?? null;

  // A session that is no longer accepted takes the screen with it.
  //
  // Only where this page believed it had one: the first load asks before
  // anybody has signed in and is answered exactly like this, and treating that
  // as an ending would clear the drafts of a session that never started.
  //
  // Guarded against re-entry, because the ending itself makes requests fail -
  // the pollers it stops are stopped by it, and each of their failures would
  // otherwise arrive here and start the same ending again.
  if (res.status === 401 && me.user && !endingTheSession) {
    endingTheSession = true;

    try {
      handBackTheScreen(t('err.sessionExpired',
        'The session has ended. Please sign in again.'));
    } finally {
      endingTheSession = false;
    }
  }

  return err;
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

  noticePermissionChange(res.headers.get('X-Permissions-Revision'));

  let body = null;
  try {
    body = await res.json();
  } catch {
    // A 204 or an error page has no JSON body; fall through to the status.
  }

  if (!res.ok) {
    throw refusalFrom(res, body);
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

  return describeRefusal(err);
}

/**
 * One refusal, in the reader's language where the server said which rule it was.
 *
 * Split out of errorMessage because a refusal does not always arrive as an error
 * status. The connection test answers 200 with the reason inside it - a database
 * that cannot be reached is information about what somebody typed, not a fault -
 * and that put it outside this path, so it was shown as the English prose the
 * server wrote. Two renderings of one thing is one too many.
 */
function describeRefusal(err) {
  // Maintenance is the one refusal whose sentence may not be ours: an
  // administrator who wrote the notice wrote it for the people who will read it.
  // The server sends this code only when nobody did, so the default is the one
  // thing translated here - and it is the sentence the banner already carries, so
  // both say the same words.
  if (err.code === 'maintenance') {
    return t('err.maintenance',
      'This installation is temporarily unavailable for maintenance.');
  }

  // The named fields first, and before the code, which is the ordering this got
  // wrong the moment a code was added to them: a rejection that says which fields
  // is strictly more use than the sentence it would otherwise fall back to, and
  // the sentence was winning simply because it was tested for first.
  //
  // The fields are labelled the way the form labels them rather than by their
  // column names, which is the whole reason they travel as data.
  if (Array.isArray(err.param) && err.param.length) {
    const named = err.param.map((field) => t(`field.${field}`, field));

    return `${t('err.invalidFields', 'Invalid field(s)')}: ${named.join(', ')}`;
  }

  if (err.code) {
    const translated = t(`err.${err.code}`, err.message ?? '');
    if (translated) return fillIn(translated, err.values);
  }

  if (err.message) return err.message;

  return JSON.stringify(err);
}

/**
 * The part of a refusal that was never going to be translatable.
 *
 * A driver's "dial tcp 10.0.0.4:5432: connect: connection refused", a directory
 * library's own wording, a file system's "permission denied". None of it is this
 * application's prose - it is written by somebody else, in English, and the list
 * of sentences it can produce has no end, so no dictionary here can cover it.
 *
 * Which leaves two bad options and one good one. Showing it is showing a German
 * reader a line they cannot use, on the screen where the problem is. Hiding it
 * is throwing away the only text that says what actually happened. Folding it
 * away serves both: the sentence on screen is in the reader's language, and the
 * words an administrator needs are one click below it.
 *
 * The reference goes with it. The same string is in the log line the server
 * wrote, so somebody reading a screenshot and somebody reading the log are
 * looking at the same occurrence - which is exactly what a generic message
 * otherwise costs.
 */
function refusalDetail(err) {
  if (!err || typeof err !== 'object') return '';

  const parts = [];

  if (err.detail) parts.push(err.detail);

  if (err.ref) {
    parts.push(fillIn(t('detail.reference', 'Reference: {0}'), [err.ref]));
  }

  return parts.join('\n\n');
}

/** A fold-away block of text that is not in the reader's language. */
function detailDisclosure(detail) {
  const box = el('details', { class: 'refusal-detail' },
    el('summary', { text: t('detail.show', 'Technical details') }),
    el('pre', { class: 'refusal-text', text: detail }));

  return box;
}

/**
 * Writes a refusal into an element: the sentence, and the detail under it.
 *
 * One function because there are two places a refusal lands - a notice in the
 * corner and a line under a form - and they had drifted into saying the same
 * thing two different ways.
 */
function showRefusal(target, err) {
  if (!target) return;

  target.replaceChildren(el('span', { text: describeRefusal(err) }));

  const detail = refusalDetail(err);
  if (detail) target.append(detailDisclosure(detail));
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

    return Number.isInteger(value) ? String(value) : fmtNumber(value);
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

/**
 * Tells somebody their rights changed while they were signed in.
 *
 * The server has always applied the change on the very next request - it resolves
 * who is calling from the database every time - but the interface read /me once at
 * start-up and kept whatever it was given. So a right taken away showed up as a
 * refusal on a button that was still on screen, and a right granted showed up as
 * nothing at all until the next sign-in.
 *
 * Three things arrive here, and all of them arrive as a revision to compare with
 * the one /me gave: the header on every response, the once-a-minute poll below,
 * and the announcement stream, which is the only one of the three that reaches a
 * page nobody is touching. Once per change, whichever of them got here first -
 * the recorded value moves forward, so the next one through is already in
 * agreement and says nothing.
 *
 * A banner rather than a toast. The case this is written for is a screen somebody
 * is reading rather than working in, and a notice that takes itself down after a
 * few seconds is one that a person who stepped away never sees - they come back
 * to a screen that looks right and is not. It is a state, like the two notices
 * above it: true until the page is loaded again.
 *
 * A reload rather than re-reading /me in place: what a role opens is more than a
 * set of tabs - it is which screens were loaded at all - and reloading is the one
 * way to be sure the whole interface agrees about it.
 */
function noticePermissionChange(revision) {
  if (!revision || !me.user || !me.permissionsRevision) return;
  if (revision === me.permissionsRevision) return;

  me.permissionsRevision = revision;

  showRightsChanged();
}

/** Puts the notice up, where it stays until the page is loaded again. */
function showRightsChanged() {
  const banner = $('#rights-banner');
  if (banner) banner.hidden = false;
}

/**
 * Takes it down.
 *
 * For a screen that is about to belong to somebody else: a session that ends
 * leaves every card behind it, and the previous account's news is the one thing
 * that must not be waiting for whoever signs in next.
 */
function hideRightsChanged() {
  const banner = $('#rights-banner');
  if (banner) banner.hidden = true;
}

/**
 * How often to ask whether this account may still do what it could a minute ago.
 *
 * The revision travels on every response, so anybody clicking around finds out
 * without this. Somebody reading a screen is not clicking around, and the case
 * that matters is exactly that one: a right is withdrawn while the person it was
 * withdrawn from is looking at the screen it opened.
 *
 * The stream is what covers that case now - the server checks the account behind
 * an open connection and writes down it when the answer moves, so the notice
 * arrives within seconds of the change and without anybody doing anything. This
 * is the fallback underneath it: a browser with no EventSource, a connection a
 * proxy has quietly dropped, a stream that has not reconnected yet.
 *
 * A minute is right for a fallback. This is a notice rather than an enforcement -
 * the API refuses a withdrawn right on the very next call whatever the interface
 * believes - so the only thing at stake is how long somebody looks at a screen
 * that has stopped being true.
 */
const PERMISSION_POLL_MS = 60000;

let permissionPoll = null;

/**
 * Asks periodically, and at once whenever the tab is looked at again.
 *
 * /me rather than a dedicated endpoint: every authenticated response carries the
 * revision header, api() compares it, and this one also happens to be the answer
 * to "what may I do", so nothing new had to be built or kept in step.
 *
 * Not while the tab is hidden. A window left open overnight would otherwise ask
 * five hundred times to tell nobody anything, and the check on becoming visible
 * again covers the whole gap in one request - which is the moment somebody would
 * find out anyway.
 */
function startPermissionPolling() {
  stopPermissionPolling();

  permissionPoll = setInterval(() => {
    if (document.hidden || !me.user) return;

    // Failures are ignored on purpose. This is a background check nobody asked
    // for, and a toast about it - on a screen somebody is reading, once a minute
    // for as long as the network is unhappy - would be worse than the thing it
    // is watching for.
    api('/me').catch(() => {});

    // On the same beat and for the same reason: whether this screen is still
    // true. The announcement stream notices maintenance within seconds, and this
    // is what covers a browser that has no EventSource at all.
    void checkWhetherStillWelcome();
  }, PERMISSION_POLL_MS);
}

/**
 * Asks whether this installation is still open to whoever is looking at it, and
 * hands the screen back if it is not.
 *
 * Maintenance mode refuses everything an ordinary account does, which is the
 * point - but what it left behind was the whole interface standing, every card
 * an error and every click another one, with nothing saying that was deliberate.
 *
 * /maintenance is one of the endpoints maintenance mode deliberately keeps
 * answering, so this works from inside it - which is the whole reason the
 * question can be asked at all.
 *
 * Not for whoever may administer the installation. They are the ones who end it,
 * and signing them out of the screen with the switch on it would be the trap
 * this mode is written to avoid.
 */
async function checkWhetherStillWelcome() {
  if (!me.user || can('settings:manage') || endingTheSession) return;

  let state;

  try {
    state = await api('/maintenance');
  } catch {
    // Unreachable, or refused. Either way this is a background question and the
    // answer to a failed one is to ask again next time.
    return;
  }

  if (!state?.enabled || endingTheSession) return;

  endingTheSession = true;

  try {
    void endTheSessionQuietly();
    handBackTheScreen(state.message || t('err.maintenance',
      'This installation is temporarily unavailable for maintenance.'));
  } finally {
    endingTheSession = false;
  }
}

function stopPermissionPolling() {
  clearInterval(permissionPoll);
  permissionPoll = null;
}

/** How often an open tab asks again. A day left open should notice a release. */
const RELEASE_POLL_MS = 60 * 60 * 1000;

let releasePoll = null;

/**
 * Tells whoever may install one that a newer version exists.
 *
 * The version card says the same thing and says it on one screen, which nobody
 * visits to find out. An installation can sit three releases behind because the
 * only place that mentions it is the place you go once a quarter.
 *
 * A banner rather than a toast, because a toast fades and this is worth not
 * losing - and not dismissable, which it once was. Dismissing read as a
 * courtesy and worked out as the same three releases behind, arrived at by
 * clicking a cross once on a busy morning. It is a state rather than news: a
 * newer version exists whether or not anybody wants to hear it, and it goes
 * when it stops being true.
 *
 * Only for the people who can act on it. Everybody else would be told about
 * work they cannot do, by an application that will not stop mentioning it.
 */
function startReleaseWatch() {
  stopReleaseWatch();

  // Only for the people who can act on it. Everybody else would be told about
  // work they cannot do, by an application that will not stop mentioning it.
  if (!can('settings:manage')) return;

  checkForRelease();

  releasePoll = setInterval(() => {
    if (document.hidden) return;

    checkForRelease();
  }, RELEASE_POLL_MS);
}

function stopReleaseWatch() {
  clearInterval(releasePoll);
  releasePoll = null;

  const banner = $('#release-banner');
  if (banner) banner.hidden = true;
}

async function checkForRelease() {
  let state;

  try {
    state = await api('/settings/update');
  } catch {
    // Taken down, not left standing. A feed that cannot be reached is not this
    // installation being broken, and the version card says so where somebody has
    // gone looking - but the refusal that matters here is the other one: this
    // endpoint needs settings:manage, so an ordinary account is answered 403.
    //
    // Returning without hiding left whatever was on screen. Sign out of an
    // administrator and in as somebody who only works here, in the same page,
    // and the banner stayed - the previous account's news, with a working link
    // to a screen the new one may not have.
    hideReleaseBanner();

    return;
  }

  showReleaseState(state);
}

/**
 * Puts an answer about the newest version on screen, or takes the notice down.
 *
 * Its own function because two things ask that question and both have to end at
 * the same banner: the hourly watch above, and the button on the version card.
 * The button redrew only the card, so the one moment somebody is certainly
 * looking - they pressed it to find out - was the one moment the stripe across
 * the top of every screen went on saying nothing. An hour later it agreed with
 * the card, which is a long time to be told two things at once.
 */
function showReleaseState(state) {
  // Newer rather than installable: a container cannot install it from here, and
  // the person reading this is still the one who should know.
  if (!state?.newer || !state.latest) {
    hideReleaseBanner();

    return;
  }

  // Through the redraw registry, so the sentence follows a language change like
  // everything else the script writes.
  redrawable('release', () => drawReleaseBanner(state));
}

// hideReleaseBanner takes the notice down and stops it being redrawn.
//
// One function because there are three reasons to do it - refused, nothing
// newer, nobody signed in - and a banner that is only hidden on two of them is
// a banner that survives the third.
function hideReleaseBanner() {
  const banner = $('#release-banner');
  if (banner) banner.hidden = true;

  stopRedrawing('release');
}

function drawReleaseBanner(state) {
  const banner = $('#release-banner');
  if (!banner) return;

  const text = $('#release-text');

  text.textContent = '';

  // The version is the link, and the sentence is built around it rather than
  // followed by a button.
  //
  // There was a button beside this reading "Open Settings". Two things to read
  // where one would do, and the one it left out is the one somebody actually
  // wants to press: a release has a number, and the number is what they are
  // deciding about. So the sentence is split at the place the number goes and
  // the number is put there as the control.
  //
  // Split on the placeholder itself rather than on a marker put through the
  // filler: where the number falls in the sentence is the translator's
  // business, and the two halves are whatever they wrote around it.
  const [before, after] = t('update.available',
    'Version {0} is available. This installation runs {1}.').split('{0}');

  text.append(before ?? '');

  const link = el('button', {
    type: 'button',
    class: 'link-button',
    id: 'release-open',
    text: state.latest,
  });

  link.addEventListener('click', openTheVersionCard);
  text.append(link);

  // The rest of the sentence still has {1} in it, which is the version running.
  text.append(fillIn(after ?? '', [state.latest, state.running]));

  banner.hidden = false;
}

/** Takes whoever pressed it to the card that installs the new version. */
function openTheVersionCard() {
  switchView('admin');

  const card = $('#update-card');
  if (card) card.scrollIntoView({ block: 'center' });
}

// --------------------------------------------------------------- announcements

/**
 * The open connection the server writes down when something is about to happen
 * to the application itself.
 *
 * Everything else here is a question this page asked. This is the one thing that
 * cannot be: an update replaces the binary underneath and, where the platform
 * allows it, restarts into it seconds later. Somebody in the middle of typing an
 * entry finds out by the application vanishing.
 *
 * EventSource rather than a shorter poll. A poll fast enough to be a warning is a
 * request per second per open tab, for ever, to say "nothing" - and it would
 * still be late. This costs one idle connection and arrives at once. It also
 * reconnects by itself, which is what turns the restart into something the page
 * can notice and recover from rather than a wall of failed requests.
 */
let announcements = null;

/** What was last announced, so a reconnection knows what it is coming back from. */
let announced = null;

function startAnnouncements() {
  stopAnnouncements();

  if (!me.user || typeof EventSource !== 'function') return;

  announcements = new EventSource('/api/v1/events');

  announcements.addEventListener('announcement', (event) => {
    let announcement;

    try {
      announcement = JSON.parse(event.data);
    } catch {
      return;
    }

    applyAnnouncement(announcement);
  });

  // The other thing this connection carries, and the only one that is about the
  // account holding it rather than about the installation.
  //
  // Here rather than in a poll of its own because the connection is already open
  // and already belongs to this account, so the answer costs nothing to receive
  // and arrives at the moment it becomes true. That is the whole point: the
  // person a right was taken from is, by definition, not the person clicking
  // around to find out.
  announcements.addEventListener('permissions', (event) => {
    let change;

    try {
      change = JSON.parse(event.data);
    } catch {
      return;
    }

    noticePermissionChange(change.revision);
  });

  // The connection dropping is not an error to report. It is the ordinary way a
  // restart looks from here, and the browser is already reconnecting - which is
  // handled where the reconnection succeeds rather than here, because a failure
  // to reconnect means the application is not back yet and there is nothing to
  // say to anybody about that.
  //
  // It is, however, the first thing a screen notices when the installation goes
  // out of service: this stream is one of the requests maintenance mode refuses,
  // and the browser retries it every few seconds. Everything else an idle screen
  // asks for is exempt - who you are, what the branding is - so without this a
  // screen nobody is touching would sit there working-looking until its next
  // click, or until the once-a-minute permission poll came round.
  announcements.addEventListener('error', () => { void checkWhetherStillWelcome(); });

  announcements.addEventListener('open', () => {
    // Back. If the last thing said was that a restart was coming, this is the
    // other side of it: the application is answering again, and it is a different
    // version from the one this page was loaded from.
    //
    // Reloading is the only honest thing to do. The script, the stylesheet and
    // the markup in this tab all came from the previous version, and carrying on
    // with them against a new API is how a page ends up half working in ways
    // nobody can reproduce.
    if (announced === 'update.restarting') {
      announced = null;
      window.location.reload();
    }
  });
}

function stopAnnouncements() {
  if (announcements) announcements.close();
  announcements = null;
  announced = null;

  stopRedrawing('announcement');

  const banner = $('#update-banner');
  if (banner) banner.hidden = true;
}

/**
 * Puts an announcement on screen.
 *
 * A banner rather than a toast: a toast fades, and this has to be true for as
 * long as it is true. Nothing is dismissable either - what it describes does not
 * stop being the case because somebody closed it.
 */
function applyAnnouncement(announcement) {
  // Maintenance is not one of the update banner's messages and must not be
  // written into it - nor recorded as the last thing announced, which is what
  // the reconnection handler reads to decide whether a restart just finished.
  //
  // It carries nothing of its own on purpose: what the state is, and what the
  // administrator wrote about it, is read from /maintenance, which keeps
  // answering while everything else is refused.
  if (announcement.kind === 'instance.maintenance') {
    void checkWhetherStillWelcome();
    void loadMaintenance();

    return;
  }

  announced = announcement.kind;

  // Through the redraw registry, so a language change reaches it like everything
  // else the script writes. Without this the banner kept whatever words it was
  // built with while the page around it changed language - and this is the one
  // message on the screen that somebody may be reading precisely because they do
  // not understand what is happening.
  redrawable('announcement', () => drawAnnouncement(announcement));
}

/** Writes one announcement into the banner, in the language now in force. */
function drawAnnouncement(announcement) {
  const banner = $('#update-banner');
  if (!banner) return;

  const version = announcement.version || '';

  const words = {
    'update.installing': () => t('announce.installing',
      'A new version ({0}) is being installed. You can carry on working; ' +
      'the application will restart shortly.'),
    'update.restarting': () => t('announce.restarting',
      'The application is restarting into version {0}. This page will reload ' +
      'by itself in a moment.'),
    'update.pending': () => t('announce.pending',
      'Version {0} has been installed and takes effect the next time the ' +
      'application is started. Nothing changes until then.'),
    'update.cancelled': () => t('announce.cancelled',
      'The update was not installed and nothing has changed. The application ' +
      'keeps running as before.'),
  }[announcement.kind];

  if (!words) {
    banner.hidden = true;

    return;
  }

  banner.textContent = fillIn(words(), [version]);
  banner.hidden = false;

  // The two that are over: an update that was abandoned, and one that will not
  // happen until somebody restarts the application by hand. Both are worth
  // saying once and neither is worth a permanent stripe across the screen.
  if (announcement.kind === 'update.cancelled' || announcement.kind === 'update.pending') {
    setTimeout(() => {
      if (announced !== announcement.kind) return;

      banner.hidden = true;
      stopRedrawing('announcement');
      announced = null;
    }, 30000);
  }
}

/**
 * Whether a failed request is the restart everybody was just warned about.
 *
 * During those few seconds every request fails, and each one would otherwise
 * raise its own red toast on top of a banner that already explains it. The
 * banner is the message; the toasts would be noise on top of it.
 */
function duringARestart() {
  return announced === 'update.restarting';
}

function toast(message, kind = 'ok', detail = '') {
  const stack = $('#toast');
  if (!stack) return;

  const note = el('div', { class: `toast-note ${kind}` });

  // Errors are announced assertively and successes politely: an error is worth
  // interrupting whatever a screen reader was saying, and "Saved" is not.
  note.setAttribute('role', kind === 'error' ? 'alert' : 'status');

  note.append(el('span', { class: 'toast-text', text: message }));

  // The words of whatever actually failed, folded away. Shown open it would be a
  // paragraph of somebody else's English across the corner of the screen for
  // every reader who cannot use it; left out altogether it would be gone by the
  // time the person who can use it is asked.
  if (detail) note.append(detailDisclosure(detail));

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

  setTimeout(() => {
    // Not while somebody is reading the detail they just opened. The timer is
    // there so a notice nobody is looking at goes away by itself, and one that
    // has been unfolded is the opposite of that.
    if (note.querySelector('details[open]')) return;

    note.remove();
  }, linger);
}

/** How many notices may be on screen, and how long each one stays. */
const TOAST_LIMIT = 4;
const TOAST_MIN_MS = 4000;
const TOAST_MAX_MS = 20000;
const TOAST_MS_PER_CHAR = 60;

/**
 * The placeholders an administrator may put in the texts they configure.
 *
 * The banner, the footer and the legal notice are written once and then read for
 * years, and the things that date them are exactly the things nobody remembers to
 * come back and change: a copyright year, a version number, the instance's own
 * name after somebody renames it. So those are written as a placeholder and
 * worked out when the page is drawn.
 *
 * Deliberately few. Every one of these is a thing somebody would otherwise type
 * as a literal and be wrong about later; anything else belongs in the text
 * itself.
 */
function placeholderValues() {
  const now = new Date();

  return {
    year: String(now.getFullYear()),
    date: fmtDate(todayISO()),
    time: new Intl.DateTimeFormat(activeLocale(), {
      hour: '2-digit', minute: '2-digit',
    }).format(now),
    version: lastBranding.version ?? '',
    instance: lastBranding.title || 'Time Recording',
  };
}

/**
 * Writes a configured text into an element: placeholders filled in, links made
 * into links, and everything else left as the words somebody typed.
 *
 * Not a rich text editor, and not HTML. The three texts this renders are shown on
 * the sign-in screen, before anybody has authenticated - so whatever an
 * administrator writes here is rendered for every visitor, including the next
 * administrator. Accepting HTML would mean accepting a script tag from anyone
 * holding settings:manage, and sanitising HTML correctly is a problem that is
 * never finished.
 *
 * What is actually wanted is a date that stays current and the occasional link.
 * Both fit in a grammar small enough to read in one line: [words](address). It is
 * built as DOM nodes rather than markup, so there is no parser to be wrong and
 * nothing is ever assigned as innerHTML.
 */
function renderConfiguredText(target, text) {
  if (!target) return;

  target.replaceChildren();

  const filled = fillPlaceholders(text ?? '');

  if (!filled) {
    target.hidden = true;

    return;
  }

  target.hidden = false;

  // [label](url), and everything between the matches as plain text.
  const pattern = /\[([^\]]+)\]\(([^)\s]+)\)/g;
  let at = 0;

  for (const match of filled.matchAll(pattern)) {
    if (match.index > at) {
      target.append(document.createTextNode(filled.slice(at, match.index)));
    }

    target.append(configuredLink(match[1], match[2]));
    at = match.index + match[0].length;
  }

  if (at < filled.length) target.append(document.createTextNode(filled.slice(at)));
}

/** Replaces {year} and its companions with what they mean right now. */
function fillPlaceholders(text) {
  const values = placeholderValues();

  return String(text).replace(/\{(\w+)\}/g, (whole, name) => (
    Object.hasOwn(values, name) ? values[name] : whole
  ));
}

/**
 * One link, or the words on their own where the address is not one this should
 * follow.
 *
 * Three schemes and no others. javascript: is the obvious one to refuse and data:
 * is the one that gets forgotten - both would turn a line of configured text into
 * something that runs.
 */
function configuredLink(label, url) {
  let parsed;

  try {
    parsed = new URL(url, window.location.origin);
  } catch {
    return document.createTextNode(label);
  }

  if (!['http:', 'https:', 'mailto:'].includes(parsed.protocol)) {
    return document.createTextNode(label);
  }

  return el('a', {
    href: parsed.href,
    text: label,
    // Somebody else's page opened from ours has no business reaching back into
    // it, and an administrator's link is still somebody else's page.
    rel: 'noreferrer noopener',
    target: '_blank',
  });
}

/** Builds an element; text is assigned via textContent, never innerHTML. */
/**
 * The attributes whose presence is the whole of their meaning.
 *
 * setAttribute('disabled', false) disables the control: the value is never read,
 * only whether the attribute is there at all. Every right on the roles screen was
 * rendered with disabled="false" and could not be ticked - so no role could be
 * given a permission through the interface, which is a feature that had simply
 * stopped working.
 */
const BOOLEAN_ATTRIBUTES = new Set([
  'disabled', 'readonly', 'required', 'hidden', 'multiple', 'selected',
  'autofocus', 'open', 'novalidate',
]);

function el(tag, props = {}, ...children) {
  const node = document.createElement(tag);
  for (const [key, value] of Object.entries(props)) {
    if (key === 'class') node.className = value;
    else if (key === 'text') node.textContent = value;
    else if (key === 'checked') node.checked = value;
    else if (key.startsWith('on')) node.addEventListener(key.slice(2), value);
    // Present or absent, never "false". aria-* is deliberately not in that set:
    // there the string is read, and aria-current="false" means something.
    else if (BOOLEAN_ATTRIBUTES.has(key)) { if (value) node.setAttribute(key, ''); }
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

/**
 * Points the browser tab at the instance's logo, after it has just changed.
 *
 * The document already arrives with the right icon: the server writes the link
 * into the page, at an address carrying a fingerprint of what it answers with. So
 * on every load there is nothing to do here, and this exists for the one moment
 * that is not a load - somebody has just saved a new logo and is still looking at
 * the page they saved it from.
 *
 * This used to do the whole job, and could not. An icon patched in after the
 * document was parsed is honoured by some engines and ignored by others; the logo
 * was correct in the DOM, correct in the tests, and not in the tab. What every
 * engine does honour is a fetch of a URL it has not seen before, which is what
 * changing the fingerprint gives it.
 */
function applyFavicon(logo) {
  const link = document.querySelector('link[rel~="icon"]');
  if (!link) return;

  const wanted = `/favicon?v=${faviconFingerprint(logo ?? '')}`;

  if (link.getAttribute('href') === wanted) return;

  // Replaced rather than pointed elsewhere: setting .href on the existing element
  // is the mutation engines disagree about honouring.
  const fresh = document.createElement('link');
  fresh.rel = 'icon';
  fresh.href = wanted;

  document.head.append(fresh);
  link.remove();
}

/**
 * The same fingerprint the server puts in the address.
 *
 * It has to match, or every load would swap the link the document arrived with
 * for an identical one - a fetch, a repaint and a flicker for nothing. The server
 * hashes the logo; this cannot, so it keeps what the server sent and compares
 * against that.
 */
let serverFaviconVersion = null;

function faviconFingerprint(logo) {
  if (serverFaviconVersion === null) {
    const link = document.querySelector('link[rel~="icon"]');
    const href = link?.getAttribute('href') ?? '';

    serverFaviconVersion = new URLSearchParams(href.split('?')[1] ?? '').get('v') ?? '';
    lastFaviconLogo = logo;
  }

  // Unchanged since the document was served: keep the server's own version, so
  // nothing is refetched.
  if (logo === lastFaviconLogo) return serverFaviconVersion;

  lastFaviconLogo = logo;

  // Changed while the page was open. The value only has to differ from the last
  // one for the browser to fetch again; the server decides what comes back.
  serverFaviconVersion = String(Date.now());

  return serverFaviconVersion;
}

/**
 * Keeps the reserved space under the sticky bar equal to the bar.
 *
 * The stylesheet reserves scroll-padding-top so that anything scrolled to the top
 * of the window lands below the bar rather than behind it - a click, a keyboard
 * focus, an in-page anchor, scrollIntoView. That reservation was a number
 * somebody measured once.
 *
 * The bar is a wrapping flex row. Its height depends on the width of the window
 * and on everything in it, so a narrow window turns one row into three, and the
 * reservation silently stops covering it. What that looks like is a tab that does
 * not open: the click lands on the bar sitting over it.
 *
 * So it is measured, and re-measured whenever the bar changes shape.
 */
function trackTopbarHeight() {
  const bar = document.querySelector('.topbar');
  if (!bar) return;

  const apply = () => {
    const height = Math.ceil(bar.getBoundingClientRect().height);
    if (height > 0) document.documentElement.style.setProperty('--topbar-height', `${height}px`);
  };

  apply();

  // Everything that changes it: the window's width, a logo arriving, a tab
  // appearing when rights change, a language whose words are longer.
  if (typeof ResizeObserver === 'function') {
    new ResizeObserver(apply).observe(bar);

    return;
  }

  window.addEventListener('resize', apply);
}

/**
 * One of the configured texts, in the language the reader is being served.
 *
 * The rule is the one that governs everything else about language here: what the
 * switcher says, and the browser's own setting when nobody has switched. A
 * language with nothing written for it falls back to the base text rather than to
 * nothing - an installation that works in one language wrote that one, and a
 * German reader is better served by a German company's German banner than by a
 * blank strip.
 */
function brandingIn(branding, field) {
  const written = branding.translations?.[activeLanguage()]?.[field];

  return written || branding[field] || '';
}

/** What a language is called, in its own words. */
function languageName(language) {
  return { en: 'English', de: 'Deutsch' }[language] ?? language;
}

/**
 * Choosing which part of the logo one place uses.
 *
 * A logo is uploaded once and drawn in three places that want three different
 * things, and the part worth showing differs between them: a wide header can take
 * the whole wordmark, a browser tab cannot - sixteen pixels of a two-to-one
 * wordmark is a smear, and what is worth keeping there is usually the mark at one
 * end. Nobody can guess which end, so it is chosen here, per place.
 *
 * The selection is free: any part, in any shape, from any corner. It opens on
 * the shape of the place it is for, because that is the one selection nothing has
 * to be done to, but nothing holds it there.
 *
 * A chosen part that is not the place's shape is fitted into that place rather
 * than stretched to fill it - scaled until it fits and left at its own
 * proportions, which is what the header and the sign-in banner do with it and
 * what the tab's icon is padded out to. So the previews show it fitted too, and
 * what is on this screen is what will be on the others.
 */
const CROP_SHAPES = {
  header: { width: 440, height: 80, label: () => t('admin.logoInHeader', 'In the header') },
  banner: { width: 656, height: 192, label: () => t('admin.logoOnSignIn', 'On the sign-in screen') },
  icon: { width: 64, height: 64, label: () => t('admin.logoAsFavicon', 'In the browser tab') },
};

/** What has been chosen, per place, while the form is open. */
let logoCrops = {};

/** Which place the chooser is currently open for. */
let croppingFor = '';

/** The selection, in fractions of the image, while it is being dragged. */
let cropBox = { x: 0, y: 0, w: 1, h: 1 };

/**
 * The smallest selection either side may be dragged to.
 *
 * Not a matter of taste: a selection dragged to nothing is one that cannot be
 * seen, cannot be grabbed again, and would be scaled up into a place from almost
 * no pixels at all.
 */
const MIN_CROP = 0.02;

function openCropChooser(use) {
  const overlay = $('#crop-overlay');
  const image = $('#crop-image');
  if (!overlay || !image || !pendingLogo) return;

  croppingFor = use;
  image.src = pendingLogo;

  $('#crop-text').textContent = fillIn(
    t('crop.text', 'Drag to choose the part of the logo used here: {0}.'),
    [CROP_SHAPES[use]?.label() ?? use]);

  // Shown first, and measured after.
  //
  // A hidden element has no size, so a selection drawn before this line came out
  // as the two pixels of its own border - present, correct in every way a test
  // could ask about, and impossible to aim at.
  overlay.hidden = false;

  // Whatever was chosen before, or - with nothing chosen yet - the largest box
  // of this place's shape that fits, which for a wide header is the whole logo
  // and for a tab is a square of it. A starting point rather than a rule: it is
  // the one selection that needs no fitting, and every corner drags away from it.
  const begin = () => {
    cropBox = logoCrops[use] ?? largestBoxOfShape(use, image);
    drawCropBox();
  };

  if (image.complete && image.naturalWidth) begin();
  else image.onload = begin;

  // Again on the next frame: the image may be decoded already while the overlay
  // it was just put into has not been laid out yet, and the first measurement
  // would then be of a box with no width.
  requestAnimationFrame(() => {
    if (croppingFor === use && image.naturalWidth) drawCropBox();
  });
}

/**
 * The largest box of the wanted shape that fits inside the image, centred.
 *
 * The starting point when nothing has been chosen, and what "the whole logo"
 * means for a place whose shape the logo does not have: a square tab cannot show
 * a two-to-one wordmark whole, so the honest answer is as much of it as a square
 * can hold.
 */
function largestBoxOfShape(use, image) {
  const shape = CROP_SHAPES[use];

  if (!shape || !image.naturalWidth || !image.naturalHeight) {
    return { x: 0, y: 0, w: 1, h: 1 };
  }

  const wanted = shape.width / shape.height;
  const actual = image.naturalWidth / image.naturalHeight;

  if (actual > wanted) {
    const w = wanted / actual;

    return { x: (1 - w) / 2, y: 0, w, h: 1 };
  }

  const h = actual / wanted;

  return { x: 0, y: (1 - h) / 2, w: 1, h };
}

/** Puts the selection on screen, where the image actually is. */
function drawCropBox() {
  const image = $('#crop-image');
  const box = $('#crop-box');
  if (!image || !box) return;

  // Measured against the drawn image rather than its element: object-fit leaves
  // bands at the sides or the top, and a selection placed against the element
  // would be off by exactly those bands.
  const area = drawnImageArea(image);

  box.style.left = `${area.left + cropBox.x * area.width}px`;
  box.style.top = `${area.top + cropBox.y * area.height}px`;
  box.style.width = `${cropBox.w * area.width}px`;
  box.style.height = `${cropBox.h * area.height}px`;
}

/** Where inside its element the image is actually drawn. */
function drawnImageArea(image) {
  const width = image.clientWidth;
  const height = image.clientHeight;

  if (!image.naturalWidth || !image.naturalHeight) {
    return { left: 0, top: 0, width, height };
  }

  const scale = Math.min(width / image.naturalWidth, height / image.naturalHeight);
  const drawnWidth = image.naturalWidth * scale;
  const drawnHeight = image.naturalHeight * scale;

  return {
    left: image.offsetLeft + (width - drawnWidth) / 2,
    top: image.offsetTop + (height - drawnHeight) / 2,
    width: drawnWidth,
    height: drawnHeight,
  };
}

function wireCropChooser() {
  const overlay = $('#crop-overlay');
  const stage = $('#crop-stage');
  const box = $('#crop-box');
  const handles = $$('.crop-handle');
  if (!overlay || !stage || !box || !handles.length) return;

  for (const button of $$('.logo-use-button')) {
    button.addEventListener('click', () => openCropChooser(button.dataset.crop));
  }

  const close = () => {
    overlay.hidden = true;
    croppingFor = '';
  };

  $('#crop-cancel').addEventListener('click', close);

  $('#crop-whole').addEventListener('click', () => {
    // The whole logo for this place, which for a square tab means as much of it
    // as a square can hold rather than a squashed wordmark.
    delete logoCrops[croppingFor];
    setLogoPreview(pendingLogo);
    close();
  });

  $('#crop-apply').addEventListener('click', () => {
    logoCrops[croppingFor] = { ...cropBox };
    setLogoPreview(pendingLogo);
    close();
  });

  // Dragging the box moves it; dragging a corner resizes it from that corner.
  // Both stay inside the image, and neither constrains the shape.
  let mode = '';
  let corner = '';
  let from = { x: 0, y: 0 };
  let start = cropBox;

  const grab = (kind) => (e) => {
    mode = kind;
    corner = e.currentTarget.dataset.corner ?? '';
    from = { x: e.clientX, y: e.clientY };
    start = { ...cropBox };

    e.preventDefault();
    e.stopPropagation();
    stage.setPointerCapture(e.pointerId);
  };

  box.addEventListener('pointerdown', grab('move'));

  for (const grip of handles) {
    grip.addEventListener('pointerdown', grab('resize'));
  }

  stage.addEventListener('pointermove', (e) => {
    if (!mode) return;

    const area = drawnImageArea($('#crop-image'));
    if (!area.width || !area.height) return;

    const dx = (e.clientX - from.x) / area.width;
    const dy = (e.clientY - from.y) / area.height;

    cropBox = mode === 'move'
      ? moveCropBox(start, dx, dy)
      : resizeCropBox(start, dx, dy, corner);

    drawCropBox();
  });

  for (const ending of ['pointerup', 'pointercancel']) {
    stage.addEventListener(ending, () => { mode = ''; });
  }

  // The stage is a share of the window, so the selection has to be redrawn when
  // that changes - otherwise it keeps the pixels it had and points at the wrong
  // part of the logo.
  window.addEventListener('resize', () => {
    if (croppingFor) drawCropBox();
  });
}

/** Moves the selection, keeping it inside the image. */
function moveCropBox(start, dx, dy) {
  return {
    ...start,
    x: Math.min(Math.max(start.x + dx, 0), 1 - start.w),
    y: Math.min(Math.max(start.y + dy, 0), 1 - start.h),
  };
}

/**
 * Resizes the selection from one of its corners, freely.
 *
 * The corner being dragged follows the pointer on both axes and the opposite one
 * stays where it is, so any part of the logo can be enclosed in any shape. The
 * two axes are worked out separately - a corner is two edges, and holding one of
 * them back because the other ran into the image's edge would make the drag stick
 * for reasons nothing on the screen explains.
 */
function resizeCropBox(start, dx, dy, corner) {
  const right = start.x + start.w;
  const bottom = start.y + start.h;

  // Held between the image's edge and the smallest selection worth having, which
  // is what keeps a corner dragged past its opposite from turning the selection
  // inside out.
  const [x, w] = corner.includes('w')
    ? (() => {
      const moved = Math.min(Math.max(start.x + dx, 0), right - MIN_CROP);

      return [moved, right - moved];
    })()
    : [start.x, Math.min(Math.max(start.w + dx, MIN_CROP), 1 - start.x)];

  const [y, h] = corner.includes('n')
    ? (() => {
      const moved = Math.min(Math.max(start.y + dy, 0), bottom - MIN_CROP);

      return [moved, bottom - moved];
    })()
    : [start.y, Math.min(Math.max(start.h + dy, MIN_CROP), 1 - start.y)];

  return { x, y, w, h };
}

/** The languages the appearance texts can be written in. */
const BRANDING_LANGUAGES = ['en', 'de'];

/** The texts that translate, per language, while the form is open. */
let brandingDraft = {};

/**
 * The texts as they were before any translation existed.
 *
 * They stay the base: what a reader gets when their language has nothing written
 * for it, and what the server puts in the document before anybody has chosen a
 * language. An installation working in one language fills these in and never
 * opens the switcher.
 */
let brandingBase = {};

/** The fields the language switcher swaps. */
const BRANDING_TEXT_FIELDS = ['title', 'tabTitle', 'banner', 'footerText', 'legalNotice'];

/**
 * Puts one language's texts into the form, keeping whatever was typed in the one
 * being left.
 */
function showBrandingLanguage(language) {
  // What is in the boxes belongs to the language it was typed in, and has to be
  // taken back into the draft before another language's words replace it.
  rememberBrandingDraft();
  fillBrandingBoxes(language);
}

/**
 * Puts one language's words into the boxes, without taking anything back first.
 *
 * Split out of showBrandingLanguage for the one caller that must not take
 * anything back: restoring a draft after a reload has just put the whole set of
 * languages into place, and the boxes at that moment hold the server's copy.
 * Capturing them would file the server's words under whichever language was
 * about to be shown and lose what had actually been written - which is what it
 * did, silently, in the half of this that was not being looked at.
 */
function fillBrandingBoxes(language) {
  const form = $('#form-branding');
  const picker = $('#branding-language');
  if (!form || !picker) return;

  const chosen = BRANDING_LANGUAGES.includes(language) ? language : BRANDING_LANGUAGES[0];

  picker.value = chosen;
  brandingLanguage = chosen;

  for (const field of BRANDING_TEXT_FIELDS) {
    // The base where this language has nothing of its own, so somebody adding a
    // translation starts from what is already on the screen rather than from an
    // empty box.
    form.elements[field].value = brandingDraft[chosen]?.[field] || brandingBase[field] || '';
  }
}

/** Which language the form is currently showing. */
let brandingLanguage = 'en';

/**
 * What a reader gets in a language nothing has been written for.
 *
 * The first language's box is the base itself, so it is read from there while
 * the form is open and from what was loaded before anybody typed.
 */
function brandingFallback(field) {
  return brandingDraft[BRANDING_LANGUAGES[0]]?.[field] || brandingBase[field] || '';
}

/** Takes what is in the form back into the draft for the language it belongs to. */
function rememberBrandingDraft() {
  const form = $('#form-branding');
  if (!form || !brandingDraft[brandingLanguage]) return;

  for (const field of BRANDING_TEXT_FIELDS) {
    const typed = form.elements[field].value;

    // The first language is the base, so whatever is in the form is it.
    if (brandingLanguage === BRANDING_LANGUAGES[0]) {
      brandingDraft[brandingLanguage][field] = typed;

      continue;
    }

    // Any other language holds a translation, and one that says exactly what
    // the base says is not a translation - it is the base, copied.
    //
    // Storing it froze the language. The form fills each language with the base
    // so that somebody switching to German sees what a German reader currently
    // gets; saving then wrote that back as German's own answer, and renaming the
    // installation afterwards reached every language except the ones that had
    // been looked at. Leaving a field as it was found has to mean what it looks
    // like it means: nothing written here, so the default applies - the same
    // rule the logo follows.
    brandingDraft[brandingLanguage][field] = typed === brandingFallback(field) ? '' : typed;
  }
}

/** What the icon currently shown was made from. */
let lastFaviconLogo = null;

function fmtDate(iso) {
  if (!iso) return '–';

  const [y, m, d] = iso.split('-').map(Number);
  const at = new Date(Date.UTC(y, m - 1, d));

  if (Number.isNaN(at.getTime())) return iso;

  return new Intl.DateTimeFormat(activeLocale(), {
    day: '2-digit', month: '2-digit', year: 'numeric', timeZone: 'UTC',
  }).format(at);
}

/**
 * A moment - a day and a time of day - in the reader's own convention.
 *
 * Deliberately not fmtDate, and the difference is not cosmetic. That one renders a
 * calendar day and pins the zone to UTC, because a booking's day carries no zone
 * and must not be shifted by one. These are instants, and the question they answer
 * - when was this credential last used - only means anything in the zone the
 * reader lives in.
 *
 * fmtDate cannot be reused for them in any case: it splits an ISO string on its
 * hyphens, so a timestamp makes its third field NaN and the raw wire format
 * reaches the screen.
 *
 * Minutes and no finer. A token's last use is read to work out whether somebody
 * else has it, and seconds add nothing to that while making the column wider than
 * the answer.
 */
function fmtMoment(iso) {
  if (!iso) return '–';

  const at = new Date(iso);

  if (Number.isNaN(at.getTime())) return iso;

  return new Intl.DateTimeFormat(activeLocale(), {
    day: '2-digit', month: '2-digit', year: 'numeric',
    hour: '2-digit', minute: '2-digit',
    // The account's own zone, falling back to the browser's before sign-in and on
    // an installation that never chose one.
    timeZone: me.user?.effectiveTimezone || undefined,
  }).format(at);
}

/**
 * A figure with two decimal places, written the way the reader writes one.
 *
 * toFixed always produces a dot, so every hour figure on screen read 5.00 h to a
 * German reader looking at a screen that was otherwise entirely German - beside
 * date fields that did use the right convention, which is what made it look like
 * a rendering fault rather than a decision.
 *
 * Display only. Nothing here goes back to the server or into a form field: a
 * comma would be a different number to JSON, and the one place a comma is read
 * *in* is the spreadsheet importer, which handles both conventions on the way
 * through.
 */
function fmtNumber(n) {
  if (typeof n !== 'number' || !Number.isFinite(n)) return String(n ?? '');

  return new Intl.NumberFormat(activeLocale(), {
    minimumFractionDigits: 2, maximumFractionDigits: 2,
  }).format(n);
}

/**
 * A number of hours, with the unit the reader uses for one.
 *
 * The unit was a literal "h" appended here, which is a word - a short one that
 * happens to be the same in English and German, which is exactly why it survived
 * every pass over the translations. It is looked up like anything else now, so a
 * language that abbreviates hours differently has somewhere to say so.
 */
const fmtHours = (n) => `${fmtNumber(n)} ${t('unit.hours', 'h')}`;

/** Renders a signed balance, coloured as a credit or a debt. */
function balanceCell(value) {
  const sign = value > 0 ? '+' : '';
  return el('td', {
    class: `num ${value > 0 ? 'plus' : value < 0 ? 'minus' : ''}`,
    text: `${sign}${fmtHours(value)}`,
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
      // No text here: refreshBulkBar writes it, and writes it again every time
      // the bar is refreshed. Set once at creation it survived a language change
      // as the old language's word, over a count beside it in the new one.
      class: 'danger',
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
  bar.querySelector('button').textContent = t('bulk.delete', 'Delete selected');

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

  // Marked by the thing that fills it, so signing out knows which lists hold an
  // account's own data and which came with the markup. See forgetTheLastAccount:
  // emptying every select there would take the log levels, the languages and the
  // appearance with them, and nothing refills those.
  select.dataset.filled = '';

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
 * The English titles of the roles that ship.
 *
 * These are what an English reader sees, because English is what a t() fallback is for.
 * Without them the fallback would be the identifier itself, and "user-admin" is not a
 * thing to put in front of somebody deciding what a colleague may do.
 *
 * A role an installation named itself is not in here and falls back to the name it was
 * given, which is the only sensible answer for a word this application has never seen.
 */
/**
 * What each right is called, in words.
 *
 * The identifiers are what the API takes and what a role's rights are stored as -
 * "timesheets:write:own" - and they were what the ticking boxes said. That asks
 * somebody deciding what a colleague may do to read a namespace: the difference
 * between "projects:write" and "projects:archive" is plain once you know the
 * system and a guess before that.
 *
 * The identifier stays beside the words rather than being replaced by them. It is
 * what an API caller and a directory configuration deal in, so the screen that
 * hands rights out is the one place it has to stay readable.
 *
 * English here, translated through perm.<right>; the fallback is what an
 * installation without a translation sees.
 */
const PERMISSION_TITLES = {
  'users:read': 'See accounts',
  'users:write': 'Create and change accounts',
  'users:delete': 'Delete accounts',
  'roles:read': 'See roles',
  'roles:write': 'Create and change roles',
  'settings:manage': 'Administer the installation',
  'projects:read': 'See projects',
  'projects:write': 'Create and change projects',
  'projects:delete': 'Delete projects',
  'projects:archive': 'Archive projects',
  'timesheets:read:own': 'See own time',
  'timesheets:write:own': 'Record own time',
  'timesheets:transfer': 'Move time between projects',
  'reports:read:own': 'Evaluate own hours',
  'settings:write:own': 'Change own settings',
};

/**
 * What each right actually allows, for the legend.
 *
 * One sentence, and specific: "See accounts" leaves open whether that includes
 * their recorded time, which is exactly the question somebody handing out rights
 * is trying to answer.
 */
const PERMISSION_DETAILS = {
  'users:read': 'The list of accounts, their addresses and which role each holds. Not their recorded time - nobody sees anybody else\u2019s.',
  'users:write': 'Add an account, change a name or an address, assign a role. Not the initial password of an account somebody else set up.',
  'users:delete': 'Remove an account. Its recorded time goes with it.',
  'roles:read': 'The roles on this screen and what each grants.',
  'roles:write': 'Make a role, change what it grants, remove one nobody holds. The roles shipped with the application stay as they are.',
  'settings:manage': 'The installation itself: the database connection, the directory, appearance, maintenance mode, telemetry, the log and the restart. This is the right that makes somebody an administrator.',
  'projects:read': 'The list of projects and what each is called.',
  'projects:write': 'Make a project and change one.',
  'projects:delete': 'Remove a project. Time booked on it keeps its hours and loses the project.',
  'projects:archive': 'Close a project to new bookings without deleting what is on it.',
  'timesheets:read:own': 'One\u2019s own entries, calendar and balance. There is no right that opens somebody else\u2019s.',
  'timesheets:write:own': 'Record, correct and delete one\u2019s own time, by hand or with the stopwatch.',
  'timesheets:transfer': 'Move one\u2019s own entries from one project to another, in bulk.',
  'reports:read:own': 'The report and the charts, over one\u2019s own hours.',
  'settings:write:own': 'One\u2019s own account: password, language, appearance, timezone, working times, two-factor.',
};

/** The areas rights are grouped under, in the order they are shown. */
const PERMISSION_GROUPS = {
  users: 'Accounts',
  roles: 'Roles',
  settings: 'Settings',
  projects: 'Projects',
  timesheets: 'Time',
  reports: 'Evaluations',
};

/** What a right is called, in the reader's language. */
function permissionTitle(right) {
  return t(`perm.${right}`, PERMISSION_TITLES[right] ?? right);
}

/** What a right allows, in the reader's language. */
function permissionDetail(right) {
  return t(`perm.desc.${right}`, PERMISSION_DETAILS[right] ?? '');
}

/** Which area a right belongs to, from the identifier's first part. */
function permissionGroup(right) {
  const area = String(right).split(':')[0];

  return { key: area, label: t(`perm.group.${area}`, PERMISSION_GROUPS[area] ?? area) };
}

// The role somebody gets when nobody has said otherwise: the one that records
// time and administers nothing.
const ORDINARY_ROLE = 'user';

const SHIPPED_ROLE_TITLES = {
  admin: 'Administrator',
  user: 'User',
  'user-admin': 'User & administrator',
};

/**
 * What a role is called, in the reader's language.
 *
 * The name in the database is an identifier: lowercase, hyphenated, English - admin,
 * user, user-admin. It is what the API takes, what the directory configuration stores
 * and what the role editor edits. It is not what somebody choosing a colleague's role
 * should have to read, and it was: the dropdown, the account list, the greeting and the
 * confirmation before deleting a role all showed it raw, so a German reader picked
 * between two lowercase English words.
 *
 * Takes the name rather than the role, because most of the places that show one have
 * only the name - an account carries the name of its role, not the role.
 */
function roleTitle(name) {
  if (!name) return '';

  return t(`role.name.${name}`, SHIPPED_ROLE_TITLES[name] ?? name);
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
 * A dropdown of bare identifiers asks somebody to guess: "user-admin" against "user" is
 * a difference you can only infer, and the difference is whether that person can
 * administer the installation. The list on the Roles screen has always shown the
 * descriptions; the place where the choice is actually made did not.
 *
 * The value stays the identifier, because that is what the API takes.
 */
function roleChoices() {
  // The roles that ship, when nothing has been loaded.
  //
  // The list normally comes from /roles, which is only fetched by somebody
  // holding roles:read - so a picker built from it is empty for anybody who may
  // configure the installation without administering its roles, and empty again
  // whenever that fetch has not happened or did not finish. An empty picker is
  // the one answer that is always wrong: these three are seeded, cannot be
  // deleted, and are therefore always a truthful set of choices.
  const roles = cache.roles.length > 0
    ? cache.roles
    : Object.keys(SHIPPED_ROLE_TITLES).map((name) => ({ name }));

  return roles.map((role) => {
    const title = roleTitle(role.name);
    const purpose = roleDescription(role);

    return { name: role.name, label: purpose ? `${title} — ${purpose}` : title };
  });
}

/** Reads a form into a plain object, dropping empty optional fields. */
/**
 * Where a form's unfinished contents wait out a page load.
 *
 * Reloading is not always a decision. It is a stray F5, a browser deciding the
 * tab has been idle long enough, a certificate prompt, a laptop coming back from
 * sleep - and whatever the reason, everything typed into a form and not yet
 * saved was simply gone, with the form coming back filled from the server as
 * though nobody had touched it. Losing twenty minutes of a legal notice that way
 * is not a small thing, and there is nothing on screen afterwards to say it
 * happened.
 *
 * sessionStorage rather than localStorage, and that is the whole of the rule
 * about how long this lasts: it survives a reload of this tab and dies with the
 * tab. Closing the page is the one way of saying "I am finished with this" that
 * cannot be done by accident, so it is the one that throws the draft away.
 *
 * Cleared on the way out as well, because a draft is the property of whoever
 * typed it and this application does not leave one person's half-written work on
 * screen for the next.
 */
const DRAFT_PREFIX = 'gtr.draft.';

/**
 * Where one account's drafts are kept, apart from anybody else's.
 *
 * A draft is what somebody was part way through writing, and it belongs to
 * them. The store is a browser's, though, and a browser is shared: signing out
 * and letting a colleague sign in on the same machine is an ordinary thing to
 * do at a shared desk, and half a form somebody else had been filling in is
 * theirs to see rather than the next person's.
 *
 * Signing out clears them, which is the first answer and not a complete one: a
 * tab that is closed by a crash, a machine put to sleep and woken by somebody
 * else, a session that ended without anybody pressing anything. So the key
 * carries who wrote it, and a draft belonging to another account is simply not
 * found - whatever did or did not get cleared.
 *
 * Before anybody has signed in there is no account and therefore no draft to
 * restore, which is right: the only form on screen then is the sign-in one, and
 * that one keeps nothing on purpose.
 */
function draftsOf() {
  const who = me.user?.id;

  return who ? `${DRAFT_PREFIX}${who}.` : '';
}

/**
 * The controls a draft is made of.
 *
 * Named ones only: an unnamed control decides what the form shows rather than
 * what it would save, and there is nothing about it worth carrying across a
 * load. Passwords never - a draft is written to storage, and nothing is worth
 * putting a password there for. Files cannot be restored at all: a file input's
 * value is not something a page is allowed to set, for good reasons.
 */
function draftableFields(form) {
  return [...form.elements].filter((field) => field.name
    && !['password', 'file', 'submit', 'button', 'reset'].includes(field.type));
}

/**
 * A form's own identifier, which is not always what form.id answers with.
 *
 * A form exposes its controls as named properties of itself, and those come
 * first: a form holding <input name="id"> answers form.id with that input. Two
 * forms here do - the time entry and the role - so both wrote their draft under
 * the key "gtr.draft.[object HTMLInputElement]", which is one key for two forms.
 * They overwrote each other, and on the way back in the first of them restored
 * whatever the other had left, then wrote its own emptiness over the top.
 *
 * The attribute is not shadowed by anything.
 */
function formKey(form) {
  return form?.getAttribute('id') ?? '';
}

/**
 * Whether a name belongs to several controls rather than to one.
 *
 * A form's elements answer to a name with the control itself where there is one
 * and with a list where there are several - which is how a set of switches is
 * told from a single switch, without either of them having to declare it.
 */
function sharedName(form, name) {
  const found = form.elements[name];

  return Boolean(found) && !('tagName' in found);
}

/** Reads a form into something that can be written down and put back. */
function draftOf(form) {
  const values = {};

  for (const field of draftableFields(form)) {
    if (field.type === 'radio') {
      if (field.checked) values[field.name] = field.value;

      continue;
    }

    // Several boxes under one name are a set, and what is worth keeping is which
    // of them are ticked. A role's rights are fifteen boxes all called
    // "permissions", told apart by what they carry, so writing down one true or
    // false under that name kept whichever box happened to be last - and put
    // every box in the set back agreeing with it. Ticking the rights for a new
    // role and reloading came back with none of them, which is what this was
    // reported as.
    if (field.type === 'checkbox') {
      if (sharedName(form, field.name)) {
        const ticked = values[field.name] ?? [];

        if (field.checked) ticked.push(field.value);

        // Assigned even when nothing is ticked, so an empty set is written down
        // as one rather than as an absence that restores nothing.
        values[field.name] = ticked;
      } else {
        values[field.name] = field.checked;
      }

      continue;
    }

    values[field.name] = field.value;
  }

  // Stamped with the version that wrote it.
  //
  // A draft is a snapshot of a form, and a form is not a fixed thing: fields are
  // added, a default changes, a card starts filling something in that it used to
  // leave blank. A draft written before such a change is not a record of what
  // somebody typed any more - it is a record of how the screen used to behave,
  // and putting it back re-creates that behaviour on top of the new screen.
  //
  // Which is exactly what happened. The connection card learnt to name the
  // database this process is connected to; a draft from the version before that
  // still held the empty type it used to leave, and restoring it put the card
  // back to naming nothing - on an installation that had just been updated to
  // fix precisely that.
  //
  // Dropped rather than migrated. What is lost is one form somebody had half
  // filled in at the moment they updated, and the alternative is carrying every
  // past shape of every form around for ever.
  // The appearance card keeps more than its boxes.
  //
  // Its texts exist once per language, and only the chosen language's are on
  // screen; the rest are held in memory, so a draft made of the boxes alone
  // would come back as the right words filed under the wrong language. What is
  // carried is therefore the whole set and which one is being looked at, and
  // restoring puts the chooser back before the boxes.
  const version = lastBranding.version ?? '';

  // Which controls were chosen rather than merely filled in.
  //
  // One control cares, and the reason is worth stating here rather than only
  // where it is read: the connection card's type decides which other fields
  // exist, so it follows what this installation is actually connected to until
  // somebody picks something else. That marker lives on the element, and an
  // element does not survive a reload - so without carrying it, a card somebody
  // had set to PostgreSQL came back describing the SQLite the environment
  // happens to run, with their host and user still in the boxes beneath it.
  const chosen = draftableFields(form)
    .filter((field) => field.dataset.chosen !== undefined)
    .map((field) => field.name);

  if (formKey(form) === 'form-branding') {
    return { version, values, chosen, brandingDraft, brandingLanguage };
  }

  return { version, values, chosen };
}

/** Writes down what is in a form, for the next load of this tab. */
function rememberDraft(form) {
  const key = formKey(form);

  if (!key || form.dataset.noDraft !== undefined) return;

  try {
    const mine = draftsOf();
    if (!mine) return;

    window.sessionStorage.setItem(mine + key, JSON.stringify(draftOf(form)));
  } catch {
    // Storage refused, or full. Nothing to do and nothing to say: the form on
    // screen is unaffected, and this was only ever insurance against a reload.
  }
}

/** Throws away a form's draft, once it is no longer unfinished. */
function forgetDraft(form) {
  const key = formKey(form);

  if (!key) return;

  try {
    window.sessionStorage.removeItem(draftsOf() + key);
  } catch {
    // Same reasoning as above.
  }
}

/** Throws away every draft in this tab. */
function forgetEveryDraft() {
  try {
    for (const key of Object.keys(window.sessionStorage)) {
      if (key.startsWith(DRAFT_PREFIX)) window.sessionStorage.removeItem(key);
    }
  } catch {
    // Same reasoning as above.
  }
}

/**
 * Where controls that hold typed work but belong to no form are kept.
 *
 * Almost everything somebody fills in here is in a form, and a form is what a
 * draft is filed under. The timer is not: its project and its description sit
 * beside the clock rather than in a form, because starting a timer is a button
 * rather than a submission - and until the timer is started they are the only
 * copy of what somebody has typed. Once it is running the server holds both and
 * gives them back on its own.
 *
 * Declared in the markup with data-keep rather than swept up by selector, so
 * what is carried across a load is a decision somebody made about that control
 * and not a consequence of where it happens to sit. A search box or a filter is
 * a question being asked, not work: they are left out.
 */
const LOOSE_DRAFT_NAME = '(loose)';

/** Writes down the controls marked to be kept. */
function rememberLooseDraft() {
  const values = {};

  for (const field of $$('[data-keep]')) {
    if (field.id && !field.disabled) values[field.id] = field.value;
  }

  try {
    window.sessionStorage.setItem(draftsOf() + LOOSE_DRAFT_NAME, JSON.stringify(values));
  } catch {
    // Storage refused, or full. The screen is unaffected.
  }
}

/** Puts them back. */
function restoreLooseDraft() {
  let values = null;

  try {
    values = JSON.parse(window.sessionStorage.getItem(draftsOf() + LOOSE_DRAFT_NAME) ?? 'null');
  } catch {
    // Unreadable or not there.
  }

  if (!values) return;

  for (const field of $$('[data-keep]')) {
    // Disabled means the screen has put it beyond reach and something else is
    // deciding it - a running timer is filled from the server, and its own
    // answer outranks anything typed before it started.
    if (!field.id || field.disabled || !(field.id in values)) continue;

    field.value = values[field.id];
    field.dispatchEvent(new Event('input', { bubbles: true }));
    field.dispatchEvent(new Event('change', { bubbles: true }));
  }
}

/**
 * Puts back what was in the forms when this tab was last loaded.
 *
 * After the loaders and not before: they fill every form from the server, so a
 * draft restored first would be overwritten by the very thing it exists to
 * survive. Marking the form as being edited is what keeps the loaders off it
 * from here on - see beingEdited.
 *
 * The values are announced the way typing announces them, so everything a form
 * derives from a field follows: which database fields are on screen, which
 * collector box the exporter allows, whether an SSL mode applies at all.
 */
function restoreDrafts() {
  // Whose drafts these are. Nobody's before anybody has signed in, and then
  // there is nothing to put back - see draftsOf.
  const mine = draftsOf();
  if (!mine) return;

  restoreLooseDraft();

  for (const form of $$('form')) {
    const key = formKey(form);

    if (!key || form.dataset.noDraft !== undefined) continue;

    let draft = null;

    try {
      draft = JSON.parse(window.sessionStorage.getItem(mine + key) ?? 'null');
    } catch {
      // Unreadable or not there. Either way there is nothing to put back.
    }

    if (!draft?.values) continue;

    // Written by a version that is no longer running - see draftOf. Cleared as
    // well as skipped, so it does not sit there being skipped for ever.
    if ((draft.version ?? '') !== (lastBranding.version ?? '')) {
      forgetDraft(form);

      continue;
    }

    // The appearance card's language first, so the boxes below land in the
    // language they were typed in rather than over the one the loader chose.
    if (key === 'form-branding' && draft.brandingDraft) {
      brandingDraft = draft.brandingDraft;

      // Filled rather than shown: showBrandingLanguage takes the boxes back into
      // the draft on its way past, and the boxes here hold what the loader just
      // put in them - so it would file the server's words under the language
      // being restored and throw away the ones that were written.
      fillBrandingBoxes(draft.brandingLanguage ?? brandingLanguage);
    }

    const restored = [];

    for (const field of draftableFields(form)) {
      // A disabled control is not somebody's unsaved work; it is something the
      // screen has put beyond reach - a shipped role's rights, the project a
      // running timer is already booked against - and putting a draft into one
      // would be writing over what the server said.
      if (field.disabled || !(field.name in draft.values)) continue;

      const was = draft.values[field.name];

      if (field.type === 'checkbox') {
        field.checked = Array.isArray(was) ? was.includes(field.value) : Boolean(was);
      } else if (field.type === 'radio') {
        field.checked = field.value === was;
      } else {
        field.value = was;
      }

      restored.push(field);
    }

    // Announced once every value is in rather than as each one lands. Handlers
    // derive one field from another here - the database port from the dialect,
    // which fields are on screen at all from the same - and one that runs part
    // way through reads a form half restored.
    for (const field of restored) {
      field.dispatchEvent(new Event('input', { bubbles: true }));
      field.dispatchEvent(new Event('change', { bubbles: true }));
    }

    // And which of them were chosen rather than filled in - see draftOf. Put
    // back after the events above rather than before: those are dispatched from
    // here, so the listeners that would otherwise set this mark deliberately
    // ignore them.
    for (const name of draft.chosen ?? []) {
      const field = form.elements[name];
      if (field) field.dataset.chosen = 'yes';
    }

    form.dataset.editing = 'yes';
  }
}

/**
 * Decides what stands in each of the two mark slots.
 *
 * The bar has three columns and the outer two mean different things: the left
 * one is the installation, the middle one is the application. They used to
 * share a single drawing and show one of it - so a fresh installation wore the
 * application's mark on the left and had a bare title in the middle, and an
 * installation with a logo had it the other way round. The title lost its mark
 * to a slot that was never about the application.
 *
 * Both are always filled now:
 *
 *   middle  the application's own mark, beside the title, always
 *   left    the uploaded logo, or the installation's initial where there is none
 *
 * The initial is the first letter of the title this installation was given. It
 * costs nothing, it differs between installations, and it changes when somebody
 * changes the title - which is more than a generic placeholder glyph could say,
 * and it never reads as a picture that failed to load.
 *
 * Only in the header. The sign-in card gives a logo the width of the card, and a
 * 26-pixel chip in that space would read as something broken.
 */
function showBrandMark(branding, ownLogo) {
  // Both places that stand in for a logo: the chip in the header, and the one on
  // the sign-in screen. The sign-in card used to show nothing when no logo was
  // configured, which reads as a page still loading rather than as an
  // installation that has not uploaded one.
  const letter = initialOf(brandingIn(branding, 'title'));

  for (const [markID, initialID] of [
    ['#brand-mark', '#brand-initial'],
    ['#login-mark', '#login-initial'],
  ]) {
    const mark = $(markID);

    // The attribute rather than the property, because these are SVG elements and
    // `hidden` is defined on HTMLElement: assigning it here creates a property
    // nobody reads and leaves the chip on screen beside the logo that replaced
    // it.
    if (mark) {
      if (ownLogo) mark.setAttribute('hidden', '');
      else mark.removeAttribute('hidden');
    }

    const initial = $(initialID);
    if (initial) initial.textContent = letter;
  }
}

/**
 * The one letter that stands for an installation.
 *
 * The first character that is a letter or a digit, upper-cased. Skipping past
 * anything else matters more than it sounds: a title beginning with a quotation
 * mark or a bracket would otherwise put punctuation in the chip, which reads as
 * a fault rather than as a name.
 *
 * Upper-cased through the reader's own locale, so a Turkish i becomes the
 * capital that language uses rather than the English one.
 *
 * Falls back to the application's own initial, because an installation whose
 * title is empty still has to have something in the corner.
 */
function initialOf(title) {
  for (const character of String(title ?? '')) {
    if (/\p{L}|\p{N}/u.test(character)) return character.toLocaleUpperCase();
  }

  return 'Z';
}

/**
 * Whether somebody is part way through filling this form in.
 *
 * Every screen in this application is refilled from the server by loaders that
 * run for reasons having nothing to do with the person in front of them: a
 * language chosen, a neighbouring card saved, a directory synchronised. Each of
 * those refills every form on the way past, and anything typed and not yet saved
 * was replaced by the server's copy without a word.
 *
 * It is not a rare window either. Choosing a language reloads every screen, so
 * an administrator half way through a database connection who switched to German
 * to read a label lost the lot - and the form that came back looked like one
 * nobody had touched, so there was nothing to notice.
 *
 * Somebody who has started typing is answering a question, and the server's copy
 * is what they are answering it against. Replacing their answer with it
 * mid-sentence is the one thing a background refresh must not do. So a form that
 * has been changed and not saved is left alone until it is.
 */
function beingEdited(form) {
  return form?.dataset.editing === 'yes';
}

/**
 * Notices that somebody has started filling a form in.
 *
 * Both events, because they cover different fields: input is typing, and change
 * is what a picker, a checkbox and a file field report. Only what a person did -
 * setting a value from script fires neither, which is exactly the distinction
 * this rests on, and is why the loaders can go on filling forms nobody has
 * touched.
 */
function watchForEditing(form) {
  for (const event of ['input', 'change']) {
    form.addEventListener(event, (e) => {
      // Unless the control only decides what the form shows. The appearance
      // card's language chooser is one: it swaps which language's words are in
      // the boxes, and looking at a translation is not writing one - a card
      // somebody glanced at would otherwise stop following the server for as
      // long as they left the screen open.
      if (e.target?.dataset?.notAnEdit !== undefined) return;

      form.dataset.editing = 'yes';

      // And written down, so a reload does not take it. Here rather than on the
      // way out of the page: a tab that is closed by the browser, by a crash or
      // by a machine going to sleep never runs anything on the way out.
      rememberDraft(form);
    });
  }

  // Discarding is finishing too: a form put back to what it was holds nothing
  // worth protecting, and nothing worth carrying across a load.
  form.addEventListener('reset', () => {
    delete form.dataset.editing;
    forgetDraft(form);
  });
}

/**
 * Saves a form, and stops protecting what is in it once the save has landed.
 *
 * The same as mutate, with the one thing every form that is refilled from the
 * server has to do afterwards. Cleared here rather than when the form is
 * submitted, because a refused save is not finished: the values stay on screen
 * beside the complaint about them, and they are still the reader's to correct.
 *
 * Before `after` rather than after it, because `after` is where the reload is -
 * and the whole point of clearing is that this reload may fill the form again.
 */
function saveForm(form, fn, successMessage, after) {
  return mutate(fn, successMessage, async (result) => {
    if (form) delete form.dataset.editing;

    // The draft with it: what it held is on the server now, so keeping it would
    // only mean putting yesterday's copy back over today's on the next load.
    forgetDraft(form);

    if (after) await after(result);
  });
}

function formData(form) {
  const out = {};
  for (const [key, raw] of new FormData(form).entries()) {
    const value = typeof raw === 'string' ? raw.trim() : raw;
    if (value !== '') out[key] = value;
  }
  return out;
}

/**
 * A form read for a correction, where a box somebody emptied is a change.
 *
 * formData drops an empty value. That is right for creating something and wrong
 * for correcting it: the server tells "there is none any more" from "I said
 * nothing about it" by whether the field arrived at all, and says so where it
 * reads an end date - "an empty string is how a form field that has been cleared
 * arrives". Dropped, it never arrives, and an emptied box saves as no change.
 *
 * The fields that may be emptied are named rather than inferred, because the
 * other kind sits in the same forms. A start date, a day, a duration are facts
 * the record must have; an empty one is somebody clearing a box rather than
 * removing a fact, and sending it would be refused. Those keep being dropped.
 *
 * One function because two forms needed it and only one had it. The project form
 * wrote its own object out by hand with a comment explaining exactly this, and
 * the booking form went on using formData - so an emptied description saved as no
 * change, and so did taking the project off an entry, which is how an entry stops
 * being categorised at all.
 *
 * id is never sent: it is the form's own identity and travels in the path.
 */
function correctionFrom(form, emptiable) {
  const out = {};

  for (const [key, raw] of new FormData(form).entries()) {
    if (key === 'id') continue;

    const value = typeof raw === 'string' ? raw.trim() : raw;

    if (value !== '' || emptiable.includes(key)) out[key] = value;
  }

  return out;
}

/**
 * Makes every date field read and accept the reader's own convention.
 *
 * <input type="date"> renders in the *browser's* UI language and ignores the
 * page entirely - measured in Chrome 151 with the same value in three inputs,
 * one of them carrying lang="de-DE" itself: started with --lang=en-US all three
 * read 08/12/2026, with --lang=de-DE all three read 12.08.2026. So an English
 * screen on a German machine asked for a date as TT.MM.JJJJ, and a German screen
 * on an English one asked American-first.
 *
 * The native field is kept rather than replaced. It is what a phone turns into a
 * date wheel and what a screen reader already knows how to announce, and writing
 * a calendar that does both of those as well as the browser does is not a trade
 * worth making for a format string. It becomes the picker: off-screen, opened by
 * the button beside the field, and its value is still the value.
 *
 * What is added is the part that was wrong - a text field, written and read in
 * the reader's own convention. Typing is honoured, because a date field somebody
 * cannot type into is worse than one in the wrong order.
 *
 * The named element stays the native one, so every form read, every
 * form.elements lookup and every getElementById keeps working unchanged. Setting
 * one from code goes through setDateField.
 */
function enhanceDateFields(root = document) {
  for (const native of $$('input[type="date"]', root)) {
    if (native.dataset.enhanced) continue;

    native.dataset.enhanced = '1';
    native.classList.add('date-native');
    native.tabIndex = -1;
    native.setAttribute('aria-hidden', 'true');

    const shown = el('input', {
      type: 'text',
      class: 'date-shown',
      // A date is digits and separators; on a phone this is the difference
      // between the number pad and the whole keyboard.
      inputmode: 'numeric',
      autocomplete: 'off',
      spellcheck: 'false',
    });

    const open = el('button', {
      type: 'button',
      class: 'date-open',
      tabindex: '-1',
      'aria-hidden': 'true',
    });

    open.append(calendarIcon());

    const wrap = el('span', { class: 'date-wrap' });
    native.replaceWith(wrap);
    wrap.append(shown, native, open);

    if (native.required) shown.required = true;

    const draw = () => {
      shown.value = native.value ? fmtDate(native.value) : '';
      shown.placeholder = datePattern();
      shown.setAttribute('aria-label', dateFieldLabel(wrap));
    };

    native.addEventListener('change', draw);
    native.addEventListener('input', draw);

    // Typed by hand. Parsed on every keystroke so a complete date takes effect
    // at once, and left alone while it is still half-written.
    shown.addEventListener('input', () => {
      const iso = parseTypedDate(shown.value);
      if (iso !== null) native.value = iso;
      else if (shown.value.trim() === '') native.value = '';
    });

    // On the way out, the box is made to agree with what is stored - so half a
    // date, or one nobody could parse, does not sit there looking accepted.
    shown.addEventListener('blur', draw);

    const showPicker = () => {
      if (typeof native.showPicker !== 'function') return;

      try {
        native.showPicker();
      } catch {
        // Chrome throws without a user gesture. Not worth a message: typing
        // still works, which is why this is a text field.
      }
    };

    // mousedown rather than click: focus would move first and close the picker
    // in the same breath.
    open.addEventListener('mousedown', (event) => {
      event.preventDefault();
      shown.focus();
      showPicker();
    });

    // The keyboard way in, on the field itself - the button is out of the tab
    // order because it would double every date field's stops for no gain.
    shown.addEventListener('keydown', (event) => {
      if (event.key === 'ArrowDown' && (event.altKey || event.metaKey)) {
        event.preventDefault();
        showPicker();
      }
    });

    draw();

    // Redrawn when the language changes, like everything else that was written
    // into the page rather than translated in it.
    redraws.set('dateField:' + redraws.size, draw);
  }
}

/** The pattern a date is written in here, as a placeholder. */
function datePattern() {
  // Read off a date whose parts cannot be mistaken for one another, so the order
  // and the separators come from the locale rather than from a guess.
  return new Intl.DateTimeFormat(activeLocale(), {
    day: '2-digit', month: '2-digit', year: 'numeric', timeZone: 'UTC',
  }).formatToParts(new Date(Date.UTC(2026, 10, 22))).map((part) => {
    if (part.type === 'day') return t('date.day', 'DD');
    if (part.type === 'month') return t('date.month', 'MM');
    if (part.type === 'year') return t('date.year', 'YYYY');

    return part.value;
  }).join('');
}

/** Which of day, month and year comes first, second and third here. */
function dateOrder() {
  return new Intl.DateTimeFormat(activeLocale(), {
    day: '2-digit', month: '2-digit', year: 'numeric', timeZone: 'UTC',
  }).formatToParts(new Date(Date.UTC(2026, 10, 22)))
    .filter((part) => ['day', 'month', 'year'].includes(part.type))
    .map((part) => part.type);
}

/**
 * Reads a date somebody typed, in the order this locale writes them.
 *
 * Null for anything incomplete, which is what leaves a half-typed date alone
 * instead of storing a guess at it.
 */
function parseTypedDate(text) {
  const groups = String(text).match(/\d+/g);
  if (!groups || groups.length < 3) return null;

  const order = dateOrder();
  const read = {};
  order.forEach((type, index) => { read[type] = groups[index]; });

  // A two-digit year is ambiguous and this is not the place to decide it.
  if (!read.year || read.year.length !== 4) return null;

  const year = Number(read.year);
  const month = Number(read.month);
  const day = Number(read.day);

  if (!year || !month || !day) return null;

  // Round-tripped through a real date, so the thirtieth of February is refused
  // rather than silently becoming the second of March.
  const at = new Date(Date.UTC(year, month - 1, day));
  if (at.getUTCFullYear() !== year || at.getUTCMonth() !== month - 1
    || at.getUTCDate() !== day) {
    return null;
  }

  return `${String(year).padStart(4, '0')}-${String(month).padStart(2, '0')}`
    + `-${String(day).padStart(2, '0')}`;
}

/** What to call a date field to a screen reader, taken from its own label. */
function dateFieldLabel(wrap) {
  const label = wrap.closest('label');
  const text = label ? leadingText(label).trim() : '';

  return text || t('field.date', 'Date');
}

/** Sets a date field from code, so both halves of it agree. */
function setDateField(native, iso) {
  if (!native) return;

  native.value = iso ?? '';
  native.dispatchEvent(new Event('change'));
}

function calendarIcon() {
  const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
  svg.setAttribute('viewBox', '0 0 24 24');
  svg.setAttribute('width', '18');
  svg.setAttribute('height', '18');
  svg.setAttribute('aria-hidden', 'true');

  const path = document.createElementNS('http://www.w3.org/2000/svg', 'path');
  path.setAttribute('fill', 'currentColor');
  path.setAttribute('d', 'M7 2v2H5a2 2 0 0 0-2 2v13a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2V6a2 2 0 0 0-2-2h-2V2h-2v2H9V2H7zm12 7v10H5V9h14z');

  svg.append(path);

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
 * English is the source language and lives in the markup itself, so the `en`
 * dictionary is intentionally empty: switching back to English restores the
 * original text nodes. Every user-visible string has a key here, including the
 * ones this script generates at run time.
 *
 * This comment used to say the opposite, naming German as the source and `de` as
 * the empty one. Both halves were wrong, and a comment that describes the mirror
 * image of the code is worse than none: it is the thing somebody reads before
 * adding a key, and it told them to put it in the wrong dictionary.
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
    'auth.disabled': 'Authentifizierung deaktiviert',
    'admin.baseDn': 'Base DN',
    'admin.bindDn': 'Bind DN (optional)',
    'admin.dbHost': 'Host',
    'admin.dbPort': 'Port',
    'admin.logo': 'Logo (PNG oder JPEG, max. 256 KB)',
    'admin.logoInHeader': 'In der Kopfzeile',
    'admin.logoCropHint': 'Jede Vorschau zeigt einen Ausschnitt des Logos. Zum Wählen darauf klicken.',
    'crop.title': 'Welcher Teil des Logos?',
    'crop.text': 'Ziehen, um den Teil des Logos zu wählen, der hier verwendet wird: {0}.',
    'crop.free': 'Ziehen Sie die Ecken in eine beliebige Form. Der gewählte Teil wird in die jeweilige Stelle eingepasst, niemals verzerrt.',
    'crop.whole': 'Ganzes Logo verwenden',
    'crop.apply': 'Diesen Teil verwenden',
    'admin.logoAsFavicon': 'Im Browser-Tab',
    'admin.logoOnSignIn': 'Auf der Anmeldeseite',
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
    'tour.welcome.title': 'Der Weg zurück',
    'tour.welcome.text': 'Der Titel bringt dich von überall auf die Willkommensseite. Dort steht, was der heutige Tag schon hat – und von dort lässt sich dieser Rundgang erneut starten.',
    'tour.nav.title': 'Alles liegt hier oben',
    'tour.nav.text': 'Diese Reiter sind die ganze Anwendung. Sichtbar ist nur, was deine Rolle erlaubt — bei manchen ist diese Leiste also kürzer als bei anderen.',
    'tour.timer.title': 'Die Stoppuhr',
    'tour.timer.text': 'Beim Anfangen starten, beim Aufhören stoppen – die gemessene Zeit wird gebucht. Sie läuft weiter, wenn du den Browser schließt, denn sie läuft auf dem Server und nicht in diesem Tab.',
    'tour.book.title': 'Zeit buchen',
    'tour.book.text': 'Datum wählen, Stunden eintragen, fertig. Ein Projekt ist optional, Zeiten lassen sich also jetzt erfassen und später einsortieren. Gespeichert wird, was du schreibst – auf die Minute genau, hier rundet nichts auf eine Viertelstunde.',
    'tour.entries.title': 'Deine Einträge',
    'tour.entries.text': 'Alles Gebuchte, filterbar nach Projekt. Ein Klick auf eine Zeile öffnet sie zum Korrigieren, mehrere angehakte lassen sich in einem Zug löschen. Es bleibt deins zum Ändern – es gibt niemanden, dem du es vorlegen müsstest.',
    'tour.sheet.title': 'Rein und raus als Tabelle',
    'tour.sheet.text': 'Deine Stunden als Tabellendatei, mit den Spaltenköpfen in deiner Sprache. Ein Import zeigt zuerst, was er ändern würde, bevor er etwas ändert. Jede Tabelle, bei der sich das lohnt, hat dasselbe Paar auf ihrem eigenen Bildschirm.',
    'tour.calendar.title': 'Der Monat auf einen Blick',
    'tour.calendar.text': 'Welche Tage Stunden haben und wie viele. Ein Klick auf einen Tag zeigt, was dahintersteckt – und ein Klick auf einen dieser Einträge öffnet ihn zum Korrigieren.',
    'tour.stats.title': 'Deine eigenen Zahlen',
    'tour.stats.text': 'Stunden pro Tag und pro Projekt, über einen Zeitraum deiner Wahl. Nur deine eigenen – und niemand sonst sieht sie.',
    'tour.overtime.title': 'Dein Überstundensaldo',
    'tour.overtime.text': 'Gebuchte Stunden gegen dein Tagesziel. Nur Tage mit Buchungen zählen, damit Wochenenden und freie Tage sich nicht stillschweigend als Minus aufsummieren.',
    'tour.projects.title': 'Deine Projekte',
    'tour.projects.text': 'Ein Platz für deine Stunden, und zwar nur deiner: Nur du siehst sie und nur du buchst darauf. Zwei Menschen an derselben Sache haben jeweils ein eigenes Projekt.',
    'tour.report.title': 'Auswertungen',
    'tour.report.text': 'Was du in einem Zeitraum gebucht hast: auf einem Projekt, über alle hinweg oder nur die Stunden ohne Projekt. Deine eigenen Stunden – es gibt keine Aufschlüsselung nach Personen, denn niemand sieht, was jemand anderes erfasst hat.',
    'tour.users.title': 'Die Menschen, die hier arbeiten',
    'tour.users.text': 'Ein Konto anlegen und ihm eine Rolle geben. Es startet mit den Vorgaben der Installation; die eigenen Arbeitszeiten setzt danach die Person selbst unter „Mein Konto“.',
    'tour.userTable.title': 'Wer hier ist und was er darf',
    'tour.userTable.text': 'Die Rolle neben jedem Namen lässt sich hier ändern. Der eingebaute Administrator ist die Ausnahme: Er lässt sich weder löschen noch umhängen, denn er ist der Weg zurück ins System.',
    'tour.roles.title': 'Rollen entscheiden, was möglich ist',
    'tour.roles.text': 'Eine Rolle ist ein Satz Rechte, und jedes einzelne wird von echtem Code durchgesetzt und nicht von einem versteckten Knopf. Hier ankreuzen, was die Rolle darf – jedes Konto mit dieser Rolle ändert sich mit.',
    'tour.account.title': 'Mein Konto',
    'tour.account.text': 'Dein Tagessoll und dein Tagesmaximum. Das Tagessoll ist der Maßstab für den Überstundensaldo, und niemand außer dir setzt es.',
    'tour.myZone.title': 'Deine Zeitzone',
    'tour.myZone.text': 'Sie entscheidet, auf welchen Kalendertag eine Buchung fällt. Bei der ersten Anmeldung wird sie aus dem Browser übernommen – und hier widersprichst du dem.',
    'tour.totp.title': 'Ein zweiter Faktor',
    'tour.totp.text': 'Ein Code aus einer Authenticator-App, eingerichtet durch Scannen eines QR-Codes – oder durch Abtippen des Geheimnisses, wenn Scannen nicht geht.',
    'tour.passkey.title': 'Anmelden ohne Kennwort',
    'tour.passkey.text': 'Ein Passkey bleibt auf diesem Gerät und wird mit dem entsperrt, was das Gerät ohnehin entsperrt. Du kannst mehrere hinterlegen, einen pro Gerät.',
    'tour.tokens.title': 'Token für Skripte',
    'tour.tokens.text': 'Mit einem persönlichen Token kann ein Skript in deinem Namen buchen – mit genau den Rechten, die deine Rolle im Moment der Nutzung hat. Er wird einmal angezeigt, beim Anlegen.',
    'tour.password.title': 'Dein Kennwort',
    'tour.password.text': 'Eine Änderung beendet alle anderen Sitzungen außer dieser. Ein Konto, das sich über das Verzeichnis anmeldet, hat hier kein Kennwort zu ändern.',
    'tour.branding.title': 'Wie diese Installation aussieht',
    'tour.branding.text': 'Titel, Banner, Logo und Fußzeile. Das Logo wird zum Symbol im Browser-Tab – und genau das unterscheidet auf einen Blick eine Testinstanz von der echten.',
    'tour.database.title': 'Die Datenbank',
    'tour.database.text': 'In welche Datenbank diese Installation schreibt. Die Verbindung wird vor dem Speichern getestet, und die Änderung gilt ab dem nächsten Start.',
    'tour.ldap.title': 'Anmelden gegen ein Verzeichnis',
    'tour.ldap.text': 'Konten können aus LDAP kommen, statt hier angelegt zu werden. Darunter läuft der Abgleich nach Zeitplan – und lässt sich vorher ansehen, bevor er von Hand ausgeführt wird.',
    'tour.maintenance.title': 'Wartungsmodus',
    'tour.maintenance.text': 'Schließt die Installation mit einer Erklärung, die auch auf der Anmeldemaske steht – wer nicht hineinkommt, erfährt also warum, statt zu raten.',
    'tour.limits.title': 'Grenzwerte und Laufzeiten',
    'tour.limits.text': 'Wie lange eine Sitzung gilt, wie viele Anfragen jemand stellen darf und mit welchen Werten ein neues Konto startet.',
    'tour.telemetry.title': 'Metriken und Tracing',
    'tour.telemetry.text': 'Log-Level, der Metrik-Endpunkt und wohin Traces exportiert werden. Alle drei werden beim Start des Prozesses gelesen, gelten also ab dem nächsten.',
    'tour.log.title': 'Das Protokoll, ohne Shell',
    'tour.log.text': 'Was dieser Prozess schreibt, filterbar nach Stufe. Die erste Anlaufstelle, wenn etwas abgelehnt wurde und der Grund nicht auf dem Bildschirm stand.',
    'tour.theme.title': 'Darstellung und Sprache',
    'tour.theme.text': 'Hell oder dunkel — automatisch richtet sich nach der Tageszeit. Die Sprache folgt dem Browser, bis du eine wählst. Das war alles, viel Erfolg.',

    'setup.decideFirst': 'Bitte diesen Schritt zuerst erledigen.',
    'setup.branding.title': 'Installation benennen',
    'setup.branding.text': 'Der Titel erscheint im Browser-Tab und in der Kopfzeile. Optional, aber genau das unterscheidet auf einen Blick eine Testinstanz von der echten.',
    'setup.directory.title': 'Verzeichnis anbinden (optional)',
    'setup.directory.text': 'Mit angebundenem LDAP oder Active Directory melden sich alle mit ihrem vorhandenen Konto an, und hier werden keine Passwörter gespeichert. Überspringen, um Konten lokal zu verwalten.',

    'ops.title': 'Betrieb und Grenzwerte',
    'ops.hint': 'Leer lassen, um den Wert aus der Konfigurationsdatei zu behalten. Änderungen wirken innerhalb weniger Sekunden, ohne Neustart.',
    'ops.sessionIdle': 'Abmelden nach Untätigkeit (Minuten, 0 = nie)',
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
    'tel.ratioShort': 'Anteil aufgezeichneter Traces',
    'tel.tracingHint': 'Läuft deploy/compose.tracing.yaml neben der Anwendung? Dann ist es Exporter OTLP und Collector jaeger:4317, und die Traces liegen unter {0}.',
    'tel.tracingLoopback': 'Antwortet die Adresse nicht, veröffentlicht die mitgelieferte Datei den Trace-Browser absichtlich nur auf dem Loopback des Servers – dann führt der Weg über {0}.',
    'tel.reset': 'Alles auf die Konfigurationsdatei zurücksetzen',
    'tel.resetDone': 'Metriken und Traces folgen wieder der Konfigurationsdatei',
    'tel.activeMetrics': 'Metriken',
    'tel.activeMetricsOff': 'werden nicht ausgeliefert',
    'tel.activeTraces': 'Traces',
    'tel.activeTracesOff': 'werden nicht exportiert',

    'password.reveal': 'Passwort anzeigen',
    'password.hide': 'Passwort verbergen',

    'restart.title': 'Neustart',
    'restart.open': 'Neustart',
    'restart.summary': '{0} gespeicherte Einstellung(en) warten auf einen {1}.',
    'restart.unsupported.noExecve': 'Ein Neustart aus der Anwendung heraus ist unter Windows nicht möglich: dafür wird execve gebraucht, das es dort nicht gibt. Gespeicherte Einstellungen werden wirksam, sobald die Anwendung so neu gestartet wird, wie sie gestartet wurde.',
    'restart.unsupported.executableUnknown': 'Ein Neustart aus der Anwendung heraus ist nicht möglich: die laufende Programmdatei lässt sich nicht auffinden. Gespeicherte Einstellungen werden wirksam, sobald die Anwendung so neu gestartet wird, wie sie gestartet wurde.',
    'restart.hint': 'Einige Einstellungen werden nur beim Start der Anwendung gelesen. Diese sind gespeichert und warten:',
    'restart.modeContainer': 'Diese Installation läuft in einem Container. Der Knopf '
      + 'hält ihn an, und Ihre Container-Verwaltung startet einen neuen aus dem '
      + 'Abbild - was sie nur tut, wenn sie dazu angewiesen wurde. Die mit dieser '
      + 'Anwendung ausgelieferte Bereitstellung ist es.',
    'restart.modeProcess': 'Die Anwendung ersetzt sich selbst, läuft also durchgehend.',
    'restart.now': 'Jetzt neu starten',
    'restart.confirm': 'Anwendung neu starten? Wer gerade darin arbeitet, muss die Seite neu laden.',
    'restart.waiting': 'Neustart läuft',
    'restart.waitingHint': 'Es wird gewartet, bis die Anwendung wieder erreichbar ist …',
    'restart.done': 'Die Anwendung wurde neu gestartet, die Einstellungen sind jetzt wirksam.',
    'restart.failed': 'Der Neustart konnte nicht gestartet werden',
    'restart.slow': 'Die Anwendung antwortet noch nicht. Möglicherweise startet sie noch — bitte die Seite gleich neu laden.',
    'restart.none': 'nichts',
    'restart.dbPassword': 'Datenbank-Passwort',

    // Why a single row of an imported file cannot be written. The server sends a
    // code and the values its English sentence interpolated, so these say the same
    // thing in the same order the reader would - see rowProblem.
    'row.nameMissing': 'Der Name fehlt.',
    'row.startDate': '„{0}“ ist kein Beginn, den der Import versteht (JJJJ-MM-TT).',
    'row.endDate': '„{0}“ ist kein Ende, das der Import versteht (JJJJ-MM-TT).',
    'row.notAStatus': '„{0}“ ist kein Status; möglich sind {1}, {2} oder {3}.',
    'row.archiveNeedsCompleted': 'Archiviert werden kann erst, was den Status „{0}“ hat; '
      + '„{1}“ ist „{2}“.',
    'row.roleNameMissing': 'Der Name fehlt, und daran wird die Zeile erkannt.',
    'row.systemRole': '„{0}“ ist eine Systemrolle, ihre Rechte lassen sich nicht ändern.',
    'row.roleGrantsNothing': '„{0}“ würde ohne jedes Recht angelegt – wer die Rolle hätte, '
      + 'könnte keinen einzigen Bereich öffnen.',
    'row.emailMissing': 'Die E-Mail-Adresse fehlt, und daran wird die Zeile erkannt.',
    'row.emailInvalid': '„{0}“ ist keine E-Mail-Adresse, und an dieser Spalte wird die '
      + 'Zeile erkannt.',
    'row.noSuchAccount': 'Für „{0}“ gibt es kein Konto; dieser Import ändert Konten und '
      + 'legt keine an.',
    'row.noSuchRole': '„{0}“ ist keine Rolle.',
    'row.dateMissing': 'Das Datum fehlt.',
    'row.dateNotUnderstood': '„{0}“ ist kein Datum, das der Import versteht (JJJJ-MM-TT).',
    'row.hoursMissing': 'Die Stunden fehlen.',
    'row.hoursNotANumber': '„{0}“ ist keine Stundenzahl.',
    'row.noSuchUser': 'Es gibt niemanden mit dem Namen „{0}“.',
    'row.notYourTime': 'Sie dürfen nur eigene Zeiten importieren, und diese Zeile gehört '
      + 'zu {0}.',
    'row.noSuchProject': 'Es gibt kein Projekt mit dem Namen „{0}“.',
    'row.projectNotActive': 'Projekt „{0}“ ist {1} und nimmt keine Zeiteinträge mehr an.',

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
    'sheet.projects.text': 'Exportiert Ihre Projekte. Ein Import '
      + 'ordnet Zeilen über den Namen zu: ein vorhandener Name wird aktualisiert, ein neuer '
      + 'angelegt. Eine leere '
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
    'sheet.roles.text': 'Exportiert jede Rolle mit einer Spalte pro Recht, die ja oder nein '
      + 'enthält – genau das Raster, das man braucht, wenn die Frage lautet, was vier Rollen '
      + 'dürfen. Ein Import ordnet Zeilen über den Namen zu, legt eine noch nicht vorhandene '
      + 'Rolle an und weist eine Spaltenüberschrift zurück, die ein hier nicht durchgesetztes '
      + 'Recht benennt. Eine System-Rolle lässt sich nur anders beschreiben, sonst nichts.',
    'sheet.roles.file': 'rollen',
    'sheet.roles.done': '{0} Zeilen geschrieben.',
    'action.edit': 'bearbeiten',
    'action.evaluate': 'Auswerten',
    'action.exportPdf': 'Als PDF exportieren',
    'action.reload': 'Neu laden',
    'action.generate': 'Erzeugen',
    'action.new': 'Neu',
    'action.save': 'Speichern',
    'action.dismiss': 'Schließen',
    'action.cancel': 'Abbrechen',
    'stats.title': 'Meine Stunden',
    'stats.perDay': 'Stunden pro Tag',
    'stats.perProject': 'Stunden pro Projekt',
    'stats.total': 'Gesamt',
    'stats.filename': 'statistik',
    'stats.noProject': 'Kein Projekt',
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
    'admin.connectionFromEnvironment': 'Diese Verbindung kommt aus der Umgebung, '
      + 'nicht aus einer gespeicherten Einstellung; die Felder unten zeigen sie '
      + 'als Platzhalter. Wird dieses Formular gespeichert, gilt die gespeicherte '
      + 'Verbindung beim nächsten Start vor der Umgebung.',
    'admin.banner': 'Banner-Text (leer = ausgeblendet)',
    'admin.bindPassword': 'Bind-Passwort',
    'admin.branding': 'Erscheinungsbild',
    'admin.brandingHint': 'Titel, Banner, Logo und Fußzeile dieser Installation.',
    'admin.company': 'Firma',
    'admin.companyUrl': 'Firmen-Website',
    'admin.database': 'Datenbank-Anbindung',
    'admin.databaseHint': 'Die Verbindung wird gespeichert und beim nächsten Start der Anwendung verwendet. Ein Wechsel im laufenden Betrieb wäre nicht gefahrlos möglich.',
    'admin.dbName': 'Datenbank / Dateiname',
    'admin.dbFile': 'Datenbankdatei – wird angelegt, falls sie fehlt',
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
    'admin.testOk': 'Die Verbindung funktioniert.',
    'admin.textLanguage': 'Sprache dieser Texte',
    'admin.title': 'Titel (Kopfzeile)',
    'admin.tabTitle': 'Browser-Tab (leer = Titel)',
    'setup.instanceName': 'Name dieser Installation',
    'admin.userFilter': 'Benutzer-Filter (%s = Anmeldename)',
    'app.language': 'Sprache',
    'banner.password': 'Das Initialpasswort ist noch aktiv. Bitte unter „Mein Konto" ändern — bis dahin bleibt die übrige Anwendung gesperrt.',
    'detail.show': 'Technische Details',
    'detail.reference': 'Referenz: {0}',
    'err.unauthenticated': 'Die Sitzung ist abgelaufen. Bitte erneut anmelden.',
    'err.notFound': '{0} mit der Kennung {1} wurde nicht gefunden.',
    'err.invalidFields': 'Ungültige Felder',
    'err.rateLimited': 'Zu viele Anfragen. Bitte in {0} Sekunden erneut versuchen.',
    'err.updateCheckedRecently': 'Die Release-Quelle wurde gerade erst gefragt. '
      + 'Erneut möglich in {0} Sekunden.',
    'err.updateCheckOff': 'Die Aktualisierungsprüfung ist für diese Installation abgeschaltet.',
    'err.bodyTooLarge': 'Die gesendeten Daten sind zu groß (Grenze: {0} MB).',
    'err.csrfRejected': 'Diese Seite ist zu lange geöffnet gewesen. Bitte neu laden und noch einmal versuchen.',
    'err.maintenance': 'Diese Installation ist wegen Wartungsarbeiten vorübergehend nicht verfügbar.',
    'err.noInstallerSession': 'Diese Installation wurde nicht über den Einrichtungs-Assistenten eingerichtet.',
    'err.noBuiltInAdmin': 'Diese Installation hat kein eingebautes Administratorkonto.',
    'err.installationAlreadyInUse': 'Diese Installation ist bereits in Betrieb. Bitte mit dem Passwort anmelden, das dafür gewählt wurde.',
    'err.cannotResetOwnPassword': 'Das eigene Passwort wird unter „Mein Konto“ geändert — dort wird nach dem aktuellen gefragt.',
    'err.cannotDeleteSelf': 'Das Konto, mit dem du angemeldet bist, kann nicht gelöscht werden.',
    'err.defaultRoleUndeletable': '„{0}“ ist eine mitgelieferte Rolle und kann nicht gelöscht werden.',
    'err.internal': 'Die Anfrage konnte nicht ausgeführt werden. Die technischen Details stehen darunter.',
    'err.probeFailed': 'Die Verbindung konnte nicht hergestellt werden.',
    'update.available': 'Version {0} ist verfügbar. Diese Installation läuft mit {1}.',
    'announce.installing': 'Eine neue Version ({0}) wird installiert. Du kannst weiterarbeiten; die Anwendung startet gleich neu.',
    'announce.restarting': 'Die Anwendung startet gerade in Version {0}. Diese Seite lädt sich gleich von selbst neu.',
    'announce.pending': 'Version {0} ist installiert und wird beim nächsten Start der Anwendung aktiv. Bis dahin ändert sich nichts.',
    'announce.cancelled': 'Das Update wurde nicht installiert, es hat sich nichts geändert. Die Anwendung läuft unverändert weiter.',
    'cal.close': 'schließen',
    'cal.clickToEdit': 'Zum Bearbeiten auf einen Eintrag klicken.',
    'cal.monthTotal': 'Gesamt im Monat',
    'cal.months': 'Januar,Februar,März,April,Mai,Juni,Juli,August,September,Oktober,November,Dezember',
    'cal.today': 'Heute',
    'cal.weekdays': 'Mo,Di,Mi,Do,Fr,Sa,So',
    'err.adminHasNoPasskey': 'Der eingebaute Administrator meldet sich mit Kennwort an, damit sich eine Installation nie durch ein verlorenes Gerät aussperrt.',
    'err.adminRoleMustAdminister': 'Der eingebaute Administrator kann nicht in die Rolle „{0}“ wechseln, ihr fehlt „{1}“.',
    'err.adminUndeletable': 'Der eingebaute Administrator kann nicht gelöscht werden.',
    'err.archiveNeedsCompleted': 'Ein Projekt kann erst archiviert werden, wenn sein Status „{0}“ ist.',
    'err.attemptExpired': 'Dieser Versuch ist abgelaufen. Bitte erneut versuchen.',
    'err.bodyNotJSON': 'Die Anfrage enthält kein gültiges JSON.',
    'err.credentialUnreadable': 'Der Anmeldeschlüssel konnte nicht gelesen werden.',
    'err.datasourceInvalid': 'Die Datenbank-Verbindung ist unvollständig oder ungültig.',
    'err.dateFormat': 'Das Datum „{0}“ muss YYYY-MM-DD oder RFC 3339 sein.',
    'err.deletionNeedsConfirming': '„{0}“ hat {1} erfasste Zeiteinträge. Sie würden mit dem Konto gelöscht und sind nicht wiederherstellbar – zum Fortfahren bitte bestätigen.',
    'err.emailTaken': 'Es gibt bereits einen Benutzer mit der E-Mail-Adresse „{0}“.',
    'err.entryAlreadyOnProject': 'Der Zeiteintrag ist bereits auf Projekt „{0}“ gebucht.',
    'err.initialPasswordPending': 'Das Anfangskennwort muss geändert werden, bevor die Anwendung genutzt werden kann.',
    'err.invalidCredentials': 'E-Mail-Adresse oder Kennwort ist falsch.',
    'err.invalidToken': 'Ungültiges Token.',
    'err.logoNotInline': 'Das Logo muss ein eingebettetes Bild sein (data:image/…).',
    'err.logoNotRaster': 'Das Logo muss ein PNG oder ein JPEG sein. SVG wird von manchen Browsern nicht als Tab-Symbol übernommen.',
    'err.logoTooLarge': 'Das Logo muss kleiner als {0} KB sein.',
    'err.logoTooManyPixels': 'Das Logo darf höchstens {0} Megapixel haben. Eine kleine Datei kann trotzdem sehr viele Bildpunkte enthalten.',
    'err.missingPermission': 'Dafür fehlt die Berechtigung „{0}“.',
    'err.onlyOwnWorkingTimes': 'Sie können nur Ihre eigenen Arbeitszeiten ändern.',
    'err.onlyOwnEntriesRead': 'Sie können nur Ihre eigenen Zeiteinträge sehen.',
    'err.onlyOwnEntriesWrite': 'Sie können nur Ihre eigenen Zeiteinträge ändern.',
    'err.onlyOwnOvertime': 'Sie können nur Ihren eigenen Überstundenstand sehen.',
    'err.onlyBuiltInAdminSyncs': 'Nur die eingebaute Administration darf das Verzeichnis '
      + 'abgleichen.',
    'err.updateInstalling': 'Eine Aktualisierung wird bereits installiert. Bitte '
      + 'warten Sie, bis sie abgeschlossen ist.',
    'err.updateDisabled': 'Die Aktualisierung ist auf dieser Installation abgeschaltet.',
    'err.updateInContainer': 'Dies läuft in einem Container. Ein dort ausgetauschtes Programm wird beim nächsten Neuerzeugen des Containers überschrieben. Bitte das Abbild von Hand aktualisieren – oder deploy/compose.update.yaml zur Bereitstellung hinzufügen, dann geht es von hier aus.',
    'err.updateNotNewer': 'Diese Installation läuft bereits mit {0}.',
    'err.onlyBuiltInAdminSchedules': 'Nur die eingebaute Administration darf den '
      + 'Verzeichnisabgleich planen.',
    'err.onlyBuiltInAdminSetsUp': 'Nur die eingebaute Administration darf die Einrichtung '
      + 'durchlaufen.',
    'err.restartUnsupported': 'Ein Neustart aus der Anwendung heraus ist auf diesem System nicht möglich. Gespeicherte Einstellungen werden beim nächsten regulären Start wirksam.',
    'err.mustChangePasswordFirst': 'Das Konto muss zuerst sein Anfangskennwort ändern.',
    'err.noAuthNoPassword': 'Diese Instanz läuft ohne Anmeldung, es gibt also kein Kennwort zu ändern.',
    'err.noDirectory': 'Es ist kein Verzeichnis konfiguriert.',
    'err.noSession': 'Keine Sitzung.',
    'err.noTimerRunning': 'Es läuft keine Stoppuhr.',
    'err.overDailyLimit': '{0} Std. würden am {2} zusammen {1} Std. ergeben und damit das Tagesmaximum von {3} Std. überschreiten.',
    'err.passkeyKnown': 'Dieser Anmeldeschlüssel ist bereits registriert.',
    'err.passkeyRejected': 'Der Anmeldeschlüssel wurde nicht akzeptiert.',
    'err.passkeyUnverified': 'Der Anmeldeschlüssel konnte nicht geprüft werden.',
    'err.passkeyWrongSession': 'Diese Registrierung gehört zu einer anderen Anmeldung.',
    'err.passwordTooShort': 'Das Kennwort muss mindestens {0} Zeichen lang sein.',
    'err.passwordTooLong': 'Das Kennwort darf höchstens {0} Byte lang sein. Umlaute und ß zählen doppelt, ein Emoji vierfach – eine lange Passphrase mit Umlauten kann die Grenze also früher erreichen, als sie aussieht.',
    'err.passwordUnchanged': 'Das neue Kennwort muss sich vom aktuellen unterscheiden.',
    'err.projectClosedForBooking': 'Projekt „{0}“ ist {1} und nimmt keine Zeiteinträge mehr an.',
    'err.projectIsBeingTimed': 'Bei {0} Person(en) läuft gerade eine Stoppuhr auf dieses Projekt. Es kann gelöscht werden, sobald sie gestoppt haben.',
    'err.projectHasEntries': 'Das Projekt hat noch {0} Zeiteinträge und kann nicht gelöscht werden.',
    'err.pageSizeTooLarge': 'Eine Seite darf höchstens {0} Einträge enthalten.',
    'err.rangeInverted': '„bis“ darf nicht vor „von“ liegen.',
    'err.roleNameTaken': 'Es gibt bereits eine Rolle namens „{0}“.',
    'err.roleStillAssigned': 'Rolle „{0}“ ist noch {1} Benutzer(n) zugewiesen.',
    'err.sessionExpired': 'Die Sitzung ist abgelaufen.',
    'err.systemRoleUndeletable': 'Die Systemrolle „{0}“ kann nicht gelöscht werden.',
    'err.systemRoleUnrenamable': 'Die Systemrolle „{0}“ kann nicht umbenannt werden.',
    'err.systemRoleDescriptionFixed': 'Die Beschreibung der mitgelieferten Rolle „{0}“ lässt sich nicht ändern – sie wird von der Oberfläche übersetzt.',
    'err.systemRoleRightsFixed': 'Die Rechte der Systemrolle „{0}“ lassen sich nicht ändern – weder entziehen noch hinzufügen. Wer hier arbeiten und zusätzlich verwalten soll, bekommt die Rolle „Benutzer & Administrator“.',
    'err.roleGrantsNothing': 'Eine Rolle muss mindestens ein Recht gewähren.',
    'err.targetOverMaximum': 'Das Tagesziel ({0} Std.) darf das Tagesmaximum ({1} Std.) nicht überschreiten.',
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
    'err.chartNotAPicture': 'Das Diagramm konnte nicht gelesen werden. '
      + 'Bitte die Auswertung erneut anzeigen und dann exportieren.',
    'err.chartTooManyPixels': 'Das Diagramm darf höchstens {0} Megapixel haben. '
      + 'Die Dateigröße sagt darüber nichts – ein Bild kann klein ankommen und '
      + 'trotzdem sehr viele Bildpunkte enthalten.',
    'err.documentTooLong': 'Die Auswertung ist zu umfangreich für ein Dokument. '
      + 'Bitte einen kürzeren Zeitraum wählen.',
    'err.unknownPermissionColumn': '„{0}“ ist kein Recht, das diese Anwendung kennt – '
      + 'die Datei stammt vermutlich aus einer anderen Installation.',
    'err.unsupportedDialect': '„{0}“ ist keine Datenbank, die diese Anwendung öffnen '
      + 'kann. Möglich sind: {1}.',
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
    'welcome.continue': 'Los geht’s',
    'welcome.goTo': 'Zur Willkommensseite',
    'welcome.text': 'Hier wird deine Arbeitszeit erfasst. Ein kurzer Rundgang dauert etwa eine Minute; du kannst ihn jederzeit unter „Mein Konto“ erneut starten.',
    'welcome.timerRunning': 'Es läuft noch eine Stoppuhr. Unter „Zeiteinträge“ stoppen, um die Zeit zu buchen.',
    'welcome.todayHours': 'Heute bislang {0} gebucht.',
    'welcome.todayNothing': 'Heute noch nichts gebucht.',
    'welcome.recent': 'Deine letzten Einträge',
    'welcome.recentAll': 'Alle Zeiteinträge',
    'welcome.recentNone': 'In den letzten 90 Tagen wurde nichts erfasst.',
    'welcome.tour': 'Rundgang starten',
    'field.banner': 'Banner-Text',
    'field.baseDn': 'Basis-DN',
    'field.code': 'Code',
    'field.companyName': 'Firma',
    'field.companyUrl': 'Firmen-Adresse',
    'field.dailyTargetHours': 'Soll/Tag',
    'field.defaultRole': 'Standardrolle',
    'field.durationHours': 'Stunden',
    'field.endDate': 'Ende',
    'field.footerText': 'Fußzeile',
    'field.expiresInDays': 'Läuft ab in (Tagen)',
    'field.host': 'Host',
    'field.id': 'Kennung',
    'field.language': 'Sprache',
    'field.ldapSyncMaxDeleteRatio': 'Höchstanteil gelöschter Konten',
    'field.legalNotice': 'Rechtlicher Hinweis',
    'field.logLevel': 'Protokollstufe',
    'field.maxDailyHours': 'Max/Tag',
    'field.port': 'Port',
    'field.projectId': 'Projekt',
    'field.rateLimit': 'Anfragegrenze',
    'field.rateLimitWindowSeconds': 'Zeitfenster der Anfragegrenze (Sekunden)',
    'field.sessionIdleMinutes': 'Abmelden nach Untätigkeit (Minuten)',
    'field.sessionLifetimeHours': 'Sitzungsdauer (Stunden)',
    'field.startDate': 'Start',
    'field.syncSchedule': 'Zeitplan',
    'field.theme': 'Erscheinungsbild',
    'field.timezone': 'Zeitzone',
    'field.tabTitle': 'Browser-Tab',
    'field.title': 'Titel',
    'field.traceExporter': 'Trace-Exporter',
    'field.tracerRatio': 'Trace-Anteil',
    'field.tracerUrl': 'Trace-Adresse',
    'field.userFilter': 'Benutzerfilter',
    'field.userId': 'Benutzer',
    'date.day': 'TT',
    'date.month': 'MM',
    'date.year': 'JJJJ',
    'field.date': 'Datum',
    'field.default': 'Standard',
    'field.description': 'Beschreibung',
    'field.email': 'E-Mail',
    'field.end': 'Ende',
    'field.from': 'Von',
    'field.hours': 'Stunden',
    'field.maxPerDay': 'Max/Tag',
    'field.newPassword': 'Neues Passwort',
    'field.password': 'Passwort',
    'field.period': 'Zeitraum',
    'field.project': 'Projekt',
    'field.projectOptional': 'Projekt (optional)',
    'field.role': 'Rolle',
    'field.targetPerDay': 'Soll/Tag',
    'field.to': 'Bis',
    'field.user': 'Benutzer',
    'filter.allProjects': 'Alle Projekte',
    'footer.source': 'Quellcode auf GitHub',
    'footer.versionTitle': 'Laufende Version dieser Installation',
    'log.clear': 'Ansicht leeren',
    'log.delay': 'Aktualisierung alle (s)',
    'log.dropped': 'Ältere Zeilen wurden aus dem Puffer verworfen und sind nicht mehr abrufbar.',
    'log.failed': 'Das Protokoll konnte nicht gelesen werden',
    'log.follow': 'Mitlaufen',
    'log.hint': 'Was dieser Prozess geschrieben hat, das Neueste unten. Hier landet nur, was die Protokollstufe zulässt – ein Level darunter anzuhaken zeigt deshalb nichts. Die Stufe steht oben unter „Protokoll, Metriken und Traces" und wirkt ab dem nächsten Start. Nur im Speicher gehalten: nach einem Neustart ist die Ansicht leer, und sie ersetzt keine Protokollsammlung.',
    'log.manual': 'Automatische Aktualisierung ist aus. Für Mitlaufen eine Sekundenzahl eintragen.',
    'log.pause': 'Anhalten',
    'log.levelTooQuiet': 'Diese Installation schreibt {0} und höher, {1} bleibt also leer. Das Log-Level wird unter „Protokollierung, Metriken und Tracing“ geändert und gilt ab dem nächsten Start.',
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
    'admin.markupLegend': 'Was in Banner, Fußzeile und rechtlichem Hinweis möglich ist',
    'markup.year': 'Das aktuelle Jahr — eine Copyright-Zeile bleibt richtig, ohne dass jemand sie ändert',
    'markup.date': 'Das heutige Datum, geschrieben wie es die lesende Person schreibt',
    'markup.time': 'Die aktuelle Uhrzeit',
    'markup.version': 'Die hier laufende Version',
    'markup.instance': 'Der oben eingestellte Titel',
    'markup.link': 'Ein Link. Nur http-, https- und mailto-Adressen werden zu Links; alles andere wird als der geschriebene Text angezeigt',
    'markup.note': 'Alles Übrige wird genau so angezeigt, wie es getippt wurde. Diese Texte lesen auch Personen, die noch nicht angemeldet sind — deshalb ist es kein HTML.',
    'maint.enabled': 'Außer Betrieb',
    'maint.hint': 'Weist alle anderen mit einem Hinweis ab, während die Installation weiterläuft. Vor dem Wiederherstellen oder Verschieben der Datenbank zu benutzen: in diesem Zeitraum erfasste Zeiten sind verloren, sobald der Stand zurückgespielt wird, und wer sie erfasst hat, erfährt es nicht.',
    'maint.message': 'Hinweis für alle anderen',
    'maint.messagePlaceholder': 'Ab 14:00 wieder erreichbar',
    'maint.offSaved': 'Die Installation ist wieder in Betrieb.',
    'maint.onSaved': 'Die Installation ist jetzt außer Betrieb.',
    'maint.title': 'Wartungsmodus',
    'maint.who': 'Solange er aktiv ist, können nur diejenigen arbeiten, die diese Installation verwalten dürfen: dieses eingebaute Konto und alle mit der Administrator-Rolle. Alle anderen werden abgewiesen — genau das ist der Zweck.',
    'maint.signInWho': 'Während der Wartung können sich nur Administratoren anmelden.',
    'msg.error': 'Fehler',
    'msg.initFailed': 'Initialisierung fehlgeschlagen',
    'msg.loadFailed': 'Konnte nicht alles laden',
    'msg.rightsChanged': 'Deine Berechtigungen haben sich geändert. Bitte die Seite neu laden.',
    'msg.passwordReset': 'Passwort zurückgesetzt',
    'msg.passwordChanged': 'Passwort geändert. Bitte neu anmelden.',
    'msg.projectArchived': 'Projekt archiviert',
    'msg.projectCompleted': 'Projekt abgeschlossen',
    'msg.projectCreated': 'Projekt angelegt',
    'msg.projectSaved': 'Projekt gespeichert',
    'msg.projectDeleted': 'Projekt gelöscht',
    'msg.roleChanged': 'Rolle geändert',
    'msg.roleCreated': 'Rolle angelegt',
    'msg.roleDeleted': 'Rolle gelöscht',
    'msg.roleSaved': 'Rolle gespeichert',
    'msg.userCreated': 'Benutzer angelegt',
    'msg.userSaved': 'Benutzer gespeichert',
    'msg.userDeleted': 'Benutzer gelöscht',
    'msg.workingTimesSaved': 'Arbeitszeiten gespeichert',
    'nav.admin': 'Einstellungen',
    'nav.calendar': 'Kalender',
    'nav.logout': 'Abmelden',
    'nav.menu': 'Menü',
    'nav.overtime': 'Überstunden',
    'nav.projects': 'Projekte',
    'nav.report': 'Auswertung',
    'nav.roles': 'Rollen',
    'nav.settings': 'Mein Konto',
    'nav.timesheets': 'Zeiteinträge',
    'nav.users': 'Benutzer',
    'ot.balance': 'Saldo',
    'ot.filename': 'ueberstunden',
    'ot.booked': 'Gebucht',
    'ot.empty': 'Keine Buchungen in diesem Zeitraum.',
    'ot.meta': '{0} · Soll {1}/Tag · gebucht {2} von {3}',
    'ot.target': 'Soll',
    'project.create': 'Projekt anlegen',
    'project.edit': 'Projekt bearbeiten',
    'project.hint': 'Ihre Projekte sind Ihre: nur Sie sehen sie, und nur Sie buchen darauf. Zwei Personen an derselben Sache haben je ein eigenes.',
    'project.empty': 'Noch keine Projekte angelegt.',
    'project.open': 'offen',
    'report.result': 'Ergebnis',
    'report.filename': 'auswertung',
    'report.exporting': 'Wird erstellt …',
    'report.chartFailed': 'Das Diagramm konnte nicht in ein Bild umgewandelt werden.',
    'report.total': '{0} gesamt',
    'report.title': 'Auswertung',
    'report.noProject': 'Kein Projekt',
    'report.chartKind': 'Diagrammart',
    'report.bars': 'Balken',
    'report.columns': 'Säulen',
    'report.pie': 'Kreis',
    'report.byProject': 'Stunden je Projekt',
    'report.byDay': 'Stunden je Tag',
    'role.create': 'Rolle anlegen',
    'role.edit': 'Rolle „{0}“ bearbeiten',
    'role.empty': 'Keine Rollen vorhanden.',
    'role.permissions': 'Berechtigungen',
    'role.rights': 'Rechte',
    'role.shippedRole': 'Mitgelieferte Rolle',
    'role.systemRole': 'Systemrolle',
    'action.view': 'anzeigen',
    'perm.legend': 'Was die einzelnen Rechte erlauben',
    'perm.group.users': 'Konten',
    'perm.group.roles': 'Rollen',
    'perm.group.settings': 'Einstellungen',
    'perm.group.projects': 'Projekte',
    'perm.group.timesheets': 'Zeiten',
    'perm.group.reports': 'Auswertungen',
    'perm.users:read': 'Konten sehen',
    'perm.users:write': 'Konten anlegen und ändern',
    'perm.users:delete': 'Konten löschen',
    'perm.roles:read': 'Rollen sehen',
    'perm.roles:write': 'Rollen anlegen und ändern',
    'perm.settings:manage': 'Installation verwalten',
    'perm.projects:read': 'Projekte sehen',
    'perm.projects:write': 'Projekte anlegen und ändern',
    'perm.projects:delete': 'Projekte löschen',
    'perm.projects:archive': 'Projekte archivieren',
    'perm.timesheets:read:own': 'Eigene Zeiten sehen',
    'perm.timesheets:write:own': 'Eigene Zeiten erfassen',
    'perm.timesheets:transfer': 'Zeiten auf andere Projekte umbuchen',
    'perm.reports:read:own': 'Eigene Stunden auswerten',
    'perm.settings:write:own': 'Eigene Einstellungen ändern',
    'perm.desc.users:read': 'Die Liste der Konten, ihre Adressen und welche Rolle jedes hat. Nicht ihre erfassten Zeiten – die sieht niemand außer der Person selbst.',
    'perm.desc.users:write': 'Ein Konto anlegen, Name oder Adresse ändern, eine Rolle zuweisen.',
    'perm.desc.users:delete': 'Ein Konto entfernen. Die darauf erfassten Zeiten gehen mit.',
    'perm.desc.roles:read': 'Die Rollen auf diesem Bildschirm und was jede gewährt.',
    'perm.desc.roles:write': 'Eine Rolle anlegen, ändern oder eine entfernen, die niemand hat. Die mitgelieferten Rollen bleiben, wie sie sind.',
    'perm.desc.settings:manage': 'Die Installation selbst: Datenbankverbindung, Verzeichnis, Erscheinungsbild, Wartungsmodus, Metriken, Protokoll und Neustart. Dieses Recht macht jemanden zum Administrator.',
    'perm.desc.projects:read': 'Die Liste der Projekte und wie sie heißen.',
    'perm.desc.projects:write': 'Ein Projekt anlegen und ändern.',
    'perm.desc.projects:delete': 'Ein Projekt entfernen. Darauf gebuchte Zeiten behalten ihre Stunden und verlieren das Projekt.',
    'perm.desc.projects:archive': 'Ein Projekt für neue Buchungen schließen, ohne zu löschen, was darauf liegt.',
    'perm.desc.timesheets:read:own': 'Die eigenen Einträge, den eigenen Kalender und den eigenen Saldo. Es gibt kein Recht, das fremde öffnet.',
    'perm.desc.timesheets:write:own': 'Eigene Zeiten erfassen, korrigieren und löschen – von Hand oder mit der Stoppuhr.',
    'perm.desc.timesheets:transfer': 'Eigene Einträge gesammelt von einem Projekt auf ein anderes umbuchen.',
    'perm.desc.reports:read:own': 'Bericht und Diagramme über die eigenen Stunden.',
    'perm.desc.settings:write:own': 'Das eigene Konto: Kennwort, Sprache, Erscheinungsbild, Zeitzone, Arbeitszeiten, Zwei-Faktor.',
    'role.view': 'Rolle „{0}“',
    'role.startNew': 'Stattdessen eine neue Rolle anlegen',
    'role.shippedFixed': 'Diese Rolle wird mitgeliefert und wird angezeigt, nicht bearbeitet: weder Name noch Beschreibung noch Rechte. Wer hier arbeiten und zusätzlich verwalten soll, bekommt die Rolle „Benutzer & Administrator“.',
    'role.desc.admin': 'Verwaltet die Installation, ihre Konten und deren Rollen – und erfasst selbst keine Zeit.',
    'role.desc.user': 'Verwaltet die eigenen Zeiten, Projekte und den eigenen Kalender.',
    'role.desc.user-admin': 'Beides: arbeitet hier und verwaltet zusätzlich die Installation.',
    'role.name.admin': 'Administrator',
    'role.name.user': 'Benutzer',
    'role.name.user-admin': 'Benutzer & Administrator',
    'role.none': 'ohne Rolle',
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
    'ts.more': 'Mehr anzeigen',
    'ts.showing': '{0} von {1} Einträgen',
    'ts.edit': 'Eintrag bearbeiten',
    'ts.entries': 'Einträge',
    'ts.noProject': 'Kein Projekt',
    'unit.hours': 'Std.',

    // Updating. The card says something true on every deployment, which is why
    // there are three endings rather than one: this can restart itself, this
    // cannot, and this is a container where replacing the binary would be undone.
    'update.title': 'Version',
    'update.running': 'Diese Installation läuft mit {0}',
    'update.current': '{0} ist die installierte Version.',
    'update.found': '{0} ist verfügbar. Diese Installation läuft mit {1}.',
    'update.check': 'Aktualisierung suchen',
    'update.checking': 'Wird gesucht …',
    'update.install': 'Herunterladen und installieren',
    'update.notes': 'Was sich geändert hat ↗',
    'update.pending': '{0} ist heruntergeladen und startet mit dem nächsten Neustart.',
    'update.pendingRestart': 'Den Neustart unten verwenden oder die Anwendung selbst neu starten.',
    'update.willRestart': 'Der Download wird gegen die Prüfsumme des Releases geprüft. '
      + 'Die Anwendung startet sich danach selbst neu.',
    'update.willAskRestart': 'Der Download wird gegen die Prüfsumme des Releases geprüft. '
      + 'Danach muss die Anwendung von Hand neu gestartet werden — dieses System kann '
      + 'sich nicht selbst neu starten.',
    'update.inContainer': 'Dies läuft in einem Container. Ein ausgetauschtes Programm wäre '
      + 'beim nächsten Neuaufbau wieder weg — stattdessen das Abbild aktualisieren: '
      + 'docker compose pull && docker compose up -d',
    'update.byImage': 'Es wird ein neues Abbild geladen, dieser Container daraus neu erzeugt und das ersetzte Abbild anschließend entfernt. Die Anwendung ist einige Sekunden weg und kommt als neue Fassung zurück – diese Seite wartet darauf.',
    'update.replacing': 'Ein neues Abbild wird geladen und dieser Container ersetzt …',
    'update.off': 'Die Suche nach neuen Versionen ist auf dieser Installation '
      + 'abgeschaltet (UPDATE_CHECK).',
    'update.confirm': 'Die neue Version herunterladen und installieren? '
      + 'Die Anwendung startet danach neu.',
    'update.downloading': 'Die neue Version wird heruntergeladen und geprüft …',
    'update.done': 'Die neue Version läuft.',
    'update.installedRestart': 'Die neue Version liegt bereit. Zum Verwenden die '
      + 'Anwendung neu starten.',
    'update.unversioned': 'Dieser Stand wurde nicht aus einem Release gebaut, es gibt '
      + 'also nichts zu vergleichen. Die neueste veröffentlichte Version ist {0}.',
    'update.unversionedAlone': 'Dieser Stand wurde nicht aus einem Release gebaut.',
    'update.unreachable': 'Die neueste Version konnte nicht abgefragt werden',
    'user.create': 'Benutzer anlegen',
    'user.edit': 'Benutzer bearbeiten',
    'user.empty': 'Noch keine Benutzer angelegt.',
    'user.initialPassword': 'leer = Initialpasswort',
    'user.deleteConfirm': 'Trotzdem löschen? Die erfassten Zeiten sind danach unwiederbringlich verloren.',
    'user.ownAccount': 'Inhaberkonto',
    'field.source': 'Herkunft',
    'user.local': 'Lokal',
    'user.fromDirectory': 'Verzeichnis',
    'err.directoryAccountReadOnly': '„{0}“ stammt aus dem Verzeichnis. Name und Adresse werden von dort übernommen; hier lässt sich nur die Rolle ändern.',
    'err.directoryAccountUndeletable': '„{0}“ stammt aus dem Verzeichnis. Bitte den Eintrag dort entfernen — der nächste Abgleich entfernt dieses Konto.',
    'user.resetPassword': 'Passwort zurücksetzen',
    'user.resetTitle': 'Passwort zurücksetzen',
    'user.resetText': 'Gib {0} ein Passwort, mit dem sie sich einmal anmelden '
      + 'kann. Sie muss dann ein eigenes wählen, bevor sie irgendetwas nutzen '
      + 'kann, und alle offenen Sitzungen dieses Kontos enden jetzt.',
    'user.resetConfirm': 'Zurücksetzen',
    'user.resetHint': 'Mindestens {0} Zeichen.',
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

  // The tooltip, for an element whose meaning is a picture. A mark on its own
  // says what it is to somebody who already knows it and nothing to anybody
  // else, and pointing at it is how the second group finds out.
  for (const node of $$('[data-i18n-title]')) {
    if (node.dataset.i18nTitleSource === undefined) {
      node.dataset.i18nTitleSource = node.title;
    }

    node.title = dict[node.dataset.i18nTitle] ?? node.dataset.i18nTitleSource;
  }

  redrawAll();
}

/**
 * How to draw each screen that only renders when somebody asks it to.
 *
 * Everything above translates the markup, which is most of the interface and not
 * the part that goes wrong. What goes wrong is what the script has already
 * written into the page: an evaluation, an overtime balance, an import preview.
 * Those are drawn once, from an answer that arrived once, and nothing translated
 * them again - so switching the language left a screen whose table heading said
 * "Zeitraum" above a cell saying 07/14/2026, beside a total reading "5.01 h in
 * total". Every key was translated; none of them was looked up a second time.
 *
 * The screens that refreshAll reloads do not need this - they are drawn again
 * from a new request. These are the ones nobody asked to reload, and re-fetching
 * them on a language change would fire requests somebody did not ask for and
 * could fail on a form they have since edited. So the answer is kept and drawn
 * again from what is already in hand.
 *
 * Keyed, so a second answer for the same screen replaces the first rather than
 * accumulating closures over responses nobody can see any more.
 */
const redraws = new Map();

/** Draws a screen now, and again whenever the language changes. */
function redrawable(key, draw) {
  redraws.set(key, draw);
  draw();
}

/** Forgets a screen's last answer, for one that has been emptied. */
function stopRedrawing(key) {
  redraws.delete(key);
}

function redrawAll() {
  for (const draw of redraws.values()) draw();

  // The greeting is written entirely by the script and is drawn on arrival
  // rather than by any loader, so nothing else would put it back into the
  // language now in force. Only while it is the screen somebody is looking at:
  // drawing a hidden one costs a request for today's figures that nobody asked
  // for.
  if (me.user && !$('#view-welcome')?.hidden) renderWelcome();
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
 *
 * This is the question "which dictionary", so the answer has to be a language
 * this application actually ships words for. For "which conventions to write a
 * date and a figure in", which is a different question with a better answer,
 * see activeLocale.
 */
function activeLanguage() {
  if (me.user?.language) return me.user.language;

  return detectBrowserLanguage();
}

/**
 * The locale to format dates, times and figures with.
 *
 * The same rule in the same order - a chosen language wins, the browser decides
 * otherwise - but the browser's answer is taken whole rather than reduced to a
 * language this application has a dictionary for.
 *
 * That reduction is right for choosing words and wrong for choosing conventions.
 * There is one English dictionary and there is not one English date: an en-GB
 * browser was being formatted as plain "en", which Intl reads as the American
 * order - so the twelfth of August was shown as 08/12/2026 to somebody whose own
 * browser would have written 12/08/2026. Nothing was mistranslated; the date was
 * simply wrong by five months for half the year.
 *
 * Where the two disagree about the language, the chosen one wins outright:
 * picking EN on a German browser is a decision about how this application should
 * read, and a date is part of how it reads.
 *
 * Where they agree about the language, the browser's tag is used, because it is
 * the same answer with a region on it. That case is not an edge: the first
 * sign-in stores the detected language on the account, so an account whose
 * language nobody ever chose looks exactly like one where somebody did. Treating
 * the stored value as a decision there would take the region away permanently,
 * on the strength of a choice that was never made.
 */
// One thing this cannot reach: the date fields.
//
// <input type="date"> renders in the browser's own UI language and nothing on
// the page changes it. Measured rather than assumed, in Chrome 151, with the
// same value in three inputs - a bare one, one inside a lang="de-DE" wrapper,
// and one carrying lang="de-DE" itself. Started with --lang=en-US all three read
// 08/12/2026; started with --lang=de-DE all three read 12.08.2026, including the
// one on a page whose lang is "en". The attribute is ignored outright.
//
// So a reader who switches this application to a language their browser is not
// set to gets their own convention in the pickers and the chosen one everywhere
// else. The only way to close that is to stop using the native field, and what
// it costs is not obvious in a desktop browser: on a phone this is what becomes
// a proper date wheel, and it is the accessible default a screen reader knows.
function activeLocale() {
  const chosen = me.user?.language;

  if (!chosen) return BROWSER_LOCALE || detectBrowserLanguage();

  if (BROWSER_LOCALE && primarySubtag(BROWSER_LOCALE) === primarySubtag(chosen)) {
    return BROWSER_LOCALE;
  }

  return chosen;
}

/** The language half of a tag: "de" for "de-AT", "de" for "de". */
function primarySubtag(tag) {
  return String(tag).toLowerCase().split('-')[0];
}

/**
 * The browser's own first preference, if Intl can use it.
 *
 * Validated once, here, rather than at each call: an unusable tag makes every
 * Intl constructor throw a RangeError, and a date formatter that throws takes
 * the screen it was rendering with it. Falling back to the language is the same
 * answer as before this existed, which is a worse date and not a broken page.
 */
const BROWSER_LOCALE = (() => {
  const tag = navigator.languages?.[0] ?? navigator.language ?? '';

  try {
    return tag && Intl.getCanonicalLocales(tag).length ? tag : '';
  } catch {
    return '';
  }
})();

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
    if (supported.includes(primarySubtag(preference))) return primarySubtag(preference);
  }

  return FALLBACK_LANGUAGE;
}

/** Translates one key for use in code-generated text. */
function t(key, fallback) {
  return TRANSLATIONS[activeLanguage()]?.[key] ?? fallback;
}

/**
 * Remembers, per account and per browser, that the zone has been offered.
 *
 * An empty stored zone means "follow the instance", which is a real choice - so
 * adopting the browser's zone every time the page loaded would make that choice
 * impossible to keep. The marker is what makes it a one-time suggestion rather
 * than a standing override.
 *
 * The zone only. A language has no "follow the browser" to protect, so it is not
 * kept behind this - see adoptBrowserDefaults.
 */
function adoptionMarker(userID) {
  return `gtr.adopted.${userID}`;
}

/**
 * Writes the browser's zone and language into the account.
 *
 * The browser knows two things the server cannot: which zone the person is
 * actually in, and which language they read. Until this, the language was
 * detected for the current page and thrown away on every load, and the zone was
 * not detected at all - so somebody in Vancouver saw their evening bookings land
 * on the instance's tomorrow until they found the setting.
 *
 * The two are not decided the same way, and the difference is the point.
 *
 * A zone is a one-time suggestion. An empty stored zone means "follow the
 * instance", which is a real choice somebody can make and see offered - so
 * adopting the browser's on every load would make it impossible to keep. It is
 * offered once per account per browser and never again, and only when it differs
 * from the instance's, because writing the same value would take the choice away
 * to no effect at all.
 *
 * A language is not. The picker offers no "follow the browser" line, so an
 * account with none stored is not an account that chose anything - it is one
 * nobody has decided for yet, and nothing on screen could put it back. So it is
 * decided on sight, every time it is found undecided: the browser's language
 * where this interface speaks it, English where it does not.
 *
 * Returns whether anything was written, so the caller can read the account back.
 */
async function adoptBrowserDefaults() {
  if (!me.user) return false;

  let adopted = false;

  // The language first, and outside the marker below on purpose. Under it, an
  // adoption that was missed once - storage switched off, or a first sign-in on
  // a browser that had already recorded one for this account - left the account
  // with no language for ever, and its picker blank.
  if (!me.user.language) {
    try {
      await api('/me/language', {
        method: 'PUT',
        body: JSON.stringify({ language: detectBrowserLanguage() }),
      });

      adopted = true;
    } catch {
      // It still reads in the detected language for this page; it is simply not
      // remembered, and the next sign-in asks again.
    }
  }

  const marker = adoptionMarker(me.user.id);

  try {
    if (window.localStorage.getItem(marker)) return adopted;
  } catch {
    // Private browsing, or storage switched off. Suggesting the zone once per
    // load is worse than never suggesting it, so that half stops here.
    return adopted;
  }

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

  try {
    window.localStorage.setItem(marker, '1');
  } catch {
    // Nothing to do. Worst case the zone is suggested again on the next load,
    // and the conditions above make that a no-op.
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
      el('span', {
        text: ` · ${roleTitle(me.user.role) || t('role.none', 'no role')}`,
      }),
    );
  } else {
    who.replaceChildren(el('span', { text: t('auth.disabled', 'Authentication disabled') }));
  }

  $("#password-banner").hidden = !me.user.mustChangePassword;
  $("#logout").hidden = !me.authEnabled;

  applyLanguage(activeLanguage());
  applyAccountTheme();
  applyPermissionVisibility();

  renderTOTPState();
}

async function loadUsers() {
  // Listing accounts is a permission, and administering them is the only thing this
  // list is for now.
  //
  // It used to be loaded for everybody, including whoever may not list accounts - who
  // got a list of exactly one entry, themselves. That was to fill four dropdowns of
  // colleagues: the booking form, the entry filter, the calendar and the overtime
  // form. They were built when a role could hold timesheets:read:all, and they never
  // worked as they read - the built-in administrator administers accounts and does not
  // read what people recorded in them, so it was offered every colleague and every
  // choice but itself came back 403. The filter was worse: "All users" quietly showed
  // only your own entries, because the server pins the scope and the label did not
  // know.
  //
  // A dropdown holding a single name is a question with one answer, so all four are
  // gone, and with them the two rights they existed to use. Whose time it is is not a
  // choice any more, it is who is signed in.
  //
  // Emptied rather than left alone on the way out: a session that loses the right -
  // its role changed while it was signed in - must not go on showing the accounts it
  // read a moment ago.
  if (!can('users:read')) {
    cache.users = [];

    return;
  }

  cache.users = (await api('/users'))?.items ?? [];

  const rows = cache.users.map((u) => {
    const actions = el('td', { class: 'actions' });

    // Whose account this is somebody else's to correct.
    //
    // Not the built-in administrator: it is the way back into an installation,
    // and its name and address are what an operator looks for when everything
    // else has gone wrong. Not the row somebody is reading their own name in
    // either - an administration screen is for other people's accounts, and a
    // person changing their own address here would be doing it in the one place
    // that cannot ask them for their password first.
    //
    // Not a directory account, and that one is the server's rule rather than
    // this screen's: the name and the address are copied from the entry on every
    // synchronisation, so a change made here holds until the next run and then
    // reverts. It is refused for exactly that reason, and a button that asks for
    // a refusal teaches somebody the screen is broken.
    if (can('users:write') && !u.isSystem && !u.isExternal && u.id !== me.user?.id) {
      actions.append(el('button', {
        class: 'link',
        'data-action': 'edit',
        text: t('action.edit', 'edit'),
        onclick: () => editUser(u),
      }));
    }

    // Letting somebody back in who has forgotten their password.
    //
    // The same three rows are left out as for editing, and for the same reasons
    // the server gives: their own account is changed under My account, which
    // asks for the password they have; the built-in administrator is the way
    // back into an installation; and a directory account's password lives in the
    // directory, so one set here could never be used.
    if (can('users:write') && !u.isSystem && !u.isExternal && u.id !== me.user?.id) {
      actions.append(el('button', {
        class: 'link',
        'data-action': 'reset-password',
        text: t('user.resetPassword', 'reset password'),
        onclick: () => resetPassword(u),
      }));
    }

    // Not your own, which the server refuses too: deleting it would end the
    // session the request arrived on and leave whoever pressed it looking at a
    // sign-in screen for an account that no longer exists.
    // Not a directory account either: removing it here removes a person, and the
    // next synchronisation creates them again from the entry that is still
    // there - so the hours are gone and nothing else has changed.
    if (can('users:delete') && !u.isSystem && !u.isExternal && u.id !== me.user?.id) {
      actions.append(deleteButton({
        label: `${u.name} <${u.email}>`,
        path: `/users/${u.id}`,
        message: t('msg.userDeleted', 'User deleted'),
        after: refreshAll,
        // Deleting one account asks something extra - whether to keep its
        // entries or purge them - so the row keeps its own dialog. A batch takes
        // the plain deletion, the answer that keeps the history.
        ask: () => deleteUser(u),
      }));
    }

    // What this row is, where it is not simply another account. The two are
    // different facts and were being told apart by whichever came first: the
    // built-in administrator is the installation's own account, and the row
    // somebody is reading their own name in is theirs. Both can be true, and the
    // one that matters to the person looking is which one is theirs.
    if (u.id === me.user?.id) {
      actions.append(el('span', {
        class: 'muted',
        text: t('user.ownAccount', 'Your account'),
      }));
    } else if (u.isSystem) {
      actions.append(el('span', {
        class: 'muted',
        text: t('user.systemAccount', 'System account'),
      }));
    }


    const roleCell = el('td', {});

    // The built-in account's role is shown as text rather than as a control that
    // cannot be used. A disabled dropdown still looks like something to click,
    // and the answer to clicking it is "no" - which is worse than not offering it.
    // Its role is fixed anyway: it must keep one that can still administer.
    if (u.isSystem) {
      roleCell.append(el('span', { text: roleTitle(u.role) }));
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
      roleCell.textContent = roleTitle(u.role) || '–';
    }

    return el('tr', { class: me.user && u.id === me.user.id ? 'self' : '' },
      el('td', { text: u.name }),
      el('td', { text: u.email }),
      roleCell,
      // Where the account is kept. It decides what may be done to it here, and
      // was only visible as a note beside the buttons - which is the wrong place
      // for the thing that explains why two of them are missing.
      el('td', {
        text: u.isExternal
          ? t('user.fromDirectory', 'Directory')
          : t('user.local', 'Local'),
        class: u.isExternal ? 'muted' : '',
        title: u.isExternal
          ? t('user.directoryHint',
            'Managed in LDAP. The password lives there, and removing the entry there removes this account.')
          : '',
      }),
      el('td', { class: 'num', text: u.dailyTargetHours ? u.dailyTargetHours.toFixed(1) : t('field.default', 'default') }),
      el('td', { class: 'num', text: u.maxDailyHours ? u.maxDailyHours.toFixed(1) : t('field.default', 'default') }),
      actions,
    );
  });

  fillTable($('#table-users tbody'), rows, 7, t('user.empty', 'No users yet.'));
}

/**
 * Gives an account a password it can get back in with.
 *
 * Asked before it is done, and the question names the person: an administrator
 * with a table of accounts open is one mis-click away from resetting the wrong
 * one, and "are you sure" would not have said which.
 *
 * What the dialog says is what actually happens, both halves of it. The account
 * has to replace this password before it can use anything, and every session it
 * has open ends now - a cookie is not re-checked against the password that
 * opened it, so a reset that left them running would leave the door it was
 * closing wide open.
 */
async function resetPassword(user) {
  const chosen = await confirmDialog({
    title: t('user.resetTitle', 'Reset password'),
    text: fillIn(t('user.resetText',
      'Give {0} a password to sign in with once. They have to choose their own '
      + 'before they can use anything, and any session this account has open ends '
      + 'now.'), [user.name]),
    confirmLabel: t('user.resetConfirm', 'Reset password'),
    danger: false,
    field: {
      id: 'reset-password-field',
      generateID: 'reset-password-generate',
      label: t('field.newPassword', 'New password'),
      hint: fillIn(t('user.resetHint', 'At least {0} characters.'), [MIN_PASSWORD_LENGTH]),
      minLength: MIN_PASSWORD_LENGTH,
      generate: t('action.generate', 'Generate'),
    },
  });

  if (!chosen) return;

  await patch(`/users/${user.id}/password`, { password: chosen },
    t('msg.passwordReset', 'Password reset'), refreshAll);
}

/** Opens an existing account in the form above the table. */
function editUser(user) {
  const form = $('#form-user');
  if (!form) return;

  form.elements.id.value = String(user.id);
  form.elements.name.value = user.name;
  form.elements.email.value = user.email;

  showUserCreationFields(false);

  $('#user-form-title').textContent = t('user.edit', 'Edit user');
  $('#user-submit').textContent = t('action.save', 'Save');
  $('#user-cancel').hidden = false;

  switchView('users');
  form.scrollIntoView({ block: 'nearest' });
  form.elements.name.focus();
}

/** Puts the form back to adding somebody. */
function resetUserForm() {
  const form = $('#form-user');
  if (!form) return;

  form.reset();
  form.elements.id.value = '';

  showUserCreationFields(true);

  $('#user-form-title').textContent = t('user.create', 'Add user');
  $('#user-submit').textContent = t('action.create', 'Create');
  $('#user-cancel').hidden = true;
}

/**
 * Shows or hides the two fields that only a new account has.
 *
 * The required flag goes with the role rather than staying on it. A control that
 * is hidden and still required is one the browser refuses to submit past and
 * cannot scroll to, so the form does nothing at all and says nothing about why -
 * which is a worse outcome than either of the two this is choosing between.
 */
function showUserCreationFields(shown) {
  const role = $('#user-role-field');
  const password = $('#user-password-field');

  if (role) role.hidden = !shown;
  if (password) password.hidden = !shown;

  const chooser = $('#form-user select[name=role]');
  if (chooser) chooser.required = shown;
}

async function loadRoles() {
  if (!can('roles:read')) return;

  cache.roles = (await api('/roles'))?.items ?? [];

  // Its own failure, rather than everybody's. This runs early in the sequence
  // that loads every screen, so a refusal here used to abort the rest of it -
  // and the screens further down, the directory card among them, were left
  // holding whatever the markup shipped with. The rights editor is the only
  // thing that needs this list; nothing else on the page should go dark because
  // it could not be had.
  try {
    cache.permissions = (await api('/permissions'))?.permissions ?? [];
  } catch {
    cache.permissions = [];
  }

  fillSelect($('#form-user select[name=role]'), roleChoices(),
    { labelKey: 'label', valueKey: 'name' });

  // Not over a role somebody is part way through. This runs on every reload of
  // the screens - a language chosen, a neighbouring card saved - and it rebuilds
  // the switches from nothing, so the rights ticked for a role that has not been
  // saved were swept away without a reload being involved at all.
  //
  // Only this call. Opening a role and starting a new one both redraw these on
  // purpose, and are somebody asking for it.
  if (!beingEdited($('#form-role'))) renderPermissionCheckboxes();

  const rows = cache.roles.map((role) => {
    const actions = el('td', { class: 'actions' });

    // A shipped role cannot be changed in any part - not its name, not its
    // description, not its rights - so the way in says "view". "Edit" was an
    // offer the server refuses, which is a button that teaches somebody the
    // screen is broken.
    const shipped = role.isSystem || role.isDefault;

    if (can('roles:write')) {
      actions.append(el('button', {
        class: 'link',
        text: shipped ? t('action.view', 'view') : t('action.edit', 'edit'),
        onclick: () => editRole(role),
      }));

      // Shipped roles are not offered a delete either, because the server
      // refuses one: they are what every new account and every synchronised one
      // is assigned.
      if (!shipped) {
        actions.append(deleteButton({
          label: `${t('field.role', 'Role')} "${roleTitle(role.name)}"`,
          path: `/roles/${role.id}`,
          message: t('msg.roleDeleted', 'Role deleted'),
          after: refreshAll,
        }));
      }
    }

    if (role.isSystem) {
      actions.append(el('span', { class: 'muted', text: t('role.systemRole', 'System role') }));
    } else if (role.isDefault) {
      actions.append(el('span', {
        class: 'muted',
        text: t('role.shippedRole', 'Shipped role'),
      }));
    }

    return el('tr', {},
      // The title, and the identifier under it for the three that have both: the
      // identifier is what the API takes and what the directory configuration stores,
      // so the screen that administers roles is the one place it has to stay visible.
      el('td', {}, el('span', { text: roleTitle(role.name) }),
        ...(roleTitle(role.name) === role.name
          ? []
          : [el('span', { class: 'muted', text: ` ${role.name}` })])),
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

  // Grouped by area, in the order the rights arrive. Fifteen boxes in one run
  // is a wall to read; six short groups is a list to scan, and the grouping is
  // already in the identifiers rather than invented here.
  let group = '';

  for (const permission of cache.permissions) {
    const area = permissionGroup(permission);

    if (area.key !== group) {
      group = area.key;
      list.append(el('h3', { class: 'perm-group', text: area.label }));
    }

    // A switch, like every other on-or-off control in this interface. A right is
    // held or it is not, which is the question a switch asks - and fifteen of
    // them read as settings rather than as a form to fill in.
    list.append(el('label', { class: `switch perm-switch${fixed ? ' muted' : ''}` },
      el('input', {
        type: 'checkbox',
        name: 'permissions',
        value: permission,
        checked: selected.includes(permission),
        disabled: fixed,
      }),
      el('span', { class: 'switch-track' }),
      el('span', { class: 'perm-name', text: permissionTitle(permission) }),
      // The identifier stays: it is what the API takes and what a directory
      // configuration stores, so this screen is where it has to remain readable.
      el('code', { class: 'perm-id', text: permission }),
    ));
  }

  renderPermissionLegend();

  // Why they cannot be set, next to them rather than in a notice after the fact,
  // and beside it the thing that can be done instead.
  const note = $('#role-fixed-note');

  if (note) note.hidden = !fixed;

  const startNew = $('#role-start-new');

  if (startNew) startNew.hidden = !fixed;
}

/** The legend: every right, and one sentence on what it actually allows. */
function renderPermissionLegend() {
  const list = $('#permission-legend-list');
  if (!list) return;

  list.replaceChildren();

  for (const permission of cache.permissions) {
    const detail = permissionDetail(permission);
    if (!detail) continue;

    list.append(el('dt', {},
      el('span', { text: permissionTitle(permission) }),
      el('code', { class: 'perm-id', text: permission })));
    list.append(el('dd', { text: detail }));
  }
}

function editRole(role) {
  const form = $('#form-role');

  // Every shipped role, not only the administrator's: the server refuses a
  // rename, a changed right and a changed description on all three, and a form
  // that offers what the server refuses is worse than one that does not.
  const shipped = role.isSystem || role.isDefault;

  form.elements.id.value = String(role.id);

  // Shown in the reader's language, because this is a reading screen. The
  // identifier and the English description in the database are what a role is
  // stored as; what a person is looking at here is the role, and it has a name
  // and a purpose in their own language everywhere else on this screen.
  form.elements.name.value = shipped ? roleTitle(role.name) : role.name;
  form.elements.description.value = shipped
    ? roleDescription(role)
    : (role.description ?? '');

  form.elements.name.readOnly = shipped;
  form.elements.description.readOnly = shipped;

  renderPermissionCheckboxes(role.permissions, { fixed: shipped });

  // Nothing to save, so nothing offering to. The way out is "New", which is
  // still there beside it.
  const save = $('#form-role button[type="submit"]');
  if (save) save.hidden = shipped;

  $('#role-form-title').textContent = shipped
    ? t('role.view', 'Role {0}').replace('{0}', roleTitle(role.name))
    : t('role.edit', 'Edit role').replace('{0}', roleTitle(role.name));

  switchView('roles');

  // Straight to the rights, which is what somebody pressed the button for.
  //
  // The form is a long way down a screen that starts with the table of roles,
  // and the legend under the rights made it longer still - so opening a role
  // used to leave the reader at the top of the table they were already looking
  // at, with the thing they asked for somewhere below the fold.
  //
  // On the next frame, because the view is only just unhidden and an element in
  // a hidden one has nowhere to be scrolled to. The offset for the sticky bar is
  // the page's own: html carries scroll-padding-top, which every scroll into
  // view respects.
  requestAnimationFrame(() => {
    $('#form-role .perms')?.scrollIntoView({ block: 'start', behavior: 'smooth' });
  });
}

function resetRoleForm() {
  const form = $('#form-role');
  form.reset();
  form.elements.id.value = '';
  form.elements.name.readOnly = false;
  form.elements.description.readOnly = false;

  const save = $('#form-role button[type="submit"]');
  if (save) save.hidden = false;

  renderPermissionCheckboxes();
  $('#role-form-title').textContent = t('role.create', 'Create role');
}

async function loadProjects() {
  if (!can('projects:read')) return;

  cache.projects = (await api('/projects'))?.items ?? [];

  const bookable = cache.projects.filter((p) => p.status === 'active');
  // A blank first option is what lets time be booked without a project, and it
  // is named the same in both places. The stopwatch called it "Project
  // (optional)" while the form beside it called it "No project" - two names for
  // the same choice, on one screen.
  fillSelect($('#timer-project'), bookable,
    { placeholder: t('ts.noProject', 'No project') });
  fillSelect($('#form-timesheet select[name=projectId]'), bookable,
    { placeholder: t('ts.noProject', 'No project') });
  // An evaluation covers all projects, one of them, or the hours that belong to
  // none - so this select carries two options that are not projects. The picker
  // used to be required and offer only real ones, which made the two questions
  // people ask most often the two the screen could not answer.
  //
  // "none" is appended after the projects and the choice restored by hand,
  // because fillSelect restores before it exists and would drop it every reload.
  const reportProject = $('#form-report select[name=projectId]');
  const chosenScope = reportProject.value;

  fillSelect(reportProject, cache.projects,
    { placeholder: t('filter.allProjects', 'All projects') });
  reportProject.append(el('option', {
    value: 'none', text: t('report.noProject', 'No project'),
  }));
  reportProject.value = chosenScope;
  // The same three choices the evaluation offers, and for the same reason: the
  // hours that never got a project are the ones somebody goes looking for, and
  // "all projects" cannot narrow to them.
  //
  // Appended after the projects and the choice restored by hand, because
  // fillSelect restores before this option exists and would drop it every reload.
  const timesheetProject = $('#filter-ts-project');
  const chosenProject = timesheetProject.value;

  fillSelect(timesheetProject, cache.projects,
    { placeholder: t('filter.allProjects', 'All projects') });

  timesheetProject.append(el('option', {
    value: 'none', text: t('report.noProject', 'No project'),
  }));

  timesheetProject.value = chosenProject;

  const rows = cache.projects.map((p) => {
    const actions = el('td', { class: 'actions' });

    // Every project belongs to somebody, and the only ones anybody is handed are
    // their own - so this is true of every row here. It stays because it is the
    // question the delete button is really asking: you may remove your own way of
    // organising your hours whatever your project permissions say.
    const mine = me.user && p.ownerId === me.user.id;

    // Offered whatever the status. An archived project is a closed record and a
    // name typed wrongly is still typed wrongly in it - and the server does not
    // refuse the change, so a screen that hid the button would be inventing a
    // rule of its own rather than reflecting one.
    if (can('projects:write')) {
      actions.append(el('button', {
        class: 'link',
        'data-action': 'edit',
        text: t('action.edit', 'edit'),
        onclick: () => editProject(p),
      }));
    }

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

/** Opens an existing project in the form above the table. */
function editProject(project) {
  const form = $('#form-project');
  if (!form) return;

  form.elements.id.value = String(project.id);
  form.elements.name.value = project.name;
  form.elements.description.value = project.description ?? '';

  // Through setDateField, because the visible box beside a date input is a second
  // field kept in step with it: writing the value straight in changes the one
  // nobody looks at and leaves the one everybody does reading the old date.
  setDateField(form.elements.startDate, project.startDate ?? '');
  setDateField(form.elements.endDate, project.endDate ?? '');

  $('#project-form-title').textContent = t('project.edit', 'Edit project');
  $('#project-submit').textContent = t('action.save', 'Save');
  $('#project-cancel').hidden = false;

  switchView('projects');
  form.scrollIntoView({ block: 'nearest' });
  form.elements.name.focus();
}

/** Puts the form back to making one. */
function resetProjectForm() {
  const form = $('#form-project');
  if (!form) return;

  form.reset();
  form.elements.id.value = '';

  // Emptied through setDateField for the same reason they were filled through it,
  // and only then today back into the start: reset empties it, and making a
  // second project should ask no more than the first one did.
  setDateField(form.elements.startDate, '');
  setDateField(form.elements.endDate, '');
  fillToday(form.elements.startDate);

  $('#project-form-title').textContent = t('project.create', 'Create project');
  $('#project-submit').textContent = t('action.create', 'Create');
  $('#project-cancel').hidden = true;
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
 * Whose it is and whether they may write their own time, and nothing else. It used
 * to also say that an approved entry is refused by the API however it is reached -
 * a rule that went with the review path, and a comment that outlived it by
 * describing a refusal the server no longer makes.
 *
 * The answer lives here so the list, the calendar and the row click cannot
 * disagree about it.
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
  setDateField(form.elements.date, entry.date);
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
 * records: two copies of these rules would be two places for "whose entry is this"
 * to be answered differently, and the copy nobody was looking at would be the
 * wrong one.
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

/**
 * How many entries the list asks for at a time.
 *
 * A screenful rather than everything. This list used to ask for every entry the
 * account had ever booked, on every load and again after every save, and the
 * request grew for as long as somebody kept recording time - the kind of slow
 * that arrives a week at a time and never becomes a bug report.
 *
 * Smaller than the API's own default page, deliberately: the endpoint answers
 * clients that are fetching rather than showing, and a table asks for what it can
 * put on a screen. Both are bounded, which is the part that matters.
 */
const TIMESHEET_PAGE = 25;

/** The entries the table is showing, which is a page of them at a time. */
let timesheetEntries = [];

/** How many there are altogether, which is what says whether there are more. */
let timesheetTotal = 0;

/**
 * Loads the entries, one page at a time.
 *
 * `more` appends the next page instead of starting again. Everything else -
 * changing the filter, booking, correcting, deleting - starts again from the
 * first page on purpose: the list has changed underneath, and an offset into a
 * list that has moved skips one entry and repeats another.
 */
async function loadTimesheets(more = false) {
  if (!can('timesheets:read:own')) return;

  const params = new URLSearchParams();
  const projectId = $('#filter-ts-project').value;
  if (projectId) params.set('projectId', projectId);

  if (!more) timesheetEntries = [];

  params.set('limit', String(TIMESHEET_PAGE));
  params.set('offset', String(timesheetEntries.length));

  const answer = await api(`/timesheets?${params}`);
  const page = answer?.items ?? [];

  // What the server says, not what arrived: those differ exactly when there is
  // another page, which is the whole question this screen has to answer.
  timesheetTotal = answer?.totalCount ?? page.length;
  timesheetEntries = timesheetEntries.concat(page);

  const entries = timesheetEntries;

  // Named `entry` rather than `t`, which would shadow the translation helper.
  // No user column and no "this row is yours" highlight: every row is, so a column
  // repeating one name and a shade behind every line say nothing.
  const rows = entries.map((entry) => {
    const actions = timesheetActions(entry);

    return el('tr', {},
      el('td', { text: fmtDate(entry.date) }),
      el('td', {
        class: entry.projectId ? '' : 'empty',
        text: entry.projectId ? projectName(entry.projectId) : t('ts.noProject', 'No project'),
      }),
      el('td', { class: 'num', text: fmtNumber(entry.durationHours) }),
      el('td', { text: entry.description ?? '–' }),
      actions,
    );
  });

  fillTable($('#table-timesheets tbody'), rows, 5, t('ts.empty', 'No entries for this filter.'));

  showTimesheetTally();
}

/**
 * Says how much of the list is on screen, and offers the rest.
 *
 * Both go down together once everything is showing. A table that is complete and
 * one that has been cut short are otherwise the same picture, which is the only
 * way bounding the request could have gone wrong quietly.
 */
function showTimesheetTally() {
  const more = timesheetEntries.length < timesheetTotal;

  $('#ts-more-row').hidden = !more;

  $('#ts-shown').textContent = more
    ? fillIn(t('ts.showing', 'Showing {0} of {1} entries'),
      [timesheetEntries.length, timesheetTotal])
    : '';
}

// ----------------------------------------------------------------- calendar

/**
 * The month the calendar is showing, as a Date on the first of that month.
 *
 * Empty until something renders it, because which month somebody is in is a
 * question about their own zone, and the zone is not known until they are signed
 * in. It used to be worked out from the browser's clock as this file loaded, which
 * is a different month from theirs for part of the first and the last day of every
 * one - so an account in Auckland read on a Berlin machine opened the calendar on
 * the month it had just left, with today highlighted nowhere in the grid.
 */
let calendarMonth = null;

const ISO_DAY = (d) => `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;

/** The first of the month a YYYY-MM-DD day falls in. */
function monthOf(iso) {
  const [year, month] = iso.split('-').map(Number);

  return new Date(year, month - 1, 1);
}

/** The month to show, worked out from this user's own today the first time it is asked. */
function currentCalendarMonth() {
  if (!calendarMonth) calendarMonth = monthOf(todayISO());

  return calendarMonth;
}

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
    setDateField(field, autofilledDate);
  }
}

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

  const first = currentCalendarMonth();
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
        (e) => (e.projectId ? projectName(e.projectId) : t('ts.noProject', 'No project'))))];
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
      el('td', { text: entry.projectId ? projectName(entry.projectId) : t('ts.noProject', 'No project') }),
      el('td', { class: 'num', text: fmtNumber(entry.durationHours) }),
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
    const from = currentCalendarMonth();
    calendarMonth = new Date(from.getFullYear(), from.getMonth() + months, 1);
    mutate(loadCalendar, null, null);
  };

  $('#calendar-prev').addEventListener('click', () => shift(-1));
  $('#calendar-next').addEventListener('click', () => shift(1));
  $('#calendar-today').addEventListener('click', () => {
    // This person's today, not the browser's - the same reason the grid highlights
    // it from todayISO(). The two disagreeing is what sent the button to a month
    // that did not contain the day it was named after.
    calendarMonth = monthOf(todayISO());
    mutate(loadCalendar, null, null);
  });
  $('#calendar-day-close').addEventListener('click', () => { $('#calendar-day-card').hidden = true; });
}

function fillSettingsForm() {
  if (!me.user) return;

  const form = $('#form-working-times');

  // Not over somebody who is part way through filling it in. This runs after
  // every save on the screen and after a language is chosen, and it used to
  // replace whatever had been typed with the server's copy.
  if (beingEdited(form)) return;

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
    el('td', { text: fmtMoment(token.createdAt) }),
    el('td', { text: token.expiresAt ? fmtMoment(token.expiresAt) : t('token.never', 'unlimited') }),
    el('td', { text: token.lastUsedAt ? fmtMoment(token.lastUsedAt) : t('token.unused', 'never') }),
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

/**
 * Whether this account administers the installation and has no working day.
 *
 * The built-in administrator, and anybody holding the admin role - which is the
 * same thing said twice: that role is the administration and none of the working
 * day, and an installation that hands it to somebody has decided that person
 * administers. This asked whether the caller *was* the built-in account, so the
 * few things reserved for it stayed reserved for it, and the way to reach them
 * was to sign in as the one account nobody can attribute to a person.
 *
 * Asked as a shape rather than by the role's name, which an installation may have
 * changed. The combined role is deliberately not this: somebody who also books
 * time keeps their own screens, their walk through and their working times, and
 * was never meant to inherit the directory purge with them.
 *
 * Mirrors Authorizer.AdministersOnly, and has to: this decides what is offered
 * and that decides what is answered. A screen offering what the server refuses is
 * worse than one that offers nothing.
 */
const administersOnly = () => !me.authEnabled
  || (me.user?.isSystem ?? false)
  || (can('settings:manage') && !can('timesheets:write:own'));

/**
 * Applies the instance branding.
 *
 * Fetched separately from /me because the sign-in screen must show the title
 * and logo before anyone has authenticated.
 */
async function loadBranding() {
  const branding = await api('/branding');

  lastBranding = branding;

  // The first answer of the page's life, which is the build this document was
  // written by. Every later one is about the process rather than about the tab.
  if (loadedVersion === null) loadedVersion = branding.version ?? '';

  // Drawn through the redraw registry, so switching language picks the texts
  // again. Everything on this screen that a language change reaches goes the same
  // way; a title and a banner written in two languages would otherwise stay in
  // whichever one the page happened to load in.
  redrawable('branding', () => drawBranding(branding));

  return branding;
}

function drawBranding(branding) {

  // Remembered on the device, so the next load has the instance's own name and
  // mark before it is painted rather than a second later. theme.js reads this;
  // see the note there for why a reload otherwise flickers back to a name nobody
  // chose.
  const title = brandingIn(branding, 'title') || 'Time Recording';

  // The tab may be named separately, because the room runs out there first: a
  // name that reads across the top of the screen is cut off after a couple of
  // dozen characters in a tab, and somebody has six of them open. Where nothing
  // separate was written, the two are the same name.
  const tabTitle = brandingIn(branding, 'tabTitle') || title;

  // Remembered on the device, so the next load has the instance's own name and
  // mark before it is painted rather than a second later. theme.js reads this;
  // see the note there for why a reload otherwise flickers back to a name nobody
  // chose. The tab's name resolved rather than both fields, since the tab is the
  // only thing that reads it.
  try {
    localStorage.setItem('gtr.branding', JSON.stringify({
      title: tabTitle,
      logo: branding.logo ?? '',
    }));
  } catch {
    // Private browsing refuses storage outright, which costs only the flicker.
  }

  document.title = tabTitle;
  // Into the span rather than the button: the button also holds the mark, and
  // writing text onto the button would take the mark out with it.
  $('#app-title-text').textContent = title;

  // These two places show the installation's own logo and nothing else.
  //
  // For a while they fell back to the shipped mark, on the reasoning that a
  // header of words alone looks unfinished. It is the wrong place for it: these
  // are the slots a company's own mark goes in, and filling them with ours makes
  // an unbranded installation look like it is branded by somebody else. The
  // application's own mark has its own place - the browser tab, and the button
  // beside the title - which is where it says which program this is without
  // claiming the space meant for whoever runs it.
  //
  // Each place gets the logo at the size it draws, made when the logo was saved.
  // The header used to be handed the original - a wordmark of a few hundred
  // kilobytes - and scaled it down with CSS, which is a large download to draw a
  // 40px mark and one every visitor of the sign-in screen paid for.
  //
  // The original is the fallback for an installation whose logo predates the
  // derived sizes; it looks the same, it is only bigger.
  // Both places are given the banner-sized copy now that both draw it at the
  // same size. The header used to take the 440x80 one, which is the right file
  // for a 40px-tall slot and a blurred one for a 96px slot - the pixels are not
  // there to enlarge. Nothing extra is fetched: this answer already carried both
  // copies, because the sign-in screen behind the session needs one of them.
  const sized = {
    '#brand-logo': branding.logoBanner || branding.logo,
    '#login-logo': branding.logoBanner || branding.logo,
  };

  for (const holder of ['#brand-logo', '#login-logo']) {
    const img = $(holder);
    if (!img) continue;

    const mark = sized[holder];

    // Emptied as well as hidden: a hidden element holding a few hundred
    // kilobytes of data URI is still holding them.
    img.src = mark || '';
    img.hidden = !mark;
    img.alt = mark ? (branding.title || '') : '';
  }

  showBrandMark(branding, Boolean(sized['#brand-logo']));

  // The announcement banner is separate from the "change your password" one.
  // Through the renderer, so a {year} in a copyright line stays right and a link
  // in a footer is a link. Drawn rather than assigned, because none of this is
  // HTML - see renderConfiguredText for why it must not be.
  renderConfiguredText($('#instance-banner'), brandingIn(branding, 'banner'));
  renderConfiguredText($('#footer-text'), brandingIn(branding, 'footerText'));
  renderConfiguredText($('#footer-legal'), brandingIn(branding, 'legalNotice'));

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

}

/**
 * Shows what this process is connected to, for a screen that has nothing stored.
 *
 * The connection can come from three places, and the screen only ever knew one
 * of them: the file the installer or this form writes. An installation
 * configured through the environment therefore saw an empty form under a line
 * saying "connected via postgres" - which reads as "not configured" and is not.
 *
 * The reason that matters beyond looking wrong: the file wins over the
 * environment. Filling in this form on such an installation overrides the
 * deployment's own settings at the next start, and nothing said so.
 */
function showRunningConnection(form, ds) {
  const note = $('#datasource-source');
  const running = ds.running ?? {};

  if (ds.stored) {
    if (note) note.hidden = true;

    for (const field of ['name', 'host', 'port', 'user']) {
      form.elements[field].placeholder = '';
    }

    return;
  }

  for (const field of ['name', 'host', 'port', 'user']) {
    form.elements[field].placeholder = running[field] ?? '';
  }

  // The type is chosen, and this was the one field left blank on purpose.
  //
  // The reasoning was that a select cannot hold a placeholder, so filling it
  // would present a value this form has not stored as though it had. What that
  // actually produced was worse than the thing it avoided: a dropdown showing
  // nothing at all, above fields whose placeholders describe a connection of a
  // type the card would not name - and, because nothing chosen was read as a
  // server, a port box filled in with 3306 beside them. A real value in one box
  // and placeholders in the rest.
  //
  // And it did not survive a reload. There is no empty option to go back to, so
  // a browser restoring this form landed on the first one, and the card came up
  // claiming SQLite on an installation running PostgreSQL.
  //
  // So it names what is running, like every other field here. The note above
  // says where that came from, which is what stops it reading as something
  // somebody saved.
  // Unless somebody has chosen one themselves.
  //
  // Asked of the control rather than of the form. "Somebody is filling this in"
  // is the right question for the text fields - they hold what was typed and
  // must not be taken away - and the wrong one for this select, because the
  // select decides which text fields exist at all. A form counted as being
  // edited for any reason, including a draft restored from an earlier visit,
  // left the type on whatever it happened to hold, and the card then described
  // a SQLite file directly under a line reading "currently connected via
  // postgres". Two answers to one question, on the one card whose job is to
  // give it.
  //
  // A person choosing a type is a different event from a script setting one:
  // isTrusted is false for anything dispatched from code, which is what restoring
  // a draft does. So this follows what is running until somebody actually picks
  // something, and then it is theirs.
  if (running.dialect && form.elements.dialect.dataset.chosen === undefined) {
    form.elements.dialect.value = running.dialect;
  }

  if (note) {
    note.textContent = t('admin.connectionFromEnvironment',
      'This connection comes from the environment, not from a saved setting. '
      + 'The fields below show it as placeholders. Saving this form stores a '
      + 'connection that takes precedence over the environment at the next start.');
    note.hidden = false;
  }
}

/**
 * Whether the chosen database is one that lives on a server rather than a file.
 *
 * Nothing chosen is not a server. It read as one - anything that was not sqlite
 * counted - so a card with no type selected offered the host, the user and the
 * password of a server nobody had asked for, and filled the port in with 3306
 * because that is what a server that is not PostgreSQL uses. A real value in one
 * box, placeholders in the others and an empty dropdown above all of it, on a
 * screen whose whole job is to say what this installation is connected to.
 */
function datasourceIsServer() {
  const chosen = $('#form-datasource').elements.dialect.value;

  return chosen !== '' && chosen !== 'sqlite';
}

/**
 * Shows the database fields that apply to the chosen type, and hides the rest.
 *
 * The same four behaviours the installation assistant has had all along, which
 * this screen never got: SQLite offered a host, a port, a user, a password and an
 * SSL mode for a file on disk; MySQL offered PostgreSQL's SSL mode; the caption
 * above the name said "database" for a thing that is a file path; and the port was
 * never filled in, so it had to be looked up.
 *
 * Kept separate from the installer's copy rather than shared: that page ships as
 * one self-contained file precisely because it runs before these assets are
 * loaded, so there is nothing to share it with.
 */
function syncDatasourceFields() {
  const form = $('#form-datasource');
  const server = datasourceIsServer();

  $('#ds-server-fields').hidden = !server;
  $('#ds-ssl-label').hidden = form.elements.dialect.value !== 'postgres';

  // The key travels with the words. This label says two different things
  // depending on the type, and only the text was being changed - so the element
  // went on declaring itself as one of them while showing the other, and a
  // language chosen afterwards put the declared one back.
  const nameLabel = $('#ds-name-label');

  if (server) {
    nameLabel.dataset.i18n = 'admin.dbName';
    setLeadingText(nameLabel, t('admin.dbName', 'Database / file name'));
  } else {
    nameLabel.dataset.i18n = 'admin.dbFile';
    setLeadingText(nameLabel,
      t('admin.dbFile', 'Database file - created if it does not exist'));
  }

  if (server && !form.elements.port.value) {
    form.elements.port.value = form.elements.dialect.value === 'postgres' ? '5432' : '3306';
  }
}

async function loadAdmin() {
  // The tab itself is a data-perm, applied as soon as who is signed in is known -
  // which is not here. This only decides whether there is anything to load, and it
  // has to agree with the tab, or an account would see a screen full of failures.
  if (!can('settings:manage')) return;

  await loadMaintenance();

  const branding = await api('/branding');
  const form = $('#form-branding');

  // Not over an appearance somebody is part way through writing. This runs after
  // every save on this screen and after a language is chosen, and the draft
  // below is rebuilt from the server - so a reload used to take back not only
  // the boxes on screen but the translations typed into the other languages.
  //
  // Skipped as one block. Filling half of it would leave the boxes showing one
  // thing and the draft holding another, which is worse than either.
  if (!beingEdited(form)) {
    // The two that are the same in every language. A logo, an address and a link
    // do not translate - translating a link would be translating where it goes.
    for (const field of ['companyName', 'companyUrl']) {
      form.elements[field].value = branding[field] ?? '';
    }

  // And the ones that do, kept per language while this screen is open so
  // switching between them does not lose what has been typed.
    brandingDraft = {};

    for (const language of BRANDING_LANGUAGES) {
      const written = branding.translations?.[language] ?? {};

      brandingDraft[language] = {
        title: written.title ?? '',
        tabTitle: written.tabTitle ?? '',
        banner: written.banner ?? '',
        footerText: written.footerText ?? '',
        legalNotice: written.legalNotice ?? '',
      };
    }

  // What the installation answered before any of this existed stays the base,
  // and is what the form shows for the language it was presumably written in.
    brandingBase = {
      title: branding.title ?? '',
      tabTitle: branding.tabTitle ?? '',
      banner: branding.banner ?? '',
      footerText: branding.footerText ?? '',
      legalNotice: branding.legalNotice ?? '',
    };

    showBrandingLanguage($('#branding-language')?.value || activeLanguage());

    // What was chosen last time, so the previews show it and the chooser opens
    // on it rather than starting again.
    logoCrops = branding.crops ?? {};

    setLogoPreview(branding.logo ?? '');
  }

  await loadOperational();
  await loadUpdate();

  const timezone = await api('/settings/timezone');
  const instanceSelect = $('#instance-timezone');

  // Not over a zone somebody has picked and not yet saved.
  if (!beingEdited($('#form-timezone'))) {
    fillTimezoneSelect(instanceSelect, timezone.timezone ?? 'UTC');
  }

  // The clock beside it either way: it follows whatever the picker shows, and
  // what time it is somewhere does not depend on anybody having saved.
  showTimeIn(instanceSelect, $('#instance-timezone-now'), 'UTC');

  const ds = await api('/settings/datasource');
  const dsForm = $('#form-datasource');

  // This card has two halves, and only one of them belongs to whoever is at the
  // keyboard.
  //
  // The values are theirs. This is the card the loss was reported from:
  // choosing a language reloads every screen, so switching to German to read a
  // label put the stored connection back over the one being typed - and an
  // empty stored connection put back nothing at all, which looked like a form
  // nobody had filled in.
  //
  // What this installation is connected to is not theirs, and it was being
  // skipped along with them. One touch of this form and the card went on saying
  // "connected via postgres" above five empty boxes for as long as the tab
  // stayed open - no note saying where the connection came from, and no
  // placeholders saying what it was. A restored draft did it without anybody
  // touching anything, because restoring one marks the form as being filled in.
  if (!beingEdited(dsForm)) {
    for (const field of ['dialect', 'name', 'host', 'port', 'user', 'sslMode']) {
      dsForm.elements[field].value = ds[field] ?? '';
    }
  }

  // Nothing stored, which is every installation configured through the
  // environment - a compose deployment, or a container run with DB_* set. The
  // form is filled from the file the installer or this screen writes, and there
  // is none, so every field was blank on a screen whose first line said it was
  // connected.
  //
  // Shown as placeholders rather than values, because they are not this form's
  // to save: typing over a placeholder is how somebody changes the connection,
  // and leaving it alone has to keep meaning "leave it alone".
  showRunningConnection(dsForm, ds);

  // After the values are in, or the port would be prefilled over a stored one.
  syncDatasourceFields();

  // What this process is connected to, whoever is typing what: it describes the
  // running application rather than the form.
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
  // The options first, and outside the guard below.
  //
  // What goes into a picker is the installation's roles, which is server data
  // rather than anything somebody typed - so protecting a half-filled card from
  // being overwritten must not take the choices with it. It did, and a card that
  // had been touched came back offering nothing to choose. fillSelect keeps a
  // selection that is still among the options, so a role chosen and not yet
  // saved survives this.
  fillSelect(ldapForm.elements.defaultRole, roleChoices(),
    { labelKey: 'label', valueKey: 'name' });

  // Not over a directory somebody is part way through configuring. A bind DN
  // and a filter are long enough to be worth not losing to a reload nobody
  // asked for.
  if (!beingEdited(ldapForm)) {
    for (const field of ldapFields) {
      ldapForm.elements[field].value = ldap[field] ?? '';
    }

    ldapForm.elements.port.value = ldap.port || 389;

    for (const flag of ['enabled', 'startTls', 'useTls', 'skipVerify']) {
      ldapForm.elements[flag].checked = Boolean(ldap[flag]);
    }

    // || rather than ??, because the value that actually arrives from an older
    // installation is an empty string rather than a missing field - and ?? lets
    // it through, which sets a <select> to a value none of its options carry and
    // leaves it showing nothing at all.
    ldapForm.elements.defaultRole.value = ldap.defaultRole || ORDINARY_ROLE;
  }

  // Whatever happened above, something is selected. A <select> set to a value
  // none of its options carry selects nothing and draws an empty box, which is
  // what this card kept showing; there is always at least one role to fall back
  // to, and none of the options is an empty one.
  //
  // The ordinary user role before the first option in the list, because the two
  // are not the same answer: the list is in whatever order the roles came back
  // in, and the first of them can as easily be the administrator. A directory
  // nobody has configured yet should provision people who record time, not
  // people who can reconfigure the installation - so if the stored role is
  // missing or unknown, this lands on the safe one rather than the nearest one.
  const picker = ldapForm.elements.defaultRole;

  if (!picker.value) picker.value = ORDINARY_ROLE;

  if (!picker.value) picker.selectedIndex = 0;

  // The directory run belongs to the built-in administrator, because it deletes
  // the accounts the directory no longer holds along with everything they
  // recorded. Somebody holding settings:manage saw the card and both buttons, and
  // was refused by the server on pressing either - and could set the schedule that
  // performs the same deletion unattended, which the server now refuses too.
  //
  // data-perm cannot express this: it names a permission, and this is about which
  // account it is.
  $('#sync-card').hidden = !administersOnly();

  const schedule = $('#form-sync-schedule');
  if (schedule) {
    // Filled whether the card is on screen or not: the schedule travels with the
    // rest of the directory settings, so an empty field here would clear it the
    // next time somebody saved the connection.
    //
    // Not over one being typed, for the same reason as every other card here.
    if (!beingEdited(schedule)) {
      schedule.elements.syncSchedule.value = ldap.syncSchedule ?? '';
    }

    // What this process is actually scheduled to do, which is not the stored
    // value until the next start - and the restart card is what says so.
    $('#sync-schedule-active').textContent = ldap.syncSchedule
      ? `${t('sync.scheduleStored', 'Saved')}: ${ldap.syncSchedule}`
      : t('sync.scheduleManual', 'Runs only when the button below is pressed.');
  }
}

/** The logo travels as a data URI, so it needs no upload endpoint. */
let pendingLogo = '';

/**
 * Shows the chosen image at both the sizes it will actually be given.
 *
 * One upload, two uses: 28px beside the navigation, and up to 96px across the
 * sign-in card. A mark that reads at one of those can be unreadable at the other,
 * and the sign-in screen is the one an administrator does not see again for as
 * long as they stay signed in - so it is shown here, before saving, rather than
 * found out the next time somebody signs out.
 */
function setLogoPreview(dataURI) {
  pendingLogo = dataURI;

  // Each preview shows the part chosen for that place, so what is on this screen
  // is what will be on the others. Drawn here rather than by the server, because
  // nothing has been saved yet - the logo may not even have left the machine.
  for (const [id, use] of [
    ['#logo-preview', 'header'],
    ['#logo-preview-login', 'banner'],
    ['#logo-preview-icon', 'icon'],
  ]) {
    const preview = $(id);
    if (!preview) continue;

    preview.src = dataURI ? cropPreview(dataURI, logoCrops[use], preview) : '';
  }

  const uses = $('#logo-preview-uses');
  if (uses) uses.hidden = !dataURI;

  const hint = $('#logo-crop-hint');
  if (hint) hint.hidden = !dataURI;
}

/**
 * The chosen part of a logo, for a preview.
 *
 * The whole image where nothing has been chosen, which is the usual case and
 * costs nothing. Where something has, the part is cut out with a canvas - the one
 * piece of image work that happens in the browser, because the alternative is a
 * round trip per preview for a picture that has not been saved yet.
 */
function cropPreview(dataURI, crop, into) {
  // Both sides, not just the width: a selection may keep the full width of a
  // logo and half its height, and asking only about the width would answer that
  // with the whole image.
  if (!crop || (crop.w >= 1 && crop.h >= 1)) return dataURI;

  const source = new Image();

  source.onload = () => {
    const canvas = document.createElement('canvas');

    canvas.width = Math.max(1, Math.round(source.naturalWidth * crop.w));
    canvas.height = Math.max(1, Math.round(source.naturalHeight * crop.h));

    canvas.getContext('2d').drawImage(
      source,
      source.naturalWidth * crop.x, source.naturalHeight * crop.y,
      canvas.width, canvas.height,
      0, 0, canvas.width, canvas.height,
    );

    into.src = canvas.toDataURL('image/png');
  };

  source.src = dataURI;

  // Until it has loaded, the whole image: a preview that is briefly too generous
  // is better than one that is briefly empty.
  return dataURI;
}

function wireAdmin() {
  $('#logo-file').addEventListener('change', (e) => {
    const file = e.target.files?.[0];
    if (!file) return;

    // The accept list on the input is a filter in the file dialog and nothing
    // more - a file can still be dragged in, or chosen after switching the
    // dialog to "all files". So the type is checked here as well, before
    // anything is read.
    if (!['image/png', 'image/jpeg'].includes(file.type)) {
      toast(t('err.logoNotRaster',
        'The logo must be a PNG or a JPEG. Some browsers will not take an SVG as a tab icon.'),
      'error');
      e.target.value = '';

      return;
    }

    // 256 KB matches the server-side limit; checking here gives a better
    // message than a rejected request would.
    if (file.size > 256 * 1024) {
      toast(t('admin.logoTooBig', 'The logo must be smaller than 256 KB.'), 'error');
      e.target.value = '';

      return;
    }

    const reader = new FileReader();

    reader.onload = () => {
      // A different logo means the parts chosen from the old one are about a
      // picture that is gone. Keeping them would crop the new logo by
      // coordinates somebody chose against another image.
      logoCrops = {};
      setLogoPreview(String(reader.result));
    };

    reader.readAsDataURL(file);
  });

  $('#logo-clear').addEventListener('click', () => {
    setLogoPreview('');
    $('#logo-file').value = '';
  });

  $('#form-branding').addEventListener('submit', (e) => {
    e.preventDefault();
    rememberBrandingDraft();

    // The base stays what is in the form for the language it was written in, so
    // an installation that never opens the switcher goes on working exactly as
    // it did - and a reader whose language has nothing written still gets
    // something rather than a blank header.
    const base = brandingDraft[BRANDING_LANGUAGES[0]] ?? brandingDraft[brandingLanguage] ?? {};

    const body = {
      ...formData(e.target),
      ...base,
      logo: pendingLogo,
      translations: brandingDraft,
      crops: logoCrops,
    };
    // Whether the tab's own identity is changing, decided before the save so the
    // comparison is against what is currently on screen.
    //
    // The chosen parts count as much as the logo does. A different corner of an
    // unchanged logo is a different icon, and leaving the reload out for that
    // case left the tab showing the old one - which is exactly the shape of the
    // bug that made this reload necessary in the first place.
    const markChanged = (lastBranding.logo ?? '') !== (pendingLogo ?? '')
      || JSON.stringify(lastBranding.crops ?? {}) !== JSON.stringify(logoCrops ?? {});

    saveForm(e.target, async () => {
      const saved = await api('/settings/branding',
        { method: 'PUT', body: JSON.stringify(body) });

      // The copy the language chooser fills its boxes from, brought up to date
      // from what was just sent - not from the reload below.
      //
      // The reload is behind the notice, and between the two there is a window
      // somebody can click in. Clicking in it filled the boxes with the name
      // from before the save, and saving that stored the old name as a
      // translation of the new one - a wrong value written by a screen that
      // looked like it was showing the right one. What was sent is what the
      // server now holds, so there is nothing to wait for.
      brandingBase = { ...brandingBase, ...base };

      return saved;
    },
      t('admin.saved', 'Settings saved'),
      async () => {
        await loadBranding();
        await loadAdmin();

        // A changed mark means the page is reloaded, and it is the one setting
        // that needs it. Every engine takes the tab icon from the document it was
        // given; Firefox in particular ignores an icon link inserted afterwards
        // entirely, so taking a logo off and saving left the old one in the tab
        // until something else caused a load. Chrome honours the swap, which is
        // why this looked finished.
        //
        // Only for the mark, and only when it actually changed - reloading after
        // every save on this screen would throw away whatever else was being
        // edited for no reason at all.
        if (markChanged) {
          rememberPlace();
          window.location.reload();
        }
      });
  });

  $('#form-datasource').elements.dialect.addEventListener('change', (e) => {
    // A person picking a type takes it over from here; see
    // showRunningConnection, which follows the running connection until then.
    //
    // isTrusted is what tells the two apart: restoring a draft sets the value
    // and dispatches a change so the fields that depend on it are redrawn, and
    // that is the script speaking rather than somebody choosing.
    if (e.isTrusted) e.target.dataset.chosen = 'yes';

    syncDatasourceFields();
  });

  $('#form-datasource').addEventListener('submit', (e) => {
    e.preventDefault();
    const body = formData(e.target);

    // A field that is not on screen for this dialect has nothing to say about the
    // connection, and sending what is left in it would store an SSL mode against
    // MySQL, or a host against a file on disk.
    if (!datasourceIsServer()) {
      for (const field of ['host', 'port', 'user', 'password']) body[field] = '';
    }

    if (body.dialect !== 'postgres') body.sslMode = '';

    saveForm(e.target,
      () => api('/settings/datasource', { method: 'PUT', body: JSON.stringify(body) }),
      null,
      // loadAdmin ends with loadRestart, so the card follows - and announceSave
      // reads what that just worked out, rather than promising a restart on
      // every press of a form somebody only opened to look at.
      async () => { await loadAdmin(); announceSave(); });
  });

  $('#form-ldap').addEventListener('submit', (e) => {
    e.preventDefault();
    saveForm(e.target,
      () => api('/settings/ldap', { method: 'PUT', body: JSON.stringify(ldapPayload()) }),
      t('admin.saved', 'Settings saved'),
      loadAdmin);
  });

  $('#form-sync-schedule').addEventListener('submit', (e) => {
    e.preventDefault();
    saveForm(e.target,
      () => api('/settings/ldap', { method: 'PUT', body: JSON.stringify(ldapPayload()) }),
      null,
      // The restart card too: the schedule is the one directory setting that
      // waits, because a scheduler is built while the application starts - and
      // only when it actually changed, which is what announceSave asks.
      async () => { await loadAdmin(); announceSave(); });
  });

  $('#datasource-test').addEventListener('click', () => {
    runConnectionTest($('#datasource-test-result'), () => api('/settings/datasource/test',
      { method: 'POST', body: JSON.stringify(formData($('#form-datasource'))) }));
  });

  $('#ldap-test').addEventListener('click', () => {
    runConnectionTest($('#ldap-test-result'), () => api('/settings/ldap/test',
      { method: 'POST', body: JSON.stringify(ldapPayload()) }));
  });
}

/**
 * Tries a connection and reports the outcome under the card that asked for it.
 *
 * The waiting line has to be taken back by whichever way the attempt ends. It used
 * to be written before the request and overwritten only where the request came
 * back, and mutate() turns a refusal into a toast - so a database or directory that
 * would not answer left "Testing the connection …" standing underneath for as long
 * as the screen stayed open, beside an error that had already been read and
 * dismissed.
 *
 * A failure now says why in the same place a success does, because that is where
 * somebody who just pressed the button is looking.
 */
async function runConnectionTest(result, attempt) {
  result.textContent = t('admin.testing', 'Testing the connection …');
  result.className = 'muted';

  try {
    const outcome = await attempt();

    // A success is named here rather than by the server, which wrote it in
    // English and had it shown in preference to this sentence.
    //
    // A failure goes through the same renderer as any other refusal. Half of
    // them are a fixed complaint - a field left empty, a port that is not a
    // number - and those are said in the reader's language and name the fields
    // the way the labels above them do. The rest is what the driver said back,
    // which nobody can anticipate and nobody can translate: that gets a generic
    // sentence in the reader's language, with the driver's own words folded away
    // underneath for whoever is going to act on them.
    if (outcome.ok) {
      // A success is named here rather than by the server, which wrote it in
      // English and had it shown in preference to this sentence.
      result.replaceChildren(el('span', {
        text: t('admin.testOk', 'The connection works.'),
      }));
    } else {
      showRefusal(result, outcome.error ?? { message: outcome.message });
    }

    result.className = outcome.ok ? 'muted plus' : 'muted minus';
    sayAtLeastSomething(result);
  } catch (err) {
    showRefusal(result, err.refusal ?? { message: err.message });
    result.className = 'muted minus';
    sayAtLeastSomething(result);

    toast(err.message, 'error', refusalDetail(err.refusal));
  }
}

/**
 * Never nothing.
 *
 * A refusal with no words in it - a request that was aborted, an answer whose
 * message is empty - renders as an empty box, which is the one outcome that says
 * less than not having pressed the button at all. Whoever is looking at it is
 * waiting for an answer, and "no answer" is not one of the answers.
 *
 * Called on both ways out rather than only on the throw. That was the first
 * attempt at this and it covered half the paths: an answer that came back
 * perfectly well and said the attempt had failed, without saying anything about
 * why, went straight past it.
 */
function sayAtLeastSomething(result) {
  if (result.textContent.trim() !== '') return;

  result.textContent = t('err.internal', 'Something went wrong on this installation.');
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

  // Drawn through redrawable, because everything it says is translated - the
  // counts, the abort reason, the empty row - and a language change is not a
  // reason to run a directory synchronisation again.
  const show = (report) => redrawable('directorySync', () => draw(report));

  const draw = (report) => {
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

/**
 * Wraps a mutating call so every failure surfaces as a toast, not a crash.
 *
 * Two failures, and they are not the same thing - which is the reason for the
 * two blocks below rather than the one this used to have. The call can fail, and
 * then nothing happened. Or the call can succeed and the reload behind it fail,
 * and then everything happened and the screen simply has not caught up.
 *
 * Both used to come out as the same red toast, carrying whatever the *read* said,
 * immediately after the green one saying the save had worked. refreshAll is the
 * `after` most callers pass and it awaits a dozen loads with no handling of its
 * own, so any one of them rejects it.
 *
 * What that costs is a retry. This application already reasons about that hazard
 * where it decides not to abort a write mid-flight - "somebody would do it again,
 * which for an import means writing every row twice" - and telling somebody that
 * a save which worked did not invites exactly that. The two other callers of
 * refreshAll had the distinction already, and the sentence for it.
 */
async function mutate(fn, successMessage, after) {
  let result;

  try {
    // Handed to `after`, so a caller that needs what the call answered does not
    // have to make the call again or smuggle it out through a closure. Every
    // existing caller ignores it, which is what makes this safe to add.
    result = await fn();
  } catch (err) {
    // Silent while the application is restarting into a new version. Every
    // request fails for those few seconds, and each one would raise its own red
    // toast on top of a banner that already says exactly what is happening. The
    // banner is the message; these would be noise piled on it.
    if (duringARestart()) return;

    toast(err.message, 'error', refusalDetail(err.refusal));

    return;
  }

  // Before the reload, because it is the answer to what was asked and the reload
  // is not: a slow refresh would otherwise hold back the one word confirming the
  // save landed.
  if (successMessage) toast(successMessage, 'ok');

  if (!after) return;

  try {
    await after(result);
  } catch (err) {
    if (duringARestart()) return;

    // Named as what it is. The save is done and is not coming undone; what
    // failed is the screen catching up, and the way out of that is to load the
    // page again rather than to save a second time.
    toast(`${t('msg.loadFailed', 'Could not load everything')}: ${err.message}`,
      'error', refusalDetail(err.refusal));
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
function confirmDialog({ title, text, detail, confirmLabel, danger = true, field = null }) {
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

    // A question that needs an answer rather than a yes.
    //
    // Here rather than in a second dialog written beside this one: a question is
    // a card, a sentence and two buttons whatever it asks, and the one that came
    // after this had all three of those and a box. What it resolves to is the
    // answer - the string typed, or false for a cancel - so a caller reads
    // truthiness exactly as every existing one already does.
    const input = field
      ? el('input', {
        type: 'password',
        id: field.id,
        minlength: String(field.minLength ?? 0),
        autocomplete: 'off',
        spellcheck: 'false',
      })
      : null;

    if (field) {
      const controls = el('span', { class: 'row' }, input);

      if (field.generate) {
        controls.append(el('button', {
          type: 'button',
          class: 'secondary',
          id: field.generateID,
          text: field.generate,
          onclick: () => {
            input.value = invented();

            // And shown, through the reveal this field already has rather than a
            // second control beside it. A password that has to be read out or
            // written down and cannot be looked at is worse than none - and
            // clicking the eye afterwards is a step nobody should have to know
            // about.
            const eye = card.querySelector('.password-toggle');
            if (eye && input.type === 'password') eye.click();

            input.focus();
          },
        }));
      }

      card.append(el('label', { text: field.label }, controls));

      if (field.hint) card.append(el('p', { class: 'muted', text: field.hint }));
    }

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
      class: `confirm-proceed${danger ? ' danger' : ''}`,
      type: 'button',
      text: confirmLabel,
      onclick: () => close(field ? input.value : true),
    });

    card.append(el('div', { class: 'row confirm-actions' }, cancel, proceed));

    document.addEventListener('keydown', onKey);
    document.body.append(overlay);

    // The reveal button every password field on this screen gets, given to this
    // one too - by the same function, so there is one of them rather than two
    // that drift.
    if (input) wirePasswordReveal(card);

    // Cancel takes the focus, not the destructive button: a stray Enter should
    // not delete anything. Where something has to be typed, the box takes it
    // instead - there is nothing to protect anybody from until it holds a value,
    // and a dialog that opens with the cursor somewhere else is one more click.
    if (input) input.focus();
    else cancel.focus();
  });
}

/**
 * The shortest password the server will hash.
 *
 * The same number as security.MinPasswordLength, and written here because the
 * form has to refuse what the server refuses before somebody presses the button
 * rather than after.
 */
const MIN_PASSWORD_LENGTH = 8;

/**
 * A password nobody chose.
 *
 * For handing an account back to somebody who has lost theirs. A password a
 * person invents under mild pressure is a password much like the last one they
 * invented, and this one is going to be spoken down a telephone or typed into a
 * chat window before it is replaced - so it is worth it being neither guessable
 * nor a variation on a theme.
 *
 * crypto.getRandomValues rather than Math.random, which is not required to be
 * unpredictable and on some engines is trivially so.
 *
 * Rejection rather than a remainder. Taking a byte modulo the alphabet length
 * makes the first few characters likelier than the rest, which is a small bias
 * and a pointless one when discarding the overhang costs a loop.
 *
 * The alphabet leaves out the characters that are read wrongly rather than
 * typed wrongly: no O and no zero, no I, l or one. This value is transcribed by
 * a person at least once, and "did you say ell or one" is the failure it would
 * otherwise produce.
 */
function invented() {
  const alphabet = '23456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz';
  const limit = 256 - (256 % alphabet.length);
  const out = [];

  while (out.length < 16) {
    const bytes = new Uint8Array(16);

    window.crypto.getRandomValues(bytes);

    for (const byte of bytes) {
      if (byte >= limit || out.length >= 16) continue;

      out.push(alphabet[byte % alphabet.length]);
    }
  }

  // In groups, because it is going to be read aloud or copied by eye once.
  return out.join('').replace(/(.{4})(?=.)/g, '$1-');
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
async function deleteUser(user) {
  const done = t('msg.userDeleted', 'User deleted');

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
/**
 * Whether the sign-in screen has been given up for a session.
 *
 * Set the moment a password is accepted and cleared only by signing out, which
 * makes it true across the gap that whether an account is loaded yet cannot
 * cover. showLogin is the only reader; the two writers are below and in
 * doLogout.
 */
let handedToASession = false;

function showLogin(message) {
  // Never over a session this page has already been given to.
  //
  // The first load starts before there is one and fails because there is none.
  // The sign-in form is wired and usable from the first paint, though, so
  // anybody quick - or any machine slow enough for the two to overlap - signs in
  // underneath it, and that failure then arrives after theirs succeeded. The
  // result is a page that is signed in, fully loaded and on a screen, with a
  // sign-in form laid across all of it. Firefox in CI reported it four times.
  //
  // Asked of handedToASession rather than of me.user, which is what this asked
  // first and was not enough. A sign-in takes this screen down the moment the
  // password is accepted and only fills in the account afterwards, so between
  // those two there is a page that has been signed into and cannot prove it -
  // and that is exactly the window the stale failure lands in.
  if (handedToASession) return;

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
  // From here this page belongs to a session, whether or not the account has
  // arrived yet. See showLogin, which is the only reader.
  handedToASession = true;

  // A new session has not been greeted yet, whatever the last one was told.
  // Cleared here rather than on the way out, because signing in is the moment
  // the question becomes open again - and the marker outliving a sign-out would
  // tell anything watching that the next account's tour had already been
  // decided. See greetAfterSignIn.
  delete document.documentElement.dataset.greeted;

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

    // The same choice a page load makes, rather than always the first tab: signing
    // out and back in used to discard the screen somebody was working on, so the
    // only way back to it was to reload the page afterwards.
    openTheStartingView();

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

/**
 * Drops everything on screen and in hand that belonged to whoever just left.
 *
 * Two things survived a sign-out, and the second is the one that matters.
 *
 * The address bar keeps the screen somebody was on - switchView writes it there
 * so a reload comes back to the same place and a link can be sent to somebody.
 * Nothing cleared it, and the starting view prefers the address bar over the
 * screen this account was last on, so signing in as somebody else landed on the
 * previous person's screen. Signing in as the *same* account hid it, because
 * both answers agreed.
 *
 * And the tables kept their rows. Every loader begins by checking a right and
 * returning if it is absent, which is correct for loading and wrong for what was
 * already loaded: an ordinary account signing in after an administrator found
 * loadUsers returning at once and the administrator's list of accounts still in
 * the document underneath a tab that is merely hidden. The API never answered
 * them anything - the rows were already there.
 *
 * So both are cleared here rather than at the next sign-in. Sign-out is the
 * moment this stops being anybody's data, and leaving it to the next arrival
 * means it sits on the sign-in screen in the meantime.
 */
/**
 * Puts the screen back to how it looks for somebody who has never been here.
 *
 * Appearance is chosen per device, which is right while somebody is using it and
 * wrong the moment they leave: the next person at that machine got the last
 * one's dark mode, on a screen that has nothing else of theirs on it. Back to
 * following the time of day, which is what a fresh browser does.
 *
 * What is not cleared, because none of it can reach anybody else: the screen
 * each account was last on, whether it has been greeted, and which release it
 * has dismissed - all three stored against that account's own id - and the
 * instance's own name and mark, which belong to the installation rather than to
 * a person.
 */
function forgetTheLastAppearance() {
  // Through the same function the picker uses, so there is one place that knows
  // what choosing an appearance means: it clears the stored choice and applies
  // the automatic one at once, rather than at the next load.
  setThemePreference('auto');

  const picker = $('#theme-picker');
  if (picker) picker.value = 'auto';

  // And the language switcher, which is a per-account setting and was left
  // showing the account that has gone. It sits behind the sign-in screen, so
  // nobody was looking at a control that disagreed with the page - but it is a
  // control that disagreed with the page, and the next thing it does is be
  // right for somebody else.
  const language = $('#language-picker');
  if (language) language.value = activeLanguage();

}

function forgetTheLastAccount() {
  // replaceState rather than assigning: assigning pushes a history entry, and
  // Back would then return to a screen belonging to the session that ended.
  if (currentHashView()) {
    history.replaceState(null, '', window.location.pathname + window.location.search);
  }

  cache.users = [];
  cache.projects = [];
  cache.roles = [];
  cache.permissions = [];

  // Emptied rather than left to the next render, which may never come: a table
  // whose loader returns early keeps whatever it last held.
  //
  // The ones a loader fills, which are the ones named by an id. Every table went
  // before, and one of them is not data at all: the legend under Appearance that
  // lists what may be written in a banner is part of the markup, filled by
  // nobody, and once emptied it stayed empty for the rest of the session - a
  // heading and a closing sentence with the six lines they explain gone from
  // between them.
  for (const body of $$('table[id] tbody')) body.replaceChildren();

  // And the lists, which are not tables and were left standing. loadProjects
  // fills four of them - the stopwatch's, the booking form's, the report form's
  // and the entry filter's - and every loader here returns at its first line when
  // the right is absent, so an account that may book time and may not read
  // projects was shown the last person's project names. Those are private: a
  // project belongs to exactly one person, which is why the record was hidden in
  // the first place.
  //
  // Only the ones a loader filled, by the mark fillSelect leaves. The rule is the
  // same one the tables above are chosen by, and for the same reason - the legend
  // under Appearance was emptied once by a sweep that took everything, and stayed
  // empty for the rest of the session.
  for (const list of $$('select[data-filled]')) {
    list.replaceChildren();
    delete list.dataset.filled;
  }

  // The selection bars belong to those rows and would otherwise stand over an
  // empty table offering to delete three things.
  for (const bar of $$('.bulk-bar')) bar.remove();
}

/**
 * Whether a session is already being ended, so one ending is not started by the
 * failures it causes.
 */
let endingTheSession = false;

/**
 * Tells the server the session is over, and says nothing if it cannot.
 *
 * Its own function because two things end a session from this side and only one
 * of them is a person pressing a button: the other is maintenance mode turning
 * an account away, which happens inside a failed request and must not wait for
 * this one to answer before the screen changes.
 */
async function endTheSessionQuietly() {
  try {
    await api('/auth/logout', { method: 'POST' });
  } catch {
    // Even a failed call should drop the client back to the sign-in screen.
  }
}

async function doLogout() {
  await endTheSessionQuietly();

  handBackTheScreen();
}

/**
 * Puts the page back the way somebody who is not signed in should find it.
 *
 * Everything doLogout did after telling the server, which is everything that has
 * to happen when a session ends for any reason. It ends for two: somebody presses
 * the button, or the server stops accepting the cookie - a lifetime that ran out,
 * an idle timeout, an administrator ending the session, a password changed
 * elsewhere.
 *
 * One function for both, because the second used to do none of it. An expired
 * session left the whole interface standing: every screen still drawn, every
 * poller still asking, the previous account's name in the corner - and a red
 * notice, once per click, saying to sign in again on a screen with nowhere to do
 * it.
 */
function handBackTheScreen(message) {
  // Before the state is cleared: both pollers ask with the session that is
  // about to end, and a timer left running would keep asking with none and
  // paint the screen with authentication failures. The announcement stream goes
  // the same way - the server turns an unauthenticated one away, and EventSource
  // would keep reopening it.
  stopLogPolling();
  stopPermissionPolling();
  stopAnnouncements();
  stopReleaseWatch();

  // And the notice those two could have put up. It is about what the account
  // that is leaving may do, and it must not be waiting for whoever signs in at
  // the same desk next.
  hideRightsChanged();

  // The screen goes back to being nobody's, which is what lets showLogin put it
  // up again at the end of this.
  handedToASession = false;

  // And the unfinished forms, which belong to whoever typed them.
  //
  // Both halves, because they are two places. forgetEveryDraft empties the
  // store a reload would be restored from; the boxes on screen are the other
  // one, and they keep what was typed into them until something says otherwise.
  // Nothing did - so signing out and letting somebody else sign in at the same
  // desk left them looking at a card still holding the previous person's
  // half-written database connection.
  //
  // Reset rather than emptied: a form goes back to what the markup says, which
  // is what somebody arriving at it should find.
  forgetEveryDraft();

  for (const form of $$('form')) form.reset();

  // And the typed work that belongs to no form, which the line above cannot
  // reach. The stopwatch's project and description sit beside the clock rather
  // than in a form - starting a timer is a button, not a submission - and that is
  // the whole reason the loose-draft machinery exists for them.
  //
  // So the store was emptied and the screen was not: signing out left the
  // description standing, and the next person at that desk signed in and found
  // it. Free text about what somebody was doing is exactly what must not be
  // handed on, and it is the same failure the paragraph above describes for the
  // forms - fixed there, and not here, because these came later and are
  // deliberately not forms.
  //
  // Cleared by the same declaration that decides what is kept, data-keep, so the
  // two cannot drift apart. A select with no empty option would be left showing
  // nothing at all, which reads as broken rather than as cleared, so it goes back
  // to its first entry instead.
  for (const field of $$('[data-keep]')) {
    field.value = '';

    if (field.tagName === 'SELECT' && field.selectedIndex < 0) field.selectedIndex = 0;
  }

  // And what this installation is waiting to restart into, which is the
  // administration of the installation and none of the next person's business.
  // It is a banner now rather than a card behind a permission-checked tab, so
  // nothing else would take it off the screen.
  const restart = $('#restart-banner');
  if (restart) restart.hidden = true;

  me = { user: null, permissions: [], authEnabled: true };

  forgetTheLastAccount();
  forgetTheLastAppearance();

  // And the news that belonged to whoever just left. checkForRelease takes it
  // down on its next answer, but the next answer is a request away and the
  // screen is here now - so somebody signing in behind an administrator would
  // see it for as long as that takes.
  hideReleaseBanner();

  // The screen belongs to nobody now, so it stops speaking the language of
  // whoever just left. activeLanguage() falls back to the browser once me.user
  // is gone, but nothing was re-rendering with it - so signing out of a German
  // account left an English browser looking at a German sign-in form.
  applyLanguage(activeLanguage());

  // And it stops holding their address. The fields keep their values across a
  // sign-out otherwise, which on a shared machine hands the next person the
  // last one's name.
  $('#form-login').reset();
  $('#login-totp-field').hidden = true;

  showLogin(message);
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

  // What is actually on screen, whether that came from the account or from the
  // browser.
  //
  // ?? was the wrong test: it steps aside for null and undefined and not for the
  // empty string, and an account that has stored no language has exactly the
  // empty string. So the picker was set to a value no option carries, and a
  // select given one of those shows nothing - a blank control on the topbar of
  // every account that had never chosen, which reads as a language that could
  // not be worked out rather than one that simply was not stored.
  picker.value = me.user?.language || activeLanguage();
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
 * a user has no Roles tab, and a tour that mentions one is a tour that
 * lies.
 */
const TOUR_STEPS = [
  {
    target: '#app-title',
    view: null,
    title: () => t('tour.welcome.title', 'The way back'),
    text: () => t('tour.welcome.text',
      'The title takes you to the welcome screen from wherever you are. It says what '
      + 'today already has on it, and it can start this walk again.'),
  },
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
      'Pick a date, enter the hours, done. A project is optional, so hours can be recorded '
      + 'now and sorted later. What you write is what is stored, to the minute: nothing '
      + 'here rounds to a quarter of an hour.'),
  },
  {
    target: '#table-timesheets',
    view: 'timesheets',
    permission: 'timesheets:read:own',
    title: () => t('tour.entries.title', 'Your entries'),
    text: () => t('tour.entries.text',
      'Everything you booked, filterable by project. Click a row to correct it, or tick '
      + 'several and delete them in one go. It stays yours to change: there is nobody to '
      + 'submit it to.'),
  },
  {
    target: '#workbook-card',
    view: 'timesheets',
    permission: 'timesheets:read:own',
    title: () => t('tour.sheet.title', 'In and out as a spreadsheet'),
    text: () => t('tour.sheet.text',
      'Your hours as a spreadsheet, with the column headings in the language you are '
      + 'reading. An import shows you what it would change before it changes anything. '
      + 'Every table worth moving this way has the same pair on its own screen.'),
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
    title: () => t('tour.report.title', 'Reports'),
    text: () => t('tour.report.text',
      'What you booked over a period: on one project, across all of them at once, or only '
      + 'the hours you never assigned to one. Your own hours - there is no breakdown per '
      + 'colleague, because nobody sees what anybody else recorded.'),
  },
  {
    target: '#form-user',
    view: 'users',
    permission: 'users:write',
    title: () => t('tour.users.title', 'The people who work here'),
    text: () => t('tour.users.text',
      'Add an account and give it a role. It starts on the instance default, and its owner '
      + 'changes their own working times from My account.'),
  },
  {
    target: '#table-users',
    view: 'users',
    permission: 'users:read',
    title: () => t('tour.userTable.title', 'Who is here, and what they may do'),
    text: () => t('tour.userTable.text',
      'The role beside each name can be changed from here. The built-in administrator is '
      + 'the exception: it cannot be deleted and its role cannot be moved, because it is '
      + 'the way back in.'),
  },
  {
    target: '#form-role',
    view: 'roles',
    permission: 'roles:write',
    title: () => t('tour.roles.title', 'Roles decide what is possible'),
    text: () => t('tour.roles.text',
      'A role is a set of rights, and each one is enforced by real code rather than by a '
      + 'hidden button. Tick what the role may do; every account holding it changes with it.'),
  },
  {
    target: '#form-working-times',
    view: 'settings',
    title: () => t('tour.account.title', 'My account'),
    text: () => t('tour.account.text',
      'Your daily target and your daily ceiling. The target is what the overtime balance '
      + 'is measured against, and nobody but you sets it.'),
  },
  {
    target: '#form-my-timezone',
    view: 'settings',
    title: () => t('tour.myZone.title', 'Your timezone'),
    text: () => t('tour.myZone.text',
      'It decides which calendar day a booking lands on. It is taken from your browser the '
      + 'first time you sign in, and this is where you disagree with that.'),
  },
  {
    target: '#totp-card',
    view: 'settings',
    title: () => t('tour.totp.title', 'A second factor'),
    text: () => t('tour.totp.text',
      'A code from an authenticator app, set up by scanning a QR code - or by typing the '
      + 'secret, where scanning is not an option.'),
  },
  {
    target: '#passkey-card',
    view: 'settings',
    title: () => t('tour.passkey.title', 'Signing in without a password'),
    text: () => t('tour.passkey.text',
      'A passkey stays on this device and unlocks with whatever already unlocks it. You '
      + 'can register several, one per device.'),
  },
  {
    target: '#token-card',
    view: 'settings',
    title: () => t('tour.tokens.title', 'Tokens for scripts'),
    text: () => t('tour.tokens.text',
      'A personal token lets a script book time as you, and carries exactly the rights your '
      + 'role has at the time it is used. It is shown once, when it is created.'),
  },
  {
    target: '#form-password',
    view: 'settings',
    title: () => t('tour.password.title', 'Your password'),
    text: () => t('tour.password.text',
      'Changing it ends every other session but this one. An account that signs in through '
      + 'the directory has no password here to change.'),
  },
  {
    target: '#form-branding',
    view: 'admin',
    permission: 'settings:manage',
    title: () => t('tour.branding.title', 'What this installation looks like'),
    text: () => t('tour.branding.text',
      'Title, banner, logo and footer. The logo becomes the icon in the browser tab, which '
      + 'is what tells a test instance from the real one at a glance.'),
  },
  {
    target: '#form-datasource',
    view: 'admin',
    permission: 'settings:manage',
    title: () => t('tour.database.title', 'The database'),
    text: () => t('tour.database.text',
      'Which database this installation writes to. The connection is tested before it is '
      + 'saved, and the change applies at the next start.'),
  },
  {
    target: '#form-ldap',
    view: 'admin',
    permission: 'settings:manage',
    title: () => t('tour.ldap.title', 'Signing in against a directory'),
    text: () => t('tour.ldap.text',
      'Accounts can come from LDAP instead of being created here. Below it, the '
      + 'reconciliation runs on a schedule, and can be previewed before it is run by hand.'),
  },
  {
    target: '#form-maintenance',
    view: 'admin',
    permission: 'settings:manage',
    title: () => t('tour.maintenance.title', 'Maintenance mode'),
    text: () => t('tour.maintenance.text',
      'Closes the installation with an explanation, shown on the sign-in screen as well - '
      + 'so somebody who cannot get in is told why rather than left guessing.'),
  },
  {
    target: '#form-operational',
    view: 'admin',
    permission: 'settings:manage',
    title: () => t('tour.limits.title', 'Limits and lifetimes'),
    text: () => t('tour.limits.text',
      'How long a session lasts, how many requests a caller may make, and the figures a '
      + 'new account starts on.'),
  },
  {
    target: '#form-telemetry',
    view: 'admin',
    permission: 'settings:manage',
    title: () => t('tour.telemetry.title', 'Metrics and tracing'),
    text: () => t('tour.telemetry.text',
      'The log level, the metrics endpoint and where traces are exported to. All three are '
      + 'read when the process starts, so they wait for the next one.'),
  },
  {
    target: '#log-card',
    view: 'admin',
    permission: 'settings:manage',
    title: () => t('tour.log.title', 'The log, without a shell'),
    text: () => t('tour.log.text',
      'What this process is writing, filterable by level. It is the first place to look '
      + 'when something refused and the reason was not on screen.'),
  },
  {
    target: '#theme-picker',
    view: null,
    title: () => t('tour.theme.title', 'Appearance and language'),
    text: () => t('tour.theme.text',
      'Light or dark - automatic follows the time of day. The language follows your browser '
      + 'until you choose one. That is everything; enjoy.'),
  },
];

/** Where we are in the tour, and the steps that apply to this person. */
let tour = { steps: [], index: 0, active: false };

/** Drops steps this person cannot reach, so the tour never points at nothing. */
function applicableTourSteps() {
  return TOUR_STEPS.filter((step) => {
    if (step.permission && !can(...step.permission.split(','))) return false;

    return tourTargetIsReachable($(step.target));
  });
}

/**
 * Whether a control is one this person could be shown.
 *
 * This asked for offsetParent, and offsetParent is null for everything inside a
 * screen that is not the current one - every `.view` but one is display:none. So
 * every step outside the screen the tour happened to start on measured as absent
 * and was dropped: a tour begun on the time entries came to four steps and looked
 * complete, and the calendar, the projects, the reports and My account were simply
 * never mentioned. A step switches to its own screen before it runs, so where
 * somebody is standing right now says nothing about whether the control exists.
 *
 * What does say so is a hidden that is not a screen - a card kept back because
 * there is nothing in it yet, or one a permission removed.
 */
function tourTargetIsReachable(node) {
  for (let current = node; current && current !== document.body; current = current.parentElement) {
    if (current.hidden && !current.classList.contains('view')) return false;
  }

  return Boolean(node);
}

/**
 * Positions the spotlight and the bubble around one element.
 *
 * Does nothing if the walk has ended in the meantime. This runs two animation
 * frames after the step was rendered - the delay is what lets a view that has
 * just been switched to settle before anything is measured against it - and
 * ending the tour inside that gap used to leave a bubble on screen belonging to
 * a walk that had finished, because this unhides what endTour had just hidden.
 *
 * Rare by hand and not rare at all for anything driving the application: the
 * interface says the walk has been decided as soon as it starts, so a caller
 * that reacts to that and ends it immediately lands in exactly this gap.
 */
function placeTour(node) {
  if (!tour.active) return;

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

  sealThePage(true);

  await renderTourStep();
}

/**
 * Puts the page out of reach while the tour is running, and back afterwards.
 *
 * Two mechanisms, because they stop different things.
 *
 * The blocker is a fixed element over the whole viewport that takes every
 * press. It has to exist because the spotlight cannot do this job: the
 * spotlight is one element with a 9999-pixel shadow, and it carries
 * pointer-events: none so that it does not swallow presses meant for the
 * control it is drawing a ring around. The result was that every press landed -
 * on the highlighted control, and on everything under the dimming too. A tour
 * explaining the stopwatch while somebody starts it is a tour narrating
 * something that is no longer on the screen it describes.
 *
 * inert is the other half, and the blocker cannot do it: a blocker stops the
 * mouse and nothing else, so Tab still walked into the page behind it and Enter
 * still pressed what it found. inert takes a subtree out of the tab order, out
 * of reach of assistive technology, and out of the way of clicks in one go.
 *
 * Applied to the body's children rather than to one wrapper, because there is
 * no wrapper - the bar, the views, the banners and the overlays are siblings.
 * Everything except the tour's own two elements, which have to keep working:
 * they are how somebody gets out of this.
 */
function sealThePage(sealed) {
  const spared = new Set(['tour-bubble', 'tour-spotlight', 'tour-blocker']);

  for (const child of document.body.children) {
    if (spared.has(child.id)) continue;

    // Scripts and templates have nothing to make inert, and marking them would
    // be noise in the markup for anybody reading it.
    if (child.tagName === 'SCRIPT' || child.tagName === 'TEMPLATE') continue;

    child.inert = sealed;
  }

  const blocker = $('#tour-blocker');
  if (blocker) blocker.hidden = !sealed;
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

  sealThePage(false);

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
 * What happens once, on arrival, beyond putting a screen up.
 *
 * Only the walk through is left here. The greeting itself is a screen now, so it
 * needs no arranging - somebody arriving at it sees it, and somebody who wants it
 * again presses the title.
 */
async function greetAfterSignIn() {
  await maybeWelcome();

  // Says the question has been settled, whichever way it went.
  //
  // The same idea as dataset.loaded and for the same reader: something watching
  // from outside, which cannot otherwise tell "the tour is not up yet" from
  // "the tour is not coming". That distinction used to cost nothing, because
  // the tour let the page be used underneath it. It does not any more - the
  // page is sealed while it runs - so anything driving this application has to
  // know whether to wait.
  //
  // Set even when nothing was offered: "no tour for this account" is an answer.
  document.documentElement.dataset.greeted = 'yes';
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
 * Greets somebody on their first sign-in by walking them through the place.
 *
 * The walk starts by itself now. It used to be offered by a modal with a "Not
 * now" beside it, and "Not now" recorded the tour as seen - so the one button
 * that looked like "later" meant "never", and the introduction the application
 * had was the introduction almost nobody got.
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
  if (administersOnly()) return;
  if (greetedThisVisit()) return;

  await startTour();
}

/**
 * Fills in the greeting screen for whoever is looking at it.
 *
 * Rendered on every arrival rather than once, because half of what it says is
 * about today - what is already booked, whether a stopwatch was left running -
 * and a screen somebody can come back to has to be right when they do.
 */
async function renderWelcome() {
  if (!me.user) return;

  // Somebody who has been walked through the application is not new to it, so
  // the same screen greets them differently. tourSeen is the only durable record
  // of "has been here before" there is.
  const returning = me.user.tourSeen;

  const greeting = returning
    ? {
      named: t('welcome.backName', 'Welcome back, {0}'),
      plain: t('welcome.back', 'Welcome back'),
    }
    : {
      named: t('welcome.hello', 'Welcome, {0}'),
      plain: t('welcome.helloPlain', 'Welcome'),
    };

  $('#welcome-title').textContent = me.user.name
    ? greeting.named.replace('{0}', me.user.name)
    : greeting.plain;

  $('#welcome-text').textContent = t('welcome.text',
    'This is where your working time is recorded. A short walk through it takes '
    + 'about a minute, and you can start it again at any time under My account.');

  // What today already has on it. Only for somebody who has hours of their own:
  // an account that only administers has no today to report.
  const today = $('#welcome-today');
  today.textContent = can('timesheets:read:own') ? await todayInOneSentence() : '';
  today.hidden = !today.textContent;

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
  $('#welcome-tour').hidden = applicableTourSteps().length === 0;

  // Not awaited with the rest: the greeting is readable without it, and holding
  // the screen for a list of five lines would be holding it for a courtesy.
  renderRecentEntries();
}

/** How many entries the greeting shows, and how far back it looks for them. */
const WELCOME_RECENT_COUNT = 5;
const WELCOME_RECENT_DAYS = 90;

/**
 * The last few entries, on the greeting.
 *
 * A bounded window rather than the whole history: this endpoint answers with
 * everything when it is given no dates, and a greeting has no business
 * downloading three years of somebody's working life to show five lines of it.
 * Ninety days is long enough that somebody coming back from a month away still
 * sees their own work, and the empty case says how far it looked rather than
 * "nothing", which would be a different and wrong statement.
 *
 * Only for somebody who records time. The built-in administrator has no entries
 * at all, and a panel explaining that is worse than no panel.
 */
async function renderRecentEntries() {
  const panel = $('#welcome-recent');
  if (!panel) return;

  if (!can('timesheets:read:own')) {
    panel.hidden = true;

    return;
  }

  const to = todayISO();
  const from = ISO_DAY(new Date(Date.parse(`${to}T00:00:00Z`) - WELCOME_RECENT_DAYS * 86400000));

  let entries = [];

  try {
    const params = new URLSearchParams({ from, to });

    entries = (await api(`/timesheets?${params}`))?.items ?? [];
  } catch {
    // A greeting is a courtesy. The entries screen reports anything that is
    // actually wrong with reading them.
    panel.hidden = true;

    return;
  }

  panel.hidden = false;

  // Newest first, and the newest of a day first within it: the answer is ordered
  // for a table that reads forwards, and this reads backwards.
  const newest = [...entries]
    .sort((a, b) => (a.date === b.date ? b.id - a.id : b.date.localeCompare(a.date)))
    .slice(0, WELCOME_RECENT_COUNT);

  $('#welcome-recent-list').replaceChildren(...newest.map(recentEntryRow));
  $('#welcome-recent-empty').hidden = newest.length > 0;
}

/** One entry, as a line of the greeting's list. */
function recentEntryRow(entry) {
  const project = entry.projectId
    ? projectName(entry.projectId)
    : t('ts.noProject', 'No project');

  return el('li', { class: 'welcome-recent-row' },
    el('span', { class: 'welcome-recent-when', text: fmtDate(entry.date) }),
    el('span', { class: 'welcome-recent-what' },
      el('span', { class: 'welcome-recent-project', text: project }),
      // The note where there is one, which is what tells two entries on one
      // project apart.
      ...(entry.description
        ? [el('span', { class: 'welcome-recent-note', text: entry.description })]
        : [])),
    el('span', { class: 'welcome-recent-hours', text: fmtHours(entry.durationHours) }),
  );
}

/** What today looks like, in one sentence. */
async function todayInOneSentence() {
  const today = todayISO();

  try {
    const running = await api('/me/timer');

    if (running?.running) {
      return t('welcome.timerRunning',
        'A stopwatch is still running. Stop it on the time entries screen to book the time.');
    }
  } catch {
    // Not worth a word: the greeting is a courtesy, and the screen behind it
    // reports anything that is actually wrong.
  }

  try {
    const entries = (await api(`/timesheets?from=${today}&to=${today}`))?.items ?? [];
    const hours = entries.reduce((sum, entry) => sum + entry.durationHours, 0);

    if (hours > 0) {
      // fmtHours rather than fmtNumber with an "h" in the sentence: the unit is
      // a word, and a sentence that spells it out is a second place a translator
      // has to remember it - which is how this one came to say "h" while the
      // dictionary's own unit said "Std."
      return t('welcome.todayHours', '{0} booked today so far.')
        .replace('{0}', fmtHours(hours));
    }

    return t('welcome.todayNothing', 'Nothing booked today yet.');
  } catch {
    return '';
  }
}

/**
 * The screen to carry on to from the greeting.
 *
 * Checked with viewIsOffered rather than trusted, for the reason written there.
 * Without it, "carry on where you were" led to a screen that no longer existed
 * and left the page blank - and, for a tab the account had merely lost, did
 * nothing at all, because viewTheReaderMayHave sent it straight back here.
 *
 * The first tab this reader does have is the honest answer to both: they were
 * somewhere once, that somewhere is gone, and this is where they can go now.
 */
function onwardView() {
  const last = rememberedView();

  return last !== 'welcome' && viewIsOffered(last) ? last : firstVisibleView();
}

function wireWelcome() {
  // The title, from anywhere. This is the way back to the greeting, and the
  // reason it is a screen rather than something that appears once and is gone.
  $('#app-title').addEventListener('click', () => switchView('welcome'));

  $('#welcome-continue').addEventListener('click', () => switchView(onwardView()));
  $('#welcome-tour').addEventListener('click', startTour);
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
    // One field per language, rather than one field.
    //
    // The title is the first thing an installation says about itself and the one
    // thing this step exists for, so a company that works in two languages should
    // not have to come back to the appearance screen afterwards to say it twice.
    // Two boxes is small enough to put on the wizard; the four texts on the
    // appearance screen are not, which is why that one has a switcher instead.
    fields: () => BRANDING_LANGUAGES.map((language) => el('label', {
      text: `${t('setup.instanceName', 'Name of this installation')} — ${languageName(language)}`,
    }, el('input', { type: 'text', name: `title.${language}`, maxlength: '80' }))),
    submit: async (values) => {
      const written = {};

      for (const language of BRANDING_LANGUAGES) {
        written[language] = (values[`title.${language}`] ?? '').trim();
      }

      // Nothing typed at all is a step somebody skipped, which it is allowed to
      // be.
      if (!Object.values(written).some(Boolean)) return;

      const branding = await api('/branding');

      // The base is the first language that has something, so an installation
      // that fills in one box is named in every language rather than in one.
      const base = written[BRANDING_LANGUAGES[0]]
        || Object.values(written).find(Boolean)
        || '';

      const translations = { ...branding.translations };

      for (const language of BRANDING_LANGUAGES) {
        translations[language] = {
          ...(translations[language] ?? {}),
          title: written[language],
        };
      }

      await api('/settings/branding', {
        method: 'PUT',
        body: JSON.stringify({ ...branding, title: base, translations }),
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
  if (!administersOnly()) {
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

  try {
    renderSetup();
  } catch {
    // The policy stated above the fetch, applied to the line it was missing
    // from: an installation is usable without the wizard, and this runs before
    // every other loader, so a throw here empties the whole screen over a hint.
    //
    // What throws is a step id this page has no definition for. renderSetup
    // guards that lookup in the step list - SETUP_STEPS[s.id]?.title() ?? s.id -
    // and not in the detail pane three lines below it. A server one version
    // ahead is what produces one, which normally cannot last, because
    // settleAfterRestart reloads the tab when the build changed; it lasts when
    // the new build arrived without this tab being told, which is a compose
    // pull or a container rollout.
    $('#setup-wizard').hidden = true;

    return;
  }

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
  wirePasswordReveal($('#setup-step-fields'));

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

  try {
    setup.state = await api('/setup');
  } catch (err) {
    // Said rather than swallowed. advanceSetup hangs straight off the Next
    // button, so a rejection here has nowhere to go: the wizard would sit on the
    // same step with no message, and the obvious response is to press Next
    // again. On the password step that is the worst place for it - the password
    // has already been changed by then, so the second attempt fails on the old
    // one too, and the screen still explains nothing.
    setupError(err.message);

    return;
  }

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

  try {
    await refreshAll();
  } catch (err) {
    // The same distinction mutate draws, and the same wording: setting up is
    // done and is not coming undone, so this must not read as the wizard having
    // failed. What failed is the screen catching up, and the way out is to load
    // the page again rather than to run the wizard a second time.
    toast(`${t('msg.loadFailed', 'Could not load everything')}: ${err.message}`, 'error');
  }
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
  'sessionLifetimeHours', 'sessionIdleMinutes', 'maxDailyHours', 'rateLimit',
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

  // Not over somebody who is part way through filling it in. This runs after
  // every save on the screen and after a language is chosen, and it used to
  // replace whatever had been typed with the server's copy.
  if (beingEdited(form)) return;

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
    saveForm(e.target,
      () => api('/settings/operational', {
        method: 'PUT', body: JSON.stringify(operationalPayload()),
      }),
      t('ops.saved', 'Limits saved'),
      loadOperational);
  });

  $('#operational-reset').addEventListener('click', () => {
    // Through saveForm like a save, because it is one: putting every value back
    // to the file is a decision, and what was typed before it is discarded on
    // purpose rather than kept from the reload.
    saveForm($('#form-operational'),
      () => api('/settings/operational', { method: 'PUT', body: JSON.stringify({}) }),
      t('ops.reset.done', 'All values follow the configuration file again'),
      loadOperational);
  });
}

async function loadOperational() {
  fillOperationalForm(await api('/settings/operational'));
}

// ----------------------------------------------------------------- updating

/**
 * What version is running, whether a newer one exists, and what can be done
 * about it here.
 *
 * The card says something true on every deployment, which is the whole
 * difficulty: a single binary can fetch its successor and put it in its own
 * place, and a container cannot - a binary swapped inside one is undone by the
 * next recreate, which is the moment somebody is most certain the update took.
 * So where installing would not last, the card says what to run instead rather
 * than offering a button that reverts itself.
 */
async function loadUpdate() {
  const card = $('#update-card');
  if (!card) return;

  // Everybody who can reach this screen. It used to be narrower - the account
  // that administers and records no time - and that made one card on the screen
  // appear for some of the people who got here and not others, which is a rule
  // nobody can hold in their head while using it.
  if (!can('settings:manage')) {
    card.hidden = true;

    return;
  }

  let state;

  try {
    state = await api('/settings/update');
  } catch {
    // A failed lookup is not a broken installation, and this is the one card on
    // the screen that depends on somebody else's service being up.
    card.hidden = true;

    return;
  }

  card.hidden = false;
  redrawable('update', () => renderUpdate(state));
}

function renderUpdate(state) {
  const running = t('update.running', 'This installation runs {0}')
    .replace('{0}', state.running);

  const line = $('#update-state');
  const hint = $('#update-hint');
  const button = $('#update-now');
  const notes = $('#update-notes');
  const problem = $('#update-problem');

  button.hidden = !state.available;
  notes.hidden = !state.url;

  if (state.url) notes.href = state.url;

  problem.hidden = !state.problem;

  // The server's own words for a lookup that failed - what went wrong reaching
  // somebody else's service is not a fixed set of sentences.
  if (state.problem) {
    // The sentence in the reader's language, and whatever the feed actually said
    // folded away underneath - the same shape every other failure here takes.
    // It used to be pasted on the end, so a German screen ended with a line of
    // somebody else's English: "… konnte nicht abgefragt werden: the release feed
    // answered 403".
    showRefusal(problem, {
      message: t('update.unreachable', 'Could not ask for the newest version'),
      detail: state.problem,
    });
  }

  hint.hidden = false;

  if (!state.enabled) {
    line.textContent = running;
    hint.textContent = t('update.off',
      'Checking for new versions is switched off on this installation (UPDATE_CHECK).');

    return;
  }

  // Already downloaded and waiting. Said before anything else, or somebody who
  // has taken the update is offered it again.
  if (state.pending) {
    line.textContent = t('update.pending', '{0} is downloaded and starts with the next restart.')
      .replace('{0}', state.pending);

    hint.textContent = state.restartable
      ? t('update.pendingRestart', 'Use the restart below, or restart the application yourself.')
      : t(`restart.unsupported.${state.restartCode || 'other'}`,
        'Restart the application the way it was started.');

    return;
  }

  // A build not made from a release calls itself "dev" and cannot be ranked
  // against anything. Saying "dev is the newest version" would be untrue, and
  // the sort of untrue that stops somebody looking further.
  if (!state.comparable) {
    line.textContent = running;
    hint.textContent = state.latest
      ? t('update.unversioned', 'This build was not made from a release, so there is '
        + 'nothing to compare. The newest published version is {0}.')
        .replace('{0}', state.latest)
      : t('update.unversionedAlone', 'This build was not made from a release.');

    return;
  }

  if (!state.newer) {
    line.textContent = state.latest
      ? t('update.current', '{0} is the installed version.').replace('{0}', state.running)
      : running;

    hint.hidden = true;

    return;
  }

  line.textContent = t('update.found', '{0} is available. This installation runs {1}.')
    .replace('{0}', state.latest).replace('{1}', state.running);

  const promise = state.restartable
    ? t('update.willRestart', 'The download is checked against the release’s own checksum. '
      + 'The application restarts itself afterwards.')
    : t('update.willAskRestart', 'The download is checked against the release’s own '
      + 'checksum. Afterwards the application has to be restarted by hand — this '
      + 'platform cannot restart itself.');

  // A container with an updater beside it takes the whole image, and what the
  // button does then is different enough to say plainly: it is not this
  // application replacing its own binary, it is something else replacing this
  // container.
  if (state.byImage) {
    hint.textContent = t('update.byImage',
      'A new image is pulled and this container is recreated from it, then the '
      + 'image it replaced is removed. The application is away for up to a minute '
      + 'and comes back as the new version - this page waits for it.');

    return;
  }

  // A container without one is not offered a button, and is told what to run.
  //
  // Swapping the binary works there and does not last: it changes this
  // container and not the image it was made from, so the next recreate brings
  // the old version back - and a recreate is how a container deployment applies
  // anything. An update that reverts on a day nobody connects to the button
  // they pressed is worse than no button.
  if (!state.installable) {
    hint.textContent = t('update.inContainer',
      'This runs in a container, where a replaced binary is undone the next time '
      + 'the container is recreated. Update the image by hand: '
      + 'docker compose pull && docker compose up -d - or add '
      + 'deploy/compose.update.yaml to the deployment and this card will do it.');

    return;
  }

  hint.textContent = promise;
}

/**
 * Waits out a process that is replacing itself, and says how it went.
 *
 * The restart button and the update button both end here, because both leave
 * the application starting again and both have to tell somebody whether it came
 * back. Written out twice, the two endings had already drifted: the same
 * translation key was given two different English fallbacks, one with a hyphen
 * and one with an em dash, so which sentence an untranslated screen showed
 * depended on which button had been pressed.
 *
 * The screen is refreshed before the good news rather than after it: refreshAll
 * redraws every card from the process that has just come back, and a message
 * put up first is a message competing with that.
 */
async function settleAfterRestart(previous, done, patience) {
  const overlay = $('#restart-overlay');

  if (await waitForRestart(previous, patience)) {
    // A different build came back, so everything in this tab is last version's.
    //
    // This was the one tab that did not reload. Every other open one did: the
    // announcement stream drops when the application goes away, reconnects to
    // the new one, and reloads because the last thing it heard was that a
    // restart was coming. The tab that pressed the button ran this instead,
    // which refreshed every card from the new server and left the script, the
    // stylesheet and the markup exactly as the old version had built them - so
    // an update that changed the interface showed nothing at all until somebody
    // reloaded by hand.
    //
    // And it was worse than doing nothing, because refreshAll below restarts
    // the announcement stream, and restarting it clears the record of what was
    // last announced. The reconnection that would have reloaded this tab a
    // moment later found nothing to act on. The one tab that knew an update had
    // happened was the one that forgot.
    //
    // Decided on the version rather than on which button was pressed. A restart
    // that comes back as the same build changed no assets, and throwing away
    // the scroll position, the open card and whatever is half-typed would be a
    // cost with nothing bought by it.
    //
    // The overlay is left standing on purpose: hiding it and then reloading is
    // a flash of a screen nobody gets to use.
    if (await theVersionChanged()) {
      sayAfterTheReload(done);
      window.location.reload();

      return;
    }

    overlay.hidden = true;
    await refreshAll();
    toast(done, 'ok');

    return;
  }

  // Not an error as such: it may still be coming back. Saying that is more use
  // than a spinner that never stops.
  overlay.hidden = true;
  toast(t('restart.slow',
    'The application has not answered yet. It may still be starting - please '
    + 'reload the page in a moment.'), 'error');
}

/**
 * Whether the process that came back is a different build from the one that
 * built this page.
 *
 * /branding rather than the version card: it is one small public answer that
 * every deployment gives, it is the same one this page read at start-up, and it
 * needs no permission - so this holds for whoever happened to press the button.
 *
 * A failure is not a version change. The application has only just come back and
 * one refused request is not evidence of anything; saying "no" leaves the screen
 * exactly as it was, which is the outcome that loses nothing.
 */
async function theVersionChanged() {
  if (!loadedVersion) return false;

  try {
    const branding = await api('/branding');

    return Boolean(branding?.version) && branding.version !== loadedVersion;
  } catch {
    return false;
  }
}

/** Where a message waits out a reload. */
const AFTER_RELOAD_KEY = 'gtr_after_reload';

/**
 * Keeps one sentence for the document that comes next.
 *
 * Somebody pressed a button and is owed the answer to it, and the answer arrives
 * after the page it was pressed on has been thrown away. Without this the update
 * would finish in silence: the overlay goes, a new document appears, and nothing
 * on it says that what was asked for actually happened.
 *
 * sessionStorage rather than a query parameter: it belongs to this tab, it does
 * not survive into a bookmark, and it does not put a message in the address bar
 * that a reload would show again.
 */
function sayAfterTheReload(message) {
  try {
    window.sessionStorage.setItem(AFTER_RELOAD_KEY, message);
  } catch {
    // A browser with no storage, or one refusing it. The reload is the point and
    // it still happens; only the sentence is lost.
  }
}

/**
 * Says it, once.
 *
 * Removed before it is shown rather than after, so a failure to draw it cannot
 * leave a message that reappears on every load for the rest of the session.
 */
function saySoAfterTheReload() {
  let message = null;

  try {
    message = window.sessionStorage.getItem(AFTER_RELOAD_KEY);
    window.sessionStorage.removeItem(AFTER_RELOAD_KEY);
  } catch {
    return;
  }

  if (message) toast(message, 'ok');
}

/**
 * The button that asks the release feed now.
 *
 * The card is drawn from an answer the server keeps for six hours, because the
 * feed allows sixty unauthenticated requests an hour per address and every open
 * card would otherwise spend them. That is right for a screen refreshing itself
 * and wrong for the minute after somebody publishes a release, when the card
 * states the version before it and is not going to change its mind until the
 * evening.
 *
 * The whole state comes back, so the card is redrawn from the answer rather than
 * loaded again - one request, and the same rendering path a fresh load takes.
 */
function wireUpdateCheck() {
  const button = $('#update-check');
  if (!button) return;

  button.addEventListener('click', async () => {
    const wasSaying = button.textContent;

    // Hold the width the button already has, for as long as it says something
    // else.
    //
    // "Wird gesucht ..." is not the width of "Aktualisierung suchen", so swapping
    // the label resized the button, which moved the row, which shifted the card -
    // and swapping it back a moment later moved everything home again. Pressing
    // it made the card twitch.
    //
    // Measured rather than set in the stylesheet, because the two labels are
    // different widths in every language and a number that fits both in German
    // is a gap in English. Through the CSSOM rather than a style attribute: the
    // policy forbids the attribute, and this is the same distinction the chart
    // export had to learn.
    button.style.minWidth = `${button.offsetWidth}px`;

    button.disabled = true;
    button.textContent = t('update.checking', 'Looking …');

    try {
      const state = await api('/settings/update/check', { method: 'POST' });

      redrawable('update', () => renderUpdate(state));

      // And the banner, which is the same answer put where whoever is not on
      // this screen would see it. It stays up until it stops being true: a
      // version that exists goes on existing after the card has scrolled away.
      showReleaseState(state);
    } catch (err) {
      // Including "asked a moment ago", which is a sentence rather than a
      // failure: the answer on the card is current, and saying so is better than
      // a button that appears to do nothing.
      toast(err.message, 'error');
    } finally {
      button.disabled = false;
      button.textContent = wasSaying;
      button.style.minWidth = '';
    }
  });
}

function wireUpdate() {
  const button = $('#update-now');
  if (!button) return;

  button.addEventListener('click', async () => {
    const proceed = await confirmDialog({
      title: t('update.title', 'Version'),
      text: t('update.confirm',
        'Download the new version and install it? The application restarts afterwards.'),
      confirmLabel: t('update.install', 'Download and install'),
    });

    if (!proceed) return;

    // The same overlay a restart uses, and for the same reason: this takes tens
    // of seconds - a thirty-megabyte download and a hash over it - and the thin
    // strip at the top of the page is not enough to say "do not press that
    // again". It also keeps the screen from being used while the binary
    // underneath it is being replaced.
    const overlay = $('#restart-overlay');
    const status = $('#restart-status');
    const previous = restartStartedAt;

    overlay.hidden = false;
    status.textContent = t('update.downloading',
      'Downloading and checking the new version …');

    let state;

    try {
      state = await api('/settings/update', { method: 'POST' });
    } catch (err) {
      overlay.hidden = true;
      toast(err.message, 'error');

      return;
    }

    // Something else is replacing this container, so there is nothing to ask
    // for: the wait is the whole of what is left to do.
    //
    // This used to fall through to the restart below, and the result was the
    // opposite of an update. The POST stopped the application; its restart
    // policy started the same container again from the same, old image; and the
    // updater's recreate arrived into the middle of that. The version that came
    // back was whichever won, which was usually the one that was already there -
    // reported as "it did not restart, and nothing changed".
    if (state?.byImage) {
      status.textContent = t('update.replacing',
        'A new image is being pulled and this container replaced …');

      await settleAfterRestart(previous,
        t('update.done', 'The new version is running.'), IMAGE_UPDATE_TIMEOUT_MS);

      return;
    }

    // In place. What happens next is the difference this card exists to say: a
    // platform that can replace its own process does it now, and one that
    // cannot has to be restarted by somebody.
    if (!state?.restartable) {
      overlay.hidden = true;
      await loadUpdate();
      await loadRestart();

      toast(t('update.installedRestart',
        'The new version is in place. Restart the application to use it.'), 'ok');

      return;
    }

    status.textContent = t('restart.waitingHint',
      'Waiting for the application to come back …');

    try {
      await api('/settings/restart', { method: 'POST' });
    } catch (err) {
      overlay.hidden = true;
      await loadUpdate();

      toast(`${t('restart.failed', 'The restart could not be started')}: ${err.message}`,
        'error');

      return;
    }

    // This used to read "refreshAll rather than location.reload()", on the
    // grounds that refreshing every screen from the new process leaves the
    // reader where they were and a reload would not. Half of that is true and
    // the half it missed is the one that matters: refreshAll reloads the data,
    // and the script, the stylesheet and the markup in this tab are still the
    // ones the *previous* version wrote. So an update that changed anything
    // about the interface showed none of it until somebody reloaded by hand,
    // and the version in the footer - offered as the proof it had worked - was
    // the one thing that did update, which made it look like it had.
    //
    // settleAfterRestart now decides by what came back: a different build
    // reloads, the same build refreshes.
    await settleAfterRestart(previous, t('update.done', 'The new version is running.'));
  });
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
    case 'tracerRatio': return t('tel.ratioShort', 'Share of traces recorded');
    case 'database': return t('admin.database', 'Database connection');
    case 'databasePassword': return t('restart.dbPassword', 'Database password');
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
  const banner = $('#restart-banner');
  if (!banner) return;

  // The same permission the screen that causes this needs. The banner is on
  // every screen rather than behind the tab the card sits on, so who may see it
  // is decided here rather than inherited from where it sits.
  if (!can('settings:manage')) {
    banner.hidden = true;

    const card = $('#restart-card');
    if (card) card.hidden = true;

    return;
  }

  const state = await api('/settings/restart');
  restartStartedAt = state.startedAt ?? '';

  const pending = state.pending ?? [];
  restartWaiting = pending.length > 0;

  // Only when something is actually waiting.
  //
  // It used to stay on screen wherever restarting is impossible - on Windows,
  // which has no execve - whether anything was pending or not, on the reasoning
  // that this is a standing property of the installation and worth knowing. It
  // is worth knowing at the moment it matters, which is when you have just saved
  // something that needs a restart. A notice that is always there is furniture:
  // it is read once, and after that it is the thing you look past.
  //
  // Nothing is lost by waiting. The card says what restarting does here and why
  // it cannot, whether or not anything is pending, and it is one press away
  // whether or not this notice is up.
  banner.hidden = pending.length === 0;

  const list = $('#restart-card-pending');
  list.replaceChildren(...pending.map((change) => {
    const label = el('strong', { text: pendingLabel(change.setting) });

    // One entry deliberately arrives without a before and an after: the database
    // password changed, and the server will not put the old one beside the new
    // one on a screen. "Passwort: none → none" reads as a fault rather than as
    // the discretion it is, so the name of the setting stands on its own.
    if (!change.running && !change.stored) return el('li', {}, label);

    return el('li', {}, label,
      el('span', { class: 'from', text: `: ${pendingValue(change.running)} → ` }),
      el('strong', { text: pendingValue(change.stored) }));
  }));

  // Offered only where pressing it would actually work. Where it would not, the
  // reason is shown instead of a button that fails on click.
  //
  // The hint promises a list of saved changes and the list follows it, so both go
  // when there are none.
  const waiting = pending.length > 0;

  // The hint promises a list of saved changes and the list follows it, so both
  // go when there are none. On the card, which is where they live now: the
  // banner is the notice and points at this.
  $('#restart-card-hint').hidden = !waiting;
  $('#restart-card-pending').hidden = !waiting;

  // One sentence saying how many, with the way to the card inside it.
  //
  // The banner is only up while something is waiting, so this is always the
  // count - there is no "nothing is waiting" wording, because nobody could read
  // it.
  //
  // The link is a word in the sentence rather than a control beside it: what
  // somebody wants to press is the thing being talked about, and a button
  // reading "go to the restart" next to a sentence about a restart is the same
  // word twice. Split on the placeholder, so a translator decides where in
  // their sentence it falls.
  const summary = $('#restart-summary');

  if (summary) {
    summary.textContent = '';

    // Split on the placeholder rather than on a marker put through the filler:
    // where the link falls in the sentence is the translator's business, and
    // the two halves are whatever they wrote around it.
    const [before, after] = t('restart.summary',
      '{0} saved setting(s) are waiting for a {1}.').split('{1}');

    summary.append(fillIn(before ?? '', [pending.length]));

    const link = el('button', {
      type: 'button',
      class: 'link-button',
      id: 'restart-open',
      text: t('restart.open', 'restart'),
    });

    link.addEventListener('click', openTheRestartCard);
    summary.append(link);

    summary.append(after ?? '');
  }

  // What the button does, said before it is pressed rather than found out
  // afterwards.
  const description = state.mode === 'container'
    ? t('restart.modeContainer',
      'This installation runs in a container. The button stops it, and your '
      + 'container manager starts a new one from the image - which it only does '
      + 'if it was told to restart the container. The deployment shipped with '
      + 'this application is.')
    : t('restart.modeProcess',
      'The application replaces itself, so it is never not running.');

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
  const refusal = state.supported
    ? ''
    : t(`restart.unsupported.${state.reasonCode || 'other'}`, state.reason ?? '');

  showRestartControls('#restart-card-mode', '#restart-card-now',
    '#restart-card-unsupported', description, refusal, state.supported, true);

  // The card appears for the same reason the notice does, and goes with it.
  //
  // It was always on screen for a while, so that a restart could be asked for
  // on purpose and so the screen said what one would do here. What that
  // produced was a card offering a restart beside the version card offering an
  // update - and the update button restarts by itself, so the two read as two
  // ways to do one thing, one of which is the wrong one.
  //
  // A restart on its own is not a thing anybody wants; it is what some other
  // change needs. So the card is here when something needs it and says what,
  // and the update looks after its own.
  const card = $('#restart-card');
  if (card) card.hidden = !waiting;
}

/**
 * Writes one set of restart controls: what it would do, the button, the reason
 * there is none.
 *
 * Two sets exist and they show the same answer - the banner and the card - so
 * this is called twice rather than written twice. The only thing that differs
 * is when the description is worth showing: in the banner it belongs to the
 * moment something is waiting, and in the card it is the whole point.
 */
function showRestartControls(modeSelector, buttonSelector, refusedSelector,
  description, refusal, supported, describe) {
  const mode = $(modeSelector);

  if (mode) {
    mode.textContent = description;
    mode.hidden = !supported || !describe;
  }

  const button = $(buttonSelector);
  if (button) button.hidden = !supported;

  const refused = $(refusedSelector);

  if (refused) {
    refused.textContent = refusal;
    refused.hidden = supported;
  }
}

/** The identity of the running process, to tell a restart from a hiccup. */
let restartStartedAt = '';

/** Whether anything is stored that the running process has not picked up. */
let restartWaiting = false;

/**
 * Reports a save on one of the cards the framework only reads while starting.
 *
 * Both of them used to announce "Applied on the next start" unconditionally, so
 * pressing Save on a form you had only looked at, or twice in a row, promised a
 * restart that would change nothing. The server already knows which it was: the
 * restart card is a comparison of what is running against what is stored, so an
 * empty list after the save means nothing is waiting.
 *
 * Call it after loadRestart, which is what fills that in - the save paths reload
 * the card anyway, so this costs no request of its own.
 */
function announceSave() {
  toast(restartWaiting
    ? t('admin.restartNeeded', 'Saved. Applied on the next start.')
    : t('admin.saved', 'Settings saved'), 'ok');
}

/** How long to wait for the application to come back before giving up on it. */
const RESTART_TIMEOUT_MS = 60000;

/**
 * And how long where a new image has to be fetched first.
 *
 * A restart is this process starting again, which is seconds. Replacing the
 * image is a download of some tens of megabytes over whatever connection the
 * server has, then a container recreated from it - and on a small machine at
 * the end of a domestic line that is minutes, not seconds. Giving up at one
 * would tell somebody it had failed while it was still working.
 */
const IMAGE_UPDATE_TIMEOUT_MS = 5 * 60000;

/**
 * Waits for a different process to answer.
 *
 * Polling for "does it respond" is not enough: replacing the process image takes
 * milliseconds, and a poll that misses that gap would report success without
 * anything having happened. The start time changing is what proves it.
 */
async function waitForRestart(previousStartedAt, patience = RESTART_TIMEOUT_MS) {
  const deadline = Date.now() + patience;

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


/**
 * Takes whoever pressed it to the card that performs the restart.
 *
 * The banner restarts nothing; it says one is waiting and leads to where it
 * happens. One button for one action, in the one place the detail is - which is
 * what stops the notice and the control drifting apart.
 */
function openTheRestartCard() {
  switchView('admin');

  const card = $('#restart-card');
  if (card) card.scrollIntoView({ block: 'center' });
}

/**
 * Wires the one button that restarts the application.
 *
 * One button, and that is the design rather than the current state: the banner
 * says a restart is waiting and leads to the card, it does not perform one - see
 * openTheRestartCard. This took a button as an argument because there were two,
 * and kept taking one after the second was deliberately removed, which left a
 * function whose parameter existed to describe a choice nobody has.
 */
function wireRestart() {
  const button = $('#restart-card-now');
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

    await settleAfterRestart(previous,
      t('restart.done', 'The application has restarted and the settings are in force.'));
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
      // field in some browsers and puts it at the end in others, and continuing
      // to type is the normal next act.
      const caret = input.value.length;

      input.focus();
      input.setSelectionRange(caret, caret);
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
 * How many colours the charts have for naming things apart.
 *
 * Matched by .chart-colour-0 .. -7 in the stylesheet, where the actual hues
 * live so that each theme can hold its own.
 */
const CHART_COLOURS = 8;

/**
 * The colour a project keeps, wherever it is drawn.
 *
 * From the project's own identity rather than from its position in the list.
 * Position would mean the colours shuffle whenever a project is added, finishes,
 * or simply has no hours in the period being looked at - and a colour that means
 * something different on every visit is decoration rather than information. This
 * way a project is the same colour in the bars, in the columns, in the pie, and
 * again tomorrow.
 *
 * Eight hues for any number of projects, so two of them can collide. That is
 * worth it: the label is beside every bar and in the pie's key, so the colour
 * groups the eye rather than carrying the meaning on its own.
 */
function chartColourFor(key) {
  if (key === undefined || key === null || key === '') return '';

  // The hours that belong to no project are not a project, and a colour of their
  // own would put them among them. They keep the neutral one.
  if (key === NO_PROJECT_KEY) return 'chart-colour-none';

  let hash = 0;

  for (const character of String(key)) {
    hash = (hash * 31 + character.codePointAt(0)) % 100000007;
  }

  return `chart-colour-${hash % CHART_COLOURS}`;
}

/** What the bucket for unassigned hours is called, in every chart. */
const NO_PROJECT_KEY = 'no-project';

/** One bar per project, each carrying the identity its colour comes from. */
function projectBars(projects) {
  return projects.map((project) => ({
    // A project with no name is one that has been deleted since; the hours it
    // holds still count, so it is shown rather than dropped.
    label: project.projectId
      ? (project.name || t('stats.deletedProject', 'deleted project'))
      : t('stats.noProject', 'No project'),
    value: project.hours,
    key: project.projectId || NO_PROJECT_KEY,
  }));
}

/**
 * The words a chart is announced by.
 *
 * role="img" is the right choice for a drawing - a screen reader working through
 * forty <text> nodes one after another is not a chart, it is a list of numbers in
 * drawing order - but it makes the drawing one opaque node, so the labels and
 * figures inside it stop being readable. Without a name the one element on these
 * screens that is entirely picture was announced as "image" and nothing else.
 *
 * Taken from the markup rather than written here. Every chart already has its
 * words directly above it - a heading on the statistics screen, the caption under
 * an evaluation - and they are already translated, so a sentence of its own would
 * be a second thing to translate and a second thing to keep in step with the
 * first. A container with nothing above it gets no name, which is no worse than
 * what it had.
 */
function nameTheChart(container, chart) {
  const naming = (container.closest('.chart-wrap') ?? container).previousElementSibling;
  const words = naming?.textContent.trim();

  if (words) chart.setAttribute('aria-label', words);
}

/**
 * Renders bars into a container.
 *
 * bars is [{label, value, key, title}], where key is the identity the colour is
 * taken from and title overrides the hover text. The scale runs from zero to the
 * largest value,
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

  nameTheChart(container, chart);

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
        x: labelWidth,
        y,
        width: barWidth,
        height: rowHeight,
        rx: 4,
        class: `chart-bar ${chartColourFor(bar.key)}`.trim(),
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

/**
 * The same figures as a column chart: one upright bar per entry.
 *
 * Its own function rather than a flag on drawBarChart, because turning that one
 * on its side is not a rotation - the label moves from beside the bar to under
 * it, which is the whole reason to choose one over the other. Columns compare a
 * handful of things well and run out of room for names at about a dozen.
 */
function drawColumnChart(container, bars, formatValue) {
  container.replaceChildren();

  if (bars.length === 0) return;

  const width = 640;
  const height = 260;
  const labelBand = 46;
  const valueBand = 16;
  const plot = height - labelBand - valueBand;
  const slot = width / bars.length;
  const barWidth = Math.min(56, slot * 0.6);

  const chart = svg('svg', {
    viewBox: `0 0 ${width} ${height}`,
    width: '100%',
    height,
    role: 'img',
  });

  nameTheChart(container, chart);

  const largest = Math.max(...bars.map((bar) => bar.value), 0);

  // The floor, so a period with nothing on it is still a chart rather than an
  // empty box.
  chart.append(svg('line', {
    x1: 0, y1: valueBand + plot, x2: width, y2: valueBand + plot, class: 'chart-axis',
  }));

  bars.forEach((bar, index) => {
    const centre = index * slot + slot / 2;
    const barHeight = largest > 0 && bar.value > 0
      ? Math.max(2, (bar.value / largest) * plot)
      : 0;
    const top = valueBand + plot - barHeight;

    if (barHeight > 0) {
      const rect = svg('rect', {
        x: centre - barWidth / 2,
        y: top,
        width: barWidth,
        height: barHeight,
        rx: 4,
        class: `chart-bar ${chartColourFor(bar.key)}`.trim(),
      });

      const title = svg('title');
      title.textContent = bar.title ?? `${bar.label}: ${formatValue(bar.value)}`;
      rect.append(title);
      chart.append(rect);

      // Above the column rather than inside it: inside is unreadable on a short
      // one, which is exactly the column somebody is squinting at.
      const value = svg('text', {
        x: centre, y: top - 4, class: 'chart-value', 'text-anchor': 'middle',
      });
      value.textContent = formatValue(bar.value);
      chart.append(value);
    }

    const label = svg('text', {
      x: centre,
      y: valueBand + plot + 16,
      class: 'chart-label',
      'text-anchor': 'middle',
    });
    label.textContent = bar.label;
    chart.append(label);
  });

  container.append(chart);
}

/**
 * The same figures as a pie: one slice per entry, sized by its share.
 *
 * Only for things that add up to something - hours per project add up to the
 * hours worked, so a share is a real quantity. Entries worth nothing are left
 * out rather than drawn as a slice of no width, which would put a label on the
 * rim pointing at a line.
 */
function drawPieChart(container, slices, formatValue) {
  container.replaceChildren();

  const shown = slices.filter((slice) => slice.value > 0);
  const total = shown.reduce((sum, slice) => sum + slice.value, 0);

  if (total <= 0) return;

  const size = 260;
  const radius = size / 2 - 8;
  const centre = size / 2;
  const width = 640;

  // One row of the key per slice, and the picture is whichever is taller. The pie
  // is a fixed 260 and the key runs down the right beside it, so from the
  // thirteenth project the key was simply outside the box - not clipped visibly,
  // just absent, on a chart that otherwise looked finished. The key is what tells
  // two projects sharing one of the eight hues apart, which is the reason
  // chartColourFor gives for eight being enough.
  const keyRow = 20;
  const keyTop = 14;
  const height = Math.max(size, keyTop + shown.length * keyRow + 6);

  const chart = svg('svg', {
    viewBox: `0 0 ${width} ${height}`,
    width: '100%',
    height,
    role: 'img',
  });

  nameTheChart(container, chart);

  let angle = -Math.PI / 2;

  shown.forEach((slice, index) => {
    const share = slice.value / total;
    const sweep = share * Math.PI * 2;
    const end = angle + sweep;

    // A full circle cannot be drawn as an arc - the start and end points would
    // be the same and the path collapses - so the single-slice case is a circle.
    const shape = shown.length === 1
      ? svg('circle', { cx: centre, cy: centre, r: radius })
      : svg('path', {
        d: [
          `M ${centre} ${centre}`,
          `L ${centre + radius * Math.cos(angle)} ${centre + radius * Math.sin(angle)}`,
          `A ${radius} ${radius} 0 ${sweep > Math.PI ? 1 : 0} 1`,
          `${centre + radius * Math.cos(end)} ${centre + radius * Math.sin(end)}`,
          'Z',
        ].join(' '),
      });

    // The same colour the bars gave it, so switching shape does not repaint
    // every project. Falls back to a step of the accent for a pie of something
    // that is not projects, where there is no identity to be stable about.
    shape.setAttribute('class',
      `chart-slice ${chartColourFor(slice.key) || `chart-slice-${index % 6}`}`);

    const title = svg('title');
    title.textContent = slice.title
      ?? `${slice.label}: ${formatValue(slice.value)} (${Math.round(share * 100)}%)`;
    shape.append(title);
    chart.append(shape);

    // The key, beside the pie rather than on it: a label on a thin slice either
    // overlaps its neighbour or points at nothing.
    const y = keyTop + index * keyRow;

    chart.append(svg('rect', {
      x: size + 24, y: y - 10, width: 12, height: 12, rx: 3,
      class: `chart-slice ${chartColourFor(slice.key) || `chart-slice-${index % 6}`}`,
    }));

    const label = svg('text', { x: size + 44, y, class: 'chart-label' });
    label.textContent = `${slice.label} — ${formatValue(slice.value)}`;
    chart.append(label);

    angle = end;
  });

  container.append(chart);
}

/** The default range: the current month, in whatever zone applies to the user. */
function defaultStatisticsRange() {
  const today = todayISO();
  const firstOfMonth = `${today.slice(0, 7)}-01`;

  return { from: firstOfMonth, to: today };
}

/**
 * Puts a period into a pair of empty date fields, on arrival.
 *
 * Every screen that evaluates a period has a From and a To, and leaving them
 * empty meant something different on each: the statistics card filled them in
 * when Evaluate was pressed - so the answer arrived for a period nobody had
 * seen until the fields changed under it - while the report and the overtime
 * balance sent nothing at all and quietly evaluated the whole history.
 *
 * Filled when the screen is opened instead, which is the only one of the three
 * that can be checked before acting. Refusing to evaluate until somebody types
 * a date would be the worst of them: there is an obvious right answer here, and
 * demanding it be typed is make-work.
 *
 * Only when both are empty. A period somebody set is theirs, including one they
 * cleared on purpose - and this runs on every visit to the screen.
 */
function fillDateRange(from, to) {
  if (!from || !to || from.value || to.value) return;

  const range = defaultStatisticsRange();

  setDateField(from, range.from);
  setDateField(to, range.to);
}

/**
 * The date pairs on the screens that evaluate a period.
 *
 * Two of them share the Overtime screen: the balance is asked for over a period,
 * and the hours card above it over another. They are separate questions with
 * separate answers, so they get separate fields - and both start filled in.
 */
function fillRangesFor(view) {
  const pairs = {
    overtime: [
      ['#statistics-from', '#statistics-to'],
      ['#form-overtime input[name="from"]', '#form-overtime input[name="to"]'],
    ],
    report: [
      ['#form-report input[name="from"]', '#form-report input[name="to"]'],
    ],
  }[view] ?? [];

  for (const [from, to] of pairs) {
    fillDateRange($(from), $(to));
  }

  // The single dates, which are the same idea with one field.
  //
  // A project is made today, so its start is today until somebody says
  // otherwise - the same courtesy the evaluation screens do with their period.
  // Its end is left alone: empty there means "no end planned", which is most
  // projects, and filling it would invent a deadline.
  if (view === 'projects') fillToday($('#form-project input[name="startDate"]'));
}

/** Puts today into a date field that is empty, on arrival. */
function fillToday(field) {
  if (!field || field.value) return;

  setDateField(field, todayISO());
}

async function loadStatistics() {
  const card = $('#statistics-card');
  if (!card || !can('timesheets:read:own')) return;

  const range = defaultStatisticsRange();

  if (!$('#statistics-from').value) setDateField($('#statistics-from'), range.from);
  if (!$('#statistics-to').value) setDateField($('#statistics-to'), range.to);

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
  // Through fmtDate like every other date on screen. These labels were the raw
  // ISO string the endpoint sends, so the one chart on the Overtime screen wrote
  // 2026-08-12 beside a table writing 12.08.2026 - the same day, twice, in two
  // conventions, on one screen.
  drawBarChart($('#chart-days'),
    days.map((day) => ({ label: fmtDate(day.date), value: day.hours })),
    fmtHours);

  drawBarChart($('#chart-projects'), projectBars(projects), fmtHours);

  $('#statistics-empty').hidden = (stats.totalHours ?? 0) > 0;
}

// The spreadsheet card, from here to the end of this section: exporting what is on
// screen, and importing a file.
//
// The import is deliberately two steps. A file assembled by hand is wrong more
// often than it is right, and the first thing somebody needs is to be shown what
// their file would do - which rows would be written, which would not, and why -
// before anything is.

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
 * Fetches a file and hands it to the browser to save under a readable name.
 *
 * Not api(): that one decodes JSON and would have to be taught about blobs, and
 * about not aborting a read that is a download. This is the plainer thing - ask,
 * check, save - and the two callers below differ only in how they ask.
 */
async function downloadFile(url, name, extension, request = {}) {
  const res = await fetch(url, { credentials: 'same-origin', ...request });

  // Everything api() reads off an answer that is not its body. Only the body is
  // this function's own business - it wants a blob, which is why it asks
  // directly - and the rest means what it always means.
  noticePermissionChange(res.headers.get('X-Permissions-Revision'));

  if (!res.ok) {
    let body = null;
    try {
      body = await res.json();
    } catch {
      // An error page rather than JSON; the status carries the meaning.
    }

    throw refusalFrom(res, body);
  }

  const blob = await res.blob();
  const objectURL = URL.createObjectURL(blob);

  const link = el('a', { href: objectURL, download: `${name}-${todayISO()}.${extension}` });
  document.body.append(link);
  link.click();
  link.remove();

  // Released once the browser has taken it; a blob left behind holds the whole
  // file in memory for as long as the page is open.
  URL.revokeObjectURL(objectURL);
}

/**
 * Fetches a spreadsheet export.
 *
 * The language travels with the request, so the headings in the file are the ones
 * on the screen of whoever asked for it. It is a query parameter rather than the
 * account's setting: the file is about to be opened by the person looking at this
 * screen, in the language they are reading it in.
 */
async function downloadSheet(path, name) {
  const separator = path.includes('?') ? '&' : '?';

  await downloadFile(
    `${API}${path}${separator}lang=${encodeURIComponent(activeLanguage())}`, name, 'xlsx');
}

/**
 * Sends an evaluation to be laid out, and saves the document that comes back.
 *
 * One language parameter, unlike everything else here: every word of the body is
 * already in this request, read off the screen in the language the screen is in.
 * The footer is not - the installation's name and the moment are the server's to
 * say, and it has to be told which language is reading in order to say them.
 */
async function downloadDocument(doc, name) {
  // The language, which is the one thing in a document the server words itself.
  // Everything else here was read off the screen and arrives already written;
  // the footer is the server's line, and it used to be signed off in English
  // under a German evaluation, with the date in an order no German page uses.
  await downloadFile(`${API}/exports/document?lang=${encodeURIComponent(activeLanguage())}`,
    name, 'pdf', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      'X-CSRF-Token': readCookie('gtr_csrf'),
    },
    body: JSON.stringify(doc),
  });
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

  noticePermissionChange(res.headers.get('X-Permissions-Revision'));

  let payload = null;
  try {
    payload = await res.json();
  } catch {
    // Falls through to the status below.
  }

  if (!res.ok) {
    throw refusalFrom(res, payload);
  }

  return payload?.data ?? null;
}

/**
 * Why a row of an imported file cannot be written, in the reader's language.
 *
 * The preview around it is translated - the headings, and the cells, down to a
 * status column reading "archiviert" because that is the word the file used - and
 * this column was English prose. The server writes it where the rule is enforced,
 * which is right for the log and wrong for the person who typed the row, so it
 * sends a code and the values its sentence interpolated as well, exactly as it
 * does for a refused request.
 */
function rowProblem(row) {
  if (!row.problem) return '';
  if (!row.problemCode) return row.problem;

  // A row refused for the shape of its values carries the column names, which are
  // not what the labels above the form say - the same case errorMessage handles,
  // and named from the same list so both screens use one word per field.
  if (row.problemCode === 'invalidFields') {
    const named = (row.problemValues ?? []).map((field) => t(`field.${field}`, field));

    return `${t('err.invalidFields', 'Invalid field(s)')}: ${named.join(', ')}`;
  }

  // Two prefixes, because some rows are refused by a rule the form enforces too -
  // a day over its ceiling is the same refusal whether it arrives one entry at a
  // time or eighty at once, and it already has a sentence under err.
  const sentence = t(`row.${row.problemCode}`, '')
    || t(`err.${row.problemCode}`, row.problem);

  return fillIn(sentence, row.problemValues);
}

/** Shows what the file would do, row by row. */
function renderWorkbookPreview(result) {
  const rows = (result?.rows ?? []).map((row) => el('tr', { class: row.problem ? 'rejected' : '' },
    el('td', { class: 'num', text: String(row.row) }),
    el('td', { text: row.date ? fmtDate(row.date) : '–' }),
    el('td', { text: row.user || '–' }),
    el('td', { text: row.project || t('ts.noProject', 'No project') }),
    el('td', { class: 'num', text: row.hours ? fmtNumber(row.hours) : '–' }),
    el('td', { text: row.description || '–' }),
    el('td', { class: row.problem ? 'minus' : 'muted', text: rowProblem(row) || '✓' }),
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

  // The preview it described is gone, so there is nothing to draw again.
  stopRedrawing('workbookPreview');

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

    // As resetWorkbookCard does, and says why: the preview it described is gone,
    // so there is nothing to draw again. Choosing a different file makes that
    // just as true as clearing the card does.
    stopRedrawing('workbookPreview');

    $('#wb-preview').hidden = !chosen;
    $('#wb-clear').hidden = !chosen;
    $('#wb-import').hidden = true;
    $('#wb-preview-wrap').hidden = true;
    $('#wb-summary').textContent = chosen
      ? t('wb.chosen', 'Check the file to see what it would do.')
      : '';
  });

  // Through redrawable, because the preview's columns and its refusals are
  // translated and the file it describes is not going to be sent again on a
  // language change.
  $('#wb-preview').addEventListener('click', () => mutate(
    () => sendWorkbook(true), null,
    (result) => redrawable('workbookPreview', () => renderWorkbookPreview(result))));

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

// --------------------------------------------- an evaluation as a document

/**
 * What has to be written into a copy of a chart before it can leave the page.
 *
 * The drawings take everything from the stylesheet - eight hues named by class,
 * strokes, type - and a copy lifted out of the document has no stylesheet to
 * take it from. getComputedStyle is what resolves that: it gives back the
 * settled value, with every var() and color-mix() already worked out, which is
 * exactly the colour that is on the screen rather than the recipe for it.
 */
const PAINTED = [
  'fill', 'fill-opacity', 'stroke', 'stroke-width', 'stroke-linecap',
  'opacity', 'font-family', 'font-size', 'font-weight', 'text-anchor',
  'dominant-baseline', 'color',
];

/**
 * How much bigger than the screen the picture is drawn.
 *
 * The chart on screen is a few hundred pixels wide and goes onto paper at about
 * 170 millimetres. At one to one that is roughly 40 dots per inch, which looks
 * like a screenshot of a chart rather than a chart. Twice over is enough to
 * print cleanly and keeps the file small enough to post.
 */
const CHART_SCALE = 2;

/**
 * The two shades a printed chart is fixed to, whatever the screen was set to.
 *
 * A chart goes onto white paper, and the theme somebody reads in is not a fact
 * about the page. The bars keep the colours they have - those say which series
 * is which, and are the whole point of the picture - but the words beside them
 * are written in ink and the empty part of a bar is unprinted paper.
 *
 * What made this worth fixing was the dark theme: labels are drawn in the muted
 * shade and the empty part of a bar in the page background, so an exported chart
 * arrived as a column of solid black bars with figures beside them in a grey
 * that disappeared.
 */
const PAPER_INK = '#000000';
const PAPER_GROUND = '#ffffff';

/**
 * Takes a picture of a chart, exactly as it stands.
 *
 * The whole reason the document is built from the screen rather than from the
 * database: these charts are drawn here, by hand, and re-drawing them on the
 * server would be a second implementation of them in a second language.
 *
 * The ground is painted first, and it is white, because the picture is going onto
 * paper. This used to take the card's own background on the reasoning that the
 * document should show the chart that was on the screen - which is true of the
 * shape and the colours and is not true of the theme: a dark screen produced a
 * dark rectangle with pale type on it, printed onto a white page.
 */
async function chartAsPicture(container) {
  const source = container?.querySelector('svg');
  if (!source) return '';

  const box = source.getBoundingClientRect();
  const width = Math.max(1, Math.round(box.width));
  const height = Math.max(1, Math.round(box.height));

  const copy = source.cloneNode(true);

  const originals = [source, ...source.querySelectorAll('*')];
  const copies = [copy, ...copy.querySelectorAll('*')];

  // Set one property at a time rather than writing a style attribute.
  //
  // The two look interchangeable and are not: style-src is 'self' with no
  // 'unsafe-inline', and writing the attribute is exactly what that governs, so
  // the browser blocked every one of them and said so ninety-six times per
  // exported chart. The picture still came out, because the attribute value is
  // set even when applying it is refused and the serializer reads the value -
  // which is why this went unnoticed: the export worked, and only the console
  // knew what it cost.
  //
  // Assigning through the CSSOM is not an inline style and is not covered by the
  // directive. It reflects into the attribute the serializer reads, so the copy
  // carries its colours out exactly as before.
  copies.forEach((node, i) => {
    const settled = window.getComputedStyle(originals[i]);

    PAINTED.forEach((property) => {
      const value = settled.getPropertyValue(property);
      if (value) node.style.setProperty(property, value);
    });
  });

  // And then the two things that are about paper rather than about the screen.
  //
  // After the loop above rather than instead of it: everything else the chart
  // says with colour is kept, because a bar's colour is which series it is. Only
  // the ink and the unprinted part are overruled.
  for (const words of copy.querySelectorAll('text')) {
    words.style.setProperty('fill', PAPER_INK);
  }

  for (const empty of copy.querySelectorAll('.chart-track')) {
    empty.style.setProperty('fill', PAPER_GROUND);
  }

  // Named explicitly: an <svg> handed to an <img> is a document of its own, and
  // one without a namespace or a size does not render at all.
  copy.setAttribute('xmlns', SVG_NS);
  copy.setAttribute('width', String(width));
  copy.setAttribute('height', String(height));

  const drawing = new XMLSerializer().serializeToString(copy);

  const picture = new Image();

  await new Promise((resolve, reject) => {
    picture.onload = resolve;
    picture.onerror = () => reject(new Error(t('report.chartFailed',
      'The chart could not be turned into a picture.')));

    // A data URI rather than a blob: the Content-Security-Policy allows data:
    // for images and nothing else, and this is the one place that matters.
    picture.src = `data:image/svg+xml;charset=utf-8,${encodeURIComponent(drawing)}`;
  });

  const canvas = document.createElement('canvas');
  canvas.width = width * CHART_SCALE;
  canvas.height = height * CHART_SCALE;

  const ink = canvas.getContext('2d');

  // The page, not the card. This took the card's own background, which is what
  // put a dark rectangle behind a chart exported from the dark theme.
  ink.fillStyle = PAPER_GROUND;
  ink.fillRect(0, 0, canvas.width, canvas.height);
  ink.drawImage(picture, 0, 0, canvas.width, canvas.height);

  return canvas.toDataURL('image/png');
}

/**
 * Reads a table off the screen as the figures it is showing.
 *
 * The rendered table rather than the answer it was drawn from: what goes in the
 * document is what somebody is looking at, already sorted, already formatted,
 * already in their number and date conventions. Reading the response again here
 * would mean formatting all of it a second time, and the second time would drift
 * from the first.
 *
 * Columns of buttons and of tick boxes are dropped. They are the two things on a
 * table that are not figures, and neither means anything on paper.
 */
function tableAsFigures(table) {
  if (!table) return null;

  const headings = [...table.querySelectorAll('thead th')];

  const wanted = headings
    .map((heading, i) => (heading.classList.contains('actions')
      || heading.classList.contains('pick') ? -1 : i))
    .filter((i) => i >= 0);

  if (wanted.length === 0) return null;

  const rows = [...table.querySelectorAll('tbody tr')]
    // The "nothing here" line is a message, not a row of figures.
    .filter((row) => !row.querySelector('td.empty'))
    .map((row) => wanted.map((i) => row.children[i]?.textContent.trim() ?? ''));

  return {
    columns: wanted.map((i) => headings[i].textContent.trim()),
    numeric: wanted.map((i) => headings[i].classList.contains('num')),
    rows,
  };
}

/**
 * The colours this screen is drawn in, for the document to be drawn in too.
 *
 * Read from the page rather than written out here, so a theme, a re-themed
 * build or a token somebody changes are all followed without this knowing about
 * any of them. getComputedStyle gives the settled value - the var() and the
 * color-mix() already worked out - which is the same reason the charts are
 * copied that way.
 *
 * The names are the interface's own tokens. A document made from a dark screen
 * therefore comes out with the dark theme's type colours around a dark chart,
 * which is what "the chart exactly as displayed" means once it is more than the
 * picture.
 */
function screenColours() {
  const settled = window.getComputedStyle(document.documentElement);

  const read = (token) => settled.getPropertyValue(token).trim();

  return {
    accent: read('--accent'),
    text: read('--text'),
    muted: read('--muted'),
    border: read('--border'),
    surface: read('--surface'),
  };
}

/**
 * The period a document covers, worded as the two date fields have it.
 */
function periodOf(from, to) {
  const start = from?.value ? fmtDate(from.value) : '';
  const end = to?.value ? fmtDate(to.value) : '';

  if (!start && !end) return '';

  return `${start} – ${end}`;
}

/**
 * Sends one evaluation off to be laid out, and saves what comes back.
 *
 * The whole card is disabled while it runs. Building the picture and posting it
 * takes a moment on a long table, and a second press would produce a second
 * identical document rather than a faster one.
 */
async function exportEvaluation(button, name, build) {
  if (!button) return;

  const wasSaying = button.textContent;

  button.disabled = true;
  button.textContent = t('report.exporting', 'Preparing …');

  try {
    await downloadDocument(await build(), name);
  } catch (err) {
    toast(err.message, 'error');
  } finally {
    button.disabled = false;
    button.textContent = wasSaying;
  }
}

/** The report screen: the chart as chosen, the table, and the total. */
async function reportDocument() {
  const total = $('#report-total').textContent.trim();

  return {
    title: t('report.title', 'Report'),
    colours: screenColours(),
    subtitle: periodOf($('#form-report').elements.from, $('#form-report').elements.to),
    sections: [{
      heading: t('report.result', 'Result'),
      caption: $('#report-chart-caption').textContent.trim(),
      chart: await chartAsPicture($('#report-chart')),
      table: tableAsFigures($('#table-report')),
    }],
    summary: total ? [{ label: t('stats.total', 'Total'), value: total }] : [],
  };
}

/** The statistics screen: both charts, in the order they are on it. */
async function statisticsDocument() {
  return {
    title: t('stats.title', 'My hours'),
    colours: screenColours(),
    subtitle: periodOf($('#statistics-from'), $('#statistics-to')),
    sections: [
      {
        heading: t('stats.perDay', 'Hours per day'),
        chart: await chartAsPicture($('#chart-days')),
      },
      {
        heading: t('stats.perProject', 'Hours per project'),
        chart: await chartAsPicture($('#chart-projects')),
      },
    ],
    summary: [{
      label: t('stats.total', 'Total'),
      value: $('#statistics-total').textContent.replace(/^[^:]*:\s*/, '').trim(),
    }],
  };
}

/** The overtime screen: the day-by-day table and the balance. */
async function overtimeDocument() {
  const form = $('#form-overtime');

  return {
    title: t('nav.overtime', 'Overtime'),
    colours: screenColours(),
    subtitle: periodOf(form.elements.from, form.elements.to),
    sections: [{
      heading: t('ot.balance', 'Balance'),
      caption: $('#overtime-meta').textContent.trim(),
      table: tableAsFigures($('#table-overtime')),
    }],
    summary: [{
      label: t('ot.balance', 'Balance'),
      value: $('#overtime-total').textContent.trim(),
    }],
  };
}

/**
 * Wires one button that turns a screen into a document.
 *
 * The name is a function rather than a string because it is read when the button
 * is pressed, not when it is wired: somebody who switches language after the
 * page loads should get a file named in the language they are now reading.
 */
function wireDocumentExport(selector, name, build) {
  const button = $(selector);
  if (!button) return;

  button.addEventListener('click', () => exportEvaluation(button, name(), build));
}

/**
 * Wires all three.
 *
 * The keys are written out at each call rather than gathered into a table above
 * it. A table would read better and would put every one of them beyond the
 * reach of the test that checks each key is translated - it scans this file for
 * t('...') and cannot follow a key through a variable.
 */
function wireDocumentExports() {
  wireDocumentExport('#report-pdf', () => t('report.filename', 'report'), reportDocument);
  wireDocumentExport('#statistics-pdf', () => t('stats.filename', 'statistics'),
    statisticsDocument);
  wireDocumentExport('#overtime-pdf', () => t('ot.filename', 'overtime'), overtimeDocument);
}

// ------------------------------------------- export and import, table by table

/**
 * The tables that go in and out as a spreadsheet, beside the time entries.
 *
 * Each one exports what its own tab shows and imports into its own table, which is
 * the whole point: a single pair of buttons somewhere general would have meant
 * guessing which table somebody had in mind.
 *
 * Two tables are deliberately absent, for two different reasons. A token's
 * secret exists once, at the moment it is created, and is not in the database to
 * export. A passkey is bound to the device holding it and means nothing anywhere
 * else. Neither is a table a spreadsheet can carry honestly.
 *
 * Roles were on that list, on the grounds that one cell holding
 * "projects:read,projects:write,..." is a list that has to be exactly right,
 * where a typo removes a right silently. That objection was to the format rather
 * than to the table: the sheet gives every permission a column of its own
 * holding yes or no, so a typo is a heading that does not match a right the
 * application enforces, and the import refuses the file by name. Reviewing what
 * four roles may do is a grid, and the role screen shows one role at a time.
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
  {
    key: 'roles',
    path: '/roles',
    view: '#view-roles',
    read: 'roles:read',
    write: 'roles:write',
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

    stopRedrawing(`sheetPreview:${spec.key}`);

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

    noticePermissionChange(res.headers.get('X-Permissions-Revision'));

    let payload = null;
    try {
      payload = await res.json();
    } catch {
      // Falls through to the status below.
    }

    if (!res.ok) {
      throw refusalFrom(res, payload);
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
      el('td', { class: row.problem ? 'minus' : 'muted', text: rowProblem(row) || '✓' }),
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

    // The same line reset() carries, for the same reason: the preview described
    // the file that was chosen before this one, so there is nothing left to draw
    // again. Without it the draw stayed registered and the next language change
    // put that verdict back on screen above a file nobody had checked - and a
    // clean verdict brings the Import button with it.
    stopRedrawing(`sheetPreview:${spec.key}`);

    check.hidden = !chosen;
    cancel.hidden = !chosen;
    write.hidden = true;
    wrap.hidden = true;
    summary.textContent = chosen ? t('wb.chosen', 'Check the file to see what it would do.') : '';
  });

  // Keyed per card, so the three cards this builds keep three previews rather
  // than overwriting each other's. The timesheet card in the markup was never
  // part of that: it has a key of its own.
  check.addEventListener('click', () => mutate(() => send(true), null,
    (result) => redrawable(`sheetPreview:${spec.key}`, () => preview(result))));

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
  projects: 'Exports your projects. An import matches rows '
    + 'by name: a name that already exists is updated, a new one is created. '
    + 'An empty cell leaves that '
    + 'value alone, so nothing is lost by a column somebody deleted.',
  roles: 'Exports every role with a column per permission, holding yes or no - which '
    + 'is the grid you want when the question is what four roles may do. An import '
    + 'matches rows by name, creates a role that is not there yet, and refuses a '
    + 'heading naming a right this application does not enforce. A system role can be '
    + 'described differently and nothing else.',
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

/**
 * What the evaluation is showing as a picture, and which shape it is drawn in.
 *
 * The shape is remembered for the session rather than reset on every
 * evaluation: somebody who prefers a pie prefers it for the next period too.
 */
// The chart's state, kept as the answer rather than as the drawing.
//
// It held finished labels and a finished caption, both translated at the moment
// the answer arrived - so a language change left "Hours per project" over bars
// labelled "no project" on an otherwise German screen. The words are made at
// drawing time now, which is also the only time they are needed.
const reportChart = { kind: 'bars', scope: 'projects', projects: [], days: [] };

/**
 * Fetches the breakdown for the period just evaluated and draws it.
 *
 * What it breaks down depends on what was asked. Across all projects the
 * interesting division is between them, which is also the only one where a pie
 * means anything - the parts add up to the whole. With one project named, or
 * only the hours belonging to none, there is one part, so the days are the
 * division worth seeing.
 */
async function loadReportChart(from, to, projectId) {
  const container = $('#report-chart');
  if (!container) return;

  // Scoped the way the evaluation was. The endpoint answers for every project
  // unless told otherwise, so an unscoped chart beside a table totalling one
  // project is two different numbers on one screen, both offered as the answer.
  const params = new URLSearchParams({ from, to });
  if (projectId) params.set('projectId', projectId);

  const stats = await api(`/me/statistics?${params}`);

  const everyProject = !projectId;

  // The field names are the ones the endpoint sends. This read project.project
  // and day.booked, and neither exists: every label came out as "no project",
  // and an undefined value threw inside the hours formatter, which mutate()
  // turned into an error toast over an empty chart. The chart under Overtime
  // reads the same answer correctly and is what this should have been copied
  // from rather than written from memory.
  reportChart.scope = everyProject ? 'projects' : 'days';
  reportChart.projects = stats.projects ?? [];
  reportChart.days = stats.days ?? [];

  redrawable('reportChart', drawReportChart);
}

/** Draws the breakdown in whichever shape is selected. */
function drawReportChart() {
  const container = $('#report-chart');
  if (!container) return;

  for (const button of $$('#report-chart-switch button')) {
    button.setAttribute('aria-pressed', String(button.dataset.chart === reportChart.kind));
    button.classList.toggle('secondary', button.dataset.chart !== reportChart.kind);
  }

  // The words, made now rather than when the answer arrived, so a language change
  // reaches them.
  const byProject = reportChart.scope === 'projects';

  const bars = byProject
    ? projectBars(reportChart.projects)
    : reportChart.days.map((day) => ({ label: fmtDate(day.date), value: day.hours }));

  $('#report-chart-caption').textContent = byProject
    ? t('report.byProject', 'Hours per project')
    : t('report.byDay', 'Hours per day');

  const draw = {
    bars: drawBarChart,
    columns: drawColumnChart,
    pie: drawPieChart,
  }[reportChart.kind] ?? drawBarChart;

  draw(container, bars, fmtHours);
}

function wireReportChart() {
  const holder = $('#report-chart-switch');
  if (!holder) return;

  holder.addEventListener('click', (e) => {
    const button = e.target.closest('button[data-chart]');
    if (!button) return;

    reportChart.kind = button.dataset.chart;
    drawReportChart();
  });
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

  // Not over somebody who is part way through filling it in. This runs after
  // every save on the screen and after a language is chosen, and it used to
  // replace whatever had been typed with the server's copy.
  if (beingEdited(form)) return;

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

  const active = data.active ?? {};

  // An empty field here means "whatever applies", and what applies was a thing
  // you had to work out from the line below. The placeholder says it in the box
  // that is asking, which is where somebody is looking when they wonder what
  // leaving it empty will do. The collector keeps the address the shipped
  // tracing overlay uses, because an empty one has no current value to show.
  form.elements.tracerRatio.placeholder = String(active.tracerRatio ?? '');

  if (active.tracerUrl) form.elements.tracerUrl.placeholder = active.tracerUrl;

  $('#telemetry-active').textContent = describeActiveTelemetry(active);

  // The log viewer's filters need this to know when they are asking for lines
  // the process never wrote. Both cards are on this screen, so the answer is
  // already here rather than worth a request of its own.
  logView.runningLevel = active.logLevel ?? null;
  warnAboutLevelsTheProcessDoesNotWrite();
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
    saveForm(e.target,
      () => api('/settings/telemetry', {
        method: 'PUT', body: JSON.stringify(telemetryPayload()),
      }),
      // Named afterwards rather than here: whether this needs a restart depends
      // on whether it changed anything the running process has not got.
      null,
      // The restart card too, so what was just saved appears in the list of
      // what is waiting rather than only in a toast that fades.
      async () => { await afterTelemetrySaved(); announceSave(); });
  });

  $('#telemetry-reset').addEventListener('click', () => {
    // Through saveForm for the same reason as the limits above.
    saveForm($('#form-telemetry'),
      () => api('/settings/telemetry', { method: 'PUT', body: JSON.stringify({}) }),
      t('tel.resetDone', 'Metrics and tracing follow the configuration file again'),
      afterTelemetrySaved);
  });
}

async function loadTelemetry() {
  fillTelemetryForm(await api('/settings/telemetry'));
  showTracingHint();
}

/**
 * Says where the traces are, on this installation.
 *
 * The sentence used to name http://127.0.0.1:16686, which is right on exactly
 * one machine: the server itself. Read from anywhere else - and this screen is
 * read from somewhere else, or nobody would be reading it in a browser - it
 * names the reader's own machine, where nothing is listening.
 *
 * So it names the host this page was reached on. The port is the trace
 * browser's own and does not follow the application's: they are two services,
 * and 8443 belongs to this one.
 *
 * The second line is there because the first may well not answer. The overlay
 * this repository ships binds the trace browser to the server's loopback on
 * purpose - it asks nobody to sign in, and traces carry request paths and the
 * identifiers in them - so on an unchanged deployment the way in is a tunnel,
 * and the command for it names the same host rather than leaving it as <server>.
 */
function showTracingHint() {
  const hint = $('#tel-tracing-hint');
  if (!hint) return;

  const host = window.location.hostname;

  hint.textContent = '';

  hint.append(t('tel.tracingHint',
    'Running deploy/compose.tracing.yaml beside the application? Then it is '
    + 'exporter OTLP and collector jaeger:4317, and the traces are at {0}.')
    .replace('{0}', `http://${host}:16686`));

  // Its own line: the first sentence is where to look, and this one is what to
  // do when looking there gives nothing. Run together they read as one
  // instruction that contradicts itself.
  hint.append(el('br'));

  hint.append(t('tel.tracingLoopback',
    'If that address does not answer, the shipped file publishes the trace '
    + 'browser on the server\'s loopback only - then the way in is {0}.')
    .replace('{0}', `ssh -L 16686:127.0.0.1:16686 ${host}`));
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
  //
  // isSystem here rather than administersOnly, which is the one place the two
  // deliberately differ. Everything else reserved for the built-in account was
  // reserved for what it *does*, and an account holding the admin role does the
  // same things - but this is reserved for what it *is*: the guaranteed way back
  // in, which a way back in tied to one particular phone would stop being.
  // Somebody who administers because they were given the role is not that account
  // and does not carry that guarantee, so withholding a phishing-resistant
  // sign-in from them would cost security rather than protect it.
  card.hidden = !passkeysAvailable || !me.user || me.user.isSystem;

  if (card.hidden) return;

  const passkeys = (await api('/me/passkeys'))?.items ?? [];

  const rows = passkeys.map((passkey) => el('tr', {},
    el('td', { text: passkey.name }),
    el('td', { text: fmtMoment(passkey.createdAt) }),
    el('td', {
      text: passkey.lastUsedAt ? fmtMoment(passkey.lastUsedAt) : t('passkey.never', 'never'),
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

      // The same arrival the password sign-in makes. Through openTheStartingView
      // rather than switchView, so the page does not call itself loaded while it
      // is still about to change screens - see the comment there.
      openTheStartingView();

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

  // Set when a poll was turned away, and read by the reschedule. See
  // stopLogPolling for why clearing the timer is not enough on its own.
  refused: false,
  levels: null,
  // A default that is useful rather than complete: DEBUG carries every SQL
  // statement the process runs, which buries everything else.
  quiet: new Set(['DEBUG']),

  // What the process is actually writing, read off the telemetry card. Here so
  // the filters can say when one of them is asking for lines that were never
  // captured - see warnAboutLevelsTheProcessDoesNotWrite.
  runningLevel: null,
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
  $('#log-levels').addEventListener('change', () => {
    // Before the poll, so somebody who has just ticked DEBUG on a process
    // running at INFO is told why nothing arrives rather than watching for it.
    warnAboutLevelsTheProcessDoesNotWrite();
    restart();
  });

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

  // Asked for deliberately - the screen was opened, the delay changed, the pause
  // lifted - so whatever turned it away last is over and it may try again. A
  // refusal that is still true simply happens again, once, instead of every few
  // seconds.
  logView.refused = false;

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
      // Not if that poll turned the viewer off itself. stopLogPolling clears
      // logView.timer, and this callback *is* that timer - there is nothing left
      // for it to clear, so without asking here the stop is undone by the line
      // below it, one interval later, for as long as the tab stays open.
      //
      // A 401 hid this: the sign-in screen goes up and me is emptied, so
      // logViewerActive answers false by itself. A 403 has no such side effect -
      // the session is fine and only the permission is gone, and me.permissions
      // is not refreshed while a session lasts.
      if (logView.refused || !logViewerActive()) return;

      // Chained rather than an interval: a slow or hanging request must not
      // pile up behind itself.
      schedulePoll();
    });
  }, seconds * 1000);
}

/**
 * Stops polling. Called when the screen is left or the session ends.
 *
 * `refused` says the poll stopped itself because the server turned it away, and
 * is the difference between stopping and appearing to. Clearing the timer is
 * enough when something outside the loop stops it - leaving the screen, the
 * session ending - because then no timer is running. It is not enough when the
 * poll that is running is the timer: there is nothing to clear, and the
 * reschedule after it would start the whole thing again.
 */
function stopLogPolling({ refused = false } = {}) {
  clearTimeout(logView.timer);
  logView.timer = null;

  if (refused) logView.refused = true;
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
  return can('settings:manage') && !$('#view-admin').hidden && $('#login-screen').hidden;
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
      stopLogPolling({ refused: true });
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

  warnAboutLevelsTheProcessDoesNotWrite();
}

/** The levels, quietest first, so two of them can be compared. */
const LOG_LEVELS_BY_DETAIL = ['DEBUG', 'INFO', 'NOTICE', 'WARN', 'ERROR', 'FATAL'];

/**
 * Says so when a ticked level is one the running process never writes.
 *
 * These boxes filter what was captured; they do not ask for more. The sink reads
 * the process's own output, so a level the logger is not writing was never
 * captured and can never appear - which makes ticking DEBUG on a process running
 * at INFO look like a filter that is broken rather than one that is working
 * exactly as intended and has nothing to show.
 *
 * The level itself is on the logging card, and it applies at the next start,
 * because the framework changes a logger's level by writing a field every
 * request goroutine reads without synchronisation. So this is a sentence rather
 * than a button: what somebody has to do next is two screens and a restart away,
 * and being told that beats waiting for lines that are not coming.
 */
function warnAboutLevelsTheProcessDoesNotWrite() {
  const warning = $('#log-level-warning');
  if (!warning) return;

  const running = LOG_LEVELS_BY_DETAIL.indexOf(logView.runningLevel ?? '');
  if (running < 0) {
    warning.hidden = true;

    return;
  }

  const quieter = selectedLogLevels()
    .filter((level) => LOG_LEVELS_BY_DETAIL.indexOf(level) >= 0
      && LOG_LEVELS_BY_DETAIL.indexOf(level) < running);

  warning.textContent = quieter.length
    ? t('log.levelTooQuiet',
      'This installation is writing {0} and above, so {1} will stay empty. '
      + 'Change the log level under Logging, metrics and tracing; it applies at the next start.')
      .replace('{0}', logView.runningLevel)
      .replace('{1}', quieter.join(', '))
    : '';

  warning.hidden = !warning.textContent;
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
  // clock on the wall while working out what happened when. The reader's
  // language for the rest, like every other figure on screen - it decides
  // nothing at all while hour12 is off, and leaving it to the browser was one
  // more place for the two to disagree later.
  return at.toLocaleTimeString(activeLocale(), { hour12: false });
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
    ? (state.message || t('err.maintenance',
      'This installation is temporarily unavailable for maintenance.'))
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

  // And who may still come in. The notice above is whatever the administrator
  // typed - "back at 14:00" - which tells somebody nothing about why their own
  // password was refused, so they try it again. This line is the application's
  // own and says the refusal is not about them.
  const who = $('#login-maintenance-who');
  if (who) who.hidden = !state.enabled;

  // The form, for the administrator who is looking at it - unless they are part
  // way through typing the notice, which this runs often enough to interrupt:
  // every load of the administration screen passes through here.
  //
  // The banners above are filled either way. They say what the installation is
  // doing, which is true whatever anybody is typing.
  const form = $('#form-maintenance');
  if (form && !beingEdited(form)) {
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

    saveForm(form,
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
    return new Intl.DateTimeFormat(activeLocale(), {
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
  // Not over somebody who is part way through filling it in. This runs after
  // every save on the screen and after a language is chosen, and it used to
  // replace whatever had been typed with the server's copy.
  if (beingEdited($('#form-my-timezone'))) return;

  const select = $('#my-timezone');
  const inherit = `${t('tz.inherit', 'Follow the instance setting')}`;

  // Naming the effective zone in this option made it read "follow the instance
  // setting (Africa/Abidjan)" right after Africa/Abidjan had been chosen by
  // hand - an offer to change nothing, and no way to find out what stopping
  // would actually give you. See instanceZone.
  const instance = instanceZone();

  fillTimezoneSelect(select, me.user?.timezone ?? '', `${inherit} (${instance})`);

  // The clock beside the picker follows the picker: with nothing chosen it
  // shows the instance's zone, which is what would apply.
  showTimeIn(select, $('#my-timezone-now'), instance);
}

/**
 * The zone "follow the instance setting" would actually give you.
 *
 * Not the zone in effect for this account: they are the same until somebody
 * picks one of their own, and then they are not. Written here rather than at
 * each of the two places that need it, because it is one rule about what an
 * option means and a rule in two places is a rule that can be changed in one.
 */
function instanceZone() {
  return me.user?.instanceTimezone ?? me.user?.effectiveTimezone ?? 'UTC';
}

/**
 * Wires one zone picker: the clock beside it follows it, and the form saves it.
 *
 * Both pickers on this screen do exactly this and differ in three things - the
 * controls, where the choice is saved, and which zone the clock falls back to
 * when nothing is chosen. Those are the arguments.
 *
 * `fallback` is a function rather than a value because the personal picker's
 * answer depends on the account, which arrives after this runs.
 */
function wireTimezonePicker(select, clock, form, endpoint, fallback) {
  // Picking the "follow the instance setting" line has to show the time it
  // would actually be, not the time in the zone being left.
  select.addEventListener('change', () => showTimeIn(select, clock, fallback()));

  form.addEventListener('submit', (e) => {
    e.preventDefault();
    saveForm(e.target,
      () => api(endpoint, {
        method: 'PUT', body: JSON.stringify({ timezone: select.value }),
      }),
      t('tz.saved', 'Timezone saved'),
      refreshAll);
  });
}

function wireTimezones() {
  wireTimezonePicker($('#my-timezone'), $('#my-timezone-now'), $('#form-my-timezone'),
    '/me/timezone', instanceZone);

  // UTC, because there is nothing above the instance to inherit from.
  wireTimezonePicker($('#instance-timezone'), $('#instance-timezone-now'), $('#form-timezone'),
    '/settings/timezone', () => 'UTC');
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

/**
 * Applies an appearance and remembers it for the next first paint.
 *
 * The remembering is a cache, not the setting. The setting lives on the account
 * - see setThemeForAccount - and the copy in this browser exists for one reason:
 * theme.js runs before anything has been asked of the server, and without it the
 * page paints in the wrong colours and corrects itself a moment later. It is
 * cleared on the way out, so the next person starts from the clock.
 */
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

/**
 * Chooses an appearance: on the account where there is one, on the page where
 * there is not.
 *
 * Signed out, the sign-in screen still has to be readable by whoever is looking
 * at it, so the choice holds for the page and is not written anywhere. Signed
 * in, it goes to the account - which is the whole point of moving it off the
 * device, where a shared machine handed the next person the last one's dark
 * mode.
 *
 * "auto" is sent as an empty value, because on the account the absence of a
 * choice is what following the clock means.
 */
function setThemeForAccount(preference) {
  setThemePreference(preference);

  if (!me.user) return;

  // Quietly: this is a preference, and a notice saying it was saved is a notice
  // about something the person can already see happened.
  api('/me/theme', {
    method: 'PUT',
    body: JSON.stringify({ theme: preference === 'auto' ? '' : preference }),
  }).then(() => {
    me.user.theme = preference === 'auto' ? '' : preference;
  }).catch(() => {
    // It holds for this page either way, which is what somebody just asked for.
  });
}

/** Puts the signed-in account's appearance on the screen. */
function applyAccountTheme() {
  setThemePreference(me.user?.theme || 'auto');

  const picker = $('#theme-picker');
  if (picker) picker.value = me.user?.theme || 'auto';
}

/** Wires the appearance picker and keeps "automatic" honest as the day passes. */
function wireTheme() {
  const picker = $('#theme-picker');
  const theme = window.gtrTheme;

  picker.value = theme.stored();
  picker.addEventListener('change', (e) => setThemeForAccount(e.target.value));

  setInterval(() => {
    if (document.documentElement.dataset.themePreference !== 'auto') return;

    const now = theme.resolve('auto');
    if (now !== document.documentElement.dataset.theme) {
      document.documentElement.dataset.theme = now;
    }
  }, THEME_RECHECK_MS);
}

/**
 * The navigation, folded away on a screen too narrow to hold it.
 *
 * Nine points do not fit beside anything on a telephone. They were a row that
 * scrolled sideways, which shows three and hides six with nothing to say the
 * others exist - so somebody looking for Roles had to discover it by dragging.
 * Folded into a list behind one control, the count is visible the moment it
 * opens.
 *
 * Closed by everything that means "I am done with this": choosing a point,
 * pressing Escape, and a press anywhere else. A menu that only closes by its own
 * button is a menu that ends up covering the screen somebody wanted to read.
 */
function wireNavigationMenu() {
  const bar = $('.topbar');
  const toggle = $('#nav-toggle');
  const tabs = $('#tabs');
  if (!bar || !toggle || !tabs) return;

  const show = (open) => {
    bar.classList.toggle('nav-open', open);
    toggle.setAttribute('aria-expanded', String(open));
  };

  toggle.addEventListener('click', (e) => {
    e.stopPropagation();
    show(!bar.classList.contains('nav-open'));
  });

  // Choosing one is the point of opening it.
  tabs.addEventListener('click', () => show(false));

  document.addEventListener('keydown', (e) => {
    if (e.key !== 'Escape' || !bar.classList.contains('nav-open')) return;

    show(false);

    // Back to the control that opened it, so a keyboard is not left with the
    // focus on something that is no longer there.
    toggle.focus();
  });

  document.addEventListener('click', (e) => {
    if (!bar.classList.contains('nav-open') || bar.contains(e.target)) return;

    show(false);
  });

  // A window widened past the breakpoint has the points on screen again, and a
  // menu left open would be a second copy of them.
  window.addEventListener('resize', () => {
    if (window.innerWidth > 900) show(false);
  });
}

// --------------------------------------------------------------- bootstrap

// viewTheReaderMayHave answers with the screen asked for, or the welcome screen
// when that one takes a permission the reader has not got.
//
// Most ways in were already covered, and it is worth saying which rather than
// implying they were not. The address bar and the remembered screen both go
// through startingView, which only accepts a view whose tab is visible; the tour
// drops steps whose permission the reader lacks; the rest are reached from
// inside a screen somebody already has.
//
// The release banner's button was the one that was not. It calls switchView
// directly, and it was still on screen for an account that had just signed in
// behind an administrator - so an ordinary user clicked it and got the
// administration screen. Both halves are fixed; this is the half that stops the
// next direct caller needing to remember.
//
// The data was never at risk: every request behind that screen is refused by the
// server on its own. What was at risk is the sentence this application is meant
// to be describable in - somebody who may not administer this installation
// should not be looking at the screen that administers it.
//
// The permission comes off the tab, so there is one declaration of who may have
// a screen rather than two that can disagree.
function viewTheReaderMayHave(name) {
  const tab = $(`.tab[data-view="${name}"]`);
  const needs = tab?.dataset.perm;

  if (!needs || can(...needs.split(','))) return name;

  return 'welcome';
}

function switchView(name) {
  name = viewTheReaderMayHave(name);

  $$('.tab').forEach((tab) => tab.setAttribute('aria-current', String(tab.dataset.view === name)));
  $$('.view').forEach((view) => { view.hidden = view.id !== `view-${name}`; });

  // In the address bar, so a reload comes back to the same screen and a link to
  // one can be sent to somebody. replaceState rather than assigning to
  // location.hash: assigning pushes an entry, and Back would then walk through
  // every tab that was looked at instead of leaving the page.
  if (currentHashView() !== name) {
    history.replaceState(null, '', `#${name}`);
  }

  rememberView(name);

  // The period this screen evaluates, visible before anything is evaluated.
  fillRangesFor(name);

  // Filled on arrival rather than once, because what it says about today goes
  // stale. Not awaited: switching screens is not something to hold up for two
  // requests, and the screen reads correctly while they are in flight.
  if (name === 'welcome') renderWelcome();

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

/**
 * Where somebody was on the page, kept across a reload this application caused.
 *
 * Saving a logo reloads: the tab's icon comes from the document, and no engine
 * takes a new one from a link swapped in afterwards. What that cost was the
 * place - the appearance settings are a long way down a long screen, and coming
 * back to the top of it after every save means scrolling back down to see
 * whether the thing that was just saved looks right.
 *
 * In the session rather than on the device, and cleared as soon as it is used:
 * this is about one reload that is already in flight, not about where somebody
 * was yesterday. That is what rememberedView is for.
 */
const PLACE_KEY = 'gtr.place';

/** Records the place, immediately before reloading. */
function rememberPlace() {
  try {
    sessionStorage.setItem(PLACE_KEY, JSON.stringify({
      view: currentHashView(),
      y: Math.round(window.scrollY),
    }));
  } catch {
    // Private browsing refuses storage; the reload then starts at the top,
    // which is where it always used to start.
  }
}

/**
 * Puts somebody back where they were, once there is enough page to do it.
 *
 * Not in one go: at the moment this runs the screen is a few hundred pixels of
 * empty form, and scrolling to where the LDAP section will be scrolls to the
 * bottom of nothing. The page grows as each panel answers, so this keeps asking
 * until the scroll actually lands or the patience runs out - either of which
 * leaves somebody looking at a page rather than at a spinner.
 */
function restorePlace() {
  let place = null;

  try {
    place = JSON.parse(sessionStorage.getItem(PLACE_KEY) || 'null');
    sessionStorage.removeItem(PLACE_KEY);
  } catch {
    return;
  }

  if (!place || typeof place.y !== 'number' || place.y <= 0) return;

  // Only back to the screen it was taken on. A reload that ends up somewhere
  // else - a session that expired, a link that named another view - has no
  // business being scrolled to an offset measured on a different page.
  if (place.view && place.view !== currentHashView()) return;

  const deadline = Date.now() + 3000;

  const settle = () => {
    window.scrollTo(0, place.y);

    // Arrived, near enough: the last panel to answer can be a pixel or two
    // shorter than it was, and chasing that would never finish.
    if (Math.abs(window.scrollY - place.y) <= 2) return;

    if (Date.now() < deadline) requestAnimationFrame(settle);
  };

  requestAnimationFrame(settle);
}

/** The view named in the address bar, if it names one. */
function currentHashView() {
  return decodeURIComponent(window.location.hash.replace(/^#/, ''));
}

/**
 * Where this person's last visit left off, if it was recorded.
 *
 * Per account and in localStorage rather than in the session: the point is to
 * survive signing out, closing the browser and coming back tomorrow. Keyed by
 * user id so two people sharing a machine do not inherit each other's screen.
 *
 * Storage can be refused outright in private browsing, which costs nothing here -
 * the first permitted tab is a perfectly good answer.
 */
function rememberedView() {
  if (!me.user) return '';

  try {
    return localStorage.getItem(`gtr_view_${me.user.id}`) ?? '';
  } catch {
    return '';
  }
}

/** Records the screen somebody is on, for the next time they sign in. */
function rememberView(name) {
  // Not while the initial password stands: the wizard forces My account open, and
  // recording that would make the forced screen look like a choice somebody made.
  if (!me.user || me.user.mustChangePassword) return;

  // The greeting is not somewhere somebody was working, it is the way back to the
  // start - recording it would make "carry on where you were" lead here.
  if (name === 'welcome') return;

  try {
    localStorage.setItem(`gtr_view_${me.user.id}`, name);
  } catch {
    // Nothing to do about a storage that will not take it.
  }
}

/**
 * The view to open: the one in the address bar when it is real and permitted, then
 * the one this person was last on, and otherwise the greeting.
 *
 * Checked against the tabs rather than trusted, because neither the hash nor the
 * remembered name is anything more than a string that was true once - the hash is
 * whatever was typed or bookmarked, and a remembered screen may belong to a tab
 * this account has since lost, or one that stopped existing between releases.
 *
 * The greeting has no tab of its own, so it is permitted by name: it asks for no
 * right, because it only ever says what this person already has. It is where a
 * first sign-in lands, and where anybody lands who has not been anywhere yet -
 * somebody who was working somewhere goes back to it instead, which is the point
 * of remembering.
 */
/**
 * Opens the screen this session starts on, and only then calls the page loaded.
 *
 * refreshAll marks the page loaded once every screen has been filled from the
 * server, and for a refresh that is the truth. On the way in it is one step
 * early: the first view has still to be chosen, and on a page load the reader
 * has still to be put back where they were. Both of those switch the view.
 *
 * That mark is what anything outside this page has to go on - a browser case, or
 * a person wondering whether a blank card is empty or unanswered - and it was
 * saying "ready" while the interface was still about to change screens. A tab
 * opened in that window was switched away from a moment later, with nothing to
 * show that anything had happened.
 *
 * So the mark is taken back and given again around the choice. Nothing is
 * awaited in between, so it is never observably given too early.
 */
function openTheStartingView({ restoring = false } = {}) {
  document.documentElement.dataset.loaded = 'no';

  switchView(startingView());

  // Back to where a reload this application caused took somebody from. Only on a
  // page load: signing in is not coming back from anywhere.
  if (restoring) restorePlace();

  document.documentElement.dataset.loaded = 'yes';
}

/**
 * Whether this reader can actually be sent to that screen.
 *
 * A view name is never more than a string that was true once: the hash is
 * whatever was typed or bookmarked, and a remembered screen may belong to a tab
 * this account has since lost, or to one that stopped existing between releases -
 * this application has removed a view before now.
 *
 * Its own function because two callers ask it and only one used to. startingView
 * had this inline; onwardView, behind the greeting's Continue button, took the
 * remembered name unchecked. A screen that no longer exists then went through to
 * switchView, which hid every view and showed one that was not there: a blank
 * page, no tab marked current, and no way out but the title or a reload.
 *
 * The greeting is permitted by name because it has no tab of its own. It asks for
 * no right - it only ever says what this person already has.
 */
function viewIsOffered(name) {
  return name === 'welcome'
    || $$('.tab').some((tab) => !tab.hidden && tab.dataset.view === name);
}

function startingView() {
  const wanted = currentHashView();
  if (viewIsOffered(wanted)) return wanted;

  const last = rememberedView();
  if (viewIsOffered(last)) return last;

  return 'welcome';
}

/** Picks the first tab the user is actually allowed to see. */
function firstVisibleView() {
  const tab = $$('.tab').find((candidate) => !candidate.hidden);
  return tab ? tab.dataset.view : 'settings';
}

async function refreshAll() {
  // Says whether every screen has been filled from the server yet.
  //
  // Written for anything watching from outside - a browser case, a person
  // wondering why a card is blank - because there is no other way to tell a
  // screen that is empty from one that has not been answered yet. Every form
  // and every card in this application is in index.html, so all of them are on
  // screen from the first paint and hold nothing until their request lands.
  //
  // Cleared here rather than only set at the end, so a second pass says
  // "loading" again while it runs.
  document.documentElement.dataset.loaded = 'no';

  await loadMe();

  // From here on, whether this account may still do what it could a moment ago
  // is checked on a timer as well as on every response. Started after /me
  // because that is what establishes the revision to compare against.
  startPermissionPolling();

  // And the connection the server writes down when the application itself is
  // about to change underneath this page. Only for somebody signed in: it is the
  // sign-in screen's business what happens after they get there.
  startAnnouncements();

  // Whether a newer version exists, for whoever may do something about it.
  startReleaseWatch();

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

    // Loaded, as far as this account is going to get: the server refuses the
    // rest until the password is replaced, so there is nothing else coming.
    // Saying "still loading" here would be waiting for requests that are never
    // going to be made.
    document.documentElement.dataset.loaded = 'yes';

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

  document.documentElement.dataset.loaded = 'yes';
}

function wireForms() {
  $('#form-user').addEventListener('submit', (e) => {
    e.preventDefault();

    const form = e.target;
    const id = form.elements.id.value;

    // Correcting an existing account, which takes the two fields that have no
    // other control. The role is changed from its own dropdown in the row, and a
    // password is not set for somebody else here - so neither is read from a form
    // that is not showing them, and neither is sent.
    if (id) {
      const correction = {
        name: form.elements.name.value.trim(),
        email: form.elements.email.value.trim(),
      };

      mutate(() => api(`/users/${id}`,
        { method: 'PUT', body: JSON.stringify(correction) }),
      t('msg.userSaved', 'User saved'),
      async () => { resetUserForm(); await refreshAll(); });

      return;
    }

    // No working times here. A new account starts on the instance default under
    // Settings and its owner changes it under My account - a daily target is a time
    // figure, and administering an installation is not the same job as recording time
    // in it.
    const body = formData(form);

    mutate(() => api('/users', { method: 'POST', body: JSON.stringify(body) }),
      t('msg.userCreated', 'User created'),
      async () => { resetUserForm(); await refreshAll(); });
  });

  $('#user-cancel').addEventListener('click', resetUserForm);

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

  $('#welcome-recent-all')?.addEventListener('click', () => switchView('timesheets'));

  $('#role-reset').addEventListener('click', resetRoleForm);

  // The same thing, offered where a shipped role is being read rather than at
  // the end of a form that is a screen further down.
  $('#role-start-new')?.addEventListener('click', () => {
    resetRoleForm();
    $('#form-role .perms')?.scrollIntoView({ block: 'start', behavior: 'smooth' });
    $('#form-role').elements.name.focus();
  });

  $('#form-project').addEventListener('submit', (e) => {
    e.preventDefault();

    const form = e.target;
    const id = form.elements.id.value;

    if (id) {
      // Through correctionFrom rather than written out here. This was the object
      // that knew an emptied optional field has to travel as empty, and it was
      // the only one - the booking form beside it did not, so an emptied
      // description there saved as no change. One reader for both now.
      //
      // The start date is not among the emptiable ones: it is the one date a
      // project must have, so an empty one is somebody clearing a box rather than
      // removing a fact, and sending it would be refused. Left out, it stays what
      // it was.
      const changes = correctionFrom(form, ['description', 'endDate']);

      mutate(() => api(`/projects/${id}`,
        { method: 'PUT', body: JSON.stringify(changes) }),
      t('msg.projectSaved', 'Project saved'),
      async () => { resetProjectForm(); await refreshAll(); });

      return;
    }

    const body = formData(form);

    mutate(() => api('/projects', { method: 'POST', body: JSON.stringify(body) }),
      t('msg.projectCreated', 'Project created'),
      async () => { resetProjectForm(); await refreshAll(); });
  });

  $('#project-cancel').addEventListener('click', resetProjectForm);

  $('#form-timesheet').addEventListener('submit', (e) => {
    e.preventDefault();
    const form = e.target;
    const id = form.elements.id.value;

    // The same form books and corrects; the id decides which - and it decides
    // how the form is read, too. Booking drops an empty box, correcting sends
    // it: see correctionFrom. Number('') is 0, which is how the service is told
    // to take a project off again; Number(undefined) is NaN, which reaches it as
    // null and means "leave it alone".
    const editing = Boolean(id);

    const raw = editing
      ? correctionFrom(form, ['description', 'projectId'])
      : formData(form);

    const body = {
      ...raw,
      projectId: Number(raw.projectId),
      durationHours: Number(raw.durationHours),
    };
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

  // One row, because the total covers the reader's own hours and nobody else's.
  // The column used to name the person, which is now always the same person.
  function renderReport(report) {
    const rows = (report.entries ?? []).map((entry) => el('tr', {},
      el('td', { text: `${fmtDate(report.from)} – ${fmtDate(report.to)}` }),
      el('td', { class: 'num', text: fmtNumber(entry.hours) }),
    ));

    fillTable($('#table-report tbody'), rows, 2, t('ot.empty', 'No bookings in this period.'));
    $('#report-total').textContent = t('report.total', '{0} in total')
      .replace('{0}', fmtHours(report.totalHours));
    $('#report-result').hidden = false;
  }

  $('#form-report').addEventListener('submit', (e) => {
    e.preventDefault();
    const { projectId, from, to } = formData(e.target);
    const params = new URLSearchParams();
    if (projectId) params.set('projectId', projectId);
    if (from) params.set('from', from);
    if (to) params.set('to', to);
    const suffix = params.toString() ? `?${params}` : '';

    mutate(async () => {
      // /reports rather than /projects/{id}/report, because the selection is not
      // always a project: an empty projectId means every one of them and "none"
      // means the hours that never got one, and neither fits in a path.
      const report = await api(`/reports${suffix}`);

      // Drawn through redrawable, so a language change draws it again from this
      // same answer rather than leaving an English total under a German heading.
      redrawable('report', () => renderReport(report));

      // The same period as a picture. The figures come from the statistics
      // endpoint rather than the report, because a total is one number and a
      // chart of one number says nothing - that endpoint already breaks the
      // same range down, scoped to this person by the same right.
      await loadReportChart(report.from, report.to, projectId);
    }, null, null);
  });

  function renderOvertime(balance) {
    const rows = (balance.days ?? []).map((d) => el('tr', {},
      el('td', { text: fmtDate(d.date) }),
      el('td', { class: 'num', text: fmtHours(d.booked) }),
      el('td', { class: 'num', text: fmtHours(d.target) }),
      balanceCell(d.balance),
    ));

    fillTable($('#table-overtime tbody'), rows, 4, t('ot.empty', 'No bookings in this period.'));

    const total = balance.totalBalance;
    const pill = $('#overtime-total');
    pill.textContent = `${total > 0 ? '+' : ''}${fmtHours(total)}`;
    pill.className = `pill ${total > 0 ? 'plus' : total < 0 ? 'minus' : ''}`;
    $('#overtime-meta').textContent = t('ot.meta', '{0} · target {1}/day · booked {2} of {3}')
      .replace('{0}', balance.userName)
      .replace('{1}', fmtHours(balance.dailyTarget))
      .replace('{2}', fmtHours(balance.totalBooked))
      .replace('{3}', fmtHours(balance.totalTarget));
    $('#overtime-result').hidden = false;
  }

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

      redrawable('overtime', () => renderOvertime(balance));
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
    saveForm(form, () => api(`/users/${me.user.id}/working-times`,
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

  // Through mutate like every other loader, so a half-filled booking form is left
  // alone while the table grows underneath it.
  $('#ts-more').addEventListener('click',
    () => mutate(() => loadTimesheets(true), null, null));

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

  // Here rather than with the rest of the wiring below, so the sign-in form has
  // its reveal before anything that could fail has been tried. It also registers
  // a submit handler per field, which runs beside a form's own rather than
  // instead of it - two listeners on one event, in the order they were added.
  wirePasswordReveal();

  // Beside the password fields, and for the same reason: a field added later is
  // covered without anybody remembering to come back here.
  enhanceDateFields();

  // Every form, and before anything is loaded, so a reader who starts typing
  // into a card the moment it appears is already protected from the loaders
  // filling the cards after it. Forms nothing refills are unaffected by it.
  $$('form').forEach(watchForEditing);

  // And the few controls that hold typed work without belonging to one. Bound to
  // the document rather than to each control, because some of them are drawn
  // later than this runs.
  for (const event of ['input', 'change']) {
    document.addEventListener(event, (e) => {
      if (e.target?.dataset?.keep !== undefined) rememberLooseDraft();
    });
  }

  // Appearance is a device setting and needs no session, so the picker works on
  // the sign-in screen too.
  wireTheme();

  // Coming back to a tab is the moment somebody would find out anyway, so it is
  // the moment to ask - and it covers however long the tab was hidden in one
  // request, which is why the interval below is allowed to skip while it is.
  document.addEventListener('visibilitychange', () => {
    if (document.hidden || !me.user) return;

    api('/me').catch(() => {});
  });

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
    wireUpdate();
    wireUpdateCheck();
    wireCropChooser();

    // The one control on the rights notice, and the whole of what there is to do
    // about it.
    $('#rights-reload')?.addEventListener('click', () => window.location.reload());

    $('#branding-language')?.addEventListener('change', (e) => {
      showBrandingLanguage(e.target.value);
    });
    wireNavigationMenu();
    trackTopbarHeight();
    wireTimer();
    wireStatistics();
    wireDocumentExports();
    wireReportChart();
    wireWorkbook();
    wireSheetCards();
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

    // After the loaders and before anything can look: everything from here to
    // the end of openTheStartingView happens without awaiting, so nothing
    // observes a page calling itself loaded while what somebody typed into it is
    // still missing.
    restoreDrafts();

    hideLogin();
    openTheStartingView({ restoring: true });

    // After the first view is up, so the tour highlights something that is
    // actually on screen.
    await greetAfterSignIn();

    // And whatever the document before this one was told to say. An update
    // finishes by throwing the page away, so the answer to the button somebody
    // pressed arrives here rather than there.
    saySoAfterTheReload();
  } catch {
    // No usable session: the sign-in screen is the whole interface until
    // there is one. Unless somebody signed in while this was running, which
    // showLogin decides - it is the same question wherever it is asked from.
    showLogin();
  }
}

document.addEventListener('DOMContentLoaded', init);
