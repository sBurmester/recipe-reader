// web/src/pages/list.ts
//
// The `#/` route: a searchable, category-filtered, paginated list of recipes.
// State (query, category, page) lives in this closure rather than in the URL —
// Task 22's router only dispatches on the hash path, so re-entering the route
// starts from a clean first page.

import { listCategories, listRecipes } from "../api";
import { clear, el } from "../dom";
import type { Category, Recipe } from "../types";

const PAGE_SIZE = 20;
const DEBOUNCE_MS = 300;

export function renderListPage(container: HTMLElement): void {
  clear(container);

  let query = "";
  let categoryId: number | undefined;
  let page = 1;

  const searchInput = el("input", {
    type: "search",
    placeholder: "Rezepte durchsuchen…",
    "aria-label": "Rezepte durchsuchen",
  });
  const categorySelect = el("select", { "aria-label": "Kategorie filtern" }, []);
  categorySelect.append(el("option", { value: "" }, ["Alle Kategorien"]));

  const resultsEl = el("div", { class: "recipe-grid" });
  const paginationEl = el("div", { class: "pagination" });

  const toolbar = el("div", { class: "toolbar" }, [searchInput, categorySelect]);
  container.append(el("h1", {}, ["Rezepte"]), toolbar, resultsEl, paginationEl);

  async function loadCategories(): Promise<void> {
    // A failing category list must not take the recipe list down with it: the
    // filter simply stays at "Alle Kategorien".
    try {
      const categories: Category[] = await listCategories();
      for (const c of categories) {
        categorySelect.append(el("option", { value: String(c.id) }, [c.name]));
      }
    } catch {
      // Intentionally ignored — search and pagination still work.
    }
  }

  async function loadResults(): Promise<void> {
    clear(resultsEl);
    clear(paginationEl);
    resultsEl.append(el("p", {}, ["Lade…"]));
    try {
      const { recipes, total } = await listRecipes({
        q: query,
        categoryId,
        page,
        pageSize: PAGE_SIZE,
      });
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
      const message = err instanceof Error ? err.message : String(err);
      resultsEl.append(el("p", { class: "error" }, [`Fehler: ${message}`]));
    }
  }

  function renderCard(r: Recipe): HTMLElement {
    const children: (Node | string)[] = [
      el("h3", {}, [r.name]),
      el("p", {}, [r.categories.map((c) => c.name).join(", ") || "—"]),
    ];
    if (r.status === "needs_review") {
      children.push(el("span", { class: "badge badge-review" }, ["zu prüfen"]));
    }
    return el("a", { href: `#/recipes/${r.id}`, class: "recipe-card" }, children);
  }

  function renderPagination(total: number): void {
    clear(paginationEl);
    const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE));

    // `disabled` is set as a property, not an attribute: `el` forwards every
    // string prop to setAttribute, and setAttribute("disabled", "") still
    // disables a button — so an attribute-based flag can never be turned off.
    const prev = el("button", {
      type: "button",
      "aria-label": "Vorherige Seite",
      onclick: () => {
        if (page <= 1) return;
        page--;
        void loadResults();
      },
    });
    prev.append("‹");
    prev.disabled = page <= 1;

    const next = el("button", {
      type: "button",
      "aria-label": "Nächste Seite",
      onclick: () => {
        if (page >= totalPages) return;
        page++;
        void loadResults();
      },
    });
    next.append("›");
    next.disabled = page >= totalPages;

    paginationEl.append(
      prev,
      el("span", {}, [`Seite ${page} von ${totalPages} (${total} Rezepte)`]),
      next,
    );
  }

  let debounce: number | undefined;
  searchInput.addEventListener("input", () => {
    window.clearTimeout(debounce);
    debounce = window.setTimeout(() => {
      query = searchInput.value;
      page = 1;
      void loadResults();
    }, DEBOUNCE_MS);
  });

  categorySelect.addEventListener("change", () => {
    categoryId = categorySelect.value ? Number(categorySelect.value) : undefined;
    page = 1;
    void loadResults();
  });

  void loadCategories();
  void loadResults();
}
