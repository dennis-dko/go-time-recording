# Architecture

Four layers; dependencies point inwards only.

```text
cmd/                            Wiring (DI), migrations, TLS, the scheduled jobs
│
├── internal/interface/         Entry points
│   ├── api/v1/rest/              HTTP handlers, DTOs, authorization, status codes
│   ├── web/                      Embedded web interface and its middleware
│   └── installer/                First-run screen, served until a database is chosen
│
├── internal/application/v1/    Use cases (CQRS-flavoured)
│   ├── command/ query/           Input and output per use case
│   ├── common/                   Result models and mappers
│   └── service/                  Orchestration and use-case rules
│
├── internal/domain/            Business core, free of framework code
│   ├── model/                    Entities, statuses, permissions
│   ├── repository/               Repository interfaces
│   └── service/                  Rules spanning several entities
│
├── internal/infrastructure/    Technical concerns
│   ├── config/                   Application settings and the datasource file
│   ├── directory/                LDAP client
│   ├── tlsserver/                Let's Encrypt termination
│   ├── announce/                 What every open browser is told at once
│   ├── selfupdate/ imageupdate/  Replacing the binary, or asking for a new image
│   ├── restart/                  Restarting from the Settings screen
│   ├── logsink/                  The recent log lines the Settings screen shows
│   └── persistence/
│       ├── sqldb/                Repositories, dialect-agnostic
│       ├── memory/               In-memory repositories for tests
│       └── migrations/           Schema definition
│
└── internal/support/           Leaf helpers that know nothing of the domain
    ├── apperror/                 The closed catalogue of refusals
    ├── security/                 Passwords, tokens and sealed secrets
    └── document/ spreadsheet/ imaging/ qrcode/ hosting/
```

Decisions that would otherwise be surprising:

- **Routes are declared explicitly**, not through GoFr's `AddRESTHandlers`.
  That helper reflects over a struct to generate CRUD straight against a table,
  which would bypass the domain and application layers this project is built on.
- **The UI is served by custom middleware**, not GoFr's `AddStaticFiles`, which
  only reads from disk and would defeat the single-binary goal.
- **Authorization sits in the handlers**, not in a route table: several rules
  depend on the resource rather than the path — reading your own time entries
  and reading everyone's share one route.
- **The database connection is stored in a file**, not in the database. It
  describes *which* database to open, so keeping it inside that database would
  make it impossible to point the application elsewhere.
