/*
 * What lets the installed application open without a network, and nothing else.
 *
 * It keeps the shell - the document, the script, the stylesheet, the pictures -
 * as last fetched, and hands it out when the installation cannot be reached.
 * Everything is asked of the network first, so a page that can reach the
 * installation always gets today's files; the copy is only for the moment it
 * cannot.
 *
 * Nothing the API answers is kept or handed out from here, ever. Who somebody is,
 * what they booked, what a balance says: all of it is asked fresh or not shown,
 * because a figure from an hour ago shown as if it were now is worse than none.
 * Bookings made meanwhile wait in the page itself - see keepForLater in app.js.
 */
const SHELL = 'gtr-shell-v1';

/** What a page needs to draw itself at all, kept from the first visit. */
const ESSENTIALS = ['/', '/app.js', '/app.css', '/theme.js', '/favicon.svg'];

self.addEventListener('install', (event) => {
  event.waitUntil(caches.open(SHELL).then((cache) => cache.addAll(ESSENTIALS)));

  // The new shell keeper takes over at once rather than at the next visit: what
  // it keeps is only ever a copy, and an older keeper has nothing to finish.
  self.skipWaiting();
});

self.addEventListener('activate', (event) => {
  event.waitUntil(caches.keys()
    .then((names) => Promise.all(names.filter((name) => name !== SHELL).map((name) => caches.delete(name))))
    .then(() => self.clients.claim()));
});

self.addEventListener('fetch', (event) => {
  const { request } = event;
  const url = new URL(request.url);

  // Only reads of this installation's own shell. The API, a write, and anything
  // from elsewhere go to the network exactly as they would without this.
  if (request.method !== 'GET' || url.origin !== self.location.origin || url.pathname.startsWith('/api/')) {
    return;
  }

  // A page is one document whatever its address says: the interface is a single
  // page, and the server answers every path with it.
  const key = request.mode === 'navigate' ? '/' : request;

  event.respondWith(fetch(request)
    .then((response) => {
      if (response.ok && response.type === 'basic') {
        const copy = response.clone();

        event.waitUntil(caches.open(SHELL).then((cache) => cache.put(key, copy)));
      }

      return response;
    })
    .catch(() => caches.match(key).then((kept) => kept ?? Response.error())));
});
