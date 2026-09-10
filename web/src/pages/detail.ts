// web/src/pages/detail.ts
//
// The detail page doubles as the edit form: a reviewer opens a freshly imported
// recipe, corrects whatever the extractor got wrong, and saves. There is no
// separate read-only view, because every recipe that lands here via the
// needs_review queue is expected to be edited.
//
// Saving is a full replace (PUT), not a patch — see handleUpdateRecipe in
// internal/api/handlers_recipes.go. Every field the form does not edit
// (source, image_url, status, categories) must therefore still be sent back
// verbatim, or the write silently blanks it. Losing `source` in particular
// would be costly: the import pipeline uses it as the duplicate key, so a
// recipe without one gets re-imported forever.

import { deleteRecipe, getRecipe, listUnits, updateRecipe } from "../api";
import { clear, el } from "../dom";
import type { Ingredient, Recipe, Unit } from "../types";

export function renderDetailPage(container: HTMLElement, id: number): void {
  clear(container);
  container.append(el("p", {}, ["Lade Rezept…"]));

  // The unit list feeds the per-ingredient picker, so both requests have to
  // land before the form can be built.
  Promise.all([getRecipe(id), listUnits()])
    .then(([recipe, units]) => renderForm(container, recipe, units))
    .catch((err: unknown) => {
      clear(container);
      container.append(el("p", { class: "error" }, [`Fehler: ${message(err)}`]));
    });
}

function renderForm(container: HTMLElement, recipe: Recipe, units: Unit[]): void {
  clear(container);

  // `current` tracks the last state the server confirmed, so the untouched
  // fields resent on the next save — and the name in the delete prompt — stay
  // in sync after a successful write.
  let current: Recipe = recipe;

  const nameInput = el("input", {
    type: "text",
    value: current.name,
    "aria-label": "Rezeptname",
  });
  const instructionsInput = el("textarea", {
    rows: "8",
    "aria-label": "Zubereitung",
  }, [current.instructions]);
  const ingredientsList = el("div", { class: "ingredient-list" });
  const statusEl = el("p", { class: "status-line" });

  // Edited as a local copy: nothing is written back to `current.ingredients`
  // until the server accepts the save.
  let ingredients: Ingredient[] = current.ingredients.map((i) => ({ ...i }));

  function renderIngredients(): void {
    clear(ingredientsList);
    ingredients.forEach((ing, idx) => {
      const nameEl = el("input", { type: "text", value: ing.name, "aria-label": "Zutat" });
      const amountEl = el("input", {
        type: "number",
        value: String(ing.amount),
        step: "0.1",
        "aria-label": "Menge",
      });

      const unitSelect = el("select", { "aria-label": "Einheit" });
      // A recipe may legitimately have no unit ("1 Prise", "etwas Salz"), so the
      // empty option is a real choice rather than a placeholder.
      unitSelect.append(el("option", { value: "" }, ["–"]));
      for (const u of units) {
        const opt = el("option", { value: u.name }, [u.name]);
        if (u.name === ing.unit) opt.selected = true;
        unitSelect.append(opt);
      }

      // Each field writes straight through to the model, so add/remove can
      // re-render the whole list without collecting values from the DOM first.
      nameEl.addEventListener("input", () => (ingredients[idx].name = nameEl.value));
      amountEl.addEventListener("input", () => (ingredients[idx].amount = Number(amountEl.value)));
      unitSelect.addEventListener("change", () => (ingredients[idx].unit = unitSelect.value));

      const removeBtn = el("button", {
        type: "button",
        "aria-label": "Zutat entfernen",
        onclick: () => {
          ingredients = ingredients.filter((_, i) => i !== idx);
          renderIngredients();
        },
      }, ["✕"]);

      ingredientsList.append(
        el("div", { class: "ingredient-row" }, [amountEl, unitSelect, nameEl, removeBtn]),
      );
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
      // The API rejects a nameless recipe on create and would happily store an
      // empty name on update; catching it here keeps the list page readable.
      const name = nameInput.value.trim();
      if (name === "") {
        statusEl.textContent = "Fehler: Der Name darf nicht leer sein.";
        nameInput.focus();
        return;
      }

      statusEl.textContent = "Speichere…";
      try {
        const saved = await updateRecipe(current.id, {
          name,
          instructions: instructionsInput.value,
          // Rows the user added but never filled in are dropped rather than
          // stored as nameless ingredients.
          ingredients: ingredients.filter((i) => i.name.trim() !== ""),
          // Not editable here, but a PUT replaces the record, so they have to
          // be echoed back or they are lost.
          categories: current.categories,
          status: current.status,
          source: current.source,
          image_url: current.image_url,
        });
        // Adopt the server's view so the form shows what was actually stored —
        // in particular the blank rows filtered out above.
        current = saved;
        ingredients = saved.ingredients.map((i) => ({ ...i }));
        renderIngredients();
        statusEl.textContent = "Gespeichert.";
      } catch (err: unknown) {
        statusEl.textContent = `Fehler: ${message(err)}`;
      }
    },
  }, ["Speichern"]);

  const deleteBtn = el("button", {
    type: "button",
    class: "danger",
    onclick: async () => {
      if (!window.confirm(`"${current.name}" wirklich löschen?`)) return;
      statusEl.textContent = "Lösche…";
      try {
        await deleteRecipe(current.id);
      } catch (err: unknown) {
        // Without this the rejection would be swallowed and the page would
        // simply sit there, looking like the click did nothing.
        statusEl.textContent = `Fehler: ${message(err)}`;
        return;
      }
      // The recipe is gone, so there is nothing left to route back to.
      window.location.hash = "#/";
    },
  }, ["Löschen"]);

  container.append(
    el("a", { href: "#/" }, ["← Zurück zur Liste"]),
    el("h1", {}, [nameInput]),
    current.status === "needs_review" ? el("p", { class: "badge badge-review" }, ["zu prüfen"]) : "",
    el("h2", {}, ["Zutaten"]),
    ingredientsList,
    addIngredientBtn,
    el("h2", {}, ["Zubereitung"]),
    instructionsInput,
    el("div", { class: "actions" }, [saveBtn, deleteBtn]),
    statusEl,
  );
}

/** message extracts a displayable reason from a rejected fetch, which may throw
 * a plain value rather than an Error. */
function message(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}
