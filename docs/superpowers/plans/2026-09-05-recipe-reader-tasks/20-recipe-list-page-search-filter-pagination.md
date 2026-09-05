> Part of the [Recipe Reader Implementation Plan](../2026-09-05-recipe-reader-implementation.md) — Phase 6: Frontend (Vanilla TS + Vite).
>
> **Status:** [ ] not started

# Task 20: Recipe List Page (Search, Filter, Pagination)

**Files:**
- Create: `web/src/pages/list.ts`

**Interfaces:**
- Consumes: `listRecipes`, `listCategories` (Task 19 `api.ts`); `el`, `clear` (Task 19 `dom.ts`); `Recipe`, `Category` (Task 19 `types.ts`).
- Produces: `export function renderListPage(container: HTMLElement): void`. Task 22 (`main.ts` router) calls this for the `#/` route.

- [ ] **Step 1: Implement**

```ts
// web/src/pages/list.ts
import { listCategories, listRecipes } from "../api";
import { clear, el } from "../dom";
import type { Category, Recipe } from "../types";

export function renderListPage(container: HTMLElement): void {
  clear(container);

  let query = "";
  let categoryId: number | undefined;
  let page = 1;
  const pageSize = 20;

  const searchInput = el("input", { type: "search", placeholder: "Rezepte durchsuchen…" }) as HTMLInputElement;
  const categorySelect = el("select") as HTMLSelectElement;
  categorySelect.append(el("option", { value: "" }, ["Alle Kategorien"]));

  const resultsEl = el("div", { class: "recipe-grid" });
  const paginationEl = el("div", { class: "pagination" });

  const form = el("div", { class: "toolbar" }, [searchInput, categorySelect]);
  container.append(el("h1", {}, ["Rezepte"]), form, resultsEl, paginationEl);

  async function loadCategories(): Promise<void> {
    const categories: Category[] = await listCategories();
    for (const c of categories) {
      categorySelect.append(el("option", { value: String(c.id) }, [c.name]));
    }
  }

  async function loadResults(): Promise<void> {
    clear(resultsEl);
    resultsEl.append(el("p", {}, ["Lade…"]));
    try {
      const { recipes, total } = await listRecipes({ q: query, categoryId, page, pageSize });
      clear(resultsEl);
      if (recipes.length === 0) {
        resultsEl.append(el("p", {}, ["Keine Rezepte gefunden."]));
      }
      for (const r of recipes) {
        resultsEl.append(renderCard(r));
      }
      renderPagination(total);
    } catch (err) {
      clear(resultsEl);
      resultsEl.append(el("p", { class: "error" }, [`Fehler: ${(err as Error).message}`]));
    }
  }

  function renderCard(r: Recipe): HTMLElement {
    const badge = r.status === "needs_review" ? el("span", { class: "badge badge-review" }, ["zu prüfen"]) : "";
    return el("a", { href: `#/recipes/${r.id}`, class: "recipe-card" }, [
      el("h3", {}, [r.name]),
      el("p", {}, [r.categories.map((c) => c.name).join(", ") || "—"]),
      ...(badge ? [badge] : []),
    ]);
  }

  function renderPagination(total: number): void {
    clear(paginationEl);
    const totalPages = Math.max(1, Math.ceil(total / pageSize));
    paginationEl.append(
      el("button", { disabled: page <= 1 ? "true" : "", onclick: () => { page--; loadResults(); } }, ["‹"]),
      el("span", {}, [`Seite ${page} von ${totalPages} (${total} Rezepte)`]),
      el("button", { disabled: page >= totalPages ? "true" : "", onclick: () => { page++; loadResults(); } }, ["›"]),
    );
  }

  let debounce: ReturnType<typeof setTimeout>;
  searchInput.addEventListener("input", () => {
    clearTimeout(debounce);
    debounce = setTimeout(() => {
      query = searchInput.value;
      page = 1;
      loadResults();
    }, 300);
  });
  categorySelect.addEventListener("change", () => {
    categoryId = categorySelect.value ? Number(categorySelect.value) : undefined;
    page = 1;
    loadResults();
  });

  loadCategories();
  loadResults();
}
```

- [ ] **Step 2: Typecheck**

Run: `npm --prefix web run typecheck`
Expected: no errors. (Manual browser verification happens in Task 22 once the router wires this page in.)

- [ ] **Step 3: Commit**

```bash
git add web/src/pages/list.ts
git commit -m "$(cat <<'EOF'
feat: add recipe list page with search, category filter, and pagination

Assisted-by: Claude Sonnet 5 via Claude Code
EOF
)"
```


---

[← Task 19](19-vite-scaffold-api-client-shared-types.md) · [Task 21 →](21-recipe-detail-edit-page.md) · [Back to plan](../2026-09-05-recipe-reader-implementation.md) · [Task index](README.md)
