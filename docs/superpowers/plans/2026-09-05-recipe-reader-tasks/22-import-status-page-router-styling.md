> Part of the [Recipe Reader Implementation Plan](../2026-09-05-recipe-reader-implementation.md) — Phase 6: Frontend (Vanilla TS + Vite).
>
> **Status:** [ ] not started

# Task 22: Import Status Page, Router & Styling

**Files:**
- Create: `web/src/pages/import.ts`, `web/src/main.ts`, `web/src/style.css`

**Interfaces:**
- Consumes: `triggerImport`, `importStatus` (Task 19 `api.ts`); `el`, `clear` (Task 19 `dom.ts`); `renderListPage` (Task 20); `renderDetailPage` (Task 21).
- Produces: the app entry point. No later Go/TS task consumes this — it is the frontend composition root, mirroring `main.go` (Task 18) on the backend.

- [ ] **Step 1: Implement `pages/import.ts`**

```ts
// web/src/pages/import.ts
import { importStatus, triggerImport } from "../api";
import { clear, el } from "../dom";
import type { ImportStatus } from "../types";

export function renderImportPage(container: HTMLElement): void {
  clear(container);

  const statusEl = el("div", { class: "import-status" });
  const triggerBtn = el("button", {
    class: "primary",
    onclick: async () => {
      triggerBtn.setAttribute("disabled", "true");
      await triggerImport();
      poll();
    },
  }, ["Import jetzt starten"]);

  container.append(el("h1", {}, ["Instagram-Import"]), triggerBtn, statusEl);

  function renderStatus(s: ImportStatus): void {
    clear(statusEl);
    statusEl.append(
      el("p", {}, [s.running ? "Läuft…" : "Bereit."]),
      el("p", {}, [`Letzter Lauf: ${s.last_run || "noch nie"}`]),
      el("ul", {}, [
        el("li", {}, [`Gesehen: ${s.seen}`]),
        el("li", {}, [`Importiert: ${s.imported}`]),
        el("li", {}, [`Übersprungen (bereits vorhanden): ${s.skipped}`]),
        el("li", {}, [`Fehlgeschlagen: ${s.failed}`]),
      ]),
      ...(s.error ? [el("p", { class: "error" }, [`Fehler: ${s.error}`])] : []),
    );
    if (!s.running) triggerBtn.removeAttribute("disabled");
  }

  let timer: ReturnType<typeof setInterval> | undefined;
  async function poll(): Promise<void> {
    const s = await importStatus();
    renderStatus(s);
    if (s.running && !timer) {
      timer = setInterval(async () => {
        const latest = await importStatus();
        renderStatus(latest);
        if (!latest.running && timer) {
          clearInterval(timer);
          timer = undefined;
        }
      }, 2000);
    }
  }

  poll();
}
```

- [ ] **Step 2: Implement `main.ts` (hash router)**

```ts
// web/src/main.ts
import { renderDetailPage } from "./pages/detail";
import { renderImportPage } from "./pages/import";
import { renderListPage } from "./pages/list";

const app = document.getElementById("app")!;
const nav = document.createElement("nav");
nav.innerHTML = `<a href="#/">Rezepte</a> <a href="#/import">Import</a>`;
document.body.prepend(nav);

const content = document.createElement("main");
app.append(content);

function route(): void {
  const hash = window.location.hash || "#/";
  const detailMatch = hash.match(/^#\/recipes\/(\d+)$/);

  if (hash === "#/import") {
    renderImportPage(content);
  } else if (detailMatch) {
    renderDetailPage(content, Number(detailMatch[1]));
  } else {
    renderListPage(content);
  }
}

window.addEventListener("hashchange", route);
route();
```

- [ ] **Step 3: Implement `style.css`**

```css
/* web/src/style.css */
:root {
  color-scheme: light dark;
  font-family: system-ui, sans-serif;
}

body {
  margin: 0;
  padding: 0 1.5rem 3rem;
}

nav {
  display: flex;
  gap: 1rem;
  padding: 1rem 0;
  border-bottom: 1px solid color-mix(in srgb, currentColor 15%, transparent);
}

.toolbar {
  display: flex;
  gap: 0.75rem;
  margin-bottom: 1rem;
}

.recipe-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(220px, 1fr));
  gap: 1rem;
}

.recipe-card {
  display: block;
  border: 1px solid color-mix(in srgb, currentColor 15%, transparent);
  border-radius: 8px;
  padding: 1rem;
  text-decoration: none;
  color: inherit;
}

.badge {
  display: inline-block;
  font-size: 0.75rem;
  padding: 0.15rem 0.5rem;
  border-radius: 999px;
}

.badge-review {
  background: color-mix(in srgb, orange 25%, transparent);
}

.ingredient-row {
  display: flex;
  gap: 0.5rem;
  margin-bottom: 0.5rem;
}

.actions {
  display: flex;
  gap: 0.75rem;
  margin-top: 1rem;
}

button.primary {
  font-weight: 600;
}

button.danger {
  color: #b00020;
}

.error {
  color: #b00020;
}

.pagination {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  margin-top: 1rem;
}
```

- [ ] **Step 4: Typecheck and build**

```bash
npm --prefix web run typecheck
npm --prefix web run build
```

Expected: both succeed; `web/dist/` is created.

- [ ] **Step 5: Manual browser verification**

```bash
make run &                # backend on :8080
npm --prefix web run dev  # frontend dev server, proxies /api to :8080
```

Open `http://localhost:5173`. Verify: the recipe list loads (empty state renders correctly with zero recipes), category filter dropdown populates, search debounces, `#/import` shows the import status page and "Import jetzt starten" triggers a run (status will show `Fehler` since Instagram credentials aren't configured in dev — that's expected; the page must still render the error state cleanly, not crash). Create a recipe via `curl -X POST localhost:8080/api/recipes ...` and confirm it appears in the list and its detail/edit page loads, saves, and deletes correctly.

- [ ] **Step 6: Commit**

```bash
git add web/src/pages/import.ts web/src/main.ts web/src/style.css
git commit -m "$(cat <<'EOF'
feat: add import status page, hash router, and base styling

Assisted-by: Claude Sonnet 5 via Claude Code
EOF
)"
```


---

[← Task 21](21-recipe-detail-edit-page.md) · [Task 23 →](23-single-binary-packaging-go-embed-dockerfile-docker-compose.md) · [Back to plan](../2026-09-05-recipe-reader-implementation.md) · [Task index](README.md)
