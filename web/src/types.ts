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

/** The failure codes GET /api/import/status can report in its `error` field. */
export type ImportErrorCode =
  | "rate_limited"
  | "instagram_auth"
  | "instagram_schema_drift"
  | "fetch_failed"
  | "cancelled"
  | "import_failed";

export interface ImportStatus {
  running: boolean;
  last_run: string;
  seen: number;
  imported: number;
  skipped: number;
  /** Captions that carried no recipe — not a failure, and not a duplicate. */
  no_recipe: number;
  /** Of `imported`, how many fell back to the rules because the LLM failed. */
  degraded: number;
  failed: number;
  /**
   * Stable classification of the last run's failure — never the server's error
   * text, which can carry endpoint paths and connection-string fragments. See
   * IMPORT_ERROR_TEXT in pages/import.ts for the German wording per code.
   */
  error?: ImportErrorCode | string;
  /** Fixed English sentence from the server, used when `error` is unknown here. */
  error_message?: string;
  /** Present only while an Instagram rate limit is being waited out. */
  cooldown_until?: string;
  cooldown_seconds?: number;
}
