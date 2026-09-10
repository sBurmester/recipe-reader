> Part of the [Recipe Reader Implementation Plan](../2026-09-05-recipe-reader-implementation.md) — Phase 6: Frontend (Vanilla TS + Vite).
>
> **Status:** [x] done
>
> **Corrections made during implementation:**
> 1. **Pagination was permanently dead (real bug, fixed).** The plan writes `{ disabled: page <= 1 ? "true" : "" }`, but `dom.ts`'s `el()` forwards every string prop to `setAttribute`, and `setAttribute("disabled", "")` still disables the element — HTML keys off the attribute's *presence*, not its value. So both `‹` and `›` were disabled in every state. This typechecks perfectly, so Step 2 could never have caught it. Fixed by setting the DOM property (`next.disabled = page >= totalPages`) plus bounds guards in the handlers, and verified in a real headless browser with 25 recipes: page 1 renders prev disabled and next enabled.
> 2. **`(err as Error).message`** is an unchecked cast under `strict`'s `useUnknownInCatchVariables`; a non-`Error` rejection would render `Fehler: undefined`. Replaced with an `instanceof` check.
> 3. **`loadCategories()` had no error handling**, so a failing `/api/categories` became a silent unhandled rejection. Now caught — the filter stays at "Alle Kategorien" and search/pagination keep working.
> 4. **`ReturnType<typeof setTimeout>` is fragile here.** It resolves to `number` only because no `@types/node` is present; any future transitive dependency pulling it in would push the type toward `NodeJS.Timeout` and break `clearTimeout`. Changed to `window.setTimeout`/`window.clearTimeout` with `number | undefined`.
> 5. Dropped the redundant `as HTMLInputElement`/`as HTMLSelectElement` casts — `el` is generic over `HTMLElementTagNameMap` and already returns the precise type, so the casts only served to hide future real type errors.

# Task 20: Recipe List Page (Search, Filter, Pagination)

**Files:**
- Create: `web/src/pages/list.ts`

**Interfaces:**
- Consumes: `listRecipes`, `listCategories` (Task 19 `api.ts`); `el`, `clear` (Task 19 `dom.ts`); `Recipe`, `Category` (Task 19 `types.ts`).
- Produces: `export function renderListPage(container: HTMLElement): void`. Task 22 (`main.ts` router) calls this for the `#/` route.

- [x] **Step 1: Implement**

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

- [x] **Step 2: Typecheck**

Run: `npm --prefix web run typecheck`
Expected: no errors. (Manual browser verification happens in Task 22 once the router wires this page in.)

- [x] **Step 3: Commit**

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
