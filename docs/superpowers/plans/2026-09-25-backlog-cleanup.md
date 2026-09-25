# Backlog-Abbau: Abbruch statt Fallback, ein Index weniger, ein arm64-Image und Provenance für alles, was ein Release trägt

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Die sechs Backlog-Einträge aus `AGENTS.md` („Backlog, deliberately not scheduled") abarbeiten: einen Eintrag, dessen Prämisse sich beim Nachmessen als falsch erwies, auf die kleine echte Lücke dahinter zurückführen; den redundanten Index entfernen; das Image für `linux/arm64` veröffentlichen; Binaries und Image mit Build-Provenance versehen; und die zwei Einträge, die kein Code sind, entscheiden beziehungsweise begründet schließen.

**Architecture:** Zwei kleine Korrekturen am Go-Code gehen voraus (M1 Abbruch, M2 Migration `0004`), danach folgen zwei Änderungen an der Release-Pipeline, die aufeinander aufbauen (M3 Multi-Arch, M4 Attestation), zuletzt die Entscheidung zum kong-Präfix (M5). Multi-Arch läuft auf nativen ARM-Runnern statt unter Emulation: je Architektur ein Image, gebaut und smoke-getestet auf einem Runner der eigenen Architektur, danach ein Manifest, das beide unter dem Release-Tag vereint. Die Attestation setzt auf dieses Manifest auf, deshalb kommt sie danach.

**Tech Stack:** Go 1.27.1, pgx/v5, goose v3, sqlc (über `go tool`), GitHub Actions (`ubuntu-24.04-arm`, `actions/attest@v4`), Docker Buildx `imagetools` (auf den Runnern vorinstalliert), `gh` ≥ 2.101 für die Prüfbefehle. **Keine neue Go-Abhängigkeit.** Genau eine neue GitHub Action (`actions/attest`, Begründung unter E4).

## Global Constraints

- **Branch:** Nie auf `main` arbeiten. Jeder Milestone hat einen eigenen Branch und einen eigenen PR; die Namen stehen unter „Branches und PRs". Jeder Branch zweigt von `main` ab, nachdem der vorige PR gemerged ist.
- **PR-Titel und Commit-Betreff** folgen Conventional Commits, und **das Präfix bestimmt die Version** (release-please, Squash-Merge). `fix:` → Patch, `feat:` → Minor (unter 1.0), `perf`/`ci`/`docs`/`refactor`/`chore` lösen kein Release aus. Ein falsches Präfix ist eine falsche Version; der Release-PR wird vor dem Merge gelesen.
- **Commit und Push nur auf Anfrage** (`AGENTS.md`). Die `git commit`-Beispiele in den Steps zeigen den Footer nicht; er ist trotzdem Pflicht:
  ```
  Assisted-by: <Modellname> (<Effort>) via Claude Code
  Co-Authored-By: <Modellname> <noreply@anthropic.com>
  ```
- **Commit-Gate (vor jedem Commit, in dieser Reihenfolge; Docker muss laufen):**
  ```bash
  go fix ./...
  gofmt -w .
  go vet ./...
  ~/.local/bin/golangci-lint run ./...
  go run golang.org/x/vuln/cmd/govulncheck@latest ./...
  go test -count=1 -race ./...
  ```
  `go fix` und `gofmt` schreiben Dateien: den Diff lesen. Jeder Befund wird vor dem Commit behoben, kein `//nolint`, keine gelockerte `.golangci.yml`. Bekannt und nicht zu beheben: GO-2026-5932 (`golang.org/x/crypto/openpgp`, vom Code nicht aufgerufen, keine Fix-Version).
- **Docker-Gate** nur für Tasks, die `Dockerfile` oder `scripts/` ändern. Dieser Plan ändert beides **nicht**, deshalb entfällt es; den Ersatz für die Image-Prüfung nennt M3 (der CI-Lauf auf beiden Architekturen).
- **CI-Ausnahme:** Die CI überspringt Läufe, bei denen jede geänderte Datei auf `**.md`, `docs/**` oder `.github/workflows/**` passt. M3 und M4 ändern nur Workflows und Doku, laufen also **nicht** von selbst. Sie werden mit `actionlint` geprüft und über `gh workflow run ci.yml --ref <branch>` von Hand angestoßen; die Release-Pipeline selbst wird erst von einem echten Release geprüft.
- **Gepinnte Fremdabhängigkeiten:** Jede Action steht mit Digest da, mit dem Tag als Kommentar (`uses: actions/attest@<sha> # v4`). Eine neu hinzugefügte Action wird sofort mit gepinnt; Renovate hebt die Digests danach. Jede zusätzliche Action braucht eine Zeile Begründung.
- **Migrationen:** Eine Datei `internal/db/migrations/000N_<name>.sql` mit `-- +goose Up` und `-- +goose Down`, ohne `BEGIN;`/`COMMIT;`. Nach einer Migration: `make sqlc-generate`, und `git diff --exit-code internal/db/sqlc` bleibt leer.
- **Tests:** Jeder Test für einen Fix wird **gezeigt rot**, bevor der Fix ihn grün macht. DB-Tests rufen kein `t.Parallel()`. Coverage wird nicht gegated.
- **Kommentare** erklären das *Warum*, nicht das *Was*, im Ton des Repos; Zeilen bis ~100 Spalten. Code, Kommentare, README und Commit-Messages sind englisch, dieser Plan ist deutsch.
- **Dokumentation im selben Change wie der Code:** `README.md`, `AGENTS.md` und der Backlog-Eintrag, den der Milestone schließt.
- **Fortschritt:** Erledigte Steps und Tasks werden **in dieser Datei** abgehakt (`[x]`); jeder Milestone-PR endet mit einem Commit `docs: mark M<n> done in the backlog plan`.

---

## Teil A: Product-Owner-Sicht

### Ausgangslage

`AGENTS.md` führt unter „Backlog, deliberately not scheduled" sechs Einträge, der Release-Plan vom 2026-09-22 führt fünf davon mit. Bevor daraus ein Plan wurde, ist jeder gegen den Code und gegen das echte Release `v0.2.0` geprüft worden. **Zwei Prämissen tragen nicht mehr**, und das ändert den Zuschnitt:

| # | Backlog-Eintrag | Befund der Prüfung (2026-09-25) | Folge |
| --- | --- | --- | --- |
| 1 | Ein aufgegebener Import endet erst nach `LLM_TIMEOUT`; die Ursache sei, dass der Abbruch nicht bis in den Extraktionspfad durchgereicht wird | **Prämisse widerlegt.** `LLMExtractor.Extract` leitet seinen Timeout vom Kontext des Aufrufers ab, und beide SDKs bauen Request und Retry-Wartezeit auf ihm auf. Gemessen gegen einen Server, der nie antwortet, bei `Timeout: 1h`: beide Provider kehren nach **~50 ms** mit `context.Canceled` zurück. **Die echte Lücke ist eine andere:** `HybridExtractor` behandelt den Abbruch als LLM-Ausfall und fällt auf das Regel-Ergebnis zurück (`Degraded`, kein Fehler), die Pipeline versucht es auf einem toten Kontext zu speichern und zählt den Post als `failed`. Ein Ctrl-C bei `recipe-reader import` meldet also einen Fehlschlag, der keiner war, und der Lauf stoppt erst an der nächsten Prüfung. | M1 |
| 2 | kongs Kommandopfad-Präfix (`serve: config: API_TOKEN …`) braucht ein Nutzerurteil | Das Präfix `serve:` setzt kong fest ein (`context.go:232`: Fehler eines Validators bekommen den Pfad des Knotens vorangestellt), es gibt keine Option dafür. `config:` ist unser eigenes und steht an fünf Stellen in `config.go`. Nur Fehler aus `Validate()`-Methoden tragen das Präfix; ein Enum-Fehler wie `--log-level` trägt keines. Kein Test hängt an der Form. | **Entscheidung O1**, M5 |
| 3 | Multi-Arch-Image (`linux/arm64`) | Alle Basis-Images (`node`, `golang`, `alpine` im Dockerfile, `postgres` im Smoke-Test und in Compose) sind Multi-Arch-Indizes mit `linux/arm64`, und die gepinnten Digests sind die Index-Digests: **das Dockerfile braucht keine Änderung.** Die arm64-Binaries gibt es schon. Native ARM-Runner (`ubuntu-24.04-arm`) sind für öffentliche Repositories kostenlos; das Repository ist öffentlich. | M3 |
| 4 | Signatur und Provenance der Release-Artefakte | **Teilweise schon da.** Das Release ist unveränderlich, und GitHub attestiert solche Releases von selbst: `gh release verify v0.2.0` meldet ✓ und listet alle vier Assets mit Digest, `gh release verify-asset` bestätigt die Binary. **Es fehlt** die *Build-Provenance* (welcher Workflow, welcher Commit hat das gebaut): `gh attestation verify` liefert für Binary **und** Image HTTP 404. Das README sagt außerdem noch „The binaries are not signed", was so nicht mehr stimmt. Attestations sind Sigstore-signiert, ein eigener Schlüssel oder `cosign` ist nicht nötig. | M4 |
| 5 | Migrationen mit goose' No-Transaction-Modus | Auslöserbasiert: nötig erst, wenn eine Migration `CREATE INDEX CONCURRENTLY` braucht. Keine tut es. Beim Prüfen von M2 fiel ein echter Stolperstein auf: **goose liest jede Kommentarzeile, die die Annotation enthält, als Direktive, auch mitten im Satz, und lehnt die Datei ab.** | Kein Task; M2 entscheidet den ersten Kandidaten und hält die Warnung fest |
| 6 | Der Index `idx_recipe_ingredients_recipe_id` ist redundant | Bestätigt. `0003` legt `UNIQUE (recipe_id, position)` an, dessen Index mit `recipe_id` beginnt; die zwei Lesezugriffe in `recipes.sql` filtern auf `recipe_id` und sortieren nach `position`, das Ersetzen der Zutaten löscht über `recipe_id`. | M2 |

### Ziele (Outcomes)

| # | Ziel | Messbar an |
| --- | --- | --- |
| Z1 | Ein abgebrochener Import meldet keinen Fehler, den es nicht gab | `Run` gibt bei Abbruch mitten im Post `context.Canceled` zurück, der Post steht in keinem Zähler (zwei Tests, rot vor dem Fix) |
| Z2 | Der Backlog-Eintrag zum Abbruch ist mit Beleg geschlossen | Ein Test hält fest, dass der Abbruch beide SDKs sofort erreicht; `AGENTS.md` führt den Eintrag nicht mehr |
| Z3 | Schreibzugriffe auf `recipe_ingredients` pflegen einen Index weniger, ohne dass eine Abfrage ihren Index verliert | Migration `0004`; ein Test prüft den Plan der Abfrage aus `recipes.sql` |
| Z4 | Das Image läuft auf `linux/arm64` | `docker manifest inspect ghcr.io/sburmester/recipe-reader:<tag>` nennt `linux/amd64` **und** `linux/arm64`; beide bestanden den Smoke-Test auf einem Runner der eigenen Architektur |
| Z5 | Jedes Artefakt eines Release hat prüfbare Build-Provenance | `gh attestation verify` ist ✓ für alle drei Binaries und für `oci://ghcr.io/sburmester/recipe-reader:<tag>` |
| Z6 | Der Backlog in `AGENTS.md` enthält nur noch, was wirklich offen ist | Nach dem Plan bleiben dort nur T-43 und T-24 (Zugang) |

### Nicht-Ziele

- **Keine Emulation.** Kein `docker/setup-qemu-action`, keine zusätzliche Action dafür (E1).
- **Kein `cosign`, kein SLSA-Generator, keine SBOM.** Die Attestation von `actions/attest` reicht für „woher kommt das und ist es unverändert"; eine SBOM beantwortet eine andere Frage und ist nicht angefragt.
- **Kein `linux/arm` und kein `darwin/amd64`** im Image beziehungsweise bei den Binaries. Es bleibt bei den vier Zielen, die es gibt.
- **Keine Änderung an T-24 und T-43.** Beide brauchen Zugang und stehen weiter unter „blocked on access".
- **Keine Neuklassifizierung von Fehlern.** Beobachtet, aber nicht Teil dieses Plans: `serve --extraction-mode llm` ohne Key und `import` ohne Konto enden als `fatal`, andere Konfigurationsfehler als `invalid command line`, und eine abgelehnte Kommandozeile wird im Default-Logformat statt im eingestellten geschrieben (Entscheidung D4 des Plans vom 2026-09-21). Wer das ändern will, macht daraus einen eigenen Eintrag.
- **Keine Änderung am HTTP-API, am Schema außer `0004`, an der Extraktion außer dem Abbruchpfad.**

### Entscheidungen

| # | Entscheidung | Begründung |
| --- | --- | --- |
| E1 | **Native ARM-Runner statt QEMU.** Die `image`-Matrix baut und testet je Architektur auf einem Runner der eigenen Architektur | Keine neue Action, und der Smoke-Test startet das Image, das ausgeliefert wird, nicht eine emulierte Kopie. Kostenlos für öffentliche Repositories. Der Preis: der Workflow hängt an einem Runner-Label (`ubuntu-24.04-arm`), das GitHub umbenennen könnte (R2). |
| E2 | Je Architektur ein Image unter einem **suffixierten Tag** (`<tag>-amd64`, `<tag>-arm64`); ein zweiter Job vereint sie mit `docker buildx imagetools create` unter `<tag>` und `latest`. **Die suffixierten Tags bleiben.** | Der Smoke-Test muss vor dem Push laufen (heutige Reihenfolge: nichts wird gepusht, was ihn nicht bestand). Die Manifest-Liste verweist auf die Manifeste der suffixierten Tags: sie aus der Registry zu löschen nähme dem Tag, was er ausliefert. Die zwei Tags sind der Preis. |
| E3 | Die CI baut und testet das Image **auch auf arm64** | Freie Runner, ein paar Minuten. Ein Defekt, der nur arm64 trifft, fiele sonst zum ersten Mal in einem Release-Lauf auf, und das Release bliebe Entwurf. Es ist zugleich die Vorabprüfung von M3: der riskante Teil (das Image läuft auf ARM) wird vor dem Merge gezeigt. |
| E4 | **`actions/attest`** (v4) für Binaries und Image | Eine Zeile Begründung, wie verlangt: es ist die erstparteiliche Action von GitHub, signiert schlüssellos über den OIDC-Token des Workflows (Sigstore) und schreibt in den Attestation-Speicher, den `gh attestation verify` liest. Ein eigener Schlüssel, `cosign` oder ein Drittanbieter-Generator wären mehr Bewegliches für dasselbe Ergebnis. Öffentliche Repositories brauchen dafür keinen bezahlten Plan. |
| E5 | Die Binaries werden **im Job attestiert, der sie baut** (`binaries`), nicht im `publish`-Job, der sie nur einsammelt | Die Attestation nennt den Job, der das Artefakt erzeugt hat. Ein Job, der es danach nur angefasst hat, wäre eine schwächere Aussage. |
| E6 | Das Image wird über den **Digest der Manifest-Liste** attestiert (einmal), nicht je Architektur | `gh attestation verify oci://<image>:<tag>` prüft den Digest, auf den das Tag zeigt, und das ist die Liste. Eine Attestation je Plattform-Manifest wäre doppelte Arbeit für einen Fall (Pull per Plattform-Digest), den niemand hat. |
| E7 | Ein fehlschlagender Attest-Schritt **blockiert** die Veröffentlichung (`publish` braucht beide Jobs) | Provenance, die stillschweigend fehlen darf, ist keine. Das Release bleibt dann Entwurf und wird wie bisher mit `gh workflow run release-artifacts.yml -f tag=…` beendet. |
| E8 | Migration `0004` nutzt ein einfaches `DROP INDEX`, nicht `CONCURRENTLY` | Die Tabelle hält die Zeilen einer einzelnen Person, die Sperre dauert Millisekunden. `CONCURRENTLY` bräuchte goose' No-Transaction-Modus und brächte hier nichts. Damit ist der einzige denkbare Kandidat für den Backlog-Eintrag 5 geprüft und verworfen. |
| E9 | **kongs Präfix bleibt** und die Form wird im README festgehalten (Entscheidung O1, Nutzer, 2026-09-25) | Das Präfix sagt, welcher Befehl gewählt wurde, auch wenn `serve` der Default war und der Betreiber nichts getippt hat. Es zu entfernen ginge nur über String-Nachbearbeitung der Fehlermeldung oder einen Umbau der Validierung, den E7/E13 des kong-Plans (kong validiert nur die Gruppen auf dem Pfad) ausdrücklich vermeiden. Verworfen wurden auch die leichte Variante, nur unser redundantes `config:` zu streichen, und die Nachbearbeitung in `main`. |
| E10 | Die PRs von M3 und M4 tragen das Präfix **`feat(ci)`** (Entscheidung O2, Nutzer, 2026-09-25) | Ausnahmsweise die bessere Wahl für ein CI-Thema: beide Änderungen sind erst mit einem echten Release prüfbar, und `feat` sorgt dafür, dass nach dem Merge ein Release-PR entsteht. Beide sind für Betreiber neu (arm64-Image, prüfbare Provenance) und gehören in den Changelog. Unter `0.x` kostet der Minor-Bump nichts. |

### Entscheidungen des Nutzers

Am 2026-09-25 entschieden; beide Fragen sind damit nicht mehr offen.

| # | Frage | Entscheidung |
| --- | --- | --- |
| **O1** | Wie sollen Validierungsfehler aussehen? Heute: `serve: config: IMPORT_INTERVAL must be positive, got 0s` | **Beibehalten** und im README festhalten (nur Doku). Nicht gewählt: kongs Präfix entfernen (fragile String-Nachbearbeitung in `main`), nur unser `config:` streichen. |
| **O2** | Präfix der PRs von M3 und M4 | **`feat(ci)`**: Minor-Release, der neue Lieferumfang erscheint im Changelog und lässt sich mit dem entstehenden Release-PR prüfen. |

### Risiken

| # | Risiko | Gegenmaßnahme |
| --- | --- | --- |
| R1 | Die Release-Pipeline wird von keinem CI-Lauf geprüft (CI-Ausnahme); ein Fehler zeigt sich erst im echten Release | `actionlint` vor dem Merge. Der riskanteste Teil von M3, das Image auf ARM, wird vorab über die CI-Matrix gezeigt (E3). Bei einem Fehlschlag bleibt das Release ein **Entwurf** (`publish` läuft nicht, E7), und der bestehende Weg (`gh workflow run release-artifacts.yml -f tag=…`) beendet es nach der Korrektur. Nichts Öffentliches ist dann kaputt. |
| R2 | Das Runner-Label `ubuntu-24.04-arm` wird umbenannt oder abgeschaltet | Die GitHub-Dokumentation nennt es am 2026-09-25 neben `ubuntu-22.04-arm` und `ubuntu-26.04-arm`. Der erste Lauf, der es nicht findet, bleibt in der Warteschlange und scheitert am `timeout-minutes`; das Label steht an genau zwei Stellen (Release-Workflow, CI). |
| R3 | In einem wiederverwendbaren Workflow dürfen die Jobs nicht mehr Rechte anfordern, als der aufrufende Job hält | `release-please.yml` (Job `artifacts`) bekommt die drei neuen Rechte in M4 mit; dort steht warum. Ohne sie lehnt GitHub den Aufruf ab, und die Release-Pipeline startet gar nicht. Von `actionlint` **nicht** erkannt, deshalb ein eigener Step. |
| R4 | Ein Rerun (`workflow_dispatch`) überschreibt die suffixierten Tags | Gewollt und harmlos: gleicher Tag, neu gebautes Image desselben Commits. `latest` bewegt ein Rerun nicht (bestehende Regel). |
| R5 | Ein Rerun attestiert die Binaries erneut | Für einen Entwurf, dessen Lauf scheiterte, richtig: `gh release upload --clobber` ersetzt die Assets, und die Attestation gilt für die Bytes, die dann im Release liegen. Ein veröffentlichtes Release kann ohnehin keine Assets mehr aufnehmen. |
| R6 | Die zwei Tags `<tag>-amd64`/`<tag>-arm64` bleiben in der Registry stehen | E2. Im README erwähnt, damit niemand sie für Überbleibsel hält und löscht. |
| R7 | goose lehnt eine Migration ab, weil ein Kommentar die Annotation enthält | In M2 gefunden und dokumentiert (README, `AGENTS.md`). `TestMigrations_UpDownUp` und der `0004`-Test lesen die Datei durch goose und scheitern sofort. |
| R8 | Ein falsches PR-Präfix erzeugt die falsche Version | O2 ist entschieden (`feat(ci)`, E10); der Release-PR wird vor dem Merge gelesen und zeigt die vorgeschlagene Version. M1 ist ein `fix`, M2 ein `perf` (kein Release), M3 und M4 sind `feat`: mit M3 wird aus `0.2.1` ein `0.3.0`, mit M4 ein `0.4.0`, wenn dazwischen ein Release entsteht. |

### Definition of Done

- [ ] Alle Tasks in dieser Datei abgehakt.
- [ ] Commit-Gate grün für jeden Commit mit Go-Änderung (M1, M2).
- [ ] `actionlint` sauber für jeden Commit mit Workflow-Änderung (M3, M4).
- [ ] Ein echtes Release nach M3 trägt ein Image mit beiden Plattformen; ein echtes Release nach M4 besteht alle Prüfbefehle aus Task 4, Step 13.
- [ ] Der Abschnitt „Backlog, deliberately not scheduled" in `AGENTS.md` ist leer, oder er nennt nur, was dieser Plan ausdrücklich offen ließ.
- [ ] Jeder geschlossene Eintrag im Release-Plan vom 2026-09-22 trägt einen `> **Erledigt.**`-Absatz mit Verweis auf diesen Plan.

### Was nach diesem Plan bleibt

- **T-43** und **T-24**, blockiert durch Zugang (`docs/reviews/2026-09-11-working-plan.md`, „Blocked on external access").
- Die unter „Nicht-Ziele" beobachtete Uneinheitlichkeit der Fehlerklassen und des Logformats bei abgelehnten Kommandozeilen, falls du sie als Eintrag willst.

---

## Teil B: Zielbild

### Dateistruktur nach Abschluss

```
internal/extraction/
  hybrid.go                          # M1: ein Abbruch des Aufrufers ist kein LLM-Ausfall
  hybrid_test.go                     # M1: + Abbruch, + Timeout-Wächter
  llm_extract_test.go                # M1: + Abbruch erreicht beide SDKs
internal/pipeline/
  pipeline.go                        # M1: interrupted(); ImportResult-Kommentar
  pipeline_test.go                   # M1: + zwei Tests
internal/db/
  migrations/0004_drop_redundant_recipe_ingredients_index.sql   # M2: neu
  migrations_test.go                 # M2: + Test für 0004 und zwei Helfer
.github/workflows/
  ci.yml                             # M3: docker-Job auf amd64 und arm64
  release-artifacts.yml              # M3: image-Matrix + manifest-Job; M4: Attestationen
  release-please.yml                 # M4: Rechte des aufrufenden Jobs
README.md                            # M2, M3, M4, M5
AGENTS.md                            # jeder Milestone
docs/superpowers/plans/2026-09-22-release-and-cleanup.md   # Backlog-Einträge als erledigt markiert
docs/reviews/2026-09-11-working-plan.md                    # M2: T-55-Notiz
```

`Dockerfile`, `scripts/smoke-test-image.sh` und `docker-compose.yml` bleiben **unverändert**. Compose löst `ghcr.io/sburmester/recipe-reader:<tag>` auf einer ARM-Maschine von selbst zur passenden Plattform der Manifest-Liste auf.

---

## Teil C: Aufgaben

### Milestones

| Milestone | Tasks | Ergebnis | Für Betreiber sichtbar |
| --- | --- | --- | --- |
| **M1:** Abbruch statt Fallback | Task 1 | Ein Ctrl-C meldet keinen Fehlschlag mehr; der Backlog-Eintrag ist mit Beleg geschlossen | ja (Zähler und Log beim Abbruch) |
| **M2:** Ein Index weniger | Task 2 | Migration `0004`; der Eintrag zum No-Transaction-Modus begründet geschlossen | nein |
| **M3:** arm64-Image | Task 3 | Das Image trägt beide Plattformen | ja |
| **M4:** Provenance | Task 4 | Binaries und Image sind mit `gh attestation verify` prüfbar | ja |
| **M5:** kong-Präfix | Task 5 | Die Entscheidung O1 (Präfix bleibt) ist im README und in `AGENTS.md` festgehalten | nein (nur Doku) |

Reihenfolge: M1 → M2 → M3 → M4 → M5. M1, M2 und M5 sind voneinander unabhängig; M4 setzt M3 voraus (das Manifest, das M4 attestiert, entsteht in M3).

#### Branches und PRs

| Milestone | Branch | PR-Titel |
| --- | --- | --- |
| M1 | `fix/cancellation-is-not-a-fallback` | `fix(pipeline): count a cancelled import post as cancelled, not failed (1/5)` |
| M2 | `perf/drop-redundant-ingredients-index` | `perf(db): drop the redundant recipe_ingredients index (2/5)` |
| M3 | `ci/arm64-image` | `feat(ci): publish a linux/arm64 image (3/5)` |
| M4 | `ci/attest-release-artifacts` | `feat(ci): attest the release binaries and image (4/5)` |
| M5 | `docs/kong-error-prefix` | `docs: record the decision on kong's error prefix (5/5)` |

Ein PR enthält die Task-Commits seines Milestones, die Korrekturen aus dem Milestone-Review und einen letzten Commit `docs: mark M<n> done in the backlog plan`. Beschreibung: Ergebnis, abgehakte Abnahmekriterien, Link auf diesen Plan, `Assisted-by`-Footer und `🤖 Generated with [Claude Code](https://claude.com/claude-code)`.

### Aufgabenliste

- [ ] **Vorbereitung:** Diesen Plan und das Abhaken der Task-Dateien vom 2026-09-25 über einen eigenen `docs:`-PR auf `main` bringen, damit jeder Milestone-Branch ihn schon enthält
- [ ] **M1: Abbruch statt Fallback**
  - [ ] **Task 1:** Ein Abbruch ist kein LLM-Ausfall und kein fehlgeschlagener Post (M)
  - [ ] **Milestone-Review**
- [ ] **M2: Ein Index weniger**
  - [ ] **Task 2:** Migration `0004` (S)
  - [ ] **Milestone-Review**
- [ ] **M3: arm64-Image**
  - [ ] **Task 3:** Image je Architektur, Manifest-Liste, CI auf beiden (M)
  - [ ] **Milestone-Review**
- [ ] **M4: Provenance**
  - [ ] **Task 4:** Attestation für Binaries und Image (M)
  - [ ] **Milestone-Review**
- [ ] **M5: kong-Präfix**
  - [ ] **Task 5:** Entscheidung O1 umsetzen (S)
  - [ ] **Milestone-Review**
- [ ] **Abschluss-Verifikation**

### Ablauf der Umsetzung

Wie in den Vorgängerplänen: ein frischer Subagent pro Task, strikt nacheinander; nach jedem Task ein zweistufiges Review (Plan-Treue, Code-Qualität); nach jedem Milestone ein Milestone-Review durch einen neuen Subagent, dessen Befunde ein weiterer bewertet und abarbeitet. Befunde, die dem Plan widersprechen, gehen an den Nutzer. Genau ein Commit pro Task. **Subagenten können in diesem Repo nicht committen** (GPG-signiert, pinentry findet dort kein Terminal): sie stagen, die Hauptsession committet.

**Push, Workflow-Läufe und das Mergen eines Release-PRs sind Handlungen nach außen** und brauchen jeweils die ausdrückliche Freigabe des Nutzers; sie stehen in den Steps als solche.

---

### Vorbereitung: Plan und Abhaken auf `main` bringen

Der Branch `docs/agents-md` ist gemerged (PR #64) und bekommt nichts mehr. Das lokale `main` war beim Schreiben dieses Plans veraltet und enthielt `AGENTS.md` noch nicht; nach einem `fetch` liegt es auf `origin/main`. Ungespeichert im Arbeitsverzeichnis liegen dieser Plan und das Abhaken der Task-Dateien vom 2026-09-25 (161 Checkboxen in `2026-09-05-recipe-reader-implementation.md` und `2026-09-05-recipe-reader-tasks/25-provider-agnostic-llm-extractor.md`).

- [ ] **Step 1: Neuen Branch von `origin/main` anlegen**

```bash
git fetch origin && git switch -c docs/backlog-cleanup-plan origin/main
```

Ungespeicherte Änderungen wandern mit, solange `origin/main` diese drei Dateien seit dem Abzweig nicht verändert hat. Verweigert `git switch` das, die Änderungen mit `git stash` beiseitelegen, wechseln und `git stash pop`.

- [ ] **Step 2: Prüfen, dass genau das im Arbeitsverzeichnis liegt**

Run: `git status --short && git diff --stat`
Expected: der Plan als neue Datei, die beiden anderen Dateien geändert, `161 insertions(+), 161 deletions(-)` (nur Checkbox-Zeichen).

- [ ] **Step 3: Commit, Push, PR, Merge** *(Freigabe des Nutzers)*

Alle geänderten Dateien liegen unter `docs/`, die CI überspringt den Lauf (CI-Ausnahme); es gibt nichts abzuwarten.

```bash
git add docs
git commit -m "docs: plan the backlog cleanup and tick the finished task steps" -m "Checks each backlog entry against the code and the latest release before planning it: two premises no longer hold. Ticks the task steps that were done but never ticked; the live-account and live-key steps stay open."
```

---

### Task 1: Ein Abbruch ist kein LLM-Ausfall und kein fehlgeschlagener Post

Der Backlog-Eintrag behauptete, ein aufgegebener Import laufe bis zu `LLM_TIMEOUT` weiter, weil der Abbruch nicht bis in die Extraktion durchgereicht werde. Das stimmt nicht (siehe Ausgangslage, Zeile 1): der Abbruch erreicht den SDK-Aufruf sofort. Was stimmt, ist, was danach passiert. `HybridExtractor.Extract` fängt jeden Fehler des LLM-Aufrufs außer `ErrNoRecipe` als „Hänger" ab und gibt das schwächere Regel-Ergebnis zurück, markiert als `Degraded`, ohne Fehler. `Pipeline.Run` versucht dann, es mit einem bereits abgebrochenen Kontext zu speichern; das scheitert, wird als `failed` gezählt und als `stage=store` gewarnt, und die Schleife läuft zum nächsten Post, bis ihre Prüfung am Schleifenanfang greift. Ein Ctrl-C bei `recipe-reader import` druckt deshalb eine Zusammenfassung mit einem Fehlschlag, der keiner war.

Die Unterscheidung, die fehlt, ist die zwischen **dem Timeout des LLM** (ein Hänger, dafür ist der Fallback da, der Aufrufer wartet noch auf ein Ergebnis) und **dem Abbruch des Aufrufers** (niemand wartet mehr). Beide sind ein Kontextfehler; unterscheiden lassen sie sich am Kontext des Aufrufers, nicht am Fehler.

**Files:**
- Modify: `internal/extraction/hybrid.go` (Abbruch-Prüfung vor dem Fallback)
- Modify: `internal/extraction/hybrid_test.go` (+ 2 Tests, + 1 Fake, + Import `fmt`)
- Modify: `internal/extraction/llm_extract_test.go` (+ 1 Test, + Imports `net/http`, `net/http/httptest`)
- Modify: `internal/pipeline/pipeline.go` (`interrupted`, zwei Aufrufstellen, Doc-Kommentar von `ImportResult`)
- Modify: `internal/pipeline/pipeline_test.go` (+ 2 Tests, + 2 Fakes)
- Modify: `AGENTS.md`, `docs/superpowers/plans/2026-09-22-release-and-cleanup.md`

**Interfaces:**
- Consumes: `extraction.Extractor`, `extraction.NewHybridExtractor(rules, llm Extractor, threshold float64)`, `pipeline.ImportResult`, `repository.RecipeRepository`, die Test-Helfer `stubExtractor` (`hybrid_test.go`), `fakeFetcher`, `fakeExtractor`, `newTestPipeline` (`pipeline_test.go`).
- Produces: unverändertes öffentliches API. Neu und paketintern: `pipeline.interrupted(ctx context.Context, result ImportResult) (ImportResult, error)`. Verhalten: `HybridExtractor.Extract` gibt bei abgebrochenem Aufrufer-Kontext `nil` und einen Fehler zurück, der `context.Canceled` umhüllt; `Pipeline.Run` gibt dann `pipeline: context canceled` zurück, und der unterbrochene Post steht in keinem Zähler und nicht in `Seen`.

- [ ] **Step 1: Branch anlegen**

```bash
git switch main && git pull --ff-only && git switch -c fix/cancellation-is-not-a-fallback
```

- [ ] **Step 2: Den Beleg gegen die echten SDKs schreiben** (an `internal/extraction/llm_extract_test.go` anhängen; die Imports `net/http` und `net/http/httptest` ergänzen)

Dieser Test **besteht schon vor dem Fix**. Er ist kein Fix-Test, sondern die Messung, die die Prämisse des Backlog-Eintrags widerlegt, und er hält sie fest, damit der Eintrag geschlossen bleibt.

```go
// A cancelled parent context has to end a real SDK call at once, whatever the
// extractor's own timeout is. The backlog once claimed that an abandoned import
// ran on for up to LLM_TIMEOUT because cancellation "was not threaded into the
// extraction path"; it is: Extract derives its timeout from the caller's
// context, and both SDKs build their requests and wait out their retry backoff
// on it. This runs each SDK against a server that accepts the request and never
// answers, with a one-hour timeout, so only the cancellation can be what ends it.
func TestLLMExtractor_ParentCancellationEndsARealCallAtOnce(t *testing.T) {
	release := make(chan struct{})
	stall := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	// Cleanups run last in, first out: the handler is let go before Close waits
	// for it, or a failing run would hang here instead of failing.
	t.Cleanup(stall.Close)
	t.Cleanup(func() { close(release) })

	for _, provider := range []LLMProvider{ProviderAnthropic, ProviderOpenAI} {
		t.Run(string(provider), func(t *testing.T) {
			e, err := NewLLMExtractor(LLMConfig{
				Provider: provider, APIKey: "sk-test", Model: "test-model",
				BaseURL: stall.URL, Timeout: time.Hour,
			})
			if err != nil {
				t.Fatalf("NewLLMExtractor() error = %v", err)
			}

			ctx, cancel := context.WithCancel(t.Context())
			time.AfterFunc(50*time.Millisecond, cancel)

			start := time.Now()
			_, err = e.Extract(ctx, "caption")
			if !errors.Is(err, context.Canceled) {
				t.Errorf("Extract() error = %v, want it to wrap context.Canceled", err)
			}
			if elapsed := time.Since(start); elapsed > 5*time.Second {
				t.Errorf("Extract() returned after %v, want the cancellation to end it at once", elapsed)
			}
		})
	}
}
```

- [ ] **Step 3: Den Beleg laufen lassen, er muss bestehen**

Run: `go test -count=1 -run TestLLMExtractor_ParentCancellation -v ./internal/extraction/`
Expected: `PASS` für `anthropic` und `openai`, je rund 50 ms. Besteht er nicht, hält der Backlog-Eintrag: dann anhalten und melden, statt den Plan weiter abzuarbeiten.

- [ ] **Step 4: Die zwei Hybrid-Tests schreiben** (an `internal/extraction/hybrid_test.go` anhängen; den Import `"fmt"` ergänzen)

```go
// cancellingExtractor cancels the context it was given and then reports what an
// SDK reports for it, which is how a shutdown looks from inside an LLM call.
type cancellingExtractor struct{ cancel context.CancelFunc }

func (c cancellingExtractor) Extract(ctx context.Context, _ string) (*ExtractedRecipe, error) {
	c.cancel()
	<-ctx.Done()
	return nil, ctx.Err()
}

// A cancelled run is not an LLM hiccup. The fallback exists so that a failing
// provider never fails an import, but when the caller itself has been cancelled
// the weaker rules result is not wanted either: it used to come back as a
// success marked Degraded, and the pipeline then tried to store it on a context
// that was already done and counted the post as failed.
func TestHybridExtractor_CancellationIsNotAnLLMFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	rules := &stubExtractor{result: &ExtractedRecipe{Name: "A", Confidence: 0.3}}
	h := NewHybridExtractor(rules, cancellingExtractor{cancel: cancel}, 0.6)

	result, err := h.Extract(ctx, "caption")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Extract() error = %v, want it to wrap context.Canceled", err)
	}
	if result != nil {
		t.Errorf("Extract() = %+v, want no result for a cancelled run", result)
	}
}

// The counterpart, so the fix cannot over-correct: the LLM's own timeout is
// also a context error, but the caller's context is still alive, and that stays
// what the fallback is for.
func TestHybridExtractor_LLMTimeoutStillFallsBackWhileTheCallerIsAlive(t *testing.T) {
	rules := &stubExtractor{result: &ExtractedRecipe{Name: "A", Confidence: 0.3}}
	llm := &stubExtractor{err: fmt.Errorf("llm extract: %w", context.DeadlineExceeded)}
	h := NewHybridExtractor(rules, llm, 0.6)

	result, err := h.Extract(t.Context(), "caption")
	if err != nil {
		t.Fatalf("Extract() error = %v, want the rules result", err)
	}
	if result.Name != "A" || !result.Degraded {
		t.Errorf("Extract() = %+v, want the rules result marked Degraded", result)
	}
}
```

- [ ] **Step 5: Die Tests laufen lassen, der erste muss fehlschlagen**

Run: `go test -count=1 -run 'TestHybridExtractor_(CancellationIsNot|LLMTimeoutStill)' -v ./internal/extraction/`
Expected: `TestHybridExtractor_CancellationIsNotAnLLMFailure` **FAIL** mit `Extract() error = <nil>, want it to wrap context.Canceled`; `TestHybridExtractor_LLMTimeoutStillFallsBackWhileTheCallerIsAlive` PASS.

- [ ] **Step 6: `hybrid.go` ändern**

Den Import um `fmt` ergänzen. Alt:

```go
import (
	"context"
	"errors"
	"log/slog"
)
```

Neu:

```go
import (
	"context"
	"errors"
	"fmt"
	"log/slog"
)
```

Im Doc-Kommentar von `Extract` nach dem Absatz zu `ErrNoRecipe` einen Verweis anfügen. Alt:

```go
// Returning the rules result there is precisely how junk rows got imported.
func (h *HybridExtractor) Extract(ctx context.Context, caption string) (*ExtractedRecipe, error) {
```

Neu:

```go
// Returning the rules result there is precisely how junk rows got imported.
//
// A cancelled ctx is the other exception: see the check below.
func (h *HybridExtractor) Extract(ctx context.Context, caption string) (*ExtractedRecipe, error) {
```

Am Anfang des `if err != nil`-Blocks nach dem LLM-Aufruf einfügen. Alt:

```go
	if err != nil {
		// Say so, and mark the result. This was the worst of the three silent
```

Neu:

```go
	if err != nil {
		// A cancelled run is not an LLM failure, and the fallback below is not
		// for it. The caller's own context is what is checked, not the error: the
		// LLM's timeout is a context error too, and that one is a hiccup the
		// fallback exists for, while the caller still wants a result. Once the
		// caller has gone, a weaker result is only something the pipeline would
		// try to store on a context that is already done.
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, fmt.Errorf("extraction: %w", ctxErr)
		}
		// Say so, and mark the result. This was the worst of the three silent
```

- [ ] **Step 7: Die Tests laufen lassen, sie müssen grün sein**

Run: `go test -count=1 -race ./internal/extraction/`
Expected: PASS

- [ ] **Step 8: Die zwei Pipeline-Tests schreiben** (an `internal/pipeline/pipeline_test.go` anhängen; alle nötigen Imports stehen schon in der Datei)

```go
// cancellingExtractor cancels the run's context while it is extracting and
// reports what an SDK reports for it: a shutdown arriving mid-call.
type cancellingExtractor struct{ cancel context.CancelFunc }

func (c cancellingExtractor) Extract(ctx context.Context, _ string) (*extraction.ExtractedRecipe, error) {
	c.cancel()
	return nil, ctx.Err()
}

// cancelBeforeCreate cancels the run's context just before the store, so the
// write is what the shutdown lands on.
type cancelBeforeCreate struct {
	repository.RecipeRepository
	cancel context.CancelFunc
}

func (c cancelBeforeCreate) Create(ctx context.Context, r *domain.Recipe) error {
	c.cancel()
	return c.RecipeRepository.Create(ctx, r)
}

// A shutdown that lands on a post is not that post failing. It used to be
// counted as Failed, with a warning naming the stage, so the import command's
// summary after a Ctrl-C reported a failure that never happened — and the run
// carried on to the next post instead of stopping. The post reached no outcome,
// so it is in none of the buckets: the next run sees it again.
func TestPipeline_ShutdownMidExtractionIsNotAFailedPost(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	p := newTestPipeline(t,
		&fakeFetcher{posts: []instagram.SavedPost{{Source: "src-cut", Caption: "c"}}},
		cancellingExtractor{cancel: cancel},
	)

	result, err := p.Run(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want it to wrap context.Canceled", err)
	}
	if result != (ImportResult{}) {
		t.Errorf("result = %+v, want the interrupted post in none of the buckets", result)
	}
}

func TestPipeline_ShutdownMidStoreIsNotAFailedPost(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	extracted := &extraction.ExtractedRecipe{Name: "Brot", Confidence: 0.9}
	p := newTestPipeline(t,
		&fakeFetcher{posts: []instagram.SavedPost{{Source: "src-cut", Caption: "c"}}},
		&fakeExtractor{byCaption: map[string]*extraction.ExtractedRecipe{"c": extracted}},
	)
	p.Recipes = cancelBeforeCreate{RecipeRepository: p.Recipes, cancel: cancel}

	result, err := p.Run(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want it to wrap context.Canceled", err)
	}
	if result != (ImportResult{}) {
		t.Errorf("result = %+v, want the interrupted post in none of the buckets", result)
	}
}
```

- [ ] **Step 9: Die Tests laufen lassen, beide müssen fehlschlagen**

Run: `go test -count=1 -run 'TestPipeline_ShutdownMid' -v ./internal/pipeline/`
Expected: beide **FAIL** mit `Run() error = <nil>, want it to wrap context.Canceled` (Docker muss laufen).

- [ ] **Step 10: `pipeline.go` ändern**

Die Extraktionsstufe. Alt:

```go
		if err != nil {
			result.Failed++
			slog.Warn("import: post failed", "stage", "extract", "source", post.Source, "error", err)
			continue
		}
```

Neu:

```go
		if err != nil {
			if ctx.Err() != nil {
				return interrupted(ctx, result)
			}
			result.Failed++
			slog.Warn("import: post failed", "stage", "extract", "source", post.Source, "error", err)
			continue
		}
```

Die Speicherstufe. Alt:

```go
		if err != nil {
			result.Failed++
			slog.Warn("import: post failed", "stage", "store", "source", post.Source, "error", err)
			continue
		}
```

Neu:

```go
		if err != nil {
			if ctx.Err() != nil {
				return interrupted(ctx, result)
			}
			result.Failed++
			slog.Warn("import: post failed", "stage", "store", "source", post.Source, "error", err)
			continue
		}
```

Die Hilfsfunktion direkt vor `toRecipe` einfügen. Alt:

```go
// toRecipe maps an extraction result onto a domain.Recipe, carrying the
```

Neu:

```go
// interrupted ends the run for a post that a cancellation cut off part-way.
//
// Such a post is not a failure: nothing was wrong with it, the run was told to
// stop. It used to be counted as Failed and warned about by stage, so a Ctrl-C
// during `recipe-reader import` printed a summary reporting a post that never
// failed — and the loop went on to the next post rather than stopping. The post
// reached no outcome, so it leaves Seen too and is in none of the buckets; the
// next run sees it again, because nothing was stored.
func interrupted(ctx context.Context, result ImportResult) (ImportResult, error) {
	result.Seen--
	slog.Warn("import: cancelled mid-run", "error", ctx.Err(), "result", result)
	return result, fmt.Errorf("pipeline: %w", ctx.Err())
}

// toRecipe maps an extraction result onto a domain.Recipe, carrying the
```

Den Doc-Kommentar von `ImportResult` ergänzen. Alt:

```go
// present, matched by source), NoRecipe (the caption carried no recipe, so
// nothing was stored), Failed (extraction or storage error on that one post).
//
// NoRecipe is counted apart from Skipped and Failed on purpose: a saved-posts
```

Neu:

```go
// present, matched by source), NoRecipe (the caption carried no recipe, so
// nothing was stored), Failed (extraction or storage error on that one post).
// A post that a cancellation cut off part-way is the one exception: it reached
// no outcome, so it is in none of the four and not in Seen either (see
// interrupted).
//
// NoRecipe is counted apart from Skipped and Failed on purpose: a saved-posts
```

- [ ] **Step 11: Die Tests laufen lassen, sie müssen grün sein**

Run: `go test -count=1 -race ./internal/extraction/ ./internal/pipeline/ ./internal/server/`
Expected: PASS. Die Suite von `server` ist dabei, weil sie den Worker und den Abbruchpfad von außen treibt.

- [ ] **Step 12: `AGENTS.md` berichtigen**

Im Abschnitt „Import and Instagram" an den Absatz zum Shutdown anhängen. Alt:

```
- `serve` shutdown: HTTP drain plus import wait, with a 10s budget. After that an import is
  abandoned with a log line. `server.Run` works on its own cancellable context and waits for its
  goroutines even on a failed start.
```

Neu:

```
- `serve` shutdown: HTTP drain plus import wait, with a 10s budget. After that an import is
  abandoned with a log line. `server.Run` works on its own cancellable context and waits for its
  goroutines even on a failed start. A cancelled context reaches an LLM call at once, whatever
  `LLM_TIMEOUT` is: `Extract` derives its timeout from the caller's context and both SDKs wait on
  it (`TestLLMExtractor_ParentCancellationEndsARealCallAtOnce`). An abandoned import therefore
  does not run on for a timeout; the old backlog entry saying so was measured and withdrawn.
```

Im Abschnitt „Extraction" hinter den Absatz, der mit `(E1, E8/T-15).` endet, ein neuer Absatz:

```
- **A cancelled caller is not an LLM failure.** Hybrid returns the caller's context error instead
  of falling back, and the pipeline ends the run on it: the interrupted post is counted nowhere
  (not `failed`, not `seen`) and is seen again by the next run. The check is on the caller's own
  context, not on the error, because the LLM's timeout is a context error too and stays a
  fallback (`TestHybridExtractor_LLMTimeoutStillFallsBackWhileTheCallerIsAlive`).
```

Im Abschnitt „Open work and backlog" den Eintrag streichen. Alt:

```
- An abandoned import ends only after its `LLM_TIMEOUT`. Fixing the cause means threading
  cancellation into the extraction path.
```

Neu: (Zeilen entfernt.)

- [ ] **Step 13: Den Backlog-Eintrag im Release-Plan schließen**

In `docs/superpowers/plans/2026-09-22-release-and-cleanup.md` im Abschnitt „Backlog (bewusst nicht in diesem Plan)" vor den Eintrag „Der aufgegebene Import endet erst nach seinem LLM-Timeout" einfügen:

```
> **Widerlegt und behoben (2026-09-25).** Der folgende Eintrag ist im Plan [2026-09-25-backlog-cleanup.md](2026-09-25-backlog-cleanup.md) geschlossen (M1). Seine Prämisse trug nicht: der Abbruch erreicht den LLM-Aufruf sofort, gemessen gegen beide SDKs bei einem Timeout von einer Stunde. Die echte Lücke war, dass `HybridExtractor` den Abbruch als LLM-Ausfall auffing und die Pipeline den unterbrochenen Post als fehlgeschlagen zählte.
```

- [ ] **Step 14: Commit-Gate und Commit**

```bash
git add internal docs AGENTS.md
git commit -m "fix(pipeline): count a cancelled import post as cancelled, not failed" -m "HybridExtractor treated a cancelled caller as an LLM failure and returned the rules result marked Degraded; the pipeline then tried to store it on a dead context, counted the post as failed and carried on. Hybrid now returns the caller's context error (its own timeout still falls back), and the pipeline ends the run on it, leaving the interrupted post in no bucket. The backlog claim that cancellation never reached the extraction path is disproved by a test against both SDKs."
```

(Footer laut Global Constraints anhängen.)

---

### Task 2: Migration `0004` entfernt den redundanten Index

`0003` machte `(recipe_id, position)` eindeutig. Der Index hinter dieser Constraint beginnt mit `recipe_id` und bedient damit alles, was `idx_recipe_ingredients_recipe_id` bediente: die zwei Lesezugriffe in `recipes.sql` (`WHERE recipe_id = $1` und `= ANY($1)`, beide `ORDER BY position`), das `DELETE … WHERE recipe_id = $1` beim Ersetzen der Zutaten und das `ON DELETE CASCADE` von `recipes`. Der Einzelspalten-Index wird nur noch bei jedem Schreiben mitgepflegt. Weil es um Indizes geht, prüft der Test den **Plan** der Abfrage, statt der Argumentation zu vertrauen.

**Files:**
- Create: `internal/db/migrations/0004_drop_redundant_recipe_ingredients_index.sql`
- Modify: `internal/db/migrations_test.go` (+ `strings`-Import, + 1 Test, + 2 Helfer)
- Modify: `README.md` (Abschnitt „Changing the database schema"), `AGENTS.md`
- Modify: `docs/reviews/2026-09-11-working-plan.md` (T-55-Notiz), `docs/superpowers/plans/2026-09-22-release-and-cleanup.md`

**Interfaces:**
- Consumes: `testdb.NewDatabase(t, name) string`, `fileProvider(t, dsn) *goose.Provider`, `connect(t, dsn) *pgxpool.Pool` (alle in `migrations_test.go`)
- Produces: die Migration; die Helfer `indexExists(t, pool, name) bool` und `lookupPlan(t, pool) string` (nur für diesen Test)

- [ ] **Step 1: Branch anlegen**

```bash
git switch main && git pull --ff-only && git switch -c perf/drop-redundant-ingredients-index
```

- [ ] **Step 2: Den Test schreiben** (an `internal/db/migrations_test.go` vor `fileProvider` einfügen; den Import `"strings"` nach `"slices"` ergänzen)

```go
// redundantIndex is what migration 0004 drops.
const redundantIndex = "idx_recipe_ingredients_recipe_id"

// Migration 0004 drops idx_recipe_ingredients_recipe_id, which 0003's
// UNIQUE (recipe_id, position) made redundant: the constraint's index leads with
// recipe_id, so it serves every lookup by recipe_id the single-column one did.
// What has to hold is that the drop leaves no lookup without an index, so this
// checks the plan rather than trusting the argument. Sequential scans are
// switched off for it, because the table is empty and the planner would
// otherwise pick one whatever indexes exist.
func TestMigration0004_DropsTheRedundantIndexWithoutLosingTheLookup(t *testing.T) {
	ctx := t.Context()
	dsn := testdb.NewDatabase(t, "migration_0004")
	provider := fileProvider(t, dsn)
	if _, err := provider.UpTo(ctx, 3); err != nil {
		t.Fatalf("migrate to 3: %v", err)
	}
	pool := connect(t, dsn)
	defer pool.Close()

	if !indexExists(t, pool, redundantIndex) {
		t.Fatalf("%s is missing at version 3, so there is nothing for 0004 to drop", redundantIndex)
	}

	if _, err := provider.UpTo(ctx, 4); err != nil {
		t.Fatalf("migrate to 4: %v", err)
	}
	if indexExists(t, pool, redundantIndex) {
		t.Errorf("%s still exists after 0004", redundantIndex)
	}
	if plan := lookupPlan(t, pool); !strings.Contains(plan, "recipe_ingredients_recipe_id_position_key") {
		t.Errorf("the lookup by recipe_id no longer uses the constraint's index:\n%s", plan)
	}

	if _, err := provider.DownTo(ctx, 3); err != nil {
		t.Fatalf("migrate down to 3: %v", err)
	}
	if !indexExists(t, pool, redundantIndex) {
		t.Errorf("%s was not restored by the down migration", redundantIndex)
	}
}

// indexExists reports whether the public schema has an index called name.
func indexExists(t *testing.T, pool *pgxpool.Pool, name string) bool {
	t.Helper()
	var present bool
	if err := pool.QueryRow(t.Context(),
		"SELECT EXISTS (SELECT 1 FROM pg_indexes WHERE schemaname = 'public' AND indexname = $1)", name).Scan(&present); err != nil {
		t.Fatal(err)
	}
	return present
}

// lookupPlan is the plan for the read recipes.sql makes for one recipe's
// ingredients, with sequential scans off so it names the index it would use.
func lookupPlan(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	ctx := t.Context()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "SET LOCAL enable_seqscan = off"); err != nil {
		t.Fatal(err)
	}
	rows, err := tx.Query(ctx, "EXPLAIN SELECT amount FROM recipe_ingredients WHERE recipe_id = 1 ORDER BY position, id")
	if err != nil {
		t.Fatal(err)
	}
	lines, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	return strings.Join(lines, "\n")
}
```

- [ ] **Step 3: Den Test laufen lassen, er muss fehlschlagen**

Run: `go test -count=1 -run TestMigration0004 -v ./internal/db/`
Expected: **FAIL** mit `idx_recipe_ingredients_recipe_id still exists after 0004`. (goose meldet für `UpTo(4)` ohne Datei keinen Fehler, deshalb scheitert die Prüfung erst dort.)

- [ ] **Step 4: Die Migration anlegen** (`internal/db/migrations/0004_drop_redundant_recipe_ingredients_index.sql`)

```sql
-- +goose Up
-- 0003 made (recipe_id, position) unique, and the index behind that constraint
-- leads with recipe_id. It therefore serves every lookup by recipe_id that
-- idx_recipe_ingredients_recipe_id served — the two reads in recipes.sql, the
-- delete that replaces a recipe's ingredients, and the ON DELETE CASCADE from
-- recipes — and serves the ORDER BY position on top of it. The single-column
-- index only stayed to be maintained on every write for nothing.
--
-- A plain DROP INDEX rather than DROP INDEX CONCURRENTLY: the table is one
-- person's recipe lines, the lock is over in milliseconds, and CONCURRENTLY
-- would need goose's no-transaction mode to buy nothing here. (Comments must
-- not spell that annotation out: goose reads any comment line containing it as
-- a directive, mid-sentence or not, and refuses the file.)
DROP INDEX IF EXISTS idx_recipe_ingredients_recipe_id;

-- +goose Down
CREATE INDEX idx_recipe_ingredients_recipe_id ON recipe_ingredients (recipe_id);
```

**Die Kommentare dieser Datei dürfen die goose-Annotation nicht ausschreiben.** Der erste Entwurf tat es (mitten im Satz), und goose lehnte die ganze Datei ab: `failed to parse annotation line … invalid annotation`. Der Klammersatz oben ist deshalb Absicht.

- [ ] **Step 5: Die Tests laufen lassen, sie müssen grün sein**

Run: `go test -count=1 -race ./internal/db/`
Expected: PASS, darunter `TestMigration0004_…`, `TestMigration0003_…` und `TestMigrations_UpDownUp` (das die neue Migration ohne Änderung in beide Richtungen abdeckt).

- [ ] **Step 6: sqlc bleibt unverändert**

Run: `make sqlc-generate && git diff --exit-code internal/db/sqlc && echo "sqlc: no drift"`
Expected: `sqlc: no drift`. Die CI prüft dieselbe Drift.

- [ ] **Step 7: Dokumentation nachziehen**

`README.md`, Abschnitt „Changing the database schema": hinter den Satz, der mit `file instead.` endet, anhängen:

```
   Keep the annotations out of comments as well: goose reads any comment line that contains one as a
   directive, mid-sentence or not, and refuses the whole file.
```

`AGENTS.md`, Abschnitt „Persistence and migrations", zwei Änderungen. Alt:

```
  `(recipe_id, ingredient_id)` is not (T-55, migration `0003`).
  `idx_recipe_ingredients_recipe_id` is now redundant, and dropping it is left to a later
  migration.
```

Neu:

```
  `(recipe_id, ingredient_id)` is not (T-55, migration `0003`).
  `idx_recipe_ingredients_recipe_id` was redundant from then on and is dropped by migration `0004`;
  `TestMigration0004_…` checks the plan of the lookup rather than the argument.
```

Alt:

```
  transaction. Use `-- +goose NO TRANSACTION` only for statements like `CREATE INDEX CONCURRENTLY`.
  Never renumber or edit an applied migration.
```

Neu:

```
  transaction. Use `-- +goose NO TRANSACTION` only for statements like `CREATE INDEX CONCURRENTLY`.
  No migration needs it yet: `0004` drops an index with a plain `DROP INDEX`, because on this
  table's size the lock is milliseconds. **A comment must not contain a `+goose` annotation, even
  mid-sentence:** goose reads such a line as a directive and refuses the file.
  Never renumber or edit an applied migration.
```

Den Eintrag im Abschnitt „Open work and backlog" streichen:

```
- The redundant `idx_recipe_ingredients_recipe_id` index.
```

`docs/reviews/2026-09-11-working-plan.md`, T-55: an die Notiz `*Noted, not done:* … Dropping it is a later migration's call.` anhängen:

```
      *Done later:* migration `0004` drops it (backlog plan of 2026-09-25, M2).
```

`docs/superpowers/plans/2026-09-22-release-and-cleanup.md`, Backlog, vor den Eintrag „Migrationen mit `-- +goose NO TRANSACTION`" einfügen:

```
> **Geprüft, kein Task (2026-09-25).** Der Eintrag ist auslöserbasiert und bleibt es: nötig wird der No-Transaction-Modus erst mit einer Migration, die `CREATE INDEX CONCURRENTLY` braucht. Der einzige Kandidat, das Entfernen des redundanten Index in `0004` (Plan [2026-09-25-backlog-cleanup.md](2026-09-25-backlog-cleanup.md), E8), braucht ihn nicht; ein einfaches `DROP INDEX` genügt bei dieser Tabellengröße. Dabei fiel auf, dass goose eine Kommentarzeile mit der Annotation als Direktive liest und die Datei ablehnt; das steht jetzt im README und in `AGENTS.md`.
```

- [ ] **Step 8: Commit-Gate und Commit**

```bash
git add internal/db README.md AGENTS.md docs
git commit -m "perf(db): drop the redundant recipe_ingredients index" -m "0003's UNIQUE (recipe_id, position) is backed by an index that leads with recipe_id, so it serves every lookup idx_recipe_ingredients_recipe_id served, and the single-column index was only being maintained on every write. Migration 0004 drops it with a plain DROP INDEX; a test checks the plan of the lookup, the down migration recreates it. Also documents that goose refuses a migration whose comment contains its annotation."
```

---

### Task 3: Image je Architektur, Manifest-Liste, CI auf beiden

Heute baut der Job `image` auf `ubuntu-latest`, testet und pusht unter `<tag>` und `latest`. Danach laufen zwei Jobs: `image` als Matrix über `amd64`/`arm64`, jeweils auf einem Runner der eigenen Architektur, und `manifest`, der beide Images unter `<tag>` (und `latest`) vereint. Das Dockerfile bleibt, wie es ist: alle Basis-Images sind Multi-Arch-Indizes.

**Files:**
- Modify: `.github/workflows/release-artifacts.yml` (`image`-Job ersetzt, `manifest`-Job neu, `publish.needs`)
- Modify: `.github/workflows/ci.yml` (`docker`-Job als Matrix)
- Modify: `README.md` (Abschnitt „Releases"), `AGENTS.md`
- Modify: `docs/superpowers/plans/2026-09-22-release-and-cleanup.md`

**Interfaces:**
- Consumes: `scripts/smoke-test-image.sh <image> <expected-version>` (unverändert), `docker/login-action`, `actions/checkout` (bestehende Pins)
- Produces: die Tags `ghcr.io/sburmester/recipe-reader:<tag>-amd64`, `…:<tag>-arm64`, die Manifest-Liste `…:<tag>` und (nicht bei `workflow_dispatch`) `…:latest`; der Job-Name `manifest`, auf den `publish` und in M4 der Attest-Schritt aufsetzen

- [ ] **Step 1: Branch anlegen**

```bash
git switch main && git pull --ff-only && git switch -c ci/arm64-image
```

- [ ] **Step 2: Den `image`-Job ersetzen und den `manifest`-Job anlegen** (`.github/workflows/release-artifacts.yml`)

Den bisherigen Job `image:` (bis Dateiende) durch die folgenden zwei Jobs ersetzen. Die Pins von `checkout` und `login-action` sind die bestehenden.

```yaml
  # One image per architecture, each built and smoke-tested on a runner of its
  # own architecture and pushed under an architecture-suffixed tag; the manifest
  # job below joins them under the release tag. Native runners rather than
  # emulation: what the smoke test starts is the image that ships, and no
  # emulation action is added to the workflow. Every base image in the
  # Dockerfile is a multi-architecture index, so the Dockerfile itself is the
  # same on both.
  image:
    strategy:
      matrix:
        include:
          - arch: amd64
            runner: ubuntu-latest
          - arch: arm64
            runner: ubuntu-24.04-arm
    runs-on: ${{ matrix.runner }}
    timeout-minutes: 30
    permissions:
      contents: read
      packages: write
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7
        with:
          ref: ${{ inputs.tag }}
      - uses: docker/login-action@dbcb813823bdd20940b903addbd779551569679f # v4
        with:
          registry: ghcr.io
          username: ${{ github.actor }}
          password: ${{ github.token }}

      # The image builds the frontend and the binary itself, so this needs no
      # Go or Node setup — the Dockerfile is the single description of how the
      # runtime image comes about. Plain docker rather than build-push-action:
      # the image has to pass the smoke test between the build and the push,
      # and that is three commands. Nothing is pushed that has not passed it.
      #
      # GHCR accepts lowercase names only, and the owner here is not.
      - name: Build, smoke-test and push
        env:
          TAG: ${{ inputs.tag }}
          ARCH: ${{ matrix.arch }}
        run: |
          image="ghcr.io/${GITHUB_REPOSITORY,,}:${TAG}-${ARCH}"
          docker build --build-arg VERSION="$TAG" -t "$image" .
          scripts/smoke-test-image.sh "$image" "$TAG"
          docker push "$image"

  # Joins the two architectures' images under the release tag, which is what a
  # pull by tag resolves to. The suffixed tags stay: the manifest list points at
  # their manifests, so removing them from the registry would remove what the
  # tag serves.
  manifest:
    needs: image
    runs-on: ubuntu-latest
    timeout-minutes: 10
    permissions:
      contents: read
      packages: write
    steps:
      - uses: docker/login-action@dbcb813823bdd20940b903addbd779551569679f # v4
        with:
          registry: ghcr.io
          username: ${{ github.actor }}
          password: ${{ github.token }}
      - name: Join the per-architecture images
        env:
          TAG: ${{ inputs.tag }}
        run: |
          image="ghcr.io/${GITHUB_REPOSITORY,,}"
          docker buildx imagetools create -t "$image:$TAG" "$image:$TAG-amd64" "$image:$TAG-arm64"
          # A manual rerun may be for an older release; only a new one moves
          # latest, which is what compose pulls by default.
          if [ "$GITHUB_EVENT_NAME" != workflow_dispatch ]; then
            docker buildx imagetools create -t "$image:latest" "$image:$TAG-amd64" "$image:$TAG-arm64"
          fi
```

`publish` wartet auf die vereinte Liste, nicht auf ein einzelnes Image. Alt:

```yaml
  publish:
    needs: [binaries, image]
```

Neu:

```yaml
  publish:
    needs: [binaries, manifest]
```

- [ ] **Step 3: Den `docker`-Job der CI auf beide Architekturen stellen** (`.github/workflows/ci.yml`)

Alt:

```yaml
  # Builds the frontend and the binary from scratch in the image, so this
  # catches Dockerfile drift that neither job above would see.
  docker:
    runs-on: ubuntu-latest
    needs: [backend, frontend]
    timeout-minutes: 20
```

Neu:

```yaml
  # Builds the frontend and the binary from scratch in the image, so this
  # catches Dockerfile drift that neither job above would see. On both
  # architectures the release image is published for, on runners of their own:
  # an arm64-only breakage would otherwise surface for the first time in a
  # release run.
  docker:
    strategy:
      fail-fast: false
      matrix:
        runner: [ubuntu-latest, ubuntu-24.04-arm]
    runs-on: ${{ matrix.runner }}
    needs: [backend, frontend]
    timeout-minutes: 20
```

- [ ] **Step 4: Die Workflows mit `actionlint` prüfen**

`actionlint` ist ein Einmalwerkzeug und keine Abhängigkeit des Repositories (kein Eintrag in `go.mod`); die CI prüft Workflow-Dateien nicht, und ein Syntaxfehler in `release-artifacts.yml` würde sich sonst erst im Release zeigen.

Run: `go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12 .github/workflows/*.yml`
Expected: keine Ausgabe, Exit 0.

- [ ] **Step 5: Das Image auf ARM vorab zeigen** *(Push und Workflow-Lauf: Freigabe des Nutzers einholen)*

Der riskanteste Teil dieses Milestones ist, ob das Image auf ARM baut und den Smoke-Test besteht. Das lässt sich vor dem Merge zeigen, denn die CI-Matrix aus Step 3 tut genau das.

```bash
git push -u origin ci/arm64-image
gh workflow run ci.yml --ref ci/arm64-image -R sBurmester/recipe-reader
run=$(gh run list --workflow ci.yml --branch ci/arm64-image --limit 1 --json databaseId -q '.[0].databaseId' -R sBurmester/recipe-reader)
gh run watch "$run" -R sBurmester/recipe-reader --exit-status
gh run view "$run" -R sBurmester/recipe-reader --json jobs -q '.jobs[] | select(.name | startswith("docker")) | "\(.name): \(.conclusion)"'
```

Expected: `docker (ubuntu-latest): success` **und** `docker (ubuntu-24.04-arm): success`; im Log beider Jobs `smoke test passed: recipe-reader:ci (…)`. Scheitert die ARM-Leg, ist es ein Befund des Milestones und wird hier behoben, bevor er gemerged wird.

- [ ] **Step 6: README und `AGENTS.md` nachziehen**

`README.md`, Abschnitt „Releases". Alt:

```
The image is built from the same tag, has to pass `scripts/smoke-test-image.sh` before it is
pushed, and lands in `ghcr.io/sburmester/recipe-reader` under the tag and `latest`. Both are the
job of `release-artifacts.yml`, which `release-please.yml` calls once it has made the release.
```

Neu:

```
The image is built from the same tag for `linux/amd64` and `linux/arm64`, each on a runner of its
own architecture. Each has to pass `scripts/smoke-test-image.sh` before it is pushed, and a
manifest joins the two in `ghcr.io/sburmester/recipe-reader` under the tag and `latest`, so a pull
on either architecture gets its own. The suffixed tags `<tag>-amd64` and `<tag>-arm64` stay in the
registry on purpose: the manifest points at them, and deleting one would break the tag. All of it is
the job of `release-artifacts.yml`, which `release-please.yml` calls once it has made the release.
```

`AGENTS.md`, Abschnitt „Delivery, CI, releases and dependencies". Alt:

```
  **draft** release. `release-artifacts.yml` attaches the binaries and `checksums.txt`, pushes the
  image (tag + `latest`, `linux/amd64` only) after the smoke test, then publishes. Releases are
```

Neu:

```
  **draft** release. `release-artifacts.yml` attaches the binaries and `checksums.txt`, builds the
  image per architecture (`linux/amd64` and `linux/arm64`, each on a native runner and smoke-tested
  before its push), joins them under the tag and `latest` with a manifest, then publishes. The
  `<tag>-amd64`/`<tag>-arm64` tags stay on purpose. CI builds and smoke-tests the image on both.
  Releases are
```

Den Eintrag im Abschnitt „Open work and backlog" ändern. Alt:

```
- A multi-arch image (`linux/arm64`), and signing or provenance for release artifacts.
```

Neu:

```
- Signing or provenance for release artifacts.
```

(In M4 entfällt auch diese Zeile.)

`docs/superpowers/plans/2026-09-22-release-and-cleanup.md`, Backlog, vor den Eintrag „Multi-Arch-Image." einfügen:

```
> **Erledigt.** Der folgende Eintrag ist im Plan [2026-09-25-backlog-cleanup.md](2026-09-25-backlog-cleanup.md) umgesetzt (M3, Entscheidungen E1–E3): das Image entsteht je Architektur auf nativen Runnern und wird als Manifest-Liste veröffentlicht, ohne Emulation.
```

- [ ] **Step 7: Commit-Gate und Commit**

```bash
git add .github README.md AGENTS.md docs
git commit -m "feat(ci): publish a linux/arm64 image" -m "The image job is a matrix over amd64 and arm64, each built and smoke-tested on a runner of its own architecture and pushed under a suffixed tag; a manifest job joins them under the release tag and latest, and publish waits for it. Native runners rather than emulation, so no new action and the smoke test runs what ships. CI builds and smoke-tests the image on both architectures too."
```

- [ ] **Step 8: Nach dem Merge das echte Release prüfen** *(Freigabe des Nutzers: den Release-PR mergen)*

Die Pipeline selbst läuft nur in einem echten Release. Den Release-PR lesen (Version und Changelog), mit dem Nutzer mergen, den Lauf beobachten und prüfen:

```bash
tag=$(gh release list -R sBurmester/recipe-reader --limit 1 --json tagName -q '.[0].tagName')
docker manifest inspect ghcr.io/sburmester/recipe-reader:$tag | python3 -c 'import sys,json; print(sorted({m["platform"]["os"]+"/"+m["platform"]["architecture"] for m in json.load(sys.stdin)["manifests"]} - {"unknown/unknown"}))'
docker pull --platform linux/arm64 ghcr.io/sburmester/recipe-reader:$tag >/dev/null && docker image inspect ghcr.io/sburmester/recipe-reader:$tag --format '{{.Architecture}}'
```

Expected: `['linux/amd64', 'linux/arm64']` und `arm64`. `latest` zeigt auf dieselbe Liste. Schlägt der Lauf fehl, bleibt das Release ein Entwurf (R1): Ursache beheben, dann `gh workflow run release-artifacts.yml -f tag=$tag`. (Lokal fehlt `docker buildx`; `docker manifest inspect` und `docker pull` genügen für die Prüfung.)

---

### Task 4: Attestation für Binaries und Image

Ausgangslage aus der Prüfung: die Assets sind durch die unveränderlichen Releases schon attestiert (das ist GitHubs Release-Attestation), aber `gh attestation verify` findet für Binary und Image **keine Build-Provenance** (HTTP 404). Nach diesem Task sagt jedes Artefakt, welcher Workflow und welcher Commit es gebaut hat.

**Files:**
- Modify: `.github/workflows/release-artifacts.yml` (Rechte und Attest-Schritt im Job `binaries`; Rechte, Digest und Attest-Schritt im Job `manifest`)
- Modify: `.github/workflows/release-please.yml` (Rechte des Jobs `artifacts`)
- Modify: `README.md` (Abschnitt „Releases"), `AGENTS.md`
- Modify: `docs/superpowers/plans/2026-09-22-release-and-cleanup.md`

**Interfaces:**
- Consumes: den Job `manifest` und das Tag `<image>:<tag>` aus Task 3
- Produces: Attestationen (SLSA-Build-Provenance) im Speicher des Repositories für `dist/recipe-reader_*` (je Matrix-Job) und für den Digest der Manifest-Liste

**Warum die Rechte auch in `release-please.yml` stehen:** `release-artifacts.yml` wird von dort aufgerufen (`workflow_call`), und die Jobs eines aufgerufenen Workflows dürfen nicht mehr Rechte anfordern, als der aufrufende Job hält. `actionlint` erkennt das **nicht**; ohne die Änderung lehnt GitHub den Aufruf ab, und die Release-Pipeline startet gar nicht.

- [ ] **Step 1: Branch anlegen** (nachdem M3 gemerged ist)

```bash
git switch main && git pull --ff-only && git switch -c ci/attest-release-artifacts
```

- [ ] **Step 2: Den Pin der Action auflösen**

`actions/attest` ist neu im Repository, also wird sie sofort gepinnt (Renovate hebt bestehende Pins, legt aber keine an).

Run: `gh api repos/actions/attest/commits/v4 --jq .sha`
Expected (Stand 2026-09-25, v4.2.2): `1e69f48acb82d1966a394da916b4c1698aa569d6`. Liefert der Befehl einen anderen Wert, ist v4 weitergewandert; den neuen SHA in den folgenden Steps verwenden und den Kommentar `# v4` beibehalten.

- [ ] **Step 3: Die Binaries im Job attestieren, der sie baut** (`.github/workflows/release-artifacts.yml`)

Rechte des Jobs `binaries`. Alt:

```yaml
  binaries:
    runs-on: ubuntu-latest
    timeout-minutes: 20
```

Neu:

```yaml
  binaries:
    runs-on: ubuntu-latest
    timeout-minutes: 20
    # Read-only on the repository: this job only checks out and builds. The other
    # three are what actions/attest needs — the OIDC token for the signing
    # certificate, and the two writes that store the attestation.
    permissions:
      contents: read
      id-token: write
      attestations: write
      artifact-metadata: write
```

Der Attest-Schritt, direkt nach dem Bauen und vor dem Hochladen. Alt:

```yaml
      - uses: actions/upload-artifact@043fb46d1a93c77aae656e7c1c64a875d1fc6a0a # v7
        with:
          name: dist-${{ matrix.goos }}-${{ matrix.goarch }}
```

Neu:

```yaml
      # Attested here, on the runner that built the binary, so the provenance
      # names the job that produced it rather than one that only handled it
      # afterwards. Verify with: gh attestation verify <file> -R <owner>/<repo>
      - uses: actions/attest@1e69f48acb82d1966a394da916b4c1698aa569d6 # v4
        with:
          subject-path: dist/recipe-reader_*

      - uses: actions/upload-artifact@043fb46d1a93c77aae656e7c1c64a875d1fc6a0a # v7
        with:
          name: dist-${{ matrix.goos }}-${{ matrix.goarch }}
```

- [ ] **Step 4: Das Image im Job `manifest` attestieren**

Rechte des Jobs. Alt:

```yaml
  manifest:
    needs: image
    runs-on: ubuntu-latest
    timeout-minutes: 10
    permissions:
      contents: read
      packages: write
```

Neu:

```yaml
  manifest:
    needs: image
    runs-on: ubuntu-latest
    timeout-minutes: 10
    permissions:
      contents: read
      packages: write
      id-token: write
      attestations: write
      artifact-metadata: write
```

Am Ende der Datei, nach dem Schritt `Join the per-architecture images`, anhängen:

```yaml

      # The attestation is for the manifest list, because that is what the tag
      # resolves to: `gh attestation verify oci://<image>:<tag>` checks the
      # digest the tag points at. The digest is read back from the registry
      # rather than from the create step, which prints none.
      - name: Read the digest of the joined image
        id: image
        env:
          TAG: ${{ inputs.tag }}
        run: |
          image="ghcr.io/${GITHUB_REPOSITORY,,}"
          digest=$(docker buildx imagetools inspect "$image:$TAG" --format '{{json .Manifest}}' | jq -r .digest)
          echo "name=$image" >> "$GITHUB_OUTPUT"
          echo "digest=$digest" >> "$GITHUB_OUTPUT"
      - uses: actions/attest@1e69f48acb82d1966a394da916b4c1698aa569d6 # v4
        with:
          subject-name: ${{ steps.image.outputs.name }}
          subject-digest: ${{ steps.image.outputs.digest }}
          push-to-registry: true
```

- [ ] **Step 5: Die Rechte des aufrufenden Jobs erweitern** (`.github/workflows/release-please.yml`)

Alt:

```yaml
    permissions:
      contents: write
      packages: write
    uses: ./.github/workflows/release-artifacts.yml
```

Neu:

```yaml
    # The most release-artifacts.yml may ask for: a called workflow's jobs can
    # be granted no more than the job that calls it holds. The last three are
    # for the attestations of the binaries and of the image.
    permissions:
      contents: write
      packages: write
      id-token: write
      attestations: write
      artifact-metadata: write
    uses: ./.github/workflows/release-artifacts.yml
```

- [ ] **Step 6: Die Workflows mit `actionlint` prüfen**

Run: `go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12 .github/workflows/*.yml`
Expected: keine Ausgabe, Exit 0. `actionlint` kennt den Scope `artifact-metadata` (ein Tippfehler wird als `unknown permission scope` gemeldet, das ist gegengeprüft).

- [ ] **Step 7: README berichtigen**

`README.md`, Abschnitt „Releases". Alt:

```
The binaries are not signed. On macOS, one downloaded through a browser is quarantined and refused
on first start; `xattr -d com.apple.quarantine recipe-reader` lifts that (`gh` does not set it).
```

Neu:

```
Every release is attested by GitHub, and the binaries and the image carry build provenance on top:
a Sigstore-signed statement of which workflow and which commit built them, checked against this
repository. Verify what you downloaded:

    gh release verify "$tag" -R sBurmester/recipe-reader                       # the release and its assets
    gh attestation verify "$file" -R sBurmester/recipe-reader                  # who built this binary
    gh attestation verify oci://ghcr.io/sburmester/recipe-reader:"$tag" -R sBurmester/recipe-reader

The binaries are not code-signed for the operating system. On macOS, one downloaded through a
browser is quarantined and refused on first start; `xattr -d com.apple.quarantine recipe-reader`
lifts that (`gh` does not set it).
```

- [ ] **Step 8: `AGENTS.md` nachziehen**

Im Abschnitt „Delivery, CI, releases and dependencies" an den Absatz zu den Releases anhängen. Alt:

```
  before its push), joins them under the tag and `latest` with a manifest, then publishes. The
  `<tag>-amd64`/`<tag>-arm64` tags stay on purpose. CI builds and smoke-tests the image on both.
  Releases are
```

Neu:

```
  before its push), joins them under the tag and `latest` with a manifest, then publishes. The
  `<tag>-amd64`/`<tag>-arm64` tags stay on purpose. CI builds and smoke-tests the image on both.
  The binaries are attested in the job that builds them and the manifest list in the job that
  joins it (`actions/attest`, keyless, verified with `gh attestation verify`); an attestation that
  fails blocks the publish. `release-please.yml` holds the permissions those jobs ask for, because a
  called workflow may not ask for more than its caller has. Releases are
```

Den Rest-Eintrag im Backlog streichen. Alt:

```
- Signing or provenance for release artifacts.
```

Neu: (Zeile entfernt; damit ist die Liste leer, siehe die Abschluss-Verifikation.)

`docs/superpowers/plans/2026-09-22-release-and-cleanup.md`, Backlog, vor den Eintrag „Signatur und Provenance" einfügen:

```
> **Erledigt (2026-09-25).** Der folgende Eintrag ist im Plan [2026-09-25-backlog-cleanup.md](2026-09-25-backlog-cleanup.md) umgesetzt (M4, Entscheidungen E4–E7). Bei der Prüfung zeigte sich, dass die Assets durch die unveränderlichen Releases schon von GitHub attestiert waren; ergänzt ist die Build-Provenance für die Binaries und das Image. Ein eigener Schlüssel oder `cosign` war nicht nötig.
```

- [ ] **Step 9: Commit-Gate und Commit**

```bash
git add .github README.md AGENTS.md docs
git commit -m "feat(ci): attest the release binaries and image" -m "The release assets were already attested by GitHub through immutable releases, but neither the binaries nor the image carried build provenance. The binaries are now attested in the job that builds them and the manifest list in the job that joins it, with actions/attest (keyless, Sigstore). A failing attestation blocks the publish. release-please.yml gains the permissions, because a called workflow cannot ask for more than its caller holds."
```

- [ ] **Step 10: Nach dem Merge das nächste echte Release prüfen** *(Freigabe des Nutzers: den Release-PR mergen)*

```bash
tag=$(gh release list -R sBurmester/recipe-reader --limit 1 --json tagName -q '.[0].tagName')
gh release download "$tag" -R sBurmester/recipe-reader -p "recipe-reader_${tag}_linux_amd64" -D /tmp/rr-check
gh release verify "$tag" -R sBurmester/recipe-reader
gh attestation verify "/tmp/rr-check/recipe-reader_${tag}_linux_amd64" -R sBurmester/recipe-reader
gh attestation verify "oci://ghcr.io/sburmester/recipe-reader:$tag" -R sBurmester/recipe-reader
```

Expected: alle drei Befehle melden ✓ (`Verification succeeded`). Für M4 ist das der Beleg; vor dem Merge des Release-PRs lautete dasselbe Kommando für die Binary noch `HTTP 404`.

Scheitert der Lauf (R1): das Release bleibt Entwurf, nichts Öffentliches ist kaputt. Häufigste Ursache ist ein fehlendes Recht im aufrufenden Job (R3); `gh run view <id> --log-failed` nennt sie. Korrigieren, mergen, dann `gh workflow run release-artifacts.yml -f tag=$tag`.

---

### Task 5: Die Entscheidung zum kong-Präfix umsetzen

Entschieden ist **O1**: das Präfix bleibt (Nutzer, 2026-09-25). Der Task ist deshalb reine Dokumentation: er hält die Form der Meldung im README fest, ersetzt in `AGENTS.md` die offene Frage durch die Entscheidung und schließt den Backlog-Eintrag. Am Code ändert sich nichts.

**Files:** `README.md`, `AGENTS.md`, `docs/superpowers/plans/2026-09-22-release-and-cleanup.md`

**Interfaces:** keine.

- [ ] **Step 1: Den Ist-Zustand festhalten**

```bash
go build -o /tmp/rr ./cmd/recipe-reader
env -i PATH="$PATH" HOME="$HOME" /tmp/rr serve --import-interval 0s; echo "exit=$?"
env -i PATH="$PATH" HOME="$HOME" /tmp/rr healthcheck --log-level banana; echo "exit=$?"
```

Expected: `… ERROR invalid command line error="serve: config: IMPORT_INTERVAL must be positive, got 0s"` und `… error="--log-level must be one of \"debug\",\"info\",\"warn\",\"error\" but got \"banana\""`, beide `exit=1`.

- [ ] **Step 2: Die Form im README festhalten**

`README.md`, Abschnitt „Commands", hinter den Absatz, der mit `and `fatal` for a command that ran and failed.` endet:

```
A rejected setting reads `<command>: config: <what is wrong>`, for example
`serve: config: IMPORT_INTERVAL must be positive, got 0s`. kong puts the command in front, and
names `serve` even when it was chosen by default and nobody typed it, so it tells you which
command's settings were checked; `config:` marks the check that refused the value. A flag or an enum
value that kong itself refuses, such as `--log-level banana`, carries no such prefix.
```

- [ ] **Step 3: `AGENTS.md` entscheiden**

Den offenen Punkt im Abschnitt „CLI and configuration" ersetzen. Alt:

```
- Open question, left to the user: whether kong's command-path prefix in error messages
  (`serve: config: API_TOKEN …`) helps operators.
```

Neu:

```
- kong's command-path prefix in error messages (`serve: config: API_TOKEN …`) stays. kong sets the
  `serve:` itself for every error from a `Validate()` method, with no option to turn it off, and it
  says which command's settings were checked even when `serve` was the default; `config:` is ours.
  Removing kong's part would mean rewriting the message in `main` or moving validation off the
  selected command's path (E7/E13 of the kong plan), neither of which is worth a cosmetic gain.
  Decided by the user on 2026-09-25.
```

Den Eintrag im Abschnitt „Open work and backlog" streichen:

```
- kong's command-path prefix in error messages needs a judgement from the user.
```

- [ ] **Step 4: Den Backlog-Eintrag im Release-Plan schließen**

`docs/superpowers/plans/2026-09-22-release-and-cleanup.md`, Backlog, vor den Eintrag „Kongs Kommandopfad-Präfix" einfügen:

```
> **Entschieden (2026-09-25).** Der folgende Eintrag ist im Plan [2026-09-25-backlog-cleanup.md](2026-09-25-backlog-cleanup.md) entschieden (M5, O1): das Präfix bleibt und ist im README beschrieben. Die Prüfung zeigte, dass `serve:` fest in kong steckt und `config:` unseres ist.
```

- [ ] **Step 5: Commit-Gate und Commit**

```bash
git add README.md AGENTS.md docs
git commit -m "docs: record the decision on kong's error prefix" -m "The prefix stays: kong sets the command path itself for every Validate() error and offers no switch, and it names the selected command even when serve was the default. The README now describes the shape of a rejected setting, and AGENTS.md records the decision in place of the open question."
```

---

### Abschluss-Verifikation

- [ ] **Step 1: Der Backlog ist leer, wo er leer sein soll**

Run: `sed -n '/^## Open work and backlog/,$p' AGENTS.md`
Expected: T-43 und T-24 unter „blocked on access"; der Unterabschnitt „Backlog, deliberately not scheduled" ist leer oder entfernt. Keine Zeile nennt mehr den Abbruch, das kong-Präfix, das Multi-Arch-Image, Signatur/Provenance oder den redundanten Index.

- [ ] **Step 2: Keine Zeile behauptet mehr „amd64 only" oder „not signed"**

Run: `grep -rnE 'amd64. only|are not signed' README.md AGENTS.md`
Expected: keine Treffer.

- [ ] **Step 3: Der Stand des neuesten Releases**

```bash
tag=$(gh release list -R sBurmester/recipe-reader --limit 1 --json tagName -q '.[0].tagName')
docker manifest inspect ghcr.io/sburmester/recipe-reader:$tag | grep -cE '"architecture": "(amd64|arm64)"'
gh attestation verify "oci://ghcr.io/sburmester/recipe-reader:$tag" -R sBurmester/recipe-reader | tail -1
```

Expected: `2` und ein ✓. Trägt das neueste Release noch keine Attestation, weil seit M4 keines erschien, ist die Definition of Done in diesem Punkt offen und wird beim nächsten Release nachgeholt.

- [ ] **Step 4: Die Definition of Done abhaken und den Plan als abgeschlossen vermerken**

---

## Teil D: Abdeckung des Backlogs

| Backlog-Eintrag (`AGENTS.md`) | Umgesetzt in |
| --- | --- |
| Ein aufgegebener Import endet erst nach `LLM_TIMEOUT` | Task 1 (Prämisse widerlegt; echte Lücke behoben) |
| kongs Kommandopfad-Präfix braucht ein Nutzerurteil | Task 5 (Entscheidung O1) |
| Multi-Arch-Image (`linux/arm64`) | Task 3 |
| Signatur und Provenance für Release-Artefakte | Task 4 |
| Der redundante Index `idx_recipe_ingredients_recipe_id` | Task 2 |
| Migrationen mit goose' No-Transaction-Modus (Release-Plan) | Task 2 (Kandidat geprüft und verworfen, E8; Stolperstein dokumentiert), kein eigener Task |
