# Backlog des CLI-Umbaus: Shutdown-Schranke, Konfiguration, Fehlermeldungen und Release-Automatik

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Die vier Einträge im Backlog von `docs/superpowers/plans/2026-09-21-cli-backlog.md` abarbeiten: der Shutdown von `serve` bekommt seine obere Schranke zurück, `.env.example` erreicht den Container, zwei irreführende Fehlermeldungen werden eindeutig, und ein Tag nach Semantic Versioning erzeugt automatisch ein GitHub-Release mit Single-Binary und Container-Image.

**Architecture:** Drei kleine Korrekturen am bestehenden Code gehen voraus, die Release-Automatik kommt zuletzt — so trägt das erste Release `v0.1.0` einen Stand, den man veröffentlichen will, und sein Changelog die drei Korrekturen. Der Advisory-Lock wandert von einer geliehenen Pool-Verbindung auf eine eigene `pgx.Connect`-Verbindung, damit `pool.Close()` nicht mehr auf ihn wartet. Die Release-Automatik besteht aus zwei getrennten Workflows: `release-please` schlägt Version und Changelog vor und legt beim Merge Tag und Release an, ein zweiter Workflow hängt beim `published`-Ereignis die Artefakte an.

**Tech Stack:** Go 1.27.1, pgx/v5, `github.com/alecthomas/kong` v1.16.1, Docker Compose 5.5.1, GitHub Actions (`googleapis/release-please-action@v5`, `docker/login-action@v4`, `docker/build-push-action@v7`), GitHub Container Registry. **Keine neue Go-Abhängigkeit.**

## Global Constraints

- **Branch:** Nie auf `main` arbeiten. Jeder Milestone hat einen eigenen Branch und einen eigenen PR; die Namen stehen unter „Branches und PRs".
- **Kompatibilität:** Bestehende Env-Variablen-Namen, Flag-Namen und Defaults bleiben **unverändert**. Exit-Codes bleiben `0` bei Erfolg und `1` bei jedem Fehler, auch bei Parse-Fehlern.
- **Konfigurationsdateien werden mitgepflegt:** Wer eine Env-Variable hinzufügt, trägt sie in `.env.example` nach, im Stil der Datei: unkommentiert, mit dem Default als Wert, thematisch gruppiert; Credentials stehen dort mit leerem Wert (`API_TOKEN=`).
- **Credentials:** `API_TOKEN`, `INSTAGRAM_PASSWORD`, `LLM_API_KEY` und `ANTHROPIC_API_KEY` bleiben nur über die Umgebung setzbar, ohne Flag-Form.
- **Namenskonvention:** Eine Funktion, die einen besonderen Parameter nimmt, heißt `…WithX` — `MigrateWithContext`, nicht `MigrateContext` (globale Go-Richtlinien, Stand 2026-09-22).
- **Kommentarstil:** Kommentare erklären das *Warum*, nicht das *Was*; Zeilen bis ~100 Spalten, wie im Rest des Repos.
- **Standardbibliothek zuerst:** Keine neue Go-Abhängigkeit in diesem Plan. GitHub Actions sind keine Go-Abhängigkeit, aber jede zusätzliche Action braucht eine Zeile Begründung.
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

### Ziele (Outcomes)

| # | Ziel | Messbar an |
| --- | --- | --- |
| Z1 | Ein aufgegebener Import hält das Herunterfahren nicht mehr auf | Der gehaltene Lock belegt keine Pool-Verbindung (Test) |
| Z2 | Was in `.env` steht, wirkt auch im Container | Eine Variable aus `.env.example` ist im laufenden Container sichtbar (Compose-Lauf) |
| Z3 | Eine Fehlermeldung sagt, was zu tun ist | Der Abbruchfall nennt die gefahrlose Wiederholung; ein Bedienfehler ist als solcher erkennbar (Tests) |
| Z4 | Ein Tag erzeugt ein vollständiges Release | `v0.1.0` trägt Binaries für drei Plattformen, Prüfsummen und ein Image in der GHCR |
| Z5 | Compose kann ein veröffentlichtes Image nutzen | `docker compose pull && docker compose up -d` läuft ohne lokalen Build |

### Nicht-Ziele

- Keine vollständige Bestandsaufnahme aller Fehlermeldungen. Dieser Plan behebt die zwei benannten Stellen; die Frage, ob kongs Kommandopfad-Präfix (`serve: config: API_TOKEN …`) einem Betreiber hilft, bleibt im Backlog, weil sie ohne Nutzerurteil nicht zu entscheiden ist.
- Keine Signatur oder Provenance-Attestierung der Artefakte.
- Kein Multi-Arch-Image. Das Image wird für `linux/amd64` gebaut, wie heute auch; Multi-Arch steht im Backlog.
- Keine Änderung am HTTP-API, an der Extraktion oder am Datenbankschema.

### User Stories & Akzeptanzkriterien

**US1: Betreiber fährt den Server herunter, während ein Import läuft.**
- [ ] Der Advisory-Lock belegt keine Verbindung aus dem Pool; `pool.Stat().AcquiredConns()` ist null, während der Lock gehalten wird.
- [ ] `server.Run` kehrt nach einem `SIGTERM` innerhalb seines Budgets zurück, auch wenn ein Import aufgegeben wurde.
- [ ] Der Doc-Kommentar von `Run` behauptet nichts mehr, was nicht gilt.

**US2: Betreiber konfiguriert über `.env`.**
- [ ] Eine Variable, die nur in `.env` steht (z. B. `EXTRACTION_MODE`), ist im laufenden `app`-Container gesetzt.
- [ ] Fehlt `.env`, startet Compose trotzdem.
- [ ] Die zusammengesetzten Werte unter `environment:` (`DB_DSN`) gewinnen weiterhin gegen `.env`.

**US3: Betreiber liest eine Fehlermeldung.**
- [ ] Ein abgebrochener `migrate`-Lauf sagt, dass bereits angewandte Migrationen angewandt bleiben und ein erneuter Lauf gefahrlos fortsetzt.
- [ ] Eine abgelehnte Kommandozeile wird als Bedienfehler gemeldet, ein gescheiterter Lauf als Fehler; beide enden weiterhin mit 1.

**US4: Betreiber installiert eine Version.**
- [ ] Ein Merge des Release-PRs erzeugt Tag und GitHub-Release mit Changelog.
- [ ] Das Release trägt Binaries für `linux/amd64`, `linux/arm64` und `darwin/arm64` sowie eine `checksums.txt`.
- [ ] `recipe-reader --version` einer geladenen Binary meldet genau das Tag.
- [ ] Das Image `ghcr.io/sburmester/recipe-reader:<tag>` existiert und besteht den Smoke-Test.

**US5: Betreiber betreibt über Compose.**
- [ ] `docker compose pull && docker compose up -d` startet ohne lokalen Build.
- [ ] `docker compose up --build` baut weiterhin lokal, für die Entwicklung.

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

### Risiken

| # | Risiko | Gegenmaßnahme |
| --- | --- | --- |
| R1 | Das erste Changelog enthält die komplette Historie | `bootstrap-sha` in `release-please-config.json` auf den Merge-Commit von PR #33 setzen (E8). Der erste Release-PR wird vor dem Merge gelesen, nicht blind bestätigt. |
| R2 | Ein falsches Commit-Präfix erzeugt die falsche Version | Der Release-PR zeigt die vorgeschlagene Version, bevor er gemergt wird. Ein `feat:`, das ein `fix:` sein sollte, kostet einen Minor — unter `0.x` folgenlos. |
| R3 | Das GHCR-Paket ist nach dem ersten Push privat, `docker compose pull` scheitert für andere | Das ist für ein Single-User-Deployment richtig so. Die README nennt den Handgriff, das Paket in den Repository-Einstellungen auf öffentlich zu stellen, falls gewünscht. |
| R4 | `env_file` schleust unerwartete Variablen in den Container | `.env` ist die Datei des Betreibers, und alles darin ist für genau diesen Dienst gedacht. Die zusammengesetzten `environment:`-Werte gewinnen weiterhin, was Task 2 prüft. |
| R5 | Der Workflow-Task wird von der CI nicht geprüft (CI-Ausnahme aus #29) | Die Workflows werden über `workflow_dispatch` bzw. einen echten Release-Lauf geprüft, nicht über CI. Task 7 endet mit einem echten `v0.1.0`. |
| R6 | `pgx.Connect` ohne die Pool-Defaults bekommt kein `connect_timeout` | Der DSN, den `ImportLock` bekommt, ist derselbe wie der des Pools; enthält er `connect_timeout`, gilt es auch hier. Der Aufruf steht ohnehin unter dem Kontext des Laufs. |

### Definition of Done

- [ ] Alle Tasks in dieser Datei abgehakt.
- [ ] Commit-Gate grün; Docker-Gate grün für die Tasks, die das Image betreffen.
- [ ] Die vier Backlog-Einträge im Plan vom 2026-09-21 sind als erledigt markiert oder verweisen auf diesen Plan.
- [ ] `v0.1.0` existiert als Tag, als GitHub-Release mit Changelog und als Image in der GHCR.
- [ ] README aktualisiert: Installation aus einem Release, Betrieb über das veröffentlichte Image, Hinweis auf `env_file`.

### Backlog (bewusst nicht in diesem Plan)

- **Der aufgegebene Import endet erst nach seinem LLM-Timeout.** E3 behebt die Wirkung auf das Herunterfahren, nicht die Ursache. Wer sie aufgreift, reicht den Kontextabbruch bis in den Extraktionspfad durch, sodass ein aufgegebener Lauf sofort endet statt nach bis zu `LLM_TIMEOUT`.
- **Kongs Kommandopfad-Präfix** (`serve: config: API_TOKEN …`): dass der Inhalt gleich bleibt, ist geprüft, ob das Präfix einem Betreiber hilft oder im Weg steht, nie. Braucht ein Nutzerurteil, keinen Task.
- **Multi-Arch-Image.** Das Release baut `linux/amd64`. Ein `linux/arm64`-Image bräuchte QEMU oder einen ARM-Runner.
- **Signatur und Provenance** der Release-Artefakte.

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
  connect.go                 # Fehlermeldung des Abbruchs
internal/server/
  server.go, import.go       # ImportLock{DSN: …}
cmd/recipe-reader/
  main.go                    # Bedienfehler vs. Laufzeitfehler
  main_test.go               # + erwartete Logzeile in der Tabelle
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

#### M1: Shutdown-Schranke

Abnahmekriterien:
- [ ] Die Akzeptanzkriterien von US1 sind abgehakt.
- [ ] `go test -count=1 -race ./internal/db/ ./internal/server/` ist grün.

#### M2: Konfiguration erreicht den Container

Abnahmekriterien:
- [ ] Die Akzeptanzkriterien von US2 sind abgehakt.
- [ ] Der Compose-Lauf aus Task 2, Step 4 ist belegt.

#### M3: Eindeutige Fehlermeldungen

Abnahmekriterien:
- [ ] Die Akzeptanzkriterien von US3 sind abgehakt.
- [ ] Beide Meldungen sind durch einen Test abgesichert, der ohne die Änderung fehlschlägt.

#### M4: Release-Automatik

Abnahmekriterien:
- [ ] Die Akzeptanzkriterien von US4 und US5 sind abgehakt.
- [ ] Die Definition of Done ist abgehakt.

#### Branches und PRs

| Milestone | Branch | PR-Titel |
| --- | --- | --- |
| M1 | `fix/import-lock-own-connection` | `fix(db): hold the import lock on its own connection (1/4)` |
| M2 | `fix/compose-env-file` | `fix(compose): pass .env into the app container (2/4)` |
| M3 | `fix/clearer-error-messages` | `fix(cli): say what a cancelled migration and a bad command line mean (3/4)` |
| M4 | `feat/release-automation` | `feat(ci): release automatically from a version tag (4/4)` |

Jeder Milestone-Branch zweigt von `main` ab, nachdem der vorige PR gemerged ist. Ein PR enthält die Task-Commits seines Milestones, die Korrekturen aus dem Milestone-Review und einen letzten Commit `docs: mark M<n> done in the release plan`.

### Aufgabenliste

- [ ] **M1: Shutdown-Schranke**
  - [ ] **Task 1:** Der Import-Lock hält eine eigene Verbindung (S)
  - [ ] **Milestone-Review**
- [ ] **M2: Konfiguration erreicht den Container**
  - [ ] **Task 2:** `env_file` für den `app`-Service (S)
  - [ ] **Milestone-Review**
- [ ] **M3: Eindeutige Fehlermeldungen**
  - [ ] **Task 3:** Der Abbruch einer Migration sagt, dass Wiederholen gefahrlos ist (S)
  - [ ] **Task 4:** `main` unterscheidet Bedienfehler von Laufzeitfehler (S)
  - [ ] **Milestone-Review**
- [ ] **M4: Release-Automatik**
  - [ ] **Task 5:** `release-please` einrichten (M)
  - [ ] **Task 6:** Binaries und Prüfsummen ans Release hängen (M)
  - [ ] **Task 7:** Image in die GHCR, Compose darauf umstellen (M)
  - [ ] **Milestone-Review**
  - [ ] **Abschluss-Verifikation**

### Ablauf der Umsetzung

Wie im Vorgängerplan: ein frischer Subagent pro Task, strikt nacheinander; nach jedem Task ein zweistufiges Review (Plan-Treue, Code-Qualität); nach jedem Milestone ein Milestone-Review durch einen neuen Subagent, dessen Befunde ein weiterer bewertet und abarbeitet. Befunde, die dem Plan widersprechen, gehen an den Nutzer. Genau ein Commit pro Task. Der Auftrag an einen Subagent ist der Task-Abschnitt samt Files- und Interfaces-Block, dazu die *Global Constraints*, die Tabelle *Entscheidungen* (E1–E8) und *Risiken* (R1–R6).

**Subagenten können in diesem Repo nicht committen:** die Commits sind GPG-signiert und pinentry findet im Subagent-Kontext kein Terminal. Subagenten stagen, die Hauptsession committet.

---

### Task 1: Der Import-Lock hält eine eigene Verbindung

`db.ImportLock` leiht sich heute eine Verbindung aus dem Pool und hält sie über den ganzen Importlauf. `server.Run` schließt den Pool am Ende mit `defer pool.Close()`, und puddle wartet dabei auf jede ausgeliehene Verbindung — auch auf die eines Imports, den das Zehn-Sekunden-Budget bereits aufgegeben hat. Eine eigene Verbindung löst das, weil der Pool sie nicht kennt.

Die Freigabe wird dabei einfacher: Postgres verwirft jeden Advisory-Lock einer Session, sobald sie endet. Wer seine eigene Verbindung schließt, braucht kein `pg_advisory_unlock` und auch kein `Hijack` für den Fall, dass es scheitert.

**Files:**
- Modify: `internal/db/lock.go` (komplett ersetzt)
- Modify: `internal/db/lock_test.go` (bestehender Test auf den neuen Typ, + neuer Test)
- Modify: `internal/server/server.go` (ein Feld im Pipeline-Literal)
- Modify: `internal/server/import.go` (eine Zeile)
- Modify: `docs/superpowers/plans/2026-09-21-cli-backlog.md` (Backlog-Eintrag als erledigt markieren)

**Interfaces:**
- Consumes: `pipeline.ImportLock` (Port, unverändert), `testdb.NewDatabase(t, name) string`, `db.Connect(ctx, dsn) (*pgxpool.Pool, error)`
- Produces: `type db.ImportLock struct { DSN string }` mit unveränderter Methode `TryAcquire(ctx context.Context) (release func(), ok bool, err error)`. **Das Feld heißt jetzt `DSN` statt `Pool`**; beide Aufrufer werden mit umgestellt.

- [ ] **Step 1: Failing Test schreiben** (an `internal/db/lock_test.go` anhängen)

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

- [ ] **Step 2: Test laufen lassen, er muss fehlschlagen**

Run: `go test -count=1 -run TestImportLock_HoldsNoPoolConnection ./internal/db/`
Expected: FAIL, `unknown field DSN in struct literal of type db.ImportLock`

- [ ] **Step 3: `internal/db/lock.go` ersetzen**

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

- [ ] **Step 4: Den bestehenden Lock-Test umstellen** (`internal/db/lock_test.go`)

`TestImportLock_SecondHolderIsRefusedUntilTheFirstReleases` baut heute zwei Pools, weil der Lock einen Pool brauchte. Er braucht jetzt keinen mehr: zwei `ImportLock`-Werte mit demselben DSN sind zwei Sessions. Die beiden `db.Connect`-Aufrufe und ihre `defer …Close()` entfallen, und die drei `TryAcquire`-Aufrufe werden zu

```go
	release, ok, err := (db.ImportLock{DSN: dsn}).TryAcquire(ctx)
```

beziehungsweise für den zweiten Halter

```go
	if _, ok, err := (db.ImportLock{DSN: dsn}).TryAcquire(ctx); err != nil {
```

Der Kommentar über dem Test spricht von „two pools" — er muss jetzt von zwei Verbindungen sprechen, sonst beschreibt er einen Aufbau, den es nicht mehr gibt.

- [ ] **Step 5: Die beiden Aufrufer umstellen**

In `internal/server/server.go`, im `pipeline.Pipeline`-Literal:

```go
			Lock: db.ImportLock{DSN: cfg.Database.DSN},
```

In `internal/server/import.go`:

```go
	release, ok, err := (db.ImportLock{DSN: cfg.Database.DSN}).TryAcquire(ctx)
```

- [ ] **Step 6: Tests laufen lassen, sie müssen grün sein**

Run: `go build ./... && go test -count=1 -race ./internal/db/ ./internal/server/ ./internal/pipeline/`
Expected: PASS

- [ ] **Step 7: Den Doc-Kommentar von `Run` berichtigen** (`internal/server/server.go`)

Der Kommentar nennt seit M3 zwei Ausnahmen von der Zusage, dass beim Rückkehren nichts mehr läuft: den aufgegebenen Import (gilt weiter) und das `pool.Close()`, das auf dessen Lock-Verbindung wartet (gilt nicht mehr). Den zweiten Teil entfernen, den ersten wortgleich stehen lassen.

- [ ] **Step 8: Den Backlog-Eintrag im Vorgängerplan als erledigt markieren**

In `docs/superpowers/plans/2026-09-21-cli-backlog.md` den Eintrag „Der Shutdown von `serve` hat keine obere Schranke mehr" mit einem Verweis auf diesen Plan versehen, im Stil des bereits erledigten Backlogs im Plan vom 2026-09-19: ein `> **Erledigt.**`-Absatz über dem Eintrag, der auf `2026-09-22-release-and-cleanup.md` und E3 zeigt und festhält, dass die Ursache (LLM-Timeout) weiterhin offen ist.

- [ ] **Step 9: Commit-Gate und Commit**

```bash
git add internal docs/superpowers/plans/2026-09-21-cli-backlog.md
git commit -m "fix(db): hold the import lock on its own connection" -m "A pooled connection held for the whole run is one pool.Close waits for, so an import the shutdown budget had abandoned could keep serve from returning for up to an LLM_TIMEOUT. The lock opens its own connection now, and closing it is the release: Postgres drops a session's advisory locks when the session ends."
```

---

### Task 2: `env_file` für den `app`-Service

Compose liest `.env` im Projektverzeichnis heute nur für die `${VAR}`-Substitution *innerhalb* der Compose-Datei. In den Container gelangt davon nichts, außer was unter `environment:` noch einmal ausdrücklich aufgeführt ist. Dreizehn Variablen aus `.env.example` sind das nicht.

**Files:**
- Modify: `docker-compose.yml` (`env_file` beim `app`-Service)
- Modify: `README.md` (der Abschnitt, der `cp .env.example .env` erklärt)
- Modify: `docs/superpowers/plans/2026-09-21-cli-backlog.md` (Backlog-Eintrag als erledigt markieren)

**Interfaces:**
- Consumes: —
- Produces: —

- [ ] **Step 1: Den Ist-Zustand festhalten**

```bash
docker compose config | sed -n '/^  app:/,/^  [a-z]/p' | head -40
```

Die Ausgabe in den Bericht aufnehmen: sie zeigt, welche Variablen der Container heute sieht, und ist die Vergleichsgrundlage für Step 4.

- [ ] **Step 2: `env_file` ergänzen** (`docker-compose.yml`)

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

- [ ] **Step 3: Die Reihenfolge prüfen**

```bash
docker compose config | grep -A2 'DB_DSN'
```

Erwartet: der aus `POSTGRES_PASSWORD` zusammengesetzte Wert, nicht ein Wert aus `.env`. Compose gibt `environment:` den Vorrang vor `env_file:`; diese Prüfung belegt es für genau diese Datei.

- [ ] **Step 4: Den Durchgriff belegen**

`.env` ist die Datei des Betreibers. **Existiert sie bereits, wird sie nicht angefasst**; existiert sie nicht, wird sie für die Prüfung angelegt und danach gelöscht:

```bash
test -f .env && echo "vorhanden, wird nicht angefasst" || cp .env.example .env
docker compose -p recipe-reader-e2e run --rm app env | grep -E '^(EXTRACTION_MODE|LOG_LEVEL)='
```

Erwartet: beide Variablen erscheinen mit ihren Werten aus `.env`. Vor dieser Änderung wäre die Ausgabe leer. Wurde `.env` für die Prüfung angelegt, danach wieder entfernen.

- [ ] **Step 5: README**

Im Abschnitt, der `cp .env.example .env` nennt, einen Satz ergänzen: dass jede Variable aus dieser Datei den Container erreicht, dass die zusammengesetzten Werte in `docker-compose.yml` (`DB_DSN`) Vorrang behalten, und dass ein Start ohne `.env` mit den Defaults funktioniert.

- [ ] **Step 6: Den Backlog-Eintrag im Vorgängerplan als erledigt markieren**

Wie Task 1, Step 8, für den Eintrag „`.env.example` und `docker-compose.yml` laufen auseinander", mit Verweis auf diesen Plan und E4.

- [ ] **Step 7: Commit**

Das Commit-Gate betrifft keinen Go-Code, läuft aber trotzdem (Pflicht laut Global Constraints). Das Docker-Gate entfällt: `Dockerfile` und `scripts/` sind unberührt.

```bash
git add docker-compose.yml README.md docs/superpowers/plans/2026-09-21-cli-backlog.md
git commit -m "fix(compose): pass .env into the app container" -m "Compose read .env only for substitution inside the compose file, so thirteen variables from .env.example never reached the app — including LOG_LEVEL and LOG_FORMAT, added one milestone earlier. env_file closes that for every future variable too; the composed values under environment: still win."
```

---

### Task 3: Der Abbruch einer Migration sagt, dass Wiederholen gefahrlos ist

`MigrateWithContext` meldet einen Abbruch als `db: migrate up: context canceled`. Das sagt nicht, was angewandt wurde, und legt nahe, es sei etwas kaputt. Tatsächlich ist der Zustand immer konsistent: golang-migrate bricht zwischen zwei Migrationen ab, die laufende wird immer zu Ende gebracht, und ein erneuter Lauf setzt fort.

**Files:**
- Modify: `internal/db/connect.go` (die beiden `ctx.Err()`-Zweige in `migrateUp`)
- Modify: `internal/db/db_test.go` (Zusicherung auf die Meldung)

**Interfaces:**
- Consumes: —
- Produces: unveränderte Signaturen; nur der Text der Fehlermeldung ändert sich.

- [ ] **Step 1: Failing Test schreiben** (an den bestehenden `TestMigrateWithContext_CancelledContextAppliesNothing` in `internal/db/db_test.go` anhängen, vor dessen schließender Klammer)

```go
	// The operator reads this line and has to decide what to do next. "context
	// canceled" alone reads like damage; the truth is that nothing is half
	// applied and a second run finishes the job.
	if !strings.Contains(err.Error(), "re-running") {
		t.Errorf("MigrateWithContext() error = %q, want it to say that re-running is safe", err)
	}
```

Die Imports von `db_test.go` um `strings` ergänzen, falls es fehlt.

- [ ] **Step 2: Test laufen lassen, er muss fehlschlagen**

Run: `go test -count=1 -run TestMigrateWithContext ./internal/db/`
Expected: FAIL, `want it to say that re-running is safe`

- [ ] **Step 3: Die Meldung ändern** (`internal/db/connect.go`)

Beide `ctx.Err()`-Zweige in `migrateUp` — der vor `open()` und der nach `Up()` — geben heute `fmt.Errorf("db: migrate up: %w", err)` zurück. Beide ersetzen durch:

```go
		return fmt.Errorf("db: migrate up: cancelled, nothing is half applied and re-running continues where it stopped: %w", err)
```

Der Satz gilt für beide Stellen: vor `open()` wurde nichts angewandt, nach `Up()` nur vollständige Migrationen.

- [ ] **Step 4: Tests laufen lassen, sie müssen grün sein**

Run: `go test -count=1 -race ./internal/db/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/db
git commit -m "fix(db): say what a cancelled migration left behind" -m "\"context canceled\" alone reads like damage. golang-migrate stops between two migrations and always finishes the one in flight, so nothing is half applied and a second run continues — which is what the operator needs to know at the moment they read the line."
```

---

### Task 4: `main` unterscheidet Bedienfehler von Laufzeitfehler

`main` loggt jeden Fehler als `slog.Error("fatal", "error", err)`. Ein Tippfehler in der Kommandozeile sieht damit aus wie ein Absturz im Betrieb, obwohl das eine durch Lesen von `--help` behoben wird und das andere nicht. kong liefert Parse-Fehler als `*kong.ParseError`, die Unterscheidung ist also vorhanden und wird nur nicht genutzt.

**Files:**
- Modify: `cmd/recipe-reader/main.go` (`main`)
- Modify: `cmd/recipe-reader/main_test.go` (Tabelle von `TestMain_ExitStatus`)

**Interfaces:**
- Consumes: `run(args []string, opts ...kong.Option) error`
- Produces: unverändertes Exit-Verhalten (0/1); nur die Logzeile unterscheidet sich.

- [ ] **Step 1: Die Tabelle um die erwartete Logzeile erweitern** (`cmd/recipe-reader/main_test.go`)

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

- [ ] **Step 2: Test laufen lassen, er muss fehlschlagen**

Run: `go test -count=1 -run TestMain_ExitStatus ./cmd/recipe-reader/`
Expected: FAIL, `want it to contain "invalid command line"` — heute loggt auch der Parse-Fehler `fatal`.

- [ ] **Step 3: `main` umbauen** (`cmd/recipe-reader/main.go`)

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

- [ ] **Step 4: Tests laufen lassen, sie müssen grün sein**

Run: `go test -count=1 -race ./cmd/recipe-reader/`
Expected: PASS

- [ ] **Step 5: Von Hand nachsehen**

```bash
go run ./cmd/recipe-reader --does-not-exist 2>&1 | tail -2
LOG_FORMAT=json go run ./cmd/recipe-reader --does-not-exist 2>&1 | tail -2
```

Erwartet: `invalid command line` in beiden Formaten. Der zweite Aufruf belegt zugleich, dass die Unterscheidung auch für einen Log-Collector sichtbar ist — allerdings noch im Default-Format, weil ein Parse-Fehler vor `logging.Configure` auftritt (Entscheidung D4 des Vorgängerplans). Das Ergebnis in den Bericht aufnehmen; weicht es davon ab, anhalten und melden.

- [ ] **Step 6: Den Backlog-Eintrag im Vorgängerplan kürzen**

In `docs/superpowers/plans/2026-09-21-cli-backlog.md` im Eintrag „Fehlermeldungen eindeutiger machen" die beiden erledigten Unterpunkte (die Migrationsmeldung und `main`s Sammelzeile) streichen und über dem Eintrag vermerken, dass sie in diesem Plan behoben sind. Der dritte Unterpunkt — kongs Kommandopfad-Präfix — bleibt stehen, weil er ein Nutzerurteil braucht.

- [ ] **Step 7: Commit**

```bash
git add cmd docs/superpowers/plans/2026-09-21-cli-backlog.md
git commit -m "fix(cli): name a bad command line apart from a failed run" -m "Both still exit 1, but one is fixed by reading --help and the other is not. kong hands back a *kong.ParseError, so the distinction was there and merely unused."
```

---

### Task 5: `release-please` einrichten

`release-please` liest die Conventional-Commits-Präfixe seit einem Startpunkt, hält einen Release-PR offen, der Version und Changelog vorschlägt, und legt beim Merge Tag und GitHub-Release an.

**Files:**
- Create: `.github/workflows/release-please.yml`
- Create: `release-please-config.json`
- Create: `.release-please-manifest.json`
- Modify: `README.md` (kurzer Abschnitt „Releases")

**Interfaces:**
- Consumes: —
- Produces: Tags der Form `v<major>.<minor>.<patch>` und GitHub-Releases; Task 6 und Task 7 hängen sich an das `release`-Ereignis.

- [ ] **Step 1: Den Startpunkt bestimmen**

```bash
git log --oneline -1 --format='%H %s' $(git rev-list -1 main --grep='(3/3)')
```

Der Merge-Commit von PR #33 ist der `bootstrap-sha`: alles davor gehört zur Vorgeschichte und soll nicht im ersten Changelog landen (E8, R1). Den vollen SHA notieren.

- [ ] **Step 2: `release-please-config.json` anlegen**

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

- [ ] **Step 3: `.release-please-manifest.json` anlegen**

```json
{
  ".": "0.0.0"
}
```

- [ ] **Step 4: `.github/workflows/release-please.yml` anlegen**

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

- [ ] **Step 5: Die Konfiguration prüfen, ohne sie laufen zu lassen**

```bash
python3 -c "import json;[json.load(open(f)) for f in ['release-please-config.json','.release-please-manifest.json']];print('json ok')"
python3 -c "import yaml;d=yaml.safe_load(open('.github/workflows/release-please.yml'));print(list(d['jobs']))"
```

Erwartet: `json ok` und `['release-please']`. Die CI prüft diesen Task nicht (CI-Ausnahme aus #29, R5), also ist das hier die einzige Syntaxprüfung vor dem Merge.

- [ ] **Step 6: README**

Einen Abschnitt „Releases" ergänzen: dass die Version aus den Commit-Präfixen entsteht, dass ein offener Release-PR die nächste Version zeigt, dass sein Merge Tag und Release erzeugt, und dass die Zählung bei `v0.1.0` beginnt, weil `0.x` unter SemVer Breaking Changes in jedem Minor erlaubt.

- [ ] **Step 7: Commit**

```bash
git add .github/workflows/release-please.yml release-please-config.json .release-please-manifest.json README.md
git commit -m "ci: let release-please propose versions and changelogs" -m "The repository writes conventional commit prefixes anyway, so the changelog falls out of them without extra work. bootstrap-sha starts the history at the merge of PR #33; without it the first changelog would be the whole project."
```

---

### Task 6: Binaries und Prüfsummen ans Release hängen

Das Release aus Task 5 trägt bisher nur den Changelog. Dieser Task hängt die Single-Binary für drei Plattformen und eine `checksums.txt` an.

**Files:**
- Create: `.github/workflows/release-artifacts.yml`
- Modify: `Makefile` (`-trimpath`)
- Modify: `Dockerfile` (`-trimpath`)

**Interfaces:**
- Consumes: das `release`-Ereignis aus Task 5; `var version` in `package main`, gespeist über `-ldflags "-X main.version=…"`
- Produces: Release-Assets `recipe-reader_<tag>_<os>_<arch>` und `checksums.txt`

- [ ] **Step 1: `-trimpath` ergänzen**

Beide Build-Aufrufe bauen heute ohne `-trimpath`, tragen also den absoluten Pfad des Build-Verzeichnisses in die Binary. Solange nur lokal gebaut wurde, war das folgenlos; eine veröffentlichte Binary soll ihn nicht enthalten.

In `Makefile`:

```make
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/recipe-reader ./cmd/recipe-reader
```

In `Dockerfile`:

```dockerfile
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-X main.version=${VERSION}" -o /recipe-reader ./cmd/recipe-reader
```

- [ ] **Step 2: Docker-Gate**

```bash
docker build --build-arg VERSION=smoke -t recipe-reader:ci .
scripts/smoke-test-image.sh recipe-reader:ci smoke
```

Erwartet: `smoke test passed: recipe-reader:ci (smoke)`. `-trimpath` darf die Versionsstempelung nicht stören — genau das prüft der Smoke-Test.

- [ ] **Step 3: `.github/workflows/release-artifacts.yml` anlegen**

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

- [ ] **Step 4: Die Workflow-Syntax prüfen**

```bash
python3 -c "import yaml;d=yaml.safe_load(open('.github/workflows/release-artifacts.yml'));print(list(d['jobs']))"
```

Erwartet: `['binaries', 'upload']`.

- [ ] **Step 5: Commit-Gate und Commit**

```bash
git add .github/workflows/release-artifacts.yml Makefile Dockerfile
git commit -m "ci: attach binaries and checksums to a release" -m "Three platforms, each a single static binary with the frontend embedded, stamped with the release tag so --version reports what was downloaded. -trimpath is added to the Makefile and the Dockerfile too, so a published binary carries no build path."
```

---

### Task 7: Image in die GHCR, Compose darauf umstellen

Ein Betreiber soll das Image ziehen können, statt es zu bauen.

**Files:**
- Modify: `.github/workflows/release-artifacts.yml` (+ Job `image`)
- Modify: `docker-compose.yml` (`image:` beim `app`-Service)
- Modify: `README.md` (Betrieb über das veröffentlichte Image)

**Interfaces:**
- Consumes: das `release`-Ereignis; `Dockerfile` mit `VERSION`-Build-Arg
- Produces: `ghcr.io/<owner>/<repo>:<tag>` und `:latest`

- [ ] **Step 1: Den Image-Job ergänzen** (`.github/workflows/release-artifacts.yml`)

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

- [ ] **Step 2: Compose auf das Image umstellen** (`docker-compose.yml`)

Beim `app`-Service, über dem bestehenden `build:`:

```yaml
    # Operators pull a published image; developers build locally with
    # `docker compose up --build`. With both keys compose uses the image it
    # finds and builds only when asked, so neither way shuts the other out.
    # RECIPE_READER_VERSION pins a release; the default follows the newest.
    image: ghcr.io/sburmester/recipe-reader:${RECIPE_READER_VERSION:-latest}
```

- [ ] **Step 3: Prüfen, dass der lokale Build weiterhin greift**

```bash
VERSION=local docker compose -p recipe-reader-e2e build app
docker compose -p recipe-reader-e2e run --rm app recipe-reader --version
```

Erwartet: `local`. Das belegt, dass `build:` neben `image:` bestehen bleibt und der lokale Weg unverändert funktioniert.

- [ ] **Step 4: README**

Im Betriebsabschnitt ergänzen: `docker compose pull && docker compose up -d` für ein veröffentlichtes Image, `RECIPE_READER_VERSION=v0.1.0` zum Festnageln einer Version, `docker compose up --build` für die Entwicklung. Dazu der Hinweis aus R3, dass das GHCR-Paket nach dem ersten Push privat ist und in den Repository-Einstellungen öffentlich gestellt werden kann.

- [ ] **Step 5: Docker-Gate und Commit**

```bash
docker build --build-arg VERSION=smoke -t recipe-reader:ci .
scripts/smoke-test-image.sh recipe-reader:ci smoke
git add .github/workflows/release-artifacts.yml docker-compose.yml README.md
git commit -m "ci: publish the image to ghcr and let compose pull it" -m "compose keeps build: next to image:, so an operator pulls a release and a developer still builds locally. The package is private until someone makes it public, which is the right default for a single-user deployment."
```

---

### Abschluss-Verifikation

- [ ] **Step 1: Commit-Gate komplett** (siehe Global Constraints). Alles grün.

- [ ] **Step 2: Docker-Gate** (siehe Global Constraints). `smoke test passed`.

- [ ] **Step 3: Den Release-PR lesen, nicht bestätigen**

Nach dem Merge von M4 öffnet `release-please` einen PR. Vor dem Merge prüfen:

- Die vorgeschlagene Version ist `v0.1.0` (E2).
- Das Changelog beginnt beim `bootstrap-sha` und enthält nicht die gesamte Projekthistorie (R1).
- Die Einträge stimmen mit den Commits der vier Milestones überein.

Weicht etwas ab, hier anhalten und dem Nutzer vorlegen.

- [ ] **Step 4: Das Release erzeugen und prüfen**

Nach dem Merge des Release-PRs:

```bash
gh release view v0.1.0
gh run list --workflow=release-artifacts.yml --limit 1
```

Erwartet: das Release existiert, der Workflow ist grün, und das Release trägt drei Binaries plus `checksums.txt`.

- [ ] **Step 5: Eine Binary gegenprüfen**

```bash
gh release download v0.1.0 --pattern 'recipe-reader_v0.1.0_linux_amd64' --dir /tmp/rr-check
gh release download v0.1.0 --pattern 'checksums.txt' --dir /tmp/rr-check
cd /tmp/rr-check && sha256sum -c --ignore-missing checksums.txt
chmod +x recipe-reader_v0.1.0_linux_amd64 && ./recipe-reader_v0.1.0_linux_amd64 --version
```

Erwartet: `OK` aus der Prüfsummenkontrolle und `v0.1.0` als Versionsausgabe.

- [ ] **Step 6: Das Image gegenprüfen**

```bash
docker pull ghcr.io/sburmester/recipe-reader:v0.1.0
scripts/smoke-test-image.sh ghcr.io/sburmester/recipe-reader:v0.1.0 v0.1.0
```

Erwartet: `smoke test passed: ghcr.io/sburmester/recipe-reader:v0.1.0 (v0.1.0)`. Das ist zugleich der Beleg, dass die Versionsstempelung durch den Build-Arg-Weg korrekt ankommt.

- [ ] **Step 7: Compose gegen das veröffentlichte Image**

```bash
export API_TOKEN=$(openssl rand -hex 32) POSTGRES_PASSWORD=$(openssl rand -hex 16) RECIPE_READER_VERSION=v0.1.0
docker compose -p recipe-reader-e2e pull app
docker compose -p recipe-reader-e2e up -d --wait
docker compose -p recipe-reader-e2e exec app recipe-reader --version
docker compose -p recipe-reader-e2e down -v
```

Erwartet: `pull` lädt das Image, der Stack wird `healthy`, und `--version` meldet `v0.1.0` — ohne lokalen Build. `down -v` auch dann ausführen, wenn ein Schritt davor scheitert; nie ohne `-p recipe-reader-e2e`, weil das Default-Projekt das Volume `db-data` mit echten Daten hält.

- [ ] **Step 8: Akzeptanzkriterien in Teil A abhaken** (US1–US5, Definition of Done)

- [ ] **Step 9: Diese Datei abhaken und committen**

```bash
git add docs/superpowers/plans/
git commit -m "docs: mark M4 done in the release plan"
```

- [ ] **Step 10: PR 4/4** erst nach Freigabe durch den Nutzer öffnen.

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
