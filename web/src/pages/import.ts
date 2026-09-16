// web/src/pages/import.ts
import { importStatus, triggerImport } from "../api";
import { clear, el } from "../dom";
import type { ImportErrorCode, ImportStatus } from "../types";

const POLL_INTERVAL_MS = 2000;

// The status endpoint reports a failure as a stable code plus a fixed English
// sentence; it deliberately never sends the underlying error text, which can
// carry endpoint paths and connection-string fragments. Translating the code
// here is what keeps the page German without the server composing prose for it.
const IMPORT_ERROR_TEXT: Record<ImportErrorCode, string> = {
  rate_limited: "Instagram drosselt den Zugriff. Der Import wird nach der Wartezeit fortgesetzt.",
  instagram_auth: "Instagram hat die Anmeldung abgelehnt. Der nächste Import versucht es erneut.",
  instagram_schema_drift: "Instagram hat unerwartet geantwortet — der Importer muss angepasst werden.",
  fetch_failed: "Die gespeicherten Beiträge konnten nicht abgerufen werden.",
  cancelled: "Der Import wurde vorzeitig beendet.",
  import_failed: "Der letzte Import ist fehlgeschlagen.",
};

// An unknown code means the server has learned a failure this build has no
// wording for. Its own sentence is the better fallback than the bare code.
function importErrorText(s: ImportStatus): string | undefined {
  if (!s.error) return undefined;
  return IMPORT_ERROR_TEXT[s.error as ImportErrorCode] ?? s.error_message ?? s.error;
}

/** Go formats a zero time.Time as year 1, which is not a real "last run". */
const NEVER_RUN_PREFIX = "0001-01-01";

function formatLastRun(iso: string): string {
  if (!iso || iso.startsWith(NEVER_RUN_PREFIX)) return "noch nie";
  const parsed = new Date(iso);
  return Number.isNaN(parsed.getTime()) ? iso : parsed.toLocaleString("de-DE");
}

function messageOf(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

export function renderImportPage(container: HTMLElement): void {
  clear(container);

  const statusEl = el("div", { class: "import-status" });
  const triggerBtn = el("button", { class: "primary", onclick: () => void startImport() }, [
    "Import jetzt starten",
  ]);

  container.append(el("h1", {}, ["Instagram-Import"]), triggerBtn, statusEl);

  let timer: ReturnType<typeof setInterval> | undefined;

  function stopPolling(): void {
    if (timer !== undefined) {
      clearInterval(timer);
      timer = undefined;
    }
  }

  // Both import endpoints answer 503 when no Instagram credentials are
  // configured, which is the normal state in dev. Rendering that as a message
  // is the difference between an explained page and a blank one with a button
  // stuck disabled, so every await below is guarded.
  function renderError(text: string): void {
    clear(statusEl);
    statusEl.append(el("p", { class: "error" }, [`Fehler: ${text}`]));
    triggerBtn.removeAttribute("disabled");
  }

  function stateLine(s: ImportStatus): string {
    if (s.running) return "Läuft…";
    // A cooldown is not "Bereit": a trigger during one is refused with 429.
    if (s.cooldown_seconds) {
      return `Pausiert (Instagram-Ratelimit), weiter in ${Math.ceil(s.cooldown_seconds / 60)} Min.`;
    }
    return "Bereit.";
  }

  function renderStatus(s: ImportStatus): void {
    clear(statusEl);
    const errorText = importErrorText(s);
    statusEl.append(
      el("p", {}, [stateLine(s)]),
      el("p", {}, [`Letzter Lauf: ${formatLastRun(s.last_run)}`]),
      el("ul", {}, [
        el("li", {}, [`Gesehen: ${s.seen}`]),
        el("li", {}, [`Importiert: ${s.imported}`]),
        el("li", {}, [`Übersprungen (bereits vorhanden): ${s.skipped}`]),
        el("li", {}, [`Kein Rezept erkannt: ${s.no_recipe}`]),
        el("li", {}, [`Fehlgeschlagen: ${s.failed}`]),
      ]),
      // Only shown when it happened: a permanent "Degraded: 0" would be noise,
      // where a non-zero count is the signal that the LLM is not working.
      ...(s.degraded
        ? [el("p", { class: "error" }, [`${s.degraded} Rezept(e) nur regelbasiert extrahiert — LLM nicht erreichbar.`])]
        : []),
      ...(errorText ? [el("p", { class: "error" }, [`Fehler: ${errorText}`])] : []),
    );
    if (!s.running) triggerBtn.removeAttribute("disabled");
  }

  async function startImport(): Promise<void> {
    triggerBtn.setAttribute("disabled", "true");
    try {
      await triggerImport();
    } catch (err) {
      renderError(messageOf(err));
      return;
    }
    await poll();
  }

  async function poll(): Promise<void> {
    try {
      const s = await importStatus();
      renderStatus(s);
      if (s.running && timer === undefined) {
        timer = setInterval(() => void tick(), POLL_INTERVAL_MS);
      }
    } catch (err) {
      stopPolling();
      renderError(messageOf(err));
    }
  }

  async function tick(): Promise<void> {
    // The hash router swaps the container's contents without notifying the
    // page it replaced, so without this check a run started here would keep
    // polling for the life of the tab.
    if (!statusEl.isConnected) {
      stopPolling();
      return;
    }
    try {
      const latest = await importStatus();
      renderStatus(latest);
      if (!latest.running) stopPolling();
    } catch (err) {
      stopPolling();
      renderError(messageOf(err));
    }
  }

  void poll();
}
