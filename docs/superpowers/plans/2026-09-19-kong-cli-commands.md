# Kong-Command-Tree & schlankes `main`: Implementierungsplan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Die flache kong-Flag-Struktur wird zu einem Kommandobaum (`serve`, `healthcheck`, `migrate`) mit `Run`-Methoden, Optionsgruppen und kong-Hooks umgebaut. `cmd/recipe-reader/main.go` parst die Kommandozeile und führt das gewählte Kommando aus; die Arbeit liegt in `internal/*` (E13).

**Architecture:** `cmd/recipe-reader/main.go` enthält den kong-Baum, parst die Kommandozeile mit gebundenem Kontext und gebundener Version, lehnt verirrte Flags ab und führt das gewählte Kommando aus (E13). In `internal/cli` liegen nur die Kommandos; jedes delegiert in seinem `Run` sofort an das Paket, das die eigentliche Arbeit macht (`internal/server` = Composition Root, `internal/healthcheck` = Probe, `internal/db` = Migration). Settings werden in `internal/config` als eingebettete kong-Gruppen deklariert und über `Validate()` bzw. `BeforeApply()` geprüft. kong ruft beide nur für das gewählte Kommando auf.

**Tech Stack:** Go 1.27.1, `github.com/alecthomas/kong` v1.16.1 (bereits in `go.mod`, laut `go list -m -u` am 2026-09-19 die aktuelle Version), pgx/v5, testcontainers-go, Docker.

**Umsetzungsmodus (vom Nutzer am 2026-09-19 festgelegt):** Subagent-Driven, mit dem Sub-Skill `superpowers:subagent-driven-development`. Details stehen unter „Ablauf der Umsetzung“ in Teil C. Die Umsetzung beginnt erst nach ausdrücklicher Freigabe durch den Nutzer.

## Global Constraints

- **Branch:** Nie auf `main` arbeiten. Jeder Milestone hat einen eigenen Branch und einen eigenen PR; die Namen stehen unter „Branches und PRs“ in Teil C und folgen dem Conventional-Commits-Präfix.
- **Kompatibilität:** Env-Variablen-Namen, Flag-Namen und Defaults bleiben **unverändert**. `.env.example` und `docker-compose.yml` (bis auf einen Kommentar) werden nicht angefasst. Einzige Ausnahme ist `--health-check`, das durch das Kommando `healthcheck` ersetzt wird (Entscheidung E2).
- **Credentials:** `API_TOKEN`, `INSTAGRAM_PASSWORD`, `LLM_API_KEY` und `ANTHROPIC_API_KEY` bleiben **nur über die Umgebung** setzbar, ohne Flag-Form (`kong:"-"`).
- **Exit-Codes:** `0` bei Erfolg, `1` bei **jedem** Fehler, auch bei Parse-Fehlern (kong würde `80` nehmen). Der Docker-`HEALTHCHECK` erwartet 0/1.
- **Version-Stamping:** `-ldflags "-X main.version=..."` in `Makefile` und `Dockerfile` bleibt unverändert, d. h. `var version` bleibt im Paket `main`.
- **Kommentarstil:** Verschobener Code behält seine Kommentare **wortgleich**. Die Repo-Kommentare erklären das *Warum*; das geht beim Neuschreiben verloren. Neuer Code folgt demselben Stil.
- **Commit-Gate (vor jedem Commit, Pflicht laut `~/.claude/CLAUDE.md`; Docker muss laufen):**
  ```bash
  gofmt -w .
  go vet ./...
  go fix ./...
  /home/ripmav/.local/bin/golangci-lint run ./...
  go run golang.org/x/vuln/cmd/govulncheck@latest ./...
  go test -race ./...
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
  Dabei das tatsächlich ausführende Modell eintragen.
- **Fortschritt:** Erledigte Steps und Tasks werden **in dieser Datei** abgehakt (`[x]`), auch in der Aufgabenliste unten.

---

## Teil A: Product-Owner-Sicht

### Ausgangslage

- `cmd/recipe-reader/` enthält vier Quelldateien (`main.go` mit 300 Zeilen, `fetcher.go`, `healthcheck.go`, `version.go`) und drei Testdateien. Das Paket `main` ist faktisch die Composition Root: Extractor-Auswahl, Instagram-Fetcher, HTTP-Server, Timeouts, Graceful Shutdown und die Health-Probe liegen alle dort.
- kong wird zwar verwendet (`internal/config`), aber nur als **flacher Flag-Parser** ohne Kommandos. `--health-check` ist ein bool-Flag, das die Binary in ein anderes Programm verwandelt. Dieses Mode-Flag durchläuft trotzdem die komplette Server-Validierung: Ein `--health-check` mit `HTTP_ADDR=:8080` ohne `API_TOKEN` scheitert an einer Regel, die für eine Probe irrelevant ist.
- `--version` und `--health-check` stehen als Felder in `config.Config`, obwohl sie keine Settings sind. Der Code sagt das selbst: *„kong has no other place to hang a flag“*.
- Validierung und Credential-Lesen laufen manuell nach `parser.Parse` (`readCredentials`, `validate`) statt über kongs Hooks.

### Ziele (Outcomes)

| # | Ziel | Messbar an |
| --- | --- | --- |
| Z1 | CLI nach kong-Best-Practices: Kommandobaum, `Run`-Methoden, DI über Bindings, `Validate()`/Hooks statt manueller Nachbearbeitung | kong-Baum in `main.go`, die Kommandos `serve`, `healthcheck`, `migrate` in `internal/cli` (E13); keine manuellen `validate`/`readCredentials`-Aufrufe mehr |
| Z2 | `main.go` so schlank wie möglich | ~~≤ 25 Zeilen, Body von `main()` ist eine Zeile~~ geändert durch E13: `main()` ruft nur `run()` und endet bei einem Fehler mit 1; `run()` parst mit kong und dispatcht; die Kommandos liegen in `internal/cli` |
| Z3 | Paket `main` so schlank wie möglich | ~~`cmd/recipe-reader/` enthält **nur** `main.go`; Imports: `os` + `internal/cli`~~ geändert durch E13: `cmd/recipe-reader/` enthält `main.go` und dessen Tests; von `internal/*` importiert `main.go` nur `internal/cli` (ab Task 4 zusätzlich `internal/config` für `config.Groups`) |
| Z4 | Jedes Kommando sieht und validiert nur die Settings, die es liest | `healthcheck` akzeptiert eine Umgebung, die `serve` ablehnt (Test) |
| Z5 | Kein Verhaltensbruch für bestehende Deployments | `recipe-reader` ohne Argumente und mit allen bisherigen Flags/Envs startet den Server; Smoke-Test grün |

### Nicht-Ziele

- Keine Umbenennung von Flags, Env-Variablen oder Defaults und kein `kong.DefaultEnvars`-Präfix.
- Keine Config-Datei (`kong.Configuration`-Loader), kein `--log-level`/`--log-format`.
- Keine Änderung an HTTP-API, Import-Pipeline, Extraktion oder Datenbankschema.
- Kein `import`-Einmallauf-Kommando in diesem Plan. Siehe Backlog.

### User Stories & Akzeptanzkriterien

**US1: Betreiber mit bestehendem Deployment.** *Als Betreiber möchte ich nach dem Update nichts an meinem Setup ändern müssen.*
- [x] `recipe-reader` ohne Argumente startet den Server (Default-Kommando `serve`).
- [x] `recipe-reader --http-addr 127.0.0.1:9090` (Flags ohne Kommando) startet den Server mit diesem Flag.
- [x] Alle Env-Variablen aus `.env.example` wirken unverändert; `docker compose up` und `make run` funktionieren ohne Änderung.
- [x] `recipe-reader --version` gibt die Version aus und beendet sich mit 0.

**US2: Container-Healthcheck.** *Als Betreiber möchte ich eine Probe, die nur prüft, ob der Server antwortet.*
- [x] `recipe-reader healthcheck` endet mit 0, wenn `/api/healthz` `{"status":"ok"}` liefert, sonst mit 1.
- [x] `healthcheck` liest nur `HTTP_ADDR`/`--http-addr` und wird durch keine `serve`-Regel abgelehnt.
- [x] Der `HEALTHCHECK` im `Dockerfile` nutzt das Kommando; der Smoke-Test meldet das Image als `healthy`.
- [x] `--health-check` wird als unbekanntes Flag abgelehnt (laut, Exit 1), statt stillschweigend einen Server zu starten.
- [x] Ein Flag vor dem Kommandonamen wird abgelehnt (Exit 1), statt stillschweigend verloren zu gehen: `recipe-reader --http-addr 10.0.0.1:9090 healthcheck` prüft nicht die Default-Adresse, sondern scheitert mit dem Hinweis, Flags hinter das Kommando zu schreiben (Entscheidung E11).

**US3: Entwickler.** *Als Entwickler möchte ich Wiring und CLI ohne `os.Exit` und ohne Datenbank testen können.*
- [x] Die Arbeit der Kommandos liegt in `internal/server` und `internal/healthcheck` und ist dort getestet; die dünnen `Run`-Methoden in `internal/cli` testet `cmd/recipe-reader/main_test.go` mit; `cmd/recipe-reader/` enthält nur `main.go` (Kommandobaum, Parsen und Dispatch, E13) und dessen Tests.
- [x] `run(args, opts...)` in `main.go` ist ohne Prozess-Exit testbar (kong-`Exit`/`Writers` injizierbar); den Exit-Status von `main()` prüft ein Test in einem Kindprozess (E13).

**US4: Betreiber liest die Hilfe.** *Als Betreiber möchte ich schnell finden, was ich einstellen kann.*
- [x] `recipe-reader --help` listet die Kommandos.
- [x] `recipe-reader serve --help` zeigt die Flags gruppiert (HTTP, Database, Instagram, Extraction, LLM, Import), jeweils mit Env-Namen.
- [x] Die Hilfe nennt weiterhin die vier env-only Credentials: `recipe-reader --help` in der Beschreibung, `recipe-reader serve --help` in der Beschreibung der Gruppe, zu der sie gehören (Entscheidung E12).

**US5: Betreiber migriert separat.** *Als Betreiber möchte ich Migrationen ausführen können, ohne den Server zu starten, z. B. vor einem Rollout oder nach einem Restore.*
- [x] `recipe-reader migrate` migriert, seedet die Lookup-Tabellen und beendet sich. Ein zweiter Aufruf ist ein No-op.
- [x] `migrate` liest nur `DB_DSN`/`--db-dsn`.
- [x] `recipe-reader --db-dsn <dsn> migrate` wird abgelehnt, statt stillschweigend die Default-Datenbank zu migrieren (Entscheidung E11).

### Entscheidungen

| # | Entscheidung | Begründung |
| --- | --- | --- |
| E1 | `serve` ist Default-Kommando mit `default:"withargs"` | Dockerfile-`ENTRYPOINT`, Compose, `make run` und eventuelle systemd-Units rufen die Binary ohne Kommando auf. `withargs` erlaubt zusätzlich `serve`-Flags ohne Kommandonamen. Per Prototyp gegen kong v1.16.1 verifiziert. |
| E2 | `--health-check` entfällt **ohne Alias**, Ersatz ist `healthcheck` | Binary und `HEALTHCHECK` liegen im selben Image und werden gemeinsam umgestellt. Compose erbt die Probe. Ein Alias würde das Mode-Flag-Antipattern konservieren. Das Risiko externer Exec-Probes ist unter R6 aufgeführt; der Commit ist als Breaking Change markiert (`refactor!`). |
| E3 | `--version` bleibt ein globales Flag (`kong.VersionFlag`), kein `version`-Kommando | Der Smoke-Test, die README und die Gewohnheit der Betreiber nutzen `--version`. Es funktioniert mit und ohne Kommando. |
| E4 | Credentials werden in **`BeforeApply`**-Hooks gelesen, nicht in `AfterApply` | kong v1.16.1 ruft `Validate()` **vor** `AfterApply` auf (`kong.go`, `Parse`). Die `API_TOKEN`-Regel muss den Token aber sehen. |
| E5 | Validierung über `Validate()`-Methoden pro Gruppe | kong validiert nur Knoten auf dem gewählten Kommandopfad (`context.go`, `Validate`). Damit ist Z4 automatisch erfüllt. `Validate` auf eingebetteten Structs wird gefunden (`getValidators` → `walkEmbedded`). |
| E6 | Kein `kong.Parse`/`FatalIfErrorf`; `main()` loggt eine Zeile und endet mit 1 (ursprünglich `cli.Main`, siehe E13) | Beide lesen `os.Args` bzw. rufen `os.Exit` auf und sind damit nicht testbar. `FatalIfErrorf` liefert bei Parse-Fehlern Exit 80, was mit dem 0/1-Kontrakt des `HEALTHCHECK` kollidiert. Das Log-Format (`slog.Error("fatal", …)`) bleibt wie bisher. |
| E7 | Pakete: `internal/cli` (die Kommandos; den Baum hält nach E13 `main.go`), `internal/server` (Composition Root für `serve`), `internal/healthcheck` (Probe), `internal/config` (Flag-Gruppen); `var version` bleibt in `main` | Aufteilung nach Verantwortung. `-X main.version` bleibt gültig, sodass `Makefile` und `Dockerfile` keine ldflags-Änderung brauchen. |
| E8 | DI über kong-Bindings: `kong.BindTo(ctx, (*context.Context)(nil))` und `kong.Bind(BuildVersion(v))` | kong bindet über den **konkreten** Typ (`callbacks.go`, `bindings.add`). Ein `kong.Bind(ctx)` würde einen `context.Context`-Parameter also nicht bedienen. |
| E9 | `migrate` ist im Scope, ein einmaliges `import` landet im Backlog | `migrate` ist ein bereits existierender, klar abgegrenzter Schritt und kostet wenig. `import` teilt sich die Session-Datei und die Login-Rationierung (15-Minuten-Floor) mit einem laufenden Server und braucht ein eigenes Design. |
| E10 | `Dockerfile` bekommt `CMD ["serve"]` | Macht den Default sichtbar; `docker run <image> migrate` bzw. `--version` überschreiben `CMD` wie gewohnt. |
| E11 | `CLI.Validate` in `main.go` lehnt beim Parsen jedes Flag ab, das nicht zu einem Knoten auf dem gewählten Kommandopfad gehört (`rejectMisplacedFlags` als kong-Hook am Wurzelknoten, sodass jeder Parser aus `newParser` ihn anwendet; ursprünglich ein eigener Aufruf in `cli.Run` bzw. `run`, siehe E13 und Milestone-Review M2, F10) | Wegen `withargs` liest kong ein Flag vor dem Kommandonamen als `serve`-Flag und wechselt erst danach das Kommando. `recipe-reader --http-addr X healthcheck` prüfte so still die Default-Adresse, `--db-dsn X migrate` würde die Default-Datenbank migrieren. Im Review vom 2026-09-19 per Prototyp gegen kong v1.16.1 nachgewiesen, ebenso der Guard. |
| E12 | Die Gruppen werden über `kong.ExplicitGroups(config.Groups())` deklariert; die Beschreibungen von HTTP, Instagram und LLM nennen die env-only Credentials der Gruppe | `serve --help` zeigt die App-Beschreibung nicht (Prototyp), also dort, wo die Flags stehen, bisher auch keine Credentials. Die Beschreibungen liegen in `internal/config` neben den Feldern. Der API_TOKEN-Satz wandert aus dem Help-Text von `Listen` in die HTTP-Beschreibung, damit `healthcheck --help` nicht behauptet, die Probe brauche einen Token. |
| E13 | **Kommandobaum, Parsen und Dispatch liegen in `cmd/recipe-reader/main.go`; `internal/cli` enthält nur die Kommandos** (Nutzerentscheidungen vom 2026-09-19, während M2). `main.go` enthält den Baum `CLI` (Felder `cli.ServeCmd`, `cli.HealthCheckCmd`, ab Task 5 `cli.MigrateCmd`), `description`, `rejectMisplacedFlags` (E11), `newParser` und `run`. `main()` ruft `run(os.Args[1:])`, loggt einen Fehler als eine Zeile und endet mit 1. `run` bindet den Signal-Kontext (`BindTo`, E8) und `cli.BuildVersion`, parst (kong ruft dabei `CLI.Validate` → `rejectMisplacedFlags`, E11) und ruft `kctx.Run()`. `internal/cli` enthält die Kommando-Typen mit ihren `Run`-Methoden und `BuildVersion`. Die Tests der Kommandozeile liegen in `cmd/recipe-reader/main_test.go` | Der Nutzer will `run` in `main.go` und kein `cli.Main`, das `main` aufruft. Den kong-Parser baut `main.go` selbst (Variante „alles in main.go“ statt eines `cli.Parse`-Helfers). Ein eigenes Paket für Baum und Dispatch ergibt keinen Sinn, in `internal/cli` gehören nur die Kommandos. Ersetzt die ursprüngliche Endform aus Task 3 (`main.go` ≤ 25 Zeilen, Imports `os` + `internal/cli`, `cli.Main`/`cli.Run`). `newParser` ist gegenüber der Skizze des Nutzers ausgelagert, damit die Parse-Tests denselben Parser nutzen, ohne ein Kommando auszuführen. |

### Risiken

| # | Risiko | Gegenmaßnahme |
| --- | --- | --- |
| R1 | kong verhält sich anders als angenommen | Am 2026-09-19 gegen den Quellcode von v1.16.1 und einen Wegwerf-Prototyp verifiziert: Default `withargs`, Reihenfolge `BeforeApply` → `Validate`, Validierung pro Pfad, Hooks auf eingebetteten Structs, Gruppen-Überschriften in der Hilfe, Struct-als-Root in Config-Tests. Im Review vom selben Tag zusätzlich: Enum-Prüfung und `Reset`-Fehler sind auf den gewählten Pfad beschränkt; die `group` eines eingebetteten Structs gilt auch für ein darin anonym eingebettetes (`Listen` in `HTTP`); `ExplicitGroups` zeigt die `Description` unter der Überschrift; ein Flag vor einem anderen Kommando fällt ohne Guard still an `serve` (→ E11); `serve --help` zeigt die App-Beschreibung nicht (→ E12). |
| R2 | Ein späteres kong-Update ändert die Hook-Reihenfolge | `TestLoad_AllowsNonLoopbackBindWithToken` schlägt dann sofort fehl. |
| R3 | `recipe-reader --help` zeigt keine `serve`-Flags mehr, nur noch Kommandos | README verweist auf `recipe-reader serve --help`. |
| R4 | Fehlermeldungen bekommen ein kong-Präfix (`serve: config: API_TOKEN …`) | Der Inhalt bleibt gleich; Tests prüfen den Inhalt, nicht das Präfix. |
| R5 | Die große mechanische Umbenennung in Task 4 | `sed`-Skript mit expliziter Abbildungstabelle; der Compiler findet jeden übersehenen Zugriff. |
| R6 | Eine externe Exec-Probe nutzt noch `--health-check` und ist nach dem Update dauerhaft *unhealthy* | Breaking Change im `BREAKING CHANGE:`-Footer der M2-PR-Beschreibung (sie wird beim Squash-Merge zum Commit-Body) und in der README dokumentiert. Das Projekt ist ein Single-User-Deployment, dessen einziger bekannter Aufrufer der eigene `HEALTHCHECK` ist. |

### Definition of Done

- [x] Alle Tasks in dieser Datei abgehakt.
- [x] Commit-Gate und Docker-Gate grün.
- [x] `cmd/recipe-reader/` enthält nur `main.go` und dessen Tests; `main.go` enthält Baum, Parsen und Dispatch; `internal/cli` enthält nur die Kommandos (E13).
- [x] Die Flag-Liste von `serve --help` ist identisch mit der Baseline von `--help`, abgesehen vom entfernten `--health-check`.
- [x] README (Commands, Configuration, Health check, Architecture) aktualisiert.

### Backlog (bewusst nicht in diesem Plan)

- `import`-Kommando für einen einmaligen Importlauf ohne Server. Offene Fragen: gleichzeitiger Betrieb mit laufendem `serve` (Session-Datei, Login-Floor) und Ausgabeformat.
- `--log-level` / `--log-format` als globale Flags.
- `server.Run` soll auch bei einem Fehler drainen (aus dem Milestone-Review von M1, F2). Heute kehrt `Run` sofort zurück, wenn `newHTTPServer` oder `ListenAndServe` scheitert (z. B. Port belegt). Der Pool ist dann geschlossen, aber der bereits gestartete Import-Zeitplan und die Shutdown-Goroutine laufen unter `ctx` weiter, bis der Aufrufer ihn abbricht. Das Verhalten ist nicht neu; beide Aufrufer beenden den Prozess danach mit 1. Seit M1 steht es im Doc-Kommentar. Vorschlag: am Anfang `ctx, cancel := context.WithCancel(ctx)` mit `defer cancel()`, `worker.Start` erst nach erfolgreichem `newHTTPServer`, und im Fehlerpfad von `ListenAndServe` `cancel()` gefolgt von `<-shutdownDone`. Danach den Absatz im Doc-Kommentar kürzen.

---

## Teil B: Zielbild

### CLI-Oberfläche

| Vorher | Nachher |
| --- | --- |
| `recipe-reader [flags]` startet Server | unverändert (Default `serve`) |
| — | `recipe-reader serve [flags]` (explizit) |
| `recipe-reader --health-check` | `recipe-reader healthcheck [--http-addr]` |
| — | `recipe-reader migrate [--db-dsn]` |
| `recipe-reader --version` | unverändert |
| `recipe-reader --help` listet alle Flags | listet Kommandos; `serve --help` listet Flags gruppiert |

### Dateistruktur nach Abschluss

```
cmd/recipe-reader/
  main.go                  # var version; CLI (Baum), description; main() → run(); run: newParser, BindTo/Bind,
                           # Parse (CLI.Validate → rejectMisplacedFlags), kctx.Run() (E13)
  main_test.go, migrate_test.go, testmain_test.go (TestMain → testdb.Main)
internal/cli/              # nur die Kommandos (E13), eine Datei pro Kommando
  serve.go                 # Paket-Doc, BuildVersion, ServeCmd → server.Run
  healthcheck.go           # HealthCheckCmd → healthcheck.Probe
  migrate.go               # MigrateCmd → db.Open
internal/server/
  server.go                # Run: Composition Root + Graceful Shutdown (war main.run)
  extractor.go             # newExtractor
  http.go                  # Timeouts + newHTTPServer (war newServer)
  fetcher.go               # newFetcher + alreadyImported
  server_test.go, fetcher_test.go, run_test.go, testmain_test.go (TestMain → testdb.Main)
internal/healthcheck/
  healthcheck.go           # Probe (war probeHealth) + dialAddr
  healthcheck_test.go
internal/config/
  config.go                # Gruppen Listen, HTTP, Database, Instagram, Extraction, LLM, Import; Config-Aggregat; Groups()
internal/db/
  connect.go               # + Open (Migrate → Connect → Seed)
```

### Feld-Abbildung `config.Config` (flach → gruppiert, Task 4)

| Alt | Neu | Alt | Neu |
| --- | --- | --- | --- |
| `HTTPAddr` | `HTTP.Addr` (via `Listen`) | `ExtractionMode` | `Extraction.Mode` |
| `CORSOrigins` | `HTTP.CORSOrigins` | `ExtractionThreshold` | `Extraction.Threshold` |
| `APIToken` | `HTTP.APIToken` | `ExtractionPublishThreshold` | `Extraction.PublishThreshold` |
| `DBDSN` | `Database.DSN` | `LLMProvider` | `LLM.Provider` |
| `InstagramUsername` | `Instagram.Username` | `LLMAPIKey` | `LLM.APIKey` |
| `InstagramPassword` | `Instagram.Password` | `LLMModel` | `LLM.Model` |
| `InstagramSessionPath` | `Instagram.SessionPath` | `LLMBaseURL` | `LLM.BaseURL` |
| `InstagramCollection` | `Instagram.Collection` | `LLMTimeout` | `LLM.Timeout` |
| `ImportInterval` | `Import.Interval` | `AnthropicAPIKey` | `LLM.AnthropicAPIKey` |
| `ImportMaxItems` | `Import.MaxItems` | `AnthropicModel` | `LLM.AnthropicModel` |
| `ImportMaxPages` | `Import.MaxPages` | `Config.LLMSettings()` | `LLM.Settings()` |
| `Version` | → `CLI.Version` in `main.go` (Task 3, E13) | `HealthCheck` | → Kommando `healthcheck` (Task 3) |

---

## Teil C: Aufgaben

### Milestones

Es gibt vier Milestones. Jeder endet in einem **releasefähigen Stand**: Tests, Lint und Build sind grün, und der Stand könnte so nach `main` gehen. Ein Milestone gilt als erreicht, wenn alle seine Tasks das zweistufige Review bestanden haben, das Milestone-Review durchgeführt und seine Befunde bewertet und abgearbeitet sind (beides siehe „Ablauf der Umsetzung“) und seine Abnahmekriterien abgehakt sind. Der nächste Milestone beginnt erst danach.

| Milestone | Tasks | Ergebnis | Für Betreiber sichtbar |
| --- | --- | --- | --- |
| **M1:** Paket `main` entschlackt | Vorbereitung, Task 1, Task 2 | Health-Probe und Composition Root liegen außerhalb von `main`; das Verhalten ist unverändert | nein |
| **M2:** kong-Kommandobaum | Task 3 | `serve` (Default) und `healthcheck` als Kommandos; `main.go` parst und dispatcht (E13) | ja: `healthcheck` ersetzt `--health-check` (**Breaking**) |
| **M3:** Konfiguration gruppiert | Task 4 | Settings in sechs Gruppen, jede validiert sich selbst; gruppierte Hilfe | ja: `serve --help` gruppiert |
| **M4:** `migrate` und Abschluss | Task 5, Abschluss-Verifikation | Kommando `migrate`; Definition of Done erfüllt | ja: neues Kommando `migrate` |

#### M1: Paket `main` entschlackt

**Ergebnis:** Die Logik verlässt das Paket `main`, noch ohne kong-Umbau. Das ist die risikoärmste Hälfte der Aufgabe: reine Verschiebungen, abgesichert durch die mitwandernden Tests.

Abnahmekriterien:
- [x] `cmd/recipe-reader/` enthält nur noch `main.go` und `version.go`; `main.go` lädt nur noch die Config und dispatcht.
- [x] `internal/healthcheck` und `internal/server` enthalten die verschobenen Tests, und alle sind grün.
- [x] `TestRun_ServesUntilCancelledThenReturnsNil` (Task 2) ist grün: `server.Run` migriert eine leere Datenbank, antwortet auf `/api/healthz` und gibt nach dem Abbruch seines Kontexts `nil` zurück.
- [x] Die Laufzeit-Stichprobe aus Task 2, Step 7 ist bestanden: Die gebaute Binary antwortet gegen eine Wegwerf-Datenbank, und nach `SIGTERM` endet der Prozess mit Exit 0.
- [x] Das Verhalten ist unverändert: Die Flag-Liste von `--help` ist identisch mit der Baseline, `--health-check` eingeschlossen.

Deckt ab: die Vorarbeit für Z3 und US3.

#### M2: kong-Kommandobaum

**Ergebnis:** Der Kern des Auftrags. Das CLI ist ein kong-Kommandobaum mit `Run`-Methoden, Bindings, `Validate` und `BeforeApply`, und `main.go` enthält den Baum, parst und dispatcht; die Kommandos liegen in `internal/cli` (E13).

Abnahmekriterien:
- [x] Die Akzeptanzkriterien von US1, US2 und US3 in Teil A sind abgehakt.
- [x] `main.go` hat seine Endform nach E13: `main()` ruft nur `run()` und endet bei einem Fehler mit 1; `run()` parst und dispatcht; kein `cli.Main`/`cli.Run`. `version.go` ist entfernt.
- [x] Das Docker-Gate ist grün, und der Image-`HEALTHCHECK` meldet mit dem neuen Kommando `healthy`.
- [x] Der Breaking Change ist in der README (Abschnitt „Commands“) und als `BREAKING CHANGE:`-Footer in der PR-Beschreibung dokumentiert (siehe „Branches und PRs“).

Deckt ab: Z1 (Kern), Z2, Z3, Z4, Z5 sowie die Entscheidungen E1 bis E8, E10, E11 und E13.

#### M3: Konfiguration gruppiert

**Ergebnis:** Die Settings sind nach Belang geschnitten und werden zwischen Kommandos geteilt, statt doppelt deklariert zu werden. Die Hilfe ist nach Themen gegliedert.

Abnahmekriterien:
- [x] Die Akzeptanzkriterien von US4 sind abgehakt.
- [x] Die Flag-Liste von `serve --help` entspricht der Baseline ohne `--health-check`.
- [x] `healthcheck` bettet `config.Listen` ein; `--http-addr` ist nur noch an einer Stelle deklariert.
- [x] Keine Env-Variable wurde umbenannt: `git diff main -- .env.example` ist leer.

Deckt ab: Z1 (Optionsgruppen), US4 und E12.

#### M4: `migrate` und Abschluss

**Ergebnis:** Das dritte Kommando ist da, und der gesamte Umbau ist end-to-end verifiziert.

Abnahmekriterien:
- [x] Die Akzeptanzkriterien von US5 sind abgehakt.
- [x] Die Abschluss-Verifikation ist vollständig abgehakt, einschließlich des Compose-End-to-End-Laufs mit `docker compose run --rm app migrate` im eigenen Projekt `recipe-reader-e2e`.
- [x] Die Definition of Done in Teil A ist abgehakt.

Deckt ab: US5, E9 und die Definition of Done.

#### Branches und PRs

Entschieden am 2026-09-19: **ein PR pro Milestone**, wie zuletzt im Repo üblich (`(1/3)`, `(2/3)` …). Die Milestones gibt es nur in diesem Plan; auf GitHub werden weder Milestones noch Issues angelegt.

| Milestone | Branch | PR-Titel |
| --- | --- | --- |
| Plan | `docs/kong-cli-refactor-plan` | `docs: add the kong CLI refactor plan` |
| M1 | `refactor/kong-cli-extract-main` | `refactor: move the health probe and the composition root out of package main (1/4)` |
| M2 | `refactor/kong-cli-command-tree` | `refactor!: dispatch through a kong command tree (2/4)` |
| M3 | `refactor/kong-cli-option-groups` | `refactor(config): split settings into per-concern kong groups (3/4)` |
| M4 | `feat/kong-cli-migrate` | `feat(cli): add a migrate command (4/4)` |

- **Plan zuerst:** Diese Datei kommt über einen eigenen Docs-PR vom Branch `docs/kong-cli-refactor-plan` nach `main`, bevor M1 beginnt. So zweigt jeder Milestone-Branch von einem `main` ab, das den Plan zum Abhaken schon enthält, und kein Milestone-PR trägt den Plan-Commit mit. Wie jeder PR wird er erst nach Freigabe durch den Nutzer geöffnet.
- **Abzweigen:** Jeder Milestone-Branch zweigt von `main` ab, nachdem der vorige PR gemerged ist (für M1 der Plan-PR). Will der Nutzer vor dem Merge weitermachen, wird der Branch auf den vorigen gestapelt (PR-Base = Branch des Vorgängers) und nach dessen Merge auf `main` rebased.
- **Inhalt:** Ein PR enthält die Task-Commits seines Milestones, die Korrektur-Commits aus dem Milestone-Review und einen letzten Commit `docs: mark M<n> done in the kong CLI plan`, der die Häkchen in dieser Datei setzt.
- **Beschreibung:** Die PR-Beschreibung nennt das Ergebnis des Milestones, listet seine abgehakten Abnahmekriterien und verlinkt diesen Plan. Sie endet mit dem `Assisted-by`-Footer und `🤖 Generated with [Claude Code](https://claude.com/claude-code)`.
- **Breaking Change:** PRs werden in diesem Repo per Squash gemergt; der Body des Squash-Commits ist die PR-Beschreibung, die Messages der einzelnen Commits gehen verloren. Der `BREAKING CHANGE:`-Footer im Commit von Task 3 allein erreicht `main` also nicht. Der PR für **M2** trägt den Breaking Change (`--health-check` → `healthcheck`) deshalb zweimal: als Hinweis am Anfang der Beschreibung und als Conventional-Commits-Footer `BREAKING CHANGE: --health-check is replaced by the healthcheck command; external exec probes must be updated.` im letzten Absatz, direkt vor `Assisted-by`. Das `!` im PR-Titel markiert ihn zusätzlich.

### Aufgabenliste

- [x] **Plan-PR:** diese Datei über `docs/kong-cli-refactor-plan` nach `main` bringen
- [x] **M1: Paket `main` entschlackt**
  - [x] **Vorbereitung:** Branch, Werkzeug-Versionen und Baseline
  - [x] **Task 1:** Health-Probe nach `internal/healthcheck` (S)
  - [x] **Task 2:** Composition Root nach `internal/server` (M)
  - [x] **Milestone-Review:** Review durch einen neuen Subagent; Befunde von einem weiteren Subagent bewertet und abgearbeitet
- [x] **M2: kong-Kommandobaum**
  - [x] **Task 3:** kong-Kommandobaum in `internal/cli` (nach E13: Baum in `main.go`, Kommandos in `internal/cli`); `healthcheck` ersetzt `--health-check` (L)
  - [x] **Milestone-Review:** Review durch einen neuen Subagent; Befunde von einem weiteren Subagent bewertet und abgearbeitet
- [x] **M3: Konfiguration gruppiert**
  - [x] **Task 4:** Settings in Optionsgruppen pro Belang (M)
  - [x] **Milestone-Review:** Review durch einen neuen Subagent; Befunde von einem weiteren Subagent bewertet und abgearbeitet
- [x] **M4: `migrate` und Abschluss**
  - [x] **Task 5:** Kommando `migrate` (S)
  - [x] **Milestone-Review:** Review durch einen neuen Subagent; Befunde von einem weiteren Subagent bewertet und abgearbeitet
  - [x] **Abschluss-Verifikation**

Jeder Task endet grün (Tests, Lint, Build) und ist einzeln reviewbar. Die Reihenfolge ist zwingend, weil jeder Task auf den Paketen des vorigen aufbaut.

### Ablauf der Umsetzung (Subagent-Driven)

Entschieden am 2026-09-19. **Start erst nach Freigabe durch den Nutzer.**

- **Orchestrierung:** Die Hauptsession führt die Umsetzung mit `superpowers:subagent-driven-development`. Sie selbst schreibt keinen Produktionscode, sondern verteilt die Tasks und prüft die Ergebnisse.
- **Ein frischer Subagent pro Task**, in der Reihenfolge Vorbereitung → Task 1 → 2 → 3 → 4 → 5 → Abschluss-Verifikation. **Strikt nacheinander, nie parallel:** Jeder Task baut auf den Paketen des vorigen auf. Die Tasks eines Milestones arbeiten auf dessen Branch (siehe „Branches und PRs“).
- **Auftrag an den Subagent:** der vollständige Task-Abschnitt aus dieser Datei samt Files- und Interfaces-Block, dazu die *Global Constraints*, die Tabelle *Entscheidungen* (E1–E13) und die Zeile R1 aus *Risiken* in Teil A. Mehr Kontext bekommt er nicht. Was er aus früheren Tasks braucht, steht in deren „Produces“. Die Entscheidungen gehören dazu, weil die Tasks auf ihnen beruhen, ohne sie zu wiederholen: Wer E4, E6 oder E8 nicht kennt, „verbessert“ `BeforeApply` zu `AfterApply`, `BindTo` zu `Bind` oder `main` zu `FatalIfErrorf` und bricht damit, was die Tests absichern. R1 ist die Referenz, an der er eine Abweichung erkennt (siehe unten).
- **Zweistufiges Review nach jedem Task**, bevor der nächste startet:
  1. **Plan-Treue:** Wurden alle Steps umgesetzt, Kommentare beim Verschieben wortgleich übernommen, Flag- und Env-Namen unverändert gelassen, keine Arbeit aus späteren Tasks vorgezogen?
  2. **Code-Qualität:** Korrektheit, Tests und Kommentarstil; außerdem müssen Commit-Gate bzw. Docker-Gate nachweislich grün gelaufen sein (Ausgabe gesehen, nicht nur behauptet).
  Befunde behebt ein Subagent vor dem nächsten Task. Ein Task, der das Review nicht besteht, blockiert den nächsten.
- **Commit:** genau ein Commit pro Task mit der Message aus dem jeweiligen Commit-Step, erst nach bestandenem Review. Ausnahmen sind reine Versions-Bumps (Vorbereitung, Step 2; Task 3, Step 0): Sie bekommen einen eigenen `build:`-Commit vor dem Task-Commit, damit der Refactoring-Commit keine fremde Änderung mitträgt.
- **Abhaken:** Nach jedem bestandenen Task hakt die Hauptsession dessen Steps und den Eintrag in der Aufgabenliste in dieser Datei ab. Einen Milestone hakt sie erst ab, wenn alle seine Abnahmekriterien nachgewiesen sind und sein Milestone-Review abgeschlossen ist.
- **Milestone-Review (Pflicht, vom Nutzer am 2026-09-19 ergänzt):** Nach jeder Fertigstellung eines Milestones MUSS ein neuer Subagent ein Review durchführen. Ein weiterer Subagent soll die gefundenen Review-Findings bewerten und abarbeiten.
  1. **Review:** Ein frischer Subagent auf dem stärksten verfügbaren Modell prüft den gesamten Diff des Milestones (`git merge-base main HEAD`..`HEAD`) gegen die Abnahmekriterien des Milestones, die *Global Constraints* und die Entscheidungen E1–E13. Er schreibt jeden Befund mit Schweregrad (Critical/Important/Minor), `Datei:Zeile` und Begründung in eine Datei. Das Review ist rein lesend.
  2. **Bewertung und Abarbeitung:** Ein zweiter, frischer Subagent bewertet jeden Befund mit Begründung als *berechtigt*, *unberechtigt*, *gehört in einen späteren Task* oder *widerspricht dem Plan*. Die berechtigten behebt er. Danach lässt er das Commit-Gate (bzw. das Docker-Gate, wenn `Dockerfile` oder `scripts/` betroffen sind) laufen und committet die Korrekturen mit Footer. Befunde, die dem Plan widersprechen, behebt er nicht; die Hauptsession legt sie dem Nutzer zur Entscheidung vor.
  3. Die Hauptsession prüft die Bewertung und die Korrektur-Commits und legt beides zusammen mit der Milestone-Abnahme vor.

  Bei M4 läuft das Milestone-Review nach Task 5 und vor der Abschluss-Verifikation, damit diese die Korrekturen mit abdeckt.
- **Milestone-Abnahme:** Ist ein Milestone erreicht, hält die Hauptsession an und legt dem Nutzer Ergebnis und Abnahmekriterien vor. Den PR öffnet sie erst nach dessen Freigabe. Der nächste Milestone beginnt erst nach dem Merge oder auf ausdrücklichen Wunsch gestapelt.
- **Abweichungen:** Muss ein Subagent vom Plan abweichen (z. B. weil kong sich anders verhält als unter R1 verifiziert), hält er an und meldet es. Die Hauptsession entscheidet dann oder fragt den Nutzer und hält die Abweichung im betroffenen Task fest.
- **PR:** einer pro Milestone, geöffnet erst nach der Milestone-Abnahme durch den Nutzer; Titel und Branch stehen unter „Branches und PRs“.

---

### Vorbereitung: Branch, Werkzeug-Versionen und Baseline

- [x] **Step 1: Branch für M1 anlegen.** Voraussetzung ist, dass der Plan-PR gemerged ist (siehe „Branches und PRs“). Ist die Datei noch nicht auf `main`, hier anhalten und den Nutzer fragen, statt von einem anderen Branch abzuzweigen.

```bash
git switch main && git pull --ff-only
test -f docs/superpowers/plans/2026-09-19-kong-cli-commands.md || { echo "the plan is not on main yet"; exit 1; }
git switch -c refactor/kong-cli-extract-main
```

(Die Branches für M2 bis M4 entstehen nach demselben Muster zu Beginn des jeweiligen Milestones; siehe „Branches und PRs“.)

- [x] **Step 2: Werkzeug-Versionen prüfen** (Pflicht laut `~/.claude/CLAUDE.md`: immer die neueste stabile Go- und golangci-lint-Version)

```bash
go version
grep '^go ' go.mod
curl -fsS 'https://go.dev/VERSION?m=text' | head -n 1
/home/ripmav/.local/bin/golangci-lint --version
curl -fsS https://api.github.com/repos/golangci/golangci-lint/releases/latest | jq -r .tag_name
```

Erwartet (Stand 2026-09-19): `go1.27.1` lokal, in `go.mod` und als neueste Version; golangci-lint `v2.13.2` lokal und als neueste Version. Dann ist nichts zu tun.

- Ist golangci-lint veraltet: mit dem Install-Befehl aus `~/.claude/CLAUDE.md` aktualisieren. Das ändert nur das Werkzeug, es gibt keinen Commit.
- Gibt es eine neuere stabile Go-Version: lokal installieren, dann die `go`-Direktive (`go mod edit -go=<version>`), das `golang:<major.minor>-alpine`-Image im `Dockerfile` und `go-version` in `.github/workflows/ci.yml` anheben. Danach laufen Commit-Gate und Docker-Gate, und es folgt ein eigener Commit `build: bump Go to <version>` vor Task 1.

- [x] **Step 3: Ausgangszustand prüfen**

```bash
go test ./... 2>&1 | tail -n 20
go run ./cmd/recipe-reader --help | grep -o -- '--[a-z][a-z-]*' | sort -u | tr '\n' ' '
```

Erwartet: Die Tests sind grün. Die Flag-Liste entspricht der am 2026-09-19 erhobenen **Baseline**, gegen die Task 4 prüft:

```
--anthropic-model --cors-origin --db-dsn --extraction-confidence-threshold --extraction-mode --extraction-publish-threshold --health-check --help --http-addr --import-interval --import-max-items --import-max-pages --instagram-collection --instagram-session-path --instagram-username --llm-base-url --llm-model --llm-provider --llm-timeout --version
```

Weicht sie ab (weil seit dem 2026-09-19 ein Flag hinzugekommen ist), die Baseline hier aktualisieren, bevor Task 1 beginnt.

---

### Task 1: Health-Probe nach `internal/healthcheck`

Reine Verschiebung. `probeHealth` wird zu `healthcheck.Probe`, weil es außerhalb von `main` gebraucht wird (Task 3).

**Files:**
- Move: `cmd/recipe-reader/healthcheck.go` → `internal/healthcheck/healthcheck.go`
- Move: `cmd/recipe-reader/healthcheck_test.go` → `internal/healthcheck/healthcheck_test.go`
- Modify: `cmd/recipe-reader/main.go` (Import + Aufruf in `run()`)

**Interfaces:**
- Consumes: —
- Produces: `func healthcheck.Probe(ctx context.Context, listenAddr string) error` (Semantik unverändert: nil nur bei 200 und `{"status":"ok"}`; loopback für unspezifizierte Hosts; 3-s-Timeout)

- [x] **Step 1: Test verschieben und umstellen**

```bash
mkdir -p internal/healthcheck
git mv cmd/recipe-reader/healthcheck_test.go internal/healthcheck/healthcheck_test.go
sed -i 's/^package main$/package healthcheck/; s/probeHealth/Probe/g' internal/healthcheck/healthcheck_test.go
```

(`s/probeHealth/Probe/g` ist case-sensitiv; Testnamen wie `TestProbeHealth_…` bleiben erhalten.)

- [x] **Step 2: Test laufen lassen, er muss fehlschlagen**

Run: `go test ./internal/healthcheck/`
Expected: FAIL, `undefined: Probe` und `undefined: dialAddr`

- [x] **Step 3: Implementierung verschieben**

```bash
git mv cmd/recipe-reader/healthcheck.go internal/healthcheck/healthcheck.go
sed -i 's/probeHealth/Probe/g' internal/healthcheck/healthcheck.go
```

Dann in `internal/healthcheck/healthcheck.go` die Zeile `package main` ersetzen durch:

```go
// Package healthcheck probes a running recipe-reader over HTTP. It is the
// client half of GET /api/healthz, and what the binary's own health check runs,
// so the runtime image needs no curl or wget.
package healthcheck
```

- [x] **Step 4: `main.go` umstellen**

In `cmd/recipe-reader/main.go` den Import `"github.com/sBurmester/recipe-reader/internal/healthcheck"` ergänzen (alphabetisch nach `.../internal/extraction`) und in `run()` ersetzen:

```go
	// A probe of another process, not a server: it touches no database and
	// starts nothing. See probeHealth.
	if cfg.HealthCheck {
		return probeHealth(context.Background(), cfg.HTTPAddr)
	}
```

durch

```go
	// A probe of another process, not a server: it touches no database and
	// starts nothing. See healthcheck.Probe.
	if cfg.HealthCheck {
		return healthcheck.Probe(context.Background(), cfg.HTTPAddr)
	}
```

- [x] **Step 5: Tests laufen lassen, sie müssen grün sein**

Run: `go test ./internal/healthcheck/ ./cmd/recipe-reader/ && go build ./...`
Expected: PASS für beide Pakete, Build ohne Fehler.

- [x] **Step 6: Commit-Gate und Commit**

```bash
git add -A cmd/recipe-reader internal/healthcheck
git commit -m "refactor: move the health probe out of package main" -m "probeHealth becomes healthcheck.Probe so the command tree can reach it; behaviour and tests are unchanged."
```

(Footer laut Global Constraints anhängen.)

---

### Task 2: Composition Root nach `internal/server`

Reine Verschiebung von `run()` (ohne Config-Laden und Signal-Handling), `newExtractor`, `newServer`, der Timeouts, `newFetcher` und `alreadyImported`. `main.go` wird danach zu einem kurzen Dispatcher. Der Rumpf von `run()` behält seine Kommentare wortgleich.

**Files:**
- Create: `internal/server/server.go` (`Run`)
- Create: `internal/server/extractor.go` (`newExtractor`, extrahiert aus `main.go`)
- Create: `internal/server/http.go` (Timeouts + `newHTTPServer`, extrahiert aus `main.go`)
- Move: `cmd/recipe-reader/fetcher.go` → `internal/server/fetcher.go` (+ `alreadyImported`)
- Move: `cmd/recipe-reader/main_test.go` → `internal/server/server_test.go`
- Move: `cmd/recipe-reader/fetcher_test.go` → `internal/server/fetcher_test.go`
- Create: `internal/server/run_test.go` (`TestRun_ServesUntilCancelledThenReturnsNil`), `internal/server/testmain_test.go`
- Modify: `cmd/recipe-reader/main.go` (komplett neu, siehe Step 5)
- Modify (nur Kommentare, die `main.go` bzw. `main` als Ort der Verdrahtung nennen): `internal/instagram/fetcher_adapter.go:27,34`, `internal/pipeline/pipeline.go:23`, `internal/pipeline/worker_lifecycle_test.go:65`, `internal/config/bounds_test.go:9`

**Interfaces:**
- Consumes: `healthcheck.Probe` (Task 1), `config.Config`/`config.Load` (unverändert), `testdb.NewDatabase(t, name) string`, `testdb.Main(m)`
- Produces: `func server.Run(ctx context.Context, cfg config.Config, version string) error`. Serviert, bis `ctx` abgebrochen wird, und wartet vor der Rückkehr auf Shutdown und Import. Intern: `newExtractor(cfg config.Config) (extraction.Extractor, error)`, `newHTTPServer(cfg config.Config, deps api.Deps) (*http.Server, error)`, `newFetcher(...)`, `alreadyImported(...)`, jeweils mit unveränderter Signatur.

- [x] **Step 1: Tests verschieben und umstellen**

```bash
mkdir -p internal/server
git mv cmd/recipe-reader/main_test.go internal/server/server_test.go
git mv cmd/recipe-reader/fetcher_test.go internal/server/fetcher_test.go
sed -i 's/^package main$/package server/; s/\bnewServer\b/newHTTPServer/g; s/run()/Run/g' \
  internal/server/server_test.go internal/server/fetcher_test.go
# Run is reachable from a test from now on (run_test.go below), just not without a database.
sed -i 's/is the wiring Run has no seam for:/is the wiring Run reaches only with a database:/' \
  internal/server/server_test.go
```

`internal/server/testmain_test.go` anlegen (gleicher Wortlaut wie in `internal/db`, `internal/api`, `internal/repository` und `internal/pipeline`):

```go
package server

import (
	"testing"

	"github.com/sBurmester/recipe-reader/internal/db/testdb"
)

// TestMain shares one Postgres container across this package's tests.
func TestMain(m *testing.M) { testdb.Main(m) }
```

`internal/server/run_test.go` anlegen. `main.run()` war der einzige Code ohne jeden Test: Das Delivery-Review vom 2026-09-11 (D5) nennt 0 % Abdeckung und eine Shutdown-Reihenfolge, die „einen Test verdient“. Mit `ctx` als Parameter ist `server.Run` erstmals testbar. Der Test ersetzt nicht die Stichprobe in Step 7, die zusätzlich die Signal-Verdrahtung der gebauten Binary prüft.

```go
package server

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/sBurmester/recipe-reader/internal/db/testdb"
	"github.com/sBurmester/recipe-reader/internal/healthcheck"
)

// Run is the composition root, and cancelling its context is how the process
// stops: SIGTERM from Docker, Ctrl-C in a terminal. Against an empty database it
// has to migrate, seed and serve, and once cancelled it has to drain and return
// nil — not an error, and not never. Nothing else reaches the shutdown sequence,
// whose order (server, then import, then pool) is the subtle part.
func TestRun_ServesUntilCancelledThenReturnsNil(t *testing.T) {
	cfg := baseConfig("rule")
	cfg.HTTPAddr = freeLoopbackAddr(t)
	cfg.DBDSN = testdb.NewDatabase(t, "server_run")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- Run(ctx, cfg, "v0.0.0-test") }()

	deadline := time.Now().Add(30 * time.Second)
	for healthcheck.Probe(context.Background(), cfg.HTTPAddr) != nil {
		select {
		case err := <-done:
			t.Fatalf("Run() returned before it served: %v", err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("GET /api/healthz did not answer ok within 30s")
		}
		time.Sleep(100 * time.Millisecond)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run() error = %v after cancellation, want nil", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("Run() did not return within 15s of cancellation; its shutdown budget is 10s")
	}
}

// freeLoopbackAddr returns a loopback address nothing is listening on. The
// listener is closed before Run binds the port, so another process could take
// it in between; on a test machine that race is theoretical.
func freeLoopbackAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := l.Addr().String()
	if err := l.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	return addr
}
```

- [x] **Step 2: Tests laufen lassen, sie müssen fehlschlagen**

Run: `go test ./internal/server/`
Expected: FAIL, `undefined: newExtractor`, `undefined: newHTTPServer`, `undefined: newFetcher`, `undefined: Run`

- [x] **Step 3: Funktionen aus `main.go` extrahieren** (vor Step 5, weil `main.go` dort überschrieben wird)

```bash
{
cat <<'EOF'
package server

import (
	"fmt"
	"log/slog"

	"github.com/sBurmester/recipe-reader/internal/config"
	"github.com/sBurmester/recipe-reader/internal/extraction"
)

EOF
sed -n '/^\/\/ newExtractor builds/,/^}$/p' cmd/recipe-reader/main.go
} > internal/server/extractor.go

{
cat <<'EOF'
package server

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/sBurmester/recipe-reader/internal/api"
	"github.com/sBurmester/recipe-reader/internal/config"
	"github.com/sBurmester/recipe-reader/internal/webui"
)

EOF
sed -n '/^\/\/ Server timeouts\./,/^}$/p' cmd/recipe-reader/main.go
} > internal/server/http.go
sed -i 's/\bnewServer\b/newHTTPServer/g; s/run()/Run/g; s/— Run itself has no seam at all, and/— Run needs a database and a socket, and/' internal/server/http.go

git mv cmd/recipe-reader/fetcher.go internal/server/fetcher.go
sed -i 's/^package main$/package server/; s/^\t"context"$/\t"context"\n\t"errors"/' internal/server/fetcher.go
{ echo; sed -n '/^\/\/ alreadyImported adapts/,/^}$/p' cmd/recipe-reader/main.go; } >> internal/server/fetcher.go
```

Prüfen: `http.go` enthält den `const (...)`-Block mit den vier Timeouts **und** `func newHTTPServer`. In den Kommentaren steht jetzt „separated from Run“ bzw. „Run needs a database and a socket“. „Run itself has no seam at all“ wäre mit `run_test.go` aus Step 1 nicht mehr wahr.

- [x] **Step 4: `internal/server/server.go` anlegen**

```go
// Package server is the composition root of the long-running process: it
// migrates and seeds the database, wires the extraction engine, the Instagram
// client, the import worker and the HTTP API together, and serves until its
// context is cancelled.
package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/sBurmester/recipe-reader/internal/api"
	"github.com/sBurmester/recipe-reader/internal/config"
	"github.com/sBurmester/recipe-reader/internal/db"
	"github.com/sBurmester/recipe-reader/internal/instagram"
	"github.com/sBurmester/recipe-reader/internal/pipeline"
	"github.com/sBurmester/recipe-reader/internal/repository"
)

// Run serves until ctx is cancelled, then drains in-flight requests and any
// running import before it returns. version is logged at startup and reported
// by GET /api/healthz.
func Run(ctx context.Context, cfg config.Config, version string) error {
	// Before anything external: a misconfigured extraction mode is a startup
	// error, and reporting it after a database migration has already run buries
	// it under whatever that says instead.
	extractor, err := newExtractor(cfg)
	if err != nil {
		return err
	}

	if err := db.Migrate(cfg.DBDSN); err != nil {
		return err
	}
	pool, err := db.Connect(ctx, cfg.DBDSN)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := db.Seed(ctx, pool); err != nil {
		return err
	}

	recipes := repository.NewRecipeRepository(pool)
	lookups := repository.NewLookupRepository(pool)

	// Instagram is optional: without an account configured the server still
	// serves the API over whatever is already in the database, and only the
	// import worker is withheld. A failed login no longer withholds it — see
	// newFetcher.
	fetcher, loginErr := newFetcher(ctx, cfg, instagram.NewClient(), recipes)

	var worker *pipeline.Worker
	if fetcher != nil {
		p := &pipeline.Pipeline{
			Fetcher: fetcher, Extractor: extractor,
			Recipes: recipes,
			// The same lookup repository the API serves the category picker
			// from, which is the point: an import may only attach a category
			// the picker already offers. Without it an attacker-authored
			// caption can talk the model into proposing any string and the
			// import creates that row for every user.
			Categories:       lookups,
			Threshold:        cfg.ExtractionThreshold,
			PublishThreshold: cfg.ExtractionPublishThreshold,
		}
		worker = pipeline.NewWorker(p, cfg.ImportInterval)
		if loginErr != nil {
			// Reported now rather than after the first scheduled run, which
			// is hours away: the import page shows it as the last error.
			// Wrapped in pipeline.ErrLogin so the status endpoint classifies it
			// as an authentication problem rather than as a generic run
			// failure — it is the one error that reaches Status without a run.
			worker.RecordFailure(fmt.Errorf("%w at startup; the next import retries it: %w", pipeline.ErrLogin, loginErr))
		}
		worker.Start(ctx)
	}

	srv, err := newHTTPServer(cfg, api.Deps{Recipes: recipes, Lookups: lookups, Worker: worker, Version: version})
	if err != nil {
		return err
	}

	// Shutdown runs on cancellation; Run waits for it to finish draining before
	// returning, so the deferred pool.Close above cannot pull the database out
	// from under a request that is still being served — or an import.
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			slog.Error("graceful shutdown failed", "error", err)
		}
		// Imports second, once no handler is left to trigger another. ctx is
		// already cancelled, so a run in flight stops at its next check;
		// waiting for it keeps the pool open until it has, and gets its outcome
		// logged instead of lost with the process. It shares the server's
		// budget rather than extending it.
		if worker != nil {
			if err := worker.Wait(shutdownCtx); err != nil {
				slog.Error("import did not stop within the shutdown budget; abandoning it", "error", err)
			}
		}
	}()

	slog.Info("recipe-reader listening", "addr", cfg.HTTPAddr, "version", version)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	<-shutdownDone
	return nil
}
```

- [x] **Step 5: `cmd/recipe-reader/main.go` durch den Dispatcher ersetzen**

```go
// Command recipe-reader loads its configuration and hands over to the server —
// or, with --health-check, probes one that is already running.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/sBurmester/recipe-reader/internal/config"
	"github.com/sBurmester/recipe-reader/internal/healthcheck"
	"github.com/sBurmester/recipe-reader/internal/server"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load(version)
	if err != nil {
		return err
	}
	// A probe of another process, not a server: it touches no database and
	// starts nothing. See healthcheck.Probe.
	if cfg.HealthCheck {
		return healthcheck.Probe(context.Background(), cfg.HTTPAddr)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	return server.Run(ctx, cfg, version)
}
```

Kommentare in anderen Paketen nachziehen, die `main.go` bzw. `main` als Ort der Verdrahtung nennen. Das ist jetzt `internal/server`:

```bash
sed -i 's/pipeline\. main\.go is the only place the two meet\./pipeline. internal\/server is the only place the two meet./; s/every time; main\.go supplies it/every time; internal\/server supplies it/' \
  internal/instagram/fetcher_adapter.go
sed -i 's/to consider for import\. main\.go$/to consider for import. internal\/server/' internal/pipeline/pipeline.go
sed -i "s/goroutine that calls Start — main's\./goroutine that calls Start — server.Run's./" internal/pipeline/worker_lifecycle_test.go
sed -i "s/Worker\.Start, which is main's —/Worker.Start, which is server.Run's —/" internal/config/bounds_test.go
grep -rn "main\.go\|main's" internal   # Expected: keine Treffer
```

- [x] **Step 6: Tests laufen lassen, sie müssen grün sein** (Docker muss laufen: `run_test.go` braucht Postgres)

Run: `go build ./... && go test ./internal/server/ ./internal/healthcheck/ ./cmd/recipe-reader/`
Expected: PASS für `internal/server` (einschließlich `TestRun_ServesUntilCancelledThenReturnsNil`) und `internal/healthcheck`; `cmd/recipe-reader` meldet `[no test files]`.

- [x] **Step 7: Laufzeit-Stichprobe** (die gebaute Binary einmal echt starten und per `SIGTERM` beenden; das prüft die Signal-Verdrahtung in `main`, die `run_test.go` nicht erreicht)

Die Stichprobe läuft gegen eine Wegwerf-Datenbank, **nicht** über `make db-up`. Das würde den `db`-Dienst des echten Compose-Projekts mit dem Volume `db-data` starten. Postgres setzt das Passwort nur bei der ersten Initialisierung; bei einem bestehenden Volume mit anderem Passwort scheitert die Anmeldung, und die Stichprobe liefe gegen echte Daten. Port `18080` muss frei sein.

```bash
db=rr-probe-db-$$
docker run -d --rm --name "$db" -p 127.0.0.1::5432 \
  -e POSTGRES_DB=recipes -e POSTGRES_USER=recipes -e POSTGRES_PASSWORD=probe \
  --health-cmd "pg_isready -U recipes" --health-interval 1s --health-retries 60 \
  postgres:18-alpine >/dev/null
for i in $(seq 1 60); do
  [ "$(docker inspect -f '{{.State.Health.Status}}' "$db")" = healthy ] && break
  sleep 1
done
port=$(docker port "$db" 5432/tcp | head -n 1 | sed 's/.*://')
bin=$(mktemp -d)/recipe-reader && go build -o "$bin" ./cmd/recipe-reader
export HTTP_ADDR=127.0.0.1:18080 DB_DSN="postgres://recipes:probe@127.0.0.1:$port/recipes?sslmode=disable"
"$bin" & pid=$!
for i in $(seq 1 60); do "$bin" --health-check 2>/dev/null && break; sleep 0.5; done
"$bin" --health-check && echo "probe ok"
kill -TERM "$pid"; wait "$pid"; echo "exit=$?"
docker rm -f "$db" >/dev/null
```

Expected: `probe ok`, danach `exit=0` und kein `graceful shutdown failed` im Log. Die Binary wird direkt gestartet und nicht über `go run`, damit das `SIGTERM` den Prozess selbst erreicht. Statt `sleep` wird gepollt, und zwar mit der Probe der Binary selbst. `docker rm -f` auch dann ausführen, wenn ein Schritt davor scheitert.

- [x] **Step 8: Commit-Gate und Commit**

```bash
git add -A cmd/recipe-reader internal
git commit -m "refactor: move the composition root into internal/server" -m "run(), newExtractor, newServer (now newHTTPServer), the server timeouts, newFetcher and alreadyImported move out of package main unchanged; main only loads config and dispatches."
```

**Abweichungen aus dem Milestone-Review von M1** (vom Nutzer am 2026-09-19 entschieden, eigene Commits nach dem Task-Commit):
- Der grep in Step 5 (`main\.go\|main's`) trifft auch `domain's` in `internal/api/handlers_recipes.go` und `handlers_recipes_test.go`. Das sind zwei erwartete Fehltreffer; künftige greps dieser Art nutzen Wortgrenzen (`\bmain's`).
- **F1:** Der Doc-Kommentar von `run_test.go` versprach, die Shutdown-Reihenfolge abzusichern. Das kann der Test nicht: Ohne Instagram-Account gibt es keinen Worker, und auch ohne `<-shutdownDone` bleibt er grün. Deshalb konfiguriert der Test jetzt einen Account ohne Passwort, sodass ein Worker existiert, und prüft, dass `GET /api/import/status` den abgelehnten Login als `instagram_auth` meldet. Das ist der erste Test des `pipeline.ErrLogin`-Wraps in `Run`. Der Kommentar sagt jetzt ausdrücklich, dass die Drain-Reihenfolge nicht geprüft wird.
- **F2:** Der Doc-Kommentar von `Run` beschreibt jetzt auch den Fehlerpfad, der ohne Drain zurückkehrt. Die Verhaltenskorrektur steht im Backlog in Teil A.
- **F3/F5:** In `http.go` ist die Begründung für `newHTTPServer` korrigiert (`Run` hat keinen Signal-Handler mehr). Die per `sed` verlängerten Kommentarzeilen in `server_test.go`, `http.go` und `bounds_test.go` sind auf 80 Spalten umgebrochen. `worker_lifecycle_test.go` folgt in Task 4, Step 5c.
- **F6:** Die drei Tests von `newHTTPServer` heißen jetzt `TestNewHTTPServer_*`, weil `\bnewServer\b` nicht in `TestNewServer_` greift. `TestLLMSettings_FallbacksAreProviderScoped` zieht in Task 4 nach `internal/config`.
- **F4/F7** (ohne Planbezug): In `internal/pipeline` und `.github/workflows/ci.yml` sind veraltete Kommentare korrigiert.

---

### Task 3: kong-Kommandobaum in `internal/cli`; `healthcheck` ersetzt `--health-check`

Kern des Umbaus. `config.Load` entfällt. kong parst jetzt einen Baum `CLI{Version, Serve, HealthCheck}`, ruft `BeforeApply` und `Validate` am `Config` auf (nur bei `serve`) und dispatcht über `kctx.Run()` mit gebundenem `context.Context` und `BuildVersion`. `main.go` erreicht seine Endform.

**Files:**
- Create: `internal/cli/cli.go`, `internal/cli/serve.go`, `internal/cli/healthcheck.go`, `internal/cli/cli_test.go`
- Modify: `internal/config/config.go` (Version/HealthCheck/description/Load raus; `BeforeApply`, `Validate` exportiert)
- Modify: `internal/config/config_test.go` (`loadArgs` über `kong.New`)
- Modify: `internal/config/credentials_test.go` (Description-Test zieht nach `cli`)
- Delete: `internal/config/version_test.go` (zieht nach `cli`), `cmd/recipe-reader/version.go` (geht in `main.go` auf)
- Modify: `cmd/recipe-reader/main.go` (Endform)
- Modify: `internal/healthcheck/healthcheck.go:25` (Doc-Kommentar)
- Modify (nur Kommentare, die `config.validate` bzw. `config.Load` nennen): `internal/pipeline/worker.go:74`, `internal/pipeline/worker_lifecycle_test.go:65`, `internal/api/middleware.go:15,173`
- Modify: `Dockerfile:46-55`, `docker-compose.yml:53`, `scripts/smoke-test-image.sh:78-81`, `README.md`
- Evtl. Modify (Step 0, eigener Commit): `Dockerfile` (`FROM`-Zeilen), `.github/workflows/ci.yml` (`node-version`)

**Interfaces:**
- Consumes: `server.Run` (Task 2), `healthcheck.Probe` (Task 1)
- Produces:
  - `func cli.Main(args []string, version string) int` (0 oder 1)
  - `func cli.Run(ctx context.Context, args []string, version string, opts ...kong.Option) error`
  - `type cli.CLI struct { Version kong.VersionFlag; Serve ServeCmd; HealthCheck HealthCheckCmd }`
  - `type cli.BuildVersion string`
  - `func newParser(root *CLI, version string, opts ...kong.Option) (*kong.Kong, error)` (paketintern, für Tests)
  - `func rejectMisplacedFlags(kctx *kong.Context) error` (paketintern; `Run` ruft es zwischen `Parse` und `kctx.Run()`, E11)
  - Test-Helfer in `cli_test.go`: `clearEnv(t)`, `credentialEnvs`, `unreachableDSN`, `parse`, `exitOf`, `panicOnExit`
  - `func (c *ServeCmd) Run(ctx context.Context, version BuildVersion) error`; `ServeCmd{ Config config.Config \`embed:""\` }`
  - `func (c *HealthCheckCmd) Run(ctx context.Context) error`; `HealthCheckCmd{ Addr string }`
  - `func (c *config.Config) BeforeApply() error`, `func (c config.Config) Validate() error`

- [x] **Step 0: Basis-Images im `Dockerfile` prüfen** (Pflicht laut `~/.claude/CLAUDE.md`, weil dieser Task das `Dockerfile` ändert)

```bash
grep -n '^FROM' Dockerfile
latest_tag() {
  curl -fsS "https://hub.docker.com/v2/repositories/library/$1/tags?page_size=100&name=$2" |
    jq -r '.results[].name' | grep -E "$3" | sort -V | tail -n 1
}
latest_tag golang -alpine '^1\.[0-9]+-alpine$'
latest_tag node -alpine '^[0-9]+-alpine$'
latest_tag alpine 3. '^3\.[0-9]+$'
```

Erwartet (Stand 2026-09-19, so auch geprüft): `1.27-alpine`, `26-alpine`, `3.24`, also genau das, was im `Dockerfile` steht. Dann ist nichts zu tun.

- Liegt ein neuerer Tag vor: `FROM`-Zeile anheben. Für `golang:` gilt: nur passend zur `go`-Direktive in `go.mod`; ein neueres Go gehört in Vorbereitung, Step 2. Für `node:` nur gerade Major-Versionen (LTS-Linie) nehmen und `node-version` in `.github/workflows/ci.yml` mitziehen.
- `postgres:18-alpine` (Compose, Smoke-Test, `testdb`) bleibt unangetastet. Ein Major-Wechsel braucht das Upgrade-Verfahren aus der README („Upgrading from Postgres 17“) und ist kein Nebenbei-Bump.
- Gibt es einen Bump: Docker-Gate laufen lassen und **vor** allen weiteren Steps separat committen: `git add Dockerfile .github/workflows/ci.yml && git commit -m "build: bump the image base versions"` (mit Footer). So trägt der Refactoring-Commit keine Versionsänderung.

- [x] **Step 1: Failing Tests für den Kommandobaum schreiben** (`internal/cli/cli_test.go`)

Alle Tests, die parsen oder `Run`/`Main` aufrufen, beginnen mit `clearEnv(t)`. Die Config-Tests machen es mit ihrem eigenen `clearEnv` genauso. Ohne den Helper hängt das Ergebnis von der Shell ab, in der `go test` läuft: Ein exportiertes `HTTP_ADDR=:8080` ohne `API_TOKEN` oder ein `IMPORT_INTERVAL=0` ließe `serve` scheitern, bevor der Test prüft, was er prüfen soll. Der Helper leitet die Namen aus dem kong-Modell ab, damit ein neues Flag automatisch mit abgedeckt ist.

```go
package cli

import (
	"bytes"
	"context"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/alecthomas/kong"

	"github.com/sBurmester/recipe-reader/internal/api"
)

const testVersion = "v0.0.0-test"

// credentialEnvs are the four settings with no flag form. The model cannot
// list them — they are not flags — so they are named here, once, for clearEnv
// and for the --help description test.
var credentialEnvs = []string{"API_TOKEN", "INSTAGRAM_PASSWORD", "LLM_API_KEY", "ANTHROPIC_API_KEY"}

// unreachableDSN points at a port nothing listens on. A test that expects serve
// to fail before it touches the database passes it, so that should serve ever
// get further, it fails at once instead of migrating whatever localhost:5432
// holds and then serving until the test binary times out.
const unreachableDSN = "postgres://recipes:x@127.0.0.1:1/recipes?sslmode=disable&connect_timeout=1"

// clearEnv unsets every environment variable the command line reads, for the
// duration of the test: each flag's env tag, taken from the parser's model so a
// flag added later is covered without anyone remembering it here, and the
// credentials. As in internal/config, t.Setenv registers the restore (and
// guards against t.Parallel), and os.Unsetenv then makes the variables absent
// rather than empty.
func clearEnv(t *testing.T) {
	t.Helper()
	var root CLI
	parser, err := newParser(&root, testVersion)
	if err != nil {
		t.Fatalf("newParser() error = %v", err)
	}
	names := append([]string(nil), credentialEnvs...)
	err = kong.Visit(parser.Model, func(node kong.Visitable, next kong.Next) error {
		if value, ok := node.(*kong.Value); ok && value.Tag != nil {
			names = append(names, value.Tag.Envs...)
		}
		return next(nil)
	})
	if err != nil {
		t.Fatalf("walk the kong model: %v", err)
	}
	for _, name := range names {
		t.Setenv(name, "")
		if err := os.Unsetenv(name); err != nil {
			t.Fatalf("Unsetenv(%q): %v", name, err)
		}
	}
}

// exitCode is what the test Exit panics with. kong calls Exit after --help and
// --version; left at os.Exit that would end the test binary, and a stub that
// merely returned would let parsing — and then serve — carry on.
type exitCode int

func panicOnExit() kong.Option {
	return kong.Exit(func(code int) { panic(exitCode(code)) })
}

// exitOf runs f and reports the code kong exited with, or exited=false when f
// returned without exiting.
func exitOf(f func()) (code exitCode, exited bool) {
	defer func() {
		if r := recover(); r != nil {
			c, isExit := r.(exitCode)
			if !isExit {
				panic(r)
			}
			code, exited = c, true
		}
	}()
	f()
	return 0, false
}

// parse builds the parser the way Run does and parses args without running
// the selected command.
func parse(t *testing.T, args ...string) (*CLI, *kong.Context, error) {
	t.Helper()
	var root CLI
	parser, err := newParser(&root, testVersion, panicOnExit())
	if err != nil {
		t.Fatalf("newParser() error = %v", err)
	}
	kctx, err := parser.Parse(args)
	return &root, kctx, err
}

// Every deployment that predates the command tree runs the binary with no
// command at all — the image's ENTRYPOINT, compose, `make run`.
func TestParse_NoArgumentsSelectsServe(t *testing.T) {
	clearEnv(t)
	_, kctx, err := parse(t)
	if err != nil {
		t.Fatalf("parse() error = %v", err)
	}
	if got := kctx.Command(); got != "serve" {
		t.Errorf("Command() = %q, want serve", got)
	}
}

// ...and passes serve's flags with no command in front of them.
func TestParse_FlagsWithoutACommandReachServe(t *testing.T) {
	clearEnv(t)
	root, kctx, err := parse(t, "--http-addr", "127.0.0.1:7777", "--extraction-mode", "rule")
	if err != nil {
		t.Fatalf("parse() error = %v", err)
	}
	if got := kctx.Command(); got != "serve" {
		t.Errorf("Command() = %q, want serve", got)
	}
	if got := root.Serve.Config.HTTPAddr; got != "127.0.0.1:7777" {
		t.Errorf("HTTP address = %q, want 127.0.0.1:7777", got)
	}
}

// The old --health-check shared serve's validation, so a probe could be
// refused for a token it never reads. healthcheck validates nothing it does not
// read: kong checks only the nodes on the selected command's path.
func TestParse_HealthCheckIgnoresServeValidation(t *testing.T) {
	clearEnv(t)
	t.Setenv("API_TOKEN", "")
	t.Setenv("HTTP_ADDR", ":8080")
	t.Setenv("IMPORT_MAX_ITEMS", "0")
	t.Setenv("EXTRACTION_MODE", "banana")

	root, kctx, err := parse(t, "healthcheck")
	if err != nil {
		t.Fatalf("parse(healthcheck) error = %v, want serve's rules not applied", err)
	}
	if got := kctx.Command(); got != "healthcheck" {
		t.Errorf("Command() = %q, want healthcheck", got)
	}
	if root.HealthCheck.Addr != ":8080" {
		t.Errorf("Addr = %q, want HTTP_ADDR's :8080", root.HealthCheck.Addr)
	}
}

// ...while serve still refuses the environment healthcheck just accepted.
func TestParse_ServeStillValidates(t *testing.T) {
	clearEnv(t)
	t.Setenv("API_TOKEN", "")
	t.Setenv("HTTP_ADDR", ":8080")

	if _, _, err := parse(t); err == nil {
		t.Error("parse() error = nil, want serve to refuse a non-loopback bind without API_TOKEN")
	}
}

// The flag is gone rather than kept as an alias: the image's HEALTHCHECK ships
// with the binary and was switched with it, and a caller still passing the flag
// must fail loudly — not fall through to the default command and start a server.
func TestParse_LegacyHealthCheckFlagIsRefused(t *testing.T) {
	clearEnv(t)
	if _, _, err := parse(t, "--health-check"); err == nil {
		t.Error("parse(--health-check) error = nil, want an unknown-flag refusal")
	}
}

// serve takes its flags without being named, so kong reads a flag in front of
// another command as serve's and only then switches commands. Unguarded,
// `--http-addr X healthcheck` probed the default address instead of X, and
// reported on whatever answered there. It has to be refused instead (E11).
func TestRun_FlagBeforeAnotherCommandIsRefused(t *testing.T) {
	clearEnv(t)
	for _, args := range [][]string{
		{"--http-addr", "127.0.0.1:1", "healthcheck"},
	} {
		err := Run(context.Background(), args, testVersion)
		if err == nil || !strings.Contains(err.Error(), "after the command name") {
			t.Errorf("Run(%q) error = %v, want a refusal saying where the flag goes", args, err)
		}
	}
}

// --version prints the stamped version and exits 0 before any command runs —
// including the default one, which would otherwise start a server.
func TestRun_VersionPrintsTheStampedVersionAndExits(t *testing.T) {
	clearEnv(t)
	var out bytes.Buffer
	code, exited := exitOf(func() {
		_ = Run(context.Background(), []string{"--version"}, "v1.2.3-4-gabc1234",
			kong.Writers(&out, &out), panicOnExit())
	})
	if !exited {
		t.Fatal("Run(--version) returned; it must exit before any command runs")
	}
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	if got := strings.TrimSpace(out.String()); got != "v1.2.3-4-gabc1234" {
		t.Errorf("printed %q, want the stamped version", got)
	}
}

// Every other setting is reachable through the environment; --version must not
// be, or a VERSION variable in a .env file would make the process print its
// version and exit instead of starting.
func TestParse_VersionIsFlagOnly(t *testing.T) {
	clearEnv(t)
	t.Setenv("VERSION", "1")
	var err error
	if _, exited := exitOf(func() { _, _, err = parse(t) }); exited {
		t.Fatal("VERSION in the environment triggered --version")
	}
	if err != nil {
		t.Fatalf("parse() error = %v", err)
	}
}

// Run reaches serve's Run with ctx and BuildVersion bound. mode=llm without a
// key is the one serve failure that happens before the database is touched, so
// it proves the dispatch without Postgres; a missing binding would fail with a
// kong error instead. unreachableDSN and the deadline are the safety net should
// that ever stop being true: without them serve would migrate the default
// database and then serve until the test binary times out.
func TestRun_ServeIsDispatchedWithItsBindings(t *testing.T) {
	clearEnv(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	args := []string{"--extraction-mode", "llm", "--db-dsn", unreachableDSN}
	err := Run(ctx, args, testVersion)
	if err == nil || !strings.Contains(err.Error(), "needs an API key") {
		t.Errorf("Run(--extraction-mode llm) error = %v, want serve's missing-key refusal", err)
	}
}

// Run dispatches healthcheck to the probe, against the real API router.
func TestRun_HealthCheckProbesARunningServer(t *testing.T) {
	clearEnv(t)
	srv := httptest.NewServer(api.NewRouter(api.Deps{}, api.Security{}))
	defer srv.Close()

	args := []string{"healthcheck", "--http-addr", srv.Listener.Addr().String()}
	if err := Run(context.Background(), args, testVersion); err != nil {
		t.Errorf("Run(healthcheck) error = %v, want nil against a healthy server", err)
	}
}

// Every failure exits 1 — a rejected command line as much as a failed
// command — because 0 and 1 are what a container HEALTHCHECK understands.
func TestMain_ExitStatus(t *testing.T) {
	clearEnv(t)
	if code := Main([]string{"--does-not-exist"}, testVersion); code != 1 {
		t.Errorf("Main(--does-not-exist) = %d, want 1", code)
	}

	srv := httptest.NewServer(api.NewRouter(api.Deps{}, api.Security{}))
	defer srv.Close()
	if code := Main([]string{"healthcheck", "--http-addr", srv.Listener.Addr().String()}, testVersion); code != 0 {
		t.Errorf("Main(healthcheck) = %d, want 0 against a healthy server", code)
	}
}

// With no flag to list them under, --help is the only place an operator finds
// the credential names; they are in the description.
func TestDescription_NamesEveryCredential(t *testing.T) {
	for _, env := range credentialEnvs {
		if !strings.Contains(description, env) {
			t.Errorf("--help description does not mention %s", env)
		}
	}
}
```

- [x] **Step 2: Config-Tests auf kong-Parse umstellen**

`internal/config/config_test.go`: den Block von `// testVersion is what loadArgs stamps` bis zum Ende von `func loadArgs` ersetzen durch

```go
// loadArgs parses args into a Config the way the serve command does: kong
// applies flags, environment and defaults, then runs BeforeApply and Validate.
func loadArgs(args []string) (Config, error) {
	var cfg Config
	parser, err := kong.New(&cfg, kong.Name("recipe-reader"))
	if err != nil {
		return Config{}, err
	}
	if _, err := parser.Parse(args); err != nil {
		return Config{}, err
	}
	return cfg, nil
}
```

und `"github.com/alecthomas/kong"` in den Import-Block aufnehmen (eigene Gruppe nach der Standardbibliothek).

```bash
git rm internal/config/version_test.go
```

`internal/config/credentials_test.go`: `TestDescription_NamesEveryCredential` samt Kommentar löschen (liegt jetzt in `cli_test.go`) und den dann unbenutzten Import `"strings"` entfernen.

- [x] **Step 3: Tests laufen lassen, sie müssen fehlschlagen**

Run: `go test ./internal/cli/ ./internal/config/`
Expected: FAIL. `internal/cli` kompiliert nicht (`undefined: CLI`, `newParser`, `Run`, `Main`, `description`). In `internal/config` schlagen u. a. `TestLoad_RejectsNonLoopbackBindWithoutToken` (kein Validate-Aufruf) und `TestLoad_AllowsNonLoopbackBindWithToken` bzw. `TestLoad_ReadsCredentialsFromTheEnvironment` (keine Credentials gelesen) fehl.

- [x] **Step 4: `internal/config/config.go` auf kong-Hooks umbauen**

1. Paket-Doc (Zeilen 1–2) ersetzen durch:

```go
// Package config declares recipe-reader's settings as kong flags and validates
// them. It parses nothing itself: internal/cli embeds Config into the serve
// command, and kong fills it — and calls BeforeApply and Validate — only when
// serve is the command being run.
```

2. In `type Config struct` die Felder `Version` und `HealthCheck` samt ihren Kommentaren löschen (von `// Version is not a setting:` bis einschließlich der `HealthCheck bool …`-Zeile und der folgenden Leerzeile).
3. Den Block `// description is the --help preamble. …` samt `const description = …` löschen; er zieht nach `internal/cli/cli.go`.
4. `Load` und `load` (von `// Load parses configuration from the process` bis zur schließenden Klammer von `load`) ersetzen durch:

```go
// BeforeApply is a kong hook. It runs once defaults and environment variables
// have been applied and before Validate — which is why the credentials are read
// here and not in AfterApply: kong validates before AfterApply runs, and the
// API_TOKEN rule in Validate has to see the token.
func (c *Config) BeforeApply() error {
	c.readCredentials()
	return nil
}
```

5. `func (c Config) validate() error` → `func (c Config) Validate() error`. Die erste Doc-Zeile `// validate rejects configurations that parse but cannot be what the operator` wird zu `// Validate is called by kong after parsing. It rejects configurations that` und die zweite zu `// parse but cannot be what the operator meant.` (Rest des Kommentars unverändert.)
6. Import `"github.com/alecthomas/kong"` entfernen, da er nicht mehr benutzt wird.

- [x] **Step 5: `internal/cli/cli.go` anlegen**

```go
// Package cli is recipe-reader's command line: the kong command tree, the flags
// each command takes, and the dispatch from a parsed command line to the
// package that does the work. No command does that work here.
package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os/signal"
	"syscall"

	"github.com/alecthomas/kong"
)

// CLI is the root of the command tree.
//
// serve is the default command and takes its flags without being named
// ("withargs"), so `recipe-reader` and `recipe-reader --http-addr ...` start
// the server exactly as they did before there were commands.
type CLI struct {
	// Version is kong's --version flag: it prints the version Run was given
	// and exits before any command runs. It has no env tag, so a VERSION
	// variable in a .env file cannot trigger it.
	Version kong.VersionFlag `help:"Print the version and exit."`

	Serve       ServeCmd       `cmd:"" default:"withargs" help:"Run the HTTP server and the background import worker. The default command."`
	HealthCheck HealthCheckCmd `cmd:"" name:"healthcheck" help:"Probe the server already listening on HTTP_ADDR; exit 0 if /api/healthz answers ok, 1 otherwise. For container health checks."`
}

// BuildVersion is the version stamped into the binary at link time. It is a
// type of its own so kong can bind it for the Run methods that report it.
type BuildVersion string

// description is the --help preamble. It names the credentials because kong
// cannot: they are not flags, so it has no entry to list them under.
const description = "Imports recipes from Instagram saved posts, extracts structured data, and serves a searchable web UI.\n\n" +
	"Credentials are read from the environment only, never from flags: API_TOKEN (bearer token for writes; " +
	"required on a non-loopback HTTP_ADDR), INSTAGRAM_PASSWORD, LLM_API_KEY, and ANTHROPIC_API_KEY " +
	"(fallback for LLM_API_KEY with the anthropic provider)."

// Main runs the command line and returns the process exit status. SIGINT and
// SIGTERM cancel the context every command runs under.
//
// Every failure — a command line kong rejects or a command that fails — is
// logged as one line and exits 1. kong's own FatalIfErrorf would exit 80 for a
// usage error, and a container HEALTHCHECK understands only 0 and 1.
func Main(args []string, version string) int {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := Run(ctx, args, version); err != nil {
		slog.Error("fatal", "error", err)
		return 1
	}
	return 0
}

// Run parses args and runs the selected command, with ctx and version bound
// for its Run method. opts are appended to the parser's own; tests use them to
// replace kong's Exit and output writers.
//
// ctx is bound with BindTo because kong matches bindings by concrete type: a
// plain Bind(ctx) would register *signalCtx, and no Run method asks for that.
func Run(ctx context.Context, args []string, version string, opts ...kong.Option) error {
	var root CLI
	parser, err := newParser(&root, version, append([]kong.Option{
		kong.BindTo(ctx, (*context.Context)(nil)),
		kong.Bind(BuildVersion(version)),
	}, opts...)...)
	if err != nil {
		return err
	}
	kctx, err := parser.Parse(args)
	if err != nil {
		return err
	}
	if err := rejectMisplacedFlags(kctx); err != nil {
		return err
	}
	return kctx.Run()
}

// rejectMisplacedFlags refuses a flag that belongs to a command other than the
// one selected.
//
// serve takes its flags without being named ("withargs"), so kong reads a flag
// in front of another command as serve's and only then switches commands:
// `recipe-reader --http-addr X healthcheck` would probe the default address,
// not X, and `--db-dsn X migrate` would migrate the default database. Both
// parse cleanly, so nothing else would say that the flag went nowhere.
func rejectMisplacedFlags(kctx *kong.Context) error {
	onPath := map[*kong.Flag]bool{}
	for node := kctx.Selected(); node != nil; node = node.Parent {
		for _, flag := range node.Flags {
			onPath[flag] = true
		}
	}
	for _, flag := range kctx.Model.Flags {
		onPath[flag] = true
	}
	for _, path := range kctx.Path {
		if path.Flag != nil && !onPath[path.Flag] {
			return fmt.Errorf("--%s is not a flag of %s; put flags after the command name", path.Flag.Name, kctx.Command())
		}
	}
	return nil
}

// newParser builds the kong parser for root. It is separate from Run so tests
// can parse a command line without running what it selects.
func newParser(root *CLI, version string, opts ...kong.Option) (*kong.Kong, error) {
	return kong.New(root, append([]kong.Option{
		kong.Name("recipe-reader"),
		kong.Description(description),
		kong.Vars{"version": version},
	}, opts...)...)
}
```

- [x] **Step 6: `internal/cli/serve.go` und `internal/cli/healthcheck.go` anlegen**

```go
package cli

import (
	"context"

	"github.com/sBurmester/recipe-reader/internal/config"
	"github.com/sBurmester/recipe-reader/internal/server"
)

// ServeCmd runs the server: the HTTP API, the embedded frontend and the
// background import worker. It takes every setting in config.Config, and kong
// validates them only when serve is the command being run.
type ServeCmd struct {
	Config config.Config `embed:""`
}

// Run serves until ctx is cancelled.
func (c *ServeCmd) Run(ctx context.Context, version BuildVersion) error {
	return server.Run(ctx, c.Config, string(version))
}
```

```go
package cli

import (
	"context"

	"github.com/sBurmester/recipe-reader/internal/healthcheck"
)

// HealthCheckCmd probes a server that is already running, for the image's
// HEALTHCHECK. It declares the listen address and nothing else: a probe needs
// nothing else, and so cannot be refused over settings it never reads — which
// the --health-check flag it replaces could be, because it shared serve's
// validation.
type HealthCheckCmd struct {
	Addr string `name:"http-addr" env:"HTTP_ADDR" default:"127.0.0.1:8080" help:"Address the server listens on. An unspecified host (\":8080\") is probed on loopback."`
}

// Run probes the server once.
func (c *HealthCheckCmd) Run(ctx context.Context) error {
	return healthcheck.Probe(ctx, c.Addr)
}
```

In `internal/healthcheck/healthcheck.go` den Satz `// It is what \`recipe-reader --health-check\` runs, so the image can carry a` ersetzen durch `// It is what \`recipe-reader healthcheck\` runs, so the image can carry a`.

Kommentare nachziehen, die das entfallene `config.Load` oder das umbenannte `validate` nennen, auch außerhalb von `internal/config`:

```bash
sed -i 's/Unreachable through config\.Load — the kong enum rejects it first —/Unreachable from the command line — the kong enum rejects it first —/' internal/server/extractor.go
sed -i 's/config\.Load refuses any other bind without a token/config validation refuses any other bind without a token/' internal/server/http.go
sed -i 's/the port, so Load refuses to produce it\./the port, so Validate refuses it./' internal/config/config_test.go
sed -i 's/see Config\.validate for when/see Config.Validate for when/' internal/config/config.go
sed -i 's/config\.validate rejects one now/config.Config.Validate rejects one now/' \
  internal/pipeline/worker.go internal/pipeline/worker_lifecycle_test.go
sed -i 's/by Config\.validate, so it cannot/by config.Config.Validate, so it cannot/' internal/api/middleware.go
sed -i 's/which config\.Load only permits on a loopback/which config validation only permits on a loopback/' internal/api/middleware.go
grep -rn 'config\.Load\|Load refuses\|onfig\.validate' internal   # Expected: keine Treffer
```

Danach in `internal/api/middleware.go` den Absatz von `// Token is the bearer token` bis `// UI.` auf 80 Spalten umbrechen, ohne Wörter zu ändern; die neue Formulierung macht die Zeile länger. Der grep läuft hier nur über `internal`, weil `cmd/recipe-reader/main.go` `config.Load` bis Step 7 noch aufruft; Step 7 wiederholt ihn über `internal cmd`.

(Task 4 zieht die drei `config.Config.Validate` auf die Gruppe nach, die die Regel dann trägt.)

*Nachgetragen im Milestone-Review von M1 (F9):* Die `middleware.go:15`-Zeile, die Aufteilung des grep und die Zeilennummer `healthcheck.go:25` fehlten in der ursprünglichen Fassung. Ohne sie hätte der grep garantiert zwei Treffer geliefert.

- [x] **Step 7: `main.go` in die Endform bringen**

```bash
git rm cmd/recipe-reader/version.go
```

`cmd/recipe-reader/main.go` komplett ersetzen:

```go
// Command recipe-reader imports recipes from Instagram saved posts and serves
// them through a searchable web UI. Everything it does lives behind
// internal/cli; this file hands over the command line and the version.
package main

import (
	"os"

	"github.com/sBurmester/recipe-reader/internal/cli"
)

// version identifies this build. It is stamped at link time — the Makefile and
// the Dockerfile both pass `-ldflags "-X main.version=..."` from `git describe`
// — and stays "dev" for a plain `go build` or `go run`. It is surfaced as
// `recipe-reader --version` and as the version field of GET /api/healthz.
var version = "dev"

func main() {
	os.Exit(cli.Main(os.Args[1:], version))
}
```

Danach den grep aus Step 6 über beide Verzeichnisse wiederholen:

```bash
grep -rn 'config\.Load\|Load refuses\|onfig\.validate' internal cmd   # Expected: keine Treffer
```

- [x] **Step 8: Tests laufen lassen, sie müssen grün sein**

Run: `go build ./... && go test ./internal/cli/ ./internal/config/ ./internal/server/ ./internal/healthcheck/`
Expected: PASS in allen vier Paketen.

Zusätzlich die Hilfe prüfen:

```bash
go run ./cmd/recipe-reader --help            # listet serve und healthcheck
go run ./cmd/recipe-reader serve --help      # listet alle serve-Flags mit ($ENV)
go run ./cmd/recipe-reader --version         # dev
go run ./cmd/recipe-reader --health-check; echo "exit=$?"   # unknown flag, exit=1
go run ./cmd/recipe-reader --http-addr 127.0.0.1:1 healthcheck; echo "exit=$?"   # "put flags after the command name", exit=1
```

- [x] **Step 9: Image, Compose, Smoke-Test umstellen**

`Dockerfile`: den Kommentar- und `HEALTHCHECK`-Block am Ende ersetzen durch

```dockerfile
# The binary is its own probe: `recipe-reader healthcheck` dials /api/healthz
# on HTTP_ADDR with the container's own environment. This image has no curl or
# wget, and adding one to probe ourselves would grow it for the sake of one GET.
# The probe gives up after 3s, inside --timeout, so a hung server fails it with
# a message rather than being killed by Docker without one. --start-interval
# polls quickly while the server starts, so a healthy container reports so in
# seconds rather than after the first 30s interval.
HEALTHCHECK --interval=30s --timeout=5s --start-period=30s --start-interval=2s --retries=3 \
    CMD ["/usr/local/bin/recipe-reader", "healthcheck"]
ENTRYPOINT ["/usr/local/bin/recipe-reader"]
# serve is the default command either way; naming it makes the default visible,
# and `docker run <image> migrate` or `--version` replaces it as usual.
CMD ["serve"]
```

`docker-compose.yml:53`: `` (`recipe-reader --health-check` against `` → `` (`recipe-reader healthcheck` against ``.

`scripts/smoke-test-image.sh:78-81`: `# The image's own HEALTHCHECK — the binary probing itself with --health-check —` → `# The image's own HEALTHCHECK — the binary probing itself with its healthcheck command —`, und `# is reached by no unit test. A renamed flag, a wrong binary path or a probe` → `# is reached by no unit test. A renamed command, a wrong binary path or a probe`.

- [x] **Step 10: README aktualisieren**

a) Neuer Abschnitt `## Commands` zwischen `## Setup` und `## Configuration`:

```markdown
## Commands

The binary is a small [kong](https://github.com/alecthomas/kong) command tree. `serve` is the
default: it runs when no command is named and takes its flags without one, so `recipe-reader` and
`recipe-reader --http-addr 127.0.0.1:9090` start the server exactly as they always have.

| Command | What it does |
| --- | --- |
| `serve` (default) | Runs the HTTP API, the embedded frontend and the background import worker. Takes every setting under [Configuration](#configuration). |
| `healthcheck` | Probes the server already listening on `HTTP_ADDR` and exits 0 if `/api/healthz` answers ok, 1 otherwise. Reads `HTTP_ADDR` and nothing else. |

`recipe-reader --help` lists the commands; `recipe-reader <command> --help` lists a command's flags
and the environment variable behind each. `--version` works with or without a command. Flags go
after the command name: `recipe-reader healthcheck --http-addr 127.0.0.1:9090`. Because `serve`
takes its flags unnamed, a flag in front of another command would otherwise be read as a `serve`
flag and silently dropped, so it is refused instead. Every failure, a rejected command line as
much as a failed command, exits 1.

`healthcheck` replaces the `--health-check` flag of earlier versions, which is now refused as an
unknown flag. The image's own `HEALTHCHECK` ships in the same image as the binary and was switched
with it; only a probe configured outside the image, such as an orchestrator's exec probe, needs
updating.
```

b) In `## Configuration`: `Run \`recipe-reader --help\` for the full list.` → `Run \`recipe-reader serve --help\` for the full list.` Der Satz „`recipe-reader --help` names them in its description“ bleibt so stehen, denn die Beschreibung erscheint weiterhin in der Hilfe der obersten Ebene.

c) In `### Health check`: `recipe-reader --health-check     # exit 0 if …` → `recipe-reader healthcheck       # exit 0 if …`.

d) In `## Architecture` vor dem Link auf den Implementierungsplan einfügen:

```markdown
The binary's own layering is deliberately thin. `cmd/recipe-reader` holds only `main.go`, which
hands the command line and the link-time version to `internal/cli`. That package is the kong
command tree, and each command's `Run` delegates at once: `serve` to `internal/server`, the
composition root that wires database, extraction, Instagram, import worker and HTTP API;
`healthcheck` to `internal/healthcheck`. The settings are declared and validated in
`internal/config`.
```

- [x] **Step 11: Docker-Gate** (siehe Global Constraints)

Expected: `smoke test passed: recipe-reader:ci (smoke)`, insbesondere die Stufe „the image's HEALTHCHECK reports … healthy“.

- [x] **Step 12: Commit-Gate und Commit**

```bash
git add -A cmd internal Dockerfile docker-compose.yml scripts README.md
git commit -m "refactor!: dispatch through a kong command tree" \
  -m "internal/cli holds the tree: serve (default, withargs), healthcheck, and the global --version. main.go only hands over os.Args and the version. Config is validated through kong's Validate and fills its env-only credentials in BeforeApply, for the serve path only. A flag placed before a command it does not belong to is refused rather than silently read as serve's." \
  -m "BREAKING CHANGE: --health-check is replaced by the healthcheck command. The image's HEALTHCHECK is switched in the same commit; external exec probes must be updated."
```

Der Footer hier dokumentiert den Commit. `main` erreicht er nur über die PR-Beschreibung, weil per Squash gemergt wird (siehe „Branches und PRs“).

**Abweichung nach dem Task-Commit (E13, Nutzerentscheidung vom 2026-09-19):** Der Nutzer will `run` in `main.go` und kein `cli.Main`, das `main` aufruft. Ein eigener Commit `refactor: parse and dispatch in main.go` nach dem Task-Commit setzt das um:
- `cli.Main`, `cli.Run` und `newParser` entfallen in `internal/cli`.
- `main.go` enthält `main()` (loggt einen Fehler, `os.Exit(1)`), `run(args, opts...)` und `newParser(root, opts...)`, jeweils mit den Kommentaren, die vorher an `cli.Main`, `cli.Run` und `newParser` standen.
- `description` wird zu `cli.Description`, `rejectMisplacedFlags` zu `cli.RejectMisplacedFlags`.
- `internal/cli/cli_test.go` zieht nach `cmd/recipe-reader/main_test.go`. `TestMain_ExitStatus` prüft den Exit-Status jetzt in einem Kindprozess (die Testbinary, neu gestartet), weil `main()` den Prozess selbst beendet.
- Die README-Architektur beschreibt den neuen Schnitt.

Direkt danach hat der Nutzer den Schnitt nachgeschärft: In `internal/cli` bleiben **nur die Kommandos** (`ServeCmd`, `HealthCheckCmd`, später `MigrateCmd`, dazu `BuildVersion`, das in der Signatur von `ServeCmd.Run` steht). Nach `main.go` ziehen:
- der Baum `CLI`,
- `description` (war `Description`),
- `rejectMisplacedFlags` (war `RejectMisplacedFlags`).

Beides steckt in einem Commit (`refactor: parse and dispatch in main.go`). Der Paket-Doc von `internal/config` bleibt, weil `ServeCmd` in `internal/cli` die `Config` weiterhin einbettet.

Die Code-Blöcke von Step 1, 5, 6 und 7 oben zeigen den ursprünglichen Stand. Task 4 und Task 5 sind auf E13 umgestellt.

**Abweichungen aus dem Milestone-Review von M2** (Nutzerentscheidungen vom 2026-09-19; eigene Commits nach dem E13-Commit):
- **F10:** Der Guard gegen verirrte Flags läuft als kong-Hook `CLI.Validate` am Wurzelknoten statt als eigener Aufruf in `run`. Damit wendet ihn jeder Parser aus `newParser` an. Sein Test ist jetzt ein reiner Parse-Test (`TestParse_FlagBeforeAnotherCommandIsRefused`) und kann auch bei einem Rückschritt nichts ausführen (F4).
- **F6:** Die Meldung lautet `--<flag> was given before the command <cmd>; put flags after the command name`, weil das Kommando das Flag durchaus haben kann.
- **F2/F3:** Ein neuer Test prüft, dass `serve` die vier Credentials über den Kommandobaum liest, mit und ohne Kommandonamen. Die beiden Ablehnungstests prüfen jetzt den Grund. Der Healthcheck-Test deckt zusätzlich einen `Reset`-Fehler ab (`IMPORT_INTERVAL=banana`). `TestMain_ExitStatus` prüft auch ein fehlschlagendes Kommando, zeigt die Ausgabe des Kindprozesses und bricht nach 30 s ab.
- **F5/F7/F13:** Kommentare korrigiert. Der `Version`-Kommentar ist korrigiert, die Begründung aus `version.go` steht wieder am `version`-Kommentar, und der `loadArgs`-Kommentar nennt kongs tatsächliche Reihenfolge. Die Testmeldungen sagen `loadArgs(` statt `load(`.
- **F12/F14:** Kommentar in `scripts/smoke-test-image.sh` umgebrochen; `web/src/main.ts` verweist auf `internal/server` als Composition Root.

**Nachtrag (Nutzerentscheidung vom 2026-09-19, im offenen PR 2/4):** `BuildVersion` zieht mit dem Paket-Doc nach `internal/cli/serve.go`, `internal/cli/cli.go` entfällt. `ServeCmd.Run` ist der einzige Nutzer, und `internal/cli` hat damit eine Datei pro Kommando. In `main.go` kann der Typ nicht liegen: `ServeCmd.Run` nennt ihn in seiner Signatur, kong bindet über den exakten Typ, und das Paket `main` ist nicht importierbar.

---

### Task 4: Settings in Optionsgruppen pro Belang

`config.Config` wird zum Aggregat eingebetteter Gruppen. Jede Gruppe trägt ihre eigene `Validate()` und, falls sie Credentials hat, ihren eigenen `BeforeApply()`. `healthcheck` teilt sich `config.Listen` mit `serve`, statt das Flag zu duplizieren. `serve --help` zeigt Überschriften pro Gruppe; die Gruppen werden über `kong.ExplicitGroups(config.Groups())` deklariert, und ihre Beschreibungen nennen die env-only Credentials (E12). Flag- und Env-Namen bleiben identisch (kein `prefix`).

**Files:**
- Modify: `internal/config/config.go` (komplett ersetzt, Code in Step 4)
- Modify: `internal/config/config_test.go` (`TestLLMSettings_Precedence`, plus `TestLLMSettings_FallbacksAreProviderScoped` aus `internal/server/server_test.go`), plus `sed` über alle Config-Tests
- Modify: `internal/server/*.go` (`sed`, einschließlich `run_test.go` aus Task 2), `internal/server/server_test.go` (`baseConfig`; `TestLLMSettings_FallbacksAreProviderScoped` zieht nach `internal/config`)
- Modify: `internal/cli/healthcheck.go` (bettet `config.Listen` ein), `cmd/recipe-reader/main.go` (`kong.ExplicitGroups` in `newParser`, E13), `cmd/recipe-reader/main_test.go` (`sed` + zwei neue Help-Tests)
- Modify (nur Kommentare): `internal/pipeline/worker.go:74`, `internal/pipeline/worker_lifecycle_test.go:65`, `internal/api/middleware.go:172` (`config.Config.Validate` → die Gruppe, die die Regel jetzt trägt)
- Modify: `README.md` (Configuration)

**Interfaces:**
- Consumes: `ServeCmd`, `HealthCheckCmd` (Task 3); `newParser` in `main.go` sowie `exitOf`, `panicOnExit`, `clearEnv`, `credentialEnvs` in `main_test.go` (Task 3 nach E13)
- Produces:
  - `func config.Groups() []kong.Group` (Überschriften und Beschreibungen für `kong.ExplicitGroups`)
  - `type config.Config struct { HTTP HTTP; Database Database; Instagram Instagram; Extraction Extraction; LLM LLM; Import Import }` (alle `embed:""` mit `group:"…"`)
  - `type config.Listen struct { Addr string }`; `type config.HTTP struct { Listen (eingebettet); CORSOrigins []string; APIToken string }`
  - `type config.Database struct { DSN string }`
  - `type config.Instagram struct { Username, Password, SessionPath, Collection string }`
  - `type config.Extraction struct { Mode string; Threshold, PublishThreshold float64 }`
  - `type config.LLM struct { Provider, APIKey, Model, BaseURL string; Timeout time.Duration; AnthropicAPIKey, AnthropicModel string }`, `func (l config.LLM) Settings() (config.LLMSettings, bool)`
  - `type config.Import struct { Interval time.Duration; MaxItems, MaxPages int }`
  - `HealthCheckCmd{ config.Listen \`embed:""\` }` (Feldzugriff `c.Addr` bleibt)

- [x] **Step 1: Failing Tests für die gruppierte Hilfe** (an `cmd/recipe-reader/main_test.go` anhängen; E13)

```go
// serveHelp returns what `recipe-reader serve --help` prints.
func serveHelp(t *testing.T) string {
	t.Helper()
	clearEnv(t)
	var out bytes.Buffer
	_, exited := exitOf(func() {
		var root CLI
		parser, err := newParser(&root, panicOnExit(), kong.Writers(&out, &out))
		if err != nil {
			t.Fatalf("newParser() error = %v", err)
		}
		_, _ = parser.Parse([]string{"serve", "--help"})
	})
	if !exited {
		t.Fatal("serve --help did not exit")
	}
	return out.String()
}

// serve takes some twenty flags. Grouped by concern, --help reads as the six
// things an operator configures rather than one alphabetical wall.
func TestHelp_ServeGroupsFlagsByConcern(t *testing.T) {
	help := serveHelp(t)
	for _, group := range []string{"HTTP", "Database", "Instagram", "Extraction", "LLM", "Import"} {
		if !strings.Contains(help, "\n"+group+"\n") {
			t.Errorf("serve --help has no %q group:\n%s", group, help)
		}
	}
}

// serve --help is where the flags are, but kong prints the app description,
// which names the credentials, only at the top level. With no flag entry of
// their own, the credentials are named in the description of the group each
// belongs to instead (E12).
func TestHelp_ServeNamesEveryCredential(t *testing.T) {
	help := serveHelp(t)
	for _, env := range credentialEnvs {
		if !strings.Contains(help, env) {
			t.Errorf("serve --help does not mention %s:\n%s", env, help)
		}
	}
}
```

- [x] **Step 2: Aufrufer (`internal/server`) und Tests auf die neuen Feldnamen umschreiben**

Das `sed`-Skript **genau einmal** ausführen. Ein zweiter Lauf würde z. B. `.HTTP.APIToken` zu `.HTTP.HTTP.APIToken` machen. Der Guard bricht ab, wenn schon umgeschriebene Zugriffe existieren.

```bash
! grep -rq '\.HTTP\.Addr\|\.LLM\.Settings()' internal/server cmd/recipe-reader internal/config \
  || { echo "field rename already applied"; exit 1; }
fields=$(mktemp)
cat > "$fields" <<'EOF'
s/\.HTTPAddr\b/.HTTP.Addr/g
s/\.CORSOrigins\b/.HTTP.CORSOrigins/g
s/\.APIToken\b/.HTTP.APIToken/g
s/\.DBDSN\b/.Database.DSN/g
s/\.InstagramUsername\b/.Instagram.Username/g
s/\.InstagramPassword\b/.Instagram.Password/g
s/\.InstagramSessionPath\b/.Instagram.SessionPath/g
s/\.InstagramCollection\b/.Instagram.Collection/g
s/\.ExtractionMode\b/.Extraction.Mode/g
s/\.ExtractionPublishThreshold\b/.Extraction.PublishThreshold/g
s/\.ExtractionThreshold\b/.Extraction.Threshold/g
s/\.AnthropicAPIKey\b/.LLM.AnthropicAPIKey/g
s/\.AnthropicModel\b/.LLM.AnthropicModel/g
s/\.LLMProvider\b/.LLM.Provider/g
s/\.LLMAPIKey\b/.LLM.APIKey/g
s/\.LLMModel\b/.LLM.Model/g
s/\.LLMBaseURL\b/.LLM.BaseURL/g
s/\.LLMTimeout\b/.LLM.Timeout/g
s/\.ImportInterval\b/.Import.Interval/g
s/\.ImportMaxItems\b/.Import.MaxItems/g
s/\.ImportMaxPages\b/.Import.MaxPages/g
s/\.LLMSettings()/.LLM.Settings()/g
EOF
sed -i -f "$fields" internal/server/*.go internal/config/*_test.go cmd/recipe-reader/*_test.go
```

*Abweichung, festgestellt bei der Umsetzung am 2026-09-20:* Die Regel `s/\.LLMProvider\b/.LLM.Provider/g` trifft auch `extraction.LLMProvider(settings.Provider)` in `internal/server/extractor.go:30` — einen Typ-Cast im Paket `extraction`, kein Config-Feld. Aus `extraction.LLMProvider(…)` wird `extraction.LLM.Provider(…)`, was in Step 3 als `undefined: extraction.LLM` auffällt. Die eine Zeile wurde wortgleich wiederhergestellt; das Milestone-Review hat per Rückabbildung der ganzen Tabelle gegen den Baseline-Baum nachgewiesen, dass es die einzige Kollateralstelle war. Wer das Skript erneut laufen lässt, prüft diese Zeile.

Struct-Literale erfasst `sed` nicht. Diese drei Stellen von Hand ersetzen (die zweite zieht dabei um):

`internal/server/server_test.go`, `baseConfig`:

```go
func baseConfig(mode string) config.Config {
	return config.Config{
		HTTP:       config.HTTP{Listen: config.Listen{Addr: "127.0.0.1:8080"}},
		Extraction: config.Extraction{Mode: mode, Threshold: 0.6},
		LLM:        config.LLM{Provider: "anthropic"},
	}
}
```

`TestLLMSettings_FallbacksAreProviderScoped` samt Doc-Kommentar aus `internal/server/server_test.go` löschen und in `internal/config/config_test.go` direkt nach `TestLLMSettings_Precedence` einfügen. Er testet eine Methode aus `internal/config` und gehört neben seinen Geschwistertest. Kommentar und Name bleiben, der Rumpf lautet dort (ohne Paketpräfix):

```go
	anthropic := LLM{Provider: "anthropic", AnthropicAPIKey: "sk-ant", AnthropicModel: "claude-x"}
	settings, ok := anthropic.Settings()
	if !ok || settings.APIKey != "sk-ant" || settings.Model != "claude-x" {
		t.Errorf("anthropic settings = %+v, ok = %v", settings, ok)
	}

	openAI := LLM{Provider: "openai", AnthropicAPIKey: "sk-ant"}
	if settings, ok := openAI.Settings(); ok {
		t.Errorf("openai settings = %+v, ok = %v — ANTHROPIC_API_KEY must not carry over", settings, ok)
	}
```

(*Nachgetragen im Milestone-Review von M1, F6.* In `package main` war die Trennung von seinem Geschwistertest harmlos; seit Task 2 liegt eine echte Paketgrenze dazwischen.)

`internal/config/config_test.go`, `TestLLMSettings_Precedence`, Rumpf:

```go
	llm := LLM{
		Provider: "anthropic", APIKey: "sk-llm", Model: "claude-new",
		AnthropicAPIKey: "sk-ant", AnthropicModel: "claude-old",
	}
	settings, ok := llm.Settings()
	if !ok || settings.APIKey != "sk-llm" || settings.Model != "claude-new" {
		t.Errorf("settings = %+v, ok = %v, want the LLM_* values to win", settings, ok)
	}

	fallback := LLM{Provider: "", AnthropicAPIKey: "sk-ant", AnthropicModel: "claude-old"}
	settings, ok = fallback.Settings()
	if !ok || settings.Provider != "anthropic" || settings.APIKey != "sk-ant" || settings.Model != "claude-old" {
		t.Errorf("settings = %+v, ok = %v, want the ANTHROPIC_* fallbacks", settings, ok)
	}

	none := LLM{Provider: "anthropic"}
	if _, ok := none.Settings(); ok {
		t.Error("Settings() ok = true with no key configured anywhere")
	}
```

- [x] **Step 3: Tests laufen lassen, sie müssen fehlschlagen**

Run: `go test ./internal/config/ ./internal/server/ ./cmd/recipe-reader/`
Expected: FAIL beim Kompilieren (`cfg.HTTP undefined`, `undefined: LLM`, `undefined: config.Listen` …)

- [x] **Step 4: `internal/config/config.go` durch die gruppierte Fassung ersetzen**

```go
// Package config declares recipe-reader's settings as kong flag groups and
// validates them. It parses nothing itself: internal/cli embeds the groups into
// the commands that need them, and kong fills them — and calls BeforeApply and
// Validate — only for the command being run.
//
// Every setting is resolved with the precedence: command-line flag, then
// environment variable, then the built-in default. The four credentials are
// the exception — they have no flag form at all; see HTTP.BeforeApply.
package config

import (
	"fmt"
	"net"
	"os"
	"time"

	"github.com/alecthomas/kong"
)

// Config is every setting the serve command takes, one embedded group per
// concern. The group tags are the headings in `recipe-reader serve --help`;
// flag and environment names carry no group prefix, so they are the same as
// before the settings were grouped.
type Config struct {
	HTTP       HTTP       `embed:"" group:"HTTP"`
	Database   Database   `embed:"" group:"Database"`
	Instagram  Instagram  `embed:"" group:"Instagram"`
	Extraction Extraction `embed:"" group:"Extraction"`
	LLM        LLM        `embed:"" group:"LLM"`
	Import     Import     `embed:"" group:"Import"`
}

// Groups are the headings of `recipe-reader serve --help`, for
// kong.ExplicitGroups. Their descriptions name the credentials: those are not
// flags, so kong has no entry to list them under, and the app description that
// names them too is printed only at the top level, not where the flags are.
func Groups() []kong.Group {
	return []kong.Group{
		{Key: "HTTP", Title: "HTTP", Description: "API_TOKEN, read from the environment only, guards the write routes; it is required when --http-addr is not loopback."},
		{Key: "Database", Title: "Database"},
		{Key: "Instagram", Title: "Instagram", Description: "INSTAGRAM_PASSWORD is read from the environment only. Without --instagram-username the server runs with no import worker."},
		{Key: "Extraction", Title: "Extraction"},
		{Key: "LLM", Title: "LLM", Description: "LLM_API_KEY, and ANTHROPIC_API_KEY as its fallback for the anthropic provider, are read from the environment only."},
		{Key: "Import", Title: "Import"},
	}
}

// Listen is the address the HTTP server binds. It is a group of its own because
// the healthcheck command needs it and nothing else.
type Listen struct {
	// Addr defaults to loopback rather than all interfaces: the insecure
	// combination (no token, network-reachable) is then something an operator
	// has to ask for, not something they get by leaving a field unset.
	//
	// The help text is shared by serve and healthcheck, so it says nothing
	// about API_TOKEN, which a probe never needs; the HTTP group's description
	// in Groups carries that rule.
	Addr string `name:"http-addr" env:"HTTP_ADDR" default:"127.0.0.1:8080" help:"Address the HTTP server listens on; healthcheck probes an unspecified host (\":8080\") on loopback."`
}

// HTTP is the server's listener and the access rules in front of it.
type HTTP struct {
	Listen `embed:""`

	// CORSOrigins is the browser-origin allowlist — the bundled frontend is
	// same-origin and needs no entry, so the default covers only the Vite dev
	// server.
	CORSOrigins []string `name:"cors-origin" env:"CORS_ORIGINS" default:"http://localhost:5173" help:"Comma-separated browser origins allowed to read API responses."`

	// APIToken guards the mutating routes; see Validate for when it is
	// mandatory. It is read from API_TOKEN only.
	APIToken string `kong:"-"`
}

// BeforeApply reads API_TOKEN from the environment.
//
// The four credentials — this one, INSTAGRAM_PASSWORD, LLM_API_KEY and
// ANTHROPIC_API_KEY — are deliberately not flags. A flag puts its value in the
// process table, readable by every other user on the host with `ps aux`, and in
// shell history and any process-listing telemetry — an Instagram password and
// three billable or write-granting tokens. kong has no environment-only field:
// a field without a name tag still gets a flag named after it. So kong is told
// to ignore these fields (kong:"-") and each group's BeforeApply hook reads its
// own.
//
// BeforeApply, not AfterApply: kong runs Validate before AfterApply, and
// HTTP.Validate has to see the token.
func (h *HTTP) BeforeApply() error {
	h.APIToken = os.Getenv("API_TOKEN")
	return nil
}

// Validate rejects a configuration that is insecure by construction: a
// listener reachable from the network with nothing in front of the mutating
// routes. Loopback without a token stays allowed, because that is the
// single-user desktop case this project is built for.
func (h HTTP) Validate() error {
	if h.APIToken == "" && !isLoopbackAddr(h.Addr) {
		return fmt.Errorf(
			"config: API_TOKEN is required when HTTP_ADDR (%q) is not loopback — "+
				"otherwise anyone who can route to the port can create, edit and delete recipes",
			h.Addr)
	}
	return nil
}

// Database is the PostgreSQL connection.
type Database struct {
	DSN string `name:"db-dsn" env:"DB_DSN" default:"postgres://recipes:recipes@localhost:5432/recipes?sslmode=disable" help:"PostgreSQL connection string."`
}

// Instagram is the account whose saved posts are imported. Without a username
// the server runs with no import worker.
type Instagram struct {
	Username string `name:"instagram-username" env:"INSTAGRAM_USERNAME" help:"Instagram account username used to fetch saved posts."`
	// Password is read from INSTAGRAM_PASSWORD only; see HTTP.BeforeApply.
	Password    string `kong:"-"`
	SessionPath string `name:"instagram-session-path" env:"INSTAGRAM_SESSION_PATH" default:"data/instagram-session.json" help:"File used to persist the Instagram login session."`
	Collection  string `name:"instagram-collection" env:"INSTAGRAM_COLLECTION" help:"Saved-posts collection to import; empty imports all saved posts."`
}

// BeforeApply reads INSTAGRAM_PASSWORD from the environment; see
// HTTP.BeforeApply for why it is not a flag.
func (i *Instagram) BeforeApply() error {
	i.Password = os.Getenv("INSTAGRAM_PASSWORD")
	return nil
}

// Extraction selects the extraction strategy and the two confidence thresholds
// it is judged by.
type Extraction struct {
	// The enum is load-bearing, not decoration: EXTRACTION_MODE used to accept
	// anything and only "hybrid" was ever inspected, so a typo — or the
	// perfectly reasonable "llm" — silently selected the weakest extractor.
	Mode      string  `name:"extraction-mode" env:"EXTRACTION_MODE" default:"hybrid" enum:"rule,llm,hybrid" help:"Recipe extraction strategy: rule, llm, or hybrid."`
	Threshold float64 `name:"extraction-confidence-threshold" env:"EXTRACTION_CONFIDENCE_THRESHOLD" default:"0.6" help:"Minimum rule-based confidence before falling back to the LLM extractor."`

	// PublishThreshold is a separate, stricter knob from Threshold. "Good
	// enough to skip the LLM" and "good enough to publish without a human
	// reading it" are different questions, and one number answering both is
	// what made needs_review unreachable.
	PublishThreshold float64 `name:"extraction-publish-threshold" env:"EXTRACTION_PUBLISH_THRESHOLD" default:"0.8" help:"Minimum extraction confidence to publish without review; below it a recipe is stored as needs_review."`
}

// Validate rejects either threshold outside 0..1. Both are compared against a
// confidence the extractors only ever produce in that range, so a value outside
// it does not fail: it silently makes one branch unreachable.
// EXTRACTION_PUBLISH_THRESHOLD=80, meaning a percentage, would send every
// recipe to needs_review, and EXTRACTION_CONFIDENCE_THRESHOLD=80 would call the
// LLM for every post — on a paid API, indefinitely, with nothing in the logs
// pointing at the configuration.
func (e Extraction) Validate() error {
	if err := validateThreshold("EXTRACTION_CONFIDENCE_THRESHOLD", e.Threshold); err != nil {
		return err
	}
	return validateThreshold("EXTRACTION_PUBLISH_THRESHOLD", e.PublishThreshold)
}

// LLM configures the LLM extractor. AnthropicAPIKey and AnthropicModel predate
// Provider and stay the fallbacks for the provider they were named after; see
// Settings.
type LLM struct {
	Provider string `name:"llm-provider" env:"LLM_PROVIDER" default:"anthropic" enum:"anthropic,openai" help:"LLM extractor transport: anthropic, or openai for any OpenAI-compatible endpoint."`
	// APIKey is read from LLM_API_KEY only; see HTTP.BeforeApply.
	APIKey  string `kong:"-"`
	Model   string `name:"llm-model" env:"LLM_MODEL" help:"Model id for the LLM extractor; falls back to ANTHROPIC_MODEL when the provider is anthropic."`
	BaseURL string `name:"llm-base-url" env:"LLM_BASE_URL" help:"Override the LLM endpoint base URL, e.g. https://api.groq.com/openai/v1 or http://localhost:11434/v1."`

	// Timeout exists because a single stalled model call used to hold the
	// import worker indefinitely. 60s suits a hosted API; a local model on CPU
	// can need more, which is why it is a setting and not a constant.
	Timeout time.Duration `name:"llm-timeout" env:"LLM_TIMEOUT" default:"60s" help:"Upper bound on one LLM extraction call, retries included. Raise it for a local model running on CPU."`

	// AnthropicAPIKey is read from ANTHROPIC_API_KEY only; see HTTP.BeforeApply.
	AnthropicAPIKey string `kong:"-"`
	AnthropicModel  string `name:"anthropic-model" env:"ANTHROPIC_MODEL" default:"claude-opus-5" help:"Anthropic model id for the LLM extractor (fallback for LLM_MODEL when the provider is anthropic)."`
}

// BeforeApply reads LLM_API_KEY and ANTHROPIC_API_KEY from the environment;
// see HTTP.BeforeApply for why they are not flags.
func (l *LLM) BeforeApply() error {
	l.APIKey = os.Getenv("LLM_API_KEY")
	l.AnthropicAPIKey = os.Getenv("ANTHROPIC_API_KEY")
	return nil
}

// LLMSettings is the resolved configuration for the LLM extractor, after the
// ANTHROPIC_* fallbacks have been applied.
type LLMSettings struct {
	Provider string
	APIKey   string
	Model    string
	BaseURL  string
	Timeout  time.Duration
}

// Settings resolves the effective LLM extractor settings. The bool is false
// when no API key is configured for the selected provider — what the caller
// does about that depends on the extraction mode, which is the caller's
// decision to make and not this function's.
func (l LLM) Settings() (LLMSettings, bool) {
	s := LLMSettings{
		Provider: l.Provider,
		APIKey:   l.APIKey,
		Model:    l.Model,
		BaseURL:  l.BaseURL,
		Timeout:  l.Timeout,
	}
	if s.Provider == "" {
		s.Provider = "anthropic"
	}
	// ANTHROPIC_API_KEY and ANTHROPIC_MODEL predate the provider setting, so
	// they stay authoritative for the provider they were named after.
	if s.Provider == "anthropic" {
		if s.APIKey == "" {
			s.APIKey = l.AnthropicAPIKey
		}
		if s.Model == "" {
			s.Model = l.AnthropicModel
		}
	}
	return s, s.APIKey != ""
}

// Import schedules and bounds the background import.
type Import struct {
	Interval time.Duration `name:"import-interval" env:"IMPORT_INTERVAL" default:"6h" help:"How often the background worker imports new saved posts."`

	// MaxItems and MaxPages bound one import run. They existed only as package
	// defaults in internal/instagram, reachable by recompiling; a backfill of a
	// large saved-posts history is exactly when an operator wants to raise
	// them, and a flagged account is when they want to lower them. The defaults
	// here match the package's own.
	MaxItems int `name:"import-max-items" env:"IMPORT_MAX_ITEMS" default:"50" help:"Most new posts one import run collects. Posts already imported do not count against it."`
	MaxPages int `name:"import-max-pages" env:"IMPORT_MAX_PAGES" default:"100" help:"Most feed pages one import run walks, whether or not they held anything new."`
}

// Validate rejects two kinds of value that parse but cannot be meant.
//
// A run bound below one: the fetcher would quietly replace it with its own
// default, so IMPORT_MAX_ITEMS=0 — plausibly meant as "pause imports" — would
// import fifty posts instead, which is the opposite.
//
// An interval at or below zero: time.NewTicker panics for one, on the calling
// goroutine, inside Worker.Start — so IMPORT_INTERVAL=0 used to abort the
// process with a stack trace instead of the clean slog.Error("fatal") path main
// was built around.
func (i Import) Validate() error {
	if i.MaxItems < 1 {
		return fmt.Errorf("config: IMPORT_MAX_ITEMS must be at least 1, got %d", i.MaxItems)
	}
	if i.MaxPages < 1 {
		return fmt.Errorf("config: IMPORT_MAX_PAGES must be at least 1, got %d", i.MaxPages)
	}
	if i.Interval <= 0 {
		return fmt.Errorf("config: IMPORT_INTERVAL must be positive, got %s", i.Interval)
	}
	return nil
}

// validateThreshold rejects a confidence threshold outside 0..1. NaN fails the
// comparison in both directions, which is why the test is written as "not
// inside the range" rather than as two bounds checks.
func validateThreshold(name string, v float64) error {
	if !(v >= 0 && v <= 1) {
		return fmt.Errorf("config: %s must be between 0 and 1, got %v", name, v)
	}
	return nil
}

// isLoopbackAddr reports whether addr binds the loopback interface only. A
// bare port (":8080") or an empty host means every interface, so it is not
// loopback — which is exactly the case that needs a token.
func isLoopbackAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
```

- [x] **Step 5: `internal/cli` auf die Gruppen umstellen**

a) `HealthCheckCmd` auf `config.Listen` umstellen (`internal/cli/healthcheck.go`):

```go
package cli

import (
	"context"

	"github.com/sBurmester/recipe-reader/internal/config"
	"github.com/sBurmester/recipe-reader/internal/healthcheck"
)

// HealthCheckCmd probes a server that is already running, for the image's
// HEALTHCHECK. It embeds the listen address and nothing else: a probe needs
// nothing else, and so cannot be refused over settings it never reads — which
// the --health-check flag it replaces could be, because it shared serve's
// validation. Sharing config.Listen with serve keeps the flag, its environment
// variable and its default in one place.
type HealthCheckCmd struct {
	config.Listen `embed:""`
}

// Run probes the server once.
func (c *HealthCheckCmd) Run(ctx context.Context) error {
	return healthcheck.Probe(ctx, c.Addr)
}
```

b) In `cmd/recipe-reader/main.go` (E13) die Gruppen in `newParser` explizit machen und `"github.com/sBurmester/recipe-reader/internal/config"` importieren (in die Gruppe mit `internal/cli`):

```go
func newParser(root *CLI, opts ...kong.Option) (*kong.Kong, error) {
	return kong.New(root, append([]kong.Option{
		kong.Name("recipe-reader"),
		kong.Description(description),
		kong.Vars{"version": version},
		kong.ExplicitGroups(config.Groups()),
	}, opts...)...)
}
```

c) Die drei Kommentare aus Task 3 auf die Gruppe umstellen, die die jeweilige Regel jetzt trägt:

```bash
sed -i 's/config\.Config\.Validate rejects one now/config.Import.Validate rejects one now/' \
  internal/pipeline/worker.go internal/pipeline/worker_lifecycle_test.go
sed -i 's/by config\.Config\.Validate, so it cannot/by config.HTTP.Validate, so it cannot/' internal/api/middleware.go
grep -rn 'config\.Config\.Validate\|Config\.Validate for' internal   # Expected: keine Treffer
```

Danach die beiden Absätze mit `config.Import.Validate rejects one now` (`internal/pipeline/worker.go` und `internal/pipeline/worker_lifecycle_test.go`) und den Absatz mit `config.HTTP.Validate` in `internal/api/middleware.go` auf 80 Spalten umbrechen, ohne Wörter zu ändern. Das geht erst jetzt, weil die `sed`s von Task 3 und Task 4 jeweils auf eine ganze Zeile zielen (*nachgetragen im Milestone-Review von M1, F5*).

- [x] **Step 6: Tests laufen lassen, sie müssen grün sein**

Run: `go build ./... && go test ./internal/config/ ./internal/server/ ./cmd/recipe-reader/`
Expected: PASS, einschließlich `TestHelp_ServeGroupsFlagsByConcern` und `TestHelp_ServeNamesEveryCredential`.

Zusätzlich die Flag-Namen gegen die Baseline aus der Vorbereitung prüfen:

```bash
go run ./cmd/recipe-reader serve --help | grep -o -- '--[a-z][a-z-]*' | sort -u | tr '\n' ' '
```

Expected: exakt die Baseline **ohne** `--health-check`, also dieselben 19 übrigen Flags mit denselben Namen.

- [x] **Step 7: README**

In `## Configuration`:
- `Run \`recipe-reader serve --help\` for the full list.` → `Run \`recipe-reader serve --help\` for the full list, grouped as HTTP, Database, Instagram, Extraction, LLM and Import.`
- `\`recipe-reader --help\` names them in its description, since there is no flag entry to list them under.` → `\`recipe-reader --help\` names them in its description, and \`recipe-reader serve --help\` in the description of the group each belongs to, since there is no flag entry to list them under.`

- [x] **Step 8: Commit-Gate und Commit**

```bash
git add -A cmd internal README.md
git commit -m "refactor(config): split settings into per-concern kong groups" -m "Config becomes an aggregate of embedded groups, each validating itself and reading its own env-only credentials in BeforeApply. Flag and environment names are unchanged; serve --help now shows one heading per group, whose description names the env-only credentials the group reads, and healthcheck shares config.Listen instead of redeclaring the flag."
```

---

### Task 5: Kommando `migrate`

Führt Migration und Seed aus und beendet sich. `serve` und `migrate` teilen sich dafür das neue `db.Open`, damit die Reihenfolge Migrate → Connect → Seed nur einmal existiert.

**Files:**
- Modify: `internal/db/connect.go` (+ `Open`)
- Modify: `internal/db/db_test.go` (+ `TestOpen_…`)
- Modify: `internal/server/server.go` (nutzt `db.Open`)
- Create: `internal/cli/migrate.go`, `cmd/recipe-reader/migrate_test.go`, `cmd/recipe-reader/testmain_test.go` (Dateiname wie in den anderen Paketen mit `testdb.Main`; die Tests liegen nach E13 bei `run` in `package main`)
- Modify: `cmd/recipe-reader/main.go` (Feld `Migrate` in `CLI`; E13)
- Modify: `cmd/recipe-reader/main_test.go` (`migrate`-Fall in `TestParse_FlagBeforeAnotherCommandIsRefused`)
- Modify: `README.md` (Commands, Architecture)

**Interfaces:**
- Consumes: `config.Database` (Task 4), `run` (`main.go`), `parse`, `clearEnv`, `unreachableDSN` (`main_test.go`; Task 3 nach E13), `testdb.NewDatabase(t, name) string`, `testdb.Main(m)`
- Produces: `func db.Open(ctx context.Context, dsn string) (*pgxpool.Pool, error)` (der Aufrufer schließt den Pool), `type cli.MigrateCmd struct { config.Database }`, `func (c *MigrateCmd) Run(ctx context.Context) error`

- [x] **Step 1: Failing Test für `db.Open`** (an `internal/db/db_test.go` anhängen)

```go
// Open is what serve and migrate both start with. Against an empty database it
// has to leave the schema migrated and the lookup tables seeded, and a second
// call — the next boot — has to change nothing.
func TestOpen_MigratesAndSeedsAnEmptyDatabase(t *testing.T) {
	ctx := context.Background()
	dsn := testdb.NewDatabase(t, "open_test")

	counts := make([]int, 2)
	for i := range counts {
		pool, err := db.Open(ctx, dsn)
		if err != nil {
			t.Fatalf("Open() #%d error = %v", i+1, err)
		}
		err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM units").Scan(&counts[i])
		pool.Close()
		if err != nil {
			t.Fatalf("count units after Open() #%d: %v", i+1, err)
		}
	}
	if counts[0] == 0 {
		t.Error("Open() left the units table empty")
	}
	if counts[1] != counts[0] {
		t.Errorf("second Open() changed the seeded units: %d -> %d", counts[0], counts[1])
	}
}
```

- [x] **Step 2: Failing Tests für `migrate`**

`cmd/recipe-reader/testmain_test.go`:

```go
package main

import (
	"testing"

	"github.com/sBurmester/recipe-reader/internal/db/testdb"
)

// The migrate tests need Postgres. testdb starts its container on first use,
// so the rest of this package's tests still run without one — including the
// children TestMain_ExitStatus re-executes, which never touch it.
func TestMain(m *testing.M) { testdb.Main(m) }
```

`cmd/recipe-reader/migrate_test.go`:

```go
package main

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sBurmester/recipe-reader/internal/db/testdb"
)

func TestRun_MigrateMigratesAndSeedsAnEmptyDatabase(t *testing.T) {
	clearEnv(t)
	ctx := context.Background()
	dsn := testdb.NewDatabase(t, "cli_migrate")
	args := []string{"migrate", "--db-dsn", dsn}

	if err := run(args); err != nil {
		t.Fatalf("run(migrate) error = %v", err)
	}
	// Idempotent: migrate after migrate is the normal case for an operator who
	// runs it ahead of every rollout.
	if err := run(args); err != nil {
		t.Fatalf("second run(migrate) error = %v", err)
	}

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pgxpool.New() error = %v", err)
	}
	defer pool.Close()
	var units int
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM units").Scan(&units); err != nil {
		t.Fatalf("count units: %v", err)
	}
	if units == 0 {
		t.Error("migrate left the units table empty")
	}
}

// migrate reads DB_DSN and nothing else, so serve's rules do not apply to it.
func TestParse_MigrateIgnoresServeValidation(t *testing.T) {
	clearEnv(t)
	t.Setenv("API_TOKEN", "")
	t.Setenv("HTTP_ADDR", ":8080")
	t.Setenv("IMPORT_MAX_ITEMS", "0")
	t.Setenv("EXTRACTION_MODE", "banana")
	t.Setenv("IMPORT_INTERVAL", "banana")

	_, kctx, err := parse(t, "migrate")
	if err != nil {
		t.Fatalf("parse(migrate) error = %v, want serve's rules not applied", err)
	}
	if got := kctx.Command(); got != "migrate" {
		t.Errorf("Command() = %q, want migrate", got)
	}
}
```

In `cmd/recipe-reader/main_test.go` die Tabelle von `TestParse_FlagBeforeAnotherCommandIsRefused` um den Fall ergänzen, der ohne E11 der gefährlichere wäre, weil er still die Default-Datenbank migrieren würde. Der Test parst nur und führt nichts aus (Milestone-Review M2, F4/F10), sodass auch ein Rückschritt des Guards keine Datenbank anfasst:

```go
		{"--db-dsn", unreachableDSN, "migrate"},
```

- [x] **Step 3: Tests laufen lassen, sie müssen fehlschlagen**

Run: `go test ./internal/db/ ./cmd/recipe-reader/`
Expected: FAIL, `undefined: db.Open`. In `cmd/recipe-reader` scheitert `parse(migrate)` mit `unexpected argument migrate`, und der neue Fall in `TestParse_FlagBeforeAnotherCommandIsRefused` scheitert mit derselben Meldung statt mit dem Hinweis „after the command name“.

- [x] **Step 4: `db.Open` implementieren** (`internal/db/connect.go`, nach `Connect`)

```go
// Open migrates the database at dsn, connects to it and seeds the lookup
// tables — everything a process needs before it can use the database. serve
// and migrate both start with it, so the order is written down once. The
// caller closes the pool.
func Open(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	if err := Migrate(dsn); err != nil {
		return nil, err
	}
	pool, err := Connect(ctx, dsn)
	if err != nil {
		return nil, err
	}
	if err := Seed(ctx, pool); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}
```

- [x] **Step 5: `server.Run` auf `db.Open` umstellen** (`internal/server/server.go`)

Ersetzen:

```go
	if err := db.Migrate(cfg.Database.DSN); err != nil {
		return err
	}
	pool, err := db.Connect(ctx, cfg.Database.DSN)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := db.Seed(ctx, pool); err != nil {
		return err
	}
```

durch

```go
	pool, err := db.Open(ctx, cfg.Database.DSN)
	if err != nil {
		return err
	}
	defer pool.Close()
```

- [x] **Step 6: `MigrateCmd` anlegen und einhängen**

`internal/cli/migrate.go`:

```go
package cli

import (
	"context"
	"log/slog"

	"github.com/sBurmester/recipe-reader/internal/config"
	"github.com/sBurmester/recipe-reader/internal/db"
)

// MigrateCmd applies pending migrations and seeds the lookup tables, then
// exits. serve does the same at every start; this does it on its own — ahead
// of a rollout, or after a restore — without starting the server or the import
// worker, and without reading any setting but the database's.
type MigrateCmd struct {
	config.Database `embed:"" group:"Database"`
}

// Run migrates and seeds once.
func (c *MigrateCmd) Run(ctx context.Context) error {
	pool, err := db.Open(ctx, c.DSN)
	if err != nil {
		return err
	}
	pool.Close()
	slog.Info("database migrated and seeded")
	return nil
}
```

In `cmd/recipe-reader/main.go` im `CLI`-Struct nach `HealthCheck` ergänzen (E13; gofmt richtet die Spalten aus):

```go
	Migrate     cli.MigrateCmd `cmd:"" help:"Apply pending database migrations and seed the lookup tables, then exit. serve does the same at every start."`
```

- [x] **Step 7: Tests laufen lassen, sie müssen grün sein**

Run: `go build ./... && go test ./internal/db/ ./cmd/recipe-reader/ ./internal/server/`
Expected: PASS

- [x] **Step 8: README**

In `## Commands` der Tabelle eine Zeile anfügen:

```markdown
| `migrate` | Applies pending migrations and seeds the lookup tables, then exits: ahead of a rollout, or after a restore. `serve` does the same at every start. Reads `DB_DSN` and nothing else. With compose: `docker compose run --rm app migrate`. |
```

In `## Architecture` im Absatz aus Task 3: `\`healthcheck\` to \`internal/healthcheck\`.` → `\`healthcheck\` to \`internal/healthcheck\`; \`migrate\` to \`internal/db\`.`

- [x] **Step 9: Commit-Gate und Commit**

```bash
git add -A cmd internal README.md
git commit -m "feat(cli): add a migrate command" -m "Runs Migrate, Connect and Seed and exits, reading only DB_DSN. db.Open now holds that order for serve and migrate alike."
```

---

### Abschluss-Verifikation

- [x] **Step 1: Paket `main` prüfen**

```bash
ls cmd/recipe-reader/
wc -l cmd/recipe-reader/main.go
go list -f '{{join .Imports " "}}' ./cmd/recipe-reader
```

Expected (E13): `main.go` und dessen Tests (`main_test.go`, `migrate_test.go`, `testmain_test.go`); `main()` ruft nur `run()`; aus `internal/*` importiert `main.go` nur `internal/cli` und `internal/config` (für `config.Groups`).

- [x] **Step 2: Commit-Gate komplett** (siehe Global Constraints). Alles grün.

- [x] **Step 3: Docker-Gate** (siehe Global Constraints). `smoke test passed`.

- [x] **Step 4: Compose End-to-End** (im eigenen Projekt `recipe-reader-e2e`, nie im Default-Projekt)

Das Default-Projekt `recipe-reader` hat das Volume `db-data`, in dem echte Daten liegen können. Postgres setzt `POSTGRES_PASSWORD` nur bei der ersten Initialisierung. Mit einem frischen Zufallspasswort gegen ein bestehendes Volume scheitert die Anmeldung; der Lauf wäre rot, obwohl der Code stimmt, und liefe außerdem gegen echte Daten. Voraussetzung: Die Ports `8080` und `127.0.0.1:5432` sind frei, ein laufender eigener Stack ist also vorher gestoppt (`docker compose ls` zeigt, was läuft).

```bash
export API_TOKEN=$(openssl rand -hex 32) POSTGRES_PASSWORD=$(openssl rand -hex 16)
docker compose -p recipe-reader-e2e up -d --build --wait
docker compose -p recipe-reader-e2e ps                            # app: healthy
docker compose -p recipe-reader-e2e run --rm app migrate          # "database migrated and seeded", Exit 0
docker compose -p recipe-reader-e2e exec app recipe-reader --version
docker compose -p recipe-reader-e2e exec app recipe-reader healthcheck; echo "exit=$?"   # exit=0
docker compose -p recipe-reader-e2e down -v
```

`down -v` entfernt die Wegwerf-Volumes des E2E-Projekts wieder. Es auch dann ausführen, wenn ein Schritt davor scheitert.

- [x] **Step 5: Akzeptanzkriterien in Teil A abhaken** (US1–US5, Definition of Done)

- [x] **Step 6: M4 und die Aufgabenliste in Teil C abhaken und diese Datei committen**

```bash
git add docs/superpowers/plans/2026-09-19-kong-cli-commands.md
git commit -m "docs: mark M4 done in the kong CLI plan"
```

- [x] **Step 7: PR 4/4** (`feat(cli): add a migrate command (4/4)` auf `feat/kong-cli-migrate`) erst nach Freigabe durch den Nutzer öffnen. Die Beschreibung verweist zusätzlich auf die erfüllte Definition of Done.

---

## Teil D: Abdeckung der Anforderungen

| Anforderung (Auftrag) | Umgesetzt in |
| --- | --- |
| Umbau auf `alecthomas/kong` mit Best Practices | Task 3 (Kommandobaum, `Run`-Methoden, Bindings, `Validate`, `BeforeApply`, Default-Kommando, `VersionFlag`), Task 4 (eingebettete Gruppen, `group`-Tags, geteilte `Listen`-Gruppe) |
| Umbau CLI-Parameter-Parsing | Task 3 (`config.Load` entfällt; kong parst, hookt und validiert), Task 4 (gruppierte Optionen) |
| Umbau auf Commands (`ServerCmd` u. a.) | Task 3 (`ServeCmd` als `serve`, `HealthCheckCmd` als `healthcheck`), Task 5 (`MigrateCmd` als `migrate`) |
| `main.go` so schlank wie möglich | Task 3, Step 7; nach E13: `main()` → `run()`, `main.go` enthält Baum, Parsen und Dispatch; die Kommandos liegen in `internal/cli`, ihre Arbeit in `internal/*` |
| Review vom 2026-09-19 (Punkte 1–9) | E11 + Task 3/5 (Flags vor dem Kommando), E12 + Task 4 (Credentials in `serve --help`), Task 2/3/4 (Kommentar-Verweise), Task 3/4/5 (`clearEnv`, `unreachableDSN`), Task 2 (`run_test.go`, isolierte Stichprobe) und Abschluss, Step 4 (eigenes Compose-Projekt), Ablauf (Briefing), Vorbereitung, Step 2 und Task 3, Step 0 (Versionen), „Branches und PRs“ (Plan-PR, Squash-Footer) |
| Paket `main` so schlank wie möglich | Task 1 (Probe raus), Task 2 (Composition Root raus), Task 3 (`version.go` in `main.go`); geprüft in der Abschluss-Verifikation, Step 1 |
