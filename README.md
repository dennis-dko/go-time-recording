<h1><img src="internal/interface/web/assets/favicon.svg" alt="" width="78" align="absmiddle">&nbsp; Go-Time-Recording</h1>

Project time tracking as **a single self-contained binary**: the REST API and
the web interface live in the same executable (the UI assets are embedded with
`go:embed`). Starting it needs no asset directory and no migration step, and no
database server unless you choose one.

Built on [GoFr](https://gofr.dev), structured after [gogs](https://github.com/gogs/gogs).

## Documentation

| Page | What it covers |
| --- | --- |
| [Getting started](doc/getting-started.md) | The installer, the first sign-in, the setup wizard and the tour |
| [Features](doc/features.md) | Everything the application does, and the rules it keeps while doing it |
| [Accounts, roles and sign-in](doc/access.md) | Rights, the administrator, API tokens, LDAP, Kerberos and OpenID Connect |
| [Security](doc/security.md) | Sessions, CSRF, passkeys, two factors, HTTPS, and what a copy of the database gives away |
| [Configuration](doc/configuration.md) | Where each setting comes from, and what the interface can change |
| [API](doc/api.md) | What a refusal looks like, and what the metrics carry |
| [Architecture](doc/architecture.md) | The four layers, and the decisions that would otherwise surprise |
| [Development](doc/development.md) | The environments, and the checks a change has to pass |
| [Releases and deployment](doc/deployment.md) | How a release is made and what it carries |
| [Operations manual](deploy/OPERATIONS.md) | For whoever runs it: the four ways to deploy, backups, restarts, health |

## License

MIT.
