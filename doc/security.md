# Security

Implemented, and verified against a running instance:

- **Session cookies** are `HttpOnly` and `SameSite=Lax`, and `Secure` whenever
  the connection is HTTPS. Sessions live in the database, so a restart does not
  sign everyone out. Changing a password ends every *other* session of that user
  — one opened with the old password on another device stops working
  immediately, and the device that just proved it knew the old password carries
  on. Ending that one as well signed people out of the setup wizard between two
  of its own steps.
- **CSRF protection** on every request that changes something. Two independent
  checks have to pass: the `Origin` (or, failing that, the `Referer`) must name
  this host, and the `X-CSRF-Token` header must equal the `gtr_csrf` cookie.
  That cookie is deliberately readable by JavaScript — the point is that our own
  script can read it and another site's cannot. The token is replaced on
  sign-in, so one handed to an anonymous visitor never carries into a session.
  Requests authenticated by a personal API token are exempt, because a browser
  never attaches that header by itself and there is nothing to forge.
- **Passkeys** (WebAuthn) are opt-in per user, under *My account*. Signing in
  is then a fingerprint, a face or a device PIN - no password typed, so nothing
  to phish, reuse or read out of somebody else's breach. The private half never
  leaves the device; what is stored here is a public key that verifies
  signatures and cannot produce them.

  They are an **addition**, never a replacement: the password keeps working
  alongside, so a lost device is an inconvenience rather than a lockout. The
  built-in administrator is deliberately excluded from registering one at all -
  it exists so an installation always has a way back in, and a way back in that
  depends on a particular phone still existing is not one.

  The relying party is derived from the request, so nothing needs configuring.
  Two consequences worth knowing: passkeys are offered only over HTTPS or on
  `localhost` (browsers refuse elsewhere, and reject an IP address as a relying
  party outright), and a credential is bound to that domain permanently -
  moving the application to a different one means everyone re-registers.
- **Two-factor authentication** (TOTP, RFC 6238) is opt-in per user. The
  implementation is checked against the RFC's published test vectors, so it
  interoperates with real authenticator apps.

  Enrolment shows a **QR code** to scan, with the key to type folded away behind
  it for a machine with no camera and for when a code will not scan. The code is
  rendered by the binary as an inline SVG, so it needs no external service — the
  strict `Content-Security-Policy` already allows `data:` images. It is cleared
  from the screen when you leave it: the picture encodes the shared secret.

  **With both enabled, a passkey sign-in does not ask for a code.** That is
  deliberate rather than an oversight: registration and sign-in both require user
  verification, so the device had to see a fingerprint or a PIN before it would
  sign — possession of the device plus verification of the person, which is
  already two factors. Google, Microsoft and Apple treat passkeys the same way:
  they *satisfy* multi-factor rather than needing another one stacked on top.

  Concurrency note, because it bit: every write to an account used to go through
  an update of the whole row, so two settings changed at once meant the second
  reverted the first — recording the guided tour as seen erased a two-factor secret
  issued a moment earlier. The settings a person changes in passing now write their
  own column, and the two two-factor columns are written together as the one fact
  they are.

  The consequence to be aware of: turning two-factor on does **not** force a
  second factor on somebody who has a passkey, because their passkey is a way in
  that never asks for one. If you need two-factor as an enforceable policy, this
  setting alone is not it. A browser test pins the behaviour, so changing it
  would have to be a decision rather than an accident.
- **Security headers** on every response: a strict `Content-Security-Policy`
  (no external origin is permitted, which nothing legitimate needs here),
  `X-Frame-Options: DENY`, `X-Content-Type-Options: nosniff`,
  `Referrer-Policy: no-referrer`, `Permissions-Policy`, and the two
  `Cross-Origin-*` isolation headers. API responses carry `Cache-Control:
  no-store`, because they contain personal data.
- **HSTS** is sent only over connections that already are HTTPS. Sending it
  over plain HTTP could lock a host out of working at all.
- **Rate limiting** on sign-in attempts and token-authenticated requests
  (default 30 per minute per client), which is what makes password and token
  guessing impractical. Ordinary browsing is not limited. A reverse proxy is
  still the right place for a hard global limit — this one is per process.
- **Secrets are never returned.** Database and LDAP passwords, and API tokens,
  are write-only over the API; clients get a `hasPassword` flag instead.
- **Enumeration is avoided**: a wrong password and an unknown account fail
  identically, and somebody else's project answers `404` rather than `403`, which
  would confirm the id exists.
- **Passwords** are bcrypt hashes; session and API tokens are 256 bits of
  randomness stored only as SHA-256.

## HTTPS with Let's Encrypt

Set `TLS_ENABLED=true` and list the hostnames in `TLS_DOMAINS`. Certificates
are obtained and renewed automatically, and cached under `TLS_CACHE_DIR` so a
restart does not request new ones.

Requirements: the host must be reachable from the internet on
`TLS_REDIRECT_PORT` (80), which answers the ACME challenge and redirects
everything else to HTTPS. Use `TLS_STAGING=true` while setting things up — its
certificates are not trusted by browsers, but its rate limits are far looser
than the production ones.

## What a copy of the database gives away

Passwords are bcrypt hashes, so a stolen dump yields none of them. Two other
values cannot be hashed, because the application has to read them back rather
than compare against them: the TOTP seed, which is fed to the code generator on
every sign-in, and the directory's bind password, which is sent to the directory.

`SECRET_KEY` encrypts those two, with the key outside the database:

```bash
openssl rand -base64 32
```

Three things follow from that, and all three are easier to know than to discover.

- **It is opt-in, and silence is not safety.** An installation without the key
  works exactly as before and stores both values in the clear; it says so once at
  every start. Setting the key for the first time encrypts what is already
  stored, and logs how many values it moved.
- **Losing the key costs every enrolled second factor.** The application refuses
  to start rather than reading rubbish, and the way back is to clear
  `users.totp_secret` and the directory password and have them entered again.
  Back the key up wherever the database password is backed up — and not in the
  same place as the database.
- **It is protection against a copy, not against the machine.** A backup on a
  laptop, a snapshot with weaker access, a managed database somebody can read.
  Whoever has the machine has the key too, and nothing here changes that.

Two things about this arrangement that are easier to know than to discover:

- **The plain `HTTP_PORT` listener stays bound, on every interface, but stops
  answering the network.** GoFr binds every interface and offers no way to say
  otherwise, so the socket is there; while HTTPS is being served from this
  process, that port answers only the loopback address the TLS front end dials
  from and sends everything else to the encrypted address with a 308. This used
  to say "close it yourself", which was a real instruction and a fragile one: a
  second step, on a different machine, after being told the installation is
  finished. Firewalling it is still tidier — nothing about this asks you not to.
- **A refused HTTPS bind stops HTTPS, and says so.** It used to be started in a
  goroutine that only logged, so an unprivileged process that may not bind 443
  came up serving plain HTTP with one error line nobody was reading. The bind now
  happens before start-up returns, and a failure is reported as
  `could not start HTTPS: cannot bind :443 …; continuing without HTTPS`.
- **It still does not stop the process.** That is deliberate — an installation
  that cannot get its certificate is more useful reachable than not — so the
  plain port keeps serving the network in exactly the case where HTTPS is not
  there to redirect to. After switching TLS on, look for `serving HTTPS on :443`
  and connect to the name; "the service is running" is not evidence.

GoFr owns its own listener and accepts only a static certificate pair, so TLS
is terminated in front of it and proxied to localhost.
