# Backlog des kong-CLI-Umbaus: Härtung, Log-Flags und ein einmaliger Import

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Die vier offenen Punkte aus dem Backlog von `docs/superpowers/plans/2026-09-19-kong-cli-commands.md` abarbeiten: `server.Run` drainiert auch im Fehlerfall, Migrationen beachten den Kontext, `--log-level`/`--log-format` werden globale Flags, und `recipe-reader import` macht einen einmaligen Importlauf ohne Server.

**Architecture:** Nichts an der Struktur des CLI ändert sich: der kong-Baum bleibt in `cmd/recipe-reader/main.go`, die Kommandos in `internal/cli`, die Arbeit in `internal/*`. Neu sind ein Port `pipeline.ImportLock` mit einem Postgres-Adapter in `internal/db`, der einen Importlauf prozessübergreifend serialisiert, ein Paket `internal/logging` für den slog-Handler, und `server.ImportOnce` als einmalige Hälfte dessen, was der Worker sonst auf einem Zeitplan tut.

**Tech Stack:** Go 1.27.1, `github.com/alecthomas/kong` v1.16.1, `github.com/golang-migrate/migrate/v4` v4.19.1, pgx/v5, testcontainers-go, Docker. **Keine neue Abhängigkeit**; alles hier ist Standardbibliothek oder schon im Modul.

## Global Constraints

- **Branch:** Nie auf `main` arbeiten. Jeder Milestone hat einen eigenen Branch und einen eigenen PR; die Namen stehen unter „Branches und PRs“.
- **Kompatibilität:** Bestehende Env-Variablen-Namen, Flag-Namen und Defaults bleiben **unverändert**. Neue Flags folgen demselben Muster (`name:`, `env:`, `default:`, `help:`) und sind additiv: jedes bestehende Kommando muss ohne sie genau so laufen wie vorher.
- **Konfigurationsdateien werden mitgepflegt:** Wer eine Env-Variable hinzufügt, trägt sie in `.env.example` nach, im Stil der Datei: unkommentiert, mit dem Default als Wert, thematisch gruppiert; Credentials stehen dort mit leerem Wert (`API_TOKEN=`). `docker-compose.yml` wird angefasst, wo eine Änderung es verlangt.
- **Credentials:** `API_TOKEN`, `INSTAGRAM_PASSWORD`, `LLM_API_KEY` und `ANTHROPIC_API_KEY` bleiben **nur über die Umgebung** setzbar, ohne Flag-Form (`kong:"-"`).
- **Exit-Codes:** `0` bei Erfolg, `1` bei **jedem** Fehler, auch bei Parse-Fehlern. Der Docker-`HEALTHCHECK` erwartet 0/1.
- **Kommentarstil:** Kommentare erklären das *Warum*, nicht das *Was*; verschobener Code behält seine Kommentare wortgleich. Zeilen bis ~100 Spalten, wie im Rest des Repos.
- **Standardbibliothek zuerst:** Keine neue Abhängigkeit in diesem Plan. Wer eine braucht, hält an und fragt.
- **Commit-Gate (vor jedem Commit, Pflicht laut `~/.claude/CLAUDE.md`; Docker muss laufen):**
  ```bash
  go fix ./...
  gofmt -w .
  go vet ./...
  /home/ripmav/.local/bin/golangci-lint run ./...
  go run golang.org/x/vuln/cmd/govulncheck@latest ./...
  go test -count=1 -race ./...
  ```
  Jeder Befund wird vor dem Commit behoben.
- **Docker-Gate (für jeden Task, der `Dockerfile` oder `scripts/` ändert):**
  ```bash
  docker build --build-arg VERSION=smoke -t recipe-reader:ci .
  scripts/smoke-test-image.sh recipe-reader:ci smoke
  ```
  Erwartet: `smoke test passed: recipe-reader:ci (smoke)`
- **Commit-Footer:** Jede Commit-Message endet mit
  ```
  Assisted-by: <Modellname> (<Effort>) via Claude Code
  Co-Authored-By: <Modellname> <noreply@anthropic.com>
  ```
  Die `git commit`-Befehle in den Task-Steps lassen diesen Footer aus Platzgründen weg; er ist trotzdem für jeden Commit Pflicht.
- **Fortschritt:** Erledigte Steps und Tasks werden **in dieser Datei** abgehakt (`[x]`).

---

## Teil A: Product-Owner-Sicht

### Ausgangslage

Der kong-Umbau (Plan vom 2026-09-19, PRs #24–#27) ist fertig. Sein Backlog nennt vier Punkte, plus einen aus dem M4-Review:

1. **`server.Run` drainiert im Fehlerfall nicht.** Scheitert `newHTTPServer` oder `ListenAndServe` (z. B. Port belegt), kehrt `Run` sofort zurück. Der Pool ist dann geschlossen, aber der schon gestartete Import-Zeitplan und die Shutdown-Goroutine laufen unter `ctx` weiter, bis der Aufrufer ihn abbricht. Beide heutigen Aufrufer beenden danach den Prozess mit 1, also fällt es nicht auf — es steht seit M1 als Absatz im Doc-Kommentar.
2. **Migrationen sehen den Kontext nicht.** `db.Open` nimmt ein `ctx`, aber sein erster Schritt `Migrate(dsn)` hat keinen Kontextparameter. Ein `Ctrl-C` während einer langen Migration wird erst beim folgenden `Connect(ctx)` bemerkt — da ist die Migration schon angewandt. Mit `migrate` als Vordergrundkommando (M4) ist das erstmals etwas, das ein Betreiber im Rollout wirklich tut.
3. **Kein `--log-level` / `--log-format`.** Der Prozess loggt über den slog-Default: Textzeilen auf stderr, alles ab Info. Weder Debug im Fehlerfall noch JSON für einen Log-Collector sind einstellbar.
4. **Kein einmaliger Import.** Importieren geht nur über den laufenden Server: entweder auf dem Zeitplan (`IMPORT_INTERVAL`, Default 6 h) oder über die API. Vor einem Rollout oder nach einem Restore einmal importieren heißt heute: Server starten und die API anstoßen.

Der offene Punkt beim Import war nie das Pipeline-Wiring, sondern die Gleichzeitigkeit: Die Session-Datei liegt auf der Platte, aber der 15-Minuten-Floor zwischen zwei Logins ist **Prozess-Zustand** (`minReloginInterval` und `c.lastLogin` in `internal/instagram/client.go`). Zwei Prozesse rationieren ihre Logins also unabhängig voneinander — und wiederholte Logins sind genau das Verhalten, das Instagram markiert.

### Ziele (Outcomes)

| # | Ziel | Messbar an |
| --- | --- | --- |
| Z1 | Ein Fehlstart hinterlässt keine laufende Arbeit | `server.Run` gibt den Listen-Fehler erst zurück, nachdem seine Goroutinen zurückgekehrt sind (Test) |
| Z2 | Eine Migration lässt sich abbrechen | `db.MigrateWithContext` mit abgebrochenem Kontext wendet nichts an und meldet den Abbruch als Fehler (Test) |
| Z3 | Logging ist einstellbar, für jedes Kommando | `--log-level debug`, `--log-format json` wirken vor und hinter dem Kommandonamen |
| Z4 | Ein Import ohne Server ist möglich und kann den laufenden Server nicht stören | `recipe-reader import` läuft; zwei gleichzeitige Läufe sind unmöglich, der zweite endet mit 1 und einer klaren Meldung (Test gegen echtes Postgres) |
| Z5 | Kein Verhaltensbruch | Alle bestehenden Flags, Env-Variablen und Defaults unverändert; `serve --help` bleibt bis auf die neue Gruppe „Logging“ gleich |

### Nicht-Ziele

- Kein `--log-file`, kein Log-Rotieren, keine Log-Attribute pro Request.
- Keine Änderung an HTTP-API, Extraktion oder Datenbankschema.
- Kein Scheduler-Ersatz: `import` ersetzt nicht den Worker, es ist derselbe Lauf ohne Server.
- Keine Migration der Logaufrufe von `slog.Info(...)` auf einen injizierten `*slog.Logger`. Das Repo loggt überall über den Default; das bleibt so.

### User Stories & Akzeptanzkriterien

**US1: Betreiber startet auf einem belegten Port.** *Als Betreiber möchte ich, dass ein Fehlstart nichts im Hintergrund weiterlaufen lässt.*
- [x] `server.Run` gibt den Fehler von `ListenAndServe` zurück, nachdem es seine eigene Kontextkopie abgebrochen und auf die Shutdown-Goroutine gewartet hat.
- [x] Der Import-Zeitplan startet erst, wenn der HTTP-Server gebaut ist; scheitert `newHTTPServer`, wurde nie ein Worker gestartet.
- [x] Der Absatz im Doc-Kommentar von `Run`, der das alte Verhalten beschreibt, ist entfernt.

**US2: Betreiber bricht eine Migration ab.** *Als Betreiber möchte ich `recipe-reader migrate` mit Ctrl-C abbrechen können, ohne dass mir „fertig“ gemeldet wird.*
- [x] `db.MigrateWithContext(ctx, dsn)` bricht zwischen zwei Migrationen ab, wenn `ctx` abgebrochen wird, und meldet den Abbruch als Fehler.
- [x] Ein bereits abgebrochener Kontext wendet **keine** Migration an.
- [x] `db.Open` nutzt `MigrateWithContext`, sodass `migrate` und `serve` beide davon profitieren; `db.Migrate(dsn)` bleibt als Kurzform bestehen (Aufrufer in `testdb` und den Tests unverändert).

**US3: Betreiber will mehr oder weniger Log.** *Als Betreiber möchte ich im Fehlerfall Debug sehen und im Betrieb JSON an meinen Collector schicken.*
- [x] `recipe-reader --log-level debug serve` und `recipe-reader serve --log-level debug` tun dasselbe; `LOG_LEVEL` wirkt genauso.
- [x] `--log-format json` schreibt JSON-Records auf stderr, `text` das heutige Format.
- [x] Ein unbekannter Wert wird beim Parsen abgelehnt (Exit 1), nicht still auf den Default gesetzt.
- [x] Die Flags gelten für **jedes** Kommando, auch für `healthcheck` und `migrate`.

**US4: Betreiber importiert einmalig.** *Als Betreiber möchte ich vor einem Rollout oder nach einem Restore einmal importieren, ohne den Server zu starten.*
- [x] `recipe-reader import` führt genau einen Importlauf aus, loggt eine Zusammenzeile mit den Zählern und endet mit 0.
- [x] `import` liest nur die Einstellungen, die es braucht: Database, Instagram, Extraction, LLM und die Import-Grenzen. `--http-addr` und `--import-interval` erscheinen **nicht** in `import --help`.
- [x] Ohne `INSTAGRAM_USERNAME` endet `import` mit 1 und einer Meldung, die sagt, was fehlt.
- [x] `recipe-reader --db-dsn <dsn> import` wird abgelehnt (Entscheidung E11 des Vorgängerplans gilt weiter).

**US5: Betreiber importiert, während der Server läuft.** *Als Betreiber möchte ich nicht aus Versehen zwei Importe gleichzeitig fahren.*
- [x] Solange ein Importlauf läuft — im Server oder als Kommando —, endet ein zweiter mit 1 und der Meldung, dass bereits ein Import läuft.
- [x] Die Sperre wird freigegeben, wenn der Lauf endet, auch wenn der Prozess stirbt (Session-Lock von Postgres).

### Entscheidungen

| # | Entscheidung | Begründung |
| --- | --- | --- |
| D1 | Gleichzeitige Importe werden über einen **Postgres-Advisory-Lock** verhindert, nicht über eine Lock-Datei | Die Datenbank ist der einzige Ort, den beide Prozesse sicher teilen — die Session-Datei sagt nichts über einen laufenden Lauf, und ein `flock` schützt den Login, nicht die Pipeline. Ein Session-Lock stirbt mit der Verbindung, also auch mit einem `kill -9`; eine Lock-Datei bliebe liegen. Vom Nutzer am 2026-09-21 entschieden. **Daraus folgt, wo der Lock genommen wird:** Wenn der Login die Kosten sind, die der Lock vermeiden soll, muss die Absage **vor** dem Login kommen. `server.ImportOnce` nimmt ihn deshalb selbst, direkt nach `db.Open` und vor `newFetcher`, und gibt der Pipeline **kein** `Lock`-Feld mit — die Pipeline-Sperre läuft erst, wenn der Fetcher sich schon eingeloggt und die Session-Datei geschrieben hat. Der Worker in `server.Run` behält die Pipeline-Sperre: dort passiert der Login einmal beim Start, nicht pro Lauf. (Korrektur aus dem M3-Review, 2026-09-22.) |
| D2 | Der Lock ist ein **Port am Konsumenten** (`pipeline.ImportLock`) mit einem Adapter in `internal/db` | `internal/pipeline` kennt die Datenbank nicht und soll sie nicht kennenlernen (`go-architecture.md`: kleine Interfaces beim Konsumenten). Ein `nil`-Lock heißt „ungesperrt“, damit der Nullwert von `Pipeline` benutzbar bleibt und die bestehenden Pipeline-Tests unverändert laufen. |
| D3 | `main.go` bekommt einen dritten `internal/`-Import: `internal/logging` | Die Definition of Done des Vorgängerplans nannte `internal/cli` und `internal/config`. Der Logger gehört in die Composition Root (`go-architecture.md`), und das ist hier `run`. Der Plan ändert die Invariante ausdrücklich auf drei Importe, statt sie zu umgehen. |
| D4 | Logging wird **nach** `Parse` konfiguriert, nicht in einem kong-Hook | Ein `BeforeApply` läuft, bevor die eigenen Flagwerte sicher gesetzt sind; ein `AfterApply` würde die Reihenfolge `BeforeApply → Validate → AfterApply` (E4 des Vorgängerplans) erben und wäre nicht früher. Preis: Einen Parse- oder Validierungsfehler loggt `main` noch im Default-Format. Das ist eine Zeile vor dem Exit und keinen Hook wert. |
| D5 | `import` bekommt **kein** `--json` | Der Nutzer hat am 2026-09-21 die Log-Zeilen plus Zusammenfassung gewählt: dieselbe Ausgabe, die der Worker schon erzeugt, und mit `--log-format json` ohnehin maschinenlesbar. |
| D6 | `config.ImportLimits` wird aus `config.Import` herausgelöst | Genau das Muster, mit dem M3 `config.Listen` aus `HTTP` gelöst hat, damit `healthcheck` nur die Adresse sieht: `import` liest die Grenzen, aber nicht das Intervall. Durch das `group:"Import"`-Tag bleibt die Hilfe von `serve` unverändert. |
| D7 | Ein fehlgeschlagener Login lässt `import` scheitern, während `serve` weiterläuft | `serve` hat einen nächsten Tick, unter dem 15-Minuten-Floor, und darf deshalb mit einem kaputten Login starten (das ist der Fix aus `newFetcher`). Ein einmaliger Lauf hat keinen nächsten Tick: Ein Login-Fehler ist sein Ergebnis. |
| D8 | `import` gibt es **nicht** im `Dockerfile` als eigenen `CMD` | `docker compose run --rm app import` reicht, genau wie bei `migrate`. Das Image bleibt unverändert, also entfällt das Docker-Gate für M2 und M3. |

### Risiken

| # | Risiko | Gegenmaßnahme |
| --- | --- | --- |
| R1 | `pg_advisory_unlock` scheitert und die Verbindung geht mit gehaltenem Lock zurück in den Pool | Der Adapter nimmt die Verbindung in dem Fall per `Hijack` aus dem Pool und schließt sie; Postgres gibt den Lock mit der Session frei. Der Fall wird geloggt. |
| R2 | `golang-migrate` verhält sich anders als angenommen | Am 2026-09-21 gegen v4.19.1 geprüft: `GracefulStop` ist gepuffert (`make(chan bool, 1)`, `migrate.go:185`), `stop()` liest ihn zwischen den Migrationen (`migrate.go:815-828`), und ein gestopptes `Up()` gibt **`nil`** zurück. Deshalb prüft `MigrateWithContext` nach `Up()` zusätzlich `ctx.Err()` — sonst sähe ein abgebrochener Lauf wie ein erfolgreicher aus. |
| R3 | Der Goroutine-Test in Task 1 wird flaky | Er vergleicht nur mit seiner eigenen Grundlinie und pollt mit großzügiger Frist (15 s). Nicht Teil der Gegenmaßnahme: `testdb.Main` startet den Container **nicht** — der kommt beim ersten `New` über `shared.once.Do(start)` hoch, also innerhalb des Tests. Deshalb liegt der 100-ms-Settle zwischen diesem `NewDatabase` und der Grundlinie. Wird der Test trotzdem unruhig, hier anhalten und melden, statt die Frist immer weiter hochzudrehen. |
| R4 | Der Lock serialisiert mehr als gewollt (z. B. zwei Deployments gegen dieselbe Datenbank) | Genau das ist die Absicht: Der Lock hängt an der Datenbank, und zwei Prozesse an derselben Datenbank sind genau der Fall, den D1 verhindern soll. |

### Definition of Done

- [x] Alle Tasks in dieser Datei abgehakt.
- [x] Commit-Gate grün; Docker-Gate grün für M1 (das Image wird gebaut, auch wenn sich nichts daran ändert).
- [x] `cmd/recipe-reader/` enthält weiterhin nur `main.go` und dessen Tests; `main.go` importiert aus `internal/*` genau `internal/cli`, `internal/config` und `internal/logging` (D3).
- [x] Die Flag-Liste von `serve --help` ist die bisherige plus `--log-level` und `--log-format`.
- [x] README aktualisiert: Commands (`import`), Configuration (Logging-Gruppe), Architecture.
- [x] Der Backlog-Abschnitt im Plan vom 2026-09-19 verweist auf diesen Plan.

### Backlog (bewusst nicht in diesem Plan)

- **Automatisches GitHub-Release je Versions-Tag** (vom Nutzer am 2026-09-21 notiert). Ein Tag nach Semantic Versioning löst ein Release aus, das zwei Artefakte trägt: die Single-Binary und ein Image, das sich per `docker-compose` einsetzen lässt. Noch nicht geschnitten; der Plan dafür muss vier Fragen beantworten, bevor Code entsteht:
  - **Woher kommt die Versionsnummer?** Entweder vergibt ein Werkzeug sie aus den Conventional-Commits-Präfixen, die dieses Repo ohnehin schreibt (`feat:`, `fix:`, `refactor!:`), oder ein von Hand geschobenes Tag löst das Release aus und die Automatik baut nur. Das erste nimmt Arbeit ab und bindet die Versionierung an die Commit-Disziplin; das zweite behält die Entscheidung beim Menschen. Beides ist vertretbar, aber es muss eines sein.
  - **Wo fängt die Zählung an?** Das Repository hat **kein einziges Tag**. `git describe --tags --always --dirty` — die Quelle, aus der `Makefile` und `Dockerfile` heute `-X main.version` speisen — liefert deshalb einen nackten SHA. Das erste Tag entscheidet zugleich, ob der Stand als `v0.x` geführt wird (Breaking Changes jederzeit erlaubt) oder als `v1.0.0` (dann verpflichtet SemVer).
  - **Welche Plattformen?** Die Binary ist mit `CGO_ENABLED=0` bereits statisch und trägt das Frontend per `go:embed` in sich, ist also von Haus aus einzeln lauffähig. Offen ist nur, für welche `GOOS`/`GOARCH`-Paare gebaut wird und ob Prüfsummen beiliegen. `-trimpath` fehlt in beiden Build-Aufrufen und gehört dazu, sobald die Binary das Haus verlässt.
  - **Was heißt „package" für Compose?** Naheliegend ist ein Image in der GitHub Container Registry. Das ändert `docker-compose.yml`: der `app`-Service baut heute lokal (`build:` mit `VERSION`-Build-Arg), ein Release-Image würde per `image:` gezogen. Ob beides nebeneinander bestehen soll — lokal bauen für die Entwicklung, gezogenes Image für den Betrieb — ist Teil der Frage und berührt den Compose-Workflow, den die README beschreibt.

  Was bereits steht und nicht neu erfunden werden muss: das Stempeln der Version über `-ldflags "-X main.version=…"` in `Makefile` und `Dockerfile`, die Ausgabe über `recipe-reader --version` und das `version`-Feld von `GET /api/healthz`, sowie `scripts/smoke-test-image.sh`, das ein gebautes Image gegen eine Wegwerf-Datenbank prüft und die exakte Versionsstempelung mit verifiziert — ein Release-Workflow kann es unverändert als Gate benutzen.

- **Fehlermeldungen eindeutiger machen** (vom Nutzer am 2026-09-21 notiert). Noch nicht geschnitten: Der erste Schritt ist eine Bestandsaufnahme dessen, was ein Betreiber im Fehlerfall tatsächlich zu sehen bekommt, erst danach lässt sich sagen, ob daraus ein Task oder ein eigener Plan wird. Drei Stellen sind beim Schreiben dieses Plans aufgefallen und taugen als Einstieg:
  - `db: migrate up: context canceled` sagt nicht, was angewandt wurde. Schlimmer: ein Abbruch, der erst nach der letzten Migration eintrifft, meldet einen vollständig durchgelaufenen Lauf als Fehler (siehe den Doc-Kommentar von `migrateUp`). Die Meldung müsste sagen, dass ein erneuter Lauf gefahrlos ist und nachholt, was fehlt.
  - kong stellt der Meldung den Kommandopfad voran (`serve: config: API_TOKEN …`, R4 des Plans vom 2026-09-19). Dass der Inhalt gleich bleibt, ist geprüft; ob das Präfix einem Betreiber hilft oder im Weg steht, nie.
  - `main` loggt jeden Fehler als eine Zeile `slog.Error("fatal", "error", err)`. Die Ursache steckt damit im Attribut, während die Nachricht für alle Fehler dieselbe ist — auch für die, bei denen der Betreiber sofort wüsste, was zu tun ist.
- **Der Shutdown von `serve` hat keine obere Schranke mehr** (aus dem Milestone-Review zu M3, 2026-09-22). Seit M3 hält ein Lauf eine Pool-Verbindung über seine ganze Dauer — die Session des Advisory-Locks. Das `defer pool.Close()` in `server.Run` wartet über puddles `destructWG` auf **jede** Verbindung, auch die ausgeliehene, also auch auf einen Import, den das 10-Sekunden-Budget gerade ausdrücklich aufgegeben hat. Zwischen „import did not stop within the shutdown budget; abandoning it" und dem tatsächlichen Return liegen dann bis zu ein `LLM_TIMEOUT` (Default 60 s) ohne eine einzige Logzeile; in der Praxis beendet die Container-Runtime den Prozess per SIGKILL, was überlebbar ist (der Lock stirbt mit der Session), aber genau den geordneten Ausstieg aushebelt, den das Budget verspricht. Der Doc-Kommentar von `Run` sagt das jetzt; eine Schranke ist es nicht. Wer das aufgreift, entscheidet zwischen drei Wegen: den Lock auf einer eigenen, nicht gepoolten Verbindung halten (`pool.Acquire` durch ein `pgx.Connect` ersetzen, dann hängt `Close` nicht daran); `pool.Close()` in eine Goroutine mit eigener Frist legen und den Prozess notfalls ohne sie verlassen; oder den Abbruch bis in den Extraktor durchreichen, sodass ein aufgegebener Lauf nicht erst nach dem nächsten LLM-Timeout endet. Das Dritte behebt die Ursache, die beiden anderen die Wirkung.

- **`.env.example` und `docker-compose.yml` laufen auseinander** (aus dem Milestone-Review zu M2). Der `app`-Service reicht genau die Variablen durch, die unter `environment:` stehen; ein `env_file:` gibt es nirgends. Dreizehn Einträge aus `.env.example` erreichen den Container daher nicht — `EXTRACTION_*`, `IMPORT_*`, `LLM_*`, `ANTHROPIC_MODEL`, `INSTAGRAM_SESSION_PATH` und seit M2 auch `LOG_LEVEL`/`LOG_FORMAT` —, obwohl die README-Anleitung mit `cp .env.example .env` beginnt. Das ist keine Lücke von M2, sondern die allgemeine: entweder ein `env_file:`, oder eine bewusst dokumentierte Auswahl. Wer sie schließt, beachtet, dass `${VAR:-}` die Variable **leer** setzt und kong einen leeren Wert bei einem `enum`-Flag ablehnt (`--log-level must be one of … but got ""`, am 2026-09-21 geprüft); in die Substitution gehört also der Default, nicht der leere String — und damit steht der Default an einer zweiten Stelle, die driften kann.

---

## Teil B: Zielbild

### CLI-Oberfläche

| Vorher | Nachher |
| --- | --- |
| `recipe-reader [serve] [flags]` | unverändert, plus `--log-level`, `--log-format` |
| `recipe-reader healthcheck` | unverändert, plus die beiden Log-Flags |
| `recipe-reader migrate` | unverändert, plus die beiden Log-Flags; abbrechbar |
| — | `recipe-reader import [--db-dsn ...] [--instagram-username ...] [--import-max-items ...]` |

### Dateistruktur nach Abschluss

```
cmd/recipe-reader/
  main.go                  # + Logging-Gruppe im Baum, + logging.Configure nach Parse, + Import-Kommando
  main_test.go, migrate_test.go, testmain_test.go
internal/logging/          # neu
  logging.go               # NewHandler + Configure
  logging_test.go
internal/cli/
  serve.go, healthcheck.go, migrate.go
  import.go                # neu: ImportCmd → server.ImportOnce
internal/server/
  server.go                # Run drainiert auch im Fehlerfall; Worker bekommt den Lock
  import.go                # neu: ImportOnce
  extractor.go, http.go, fetcher.go, *_test.go
internal/pipeline/
  pipeline.go              # + ImportLock-Port, + Lock-Feld, + ErrImportInProgress
  lock_test.go             # neu
internal/db/
  connect.go               # + MigrateWithContext; Migrate delegiert; Open nutzt es
  migrate_test.go          # neu: treibt das paketinterne migrateUp mit einem Fake
  lock.go                  # neu: ImportLock (Adapter)
  lock_test.go             # neu
internal/config/
  config.go                # + Logging-Gruppe, + ImportLimits aus Import herausgelöst
```

---

## Teil C: Aufgaben

### Milestones

| Milestone | Tasks | Ergebnis | Für Betreiber sichtbar |
| --- | --- | --- | --- |
| **M1:** Laufzeit-Härtung | Task 1, Task 2 | Fehlstart hinterlässt nichts; Migration ist abbrechbar | nein (außer: Ctrl-C wirkt) |
| **M2:** Log-Flags | Task 3 | `--log-level`, `--log-format` für jedes Kommando | ja |
| **M3:** Einmaliger Import | Task 4, Task 5, Task 6 | `import`-Kommando mit prozessübergreifender Sperre | ja |

Jeder Milestone endet in einem releasefähigen Stand und bekommt einen eigenen PR. Der nächste Milestone beginnt nach dem Merge des vorigen (oder gestapelt auf ausdrücklichen Wunsch).

#### M1: Laufzeit-Härtung

Abnahmekriterien:
- [x] Die Akzeptanzkriterien von US1 und US2 sind abgehakt.
- [x] `go test -count=1 -race ./internal/server/ ./internal/db/` ist grün.
- [x] Das Docker-Gate ist grün (das Image wird unverändert gebaut und smoke-getestet).

#### M2: Log-Flags

Abnahmekriterien:
- [x] Die Akzeptanzkriterien von US3 sind abgehakt.
- [x] `serve --help` zeigt die Gruppe „Logging“ mit beiden Flags und ihren Env-Namen; `healthcheck --help` und `migrate --help` ebenso.
- [x] `.env.example` nennt `LOG_LEVEL` und `LOG_FORMAT` mit ihren Defaults und den erlaubten Werten.
- [x] Keine bestehende Env-Variable wurde umbenannt.

#### M3: Einmaliger Import

Abnahmekriterien:
- [x] Die Akzeptanzkriterien von US4 und US5 sind abgehakt.
- [x] `import --help` zeigt weder `--http-addr` noch `--import-interval`.
- [x] Die Sperre ist gegen echtes Postgres getestet, nicht gegen einen Fake.
- [x] Die Definition of Done in Teil A ist abgehakt.

#### Branches und PRs

| Milestone | Branch | PR-Titel |
| --- | --- | --- |
| M1 | `fix/server-drain-and-migration-cancel` | `fix: drain on a failed start and let migrations be cancelled (1/3)` |
| M2 | `feat/log-flags` | `feat(cli): add --log-level and --log-format (2/3)` |
| M3 | `feat/import-command` | `feat(cli): add a one-off import command (3/3)` |

- **Abzweigen:** Jeder Milestone-Branch zweigt von `main` ab, nachdem der vorige PR gemerged ist. Der erste zweigt von dem `main` ab, das PR #27 enthält.
- **Inhalt:** Die Task-Commits des Milestones, die Korrektur-Commits aus dem Milestone-Review und ein letzter Commit `docs: mark M<n> done in the CLI backlog plan`.
- **Beschreibung:** Ergebnis, abgehakte Abnahmekriterien, Link auf diesen Plan, `Assisted-by`-Footer und `🤖 Generated with [Claude Code](https://claude.com/claude-code)`.

### Aufgabenliste

- [x] **M1: Laufzeit-Härtung**
  - [x] **Task 1:** `server.Run` drainiert auch im Fehlerfall (S)
  - [x] **Task 2:** Migrationen beachten den Kontext (S)
  - [x] **Milestone-Review:** Review durch einen neuen Subagent; Befunde von einem weiteren Subagent bewertet und abgearbeitet
- [x] **M2: Log-Flags**
  - [x] **Task 3:** `--log-level` und `--log-format` (M)
  - [x] **Milestone-Review**
- [x] **M3: Einmaliger Import**
  - [x] **Task 4:** Prozessübergreifende Import-Sperre (M)
  - [x] **Task 5:** `config.ImportLimits` aus `config.Import` lösen (S)
  - [x] **Task 6:** Kommando `import` (M)
  - [x] **Milestone-Review**
  - [x] **Abschluss-Verifikation**

### Ablauf der Umsetzung

Wie im Vorgängerplan: ein frischer Subagent pro Task, strikt nacheinander; nach jedem Task ein zweistufiges Review (Plan-Treue, Code-Qualität); nach jedem Milestone ein Milestone-Review durch einen neuen Subagent, dessen Befunde ein weiterer Subagent bewertet und abarbeitet. Befunde, die dem Plan widersprechen, werden dem Nutzer vorgelegt. Genau ein Commit pro Task, erst nach bestandenem Review. Der Auftrag an einen Subagent ist der Task-Abschnitt samt Files- und Interfaces-Block, dazu die *Global Constraints*, die Tabelle *Entscheidungen* (D1–D8) und *Risiken* (R1–R4).

---

### Task 1: `server.Run` drainiert auch im Fehlerfall

Heute kehrt `Run` bei einem Listen-Fehler sofort zurück und lässt die Shutdown-Goroutine sowie den Import-Zeitplan unter `ctx` zurück. Nach diesem Task besitzt `Run` seine eigene Kontextkopie, startet den Zeitplan erst, wenn der HTTP-Server steht, und wartet im Fehlerpfad auf die Shutdown-Goroutine.

**Files:**
- Modify: `internal/server/server.go` (Doc-Kommentar, Kontextkopie, Reihenfolge, Fehlerpfad)
- Modify: `internal/server/run_test.go` (+ `TestRun_ListenFailureLeavesNoWorkBehind`)

**Interfaces:**
- Consumes: `db.Open` (unverändert), `pipeline.Worker` (unverändert)
- Produces: `func server.Run(ctx context.Context, cfg config.Config, version string) error` — Signatur unverändert, Zusage stärker: Bei jedem Rückgabeweg sind die Goroutinen, die `Run` gestartet hat, zurückgekehrt.

- [x] **Step 1: Failing Test schreiben** (an `internal/server/run_test.go` anhängen)

Der Kontext ist `t.Context()` und nicht `context.WithCancel(context.Background())`: `go fix ./...` — laut Global Constraints Pflicht vor jedem Commit — schreibt die zweite Form in Tests automatisch in die erste um. Wer sie „zurückkorrigiert", bekommt sie beim nächsten Gate-Lauf wieder. `t.Context()` wird erst abgebrochen, wenn die Testfunktion zurückkehrt, also nach der Zählung; für diesen Test ist das dasselbe wie ein nie abgebrochener Kontext.

```go
// A failed start used to leave work behind: Run returned the listen error while
// the shutdown goroutine sat on <-ctx.Done() and the import schedule kept
// ticking, both under a context only the caller could cancel. Both callers
// exited the process straight after, which is why it never showed. Nothing in
// this test cancels the context — t.Context() ends only once the test returns —
// so anything Run leaves running is still running while the count is taken, and
// the goroutine count says so.
func TestRun_ListenFailureLeavesNoWorkBehind(t *testing.T) {
	cfg := baseConfig("rule")
	cfg.Database.DSN = testdb.NewDatabase(t, "server_listen_fail")
	// An account with no password: instago refuses the startup login before
	// any request, so the worker exists without the test touching the
	// network. Import.Interval is set because a zero one leaves the
	// schedule unstarted, and an unstarted schedule can't leave anything
	// behind for this test to catch.
	cfg.Instagram.Username = "someone"
	cfg.Instagram.SessionPath = filepath.Join(t.TempDir(), "session.json")
	cfg.Import.Interval = time.Hour

	// Hold the port so ListenAndServe cannot have it.
	held, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = held.Close() }()
	cfg.HTTP.Addr = held.Addr().String()

	ctx := t.Context()

	// Let the package's container and pool settle before counting.
	time.Sleep(100 * time.Millisecond)
	before := runtime.NumGoroutine()

	if err := Run(ctx, cfg, "v0.0.0-test"); err == nil {
		t.Fatal("Run() = nil, want the error from a port that is already bound")
	}

	deadline := time.Now().Add(15 * time.Second)
	for runtime.NumGoroutine() > before {
		if time.Now().After(deadline) {
			t.Fatalf("Run() returned with %d goroutines still running above the %d it started with",
				runtime.NumGoroutine()-before, before)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
```

Die Imports `net`, `runtime` und `time` in `run_test.go` ergänzen, falls sie fehlen.

- [x] **Step 2: Test laufen lassen, er muss fehlschlagen**

Run: `go test -count=1 -run TestRun_ListenFailureLeavesNoWorkBehind ./internal/server/`
Expected: FAIL mit „Run() returned with 1 goroutines still running above the N it started with“ — die Shutdown-Goroutine hängt an `<-ctx.Done()`.

- [x] **Step 3: `Run` umbauen** (`internal/server/server.go`)

Den Doc-Kommentar ersetzen. Alt:

```go
// Run serves until ctx is cancelled, then drains in-flight requests and any
// running import before it returns. version is logged at startup and reported
// by GET /api/healthz.
//
// An error return skips that drain. If the HTTP server cannot be built or
// cannot listen, Run returns at once with the pool closed, while the import
// schedule it may already have started keeps running under ctx, and so does
// the goroutine waiting to shut the server down. Both stop when ctx is
// cancelled, so a caller that carries on after an error rather than exiting
// cancels ctx first.
```

Neu:

```go
// Run serves until ctx is cancelled, then drains in-flight requests and any
// running import before it returns. version is logged at startup and reported
// by GET /api/healthz.
//
// An error returns the same way: Run works on its own cancellable copy of ctx,
// so whatever it started is stopped and waited for before it returns. A caller
// that carries on after an error rather than exiting is left with nothing of
// Run's still running — with one logged exception: if the ten-second shutdown
// budget runs out, the import is abandoned rather than waited for, and Run
// returns while it is still going.
//
// "Returns" is approximate in that one case, and deliberately so rather than by
// oversight. The budget bounds how long Run waits for the import, not how long
// Run takes: the deferred pool.Close below waits for every pooled connection to
// come back, and a run holds one — the import lock's session — for its whole
// length, so an abandoned import keeps Run inside that Close until it reaches
// its next per-post cancellation check, up to one LLM_TIMEOUT past the budget
// and with no log line saying why. The process usually gets SIGKILLed instead,
// which is survivable (the advisory lock dies with the session) but is not the
// orderly exit the budget promises. Bounding it is in the plan's backlog.
```

Am Anfang des Rumpfes, direkt vor `newExtractor`, die eigene Kontextkopie einziehen:

```go
	// Run's own handle on cancellation: the caller's ctx still stops it, and a
	// failure below can stop it too, without reaching back into the caller's.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
```

`worker.Start(ctx)` aus dem `if fetcher != nil`-Block **entfernen** (die Zeile direkt nach dem `RecordFailure`-Block) und stattdessen nach dem erfolgreichen `newHTTPServer` einsetzen:

```go
	srv, err := newHTTPServer(cfg, api.Deps{Recipes: recipes, Lookups: lookups, Worker: worker, Version: version})
	if err != nil {
		return err
	}

	// Started only now: a server that cannot be built is a startup failure, and
	// a schedule started before it would be work nobody is going to serve.
	if worker != nil {
		worker.Start(ctx)
	}
```

Den Fehlerpfad von `ListenAndServe` ersetzen. Alt:

```go
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	<-shutdownDone
	return nil
```

Neu:

```go
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		// The shutdown goroutine is waiting on ctx, and the schedule is running
		// under it. Cancelling is what releases both; waiting is what makes this
		// return mean "nothing of mine is still running".
		cancel()
		<-shutdownDone
		return err
	}
	<-shutdownDone
	return nil
```

- [x] **Step 4: Tests laufen lassen, sie müssen grün sein**

Run: `go test -count=1 -race ./internal/server/`
Expected: PASS, `TestRun_ServesUntilCancelledThenReturnsNil` eingeschlossen.

- [x] **Step 5: Commit-Gate und Commit**

```bash
git add internal/server
git commit -m "fix(server): drain before returning a failed start" -m "Run works on its own cancellable copy of ctx, starts the import schedule only once the HTTP server is built, and waits for the shutdown goroutine before returning a listen error. The work it started no longer outlives the call."
```

(Footer laut Global Constraints anhängen.)

---

### Task 2: Migrationen beachten den Kontext

`Migrate` bekommt eine Kontextvariante. `golang-migrate` kennt keinen Kontextparameter, wohl aber den Kanal `GracefulStop`, der zwischen zwei Migrationen gelesen wird.

**Files:**
- Modify: `internal/db/connect.go` (+ `MigrateWithContext`, `Migrate` delegiert, `Open` nutzt es)
- Modify: `internal/db/db_test.go` (+ `TestMigrateWithContext_CancelledContextAppliesNothing`)
- Create: `internal/db/migrate_test.go` (Paket `db`: der Fake und `TestMigrateUp_GracefulStopIsReportedAsCancellation` — `migrateUp` ist paketintern und aus `db_test` nicht erreichbar)

**Interfaces:**
- Consumes: `newMigrate` (unverändert, paketintern)
- Produces: `func db.MigrateWithContext(ctx context.Context, dsn string) error`; `func db.Migrate(dsn string) error` bleibt bestehen und ruft `MigrateWithContext(context.Background(), dsn)`.

- [x] **Step 1: Failing Test schreiben** (an `internal/db/db_test.go` anhängen)

```go
// Up has no context parameter, so a cancelled migrate used to be noticed only
// by the Connect that followed it — after the schema had already changed. A
// context that is already done must leave the database exactly as it was.
func TestMigrateWithContext_CancelledContextAppliesNothing(t *testing.T) {
	dsn := testdb.NewDatabase(t, "migrate_cancelled")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := db.MigrateWithContext(ctx, dsn)
	if err == nil {
		t.Fatal("MigrateWithContext() = nil, want the cancellation reported")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("MigrateWithContext() error = %v, want it to wrap context.Canceled", err)
	}

	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("pgxpool.New() error = %v", err)
	}
	defer pool.Close()
	var exists bool
	if err := pool.QueryRow(context.Background(),
		"SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'units')").Scan(&exists); err != nil {
		t.Fatalf("look for the units table: %v", err)
	}
	if exists {
		t.Error("MigrateWithContext() applied a migration although its context was already cancelled")
	}
}

// The rest of this block is internal/db/migrate_test.go (package db): migrateUp
// is unexported, so the test that drives it cannot live in package db_test.

// stopWait bounds the fake's Up. A migrateUp that never stops its migrator
// would otherwise block here for as long as the suite is willing to wait; this
// turns that into a failed assertion with a name on it.
const stopWait = 5 * time.Second

// errNeverStopped is what the fake reports when the cancellation never reached
// it, which is the failure the AfterFunc in migrateUp exists to prevent.
var errNeverStopped = errors.New("Stop was never called")

// stoppingMigrator is golang-migrate's graceful stop with the migration taken
// out: Up blocks until Stop is called and then returns nil, exactly as a
// stopped *migrate.Migrate does. The embedded migrations run far too fast to
// interrupt on purpose, so the window a real Ctrl-C would land in is made here
// instead — Up cancels the context itself, which is why the cancellation always
// arrives while Up is in flight and never before or after it.
type stoppingMigrator struct {
	cancel  context.CancelFunc
	stopped chan struct{}
}

func (m *stoppingMigrator) Up() error {
	m.cancel()
	select {
	case <-m.stopped:
		return nil
	case <-time.After(stopWait):
		return errNeverStopped
	}
}

// Stop closes rather than signals: context.AfterFunc runs its function at most
// once, so a second close would be a bug worth the panic.
func (m *stoppingMigrator) Stop() { close(m.stopped) }

func (m *stoppingMigrator) Close() (source error, database error) { return nil, nil }

// The promise MigrateWithContext adds to golang-migrate is that a cancelled run is
// reported as one. A gracefully stopped Up returns nil, so a run that was cut
// short after applying part of the schema would otherwise be indistinguishable
// from a clean one — and the caller would carry on against a half-migrated
// database. Deleting either the AfterFunc or the ctx.Err() check after Up
// fails this test.
func TestMigrateUp_GracefulStopIsReportedAsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	m := &stoppingMigrator{cancel: cancel, stopped: make(chan struct{})}

	err := migrateUp(ctx, func() (migrator, error) { return m, nil })
	if err == nil {
		t.Fatal("migrateUp() = nil, want the cancellation reported although Up returned nil")
	}
	if errors.Is(err, errNeverStopped) {
		t.Fatalf("migrateUp() error = %v: the cancellation never reached the migrator", err)
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("migrateUp() error = %v, want it to wrap context.Canceled", err)
	}
}
```

Die Imports `errors`, `context` und `github.com/jackc/pgx/v5/pgxpool` in `db_test.go` ergänzen, falls sie fehlen.

- [x] **Step 2: Test laufen lassen, er muss fehlschlagen**

Run: `go test -count=1 -run TestMigrateWithContext ./internal/db/`
Expected: FAIL, `undefined: db.MigrateWithContext`

- [x] **Step 3: `MigrateWithContext` implementieren** (`internal/db/connect.go`)

`Migrate` ersetzen durch:

```go
// Migrate applies all pending embedded migrations against dsn (a
// postgres:// URL — the same one passed to Connect).
func Migrate(dsn string) error {
	return MigrateWithContext(context.Background(), dsn)
}

// MigrateWithContext is Migrate, bounded by ctx. Cancelling it stops the migrator
// between two migrations; the one in flight is always finished, because a
// half-applied migration is worse than a slow Ctrl-C.
//
// golang-migrate has no context parameter, only the GracefulStop channel, so
// the cancellation path is this file's own — see migrateUp, which holds it.
func MigrateWithContext(ctx context.Context, dsn string) error {
	return migrateUp(ctx, func() (migrator, error) {
		m, err := newMigrate(dsn)
		if err != nil {
			return nil, err
		}
		return gracefulMigrator{m}, nil
	})
}

// migrator is the part of *migrate.Migrate that the cancellation path drives.
// It exists so that path — the only thing this file adds to golang-migrate —
// can be tested without a migration slow enough to interrupt.
type migrator interface {
	Up() error
	// Stop asks the migrator to stop before it starts the next migration.
	Stop()
	Close() (source error, database error)
}

// gracefulMigrator adapts *migrate.Migrate to migrator. GracefulStop is
// buffered with room for one (migrate.go:185) and read between migrations, so
// the send never blocks and at most one ever happens.
//
// This one statement is deliberately the uncovered line of the cancellation
// path. Reaching it from a test needs a migration slow enough to interrupt,
// and golang-migrate reads and writes its isGracefulStop flag from two
// goroutines without synchronization (migrate.go:71, :549, :726, :816), so a
// test that drove the real migrator to a graceful stop would be liable to
// report a race inside the dependency rather than a bug here. In production
// that race is harmless — the buffered channel is the authoritative signal,
// and a stale read costs at most one further migration before the stop takes.
// migrate_test.go's fake stands in for this instead.
type gracefulMigrator struct{ *migrate.Migrate }

func (g gracefulMigrator) Stop() { g.GracefulStop <- true }

// migrateUp runs the migrator open returns, bounded by ctx. The three guards
// are the whole point: the first refuses to open anything under a context that
// is already done, the AfterFunc turns a later cancellation into a stop between
// two migrations, and the last one reports it — a stopped Up returns nil rather
// than an error, so without it a cancelled, partly applied run would look like
// a successful one.
//
// That last guard is deliberately blunt: a cancellation arriving while the
// final migration runs, or after Up has already returned, reports a run that
// in fact applied everything as a failure. golang-migrate keeps its
// isGracefulStop flag unexported and offers no way to ask whether the stop
// actually took, so the two cases cannot be told apart from here. Re-running
// is the remedy — ErrNoChange makes the retry a success.
//
// open is a parameter rather than a package-level variable so a test can hand
// in a fake migrator without the package growing mutable state to swap.
func migrateUp(ctx context.Context, open func() (migrator, error)) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("db: migrate up: %w", err)
	}
	m, err := open()
	if err != nil {
		return err
	}
	defer func() { _, _ = m.Close() }()

	stop := context.AfterFunc(ctx, m.Stop)
	defer stop()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("db: migrate up: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("db: migrate up: %w", err)
	}
	return nil
}
```

- [x] **Step 4: `Open` auf `MigrateWithContext` umstellen** (`internal/db/connect.go`)

In `Open` ersetzen:

```go
	if err := Migrate(dsn); err != nil {
```

durch

```go
	if err := MigrateWithContext(ctx, dsn); err != nil {
```

- [x] **Step 5: Tests laufen lassen, sie müssen grün sein**

Run: `go test -count=1 -race ./internal/db/ ./internal/server/ ./cmd/recipe-reader/`
Expected: PASS. `TestOpen_MigratesAndSeedsAnEmptyDatabase` und `TestRun_MigrateMigratesAndSeedsAnEmptyDatabase` decken den nicht abgebrochenen Weg ab.

- [x] **Step 6: Commit-Gate und Commit**

```bash
git add internal/db
git commit -m "feat(db): let a migration be cancelled" -m "MigrateWithContext wires ctx to golang-migrate's GracefulStop and reports the cancellation, which a stopped Up does not. Open uses it, so Ctrl-C during recipe-reader migrate stops between migrations instead of being noticed by the connect that follows."
```

---

### Task 3: `--log-level` und `--log-format`

**Files:**
- Create: `internal/logging/logging.go`, `internal/logging/logging_test.go`
- Modify: `internal/config/config.go` (+ `Logging`-Gruppe, + Eintrag in `Groups()`)
- Modify: `cmd/recipe-reader/main.go` (Gruppe im Baum, `logging.Configure` nach `Parse`)
- Modify: `cmd/recipe-reader/main_test.go` (+ zwei Parse-Tests)
- Modify: `README.md` (Configuration)
- Modify: `.env.example` (+ `LOG_LEVEL`, `LOG_FORMAT`)

**Interfaces:**
- Consumes: `config.Groups()` (Task-fremd, unverändert), `run`/`parse` aus `main.go`
- Produces: `type config.Logging struct{ Level, Format string }`; `func logging.NewHandler(w io.Writer, level, format string) (slog.Handler, error)`; `func logging.Configure(level, format string) error`

- [x] **Step 1: Failing Test für den Handler** (`internal/logging/logging_test.go`)

```go
package logging_test

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"log/slog"
	"strings"
	"testing"

	"github.com/sBurmester/recipe-reader/internal/logging"
)

// A collector reads the json format as newline-delimited JSON: one record per
// line, each line an object of its own. Two records, so that a handler which
// ran them together into one stream fails here and not in the collector.
func TestNewHandler_JSONWritesOneObjectPerRecord(t *testing.T) {
	var buf bytes.Buffer
	h, err := logging.NewHandler(&buf, "info", "json")
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}

	logger := slog.New(h)
	logger.Info("hello", "count", 3)
	logger.Warn("world")

	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("output is %d lines, want one per record:\n%s", len(lines), buf.String())
	}
	records := make([]map[string]any, len(lines))
	for i, line := range lines {
		if err := json.Unmarshal([]byte(line), &records[i]); err != nil {
			t.Fatalf("line %d is not a JSON object: %v (%q)", i+1, err, line)
		}
	}
	if records[0]["msg"] != "hello" || records[0]["level"] != "INFO" {
		t.Errorf("first record = %v, want msg hello at level INFO", records[0])
	}
	if records[1]["msg"] != "world" || records[1]["level"] != "WARN" {
		t.Errorf("second record = %v, want msg world at level WARN", records[1])
	}
}

func TestNewHandler_TextIsTheDefaultFormat(t *testing.T) {
	var buf bytes.Buffer
	h, err := logging.NewHandler(&buf, "info", "text")
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}

	slog.New(h).Info("hello", "count", 3)

	if got := buf.String(); !strings.Contains(got, "msg=hello") || !strings.Contains(got, "count=3") {
		t.Errorf("text output = %q, want it to carry msg and the attribute", got)
	}
}

// The level is the reason the flag exists: below it, a record must not cost
// anything, which is what Enabled reports.
func TestNewHandler_LevelSilencesWhatIsBelowIt(t *testing.T) {
	var buf bytes.Buffer
	h, err := logging.NewHandler(&buf, "warn", "text")
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}

	if h.Enabled(context.Background(), slog.LevelInfo) {
		t.Error("Enabled(Info) = true at level warn, want false")
	}
	if !h.Enabled(context.Background(), slog.LevelError) {
		t.Error("Enabled(Error) = false at level warn, want true")
	}

	logger := slog.New(h)
	logger.Info("hello")
	if got := buf.String(); got != "" {
		t.Errorf("output after Info at level warn = %q, want empty", got)
	}

	logger.Error("world")
	if got := buf.String(); got == "" {
		t.Error("output after Error at level warn = \"\", want a record")
	}
}

func TestNewHandler_RejectsWhatItCannotBuild(t *testing.T) {
	for _, tc := range []struct{ name, level, format string }{
		{"unknown level", "banana", "text"},
		{"unknown format", "info", "banana"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := logging.NewHandler(&bytes.Buffer{}, tc.level, tc.format); err == nil {
				t.Fatalf("NewHandler(%q, %q) = nil error, want one", tc.level, tc.format)
			}
		})
	}
}
```

(`encoding/json/v2` ist ab Go 1.27 in der Standardbibliothek und in diesem Paket neu, also ohne Mischung mit v1.)

- [x] **Step 2: Test laufen lassen, er muss fehlschlagen**

Run: `go test ./internal/logging/`
Expected: FAIL, das Paket existiert noch nicht.

- [x] **Step 3: `internal/logging/logging.go` anlegen**

```go
// Package logging builds the process-wide slog handler from the --log-level
// and --log-format flags.
//
// Every package in this repo logs through slog's default logger, so there is
// one handler for the process and Configure installs it. That is a deliberate
// global: injecting a *slog.Logger into every constructor would be a larger
// change than the two flags are worth, and slog's default exists for exactly
// this shape of program.
package logging

import (
	"fmt"
	"io"
	"log/slog"
	"os"
)

// NewHandler builds the handler for level and format, writing to w. level is
// one of debug, info, warn, error and format is text or json; kong's enum tags
// reject anything else at parse time, and this rejects it again for callers
// that do not come through the command line.
func NewHandler(w io.Writer, level, format string) (slog.Handler, error) {
	var lvl slog.Level
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "info":
		lvl = slog.LevelInfo
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		return nil, fmt.Errorf("logging: unknown level %q", level)
	}

	opts := &slog.HandlerOptions{Level: lvl}
	switch format {
	case "text":
		return slog.NewTextHandler(w, opts), nil
	case "json":
		return slog.NewJSONHandler(w, opts), nil
	default:
		return nil, fmt.Errorf("logging: unknown format %q", format)
	}
}

// Configure installs the handler for level and format as slog's default, so
// every package that logs picks it up. Logs go to stderr, where they already
// went: stdout belongs to whatever a command prints for a human to read.
func Configure(level, format string) error {
	h, err := NewHandler(os.Stderr, level, format)
	if err != nil {
		return err
	}
	slog.SetDefault(slog.New(h))
	return nil
}
```

- [x] **Step 4: Test laufen lassen, er muss grün sein**

Run: `go test ./internal/logging/`
Expected: PASS

- [x] **Step 5: Die Gruppe in `internal/config/config.go` deklarieren**

Nach der `Listen`-Gruppe einfügen:

```go
// Logging is how the process writes its own logs. It is a group of its own,
// declared on the root of the command tree rather than in Config, because every
// command logs — healthcheck and migrate included — while Config is what serve
// takes.
type Logging struct {
	Level  string `name:"log-level" env:"LOG_LEVEL" default:"info" enum:"debug,info,warn,error" help:"Lowest level that is logged: debug, info, warn, error."`
	Format string `name:"log-format" env:"LOG_FORMAT" default:"text" enum:"text,json" help:"Log output format: text for a human, json for a log collector."`
}
```

In `Groups()` als letzten Eintrag ergänzen:

```go
		{Key: "Logging", Title: "Logging", Description: "Applies to every command, and may be given before or after the command name."},
```

- [x] **Step 6: Failing Parse-Tests** (an `cmd/recipe-reader/main_test.go` anhängen)

```go
// The log flags sit on the root of the tree, so they are the one kind of flag
// that may precede a command name: rejectMisplacedFlags allows the root's own
// flags everywhere, and an operator typing `recipe-reader --log-level debug
// migrate` is doing the obvious thing.
func TestParse_LogFlagsAreAllowedBeforeTheCommand(t *testing.T) {
	clearEnv(t)

	root, kctx, err := parse(t, "--log-level", "debug", "--log-format", "json", "migrate")
	if err != nil {
		t.Fatalf("parse() error = %v, want the root flags accepted before the command", err)
	}
	if got := kctx.Command(); got != "migrate" {
		t.Errorf("Command() = %q, want migrate", got)
	}
	if root.Logging.Level != "debug" || root.Logging.Format != "json" {
		t.Errorf("Logging = %+v, want debug/json", root.Logging)
	}
}

func TestParse_RejectsAnUnknownLogLevel(t *testing.T) {
	clearEnv(t)

	if _, _, err := parse(t, "--log-level", "banana", "migrate"); err == nil {
		t.Fatal("parse(--log-level banana) = nil error, want the enum to reject it")
	}
}
```

- [x] **Step 7: Tests laufen lassen, sie müssen fehlschlagen**

Run: `go test -count=1 -run 'TestParse_LogFlags|TestParse_RejectsAnUnknownLogLevel' ./cmd/recipe-reader/`
Expected: FAIL. Der erste Test liest `root.Logging`, das es noch nicht gibt, also scheitert schon die Übersetzung (`root.Logging undefined`) und nicht erst kong mit `unknown flag --log-level`. Rot ist rot; wer die Meldung erwartet und die andere bekommt, hat trotzdem den Beweis, dass der Test ohne Step 8 nicht durchläuft.

- [x] **Step 8: `main.go` umstellen**

Im `CLI`-Struct vor `Version` einfügen:

```go
	// Logging applies to every command, so it sits on the root rather than in
	// any one command's flags.
	Logging config.Logging `embed:"" group:"Logging"`
```

In `run`, zwischen `parser.Parse` und `kctx.Run()`:

```go
	kctx, err := parser.Parse(args)
	if err != nil {
		return err
	}
	// After Parse, because the flags that say how to log are only known once
	// they are parsed. A command line kong rejects is therefore still reported
	// in the default format — one line, and then the process is over.
	if err := logging.Configure(root.Logging.Level, root.Logging.Format); err != nil {
		return err
	}
	return kctx.Run()
```

Den Import `"github.com/sBurmester/recipe-reader/internal/logging"` ergänzen (alphabetisch nach `.../internal/config`).

- [x] **Step 9: Tests laufen lassen, sie müssen grün sein**

Run: `go build ./... && go test -count=1 -race ./cmd/recipe-reader/ ./internal/config/ ./internal/logging/`
Expected: PASS

Zusätzlich von Hand prüfen:

```bash
go run ./cmd/recipe-reader serve --help | grep -A3 'Logging'
go run ./cmd/recipe-reader healthcheck --help | grep -c 'log-level'   # 1
LOG_FORMAT=json go run ./cmd/recipe-reader migrate --db-dsn 'postgres://nobody@127.0.0.1:1/nope' 2>&1 | tail -2
```

Die Fehlerzeile muss ein JSON-Record sein — der Beleg, dass `Configure` vor dem Kommando greift. `tail -2`, weil `go run` bei einem Exit-Status ungleich null noch eine eigene Zeile anhängt; gegen eine gebaute Binary reicht `tail -1`.

Das `--db-dsn` steht **hinter** dem Kommandonamen, und das ist keine Stilfrage: davor wäre es genau die Fehlstellung, die `rejectMisplacedFlags` nach E11 des Vorgängerplans ablehnt. Der Lauf stürbe dann schon im Parsen und läge damit vor `logging.Configure` — die Ausgabe wäre Text, und die Prüfung würde das Gegenteil dessen belegen, wofür sie da ist. Wer sie scheitern sieht, korrigiert den Aufruf und nicht den Guard.

- [x] **Step 10: README und `.env.example`**

In `## Configuration` die neue Gruppe dokumentieren, im Stil der bestehenden Tabellen: `LOG_LEVEL` (`--log-level`, Default `info`, Werte `debug,info,warn,error`) und `LOG_FORMAT` (`--log-format`, Default `text`, Werte `text,json`), mit dem Hinweis, dass beide für jedes Kommando gelten und vor oder hinter dem Kommandonamen stehen dürfen.

In `.env.example` die beiden Variablen nachtragen. Die Datei führt jede Variable **unkommentiert** mit ihrem Default und gruppiert sie thematisch durch Leerzeilen; ein Kommentar steht nur dort, wo ein Wert eine Erklärung braucht (siehe `DB_DSN`). Als eigener Block ans Ende, weil Logging keine der bestehenden Gruppen ist:

```dotenv
# Applies to every command. Levels: debug, info, warn, error. Formats: text, json.
LOG_LEVEL=info
LOG_FORMAT=text
```

Vor dem Bearbeiten den aktuellen Stand ansehen, damit Reihenfolge und Kommentarstil passen: `cat .env.example`.

- [x] **Step 11: Commit-Gate und Commit**

```bash
git add cmd internal README.md .env.example
git commit -m "feat(cli): add --log-level and --log-format" -m "Both sit on the root of the command tree, so they apply to every command and may precede the command name. internal/logging builds the handler; run installs it after parsing."
```

---

### Task 4: Prozessübergreifende Import-Sperre

Ein Importlauf darf nicht zweimal gleichzeitig laufen — nicht im selben Prozess (das verhindert `Worker.RunOnce` schon) und nicht in zweien. Der Port liegt beim Konsumenten, der Adapter in `internal/db` (D2).

Die Sperre in `Pipeline.Run` ist die des **Worker-Pfads**. Der einmalige Lauf nimmt den Lock eine Ebene höher, in `ImportOnce` vor `newFetcher` (D1, Task 6): ein Lauf, der erst nach dem Login abgelehnt wird, hat genau das getan, was der Lock verhindern soll. Beide Pfade nehmen denselben Lock, aber nie beide in einem Prozess — zwei Verbindungen aus demselben Pool bekämen ihn nicht.

**Files:**
- Modify: `internal/pipeline/pipeline.go` (+ `ImportLock`, + `ErrImportInProgress`, + `Lock`-Feld, + Sperre am Anfang von `Run`)
- Create: `internal/pipeline/lock_test.go`
- Create: `internal/db/lock.go`, `internal/db/lock_test.go`
- Modify: `internal/server/server.go` (der Worker-Pipeline den Adapter mitgeben)

**Interfaces:**
- Consumes: `pipeline.Pipeline.Run` (unverändert in der Signatur), `db.Connect`/`Open`
- Produces:
  - `type pipeline.ImportLock interface { TryAcquire(ctx context.Context) (release func(), ok bool, err error) }`
  - `var pipeline.ErrImportInProgress error`
  - Feld `Pipeline.Lock ImportLock` (nil = ungesperrt)
  - `type db.ImportLock struct { Pool *pgxpool.Pool }` mit `TryAcquire`

- [x] **Step 1: Failing Test für die Pipeline-Seite** (`internal/pipeline/lock_test.go`)

```go
package pipeline

import (
	"context"
	"errors"
	"testing"
)

// lockStub is an ImportLock whose answer the test chooses. The real lock is
// tested against Postgres in internal/db; what matters here is the branch:
// a refused lock must stop the run before it fetches anything.
type lockStub struct {
	ok       bool
	err      error
	acquired int
	released int
}

func (l *lockStub) TryAcquire(context.Context) (func(), bool, error) {
	l.acquired++
	if l.err != nil {
		return nil, false, l.err
	}
	if !l.ok {
		return nil, false, nil
	}
	return func() { l.released++ }, true, nil
}

func TestRun_RefusedLockStopsBeforeFetching(t *testing.T) {
	lock := &lockStub{ok: false}
	p := &Pipeline{Lock: lock}

	_, err := p.Run(context.Background())

	if !errors.Is(err, ErrImportInProgress) {
		t.Fatalf("Run() error = %v, want ErrImportInProgress", err)
	}
	if lock.acquired != 1 {
		t.Errorf("TryAcquire called %d times, want 1", lock.acquired)
	}
	// A Pipeline with no Fetcher would panic or error the moment it fetched;
	// reaching neither is the proof that the lock came first.
	if lock.released != 0 {
		t.Errorf("release called %d times after a refused lock, want 0", lock.released)
	}
}

func TestRun_LockFailureIsReported(t *testing.T) {
	wantErr := errors.New("boom")
	p := &Pipeline{Lock: &lockStub{err: wantErr}}

	if _, err := p.Run(context.Background()); !errors.Is(err, wantErr) {
		t.Fatalf("Run() error = %v, want it to wrap %v", err, wantErr)
	}
}
```

- [x] **Step 2: Test laufen lassen, er muss fehlschlagen**

Run: `go test -count=1 -run 'TestRun_RefusedLock|TestRun_LockFailure' ./internal/pipeline/`
Expected: FAIL, `undefined: ImportLock`, `undefined: ErrImportInProgress`, `unknown field Lock`

- [x] **Step 3: Port und Sperre in `internal/pipeline/pipeline.go`**

Neben den vorhandenen Ports (`PostFetcher`, `CategoryLister`) ergänzen:

```go
// ImportLock serializes import runs across processes. The scheduled worker in a
// running server and a one-off `recipe-reader import` are two processes sharing
// one Instagram account, and the 15-minute floor between logins lives in the
// client — that is, in each process separately. Two runs at once would ration
// nothing, and repeated logins are what gets an account flagged.
//
// It is an interface here and a Postgres advisory lock in internal/db: the
// pipeline has no business knowing there is a database behind it.
type ImportLock interface {
	// TryAcquire takes the lock without waiting. ok reports whether it got it;
	// release is valid only when ok is true and must be called when the run
	// ends. An error means the lock could not be consulted at all.
	TryAcquire(ctx context.Context) (release func(), ok bool, err error)
}

// ErrImportInProgress reports that another import holds the lock. For the
// import command it is the whole outcome — exit 1, nothing done — so it is a
// sentinel rather than a string.
var ErrImportInProgress = errors.New("import: another import is already running")
```

Im `Pipeline`-Struct ergänzen:

```go
	// Lock, when set, serializes this run against every other process using the
	// same database. Nil means unlocked, which is what the pipeline's own tests
	// and any single-process use want.
	Lock ImportLock
```

Als Erstes im Rumpf von `Run`:

```go
	if p.Lock != nil {
		release, ok, err := p.Lock.TryAcquire(ctx)
		if err != nil {
			return ImportResult{}, fmt.Errorf("import: taking the import lock: %w", err)
		}
		if !ok {
			return ImportResult{}, ErrImportInProgress
		}
		defer release()
	}
```

- [x] **Step 4: Failing Test für den Postgres-Adapter** (`internal/db/lock_test.go`)

```go
package db_test

import (
	"context"
	"testing"

	"github.com/sBurmester/recipe-reader/internal/db"
	"github.com/sBurmester/recipe-reader/internal/db/testdb"
)

// The point of the lock is what happens to the second holder, and that is a
// property of Postgres sessions — so this runs against a real database with two
// pools, the way two processes would meet.
func TestImportLock_SecondHolderIsRefusedUntilTheFirstReleases(t *testing.T) {
	ctx := context.Background()
	dsn := testdb.NewDatabase(t, "import_lock")

	first, err := db.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("Connect() first error = %v", err)
	}
	defer first.Close()
	second, err := db.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("Connect() second error = %v", err)
	}
	defer second.Close()

	release, ok, err := db.ImportLock{Pool: first}.TryAcquire(ctx)
	if err != nil {
		t.Fatalf("TryAcquire() first error = %v", err)
	}
	if !ok {
		t.Fatal("TryAcquire() first ok = false, want the lock to be free")
	}

	if _, ok, err := (db.ImportLock{Pool: second}).TryAcquire(ctx); err != nil {
		t.Fatalf("TryAcquire() second error = %v", err)
	} else if ok {
		t.Fatal("TryAcquire() second ok = true, want it refused while the first holds the lock")
	}

	release()

	release2, ok, err := db.ImportLock{Pool: second}.TryAcquire(ctx)
	if err != nil {
		t.Fatalf("TryAcquire() after release error = %v", err)
	}
	if !ok {
		t.Fatal("TryAcquire() after release ok = false, want the lock free again")
	}
	release2()
}
```

- [x] **Step 5: Test laufen lassen, er muss fehlschlagen**

Run: `go test -count=1 -run TestImportLock ./internal/db/`
Expected: FAIL, `undefined: db.ImportLock`

- [x] **Step 6: `internal/db/lock.go` anlegen**

```go
package db

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// importLockKey identifies the import lock among all advisory locks in this
// database. The value is arbitrary and only has to stay stable: change it and
// two versions of the binary would no longer see each other's locks.
const importLockKey int64 = 8_233_071_001

// releaseTimeout bounds the two statements that run after a run is already
// over: the unlock, and the connection close that stands in for it when the
// unlock fails. Both detach from the run's context, because releasing is what
// has to happen when the run was cancelled — and detaching drops the deadline
// along with the cancellation, so without one of their own a Postgres that
// accepts the connection but never answers would block `defer release()`
// forever, with no context left for a Ctrl-C to cancel.
const releaseTimeout = 5 * time.Second

// ImportLock is pipeline.ImportLock backed by a Postgres advisory lock, which
// is what lets a running server and a one-off import command see each other.
//
// The lock is session-scoped, so it is held by one connection out of the pool
// for as long as the run lasts, and Postgres drops it when that session ends —
// including when the process is killed. That is the property a lock file does
// not have.
type ImportLock struct {
	Pool *pgxpool.Pool
}

// TryAcquire implements pipeline.ImportLock.
func (l ImportLock) TryAcquire(ctx context.Context) (func(), bool, error) {
	conn, err := l.Pool.Acquire(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("db: acquire a connection for the import lock: %w", err)
	}

	var got bool
	if err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", importLockKey).Scan(&got); err != nil {
		// Whether Postgres took the lock before this failed is not knowable
		// from here: a cancellation can land between the server executing the
		// statement and the row reaching us. Releasing the connection would
		// then lend the next borrower a lock nobody will ever release, so it is
		// discarded the same way a failed unlock discards it. The cost of being
		// wrong is one reconnect on a path that is already failing the run.
		discard(ctx, conn)
		return nil, false, fmt.Errorf("db: take the import lock: %w", err)
	}
	if !got {
		conn.Release()
		return nil, false, nil
	}

	return func() {
		// WithoutCancel because releasing is what has to happen when the run was
		// cancelled, which is exactly when ctx is already done; with a deadline
		// of its own because WithoutCancel strips the run's — see releaseTimeout.
		unlockCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), releaseTimeout)
		defer cancel()
		if _, err := conn.Exec(unlockCtx, "SELECT pg_advisory_unlock($1)", importLockKey); err != nil {
			slog.Warn("db: could not release the import lock; discarding its connection", "error", err)
			// The lock belongs to this session. Handing the connection back to
			// the pool would lend the next borrower a lock nobody can release;
			// closing it ends the session, and Postgres frees the lock with it.
			discard(ctx, conn)
			return
		}
		conn.Release()
	}, true, nil
}

// discard takes conn out of the pool and closes it, which ends its Postgres
// session and with it every advisory lock that session still holds. It answers
// both ways a connection can end up back in the pool holding a lock nobody can
// release: an unlock that failed, and an acquire whose answer never arrived.
//
// It derives its own bounded context from ctx rather than taking one, because
// every caller is on a path where ctx may already be cancelled and a close
// still has to be attempted.
func discard(ctx context.Context, conn *pgxpool.Conn) {
	closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), releaseTimeout)
	defer cancel()
	if err := conn.Hijack().Close(closeCtx); err != nil {
		slog.Warn("db: closing the import lock's connection failed", "error", err)
	}
}
```

- [x] **Step 7: Den Worker-Lauf sperren** (`internal/server/server.go`)

Im `pipeline.Pipeline`-Literal in `Run` als letztes Feld ergänzen:

```go
			// The same lock the import command takes, so the two cannot run at
			// once against one Instagram account.
			Lock: db.ImportLock{Pool: pool},
```

- [x] **Step 8: Tests laufen lassen, sie müssen grün sein**

Run: `go build ./... && go test -count=1 -race ./internal/pipeline/ ./internal/db/ ./internal/server/`
Expected: PASS

- [x] **Step 9: Commit-Gate und Commit**

```bash
git add internal
git commit -m "feat(pipeline): serialize imports across processes" -m "A run now takes a Postgres advisory lock through a port defined at the pipeline, so the scheduled worker and a one-off import cannot fetch from one Instagram account at the same time. The lock dies with its session, which a lock file would not."
```

---

### Task 5: `config.ImportLimits` aus `config.Import` lösen

Damit `import` die Grenzen sieht, aber nicht das Intervall — dasselbe Muster wie `Listen` in `HTTP` (D6). Die Hilfe von `serve` ändert sich dadurch nicht, weil das `group:"Import"`-Tag auf dem äußeren Feld auch für das eingebettete gilt.

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/import_bounds_test.go` (Testnamen und Aufbau an die neue Struktur anpassen, falls sie `config.Import{...}` direkt bauen)

**Interfaces:**
- Consumes: —
- Produces: `type config.ImportLimits struct { MaxItems, MaxPages int }` mit `Validate()`; `config.Import` bettet es ein und behält `Interval` samt eigenem `Validate()`. Zugriffe wie `cfg.Import.MaxItems` bleiben durch Promotion unverändert gültig.

- [x] **Step 1: Failing Test** (an `internal/config/import_bounds_test.go` anhängen)

Die Datei ist `package config` — also ohne `config.`-Präfix — und importiert bisher nur `testing`; `strings` dazunehmen.

```go
// import reads the limits but not the interval, so the two validate
// separately — the same split that lets healthcheck share Listen without
// inheriting the rest of HTTP.
func TestImportLimits_ValidateRejectsABoundBelowOne(t *testing.T) {
	for _, tc := range []struct {
		name   string
		limits ImportLimits
		want   string
	}{
		{"max items", ImportLimits{MaxItems: 0, MaxPages: 100}, "IMPORT_MAX_ITEMS"},
		{"max pages", ImportLimits{MaxItems: 50, MaxPages: 0}, "IMPORT_MAX_PAGES"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.limits.Validate()
			if err == nil {
				t.Fatalf("Validate(%+v) = nil, want an error naming %s", tc.limits, tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("Validate() error = %v, want it to name %s", err, tc.want)
			}
		})
	}
}

// The interval stays with Import, and a valid pair of limits must not make it
// pass on its own.
func TestImport_ValidateStillRejectsANonPositiveInterval(t *testing.T) {
	cfg := Import{MaxItems: 50, MaxPages: 100}

	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() = nil for a zero interval, want an error")
	}
}
```

- [x] **Step 2: Test laufen lassen, er muss fehlschlagen**

Run: `go test -count=1 -run 'TestImportLimits|TestImport_ValidateStill' ./internal/config/`
Expected: FAIL, `undefined: config.ImportLimits`

- [x] **Step 3: Die Gruppe teilen** (`internal/config/config.go`)

`Import` und sein `Validate` ersetzen durch:

```go
// ImportLimits bounds one import run. It is a group of its own because both
// readers want it and only one of them wants the schedule: the worker in serve
// runs on an interval, the import command runs once. Same reason Listen is
// separate from HTTP.
//
// The two existed only as package defaults in internal/instagram, reachable by
// recompiling; a backfill of a large saved-posts history is exactly when an
// operator wants to raise them, and a flagged account is when they want to
// lower them. The defaults here match the package's own.
type ImportLimits struct {
	MaxItems int `name:"import-max-items" env:"IMPORT_MAX_ITEMS" default:"50" help:"Most new posts one import run collects. Posts already imported do not count against it."`
	MaxPages int `name:"import-max-pages" env:"IMPORT_MAX_PAGES" default:"100" help:"Most feed pages one import run walks, whether or not they held anything new."`
}

// Validate rejects a run bound below one: the fetcher would quietly replace it
// with its own default, so IMPORT_MAX_ITEMS=0 — plausibly meant as "pause
// imports" — would import fifty posts instead, which is the opposite.
func (i ImportLimits) Validate() error {
	if i.MaxItems < 1 {
		return fmt.Errorf("config: IMPORT_MAX_ITEMS must be at least 1, got %d", i.MaxItems)
	}
	if i.MaxPages < 1 {
		return fmt.Errorf("config: IMPORT_MAX_PAGES must be at least 1, got %d", i.MaxPages)
	}
	return nil
}

// Import schedules and bounds the background import.
type Import struct {
	Interval time.Duration `name:"import-interval" env:"IMPORT_INTERVAL" default:"6h" help:"How often the background worker imports new saved posts."`

	ImportLimits `embed:""`
}

// Validate rejects an interval at or below zero: time.NewTicker panics for one,
// on the calling goroutine, inside Worker.Start — so IMPORT_INTERVAL=0 used to
// abort the process with a stack trace instead of the clean slog.Error("fatal")
// path main was built around. The limits validate themselves.
func (i Import) Validate() error {
	if i.Interval <= 0 {
		return fmt.Errorf("config: IMPORT_INTERVAL must be positive, got %s", i.Interval)
	}
	return nil
}
```

- [x] **Step 4: Tests laufen lassen, sie müssen grün sein**

Run: `go build ./... && go test -count=1 -race ./internal/config/ ./internal/server/ ./cmd/recipe-reader/`
Expected: PASS. Kein Aufrufer ändert sich: `cfg.Import.MaxItems` und `cfg.Import.MaxPages` funktionieren über die Einbettung weiter.

Zusätzlich prüfen, dass die Hilfe unverändert ist:

```bash
go run ./cmd/recipe-reader serve --help | grep -o -- '--[a-z][a-z-]*' | sort -u | tr '\n' ' '
```

Erwartet: dieselbe Liste wie vor diesem Task (inklusive `--log-level`/`--log-format` aus Task 3).

- [x] **Step 5: Commit-Gate und Commit**

```bash
git add internal/config
git commit -m "refactor(config): split the import limits from the schedule" -m "ImportLimits holds max-items and max-pages and validates them; Import keeps the interval. The import command reads the limits without inheriting a schedule it does not have, the way healthcheck shares Listen. serve's help is unchanged."
```

---

### Task 6: Kommando `import`

**Files:**
- Create: `internal/server/import.go` (`ImportOnce`)
- Create: `internal/cli/import.go` (`ImportCmd`)
- Modify: `cmd/recipe-reader/main.go` (Feld `Import` im `CLI`)
- Modify: `cmd/recipe-reader/main_test.go` (`import`-Fall in `TestParse_FlagBeforeAnotherCommandIsRefused`)
- Create: `cmd/recipe-reader/import_test.go`
- Modify: `README.md` (Commands, Architecture)

**Interfaces:**
- Consumes: `newExtractor`, `newFetcher`, `alreadyImported` (paketintern in `internal/server`), `db.Open`, `db.ImportLock` (Task 4), `pipeline.Pipeline`, `config.ImportLimits` (Task 5), `run`/`parse`/`clearEnv`/`unreachableDSN` aus `cmd/recipe-reader`
- Produces: `func server.ImportOnce(ctx context.Context, cfg config.Config) (pipeline.ImportResult, error)`; `type cli.ImportCmd`; `func (c *ImportCmd) Run(ctx context.Context) error`

- [x] **Step 1: Failing Tests** (`cmd/recipe-reader/import_test.go`)

```go
package main

import (
	"strings"
	"testing"
)

// Without an account there is nothing to import, and a one-off run has no next
// tick to recover at — so it says what is missing and fails, where serve would
// start anyway with the import worker withheld.
func TestRun_ImportWithoutAnAccountFails(t *testing.T) {
	clearEnv(t)

	err := run([]string{"import", "--db-dsn", unreachableDSN})

	if err == nil {
		t.Fatal("run(import) = nil, want an error naming the missing account")
	}
	if !strings.Contains(err.Error(), "INSTAGRAM_USERNAME") {
		t.Errorf("run(import) error = %v, want it to name INSTAGRAM_USERNAME", err)
	}
}

// import reads the database, the account, extraction, the LLM and the run
// limits — not the listen address, and not the schedule it has no part in.
func TestParse_ImportTakesNeitherTheAddressNorTheInterval(t *testing.T) {
	clearEnv(t)

	for _, flag := range []string{"--http-addr", "--import-interval"} {
		t.Run(flag, func(t *testing.T) {
			_, _, err := parse(t, "import", flag, "1s")
			if err == nil {
				t.Fatalf("parse(import %s) = nil error, want it rejected as unknown", flag)
			}
			if !strings.Contains(err.Error(), "unknown flag") {
				t.Errorf("parse(import %s) error = %v, want an unknown-flag error", flag, err)
			}
		})
	}
}
```

Der Fall „Flag vor dem Kommandonamen“ (E11) bekommt **keinen** eigenen Test: Dafür gibt es die Tabelle in `TestParse_FlagBeforeAnotherCommandIsRefused`, die schon `healthcheck` und `migrate` abdeckt. In `cmd/recipe-reader/main_test.go` dort den Fall ergänzen:

```go
		{"--db-dsn", unreachableDSN, "import"},
```

- [x] **Step 2: Tests laufen lassen, sie müssen fehlschlagen**

Run: `go test -count=1 -run 'Import' ./cmd/recipe-reader/`
Expected: FAIL, `unexpected argument import`

- [x] **Step 3: `internal/server/import.go` anlegen**

```go
package server

import (
	"context"
	"errors"
	"fmt"

	"github.com/sBurmester/recipe-reader/internal/config"
	"github.com/sBurmester/recipe-reader/internal/db"
	"github.com/sBurmester/recipe-reader/internal/instagram"
	"github.com/sBurmester/recipe-reader/internal/pipeline"
	"github.com/sBurmester/recipe-reader/internal/repository"
)

// ImportOnce runs exactly one import and returns its tally. It is the one-off
// half of what Run does on a schedule — same extractor, same fetcher, same
// pipeline, same cross-process lock — without the HTTP server, the worker and
// the shutdown sequence.
//
// cfg.HTTP is not read: an import serves nothing.
func ImportOnce(ctx context.Context, cfg config.Config) (pipeline.ImportResult, error) {
	if cfg.Instagram.Username == "" {
		return pipeline.ImportResult{}, errors.New("import: no Instagram account configured; set INSTAGRAM_USERNAME")
	}

	// Before anything external, exactly as in Run: a misconfigured extraction
	// mode is a startup error and should not be buried under a migration.
	extractor, err := newExtractor(cfg)
	if err != nil {
		return pipeline.ImportResult{}, err
	}

	pool, err := db.Open(ctx, cfg.Database.DSN)
	if err != nil {
		return pipeline.ImportResult{}, err
	}
	defer pool.Close()

	// Taken here, before newFetcher, and not by the Pipeline below. The lock
	// exists to stop two processes logging into one Instagram account (D1), and
	// the pipeline's own guard runs after the fetcher has already logged in and
	// written the session file — so a refused run used to pay the exact cost the
	// lock was introduced to avoid before it said no.
	//
	// The Pipeline built below therefore has no Lock: it would ask this pool for
	// a second connection and Postgres would refuse this process the lock it is
	// already holding, so the command would report itself as "another import".
	// serve keeps the pipeline's guard — there the login happens once at
	// startup, not per run, so the guard has nothing to get in front of.
	release, ok, err := (db.ImportLock{Pool: pool}).TryAcquire(ctx)
	if err != nil {
		return pipeline.ImportResult{}, fmt.Errorf("import: taking the import lock: %w", err)
	}
	if !ok {
		return pipeline.ImportResult{}, pipeline.ErrImportInProgress
	}
	defer release()

	recipes := repository.NewRecipeRepository(pool)
	lookups := repository.NewLookupRepository(pool)

	fetcher, loginErr := newFetcher(ctx, cfg, instagram.NewClient(), recipes)
	if loginErr != nil {
		// serve carries on and retries at the next tick, under the 15-minute
		// floor. A single run has no next tick, so a failed login is its result.
		return pipeline.ImportResult{}, loginErr
	}

	p := &pipeline.Pipeline{
		Fetcher:   fetcher,
		Extractor: extractor,
		Recipes:   recipes,
		// The lookup repository the API serves the category picker from, so an
		// import may only attach a category the picker already offers.
		Categories:       lookups,
		Threshold:        cfg.Extraction.Threshold,
		PublishThreshold: cfg.Extraction.PublishThreshold,
	}
	return p.Run(ctx)
}
```

- [x] **Step 4: `internal/cli/import.go` anlegen**

```go
package cli

import (
	"context"
	"log/slog"

	"github.com/sBurmester/recipe-reader/internal/config"
	"github.com/sBurmester/recipe-reader/internal/server"
)

// ImportCmd runs one import and exits. serve does the same every
// IMPORT_INTERVAL; this does it now — before a rollout, after a restore, or
// after fixing a login — without starting the server.
//
// It takes the settings an import reads and no others: the database, the
// account, the extraction rules, the LLM and the limits of one run. The
// schedule is serve's business, and the listen address is nobody's here.
type ImportCmd struct {
	config.Database     `embed:"" group:"Database"`
	config.Instagram    `embed:"" group:"Instagram"`
	config.Extraction   `embed:"" group:"Extraction"`
	config.LLM          `embed:"" group:"LLM"`
	config.ImportLimits `embed:"" group:"Import"`
}

// Run imports once and reports the tally. A run refused by the import lock —
// because a server or another command is importing right now — fails, so the
// operator sees it rather than reading an empty tally as "nothing new".
func (c *ImportCmd) Run(ctx context.Context) error {
	cfg := config.Config{
		Database:   c.Database,
		Instagram:  c.Instagram,
		Extraction: c.Extraction,
		LLM:        c.LLM,
		// Interval stays zero: ImportOnce never schedules anything.
		Import: config.Import{ImportLimits: c.ImportLimits},
	}

	result, err := server.ImportOnce(ctx, cfg)
	if err != nil {
		// A fetch that fails part-way still imports what it already collected,
		// so a failed run can carry a real tally — and "imported nine, then
		// throttled" is a different decision from "did nothing". Only the
		// counts, not the error: main logs that. Seen is zero for the failures
		// that happen before the first post — a refused lock, a failed login, a
		// fetch that returned nothing — and those get no line.
		if result.Seen > 0 {
			slog.Warn("import did not finish",
				"seen", result.Seen, "imported", result.Imported, "skipped", result.Skipped,
				"no_recipe", result.NoRecipe, "failed", result.Failed, "degraded", result.Degraded)
		}
		return err
	}
	slog.Info("import finished",
		"seen", result.Seen, "imported", result.Imported, "skipped", result.Skipped,
		"no_recipe", result.NoRecipe, "failed", result.Failed, "degraded", result.Degraded)
	return nil
}
```

- [x] **Step 5: Ins Kommando-Menü hängen** (`cmd/recipe-reader/main.go`)

Im `CLI`-Struct nach `Migrate` ergänzen (gofmt richtet die Spalten aus):

```go
	Import      cli.ImportCmd      `cmd:"" help:"Run one import of new saved posts and exit. serve does the same every IMPORT_INTERVAL; this does it now, without starting the server."`
```

- [x] **Step 6: Tests laufen lassen, sie müssen grün sein**

Run: `go build ./... && go test -count=1 -race ./cmd/recipe-reader/ ./internal/server/ ./internal/cli/`
Expected: PASS

Zusätzlich von Hand:

```bash
go run ./cmd/recipe-reader import --help | grep -c -- '--http-addr\|--import-interval'   # 0
go run ./cmd/recipe-reader --help | grep import
```

- [x] **Step 7: README**

In `## Commands` der Tabelle eine Zeile anfügen:

```markdown
| `import` | Runs one import of new saved posts and exits: before a rollout, after a restore, or after fixing a login. `serve` does the same every `IMPORT_INTERVAL`. Applies pending migrations and seeds the lookup tables first, exactly as `serve` and `migrate` do — so running it against the database of an older server migrates that database. Reads the database, Instagram, extraction, LLM and import-limit settings. Refuses to start while another import is running, in this process or in a server, and refuses before it logs in. With compose: `docker compose run --rm app import`. |
```

In `## Architecture` den Satz aus Task 5 des Vorgängerplans erweitern: `` `migrate` to `internal/db`. `` → `` `migrate` to `internal/db`; `import` to `internal/server`, which runs the same pipeline the worker runs, once. ``

Im Abschnitt über den Import ergänzen, dass ein Lauf einen Postgres-Advisory-Lock hält und ein zweiter Lauf deshalb mit Exit 1 abgelehnt wird — **vor** dem Login, weil der Login die Kosten sind, die der Lock vermeiden soll — und dass der Worker eines Servers eine Absage nicht als Lauf verbucht, sondern weiter die letzte echte Bilanz meldet.

- [x] **Step 8: Commit-Gate und Commit**

```bash
git add cmd internal README.md
git commit -m "feat(cli): add a one-off import command" -m "import runs the pipeline once and exits, reading only the settings an import needs. It shares the advisory lock with the server's worker, so the two cannot import against one Instagram account at the same time."
```

---

### Abschluss-Verifikation

- [x] **Step 1: Paket `main` prüfen**

```bash
ls cmd/recipe-reader/
go list -f '{{join .Imports " "}}' ./cmd/recipe-reader
```

Erwartet: nur `main.go` und Tests; aus `internal/*` genau `internal/cli`, `internal/config` und `internal/logging` (D3).

- [x] **Step 2: Commit-Gate komplett** (siehe Global Constraints). Alles grün.

- [x] **Step 3: Docker-Gate** (siehe Global Constraints). `smoke test passed`.

- [x] **Step 4: Compose End-to-End** (im eigenen Projekt `recipe-reader-e2e`, nie im Default-Projekt — dort liegt das Volume `db-data` mit echten Daten)

```bash
export API_TOKEN=$(openssl rand -hex 32) POSTGRES_PASSWORD=$(openssl rand -hex 16)
docker compose -p recipe-reader-e2e up -d --build --wait
docker compose -p recipe-reader-e2e run --rm app migrate
docker compose -p recipe-reader-e2e run --rm app import          # ohne Account: Exit 1, Meldung nennt INSTAGRAM_USERNAME
docker compose -p recipe-reader-e2e run --rm app --log-format json healthcheck
docker compose -p recipe-reader-e2e down -v
```

`down -v` auch dann ausführen, wenn ein Schritt davor scheitert.

- [x] **Step 5: Akzeptanzkriterien in Teil A abhaken** (US1–US5, Definition of Done)

- [x] **Step 6: Backlog des Vorgängerplans aktualisieren**

In `docs/superpowers/plans/2026-09-19-kong-cli-commands.md` im Abschnitt „Backlog“ vermerken, dass die vier Punkte in diesem Plan umgesetzt sind, mit Link.

- [x] **Step 7: Diese Datei abhaken und committen**

```bash
git add docs/superpowers/plans/
git commit -m "docs: mark M3 done in the CLI backlog plan"
```

- [x] **Step 8: PR 3/3** erst nach Freigabe durch den Nutzer öffnen.

---

## Teil D: Abdeckung des Backlogs

| Backlog-Punkt (Plan vom 2026-09-19) | Umgesetzt in |
| --- | --- |
| `server.Run` soll auch bei einem Fehler drainen (M1-Review, F2) | Task 1 |
| `db.Open`: Migration sieht den Kontext nicht (M4-Review, Minor 5) | Task 2 |
| `--log-level` / `--log-format` als globale Flags | Task 3 |
| `import`-Kommando für einen einmaligen Importlauf ohne Server | Task 4 (Sperre), Task 5 (Grenzen), Task 6 (Kommando) |
| Offene Frage „gleichzeitiger Betrieb mit laufendem `serve`“ | D1, Task 4 — Postgres-Advisory-Lock |
| Offene Frage „Ausgabeformat“ | D5, Task 6 — Log-Zeilen plus Zusammenfassung, mit `--log-format json` maschinenlesbar |
