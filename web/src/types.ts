// web/src/types.ts
//
// These mirror the Go API DTOs in internal/api/dto.go field-for-field. If
// either side changes, change both — nothing enforces the correspondence at
// compile time, so a silent drift here shows up as undefined values in the UI.

export interface Ingredient {
  name: string;
  amount: number;
  unit: string;
}

export interface Category {
  id: number;
  name: string;
}

export interface Unit {
  id: number;
  name: string;
}

export type RecipeStatus = "needs_review" | "published";

export interface Recipe {
  id: number;
  name: string;
  instructions: string;
  image_url: string;
  source: string;
  status: RecipeStatus;
  ingredients: Ingredient[];
  categories: Category[];
}

export interface ImportStatus {
  running: boolean;
  last_run: string;
  seen: number;
  imported: number;
  skipped: number;
  /** Captions that carried no recipe — not a failure, and not a duplicate. */
  no_recipe: number;
  failed: number;
  error?: string;
}
