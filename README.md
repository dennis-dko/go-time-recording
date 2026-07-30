# Go-Time-Recording

Projektbezogene Zeiterfassung als **eine einzige, selbstständige Binary**: REST-API
und Weboberfläche stecken zusammen im Executable (die UI-Assets über `go:embed`),
die Datenbank ist standardmäßig SQLite. Für einen Start braucht es weder einen
Datenbankserver noch ein Asset-Verzeichnis noch einen Migrationsschritt.

Gebaut mit [GoFr](https://gofr.dev). Aufbau angelehnt an [gogs](https://github.com/gogs/gogs).

## Schnellstart

```bash
task run          # baut und startet (Standard: dev)
# oder direkt:
go run ./cmd/main.go
```

Danach: <http://localhost:8000> — die Weboberfläche wird von derselben Binary
ausgeliefert wie die API.

## Was drin ist

| Bereich | Umsetzung |
| --- | --- |
| Datenhaltung | SQLite (Default, pure Go – kein cgo), umschaltbar auf PostgreSQL/MySQL |
| Schema | GoFr-Migrationen, laufen automatisch beim Start |
| API | REST unter `/api/v1` |
| Weboberfläche | eingebettet via `go:embed`, Vanilla JS ohne Build-Schritt |
| Hintergrundjob | Cron: reicht zu lange offene Zeiteinträge automatisch ein |
| Auth | optionale Basic Auth über die gesamte Anwendung |
| Betrieb | Health-/Alive-Endpunkte, Prometheus-Metriken, Tracing (alles von GoFr) |

## Architektur

Vier Schichten, Abhängigkeiten zeigen ausschließlich nach innen:

```
cmd/main.go                     Verdrahtung (DI), Migrationen, Cron, Auth
│
├── internal/interface/         Eintrittspunkte
│   ├── api/v1/rest/              HTTP-Handler, DTOs, Status-Code-Mapping
│   ├── web/                      eingebettete Weboberfläche + Middleware
│   └── worker/                   geplante Hintergrundjobs
│
├── internal/application/v1/    Anwendungsfälle (CQRS-artig)
│   ├── command/ query/           Ein-/Ausgabe je Use Case
│   ├── common/                   Result-Modelle + Mapper
│   └── service/                  Orchestrierung + Use-Case-Regeln
│
├── internal/domain/            Fachlicher Kern (frameworkfrei)
│   ├── model/                    Entitäten und Statuswerte
│   ├── repository/               Repository-Interfaces
│   └── service/                  fachliche Regeln über mehrere Entitäten
│
└── internal/infrastructure/    Technik
    ├── config/                   anwendungseigene Einstellungen
    └── persistence/
        ├── sqldb/                Repositories (dialektübergreifend)
        ├── memory/               In-Memory-Repositories für Tests
        └── migrations/           Schemadefinition
```

Zwei Entscheidungen, die beim Lesen sonst überraschen:

- **Die Routen sind explizit deklariert**, nicht über GoFrs `AddRESTHandlers`.
  Dieser Helfer erzeugt CRUD per Reflection direkt gegen eine Tabelle und würde
  die Domain- und Application-Schicht umgehen.
- **Die UI läuft über eigene Middleware**, nicht über GoFrs `AddStaticFiles`.
  Letzteres liest ausschließlich von der Festplatte und würde das Ziel „eine
  Binary" zunichtemachen.

## Konfiguration

Alle Werte kommen aus `cmd/configs/.<env>.env`; die Umgebung wählt `APP_ENV`.

| Variable | Default | Bedeutung |
| --- | --- | --- |
| `HTTP_PORT` | `8000` | API und Weboberfläche |
| `METRICS_PORT` | `2121` | Prometheus-Endpunkt |
| `DB_DIALECT` | `sqlite` | auch `postgres`, `mysql` |
| `DB_NAME` | `go-time-recording` | bei SQLite der Dateiname (ohne `.db`) |
| `UI_ENABLED` | `true` | `false` betreibt die Binary als reine API |
| `BASIC_AUTH_USER` / `BASIC_AUTH_PASSWORD` | leer | beide gesetzt ⇒ Auth aktiv |
| `AUTO_CLOSE_SCHEDULE` | `0 2 * * *` | Cron für den Aufräumjob, leer = aus |
| `AUTO_CLOSE_AFTER_DAYS` | `14` | ab wann ein offener Eintrag eingereicht wird |
| `MAX_DAILY_HOURS` | `24` | Obergrenze pro Person und Tag |

Für PostgreSQL zusätzlich `DB_HOST`, `DB_PORT`, `DB_USER`, `DB_PASSWORD`,
`DB_SSL_MODE` setzen.

## API

Antworten sind in GoFrs Hülle verpackt: `{"data": ...}` bzw. `{"error": ...}`.

| Methode | Pfad | Zweck |
| --- | --- | --- |
| `GET` | `/api/v1/users` | Liste (`page`, `limit`) |
| `GET/POST` | `/api/v1/users`, `/users/{id}` | Lesen, Anlegen |
| `PUT/DELETE` | `/api/v1/users/{id}` | Ändern (partiell), Löschen |
| `PUT` | `/api/v1/users/{id}/role` | Rolle zuweisen |
| `GET` | `/api/v1/projects` | Liste (`status`) |
| `GET/POST/PUT/DELETE` | `/api/v1/projects`, `/projects/{id}` | CRUD |
| `POST` | `/api/v1/projects/{id}/archive` | archivieren |
| `GET` | `/api/v1/projects/{id}/report` | Auswertung (`from`, `to`) |
| `GET` | `/api/v1/timesheets` | Liste (`userId`, `projectId`, `status`, `from`, `to`) |
| `GET/POST/PUT/DELETE` | `/api/v1/timesheets`, `/timesheets/{id}` | CRUD |
| `POST` | `/api/v1/timesheets/{id}/transfer` | auf anderes Projekt umbuchen |

Betrieb: `/.well-known/health`, `/.well-known/alive`, Metriken auf Port 2121.

Beispiel:

```bash
curl -X POST localhost:8000/api/v1/users \
  -H 'Content-Type: application/json' \
  -d '{"name":"Dennis","email":"dennis@example.com","role":"admin"}'

curl -X POST localhost:8000/api/v1/timesheets \
  -H 'Content-Type: application/json' \
  -d '{"userId":1,"projectId":1,"date":"2026-07-15","durationHours":8}'
```

## Fachliche Regeln

Diese Regeln setzt der Server durch — die Oberfläche blendet unzulässige
Aktionen nur zusätzlich aus:

- Zeiteinträge folgen `offen → eingereicht → genehmigt / abgelehnt`,
  abgelehnte dürfen zurück auf `offen`. Andere Sprünge werden abgelehnt.
- Ein **genehmigter** Eintrag lässt sich weder ändern, löschen noch umbuchen.
- Gebucht wird nur auf **aktive** Projekte.
- Pro Person und Tag gilt `MAX_DAILY_HOURS`.
- Archiviert wird nur, was abgeschlossen ist und keine offenen Einträge mehr hat.
- Ein Projekt mit Zeiteinträgen lässt sich nicht löschen.
- E-Mail-Adressen sind eindeutig.

## Entwicklung

```bash
task            # Build (dev)
task run        # Build + Start, ENV=staging o. Ä. möglich
task test       # Tests
task upgrade    # Go-Toolchain in go.mod + alle Abhängigkeiten aktualisieren
task release VERSION=v1.2.3   # taggt; den Rest erledigt die CI
```

Formatierung, Vet und Linting laufen bewusst nicht über den Taskfile: das
erledigt lokal die VS-Code-Go-Extension und in CI
[`ci.yml`](.github/workflows/ci.yml).

## Deployment

```bash
docker build -t go-time-recording .
docker run -p 8000:8000 -v go-time-data:/data go-time-recording
```

Das Image ist zweistufig gebaut; im finalen Layer liegen nur die statische
Binary, die Konfiguration und ein Nicht-Root-Nutzer. Die SQLite-Datei liegt
unter `/data` und gehört auf ein Volume, sonst ist sie beim Ersetzen des
Containers weg.

Auf Tags `vX.Y.Z` veröffentlicht [`release.yml`](.github/workflows/release.yml)
das Image nach GHCR und legt einen GitHub-Release an.

## Lizenz

MIT.
