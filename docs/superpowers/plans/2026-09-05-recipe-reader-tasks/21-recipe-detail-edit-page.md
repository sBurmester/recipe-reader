> Part of the [Recipe Reader Implementation Plan](../2026-09-05-recipe-reader-implementation.md) — Phase 6: Frontend (Vanilla TS + Vite).
>
> **Status:** [x] done
>
> **Corrections made during implementation:**
> 1. **Delete had no error handling (real bug, fixed).** `await deleteRecipe(...)` sat in an async `onclick` with no `try`/`catch`, so a 404 or 500 became an unhandled rejection and the page simply looked as though the click did nothing. Now reports to the status line, with a progress message while in flight.
> 2. **Stale local state after save (real bug, fixed).** The plan filters blank ingredient rows out of the *payload* but never out of the local array, so a row the server dropped kept displaying as if it had been saved; the delete confirmation likewise kept showing the pre-rename name. Fixed by adopting the `Recipe` that `updateRecipe` returns and re-rendering.
> 3. **The plan's stated premise about validation is wrong, and the gap is real.** It assumes a cleared name would be rejected by the backend, but `handleUpdateRecipe` performs **no validation at all** — only `handleCreateRecipe` checks name/source. Since PUT is a full replace, a blank name would have been stored silently. A client-side guard now blocks the save. **The underlying API gap is unfixed and is worth a follow-up** — see the note in [Task 15](15-recipe-handlers-crud-search.md).
> 4. Dropped the no-op `as HTMLInputElement`/`as HTMLSelectElement`/`as HTMLOptionElement` casts (`el` already returns the precise element type), switched bare `confirm(...)` to `window.confirm(...)`, and added the `aria-label`s the plan omitted — it nests an `<input>` inside `<h1>` with no labelling anywhere. All class names are unchanged, since Task 22's stylesheet keys off them.

# Task 21: Recipe Detail/Edit Page

**Files:**
- Create: `web/src/pages/detail.ts`

**Interfaces:**
- Consumes: `getRecipe`, `updateRecipe`, `deleteRecipe`, `listUnits` (Task 19 `api.ts`); `el`, `clear` (Task 19 `dom.ts`); `Recipe`, `Ingredient`, `Unit` (Task 19 `types.ts`).
- Produces: `export function renderDetailPage(container: HTMLElement, id: number): void`. Task 22 calls this for the `#/recipes/:id` route.

- [x] **Step 1: Implement**

```ts
// web/src/pages/detail.ts
import { deleteRecipe, getRecipe, listUnits, updateRecipe } from "../api";
import { clear, el } from "../dom";
import type { Ingredient, Recipe, Unit } from "../types";

export function renderDetailPage(container: HTMLElement, id: number): void {
  clear(container);
  container.append(el("p", {}, ["Lade Rezept…"]));

  Promise.all([getRecipe(id), listUnits()])
    .then(([recipe, units]) => renderForm(container, recipe, units))
    .catch((err) => {
      clear(container);
      container.append(el("p", { class: "error" }, [`Fehler: ${(err as Error).message}`]));
    });
}

function renderForm(container: HTMLElement, recipe: Recipe, units: Unit[]): void {
  clear(container);

  const nameInput = el("input", { type: "text", value: recipe.name }) as HTMLInputElement;
  const instructionsInput = el("textarea", { rows: "8" }, [recipe.instructions]) as HTMLTextAreaElement;
  const ingredientsList = el("div", { class: "ingredient-list" });
  const statusEl = el("p", { class: "status-line" });

  let ingredients: Ingredient[] = recipe.ingredients.map((i) => ({ ...i }));

  function renderIngredients(): void {
    clear(ingredientsList);
    ingredients.forEach((ing, idx) => {
      const nameEl = el("input", { type: "text", value: ing.name }) as HTMLInputElement;
      const amountEl = el("input", { type: "number", value: String(ing.amount), step: "0.1" }) as HTMLInputElement;
      const unitSelect = el("select") as HTMLSelectElement;
      unitSelect.append(el("option", { value: "" }, ["–"]));
      for (const u of units) {
        const opt = el("option", { value: u.name }, [u.name]) as HTMLOptionElement;
        if (u.name === ing.unit) opt.selected = true;
        unitSelect.append(opt);
      }
      nameEl.addEventListener("input", () => (ingredients[idx].name = nameEl.value));
      amountEl.addEventListener("input", () => (ingredients[idx].amount = Number(amountEl.value)));
      unitSelect.addEventListener("change", () => (ingredients[idx].unit = unitSelect.value));

      const removeBtn = el("button", {
        type: "button",
        onclick: () => {
          ingredients = ingredients.filter((_, i) => i !== idx);
          renderIngredients();
        },
      }, ["✕"]);

      ingredientsList.append(el("div", { class: "ingredient-row" }, [amountEl, unitSelect, nameEl, removeBtn]));
    });
  }
  renderIngredients();

  const addIngredientBtn = el("button", {
    type: "button",
    onclick: () => {
      ingredients.push({ name: "", amount: 0, unit: "" });
      renderIngredients();
    },
  }, ["+ Zutat hinzufügen"]);

  const saveBtn = el("button", {
    type: "button",
    class: "primary",
    onclick: async () => {
      statusEl.textContent = "Speichere…";
      try {
        await updateRecipe(recipe.id, {
          name: nameInput.value,
          instructions: instructionsInput.value,
          ingredients: ingredients.filter((i) => i.name.trim() !== ""),
          categories: recipe.categories,
          status: recipe.status,
          source: recipe.source,
          image_url: recipe.image_url,
        });
        statusEl.textContent = "Gespeichert.";
      } catch (err) {
        statusEl.textContent = `Fehler: ${(err as Error).message}`;
      }
    },
  }, ["Speichern"]);

  const deleteBtn = el("button", {
    type: "button",
    class: "danger",
    onclick: async () => {
      if (!confirm(`"${recipe.name}" wirklich löschen?`)) return;
      await deleteRecipe(recipe.id);
      window.location.hash = "#/";
    },
  }, ["Löschen"]);

  container.append(
    el("a", { href: "#/" }, ["← Zurück zur Liste"]),
    el("h1", {}, [nameInput]),
    recipe.status === "needs_review" ? el("p", { class: "badge badge-review" }, ["zu prüfen"]) : "",
    el("h2", {}, ["Zutaten"]),
    ingredientsList,
    addIngredientBtn,
    el("h2", {}, ["Zubereitung"]),
    instructionsInput,
    el("div", { class: "actions" }, [saveBtn, deleteBtn]),
    statusEl,
  );
}
```

- [x] **Step 2: Typecheck**

Run: `npm --prefix web run typecheck`
Expected: no errors.

- [x] **Step 3: Commit**

```bash
git add web/src/pages/detail.ts
git commit -m "$(cat <<'EOF'
feat: add recipe detail/edit page with ingredient editing and delete

Assisted-by: Claude Sonnet 5 via Claude Code
EOF
)"
```


---

[← Task 20](20-recipe-list-page-search-filter-pagination.md) · [Task 22 →](22-import-status-page-router-styling.md) · [Back to plan](../2026-09-05-recipe-reader-implementation.md) · [Task index](README.md)
