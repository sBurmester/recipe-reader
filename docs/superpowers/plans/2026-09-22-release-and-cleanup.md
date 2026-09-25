# Backlog des CLI-Umbaus: Shutdown-Schranke, Konfiguration, Fehlermeldungen, Release-Automatik und Migrations-Engine

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Die vier Einträge im Backlog von `docs/superpowers/plans/2026-09-21-cli-backlog.md` abarbeiten — der Shutdown von `serve` bekommt seine obere Schranke zurück, `.env.example` erreicht den Container, zwei irreführende Fehlermeldungen werden eindeutig, und ein Tag nach Semantic Versioning erzeugt automatisch ein GitHub-Release mit Single-Binary und Container-Image — danach die Abhängigkeiten unter Aufsicht stellen: Renovate aktualisiert Go-Module, GitHub Actions und Docker-Images, letztere beide auf Digest gepinnt — und zuletzt die Migrations-Engine von `golang-migrate` auf `pressly/goose` umstellen.

**Architecture:** Drei kleine Korrekturen am bestehenden Code gehen voraus, die Release-Automatik folgt — so trägt das erste Release `v0.1.0` einen Stand, den man veröffentlichen will, und sein Changelog die drei Korrekturen. Der Advisory-Lock wandert von einer geliehenen Pool-Verbindung auf eine eigene `pgx.Connect`-Verbindung, damit `pool.Close()` nicht mehr auf ihn wartet. Die Release-Automatik besteht aus zwei getrennten Workflows: `release-please` schlägt Version und Changelog vor und legt beim Merge Tag und Release an, ein zweiter Workflow hängt beim `published`-Ereignis die Artefakte an. Renovate kommt danach, weil es die Workflows pinnen soll, die die Release-Automatik erst anlegt; es läuft als Mend-Renovate-GitHub-App (E9, geändert am 2026-09-23). Ganz zuletzt wird `golang-migrate` durch `pressly/goose` ersetzt: goose nimmt einen `context.Context` entgegen, legt jede Migration in eine eigene Transaktion und bringt den Advisory-Lock mit, sodass der handgeschriebene Abbruchpfad in `internal/db/connect.go` ersatzlos entfällt. Die sechs `*.up.sql`/`*.down.sql`-Dateien werden zu drei annotierten Dateien. Eine Übernahme bestehender `schema_migrations`-Tabellen gibt es nicht: die Software war nie im Einsatz, also existiert keine solche Datenbank außerhalb der Entwicklung.

**Tech Stack:** Go 1.27.1, pgx/v5, `github.com/alecthomas/kong` v1.16.1, Docker Compose 5.5.1, GitHub Actions (`googleapis/release-please-action@v5`, `docker/login-action@v4`, `docker/build-push-action@v7`, Mend-Renovate-GitHub-App), GitHub Container Registry, `github.com/pressly/goose/v3` v3.28.0 (ersetzt `github.com/golang-migrate/migrate/v4` v4.19.1) über `github.com/jackc/pgx/v5/stdlib`. **Genau ein Abhängigkeitstausch, sonst keine neue Go-Abhängigkeit.**

## Global Constraints

- **Branch:** Nie auf `main` arbeiten. Jeder Milestone hat einen eigenen Branch und einen eigenen PR; die Namen stehen unter „Branches und PRs".
- **Kompatibilität:** Bestehende Env-Variablen-Namen, Flag-Namen und Defaults bleiben **unverändert**. Exit-Codes bleiben `0` bei Erfolg und `1` bei jedem Fehler, auch bei Parse-Fehlern.
- **Konfigurationsdateien werden mitgepflegt:** Wer eine Env-Variable hinzufügt, trägt sie in `.env.example` nach, im Stil der Datei: unkommentiert, mit dem Default als Wert, thematisch gruppiert; Credentials stehen dort mit leerem Wert (`API_TOKEN=`).
- **Credentials:** `API_TOKEN`, `INSTAGRAM_PASSWORD`, `LLM_API_KEY` und `ANTHROPIC_API_KEY` bleiben nur über die Umgebung setzbar, ohne Flag-Form.
- **Namenskonvention:** Eine Funktion, die einen besonderen Parameter nimmt, heißt `…WithX` — `MigrateWithContext`, nicht `MigrateContext` (globale Go-Richtlinien, Stand 2026-09-22).
- **Kommentarstil:** Kommentare erklären das *Warum*, nicht das *Was*; Zeilen bis ~100 Spalten, wie im Rest des Repos.
- **Standardbibliothek zuerst:** Keine neue Go-Abhängigkeit in diesem Plan — mit genau einer Ausnahme, M6: `github.com/pressly/goose/v3` **ersetzt** `github.com/golang-migrate/migrate/v4`, es kommt nichts hinzu. Kein Task außerhalb von M6 darf eine Abhängigkeit ziehen. GitHub Actions sind keine Go-Abhängigkeit, aber jede zusätzliche Action braucht eine Zeile Begründung.
- **Migrationen ab M6:** Eine neue Migration ist **eine** Datei `internal/db/migrations/000N_<name>.sql` mit `-- +goose Up` und `-- +goose Down`, **ohne** eigenes `BEGIN;`/`COMMIT;` — goose legt jede Migration selbst in eine Transaktion. Vor M6 gilt weiter das Paar `*.up.sql`/`*.down.sql`.
- **Commit-Gate (vor jedem Commit, Pflicht laut `~/.claude/CLAUDE.md`; Docker muss laufen):**
  ```bash
  go fix ./...
  gofmt -w .
  go vet ./...
  ~/.local/bin/golangci-lint run ./...
  go run golang.org/x/vuln/cmd/govulncheck@latest ./...
  go test -count=1 -race ./...
  ```
  Jeder Befund wird vor dem Commit behoben. Die `git commit`-Beispiele in den Task-Schritten zeigen den Footer nicht; er ist trotzdem Pflicht.
- **Docker-Gate (für jeden Task, der `Dockerfile` oder `scripts/` ändert):**
  ```bash
  docker build --build-arg VERSION=smoke -t recipe-reader:ci .
  scripts/smoke-test-image.sh recipe-reader:ci smoke
  ```
  Erwartet: `smoke test passed: recipe-reader:ci (smoke)`
- **CI-Ausnahme:** Seit PR #29 überspringt die CI Läufe, bei denen **jede** geänderte Datei auf `**.md`, `docs/**` oder `.github/workflows/**` passt. Ein Task, der nur einen Workflow ändert, wird von der CI also nicht geprüft — er wird von Hand über `workflow_dispatch` oder auf einem Branch mit Codeänderung geprüft.
- **Gepinnte Fremdabhängigkeiten (ab M5):** Jede GitHub Action und jedes Docker-Basisimage steht mit seinem Digest da, mit dem lesbaren Tag als Kommentar dahinter (`uses: actions/checkout@<sha> # v7`). Wer eine Action oder ein Image hinzufügt, pinnt es sofort mit; Renovate hebt die Digests danach.
- **Commit-Footer:** Jede Commit-Message endet mit
  ```
  Assisted-by: <Modellname> (<Effort>) via Claude Code
  Co-Authored-By: <Modellname> <noreply@anthropic.com>
  ```
- **Fortschritt:** Erledigte Steps und Tasks werden **in dieser Datei** abgehakt (`[x]`).

---

## Teil A: Product-Owner-Sicht

### Ausgangslage

Der Plan vom 2026-09-21 ist abgeschlossen (PRs #30, #32, #33). Sein Backlog nennt vier Punkte:

1. **Der Shutdown von `serve` hat keine obere Schranke mehr.** Seit M3 hält ein Importlauf eine Pool-Verbindung über seine ganze Dauer — die Session des Advisory-Locks. Das `defer pool.Close()` in `server.Run` wartet über puddles `destructWG` auf jede Verbindung, also auch auf einen Import, den das Zehn-Sekunden-Budget gerade ausdrücklich aufgegeben hat. Zwischen „import did not stop within the shutdown budget; abandoning it" und dem tatsächlichen Return liegt dann bis zu ein `LLM_TIMEOUT` (Default 60 s) ohne eine Logzeile.
2. **`.env.example` und `docker-compose.yml` laufen auseinander.** Der `app`-Service reicht nur die Variablen durch, die unter `environment:` stehen; ein `env_file:` gibt es nicht. Dreizehn Einträge aus `.env.example` erreichen den Container nie — `EXTRACTION_*`, `IMPORT_*`, `LLM_*`, `ANTHROPIC_MODEL`, `INSTAGRAM_SESSION_PATH`, `LOG_LEVEL`, `LOG_FORMAT` —, obwohl die README-Anleitung mit `cp .env.example .env` beginnt.
3. **Fehlermeldungen sind nicht eindeutig.** Zwei Stellen sind benannt: ein abgebrochener Migrationslauf meldet `db: migrate up: context canceled`, ohne zu sagen, ob etwas angewandt wurde oder ob ein zweiter Versuch gefahrlos ist; und `main` loggt jeden Fehler als dieselbe Zeile `slog.Error("fatal", "error", err)`, sodass ein Tippfehler in der Kommandozeile aussieht wie ein Absturz im Betrieb.
4. **Es gibt keine Releases.** Das Repository hat kein einziges Tag; `git describe --tags --always --dirty` — die Quelle, aus der `Makefile` und `Dockerfile` `-X main.version` speisen — liefert einen nackten SHA. Wer die Software einsetzen will, muss sie selbst bauen.

Dazu kommt ein fünfter Punkt, den der Nutzer am 2026-09-22 gesetzt hat:

5. **`golang-migrate` passt nicht mehr zu dem, was der Code von ihm verlangt.** Die Bibliothek kennt keinen `context.Context`, nur den Kanal `GracefulStop`. Um einen Abbruch überhaupt melden zu können, trägt `internal/db/connect.go` dafür ein eigenes Gerüst: das Interface `migrator`, den Adapter `gracefulMigrator`, ein `context.AfterFunc` und eine `ctx.Err()`-Prüfung nach `Up()`, weil ein gestopptes `Up()` `nil` zurückgibt. Der Doc-Kommentar dazu ist länger als die Funktion und nennt selbst zwei Schwächen: der letzte Guard meldet einen vollständigen Lauf als Fehler, wenn der Abbruch während der letzten Migration eintrifft, und `gracefulMigrator.Stop` ist bewusst ungetestet, weil golang-migrate sein `isGracefulStop`-Flag ohne Synchronisation aus zwei Goroutinen liest und schreibt. `pressly/goose` nimmt einen Kontext entgegen, legt jede Migration in eine eigene Transaktion und bringt den Advisory-Lock als Option mit — das gesamte Gerüst wird damit überflüssig.

### Ziele (Outcomes)

| # | Ziel | Messbar an |
| --- | --- | --- |
| Z1 | Ein aufgegebener Import hält das Herunterfahren nicht mehr auf | Der gehaltene Lock belegt keine Pool-Verbindung (Test) |
| Z2 | Was in `.env` steht, wirkt auch im Container | Eine Variable aus `.env.example` ist im laufenden Container sichtbar (Compose-Lauf) |
| Z3 | Eine Fehlermeldung sagt, was zu tun ist | Der Abbruchfall nennt die gefahrlose Wiederholung; ein Bedienfehler ist als solcher erkennbar (Tests) |
| Z4 | Ein Tag erzeugt ein vollständiges Release | `v0.1.0` trägt Binaries für drei Plattformen, Prüfsummen und ein Image in der GHCR |
| Z5 | Compose kann ein veröffentlichtes Image nutzen | `docker compose pull && docker compose up -d` läuft ohne lokalen Build |
| Z6 | Die Migrationen brauchen keinen handgeschriebenen Abbruchpfad mehr | `internal/db/connect.go` enthält weder `migrator` noch `gracefulMigrator` noch ein `context.AfterFunc`, und `go test -race ./...` bleibt grün |

### Nicht-Ziele

- Keine vollständige Bestandsaufnahme aller Fehlermeldungen. Dieser Plan behebt die zwei benannten Stellen; die Frage, ob kongs Kommandopfad-Präfix (`serve: config: API_TOKEN …`) einem Betreiber hilft, bleibt im Backlog, weil sie ohne Nutzerurteil nicht zu entscheiden ist.
- Keine Signatur oder Provenance-Attestierung der Artefakte.
- Kein Multi-Arch-Image. Das Image wird für `linux/amd64` gebaut, wie heute auch; Multi-Arch steht im Backlog.
- Keine Änderung am HTTP-API, an der Extraktion oder am Datenbankschema. Das gilt ausdrücklich auch für M6: die Migrationsdateien werden **umformatiert**, ihre Anweisungen bleiben Zeichen für Zeichen dieselben — mit der einen Ausnahme der `BEGIN;`/`COMMIT;`-Klammern, die goose selbst setzt, und der zwei Kommentarzeilen, die auf sie verwiesen.
- Keine neue Migration in M6, keine Umnummerierung. `0001`, `0002`, `0003` bleiben `0001`, `0002`, `0003`.
- **Keine Übernahme bestehender `schema_migrations`-Tabellen** (Nutzer, 2026-09-22): die Software war nie im Einsatz. Eine Entwicklungsdatenbank, die ein älterer Build angelegt hat, wird neu angelegt statt umgeschrieben — die README nennt beide Wege (Task 12).
- Kein `goose`-Kommandozeilenwerkzeug im Image und kein `goose`-Unterkommando in der CLI. Die Binary migriert weiter über `recipe-reader migrate` und beim Start von `serve`.
- Keine Go-Migrationen (`goose.AddMigration`). Migrationen bleiben SQL-Dateien.

### User Stories & Akzeptanzkriterien

**US1: Betreiber fährt den Server herunter, während ein Import läuft.**
- [x] Der Advisory-Lock belegt keine Verbindung aus dem Pool; `pool.Stat().AcquiredConns()` ist null, während der Lock gehalten wird.
- [x] `server.Run` kehrt nach einem `SIGTERM` innerhalb seines Budgets zurück, auch wenn ein Import aufgegeben wurde.
- [x] Der Doc-Kommentar von `Run` behauptet nichts mehr, was nicht gilt.

**US2: Betreiber konfiguriert über `.env`.**
- [x] Eine Variable, die nur in `.env` steht (z. B. `EXTRACTION_MODE`), ist im laufenden `app`-Container gesetzt.
- [x] Fehlt `.env`, startet Compose trotzdem.
- [x] Die zusammengesetzten Werte unter `environment:` (`DB_DSN`) gewinnen gegen `.env` — für `DB_DSN` erst seit M2, siehe die Abweichung in Task 2.

**US3: Betreiber liest eine Fehlermeldung.**
- [x] Ein abgebrochener `migrate`-Lauf sagt, dass bereits angewandte Migrationen angewandt bleiben und ein erneuter Lauf gefahrlos fortsetzt.
- [x] Eine abgelehnte Kommandozeile wird als Bedienfehler gemeldet, ein gescheiterter Lauf als Fehler; beide enden weiterhin mit 1.

**US4: Betreiber installiert eine Version.**
- [x] Ein Merge des Release-PRs erzeugt Tag und GitHub-Release mit Changelog.
- [x] Das Release trägt Binaries für `linux/amd64`, `linux/arm64` und `darwin/arm64` sowie eine `checksums.txt`.
- [x] `recipe-reader --version` einer geladenen Binary meldet genau das Tag.
- [x] Das Image `ghcr.io/sburmester/recipe-reader:<tag>` existiert und besteht den Smoke-Test.

**US5: Betreiber betreibt über Compose.**
- [x] `docker compose pull && docker compose up -d` startet ohne lokalen Build.
- [x] `docker compose up --build` baut weiterhin lokal, für die Entwicklung.

**US6: Entwickler hält die Abhängigkeiten aktuell, ohne sie zu suchen.**
- [x] Renovate läuft nach Zeitplan als GitHub-App und lässt sich über das „Dependency Dashboard“-Issue von Hand anstoßen.
- [x] Es öffnet PRs für Go-Module, GitHub Actions und Docker-Images.
- [x] Sicherheitslücken (OSV) werden mindestens täglich geprüft; ein Fix-PR wartet nicht auf den Wochenplan. *(Abgenommen über die Konfiguration, siehe die Abnahme unter M5.)*
- [x] Jede Action und jedes Basisimage steht mit Digest im Repository, mit dem Tag als Kommentar.
- [x] Ein Go-Update-PR durchläuft die CI wie jeder andere Code-PR.

**US7: Entwickler fügt eine Migration hinzu.**
- [x] Die README beschreibt **eine** Datei mit `-- +goose Up` und `-- +goose Down`.
- [x] `make sqlc-generate` erzeugt danach unveränderten Code, und die CI-Drift-Prüfung bleibt grün.
- [x] `TestMigrations_UpDownUp` deckt die neue Migration in beide Richtungen ab, ohne dass der Test angefasst wird.

### Entscheidungen

| # | Entscheidung | Begründung |
| --- | --- | --- |
| E1 | Die Version vergibt **`release-please`** aus den Conventional-Commits-Präfixen, nicht ein von Hand geschobenes Tag (Nutzer, 2026-09-22) | Das Repo schreibt seit Beginn disziplinierte Präfixe (`feat:`, `fix:`, `refactor!:`), und der Changelog fällt damit ohne Zusatzarbeit ab. Der Preis: ein falsch gewähltes Präfix wird zur falschen Version — deshalb R1. |
| E2 | Die Zählung beginnt bei **`v0.1.0`**, nicht bei `v1.0.0` (Nutzer, 2026-09-22) | Unter SemVer erlaubt `0.x` Breaking Changes in jedem Minor. Das Projekt hat in drei Tagen zweimal gebrochen (`--health-check` entfiel, `MigrateContext` wurde umbenannt); `v1.0.0` würde eine Stabilität zusichern, die die Oberfläche nicht hat. |
| E3 | Der Advisory-Lock läuft auf einer **eigenen `pgx.Connect`-Verbindung**, nicht auf einer geliehenen Pool-Verbindung (Nutzer, 2026-09-22) | Kleinster Eingriff, der die Wirkung sauber behebt: was nicht aus dem Pool stammt, kann `pool.Close()` nicht aufhalten. Die Ursache — ein aufgegebener Lauf endet erst nach seinem LLM-Timeout — bleibt und steht im Backlog. |
| E4 | Compose bekommt **`env_file:`**, statt die fehlenden dreizehn Variablen einzeln unter `environment:` nachzutragen (Nutzer, 2026-09-22) | Eine Zeile, und die Drift kann nicht wiederkehren: jede künftige Variable wirkt ohne weiteres Zutun. Eine kuratierte Liste müsste bei jeder neuen Variable an zwei Stellen gepflegt werden, und `${VAR:-}` würde den Default ein zweites Mal festschreiben. |
| E5 | Die Release-Automatik kommt **zuletzt**, nach den drei Korrekturen | Das erste Release soll einen Stand tragen, den man veröffentlichen will. Nebenbei füllt es das Changelog von `v0.1.0` mit den drei Korrekturen statt mit nichts. |
| E6 | `docker-compose.yml` bekommt **`image:` und behält `build:`** | Betreiber ziehen das veröffentlichte Image (`docker compose pull`), Entwickler bauen lokal (`docker compose up --build`). Compose nimmt bei beiden Schlüsseln das lokal vorhandene Image und baut sonst — beide Arbeitsweisen bestehen nebeneinander, ohne dass eine die andere ausschließt. |
| E7 | Zwei Workflows statt einem: `release-please.yml` und `release-artifacts.yml` | `release-please` läuft bei jedem Push auf `main` und hält einen PR offen; das Bauen der Artefakte soll genau einmal laufen, wenn das Release existiert. Ein Workflow müsste beides in einem Lauf unterscheiden. |
| E8 | Das Changelog beginnt bei einem **`bootstrap-sha`**, nicht bei der ersten Zeile der Historie | Ohne Grenze schreibt `release-please` die gesamte Projekthistorie in das erste Changelog. Siehe R1. |
| E9 | Renovate läuft als **Mend-Renovate-GitHub-App**, nicht selbst gehostet als Workflow (Nutzer, 2026-09-23; ersetzt die Entscheidung vom 2026-09-22) | Die ursprüngliche Begründung für den Workflow — das Repository sei privat und eine fremde App bräuchte Lesezugriff — trifft nicht zu: das Repository ist öffentlich. Die App braucht kein Personal Access Token, das angelegt und erneuert werden muss, und ihre PRs lösen die CI aus. Die Konfiguration bleibt versioniert in `renovate.json`, der Zeitplan steht dort (`schedule`). Preis: eine Abhängigkeit von Mends Dienst; die Logs der Läufe liegen im Mend-Portal (developer.mend.io), nicht in GitHub Actions. Die Installation macht der Nutzer (Task 9). |
| E10 | Das Pinnen macht **Renovate selbst**, nicht ein Task von Hand (Nutzer, 2026-09-22) | Genau dafür stehen `helpers:pinGitHubActionDigests` und `pinDigests` in `renovate.json`. Digests von Hand aufzulösen würde denselben Mechanismus ein zweites Mal bauen — und die Handarbeit wäre schon beim nächsten Update überholt. Task 10 stößt Renovate an, liest seine PRs und merged sie; überprüfbar ist das Ergebnis, nicht der Weg dorthin. |
| E11 | M5 kommt **nach** M4 | Gepinnt werden soll auch, was M4 anlegt: `release-please.yml` und `release-artifacts.yml` bringen fünf weitere Actions mit. Andersherum müsste M4 an das Pinning denken, und M5 müsste nachbessern. |
| E12 | `pressly/goose` **ersetzt** `golang-migrate` (Nutzer, 2026-09-22) | goose nimmt überall einen `context.Context`, legt jede SQL-Migration in eine eigene Transaktion und bringt den Advisory-Lock als Option mit. Damit entfallen `migrator`, `gracefulMigrator`, das `context.AfterFunc` und die `ctx.Err()`-Prüfung nach `Up()` ersatzlos — rund 70 Zeilen Gerüst plus der Kommentar, der zwei bekannte Schwächen davon einräumt. Der Preis ist ein Modultausch, kein Zuwachs: golang-migrate geht, goose kommt. |
| E13 | M6 kommt **zuletzt**, nach M5 | Renovate steht dann schon und beobachtet die neue Abhängigkeit vom ersten Tag an, und `v0.1.0` wird nicht von einem Engine-Tausch aufgehalten. Preis: `v0.1.0` trägt noch golang-migrate, und die Meldung aus Task 3 wird in Task 11 einmal nachgeschärft (R13). Wer das anders gewichtet, zieht M6 vor M4 — dann trägt kein veröffentlichtes Artefakt je golang-migrate. |
| E14 | Die sechs Migrationsdateien werden zu **drei** Dateien mit `-- +goose Up`/`-- +goose Down` | Das ist goose' einziges Dateiformat; zwei Dateien je Version kann es nicht lesen. Die `BEGIN;`/`COMMIT;`-Klammern in `0002` und `0003` **entfallen dabei**, weil goose jede Migration ohnehin in eine Transaktion legt — ein `COMMIT;` mitten darin würde goose' eigene Transaktion vorzeitig beenden. Das SQL bleibt sonst unverändert, Kommentare eingeschlossen. |
| E15 | **Keine** Übernahme bestehender `schema_migrations`-Tabellen (Nutzer, 2026-09-22) | Die Software war nie im Einsatz, also gibt es keine Datenbank, die übernommen werden müsste. Der Code dafür wäre eine Datei, ein Testfile und eine Abfrage bei jedem Start — für einen Fall, der nicht eintritt. Was bleibt, ist die Entwicklungsdatenbank des Betreibers: sie wird neu angelegt, und wer ihre Zeilen behalten will, schreibt das Bookkeeping einmal von Hand (drei Anweisungen, in der README unter Task 12). |
| E16 | goose bekommt den `PostgresSessionLocker` | Kein Zugewinn, sondern Gleichstand: golang-migrates pgx-Treiber nimmt von sich aus ein `pg_advisory_lock` (`database/postgres/postgres.go:241`). Ohne die Option fiele dieser Schutz beim Tausch ersatzlos weg, und zwei gleichzeitig startende Instanzen würden dieselbe Migration nebeneinander anwenden. goose' Default-ID ist `4097083626` und kollidiert nicht mit dem Import-Lock dieses Projekts (`8_233_071_001`). |
| E17 | Die Migrationen laufen über ein **eigenes `*sql.DB`** (`pgx/v5/stdlib`), nicht über den `pgxpool` | goose spricht `database/sql`, das Projekt spricht pgx. Genau eine Stelle übersetzt, und sie wird nach dem Lauf geschlossen. Das ist zugleich die Linie aus E3: die Migration borgt sich nichts aus dem Pool, den der Dienst danach braucht. |
| E18 | goose loggt über `slog.Default()` mit `WithVerbose(true)` | Heute migriert das Programm stumm; fällt ein Start auf, sagt nichts, welche Migration gerade läuft. goose ist ohne `WithVerbose` ebenfalls stumm, mit `WithSlog(slog.Default())` fügt es sich in `LOG_LEVEL`/`LOG_FORMAT` ein. Preis: eine Zeile je angewandter Migration und eine `no migrations to run`-Zeile je Start. |

### Risiken

| # | Risiko | Gegenmaßnahme |
| --- | --- | --- |
| R1 | Das erste Changelog enthält die komplette Historie | `bootstrap-sha` in `release-please-config.json` auf den Merge-Commit von PR #33 setzen (E8). Der erste Release-PR wird vor dem Merge gelesen, nicht blind bestätigt. |
| R2 | Ein falsches Commit-Präfix erzeugt die falsche Version | Der Release-PR zeigt die vorgeschlagene Version, bevor er gemergt wird. Ein `feat:`, das ein `fix:` sein sollte, kostet einen Minor — unter `0.x` folgenlos. |
| R3 | Das GHCR-Paket ist nach dem ersten Push privat, `docker compose pull` scheitert für andere | Das ist für ein Single-User-Deployment richtig so. Die README nennt den Handgriff, das Paket in den Repository-Einstellungen auf öffentlich zu stellen, falls gewünscht. |
| R4 | `env_file` schleust unerwartete Variablen in den Container | `.env` ist die Datei des Betreibers, und alles darin ist für genau diesen Dienst gedacht. Die zusammengesetzten `environment:`-Werte gewinnen weiterhin, was Task 2 prüft. |
| R5 | Der Workflow-Task wird von der CI nicht geprüft (CI-Ausnahme aus #29) | Die Workflows werden über `workflow_dispatch` bzw. einen echten Release-Lauf geprüft, nicht über CI. Task 7 endet mit einem echten `v0.1.0`. |
| R6 | `pgx.Connect` ohne die Pool-Defaults bekommt kein `connect_timeout` | Der DSN, den `ImportLock` bekommt, ist derselbe wie der des Pools; enthält er `connect_timeout`, gilt es auch hier. Der Aufruf steht ohnehin unter dem Kontext des Laufs. |
| R7 | ~~Renovate braucht ein Token, das der Nutzer anlegen muss~~ | Entfallen mit E9 in der Fassung vom 2026-09-23: die GitHub-App bringt ihre eigene Berechtigung mit. An seine Stelle tritt die Installation der App durch den Nutzer (Task 9); ohne sie öffnet Renovate nichts, und Task 10 hält an. |
| R8 | Renovate-PRs, die **nur** Workflows anfassen, laufen wegen der CI-Ausnahme ohne CI | Genau die Änderungen, die man geprüft haben will, sind ungeprüft. Gegenmaßnahme: Renovate fasst Action-Updates zu einem PR zusammen (`groupName`), der von Hand über `workflow_dispatch` der CI vorgelegt wird, bevor er gemergt wird. Der Hinweis steht in der README. |
| R9 | Ein gepinntes Basisimage friert Sicherheitsupdates ein, wenn Renovate ausfällt | Heute zieht `golang:1.27-alpine` Patches beim nächsten Build von selbst — gepinnt nicht mehr. Der Zeitplan (wöchentlich) und das „Dependency Dashboard“, über das sich ein wartendes Update sofort anstoßen lässt, sind die Gegenmaßnahme; die Kommentare im `Dockerfile`, die das alte Verhalten beschreiben, werden in Task 10 berichtigt, damit niemand sich auf etwas verlässt, das nicht mehr gilt. |
| R10 | sqlc liest dieselben Migrationsdateien; die goose-Annotationen könnten den generierten Code ändern | Am 2026-09-22 gegen die im Modul gepinnte sqlc v1.31.1 geprüft: `internal/migrations/migrations.go:26` bricht das Einlesen bei `-- +goose down` ab (Kleinschreibung, `strings.ToLower` davor), `IsDown` streicht `*.down.sql`. Beide Formate werden also unterstützt. Task 11 belegt es trotzdem: `make sqlc-generate` und `git diff --exit-code internal/db/sqlc` — die CI prüft dieselbe Drift. |
| R11 | `testdb.reset` truncatet jede Tabelle außer `schema_migrations` — also künftig auch `goose_db_version` | Konkret benannt in Task 11, Steps 7 und 8: die Ausnahmeliste in `internal/db/migrations_test.go:182` und in `internal/db/testdb/testdb.go:155` muss auf `goose_db_version` umgestellt werden. Bliebe sie stehen, leerte der erste Test die Versionstabelle des geteilten Containers. |
| R12 | Eine Datenbank, die ein Build vor M6 angelegt hat, startet nicht mehr | goose findet kein `goose_db_version`, hält die Datenbank für leer, wendet `0001` erneut an und scheitert an `CREATE TABLE units`. Das ist gewollt (E15) und betrifft nur Entwicklungsdatenbanken — die Tests legen über `testdb` immer frische an. Die README nennt beide Auswege, Neuanlegen und das Bookkeeping von Hand; die Abschluss-Verifikation führt einen davon aus. |
| R13 | Die Zusicherung bei Abbruch ändert sich gegenüber Task 3 | golang-migrate bricht *zwischen* zwei Migrationen ab und beendet die laufende; goose bricht die laufende ab und rollt ihre Transaktion zurück. Beide Male ist nichts halb angewandt und ein zweiter Lauf gefahrlos — nur der Halbsatz *warum* stimmt danach nicht mehr. Task 11 schärft ihn nach; das Wort `re-running`, auf das der Test aus Task 3 prüft, bleibt erhalten. |

### Definition of Done

- [x] Alle Tasks in dieser Datei abgehakt.
- [x] Commit-Gate grün; Docker-Gate grün für die Tasks, die das Image betreffen.
- [x] Die vier Backlog-Einträge im Plan vom 2026-09-21 sind als erledigt markiert oder verweisen auf diesen Plan.
- [x] `v0.1.0` existiert als Tag, als GitHub-Release mit Changelog und als Image in der GHCR.
- [x] Renovate läuft und hat mindestens einen PR geöffnet; jede Action und jedes Basisimage ist auf Digest gepinnt.
- [x] `go.mod` nennt `github.com/pressly/goose/v3` und **nicht** mehr `github.com/golang-migrate/migrate/v4`; `grep -rn golang-migrate --include='*.go' .` findet nichts mehr.
- [x] README aktualisiert: Installation aus einem Release, Betrieb über das veröffentlichte Image, Hinweis auf `env_file`, Abschnitt zu Renovate und den gepinnten Digests, das goose-Dateiformat unter „Changing the database schema“, `goose_db_version` im Restore-Abschnitt und der Hinweis, wie eine ältere Entwicklungsdatenbank wieder brauchbar wird.

### Backlog (bewusst nicht in diesem Plan)

> **Widerlegt und behoben (2026-09-25).** Der folgende Eintrag ist im Plan [2026-09-25-backlog-cleanup.md](2026-09-25-backlog-cleanup.md) geschlossen (M1). Seine Prämisse trug nicht: der Abbruch erreicht den LLM-Aufruf sofort, gemessen gegen beide SDKs bei einem Timeout von einer Stunde. Die echte Lücke war, dass `HybridExtractor` den Abbruch als LLM-Ausfall auffing und die Pipeline den unterbrochenen Post als fehlgeschlagen zählte.

- **Der aufgegebene Import endet erst nach seinem LLM-Timeout.** E3 behebt die Wirkung auf das Herunterfahren, nicht die Ursache. Wer sie aufgreift, reicht den Kontextabbruch bis in den Extraktionspfad durch, sodass ein aufgegebener Lauf sofort endet statt nach bis zu `LLM_TIMEOUT`.
- **Kongs Kommandopfad-Präfix** (`serve: config: API_TOKEN …`): dass der Inhalt gleich bleibt, ist geprüft, ob das Präfix einem Betreiber hilft oder im Weg steht, nie. Braucht ein Nutzerurteil, keinen Task.
- **Multi-Arch-Image.** Das Release baut `linux/amd64`. Ein `linux/arm64`-Image bräuchte QEMU oder einen ARM-Runner.
- **Signatur und Provenance** der Release-Artefakte.
- **Migrationen mit `-- +goose NO TRANSACTION`.** goose kann eine Migration auf Wunsch ohne Transaktion fahren, was `CREATE INDEX CONCURRENTLY` erst möglich macht. Keine der heutigen Migrationen braucht es; die erste, die einen Index auf einer großen Tabelle anlegen will, schon.

---

## Teil B: Zielbild

### Dateistruktur nach Abschluss

```
.github/workflows/
  ci.yml                     # unverändert
  release-please.yml         # neu: Release-PR, Tag, GitHub-Release
  release-artifacts.yml      # neu: Binaries + Prüfsummen + GHCR-Image, on: release published
release-please-config.json   # neu
.release-please-manifest.json# neu
CHANGELOG.md                 # neu, von release-please gepflegt
docker-compose.yml           # + env_file, + image
internal/db/
  lock.go                    # ImportLock hält eine eigene Verbindung
  lock_test.go               # + Test, dass der Pool unberührt bleibt
  connect.go                 # Fehlermeldung des Abbruchs; ab M6 goose statt golang-migrate
  migrate_test.go            # M6: der Fake-Seam entfällt, ein Lock-Test tritt an seine Stelle
  migrations_test.go         # M6: fileMigrator wird zum goose-Provider
  migrations/
    0001_init.sql            # M6: ersetzt 0001_init.up.sql + 0001_init.down.sql
    0002_recipe_status_check.sql
    0003_recipe_ingredient_position_unique.sql
  testdb/testdb.go           # M6: reset verschont goose_db_version statt schema_migrations
internal/server/
  server.go, import.go       # ImportLock{DSN: …}
cmd/recipe-reader/
  main.go                    # Bedienfehler vs. Laufzeitfehler
  main_test.go               # + erwartete Logzeile in der Tabelle
README.md                    # M6: Migrationsformat, goose_db_version, Hinweis auf alte Entwicklungsdatenbanken
```

---

## Teil C: Aufgaben

### Milestones

| Milestone | Tasks | Ergebnis | Für Betreiber sichtbar |
| --- | --- | --- | --- |
| **M1:** Shutdown-Schranke | Task 1 | Der Lock hält keine Pool-Verbindung mehr | nein |
| **M2:** Konfiguration erreicht den Container | Task 2 | `.env` wirkt im Container | ja |
| **M3:** Eindeutige Fehlermeldungen | Task 3, Task 4 | Abbruch und Bedienfehler sagen, was sie sind | ja |
| **M4:** Release-Automatik | Task 5, Task 6, Task 7 | `v0.1.0` mit Binaries und Image | ja |
| **M5:** Abhängigkeiten unter Aufsicht | Task 8, Task 9, Task 10 | Renovate hält Go, Actions und Images aktuell; Actions und Images sind auf Digest gepinnt | nein |
| **M6:** Migrationen unter goose | Task 11, Task 12 | `golang-migrate` ist fort, der handgeschriebene Abbruchpfad auch | nur im Log |

#### M1: Shutdown-Schranke

Abnahmekriterien:
- [x] Die Akzeptanzkriterien von US1 sind abgehakt.
- [x] `go test -count=1 -race ./internal/db/ ./internal/server/` ist grün.

#### M2: Konfiguration erreicht den Container

Abnahmekriterien:
- [x] Die Akzeptanzkriterien von US2 sind abgehakt.
- [x] Der Compose-Lauf aus Task 2, Step 4 ist belegt.

#### M3: Eindeutige Fehlermeldungen

Abnahmekriterien:
- [x] Die Akzeptanzkriterien von US3 sind abgehakt.
- [x] Beide Meldungen sind durch einen Test abgesichert, der ohne die Änderung fehlschlägt.

#### M4: Release-Automatik

Abnahmekriterien:
- [x] Die Akzeptanzkriterien von US4 und US5 sind abgehakt.
- [x] `v0.1.0` existiert als Tag, Release und Image.

> **Abnahme (2026-09-23).** `v0.1.0` existiert als Tag, Release und Image, aber ohne Binaries (Immutable Releases, siehe Task 6). Vollständig ist das erste Release `v0.1.1` (Lauf 35897640656): als Entwurf angelegt, drei Binaries und `checksums.txt` angehängt, danach veröffentlicht; `isImmutable: true`. Geprüft wie ein Betreiber: `gh release download` + `sha256sum --check` OK, `--version` = `v0.1.1`, keine Build-Pfade, echtes Vite-Bundle eingebettet; `ghcr.io/sburmester/recipe-reader:v0.1.1` anonym ziehbar, `latest` hat denselben Digest, Smoke-Test bestanden; `docker compose pull && up -d --no-build` → beide Container healthy, `/api/healthz` meldet `v0.1.1`; `RECIPE_READER_VERSION=v0.1.0` zieht die ältere Version.

#### M5: Abhängigkeiten unter Aufsicht

Abnahmekriterien:
- [x] Die Akzeptanzkriterien von US6 sind abgehakt.
- [x] `grep -rn 'uses: .*@v[0-9]' .github/workflows/` findet nichts mehr.
- [x] Jedes `FROM` im `Dockerfile` und jedes `image:` in `docker-compose.yml`, das nicht aus der GHCR dieses Projekts stammt, trägt einen Digest.

> **Stand (2026-09-23).** Offen bis zum ersten regulären Lauf: *„Ein Go-Update-PR durchläuft die CI“* (die Gruppe `go modules` war rate-limited, die Limits hebt #53 auf) und *„Sicherheitslücken … mindestens täglich“* (konfiguriert, noch ohne Anlass). Damit ist auch das Abnahmekriterium „Akzeptanzkriterien von US6“ noch offen.

> **Nachtrag (2026-09-23).** Der Go-Update-PR ist belegt: #56 `fix(go): update go modules` (Labels `dependencies`, `go`; anthropic-sdk-go 1.75.0, golang-migrate 4.20.1, pgx 5.11.0, `go.mod` + `go.sum`) lief durch backend, frontend und docker, alles grün. Offen bleibt allein der erste Sicherheits-PR — er entsteht erst, wenn OSV eine Lücke in einer Abhängigkeit meldet. Außerdem belegt: der Release-PR #55 (`v0.1.3`) enthält nur den `fix:` aus #52; die `ci:`/`docs:`/`chore:`-Commits #49–#54 erscheinen nicht und lösten kein Release aus.

> **Abnahme (2026-09-23, vom Nutzer entschieden).** Das OSV-Kriterium gilt als erfüllt, obwohl noch kein Sicherheits-PR existiert: es gab keinen Anlass für einen. `renovate.json` setzt `osvVulnerabilityAlerts: true` und `vulnerabilityAlerts.schedule: ["at any time"]`, ohne PR-Limit. `govulncheck` auf `main` (`16d79d0`) meldet allein GO-2026-5932 (`golang.org/x/crypto/openpgp`, vom Code nicht aufgerufen) — ohne Fix-Version (`Fixed in: N/A`), also richtigerweise ohne PR; `npm audit` meldet 0 Lücken. Die übrigen Kriterien sind auf `main` nachgeprüft: kein `uses: …@v<n>` in `.github/workflows/`, jedes fremde `FROM`/`image:` mit Digest, PRs für Go (#56, #59), Actions (#49, #58) und Docker (#51).

#### M6: Migrationen unter goose

Abnahmekriterien:
- [x] Die Akzeptanzkriterien von US7 sind abgehakt.
- [x] `go test -count=1 -race ./...` ist grün.
- [x] `grep -rn 'golang-migrate' --include='*.go' .` findet nichts mehr, und `go.mod` nennt es nicht mehr.
- [x] `internal/db/connect.go` enthält weder `migrator` noch `gracefulMigrator` noch `context.AfterFunc`.
- [x] `make sqlc-generate` lässt `internal/db/sqlc/` unverändert (`git diff --exit-code internal/db/sqlc`).
- [x] Die Definition of Done ist abgehakt.

#### Branches und PRs

| Milestone | Branch | PR-Titel |
| --- | --- | --- |
| M1 | `fix/import-lock-own-connection` | `fix(db): hold the import lock on its own connection (1/6)` |
| M2 | `fix/compose-env-file` | `fix(compose): pass .env into the app container (2/6)` |
| M3 | `fix/clearer-error-messages` | `fix(cli): say what a cancelled migration and a bad command line mean (3/6)` |
| M4 | `feat/release-automation` | `feat(ci): release automatically from a version tag (4/6)` |
| M5 | `ci/renovate` | `ci: keep dependencies updated with renovate (5/6)` |
| M6 | `refactor/goose-migrations` | `refactor(db)!: migrate with goose instead of golang-migrate (6/6)` |

Das `!` im Titel von PR 6/6 ist beabsichtigt und keine Formsache. Die Versionstabelle heißt danach `goose_db_version`, und nichts überträgt die alte (E15): eine Datenbank, die ein älterer Build angelegt hat, ist danach nicht mehr benutzbar, und eine ältere Binary kann eine neu angelegte nicht lesen. Das ist ein Breaking Change und gehört ins Changelog, auch wenn niemand betroffen ist. Nebenbei ist es das, was `release-please` überhaupt zu einem Release bewegt: `refactor:` allein hebt keine Version und steht nicht im Changelog (seit 2026-09-23 `hidden`, siehe Task 5); erst das `!` macht daraus einen Breaking Change mit Release.

Jeder Milestone-Branch zweigt von `main` ab, nachdem der vorige PR gemerged ist. Ein PR enthält die Task-Commits seines Milestones, die Korrekturen aus dem Milestone-Review und einen letzten Commit `docs: mark M<n> done in the release plan`.

### Aufgabenliste

- [x] **M1: Shutdown-Schranke**
  - [x] **Task 1:** Der Import-Lock hält eine eigene Verbindung (S)
  - [x] **Milestone-Review**
- [x] **M2: Konfiguration erreicht den Container**
  - [x] **Task 2:** `env_file` für den `app`-Service (S)
  - [x] **Milestone-Review**
- [x] **M3: Eindeutige Fehlermeldungen**
  - [x] **Task 3:** Der Abbruch einer Migration sagt, dass Wiederholen gefahrlos ist (S)
  - [x] **Task 4:** `main` unterscheidet Bedienfehler von Laufzeitfehler (S)
  - [x] **Milestone-Review**
- [x] **M4: Release-Automatik**
  - [x] **Task 5:** `release-please` einrichten (M)
  - [x] **Task 6:** Binaries und Prüfsummen ans Release hängen (M)
  - [x] **Task 7:** Image in die GHCR, Compose darauf umstellen (M)
  - [x] **Milestone-Review**
- [x] **M5: Abhängigkeiten unter Aufsicht**
  - [x] **Task 8:** `renovate.json` (S)
  - [x] **Task 9:** Renovate als GitHub-App (S)
  - [x] **Task 10:** Renovate pinnt Actions und Images (M)
  - [x] **Milestone-Review**
- [x] **M6: Migrationen unter goose**
  - [x] **Task 11:** goose ersetzt golang-migrate (L)
  - [x] **Task 12:** Die Anleitung beschreibt das goose-Format (S)
  - [x] **Milestone-Review**
  - [x] **Abschluss-Verifikation**

### Ablauf der Umsetzung

Wie im Vorgängerplan: ein frischer Subagent pro Task, strikt nacheinander; nach jedem Task ein zweistufiges Review (Plan-Treue, Code-Qualität); nach jedem Milestone ein Milestone-Review durch einen neuen Subagent, dessen Befunde ein weiterer bewertet und abarbeitet. Befunde, die dem Plan widersprechen, gehen an den Nutzer. Genau ein Commit pro Task. Der Auftrag an einen Subagent ist der Task-Abschnitt samt Files- und Interfaces-Block, dazu die *Global Constraints*, die Tabelle *Entscheidungen* (E1–E18) und *Risiken* (R1–R14).

**Subagenten können in diesem Repo nicht committen:** die Commits sind GPG-signiert und pinentry findet im Subagent-Kontext kein Terminal. Subagenten stagen, die Hauptsession committet.

---

### Task 1: Der Import-Lock hält eine eigene Verbindung

`db.ImportLock` leiht sich heute eine Verbindung aus dem Pool und hält sie über den ganzen Importlauf. `server.Run` schließt den Pool am Ende mit `defer pool.Close()`, und puddle wartet dabei auf jede ausgeliehene Verbindung — auch auf die eines Imports, den das Zehn-Sekunden-Budget bereits aufgegeben hat. Eine eigene Verbindung löst das, weil der Pool sie nicht kennt.

Die Freigabe wird dabei einfacher: Postgres verwirft jeden Advisory-Lock einer Session, sobald sie endet. Wer seine eigene Verbindung schließt, braucht kein `pg_advisory_unlock` und auch kein `Hijack` für den Fall, dass es scheitert.

> **Abweichung aus dem Milestone-Review (2026-09-23).** Der Code in Step 3 öffnet die Verbindung mit `pgx.Connect(ctx, l.DSN)`. Das scheitert an jedem DSN, der die Pool-Einstellungen trägt, die die README dokumentiert (`pool_max_conns=…`): `pgx.ParseConfig` reicht sie als Laufzeitparameter an Postgres weiter, und Postgres lehnt die Verbindung ab — jeder Import wäre gescheitert. Umgesetzt ist deshalb `pgxpool.ParseConfig` + `applyPoolDefaults` + `pgx.ConnectConfig(ctx, cfg.ConnConfig)`; die Verbindung bleibt eine eigene (E3), bekommt aber den `connect_timeout`-Default des Pools, womit sich auch R6 erledigt. Abgesichert durch `TestImportLock_AcceptsPoolSettingsInTheDSN`. Außerdem nennt die Files-Liste `internal/server/import_test.go` nicht, obwohl es zwei `ImportLock{Pool: …}`-Literale enthielt; es ist mit umgestellt.

**Files:**
- Modify: `internal/db/lock.go` (komplett ersetzt)
- Modify: `internal/db/lock_test.go` (bestehender Test auf den neuen Typ, + neuer Test)
- Modify: `internal/server/server.go` (ein Feld im Pipeline-Literal)
- Modify: `internal/server/import.go` (eine Zeile)
- Modify: `docs/superpowers/plans/2026-09-21-cli-backlog.md` (Backlog-Eintrag als erledigt markieren)

**Interfaces:**
- Consumes: `pipeline.ImportLock` (Port, unverändert), `testdb.NewDatabase(t, name) string`, `db.Connect(ctx, dsn) (*pgxpool.Pool, error)`
- Produces: `type db.ImportLock struct { DSN string }` mit unveränderter Methode `TryAcquire(ctx context.Context) (release func(), ok bool, err error)`. **Das Feld heißt jetzt `DSN` statt `Pool`**; beide Aufrufer werden mit umgestellt.

- [x] **Step 1: Failing Test schreiben** (an `internal/db/lock_test.go` anhängen)

```go
// The lock used to live on a pooled connection, so server.Run's deferred
// pool.Close waited for it — including for an import the ten-second shutdown
// budget had just given up on. A lock that borrows nothing from the pool
// cannot hold Close up, and that is what this asserts: the pool is idle while
// the lock is held.
func TestImportLock_HoldsNoPoolConnection(t *testing.T) {
	ctx := t.Context()
	dsn := testdb.NewDatabase(t, "import_lock_pool")

	pool, err := db.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer pool.Close()

	release, ok, err := (db.ImportLock{DSN: dsn}).TryAcquire(ctx)
	if err != nil {
		t.Fatalf("TryAcquire() error = %v", err)
	}
	if !ok {
		t.Fatal("TryAcquire() ok = false, want the lock to be free")
	}
	defer release()

	if got := pool.Stat().AcquiredConns(); got != 0 {
		t.Errorf("pool has %d connection(s) checked out while the lock is held, want 0", got)
	}
}
```

- [x] **Step 2: Test laufen lassen, er muss fehlschlagen**

Run: `go test -count=1 -run TestImportLock_HoldsNoPoolConnection ./internal/db/`
Expected: FAIL, `unknown field DSN in struct literal of type db.ImportLock`

- [x] **Step 3: `internal/db/lock.go` ersetzen**

```go
package db

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
)

// importLockKey identifies the import lock among all advisory locks in this
// database. The value is arbitrary and only has to stay stable: change it and
// two versions of the binary would no longer see each other's locks.
const importLockKey int64 = 8_233_071_001

// releaseTimeout bounds giving the lock back. Releasing runs on a context that
// is usually already cancelled and therefore carries no deadline of its own,
// and a Postgres that accepts the connection but never answers would otherwise
// hold the caller for as long as it likes.
const releaseTimeout = 5 * time.Second

// ImportLock is pipeline.ImportLock backed by a Postgres advisory lock, which
// is what lets a running server and a one-off import command see each other.
//
// It opens a connection of its own rather than borrowing one from the pool. The
// lock is session-scoped and has to be held for the whole run, and a pooled
// connection held that long is one that server.Run's deferred pool.Close waits
// for — including for an import the shutdown budget has already given up on.
// Its own connection leaves the pool free to close on time.
type ImportLock struct {
	// DSN is the same postgres:// URL the pool was opened with.
	DSN string
}

// TryAcquire implements pipeline.ImportLock.
func (l ImportLock) TryAcquire(ctx context.Context) (func(), bool, error) {
	conn, err := pgx.Connect(ctx, l.DSN)
	if err != nil {
		return nil, false, fmt.Errorf("db: connect for the import lock: %w", err)
	}

	var got bool
	if err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", importLockKey).Scan(&got); err != nil {
		l.close(ctx, conn)
		return nil, false, fmt.Errorf("db: take the import lock: %w", err)
	}
	if !got {
		l.close(ctx, conn)
		return nil, false, nil
	}

	return func() { l.close(ctx, conn) }, true, nil
}

// close ends the session, which is what releases the lock: Postgres drops every
// advisory lock a session holds when it ends. There is nothing to unlock first,
// and nothing is left behind when the process dies instead of closing cleanly.
func (l ImportLock) close(ctx context.Context, conn *pgx.Conn) {
	// WithoutCancel because closing is what has to happen when the run was
	// cancelled, which is exactly when ctx is already done.
	closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), releaseTimeout)
	defer cancel()
	if err := conn.Close(closeCtx); err != nil {
		slog.Warn("db: closing the import lock's connection failed; Postgres frees the lock when the session ends anyway", "error", err)
	}
}
```

- [x] **Step 4: Den bestehenden Lock-Test umstellen** (`internal/db/lock_test.go`)

`TestImportLock_SecondHolderIsRefusedUntilTheFirstReleases` baut heute zwei Pools, weil der Lock einen Pool brauchte. Er braucht jetzt keinen mehr: zwei `ImportLock`-Werte mit demselben DSN sind zwei Sessions. Die beiden `db.Connect`-Aufrufe und ihre `defer …Close()` entfallen, und die drei `TryAcquire`-Aufrufe werden zu

```go
	release, ok, err := (db.ImportLock{DSN: dsn}).TryAcquire(ctx)
```

beziehungsweise für den zweiten Halter

```go
	if _, ok, err := (db.ImportLock{DSN: dsn}).TryAcquire(ctx); err != nil {
```

Der Kommentar über dem Test spricht von „two pools" — er muss jetzt von zwei Verbindungen sprechen, sonst beschreibt er einen Aufbau, den es nicht mehr gibt.

- [x] **Step 5: Die beiden Aufrufer umstellen**

In `internal/server/server.go`, im `pipeline.Pipeline`-Literal:

```go
			Lock: db.ImportLock{DSN: cfg.Database.DSN},
```

In `internal/server/import.go`:

```go
	release, ok, err := (db.ImportLock{DSN: cfg.Database.DSN}).TryAcquire(ctx)
```

- [x] **Step 6: Tests laufen lassen, sie müssen grün sein**

Run: `go build ./... && go test -count=1 -race ./internal/db/ ./internal/server/ ./internal/pipeline/`
Expected: PASS

- [x] **Step 7: Den Doc-Kommentar von `Run` berichtigen** (`internal/server/server.go`)

Der Kommentar nennt seit M3 zwei Ausnahmen von der Zusage, dass beim Rückkehren nichts mehr läuft: den aufgegebenen Import (gilt weiter) und das `pool.Close()`, das auf dessen Lock-Verbindung wartet (gilt nicht mehr). Den zweiten Teil entfernen, den ersten wortgleich stehen lassen.

- [x] **Step 8: Den Backlog-Eintrag im Vorgängerplan als erledigt markieren**

In `docs/superpowers/plans/2026-09-21-cli-backlog.md` den Eintrag „Der Shutdown von `serve` hat keine obere Schranke mehr" mit einem Verweis auf diesen Plan versehen, im Stil des bereits erledigten Backlogs im Plan vom 2026-09-19: ein `> **Erledigt.**`-Absatz über dem Eintrag, der auf `2026-09-22-release-and-cleanup.md` und E3 zeigt und festhält, dass die Ursache (LLM-Timeout) weiterhin offen ist.

- [x] **Step 9: Commit-Gate und Commit**

```bash
git add internal docs/superpowers/plans/2026-09-21-cli-backlog.md
git commit -m "fix(db): hold the import lock on its own connection" -m "A pooled connection held for the whole run is one pool.Close waits for, so an import the shutdown budget had abandoned could keep serve from returning for up to an LLM_TIMEOUT. The lock opens its own connection now, and closing it is the release: Postgres drops a session's advisory locks when the session ends."
```

---

### Task 2: `env_file` für den `app`-Service

Compose liest `.env` im Projektverzeichnis heute nur für die `${VAR}`-Substitution *innerhalb* der Compose-Datei. In den Container gelangt davon nichts, außer was unter `environment:` noch einmal ausdrücklich aufgeführt ist. Dreizehn Variablen aus `.env.example` sind das nicht.

> **Abweichung (2026-09-23, mit dem Nutzer entschieden).** Step 3 erwartet den zusammengesetzten `DB_DSN`, auch wenn `.env` einen eigenen trägt. Das galt nie: `${DB_DSN:-…}` substituiert den Wert aus `.env`, und `.env.example` setzt ihn auf `localhost` — für eine Binary auf dem Host. `cp .env.example .env && docker compose up` gab der App also schon vor M2 eine unerreichbare Datenbank. Umgesetzt ist deshalb `DB_DSN: ${CONTAINER_DB_DSN:-postgres://…@db:5432/…}`; das Überschreiben läuft über die neue Variable `CONTAINER_DB_DSN` (in `.env.example` mit leerem Wert, leer heißt: zusammensetzen). Der Name meidet das Präfix `COMPOSE_`, das Compose für eigene Einstellungen nutzt. Der Kommentar aus Step 2 ist entsprechend berichtigt. Außerdem braucht Step 4 `--entrypoint env`, weil der Entrypoint des Images `recipe-reader` ist; geprüft wurde zusätzlich `docker compose run --rm app migrate` gegen den `db`-Service mit einer aus `.env.example` kopierten `.env` (Exit 0).

**Files:**
- Modify: `docker-compose.yml` (`env_file` beim `app`-Service)
- Modify: `README.md` (der Abschnitt, der `cp .env.example .env` erklärt)
- Modify: `docs/superpowers/plans/2026-09-21-cli-backlog.md` (Backlog-Eintrag als erledigt markieren)

**Interfaces:**
- Consumes: —
- Produces: —

- [x] **Step 1: Den Ist-Zustand festhalten**

```bash
docker compose config | sed -n '/^  app:/,/^  [a-z]/p' | head -40
```

Die Ausgabe in den Bericht aufnehmen: sie zeigt, welche Variablen der Container heute sieht, und ist die Vergleichsgrundlage für Step 4.

- [x] **Step 2: `env_file` ergänzen** (`docker-compose.yml`)

Beim `app`-Service, **vor** dem bestehenden `environment:`-Block:

```yaml
    # Everything the operator put in .env reaches the container, not just the
    # names repeated under environment: below. Thirteen variables from
    # .env.example used to stop at the compose file — the README tells people to
    # start with `cp .env.example .env`, so that was a promise the file broke.
    #
    # required: false because a checkout without .env has to start: the
    # defaults in the binary are a working configuration on their own.
    # environment: below still wins, which is what keeps the composed DB_DSN
    # authoritative over a stale copy in someone's .env.
    env_file:
      - path: .env
        required: false
```

- [x] **Step 3: Die Reihenfolge prüfen**

```bash
docker compose config | grep -A2 'DB_DSN'
```

Erwartet: der aus `POSTGRES_PASSWORD` zusammengesetzte Wert, nicht ein Wert aus `.env`. Compose gibt `environment:` den Vorrang vor `env_file:`; diese Prüfung belegt es für genau diese Datei.

- [x] **Step 4: Den Durchgriff belegen**

`.env` ist die Datei des Betreibers. **Existiert sie bereits, wird sie nicht angefasst**; existiert sie nicht, wird sie für die Prüfung angelegt und danach gelöscht:

```bash
test -f .env && echo "vorhanden, wird nicht angefasst" || cp .env.example .env
docker compose -p recipe-reader-e2e run --rm app env | grep -E '^(EXTRACTION_MODE|LOG_LEVEL)='
```

Erwartet: beide Variablen erscheinen mit ihren Werten aus `.env`. Vor dieser Änderung wäre die Ausgabe leer. Wurde `.env` für die Prüfung angelegt, danach wieder entfernen.

- [x] **Step 5: README**

Im Abschnitt, der `cp .env.example .env` nennt, einen Satz ergänzen: dass jede Variable aus dieser Datei den Container erreicht, dass die zusammengesetzten Werte in `docker-compose.yml` (`DB_DSN`) Vorrang behalten, und dass ein Start ohne `.env` mit den Defaults funktioniert.

- [x] **Step 6: Den Backlog-Eintrag im Vorgängerplan als erledigt markieren**

Wie Task 1, Step 8, für den Eintrag „`.env.example` und `docker-compose.yml` laufen auseinander", mit Verweis auf diesen Plan und E4.

- [x] **Step 7: Commit**

Das Commit-Gate betrifft keinen Go-Code, läuft aber trotzdem (Pflicht laut Global Constraints). Das Docker-Gate entfällt: `Dockerfile` und `scripts/` sind unberührt.

```bash
git add docker-compose.yml README.md docs/superpowers/plans/2026-09-21-cli-backlog.md
git commit -m "fix(compose): pass .env into the app container" -m "Compose read .env only for substitution inside the compose file, so thirteen variables from .env.example never reached the app — including LOG_LEVEL and LOG_FORMAT, added one milestone earlier. env_file closes that for every future variable too; the composed values under environment: still win."
```

---

### Task 3: Der Abbruch einer Migration sagt, dass Wiederholen gefahrlos ist

`MigrateWithContext` meldet einen Abbruch als `db: migrate up: context canceled`. Das sagt nicht, was angewandt wurde, und legt nahe, es sei etwas kaputt. Tatsächlich ist der Zustand immer konsistent: golang-migrate bricht zwischen zwei Migrationen ab, die laufende wird immer zu Ende gebracht, und ein erneuter Lauf setzt fort.

> **Abweichung (2026-09-23).** Step 1 hängt die Prüfung „vor der schließenden Klammer“ an — dort ist `err` aber schon durch `pool, err := pgxpool.New(…)` überschrieben und `nil`, `err.Error()` würde paniken. Die Prüfung steht deshalb direkt nach der `errors.Is`-Prüfung. Der Wortlaut aus Step 3 steht einmal, in `migrateCancelled`, das beide `ctx.Err()`-Zweige aufrufen.

**Files:**
- Modify: `internal/db/connect.go` (die beiden `ctx.Err()`-Zweige in `migrateUp`)
- Modify: `internal/db/db_test.go` (Zusicherung auf die Meldung)

**Interfaces:**
- Consumes: —
- Produces: unveränderte Signaturen; nur der Text der Fehlermeldung ändert sich.

- [x] **Step 1: Failing Test schreiben** (an den bestehenden `TestMigrateWithContext_CancelledContextAppliesNothing` in `internal/db/db_test.go` anhängen, vor dessen schließender Klammer)

```go
	// The operator reads this line and has to decide what to do next. "context
	// canceled" alone reads like damage; the truth is that nothing is half
	// applied and a second run finishes the job.
	if !strings.Contains(err.Error(), "re-running") {
		t.Errorf("MigrateWithContext() error = %q, want it to say that re-running is safe", err)
	}
```

Die Imports von `db_test.go` um `strings` ergänzen, falls es fehlt.

- [x] **Step 2: Test laufen lassen, er muss fehlschlagen**

Run: `go test -count=1 -run TestMigrateWithContext ./internal/db/`
Expected: FAIL, `want it to say that re-running is safe`

- [x] **Step 3: Die Meldung ändern** (`internal/db/connect.go`)

Beide `ctx.Err()`-Zweige in `migrateUp` — der vor `open()` und der nach `Up()` — geben heute `fmt.Errorf("db: migrate up: %w", err)` zurück. Beide ersetzen durch:

```go
		return fmt.Errorf("db: migrate up: cancelled, nothing is half applied and re-running continues where it stopped: %w", err)
```

Der Satz gilt für beide Stellen: vor `open()` wurde nichts angewandt, nach `Up()` nur vollständige Migrationen.

**Dieser Wortlaut hält bis M6 und wird dort ein letztes Mal nachgeschärft** (R13): unter goose wird die laufende Migration nicht zu Ende gebracht, sondern zurückgerollt. Der Schluss bleibt derselbe — nichts ist halb angewandt, ein zweiter Lauf setzt fort —, nur die Begründung wechselt. Task 11 aus M6 ändert deshalb den Satz und nicht das Wort `re-running`, auf das der Test hier prüft. Wer Task 3 reviewt, soll das nicht als Fehler melden.

- [x] **Step 4: Tests laufen lassen, sie müssen grün sein**

Run: `go test -count=1 -race ./internal/db/`
Expected: PASS

- [x] **Step 5: Commit**

```bash
git add internal/db
git commit -m "fix(db): say what a cancelled migration left behind" -m "\"context canceled\" alone reads like damage. golang-migrate stops between two migrations and always finishes the one in flight, so nothing is half applied and a second run continues — which is what the operator needs to know at the moment they read the line."
```

---

### Task 4: `main` unterscheidet Bedienfehler von Laufzeitfehler

`main` loggt jeden Fehler als `slog.Error("fatal", "error", err)`. Ein Tippfehler in der Kommandozeile sieht damit aus wie ein Absturz im Betrieb, obwohl das eine durch Lesen von `--help` behoben wird und das andere nicht. kong liefert Parse-Fehler als `*kong.ParseError`, die Unterscheidung ist also vorhanden und wird nur nicht genutzt.

> **Befund (2026-09-23).** Step 5 wie erwartet: `invalid command line` in beiden Formaten, im Default-Format. Zusätzlich geprüft: auch Konfigurationsfehler aus der Umgebung (`API_TOKEN` fehlt bei nicht-loopback `HTTP_ADDR`, `EXTRACTION_MODE=bogus`) kommen von kong als `*kong.ParseError` und werden als `invalid command line` gemeldet. Die README sagt das ausdrücklich.

**Files:**
- Modify: `cmd/recipe-reader/main.go` (`main`)
- Modify: `cmd/recipe-reader/main_test.go` (Tabelle von `TestMain_ExitStatus`)

**Interfaces:**
- Consumes: `run(args []string, opts ...kong.Option) error`
- Produces: unverändertes Exit-Verhalten (0/1); nur die Logzeile unterscheidet sich.

- [x] **Step 1: Die Tabelle um die erwartete Logzeile erweitern** (`cmd/recipe-reader/main_test.go`)

`TestMain_ExitStatus` fängt die Ausgabe des Kindprozesses bereits in `out` ab und prüft sie nur nicht. Das Tabellenfeld ergänzen:

```go
	for _, tc := range []struct {
		args    string
		want    int
		wantLog string
	}{
		{"--does-not-exist", 1, "invalid command line"},
		{"healthcheck --http-addr 127.0.0.1:1", 1, "fatal"},
		{"healthcheck --http-addr " + srv.Listener.Addr().String(), 0, ""},
	} {
```

und nach der bestehenden Prüfung des Exit-Codes anfügen:

```go
		// A command line the binary could not read and a run that went wrong
		// are different problems for whoever reads the log: the first is fixed
		// by reading --help, the second is not.
		if tc.wantLog != "" && !strings.Contains(string(out), tc.wantLog) {
			t.Errorf("recipe-reader %s logged:\n%s\nwant it to contain %q", tc.args, out, tc.wantLog)
		}
```

- [x] **Step 2: Test laufen lassen, er muss fehlschlagen**

Run: `go test -count=1 -run TestMain_ExitStatus ./cmd/recipe-reader/`
Expected: FAIL, `want it to contain "invalid command line"` — heute loggt auch der Parse-Fehler `fatal`.

- [x] **Step 3: `main` umbauen** (`cmd/recipe-reader/main.go`)

```go
// main runs the command line. Every failure exits 1 — kong's own FatalIfErrorf
// would exit 80 for a usage error, and a container HEALTHCHECK understands only
// 0 and 1 — but the two kinds of failure are named apart. A command line this
// binary could not read is fixed by reading --help; a run that went wrong is
// not, and an operator scanning the log should not have to open the error
// attribute to tell which one they are looking at.
func main() {
	if err := run(os.Args[1:]); err != nil {
		if _, ok := errors.AsType[*kong.ParseError](err); ok {
			slog.Error("invalid command line", "error", err)
		} else {
			slog.Error("fatal", "error", err)
		}
		os.Exit(1)
	}
}
```

Die Imports um `errors` ergänzen; `kong` und `slog` sind bereits da.

- [x] **Step 4: Tests laufen lassen, sie müssen grün sein**

Run: `go test -count=1 -race ./cmd/recipe-reader/`
Expected: PASS

- [x] **Step 5: Von Hand nachsehen**

```bash
go run ./cmd/recipe-reader --does-not-exist 2>&1 | tail -2
LOG_FORMAT=json go run ./cmd/recipe-reader --does-not-exist 2>&1 | tail -2
```

Erwartet: `invalid command line` in beiden Formaten. Der zweite Aufruf belegt zugleich, dass die Unterscheidung auch für einen Log-Collector sichtbar ist — allerdings noch im Default-Format, weil ein Parse-Fehler vor `logging.Configure` auftritt (Entscheidung D4 des Vorgängerplans). Das Ergebnis in den Bericht aufnehmen; weicht es davon ab, anhalten und melden.

- [x] **Step 6: Den Backlog-Eintrag im Vorgängerplan kürzen**

In `docs/superpowers/plans/2026-09-21-cli-backlog.md` im Eintrag „Fehlermeldungen eindeutiger machen" die beiden erledigten Unterpunkte (die Migrationsmeldung und `main`s Sammelzeile) streichen und über dem Eintrag vermerken, dass sie in diesem Plan behoben sind. Der dritte Unterpunkt — kongs Kommandopfad-Präfix — bleibt stehen, weil er ein Nutzerurteil braucht.

- [x] **Step 7: Commit**

```bash
git add cmd docs/superpowers/plans/2026-09-21-cli-backlog.md
git commit -m "fix(cli): name a bad command line apart from a failed run" -m "Both still exit 1, but one is fixed by reading --help and the other is not. kong hands back a *kong.ParseError, so the distinction was there and merely unused."
```

---

### Task 5: `release-please` einrichten

`release-please` liest die Conventional-Commits-Präfixe seit einem Startpunkt, hält einen Release-PR offen, der Version und Changelog vorschlägt, und legt beim Merge Tag und GitHub-Release an.

> **Abweichung (2026-09-23, mit dem Nutzer entschieden).** Die Repository-Einstellung „Allow GitHub Actions to create and approve pull requests“ war aus; ohne sie kann `release-please` mit `GITHUB_TOKEN` keinen PR öffnen. Sie ist per `gh api` eingeschaltet (`can_approve_pull_request_reviews=true`, `default_workflow_permissions` bleibt `read`). Der Workflow ruft außerdem `release-artifacts.yml` als Job `artifacts` auf, siehe Task 6. Syntaxprüfung zusätzlich mit `actionlint`. Nebenbefund für M5: das Repository ist **öffentlich**, nicht privat, wie E9 annimmt.

> **Korrektur (2026-09-23, mit dem Nutzer entschieden).** release-please behandelt jeden Commit-Typ als release-würdig, dessen Abschnitt im Changelog sichtbar ist — ein reiner `ci:`-Commit (#45) erzeugte `v0.1.2`. `refactor`, `perf`, `build` und `ci` sind deshalb jetzt `hidden`: nur `feat`, `fix` und Breaking Changes lösen ein Release aus und erscheinen im Changelog.

**Files:**
- Create: `.github/workflows/release-please.yml`
- Create: `release-please-config.json`
- Create: `.release-please-manifest.json`
- Modify: `README.md` (kurzer Abschnitt „Releases")

**Interfaces:**
- Consumes: —
- Produces: Tags der Form `v<major>.<minor>.<patch>` und GitHub-Releases; Task 6 und Task 7 hängen sich an das `release`-Ereignis.

- [x] **Step 1: Den Startpunkt bestimmen**

```bash
git log --oneline -1 --format='%H %s' $(git rev-list -1 main --grep='(3/3)')
```

Der Merge-Commit von PR #33 ist der `bootstrap-sha`: alles davor gehört zur Vorgeschichte und soll nicht im ersten Changelog landen (E8, R1). Den vollen SHA notieren.

- [x] **Step 2: `release-please-config.json` anlegen**

`<BOOTSTRAP_SHA>` durch den SHA aus Step 1 ersetzen:

```json
{
  "$schema": "https://raw.githubusercontent.com/googleapis/release-please/main/schemas/config.json",
  "bootstrap-sha": "<BOOTSTRAP_SHA>",
  "packages": {
    ".": {
      "release-type": "go",
      "changelog-path": "CHANGELOG.md",
      "bump-minor-pre-major": true,
      "bump-patch-for-minor-pre-major": false,
      "include-component-in-tag": false,
      "changelog-sections": [
        { "type": "feat", "section": "Features" },
        { "type": "fix", "section": "Bug Fixes" },
        { "type": "refactor", "section": "Refactoring" },
        { "type": "perf", "section": "Performance" },
        { "type": "build", "section": "Build" },
        { "type": "ci", "section": "CI" },
        { "type": "docs", "section": "Documentation", "hidden": true },
        { "type": "test", "section": "Tests", "hidden": true },
        { "type": "chore", "section": "Chores", "hidden": true }
      ]
    }
  }
}
```

`bump-minor-pre-major` sorgt dafür, dass ein `feat:` unterhalb von 1.0.0 den Minor hebt — so entsteht aus dem Startwert `0.0.0` die erste Version `v0.1.0` (E2).

> **Korrektur (2026-09-23).** Das stimmte nicht: ohne ein vorhandenes Release-Tag ignoriert `release-please` den Wert im Manifest und nimmt `initial-version`, Default `1.0.0` — der erste Release-PR (#40) hieß `release 1.0.0`. `"initial-version": "0.1.0"` in `release-please-config.json` setzt den Startpunkt; `bump-minor-pre-major` wirkt erst ab dem zweiten Release.

- [x] **Step 3: `.release-please-manifest.json` anlegen**

```json
{
  ".": "0.0.0"
}
```

- [x] **Step 4: `.github/workflows/release-please.yml` anlegen**

```yaml
name: Release Please

# Runs on every push to main and keeps a release PR open that shows the next
# version and the changelog it would write. Merging that PR is what creates the
# tag and the GitHub release; release-artifacts.yml then hangs the binaries and
# the image on it.
on:
  push:
    branches: [main]

permissions:
  contents: write
  pull-requests: write

jobs:
  release-please:
    runs-on: ubuntu-latest
    steps:
      - uses: googleapis/release-please-action@v5
        with:
          config-file: release-please-config.json
          manifest-file: .release-please-manifest.json
```

- [x] **Step 5: Die Konfiguration prüfen, ohne sie laufen zu lassen**

```bash
python3 -c "import json;[json.load(open(f)) for f in ['release-please-config.json','.release-please-manifest.json']];print('json ok')"
python3 -c "import yaml;d=yaml.safe_load(open('.github/workflows/release-please.yml'));print(list(d['jobs']))"
```

Erwartet: `json ok` und `['release-please']`. Die CI prüft diesen Task nicht (CI-Ausnahme aus #29, R5), also ist das hier die einzige Syntaxprüfung vor dem Merge.

- [x] **Step 6: README**

Einen Abschnitt „Releases" ergänzen: dass die Version aus den Commit-Präfixen entsteht, dass ein offener Release-PR die nächste Version zeigt, dass sein Merge Tag und Release erzeugt, und dass die Zählung bei `v0.1.0` beginnt, weil `0.x` unter SemVer Breaking Changes in jedem Minor erlaubt.

- [x] **Step 7: Commit**

```bash
git add .github/workflows/release-please.yml release-please-config.json .release-please-manifest.json README.md
git commit -m "ci: let release-please propose versions and changelogs" -m "The repository writes conventional commit prefixes anyway, so the changelog falls out of them without extra work. bootstrap-sha starts the history at the merge of PR #33; without it the first changelog would be the whole project."
```

---

### Task 6: Binaries und Prüfsummen ans Release hängen

Das Release aus Task 5 trägt bisher nur den Changelog. Dieser Task hängt die Single-Binary für drei Plattformen und eine `checksums.txt` an.

> **Abweichungen (2026-09-23).** (1) *Mit dem Nutzer entschieden:* Ein Release, das `release-please` mit `GITHUB_TOKEN` anlegt, löst in keinem anderen Workflow ein `release`-Ereignis aus — `on: release: published` wäre nie gelaufen. `release-artifacts.yml` ist deshalb ein wiederverwendbarer Workflow (`workflow_call` + `workflow_dispatch`, Input `tag`), den `release-please.yml` aufruft, wenn `release_created` gesetzt ist. E7 gilt weiter: zwei Dateien, Artefakte genau einmal. (2) `npm run build` schreibt nach `web/dist`, `go:embed` liest `internal/webui/dist` — die Binaries hätten die Platzhalterseite ausgeliefert. Der Workflow ruft `make frontend` und prüft `internal/webui/dist/index.html`. (3) Jeder Matrix-Job schrieb eine eigene `checksums.txt`; `merge-multiple` hätte nur die letzte behalten. Die Prüfsummen entstehen jetzt im `upload`-Job über alle Binaries. (4) `checkout` bekommt `ref: <tag>`, sonst baut ein manueller Lauf `main` unter dem Namen des Tags. (5) Der Tag geht über `env:` in die Skripte statt per `${{ }}` direkt hinein. (6) `actions/upload-artifact@v7` statt `@v5` (aktuelles Major). Lokal geprüft: `make frontend`, drei Cross-Builds, `checksums.txt` mit drei Zeilen, `--version` meldet den Tag, kein Build-Pfad in der Binary.

> **Korrektur nach `v0.1.0` (2026-09-23).** Im Repository sind *Immutable Releases* aktiv. `release-please` veröffentlichte `v0.1.0` sofort, und der `upload`-Job scheiterte mit `HTTP 422: Cannot upload assets to an immutable release` — `v0.1.0` trägt deshalb keine Binaries und kann sie nie mehr bekommen; Image und Tag sind vollständig. Seitdem legt `release-please` das Release als Entwurf an (`draft: true`, `force-tag-creation: true`, damit das Tag sofort entsteht), und der Job `publish` (vorher `upload`, jetzt nach `binaries` **und** `image`) hängt die Assets an und veröffentlicht erst danach.

**Files:**
- Create: `.github/workflows/release-artifacts.yml`
- Modify: `Makefile` (`-trimpath`)
- Modify: `Dockerfile` (`-trimpath`)

**Interfaces:**
- Consumes: das `release`-Ereignis aus Task 5; `var version` in `package main`, gespeist über `-ldflags "-X main.version=…"`
- Produces: Release-Assets `recipe-reader_<tag>_<os>_<arch>` und `checksums.txt`

- [x] **Step 1: `-trimpath` ergänzen**

Beide Build-Aufrufe bauen heute ohne `-trimpath`, tragen also den absoluten Pfad des Build-Verzeichnisses in die Binary. Solange nur lokal gebaut wurde, war das folgenlos; eine veröffentlichte Binary soll ihn nicht enthalten.

In `Makefile`:

```make
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/recipe-reader ./cmd/recipe-reader
```

In `Dockerfile`:

```dockerfile
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-X main.version=${VERSION}" -o /recipe-reader ./cmd/recipe-reader
```

- [x] **Step 2: Docker-Gate**

```bash
docker build --build-arg VERSION=smoke -t recipe-reader:ci .
scripts/smoke-test-image.sh recipe-reader:ci smoke
```

Erwartet: `smoke test passed: recipe-reader:ci (smoke)`. `-trimpath` darf die Versionsstempelung nicht stören — genau das prüft der Smoke-Test.

- [x] **Step 3: `.github/workflows/release-artifacts.yml` anlegen**

```yaml
name: Release artifacts

# Hangs the binaries on a release release-please just created. Keyed to the
# release event rather than to the tag push so it runs exactly once, after the
# release exists to upload to.
on:
  release:
    types: [published]
  workflow_dispatch:
    inputs:
      tag:
        description: "Existing release tag to build and upload to"
        required: true

permissions:
  contents: write

jobs:
  binaries:
    runs-on: ubuntu-latest
    timeout-minutes: 20
    strategy:
      matrix:
        include:
          - goos: linux
            goarch: amd64
          - goos: linux
            goarch: arm64
          - goos: darwin
            goarch: arm64
    steps:
      - uses: actions/checkout@v7
      - uses: actions/setup-go@v7
        with:
          go-version: "1.27"

      # The frontend is embedded with go:embed, so it has to exist before the
      # binary is built — otherwise the single binary serves the placeholder
      # page instead of the real UI.
      - uses: actions/setup-node@v7
        with:
          node-version: "26"
          cache: "npm"
          cache-dependency-path: web/package-lock.json
      - run: npm ci
        working-directory: web
      - run: npm run build
        working-directory: web

      - name: Build
        env:
          GOOS: ${{ matrix.goos }}
          GOARCH: ${{ matrix.goarch }}
        run: |
          tag="${{ github.event.release.tag_name || inputs.tag }}"
          name="recipe-reader_${tag}_${GOOS}_${GOARCH}"
          CGO_ENABLED=0 go build -trimpath \
            -ldflags "-X main.version=${tag}" \
            -o "dist/${name}" ./cmd/recipe-reader
          (cd dist && sha256sum "${name}" >> checksums.txt)

      - uses: actions/upload-artifact@v5
        with:
          name: dist-${{ matrix.goos }}-${{ matrix.goarch }}
          path: dist/

  upload:
    needs: binaries
    runs-on: ubuntu-latest
    timeout-minutes: 10
    steps:
      - uses: actions/download-artifact@v8
        with:
          path: dist
          merge-multiple: true

      # One checksums.txt for the release, assembled from the per-job lines.
      - name: Upload to the release
        env:
          GH_TOKEN: ${{ github.token }}
        run: |
          tag="${{ github.event.release.tag_name || inputs.tag }}"
          cd dist
          gh release upload "$tag" recipe-reader_* checksums.txt --clobber \
            --repo "${{ github.repository }}"
```

- [x] **Step 4: Die Workflow-Syntax prüfen**

```bash
python3 -c "import yaml;d=yaml.safe_load(open('.github/workflows/release-artifacts.yml'));print(list(d['jobs']))"
```

Erwartet: `['binaries', 'upload']`.

- [x] **Step 5: Commit-Gate und Commit**

```bash
git add .github/workflows/release-artifacts.yml Makefile Dockerfile
git commit -m "ci: attach binaries and checksums to a release" -m "Three platforms, each a single static binary with the frontend embedded, stamped with the release tag so --version reports what was downloaded. -trimpath is added to the Makefile and the Dockerfile too, so a published binary carries no build path."
```

---

### Task 7: Image in die GHCR, Compose darauf umstellen

Ein Betreiber soll das Image ziehen können, statt es zu bauen.

> **Abweichungen (2026-09-23).** (1) GHCR nimmt nur Kleinbuchstaben, `github.repository` ist `sBurmester/…` — der Name wird mit `${GITHUB_REPOSITORY,,}` klein geschrieben. (2) Statt `docker/build-push-action` baut der Job mit `docker build`, prüft das Image mit `scripts/smoke-test-image.sh` (US4 verlangt den Smoke-Test) und pusht erst danach; eine Action weniger. (3) `latest` wird nur bei einem neuen Release bewegt, nicht bei einem manuellen Lauf für ein älteres Tag. (4) Step 3 braucht `run --rm --no-deps app --version` — der Entrypoint ist schon `recipe-reader`. Ergebnis: `local`. (5) README zusätzlich: Installation aus einem Release mit Prüfsummen-Check und der Gatekeeper-Hinweis für die unsignierte macOS-Binary.

**Files:**
- Modify: `.github/workflows/release-artifacts.yml` (+ Job `image`)
- Modify: `docker-compose.yml` (`image:` beim `app`-Service)
- Modify: `README.md` (Betrieb über das veröffentlichte Image)

**Interfaces:**
- Consumes: das `release`-Ereignis; `Dockerfile` mit `VERSION`-Build-Arg
- Produces: `ghcr.io/<owner>/<repo>:<tag>` und `:latest`

- [x] **Step 1: Den Image-Job ergänzen** (`.github/workflows/release-artifacts.yml`)

Als zusätzlichen Job, neben `binaries`:

```yaml
  image:
    runs-on: ubuntu-latest
    timeout-minutes: 20
    permissions:
      contents: read
      packages: write
    steps:
      - uses: actions/checkout@v7
      - uses: docker/login-action@v4
        with:
          registry: ghcr.io
          username: ${{ github.actor }}
          password: ${{ github.token }}

      # The image builds the frontend and the binary itself, so this needs no
      # Go or Node setup — the Dockerfile is the single description of how the
      # runtime image comes about, and the smoke test checks that it still is.
      - name: Build and push
        uses: docker/build-push-action@v7
        with:
          context: .
          push: true
          build-args: |
            VERSION=${{ github.event.release.tag_name || inputs.tag }}
          tags: |
            ghcr.io/${{ github.repository }}:${{ github.event.release.tag_name || inputs.tag }}
            ghcr.io/${{ github.repository }}:latest
```

- [x] **Step 2: Compose auf das Image umstellen** (`docker-compose.yml`)

Beim `app`-Service, über dem bestehenden `build:`:

```yaml
    # Operators pull a published image; developers build locally with
    # `docker compose up --build`. With both keys compose uses the image it
    # finds and builds only when asked, so neither way shuts the other out.
    # RECIPE_READER_VERSION pins a release; the default follows the newest.
    image: ghcr.io/sburmester/recipe-reader:${RECIPE_READER_VERSION:-latest}
```

- [x] **Step 3: Prüfen, dass der lokale Build weiterhin greift**

```bash
VERSION=local docker compose -p recipe-reader-e2e build app
docker compose -p recipe-reader-e2e run --rm app recipe-reader --version
```

Erwartet: `local`. Das belegt, dass `build:` neben `image:` bestehen bleibt und der lokale Weg unverändert funktioniert.

- [x] **Step 4: README**

Im Betriebsabschnitt ergänzen: `docker compose pull && docker compose up -d` für ein veröffentlichtes Image, `RECIPE_READER_VERSION=v0.1.0` zum Festnageln einer Version, `docker compose up --build` für die Entwicklung. Dazu der Hinweis aus R3, dass das GHCR-Paket nach dem ersten Push privat ist und in den Repository-Einstellungen öffentlich gestellt werden kann.

- [x] **Step 5: Docker-Gate und Commit**

```bash
docker build --build-arg VERSION=smoke -t recipe-reader:ci .
scripts/smoke-test-image.sh recipe-reader:ci smoke
git add .github/workflows/release-artifacts.yml docker-compose.yml README.md
git commit -m "ci: publish the image to ghcr and let compose pull it" -m "compose keeps build: next to image:, so an operator pulls a release and a developer still builds locally. The package is private until someone makes it public, which is the right default for a single-user deployment."
```

---

### Task 8: `renovate.json`

Die Konfiguration ist von der Frage unabhängig, wie Renovate läuft — sie gilt für die App (E9) genauso wie für eine selbst gehostete Variante. Deshalb ein eigener Task: er lässt sich lesen und beurteilen, bevor die App installiert ist. Liegt `renovate.json` auf `main`, bevor die App installiert wird, überspringt Renovate seinen Onboarding-PR.

> **Abweichung (2026-09-23).** Zusätzlich zur Datei unten eine Regel, die Postgres-Majors ausschließt (`matchPackageNames: ["postgres"]`, `matchUpdateTypes: ["major"]`, `enabled: false`): ein Wechsel von 18 auf 19 ändert das Datenformat auf der Platte und braucht die Upgrade-Anleitung der README, keinen Renovate-PR. `renovate-config-validator`: „Config validated successfully“.

> **Ergänzung (Nutzer, 2026-09-23).** Renovate prüft zusätzlich täglich auf Sicherheitslücken: `osvVulnerabilityAlerts: true` (OSV-Datenbank, keine Repository-Einstellung nötig — die Dependabot-Alerts des Repos sind aus) und `vulnerabilityAlerts` mit `schedule: ["at any time"]` und dem Label `security`. Sicherheits-PRs warten damit nicht auf Montag, sondern entstehen beim nächsten Lauf der App, der mehrmals täglich stattfindet. `prPriority` lehnt der Validator in `vulnerabilityAlerts` ab und ist deshalb weggelassen.

> **Ergänzung (Nutzer, 2026-09-23).** Jede Renovate-Regel setzt einen `semanticCommitScope` und ein zusätzliches Label, damit Titel und Labels zeigen, was aktualisiert wird: `go`/`go`, `web`/`npm`, `docker`/`docker`, `actions`/`github-actions` — aus `chore(deps): pin dependencies` wird `chore(docker): pin dependencies`. Außerdem bekannt seit dem ersten Lauf: die Mend-App startete im *Silent Mode* (`dryRun=lookup`, keine PRs und Issues, nur „Pending Approval“ im Portal), weil sie für alle Repositories installiert war; der Nutzer hat auf *Interactive* umgestellt.

> **Ergänzung (Nutzer, 2026-09-23).** Ein PR je Ökosystem: `groupName` `go modules`, `npm packages`, `docker images` (auch für die Pins — vorher bündelte Renovates Pin-Gruppe golang, node, alpine und postgres), `github actions`. Majors bleiben je Ökosystem ein eigener PR (`separateMajorMinor` aus `config:recommended`), Sicherheits-Fixes einzeln.

> **Ergänzung (Nutzer, 2026-09-23).** Jedes Ökosystem soll seinen PR öffnen können, Sicherheits-Updates sofort. Die Limits aus `config:recommended` (`prHourlyLimit: 2`) und aus Step 1 (`prConcurrentLimit: 5`) hielten Gruppen als „Rate-Limited“ zurück; beide stehen jetzt auf `0` (unbegrenzt). Die Gruppierung begrenzt die Zahl ohnehin auf einen PR je Ökosystem plus Majors.

**Files:**
- Create: `renovate.json`

**Interfaces:**
- Consumes: —
- Produces: `renovate.json` im Wurzelverzeichnis, die Datei, die die App aus Task 9 liest.

- [x] **Step 1: `renovate.json` anlegen**

```json
{
  "$schema": "https://docs.renovatebot.com/renovate-schema.json",
  "extends": [
    "config:recommended",
    "helpers:pinGitHubActionDigests"
  ],
  "timezone": "Europe/Berlin",
  "schedule": ["before 6am on monday"],
  "prConcurrentLimit": 5,
  "labels": ["dependencies"],
  "packageRules": [
    {
      "description": "go mod tidy after a module update, so go.sum matches what the PR changed instead of failing CI on the first run.",
      "matchManagers": ["gomod"],
      "postUpdateOptions": ["gomodTidy"]
    },
    {
      "description": "Base images by digest, like the actions. The tag stays in the comment Renovate maintains, so the file still says which version it means.",
      "matchManagers": ["dockerfile", "docker-compose"],
      "pinDigests": true
    },
    {
      "description": "Action updates arrive as one pull request. CI skips a change that touches only .github/workflows (see #29), so these are read and run by hand — one pull request a week is reviewable, a dozen is not.",
      "matchManagers": ["github-actions"],
      "groupName": "github actions"
    },
    {
      "description": "This project's own image is published by its release workflow, not updated by Renovate.",
      "matchPackageNames": ["ghcr.io/sburmester/recipe-reader"],
      "enabled": false
    }
  ]
}
```

- [x] **Step 2: Die Datei prüfen**

```bash
python3 -c "import json;json.load(open('renovate.json'));print('json ok')"
npx --yes --package renovate -- renovate-config-validator renovate.json
```

Erwartet: `json ok`, und der Validator meldet die Konfiguration als gültig. Der zweite Befehl lädt Renovate einmalig über `npx` — er gehört nicht ins Repository und wird nur hier ausgeführt. Meldet er einen unbekannten Schlüssel, ist das ein Befund: anhalten und melden, statt den Schlüssel zu raten.

- [x] **Step 3: Commit**

```bash
git add renovate.json
git commit -m "ci: configure renovate" -m "Go modules, GitHub Actions and Docker images, weekly. Actions and base images are pinned to digests; action updates arrive grouped, because CI skips a pull request that touches only workflows and one grouped pull request a week can be run by hand."
```

---

### Task 9: Renovate als GitHub-App

> **Neu gefasst am 2026-09-23** (E9 geändert): statt eines Workflows mit Personal Access Token läuft Renovate als Mend-GitHub-App. Der frühere Task — `.github/workflows/renovate.yml`, `renovatebot/github-action`, das Secret `RENOVATE_TOKEN` — entfällt ersatzlos.

**Files:**
- Modify: `README.md` (Abschnitt „Dependencies")

**Interfaces:**
- Consumes: `renovate.json` (Task 8)
- Produces: einen wöchentlichen Lauf der App und das Issue „Dependency Dashboard“, über das sich ein Update von Hand anstoßen lässt

- [x] **Step 1: README**

Einen Abschnitt „Dependencies" ergänzen: dass Renovate als GitHub-App läuft und `renovate.json` liest, wöchentlich (montags vor 6 Uhr, Europe/Berlin) PRs öffnet und über das „Dependency Dashboard“-Issue sofort angestoßen werden kann; dass Actions und Basisimages auf Digest gepinnt sind und der Tag daneben steht; dass Postgres-Majors ausgenommen sind; und — wegen R8 — dass ein PR, der nur Workflows anfasst, keine CI bekommt und deshalb vor dem Merge von Hand über `gh workflow run ci.yml --ref <branch>` gegen die CI geschickt wird.

- [x] **Step 2: Commit**

```bash
git add README.md
git commit -m "docs: describe how dependencies are kept up to date"
```

- [x] **Step 3: Installation durch den Nutzer** (nicht vom Agent ausführbar)

1. https://github.com/apps/renovate → *Install* → *Only select repositories* → `recipe-reader`.
2. Beim Mend Developer Portal (developer.mend.io) mit dem GitHub-Account anmelden; kein eigenes Konto.
3. Öffnet Renovate trotzdem einen Onboarding-PR „Configure Renovate“, weil die App vor dem Merge von Task 8 installiert wurde: schließen, nicht mergen — die Konfiguration ist die aus Task 8.

Geprüft wird die Installation in Task 10, Step 1: das Issue „Dependency Dashboard“ existiert.

---

### Task 10: Renovate pinnt Actions und Images

Ein Tag ist verschiebbar: `actions/checkout@v7` kann morgen auf anderen Code zeigen als heute. Ein Digest kann das nicht. Das Pinnen macht **Renovate selbst** (E10) — `helpers:pinGitHubActionDigests` und die `pinDigests`-Regel aus Task 8 sind genau dafür da. Dieser Task stößt den ersten Lauf an, liest die PRs, merged sie und prüft das Ergebnis; er löst keine Digests von Hand auf.

Zwei Dinge macht Renovate **nicht**, und die bleiben Handarbeit: die Kommentare berichtigen, die das alte Verhalten beschreiben, und dafür sorgen, dass das eigene Release-Image ungepinnt bleibt.

Dieser Task läuft **nach** M4, damit auch `release-please.yml` und `release-artifacts.yml` erfasst werden (E11), und er setzt voraus, dass die Installation aus **Task 9, Step 3** erledigt ist: ohne die App öffnet Renovate keine PRs, und dieser Task hat nichts zu tun. Ist sie nicht installiert, hier anhalten und den Nutzer erinnern, statt ersatzweise von Hand zu pinnen.

> **Durchführung (2026-09-23).** Die App lief zuerst im *Silent Mode* (siehe Task 8); danach Dashboard #47. Die Pin-PRs wurden über das Dashboard angestoßen: #49 (Actions, SHA + `# vN`, drei SHAs gegen ihre Tags geprüft, CI von Hand über `gh workflow run ci.yml --ref renovate/github-actions` grün) und #48 (Images, Docker-Gate lokal grün). Nach der Umstellung auf Gruppen je Ökosystem schloss Renovate #48 und legte denselben Inhalt als #51 (`chore(docker): pin dependencies`) neu an; dessen `docker`-Job scheiterte einmal an einem Timing-Problem im Smoke-Test (`pg_isready` über den Unix-Socket meldete Postgres während der Init-Phase bereit) — behoben in #52 für Skript **und** Compose-Healthcheck — und war im zweiten Lauf grün. Step 4: Das `Dockerfile` hatte, anders als angenommen, keine solchen Kommentare; berichtigt sind der `db`-Kommentar in `docker-compose.yml` und der README-Abschnitt „Postgres version“, der außerdem nicht mehr behauptet, die Tests liefen gegen das deployte Image: `testdb.go` und `smoke-test-image.sh` nutzen `postgres:18-alpine` ungepinnt, was Renovate nicht erkennt. Step 6: `grep` ohne Treffer, jedes `FROM` und `postgres` mit Digest, das eigene Image ohne.

**Files:**
- Modify (durch Renovates PRs): `.github/workflows/*.yml`, `Dockerfile`, `docker-compose.yml`
- Modify (von Hand, Step 4): `Dockerfile`, `docker-compose.yml` — nur die Kommentare

**Interfaces:**
- Consumes: `renovate.json` (Task 8), die installierte App (Task 9)
- Produces: —

- [x] **Step 1: Renovate anstoßen**

```bash
gh issue list --search 'Dependency Dashboard in:title' --json number,title
```

Erwartet: das Issue „Dependency Dashboard“ — der Beleg, dass die App installiert ist und `renovate.json` gelesen hat. Die Pin-PRs fallen unter den Zeitplan (montags vor 6 Uhr); im Dashboard stehen sie dann unter „Awaiting Schedule“. Die Checkboxen dort erzeugen den PR sofort — der Nutzer oder der Agent hakt sie im Issue an (`gh issue edit` am Body oder im Browser). Fehlt das Issue nach einigen Minuten, die Logs im Mend-Portal lesen lassen und melden.

- [x] **Step 2: Die PRs lesen, bevor sie gemergt werden**

```bash
gh pr list --label dependencies --json number,title,files --jq '.[] | "\(.number)\t\(.title)"'
```

Erwartet: mindestens ein PR, der die Actions auf Digests umstellt (durch `groupName` zusammengefasst), und je einer für die Basisimages. Jeden PR ansehen und prüfen:

- Die `uses:`-Zeilen tragen einen Digest **und** den Tag als Kommentar (`actions/checkout@<sha> # v7`). Fehlt der Kommentar, sagt die Datei nicht mehr, welche Version gemeint ist — dann ist die Konfiguration aus Task 8 falsch und der Task hält an.
- Die `FROM`-Zeilen tragen `@sha256:…`.
- Das `image:` des `app`-Service ist **nicht** angefasst: es ist das eigene Release-Image und trägt ein Versions-Tag (Task 7, die `enabled: false`-Regel aus Task 8).

- [x] **Step 3: Den Actions-PR der CI vorlegen und mergen**

Ein PR, der nur `.github/workflows/**` anfasst, bekommt wegen der CI-Ausnahme aus #29 keine Prüfung (R8). Deshalb von Hand:

```bash
gh workflow run ci.yml --ref <branch-des-prs>
gh run list --workflow=ci.yml --limit 1
```

Erst wenn dieser Lauf grün ist, den PR mergen. Die übrigen PRs (Dockerfile, Compose) fassen Code-relevante Dateien an und bekommen ihre CI von selbst.

- [x] **Step 4: Die Kommentare berichtigen**

Renovate pinnt, aber es liest keine Prosa. Über den `FROM`-Zeilen im `Dockerfile` und über `postgres:` in `docker-compose.yml` steht, dass Patch-Releases von selbst ankommen — beim nächsten Build beziehungsweise mit `docker compose pull`. Das gilt nach dem Pinnen nicht mehr (R9).

Die Kommentare so berichtigen, dass sie sagen, was jetzt stimmt: Der Digest friert das Image ein, Renovate hebt ihn wöchentlich, und wer schneller will, stößt das Update im „Dependency Dashboard“ an. Der Hinweis auf den Major-Wechsel von Postgres bleibt wortgleich stehen — er gilt unverändert.

- [x] **Step 5: Docker-Gate**

```bash
docker build --build-arg VERSION=smoke -t recipe-reader:ci .
scripts/smoke-test-image.sh recipe-reader:ci smoke
```

Erwartet: `smoke test passed: recipe-reader:ci (smoke)`. Ein Digest, der nicht auf das Image zeigt, das er soll, lässt den Build sofort scheitern — deshalb ist das Gate hier die eigentliche Prüfung der gemergten PRs.

- [x] **Step 6: Prüfen, dass nichts übrig ist**

```bash
grep -rn 'uses: .*@v[0-9]' .github/workflows/ ; echo "exit=$?"
grep -n '^FROM' Dockerfile
grep -n 'image:' docker-compose.yml
```

Erwartet: `exit=1` aus dem `grep` (kein Treffer), jedes `FROM` mit `@sha256:`, und in Compose der gepinnte Postgres neben dem ungepinnten eigenen Image. Findet der erste Befehl etwas, hat Renovate eine Stelle nicht erfasst — die Fundstelle melden, nicht von Hand nachziehen: dann stimmt die Konfiguration nicht, und von Hand gepinnt würde derselbe Fehler beim nächsten Update wiederkehren.

- [x] **Step 7: Commit-Gate und Commit**

Nur die Kommentare aus Step 4 sind noch uncommittet; das Pinnen selbst steckt in Renovates gemergten PRs.

```bash
git add Dockerfile docker-compose.yml
git commit -m "docs: say what a pinned base image means" -m "The comments promised that patch releases arrive on their own — with the digests Renovate just pinned, they do not. Renovate raises them weekly, and the Dependency Dashboard triggers one at once when that is too slow."
```

---

### Task 11: goose ersetzt golang-migrate

Der Tausch. Er ist ein Task und nicht drei, weil kein Zwischenstand übersetzt und läuft: sobald die Migrationsdateien goose' Format tragen, findet golang-migrate keine mehr, und solange `connect.go` golang-migrate benutzt, kann goose die Dateien nicht anwenden.

Was verschwindet: das Interface `migrator`, der Adapter `gracefulMigrator`, `migrateUp` mit seinen drei Guards, das `context.AfterFunc`, die Funktionen `newMigrate` und `migrateURL` und der Kommentar, der zwei bekannte Schwächen dieser Konstruktion einräumt. Was an ihre Stelle tritt, ist ein Aufruf mit einem Kontext darin.

> **Abweichungen (2026-09-23).**
> - `TestMigration0002_*` steht in `internal/db/migration_0002_test.go`, nicht in `migrations_test.go`; die Datei ist mit umgestellt (`UpTo(ctx, 1)`, `UpTo(ctx, 2)`, am Ende `DownTo(ctx, 1)`).
> - Der Kommentar am Session-Locker nennt golang-migrate nicht mehr beim Namen, sonst fände das M6-Kriterium `grep -rn 'golang-migrate' --include='*.go' .` ihn.
> - E18 stimmt so nicht: `WithVerbose(true)` loggt nicht nur eine Zeile je Migration, sondern **jede SQL-Anweisung samt Text** auf Info (`executing statement`, goose `provider_run.go` `runSQL`). `statementsAtDebug` in `connect.go` stuft genau diese Zeile auf Debug herab; `migration completed` je Migration und `successfully migrated database` bleiben auf Info. Test: `TestStatementsAtDebug`. Eine `no migrations to run`-Zeile gibt es bei einem Start ohne Arbeit nicht — goose ist dann still.
> - `go get goose@v3.28.0` hob per MVS einige indirekte Module an (u. a. `otel` 1.46.0, `grpc` 1.83.2, `testify` 1.12.1); keine neue direkte Abhängigkeit außer goose.
> - Gegenprobe: ohne `WithSessionLocker` schlägt `TestMigrateWithContext_WaitsForTheLockAndReportsGivingUp` fehl (`= nil, want it to report the lock it never got`).

**Files:**
- Create: `internal/db/migrations/0001_init.sql`, `0002_recipe_status_check.sql`, `0003_recipe_ingredient_position_unique.sql`
- Delete: `internal/db/migrations/0001_init.up.sql`, `0001_init.down.sql`, `0002_recipe_status_check.up.sql`, `0002_recipe_status_check.down.sql`, `0003_recipe_ingredient_position_unique.up.sql`, `0003_recipe_ingredient_position_unique.down.sql`
- Modify: `internal/db/connect.go` (alles ab `Migrate`; `Connect`, `Open`, `applyPoolDefaults`, `dsnParams` bleiben unangetastet)
- Modify: `internal/db/migrate_test.go` (Paket und Inhalt)
- Modify: `internal/db/migrations_test.go` (`fileMigrator`, `tables`, die Sequenzabfrage)
- Modify: `internal/db/testdb/testdb.go` (`reset`)
- Modify: `go.mod`, `go.sum`

**Interfaces:**
- Consumes: —
- Produces: `Migrate(dsn) error`, `MigrateWithContext(ctx, dsn) error`, `MigrateDown(dsn) error` — **unveränderte Signaturen**. Nur die Fehlertexte ändern sich, und `migrator`/`gracefulMigrator`/`migrateUp` gibt es nicht mehr.

- [x] **Step 1: goose ins Modul holen**

```bash
go get github.com/pressly/goose/v3@v3.28.0
```

Erwartet: `go.mod` nennt `github.com/pressly/goose/v3 v3.28.0` als direkte Abhängigkeit. golang-migrate bleibt bis Step 9 stehen.

Begründung fürs Protokoll (die Global Constraints verlangen eine): goose **ersetzt** golang-migrate, es kommt nichts hinzu (E12). `pgx/v5/stdlib` ist kein neues Modul, sondern ein Paket aus dem schon vorhandenen `github.com/jackc/pgx/v5`.

- [x] **Step 2: Die sechs Migrationsdateien zu drei zusammenführen**

Die Anweisungen werden nicht angefasst — nur die Klammern `BEGIN;`/`COMMIT;` fallen weg, weil goose jede Migration selbst in eine Transaktion legt und ein `COMMIT;` mittendrin goose' eigene Transaktion vorzeitig beenden würde (E14). Die zwei Kommentarzeilen, die auf diese Klammern verwiesen, sagen danach, wessen Transaktion es ist.

`internal/db/migrations/0001_init.sql` (neu; `0001_init.up.sql` + `0001_init.down.sql` löschen):

```sql
-- +goose Up
CREATE TABLE units (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL UNIQUE
);

CREATE TABLE categories (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL UNIQUE
);

CREATE TABLE ingredients (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL UNIQUE
);

CREATE TABLE recipes (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL,
    instructions TEXT NOT NULL DEFAULT '',
    image_url TEXT NOT NULL DEFAULT '',
    source TEXT NOT NULL UNIQUE,
    status TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- idx_recipes_name_lower serves a future exact lower(name) = $1 lookup only.
-- It does NOT accelerate SearchRecipes: that filter is a leading-wildcard
-- LIKE ('%term%'), which no btree index can serve — substring search is a
-- seq scan until this is replaced with pg_trgm + a GIN index.
CREATE INDEX idx_recipes_name_lower ON recipes (lower(name));
CREATE INDEX idx_recipes_status ON recipes (status);

CREATE TABLE recipe_ingredients (
    id BIGSERIAL PRIMARY KEY,
    recipe_id BIGINT NOT NULL REFERENCES recipes(id) ON DELETE CASCADE,
    ingredient_id BIGINT NOT NULL REFERENCES ingredients(id),
    amount DOUBLE PRECISION NOT NULL DEFAULT 0,
    unit_id BIGINT REFERENCES units(id),
    position INT NOT NULL DEFAULT 0
);
CREATE INDEX idx_recipe_ingredients_recipe_id ON recipe_ingredients (recipe_id);

CREATE TABLE recipe_categories (
    recipe_id BIGINT NOT NULL REFERENCES recipes(id) ON DELETE CASCADE,
    category_id BIGINT NOT NULL REFERENCES categories(id) ON DELETE CASCADE,
    PRIMARY KEY (recipe_id, category_id)
);

-- +goose Down
DROP TABLE IF EXISTS recipe_categories;
DROP TABLE IF EXISTS recipe_ingredients;
DROP TABLE IF EXISTS recipes;
DROP TABLE IF EXISTS ingredients;
DROP TABLE IF EXISTS categories;
DROP TABLE IF EXISTS units;
```

`internal/db/migrations/0002_recipe_status_check.sql` (neu; beide `0002_*`-Dateien löschen):

```sql
-- +goose Up
-- recipes.status has exactly two legal values (domain.RecipeStatus), and until
-- this migration nothing enforced either: both write handlers stored whatever
-- status a client sent, so a database that has been written to may already hold
-- '' or anything else.
--
-- Those rows are moved to needs_review first — the state that puts a person in
-- front of them — because the constraint would otherwise refuse to apply, and a
-- migration that fails stops an existing deployment from starting at all. goose
-- runs each migration in one transaction, so a failure leaves neither half
-- behind.
UPDATE recipes SET status = 'needs_review'
WHERE status NOT IN ('needs_review', 'published');

ALTER TABLE recipes ADD CONSTRAINT recipes_status_check
    CHECK (status IN ('needs_review', 'published'));

-- +goose Down
-- The backfilled rows are not restored: which ones held an out-of-domain status
-- was never recorded, and needs_review is a legal value under either schema.
ALTER TABLE recipes DROP CONSTRAINT IF EXISTS recipes_status_check;
```

`internal/db/migrations/0003_recipe_ingredient_position_unique.sql` (neu; beide `0003_*`-Dateien löschen):

```sql
-- +goose Up
-- persistence P9 asked whether a recipe may list the same ingredient twice, and
-- the answer is yes: "Für den Teig: 200 g Zucker" and "Für den Belag: 50 g
-- Zucker" are two lines of one recipe. UNIQUE (recipe_id, ingredient_id) would
-- refuse that recipe, or force the two lines into one and lose the difference.
--
-- What tells two such lines apart is their position, so that is what is made
-- unique: the order of a recipe's ingredients is total, and a repeated
-- ingredient is always a separate, ordered line rather than an accidental copy.
--
-- writeAssociations has always written positions 0..n-1 after deleting the old
-- rows, so nothing it wrote collides. A row written by hand may; each recipe's
-- lines are renumbered first, in the order they are already read in
-- (position, then id), which changes no position that was already unique. One
-- transaction, as in 0002, and goose is the one that opens it.
UPDATE recipe_ingredients AS ri
SET position = renumbered.position
FROM (
    SELECT id, (row_number() OVER (PARTITION BY recipe_id ORDER BY position, id) - 1)::int AS position
    FROM recipe_ingredients
) AS renumbered
WHERE ri.id = renumbered.id AND ri.position <> renumbered.position;

ALTER TABLE recipe_ingredients
    ADD CONSTRAINT recipe_ingredients_recipe_id_position_key UNIQUE (recipe_id, position);

-- +goose Down
-- The renumbered positions are not restored: which rows collided was never
-- recorded, and the renumbering kept their order.
ALTER TABLE recipe_ingredients DROP CONSTRAINT IF EXISTS recipe_ingredients_recipe_id_position_key;
```

```bash
git rm internal/db/migrations/0001_init.up.sql internal/db/migrations/0001_init.down.sql \
       internal/db/migrations/0002_recipe_status_check.up.sql internal/db/migrations/0002_recipe_status_check.down.sql \
       internal/db/migrations/0003_recipe_ingredient_position_unique.up.sql internal/db/migrations/0003_recipe_ingredient_position_unique.down.sql
```

- [x] **Step 3: Belegen, dass der alte Stand die neuen Dateien nicht lesen kann**

Run: `go test -count=1 -run TestMigrateConnectSeed ./internal/db/`
Expected: FAIL. golang-migrates `iofs`-Quelle erkennt nur `*.up.sql`/`*.down.sql`; die Meldung nennt `no migration found` oder `file does not exist`.

Das ist der Grund, warum dieser Task nicht teilbar ist: von hier bis Step 10 ist der Baum rot.

- [x] **Step 4: `connect.go` auf goose umbauen**

Der Importblock von `internal/db/connect.go` — `migrate`, der Blankimport von `database/pgx/v5`, `source/iofs` und `strings` fallen weg, `database/sql`, `io/fs`, `log/slog`, `pgx` selbst, `pgx/v5/stdlib` und die drei goose-Pakete kommen dazu:

```go
import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/url"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/database"
	"github.com/pressly/goose/v3/lock"
)
```

Alles ab `// Migrate applies all pending embedded migrations` bis zum Dateiende ersetzen durch:

```go
// Migrate applies all pending embedded migrations against dsn (a
// postgres:// URL — the same one passed to Connect).
func Migrate(dsn string) error {
	return MigrateWithContext(context.Background(), dsn)
}

// MigrateWithContext is Migrate, bounded by ctx. Cancelling it rolls back the
// migration in flight and leaves every migration applied before it applied, so
// a second run continues where this one stopped.
//
// That promise is goose's rather than this file's: it runs each SQL migration
// in its own transaction and holds a Postgres advisory lock for the length of
// the run, so two instances starting at once queue up instead of racing. The
// version before this one built the same guarantee by hand out of golang-
// migrate's GracefulStop channel, a context.AfterFunc and two ctx.Err()
// checks, and still could not tell a cancelled run from a completed one when
// the cancellation arrived during the last migration.
func MigrateWithContext(ctx context.Context, dsn string) error {
	return withMigrator(ctx, dsn, func(provider *goose.Provider) error {
		_, err := provider.Up(ctx)
		return err
	})
}

// MigrateDown reverts every applied migration against dsn, newest first,
// leaving no application table behind. It destroys every row in them.
// goose_db_version stays, holding only its zero row: that is how goose records
// a database at no version, as opposed to one it has never seen.
//
// Nothing in the binary calls it. It exists so the down migrations run through
// the same embedded source and the same DSN handling as Migrate, which is what
// a rollback of a deployed binary would have to use — the image carries no
// migration files for a goose command line to read. A down migration that has
// never been run is not a rollback plan: until TestMigrations_UpDownUp ran
// this, the only evidence that 0001 drops its tables in a workable order was
// reading it.
func MigrateDown(dsn string) error {
	ctx := context.Background()
	return withMigrator(ctx, dsn, func(provider *goose.Provider) error {
		_, err := provider.DownTo(ctx, 0)
		return err
	})
}

// withMigrator opens a goose provider over the embedded migrations for dsn and
// hands it to run.
//
// The guard on the way in is the one piece of the hand-written cancellation
// path worth keeping: without it an already cancelled context would still cost
// a connection and a lock attempt before failing.
func withMigrator(ctx context.Context, dsn string, run func(*goose.Provider) error) (retErr error) {
	if err := ctx.Err(); err != nil {
		return cancelledMigration(err)
	}
	sqlDB, err := openSQL(dsn)
	if err != nil {
		return err
	}
	defer func() { retErr = errors.Join(retErr, sqlDB.Close()) }()

	src, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("db: migration source: %w", err)
	}
	// golang-migrate's pgx driver took an advisory lock of its own accord
	// (database/postgres/postgres.go:241). goose does it only when asked, and
	// dropping the option would quietly lose the protection: two instances
	// starting at once would apply the same migration side by side.
	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return fmt.Errorf("db: migration lock: %w", err)
	}
	provider, err := goose.NewProvider(database.DialectPostgres, sqlDB, src,
		goose.WithSessionLocker(locker),
		// Migrating used to be silent, so a slow start said nothing about which
		// migration it was in. goose is silent too unless asked, and through
		// slog it obeys LOG_LEVEL and LOG_FORMAT like every other line.
		goose.WithSlog(slog.Default()),
		goose.WithVerbose(true),
	)
	if err != nil {
		return fmt.Errorf("db: migration init: %w", err)
	}

	if err := run(provider); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return cancelledMigration(err)
		}
		return fmt.Errorf("db: migrate: %w", err)
	}
	return nil
}

// openSQL opens a database/sql handle on dsn through pgx's stdlib adapter.
//
// goose speaks database/sql and the rest of this project speaks pgx, so
// exactly one place translates between them, and this is it. The handle is
// capped at a single connection because a migration run is a single connection
// by construction — goose takes one *sql.Conn, holds its advisory lock on it
// and applies every migration through it — and the cap says so rather than
// leaving a pool idling behind the migrator. The caller closes it.
func openSQL(dsn string) (*sql.DB, error) {
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("db: parse dsn: %w", err)
	}
	sqlDB := stdlib.OpenDB(*cfg)
	sqlDB.SetMaxOpenConns(1)
	return sqlDB, nil
}

// cancelledMigration says what an operator reading the line needs to decide
// what to do next. "context canceled" on its own reads like damage; what in
// fact happened is that the migration in flight was rolled back and every
// earlier one stands, so running the command again finishes the job.
func cancelledMigration(err error) error {
	return fmt.Errorf("db: migrate: cancelled, the migration in flight was rolled back and "+
		"re-running continues where it stopped: %w", err)
}
```

`//go:embed migrations/*.sql` und `var migrationsFS embed.FS` bleiben, wo sie stehen — der Ausdruck passt auf die drei neuen Dateien genauso.

Hinweise für den Review:
- Der Präfix der Abbruchmeldung ändert sich von `db: migrate up:` auf `db: migrate:`, weil dieselbe Meldung jetzt auch `MigrateDown` bedient. Das Wort `re-running`, auf das der Test aus Task 3 prüft, bleibt (R13).
- `lock.DefaultLockID` ist `4097083626`; der Import-Lock dieses Projekts steht auf `8_233_071_001` (`internal/db/lock.go`). Keine Kollision — aber nachsehen, nicht glauben.
- `errors.Join(retErr, sqlDB.Close())` gibt `nil` zurück, wenn beide `nil` sind; ein Fehler beim Schließen geht damit nicht verloren, verdrängt aber auch keinen echten.

- [x] **Step 5: `migrate_test.go` ersetzen**

Der bisherige Inhalt prüft das Gerüst, das es nicht mehr gibt — `stoppingMigrator`, `errNeverStopped`, `migrateUp`. Er fällt ersatzlos weg. An seine Stelle tritt der einzige Teil, den goose nicht schon selbst testet: dass der Lock überhaupt gesetzt ist, und dass ein Aufrufer, dem das Warten zu lang wird, einen Satz bekommt, mit dem er etwas anfangen kann.

Die Datei ist danach `package db_test` statt `package db`, weil sie eine echte Datenbank braucht und `testdb` dieses Paket importiert.

```go
package db_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/pressly/goose/v3/lock"

	"github.com/sBurmester/recipe-reader/internal/db"
	"github.com/sBurmester/recipe-reader/internal/db/testdb"
)

// The advisory lock is goose's, but asking for it is this package's doing, and
// nothing else in the suite would notice if the option were dropped: a lock
// that is never contended looks exactly like no lock at all. So the test
// contends it — and then checks that a caller who gives up waiting is told
// what that means, because that sentence is the other thing this file owns.
func TestMigrateWithContext_WaitsForTheLockAndReportsGivingUp(t *testing.T) {
	dsn := testdb.NewDatabase(t, "migrate_lock_contention")

	holder, err := pgx.Connect(t.Context(), dsn)
	if err != nil {
		t.Fatalf("connect the lock holder: %v", err)
	}
	defer func() { _ = holder.Close(context.WithoutCancel(t.Context())) }()
	if _, err := holder.Exec(t.Context(), "SELECT pg_advisory_lock($1)", lock.DefaultLockID); err != nil {
		t.Fatalf("take the migration lock: %v", err)
	}

	const budget = 2 * time.Second
	ctx, cancel := context.WithTimeout(t.Context(), budget)
	defer cancel()
	start := time.Now()
	err = db.MigrateWithContext(ctx, dsn)

	if err == nil {
		t.Fatal("MigrateWithContext() = nil, want it to report the lock it never got")
	}
	if waited := time.Since(start); waited < budget/2 {
		t.Errorf("MigrateWithContext() gave up after %v, want it to wait for the lock", waited)
	}
	if !strings.Contains(err.Error(), "re-running") {
		t.Errorf("MigrateWithContext() error = %q, want it to say that re-running is safe", err)
	}
	if tableExists(t, dsn, "units") {
		t.Error("MigrateWithContext() applied a migration although it never held the lock")
	}
}

// tableExists reports whether a table of that name is in the public schema.
func tableExists(t *testing.T, dsn, name string) bool {
	t.Helper()
	pool := connect(t, dsn)
	defer pool.Close()
	var exists bool
	if err := pool.QueryRow(t.Context(), "SELECT to_regclass($1) IS NOT NULL", "public."+name).Scan(&exists); err != nil {
		t.Fatalf("look for %s: %v", name, err)
	}
	return exists
}
```

`connect` stammt aus `migrations_test.go`, gleiches Testpaket.

- [x] **Step 6: `migrations_test.go` auf goose umstellen**

Drei Stellen. Erstens der Importblock: `github.com/golang-migrate/migrate/v4`, der Blankimport `_ ".../source/file"` und `net/url` fallen weg; dazu kommen

```go
	"database/sql"
	"os"

	// Registers the "pgx" driver name with database/sql, which goose speaks.
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/database"
```

`github.com/jackc/pgx/v5` und `pgconn` bleiben — `connect`, `tables` und die Constraint-Prüfungen nutzen sie weiter.

Zweitens `fileMigrator` wird zu `fileProvider`:

```go
// fileProvider returns a migrator over ./migrations for dsn, closed when the
// test ends. It reads the files from disk rather than the embedded copy — the
// same files — because stepping to an exact version needs goose's own API,
// which db deliberately does not export.
func fileProvider(t *testing.T, dsn string) *goose.Provider {
	t.Helper()
	sqlDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	provider, err := goose.NewProvider(database.DialectPostgres, sqlDB, os.DirFS("migrations"))
	if err != nil {
		t.Fatalf("goose.NewProvider: %v", err)
	}
	return provider
}
```

`sql.Open` statt `openSQL`: letzteres ist unexportiert, und dieses Testpaket ist extern.

Drittens die Aufrufe. golang-migrates `Migrate(n)` lief in beide Richtungen, goose will wissen, in welche:

| bisher | neu |
| --- | --- |
| `m.Migrate(1)` in `TestMigration0002_*` | `provider.UpTo(ctx, 1)` |
| `m.Migrate(2)` in `TestMigration0002_*` | `provider.UpTo(ctx, 2)` |
| `m.Migrate(2)` in `TestMigration0003_*` (erster) | `provider.UpTo(ctx, 2)` |
| `m.Migrate(3)` in `TestMigration0003_*` | `provider.UpTo(ctx, 3)` |
| `m.Migrate(2)` in `TestMigration0003_*` (letzter, am Testende) | `provider.DownTo(ctx, 2)` |

Beide geben `([]*goose.MigrationResult, error)` zurück; der Rückgabewert wird verworfen, der Fehler geprüft — die bestehenden `t.Fatalf`-Meldungen passen weiter.

- [x] **Step 7: Die beiden Katalogabfragen in `migrations_test.go` nachziehen**

`tables` schließt heute `schema_migrations` aus. Die Tabelle gibt es nicht mehr; `goose_db_version` schon:

```go
	rows, err := pool.Query(context.Background(),
		`SELECT tablename FROM pg_tables
		 WHERE schemaname = 'public' AND tablename <> 'goose_db_version' ORDER BY tablename`)
```

Und in `TestMigrations_UpDownUp` die Sequenzabfrage. goose' Versionstabelle hat eine `GENERATED BY DEFAULT AS IDENTITY`-Spalte, und die bringt eine Sequenz mit — die Behauptung „nach dem Down ist keine Sequenz übrig" wäre sonst falsch, obwohl das Schema sauber ist:

```go
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		 WHERE n.nspname = 'public' AND c.relkind = 'S'
		   AND c.relname NOT LIKE 'goose\_db\_version%'`).Scan(&sequences); err != nil {
```

- [x] **Step 8: `reset` in `internal/db/testdb/testdb.go` nachziehen**

`reset` truncatet jede Tabelle im Schema `public` außer `schema_migrations` — also künftig auch die Versionstabelle des geteilten Containers (R11). Abfrage und Kommentar:

```go
// The table list comes from the catalog rather than being written out, so a
// table a later migration adds is covered without anyone remembering to add it
// here. goose_db_version is left alone: it records the schema version, not
// test data, and truncating it would tell the next migration run that the
// container is empty.
func reset(ctx context.Context, pool *pgxpool.Pool) error {
	rows, err := pool.Query(ctx,
		`SELECT quote_ident(tablename) FROM pg_tables
		 WHERE schemaname = 'public' AND tablename <> 'goose_db_version'`)
```

- [x] **Step 9: golang-migrate aus dem Modul entfernen**

```bash
go mod tidy
grep -rn 'golang-migrate' --include='*.go' . ; grep -n 'golang-migrate' go.mod
```

Erwartet: beide `grep` finden nichts, und `go.mod` nennt `github.com/pressly/goose/v3` unter den direkten Abhängigkeiten. Findet der erste `grep` noch etwas, ist eine Stelle übersehen — nicht `go mod tidy` wiederholen, sondern die Stelle umbauen.

- [x] **Step 10: Tests laufen lassen, sie müssen grün sein**

Run: `go test -count=1 -race ./internal/db/...`
Expected: PASS — `TestMigrations_UpDownUp`, `TestMigration0002_*`, `TestMigration0003_*` und `TestMigrateWithContext_*` eingeschlossen.

Die Tests legen ihre Datenbanken über `testdb` frisch an, es gibt darin also nie eine `schema_migrations`-Tabelle. Falls ein Lauf trotzdem an `CREATE TABLE units` scheitert, steht ein alter Container: `docker ps` und ihn wegräumen.

- [x] **Step 11: Belegen, dass sqlc nichts anderes generiert**

Die Migrationsdateien sind zugleich sqlc' Schemaquelle. sqlc versteht goose' Annotationen (`internal/migrations/migrations.go:26`), aber das ist eine Behauptung über eine Fremdbibliothek — also nachmessen (R10):

```bash
make sqlc-generate
git diff --exit-code internal/db/sqlc
```

Erwartet: Exit 0, keine Ausgabe. Gibt es eine Ausgabe, hier anhalten: entweder ist eine Anweisung beim Zusammenführen verrutscht, oder sqlc liest das neue Format anders als erwartet. Beides gehört dem Nutzer vorgelegt, nicht durch Nachgenerieren übertüncht.

- [x] **Step 12: Den ganzen Baum prüfen**

```bash
go test -count=1 -race ./...
```

Erwartet: PASS. `internal/server` und `cmd/recipe-reader` migrieren über dieselben Funktionen und müssen es unverändert tun.

- [x] **Step 13: Das Image gegenprüfen**

Der Docker-Gate greift laut Global Constraints nur bei Änderungen an `Dockerfile` oder `scripts/`, und beides bleibt unberührt. Trotzdem: die Migrationen liegen als `embed.FS` in der Binary, und dass der neue Leser sie dort auch findet, zeigt erst ein Lauf gegen ein echtes Postgres im Image.

```bash
docker build --build-arg VERSION=smoke -t recipe-reader:ci .
scripts/smoke-test-image.sh recipe-reader:ci smoke
```

Erwartet: `smoke test passed: recipe-reader:ci (smoke)`.

- [x] **Step 14: Commit-Gate** (siehe Global Constraints), dann stagen

```bash
git add go.mod go.sum internal/db
```

Commit-Message (die Hauptsession committet, siehe „Ablauf der Umsetzung"):

```
refactor(db)!: migrate with goose instead of golang-migrate

golang-migrate has no context parameter, only a GracefulStop channel, so
internal/db carried a migrator interface, a gracefulMigrator adapter, a
context.AfterFunc and two ctx.Err() checks to turn that into a
cancellation — and still could not tell a cancelled run from a completed
one when the cancellation arrived during the last migration. goose takes
a context, runs each migration in its own transaction and takes the
advisory lock golang-migrate's driver took by itself, so all of that
goes.

The six *.up.sql/*.down.sql files become three annotated ones. Their
statements are unchanged except for the BEGIN;/COMMIT; pairs in 0002 and
0003, which goose now opens itself.

BREAKING CHANGE: the version table moves from schema_migrations to
goose_db_version, and nothing carries the old one over. A database an
earlier build created has to be recreated — see the README. The software
has not been deployed, so no such database exists outside development.
```

---

### Task 12: Die Anleitung beschreibt das goose-Format

Die README beschreibt an drei Stellen, was jetzt anders ist: wie eine Migration aussieht, was ein Ctrl-C während `migrate` hinterlässt, und wie die Versionstabelle heißt, die ein Restore mitbringt. Ohne diesen Task legt der nächste Entwickler ein `000N_*.up.sql` an, das niemand liest.

**Files:**
- Modify: `README.md` (Zeile ~43, der `migrate`-Eintrag der Kommandotabelle; Abschnitt „Upgrading from Postgres 17"; Abschnitt „Changing the database schema")

**Interfaces:**
- Consumes: den Stand nach Task 11
- Produces: keine Codeänderung

- [x] **Step 1: Den `migrate`-Eintrag der Kommandotabelle berichtigen**

Heute steht dort:

> Ctrl-C or `SIGTERM` stops it between two migrations — the one in flight always finishes — and it then exits 1 reporting the cancellation rather than success. Re-running is safe and picks up where it left off.

Das galt für golang-migrate. Ersetzen durch:

> Ctrl-C or `SIGTERM` rolls back the migration in flight and leaves every earlier one applied; it then exits 1 reporting the cancellation rather than success. Re-running is safe and picks up where it left off.

Der Rest des Eintrags bleibt.

- [x] **Step 2: Den Restore-Abschnitt berichtigen**

Im Abschnitt „Upgrading from Postgres 17" steht heute:

> The restored `schema_migrations` table leaves the app nothing to migrate, and the ID sequences carry on from where they were.

Ersetzen durch:

> The restored `goose_db_version` table leaves the app nothing to migrate, and the ID sequences carry on from where they were.

- [x] **Step 3: „Changing the database schema" auf das neue Format bringen**

Schritt 1 der nummerierten Liste lautet heute:

> 1. Add `internal/db/migrations/000N_*.up.sql` and the matching `.down.sql`.

Ersetzen durch:

> 1. Add one file, `internal/db/migrations/000N_<name>.sql`, holding both directions:
>
>    ```sql
>    -- +goose Up
>    ALTER TABLE recipes ADD COLUMN servings INT NOT NULL DEFAULT 0;
>
>    -- +goose Down
>    ALTER TABLE recipes DROP COLUMN IF EXISTS servings;
>    ```
>
>    Do not write `BEGIN;`/`COMMIT;` — goose runs each migration in a transaction of its own, and a
>    `COMMIT;` in the middle would end it early. A statement that cannot run inside a transaction,
>    `CREATE INDEX CONCURRENTLY` above all, needs `-- +goose NO TRANSACTION` on the first line of the
>    file instead.

Die Schritte 2 bis 4 bleiben unverändert.

- [x] **Step 4: Den Absatz darunter ergänzen**

Hinter dem Absatz, der `TestMigrations_UpDownUp` beschreibt, zwei neue anhängen:

> Migrations run under [goose](https://github.com/pressly/goose), embedded in the binary — there is no
> migration tool to install and no migration files in the image. goose records what it has applied in
> `goose_db_version`, one row per version, and holds a Postgres advisory lock for the length of a run,
> so two instances starting at once queue up instead of racing. The same files are sqlc's schema
> source; sqlc stops reading at `-- +goose Down`, so the down direction never reaches the generated
> code.
>
> Before goose the bookkeeping lived in `schema_migrations`, written by golang-migrate, and nothing
> carries that table over. A database created by a build older than the switch is therefore not
> usable: goose finds no version table, takes the database for empty and fails on `CREATE TABLE
> units`. Recreate it — `docker compose down -v`, or `dropdb` and start the app — or, to keep the
> rows, write the bookkeeping by hand once:
>
>     CREATE TABLE goose_db_version (
>         id integer PRIMARY KEY GENERATED BY DEFAULT AS IDENTITY,
>         version_id bigint NOT NULL,
>         is_applied boolean NOT NULL,
>         tstamp timestamp NOT NULL DEFAULT now()
>     );
>     INSERT INTO goose_db_version (version_id, is_applied)
>         VALUES (0, true), (1, true), (2, true), (3, true);
>     DROP TABLE schema_migrations;

- [x] **Step 5: Die Änderung gegenlesen**

```bash
grep -n 'schema_migrations\|\.up\.sql\|\.down\.sql' README.md
```

Erwartet: nur noch die Fundstellen im neuen Absatz aus Step 4, die `schema_migrations` bewusst als den alten Namen nennen. Findet sich sonst etwas, ist eine Stelle übersehen.

- [x] **Step 6: Commit-Gate** (siehe Global Constraints — der Go-Teil läuft auch bei einer reinen Dokumentationsänderung, er ist billig und beweist, dass der Baum nach Task 11 steht), dann stagen

```bash
git add README.md
```

Commit-Message:

```
docs: describe the goose migration format

A migration is one file with -- +goose Up and -- +goose Down and no
BEGIN;/COMMIT; of its own. Ctrl-C during migrate now rolls the migration
in flight back rather than finishing it, and a database created before
the switch has to be recreated, because nothing carries schema_migrations
over.
```

- [x] **Step 7: Diese Datei abhaken** — Task 11, Task 12 und die Abnahmekriterien von M6.

> **Abweichung (2026-09-23).** Das Bookkeeping-SQL aus Step 4 scheiterte in der Probe: ein goose-Start gegen eine Datenbank von `v0.1.4` legt `goose_db_version` (mit Version 0) an, **bevor** er an `CREATE TABLE units` scheitert — wer die Meldung liest und dann das SQL ausführt, bekam `relation "goose_db_version" already exists`. Die README nimmt deshalb `CREATE TABLE IF NOT EXISTS` und fügt nur die fehlenden Versionen ein (`generate_series(0, 3)` mit `NOT EXISTS`). Geprüft an einer Wegwerf-Datenbank: `v0.1.4` migriert, eine Zeile geschrieben, das neue Image scheitert wie beschrieben, SQL angewandt, das neue Image migriert ohne Arbeit, `goose_db_version` = `0,1,2,3`, die Zeile steht noch.

---

### Abschluss-Verifikation

- [x] **Step 1: Commit-Gate komplett** (siehe Global Constraints). Alles grün.

- [x] **Step 2: Docker-Gate** (siehe Global Constraints). `smoke test passed`.

- [x] **Step 3: Den Release-PR lesen, nicht bestätigen**

Nach dem Merge von M4 öffnet `release-please` einen PR. Vor dem Merge prüfen:

- Die vorgeschlagene Version ist `v0.1.0` (E2).
- Das Changelog beginnt beim `bootstrap-sha` und enthält nicht die gesamte Projekthistorie (R1).
- Die Einträge stimmen mit den Commits der vier Milestones überein.

Weicht etwas ab, hier anhalten und dem Nutzer vorlegen.

- [x] **Step 4: Das Release erzeugen und prüfen**

Nach dem Merge des Release-PRs:

```bash
gh release view v0.1.0
gh run list --workflow=release-artifacts.yml --limit 1
```

Erwartet: das Release existiert, der Workflow ist grün, und das Release trägt drei Binaries plus `checksums.txt`.

- [x] **Step 5: Eine Binary gegenprüfen**

```bash
gh release download v0.1.0 --pattern 'recipe-reader_v0.1.0_linux_amd64' --dir /tmp/rr-check
gh release download v0.1.0 --pattern 'checksums.txt' --dir /tmp/rr-check
cd /tmp/rr-check && sha256sum -c --ignore-missing checksums.txt
chmod +x recipe-reader_v0.1.0_linux_amd64 && ./recipe-reader_v0.1.0_linux_amd64 --version
```

Erwartet: `OK` aus der Prüfsummenkontrolle und `v0.1.0` als Versionsausgabe.

- [x] **Step 6: Das Image gegenprüfen**

```bash
docker pull ghcr.io/sburmester/recipe-reader:v0.1.0
scripts/smoke-test-image.sh ghcr.io/sburmester/recipe-reader:v0.1.0 v0.1.0
```

Erwartet: `smoke test passed: ghcr.io/sburmester/recipe-reader:v0.1.0 (v0.1.0)`. Das ist zugleich der Beleg, dass die Versionsstempelung durch den Build-Arg-Weg korrekt ankommt.

- [x] **Step 7: Compose gegen das veröffentlichte Image**

```bash
export API_TOKEN=$(openssl rand -hex 32) POSTGRES_PASSWORD=$(openssl rand -hex 16) RECIPE_READER_VERSION=v0.1.0
docker compose -p recipe-reader-e2e pull app
docker compose -p recipe-reader-e2e up -d --wait
docker compose -p recipe-reader-e2e exec app recipe-reader --version
docker compose -p recipe-reader-e2e down -v
```

Erwartet: `pull` lädt das Image, der Stack wird `healthy`, und `--version` meldet `v0.1.0` — ohne lokalen Build. `down -v` auch dann ausführen, wenn ein Schritt davor scheitert; nie ohne `-p recipe-reader-e2e`, weil das Default-Projekt das Volume `db-data` mit echten Daten hält.

- [x] **Step 8: Renovate im Betrieb nachsehen**

```bash
gh issue list --search 'Dependency Dashboard in:title' --json number,title
gh pr list --label dependencies --state all --limit 10 --json number,title,state --jq '.[] | "\(.number)\t\(.state)\t\(.title)"'
```

Erwartet: das Dashboard-Issue und mindestens ein PR, der geöffnet und gemergt wurde — der Beleg, dass Renovate nicht nur konfiguriert ist, sondern arbeitet. Findet sich kein einziger PR, ist entweder nichts zu aktualisieren (dann sagt das Dashboard das) oder die App ist nicht installiert (Task 9); beides ist zu unterscheiden und zu melden, nicht als Erfolg zu verbuchen.

- [x] **Step 9: Die Entwicklungsdatenbank auf das neue Bookkeeping bringen**

Jede Datenbank, die ein Build vor M6 angelegt hat, trägt `schema_migrations` und kein `goose_db_version`. goose hält sie für leer, wendet `0001` erneut an und scheitert an `CREATE TABLE units` (R12). Die Testdatenbanken sind davon nicht betroffen — `testdb` legt sie frisch an —, die lokale Compose-Instanz schon.

**Den Nutzer fragen, welcher Weg.** Der erste wirft die Daten weg.

```bash
# Weg A: neu anlegen. Zerstört das Volume db-data mit allem darin.
docker compose down -v
docker compose up -d --wait

# Weg B: die Zeilen behalten und das Bookkeeping einmal von Hand schreiben.
docker compose exec -T db pg_dump -U recipes -Fc recipes > recipes-before-goose.dump
docker compose exec -T db psql -U recipes -d recipes -v ON_ERROR_STOP=1 <<'SQL'
CREATE TABLE goose_db_version (
    id integer PRIMARY KEY GENERATED BY DEFAULT AS IDENTITY,
    version_id bigint NOT NULL,
    is_applied boolean NOT NULL,
    tstamp timestamp NOT NULL DEFAULT now()
);
INSERT INTO goose_db_version (version_id, is_applied)
    VALUES (0, true), (1, true), (2, true), (3, true);
DROP TABLE schema_migrations;
SQL
docker compose build app
docker compose run --rm app migrate
docker compose up -d --wait
```

Erwartet bei Weg B: `migrate` loggt `no migrations to run, current version: 3` und wendet nichts an. Wendet es etwas an, hier anhalten, den Dienst nicht starten und dem Nutzer vorlegen — die Sicherung aus Zeile 1 ist dann der Weg zurück.

Erwartet bei beiden Wegen: der Stack wird `healthy`, und `docker compose exec -T db psql -U recipes -d recipes -c 'SELECT version_id FROM goose_db_version ORDER BY version_id'` zeigt `0,1,2,3`.

- [x] **Step 10: Akzeptanzkriterien in Teil A abhaken** (US1–US7, Definition of Done)

> **Abnahme (2026-09-23).** Steps 1–2: Commit-Gate und Docker-Gate grün (`smoke test passed: recipe-reader:ci (smoke)`); `govulncheck` meldet nur GO-2026-5932 (`golang.org/x/crypto/openpgp`, nicht aufgerufen, ohne Fix-Version). Steps 3–7 sind mit M4 abgenommen (siehe die Abnahme unter M4; vollständig ab `v0.1.1`). Step 8: Dashboard #47, gemergte Renovate-PRs #49, #51, #56, #58, #59. Step 9: auf diesem Rechner gibt es kein `db-data`-Volume und keine `.env`, also keine Entwicklungsdatenbank umzustellen; Weg B ist stattdessen an einer Wegwerf-Datenbank geprüft (siehe Abweichung unter Task 12), mit der wiederholbaren Fassung des SQL.

- [x] **Step 11: Diese Datei abhaken und committen**

```bash
git add docs/superpowers/plans/
git commit -m "docs: mark M6 done in the release plan"
```

- [x] **Step 12: PR 6/6** erst nach Freigabe durch den Nutzer öffnen. *(#62, 2026-09-23)*

---

## Teil D: Abdeckung des Backlogs

| Backlog-Punkt (Plan vom 2026-09-21) | Umgesetzt in |
| --- | --- |
| Der Shutdown von `serve` hat keine obere Schranke mehr | Task 1 (E3); die Ursache bleibt im Backlog dieses Plans |
| `.env.example` und `docker-compose.yml` laufen auseinander | Task 2 (E4) |
| Fehlermeldungen eindeutiger machen | Task 3 und Task 4; kongs Präfix bleibt im Backlog, weil es ein Nutzerurteil braucht |
| Automatisches GitHub-Release je Versions-Tag | Task 5 (Version und Changelog), Task 6 (Binaries), Task 7 (Image und Compose) |
| Offene Frage „Woher kommt die Versionsnummer?" | E1 — `release-please` aus den Commit-Präfixen |
| Offene Frage „Wo fängt die Zählung an?" | E2 — `v0.1.0` |
| Offene Frage „Welche Plattformen?" | Task 6 — `linux/amd64`, `linux/arm64`, `darwin/arm64`, mit Prüfsummen |
| Offene Frage „Was heißt *package* für Compose?" | E6 und Task 7 — GHCR-Image neben dem lokalen Build |
| Renovate für Go, Actions und Docker (Nutzer, 2026-09-22) | Task 8 (Konfiguration), Task 9 (Betrieb als GitHub-App, E9) |
| Actions und Docker-Abhängigkeiten auf Digest pinnen (Nutzer, 2026-09-22) | Task 8 (`helpers:pinGitHubActionDigests`, `pinDigests`), Task 10 — Renovate pinnt, der Task prüft (E10) |
| Migration von `golang-migrate` auf `pressly/goose` (Nutzer, 2026-09-22) | Task 11 (Dateiformat E14, Engine E12/E16/E17/E18), Task 12 (Anleitung) |
| Offene Frage „Was wird aus dem handgeschriebenen Abbruchpfad?" | Task 11 — `migrator`, `gracefulMigrator`, `migrateUp` und das `context.AfterFunc` entfallen ersatzlos; goose nimmt den Kontext selbst |
| Offene Frage „Was wird aus `schema_migrations`?" | Nichts — die Software war nie im Einsatz (E15). Die Entwicklungsdatenbank wird neu angelegt, Abschluss-Verifikation Step 9 |
