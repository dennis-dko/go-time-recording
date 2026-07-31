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

**Erste Anmeldung:** Beim ersten Start wird automatisch ein Administrator
angelegt:

| | |
| --- | --- |
| Benutzer | `admin@local` |
| Passwort | `changeme123` |

Dieses Konto ist **nicht löschbar** und kann nicht in eine Rolle ohne
Administrationsrechte verschoben werden — eine Installation kann sich damit
nicht selbst aussperren. Das Initialpasswort ist als „muss geändert werden"
markiert; die Oberfläche weist darauf hin, bis es ersetzt wurde.

## Was drin ist

| Bereich | Umsetzung |
| --- | --- |
| Datenhaltung | SQLite (Default, pure Go – kein cgo), umschaltbar auf PostgreSQL/MySQL |
| Schema | GoFr-Migrationen, laufen automatisch beim Start |
| API | REST unter `/api/v1` |
| Weboberfläche | eingebettet via `go:embed`, Vanilla JS ohne Build-Schritt |
| Rechte | RBAC mit frei administrierbaren Rollen, Passwörter als bcrypt-Hash |
| Überstunden | Saldo pro Tag und Zeitraum, gegen ein Tagessoll pro Person |
| Hintergrundjob | Cron: reicht zu lange offene Zeiteinträge automatisch ein |
| Betrieb | Health-/Alive-Endpunkte, Prometheus-Metriken, Tracing (alles von GoFr) |

## Rechtesystem

Anmeldung erfolgt mit E-Mail-Adresse und Passwort. Was jemand darf, ergibt
sich aus der **Rolle**, und Rollen sind zur Laufzeit über die Oberfläche
administrierbar (anlegen, Rechte setzen, löschen).

Mitgelieferte Rollen:

| Rolle | Zweck |
| --- | --- |
| `admin` | alles; Systemrolle, nicht löschbar und nicht entrechtbar |
| `manager` | Projekte führen, Zeiten aller Personen sehen und genehmigen |
| `employee` | eigene Zeiten buchen und einreichen, Projekte lesen |

Berechtigungen sind fein granuliert, z. B. `timesheets:read:own` gegenüber
`timesheets:read:all`, oder `timesheets:approve` getrennt vom Schreiben. Eine
Person mit nur `…:own` wird serverseitig auf die eigenen Daten eingegrenzt,
auch wenn sie einen fremden Filter mitschickt.

Die Berechtigungen sind Konstanten im Code, keine Datenbankzeilen: jede wird
von einer konkreten Codestelle geprüft, und eine frei erfundene Berechtigung
würde nichts gewähren. Die Rollenverwaltung bietet deshalb genau die
Berechtigungen an, die tatsächlich durchgesetzt werden.

**Wichtig zur Abmeldung:** Die Authentifizierung nutzt HTTP Basic Auth. Browser
merken sich diese Zugangsdaten bis zum Schließen des Fensters — es gibt daher
bewusst keinen Logout-Button, weil er nichts bewirken würde.

## Überstunden

Jede Person hat ein **Tagessoll** (Grundlage der Überstunden) und ein
**Tagesmaximum** (Obergrenze fürs Buchen); beides ist unter „Mein Konto"
selbst einstellbar. Ohne eigene Einstellung gelten 8 h Soll und das
instanzweite `MAX_DAILY_HOURS`.

Der Saldo ist die Summe aus `gebucht − Soll` über alle Tage **mit Buchungen**.
Tage ohne Buchung zählen bewusst nicht mit: ohne Feiertags- und Urlaubskalender
– den diese Anwendung nicht hat – würden Wochenenden und Urlaub sonst als
wachsendes Minus erscheinen. Abgelehnte Einträge zählen nicht mit.

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
| `AUTH_ENABLED` | `true` | `false` gibt **jedem** Aufrufer volle Adminrechte |
| `CREDENTIAL_CACHE_TTL` | `1m` | wie lange eine geprüfte Anmeldung gemerkt wird |
| `AUTO_CLOSE_SCHEDULE` | `0 2 * * *` | Cron für den Aufräumjob, leer = aus |
| `AUTO_CLOSE_AFTER_DAYS` | `14` | ab wann ein offener Eintrag eingereicht wird |
| `MAX_DAILY_HOURS` | `24` | instanzweite Obergrenze pro Person und Tag |

`CREDENTIAL_CACHE_TTL` existiert, weil Basic Auth das Passwort bei *jeder*
Anfrage mitschickt und bcrypt absichtlich langsam ist (~50–100 ms). Erfolgreich
geprüfte Anmeldungen werden deshalb kurz gemerkt; das Passwort selbst wird nie
gespeichert, der Schlüssel ist ein HMAC mit einem prozesslokalen Zufallswert.
Bei Passwort- oder Rollenänderungen wird der Cache sofort geleert.

Für PostgreSQL zusätzlich `DB_HOST`, `DB_PORT`, `DB_USER`, `DB_PASSWORD`,
`DB_SSL_MODE` setzen.

## API

Antworten sind in GoFrs Hülle verpackt: `{"data": ...}` bzw. `{"error": ...}`.

| Methode | Pfad | Zweck |
| --- | --- | --- |
| `GET` | `/api/v1/me` | eigene Identität und Berechtigungen |
| `PUT` | `/api/v1/me/password` | eigenes Passwort ändern |
| `GET` | `/api/v1/users` | Liste (`page`, `limit`) |
| `GET/POST` | `/api/v1/users`, `/users/{id}` | Lesen, Anlegen |
| `PUT/DELETE` | `/api/v1/users/{id}` | Ändern (partiell), Löschen |
| `PUT` | `/api/v1/users/{id}/role` | Rolle zuweisen |
| `PUT` | `/api/v1/users/{id}/working-times` | Tagessoll und -maximum setzen |
| `GET` | `/api/v1/users/{id}/overtime` | Überstundensaldo (`from`, `to`) |
| `GET` | `/api/v1/overtime` | Saldo aller Personen |
| `GET/POST` | `/api/v1/roles` | Rollen lesen, anlegen |
| `GET/PUT/DELETE` | `/api/v1/roles/{id}` | Rolle lesen, ändern, löschen |
| `GET` | `/api/v1/permissions` | alle durchgesetzten Berechtigungen |
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
- Pro Person und Tag gilt das persönliche Tagesmaximum, ersatzweise
  `MAX_DAILY_HOURS`.
- Genehmigen und Ablehnen verlangt `timesheets:approve` — wer nur eigene Zeiten
  schreiben darf, kann sie einreichen, aber nicht selbst genehmigen.
- Archiviert wird nur, was abgeschlossen ist und keine offenen Einträge mehr hat.
- Ein Projekt mit Zeiteinträgen lässt sich nicht löschen.
- E-Mail-Adressen sind eindeutig.
- Der eingebaute Administrator ist nicht löschbar und nicht entrechtbar.
- Systemrollen sind nicht löschbar, nicht umbenennbar und nicht entrechtbar.
- Eine Rolle, die noch jemandem zugewiesen ist, lässt sich nicht löschen.

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
