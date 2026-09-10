> Part of the [Recipe Reader Implementation Plan](../2026-09-05-recipe-reader-implementation.md) — Phase 6: Frontend (Vanilla TS + Vite).
>
> **Status:** [x] done
>
> **Notes from implementation:**
> 1. **Dependency versions bumped to current majors:** TypeScript `^7.0.0` (7.0.2) and Vite `^8.0.0` (8.3.0), rather than the plan's `^5.6.0` / `^6.0.0`, which were two majors stale. Validated empirically — install clean with 0 vulnerabilities, `tsc --noEmit` clean, `tsc -b` build mode works on this non-composite project, and `vite build` produces `dist/`. No fallback needed.
> 2. `web/dist-ts/` (the `tsc -b` output) was added to `.gitignore`; only `web/dist/` was covered before.
> 3. **Verified against the shipped Go DTOs, all matching:** `RecipeDTO`/`IngredientDTO`/`CategoryDTO`/`UnitDTO` JSON tags, the lookup endpoints returning bare arrays, the import status shape, and `q` as the free-text search parameter. Notably DELETE really does answer `204 No Content`, which is exactly why `request()`'s `status === 204` short-circuit is required — `res.json()` would otherwise throw on the empty body.
> 4. A documentation note was added to `el()` in `dom.ts` about boolean attributes — see the bug recorded in [Task 20](20-recipe-list-page-search-filter-pagination.md).

# Task 19: Vite Scaffold, API Client & Shared Types

**Files:**
- Create: `web/package.json`, `web/vite.config.ts`, `web/tsconfig.json`, `web/index.html`
- Create: `web/src/types.ts`, `web/src/api.ts`, `web/src/dom.ts`

**Interfaces:**
- Produces (mirrors the Go API DTOs from Task 15/16 field-for-field — keep these two files in sync if either side changes):

```ts
export interface Ingredient { name: string; amount: number; unit: string }
export interface Category { id: number; name: string }
export interface Unit { id: number; name: string }
export interface Recipe {
  id: number; name: string; instructions: string; image_url: string;
  source: string; status: "needs_review" | "published";
  ingredients: Ingredient[]; categories: Category[];
}

// api.ts
export function listRecipes(params): Promise<{recipes: Recipe[], total: number}>
export function getRecipe(id: number): Promise<Recipe>
export function createRecipe(r: Partial<Recipe>): Promise<Recipe>
export function updateRecipe(id: number, r: Partial<Recipe>): Promise<Recipe>
export function deleteRecipe(id: number): Promise<void>
export function listCategories(): Promise<Category[]>
export function listUnits(): Promise<Unit[]>
export function triggerImport(): Promise<void>
export function importStatus(): Promise<ImportStatus>
```

Task 20 (list page), Task 21 (detail page), and Task 22 (import page) consume every function in `api.ts` and the `el()` helper from `dom.ts` — do not rename any of them later.

- [x] **Step 1: Scaffold the Vite project**

```bash
mkdir -p web/src/pages
```

`web/package.json`:

```json
{
  "name": "recipe-reader-web",
  "private": true,
  "type": "module",
  "scripts": {
    "dev": "vite",
    "build": "tsc -b && vite build",
    "typecheck": "tsc --noEmit"
  },
  "devDependencies": {
    "typescript": "^5.6.0",
    "vite": "^6.0.0"
  }
}
```

`web/vite.config.ts`:

```ts
import { defineConfig } from "vite";

export default defineConfig({
  server: {
    proxy: {
      "/api": "http://localhost:8080",
    },
  },
  build: {
    outDir: "dist",
  },
});
```

`web/tsconfig.json`:

```json
{
  "compilerOptions": {
    "target": "ES2022",
    "module": "ESNext",
    "moduleResolution": "Bundler",
    "strict": true,
    "outDir": "dist-ts",
    "rootDir": "src",
    "skipLibCheck": true
  },
  "include": ["src"]
}
```

`web/index.html`:

```html
<!doctype html>
<html lang="de">
  <head>
    <meta charset="UTF-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1.0" />
    <title>Recipe Reader</title>
    <link rel="stylesheet" href="/src/style.css" />
  </head>
  <body>
    <div id="app"></div>
    <script type="module" src="/src/main.ts"></script>
  </body>
</html>
```

- [x] **Step 2: Install dependencies**

```bash
npm --prefix web install
```

- [x] **Step 3: Implement `types.ts`**

```ts
// web/src/types.ts
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
  failed: number;
  error?: string;
}
```

- [x] **Step 4: Implement `dom.ts`**

```ts
// web/src/dom.ts
type Props = Record<string, string | ((ev: Event) => void)>;

export function el<K extends keyof HTMLElementTagNameMap>(
  tag: K,
  props: Props = {},
  children: (Node | string)[] = [],
): HTMLElementTagNameMap[K] {
  const node = document.createElement(tag);
  for (const [key, value] of Object.entries(props)) {
    if (key.startsWith("on") && typeof value === "function") {
      node.addEventListener(key.slice(2).toLowerCase(), value as EventListener);
    } else if (typeof value === "string") {
      node.setAttribute(key, value);
    }
  }
  for (const child of children) {
    node.append(typeof child === "string" ? document.createTextNode(child) : child);
  }
  return node;
}

export function clear(node: Element): void {
  while (node.firstChild) node.removeChild(node.firstChild);
}
```

- [x] **Step 5: Implement `api.ts`**

```ts
// web/src/api.ts
import type { Category, ImportStatus, Recipe, Unit } from "./types";

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, {
    headers: { "Content-Type": "application/json" },
    ...init,
  });
  if (!res.ok) {
    const body = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(body.error ?? `request to ${path} failed with ${res.status}`);
  }
  if (res.status === 204) return undefined as T;
  return res.json() as Promise<T>;
}

export interface SearchParams {
  q?: string;
  categoryId?: number;
  status?: string;
  page?: number;
  pageSize?: number;
}

export function listRecipes(params: SearchParams = {}): Promise<{ recipes: Recipe[]; total: number }> {
  const usp = new URLSearchParams();
  if (params.q) usp.set("q", params.q);
  if (params.categoryId) usp.set("category_id", String(params.categoryId));
  if (params.status) usp.set("status", params.status);
  if (params.page) usp.set("page", String(params.page));
  if (params.pageSize) usp.set("page_size", String(params.pageSize));
  return request(`/api/recipes?${usp.toString()}`);
}

export function getRecipe(id: number): Promise<Recipe> {
  return request(`/api/recipes/${id}`);
}

export function createRecipe(r: Partial<Recipe>): Promise<Recipe> {
  return request("/api/recipes", { method: "POST", body: JSON.stringify(r) });
}

export function updateRecipe(id: number, r: Partial<Recipe>): Promise<Recipe> {
  return request(`/api/recipes/${id}`, { method: "PUT", body: JSON.stringify(r) });
}

export function deleteRecipe(id: number): Promise<void> {
  return request(`/api/recipes/${id}`, { method: "DELETE" });
}

export function listCategories(): Promise<Category[]> {
  return request("/api/categories");
}

export function listUnits(): Promise<Unit[]> {
  return request("/api/units");
}

export function triggerImport(): Promise<void> {
  return request("/api/import/run", { method: "POST" });
}

export function importStatus(): Promise<ImportStatus> {
  return request("/api/import/status");
}
```

- [x] **Step 6: Verify it typechecks**

Run: `npm --prefix web run typecheck`
Expected: no errors (there are no pages/main.ts yet, but `types.ts`, `dom.ts`, `api.ts` compile standalone).

- [x] **Step 7: Commit**

```bash
git add web/package.json web/vite.config.ts web/tsconfig.json web/index.html web/src/types.ts web/src/api.ts web/src/dom.ts
git commit -m "$(cat <<'EOF'
feat: scaffold Vite + TypeScript frontend with API client and DOM helper

Assisted-by: Claude Sonnet 5 via Claude Code
EOF
)"
```


---

[← Task 18](18-main-go-wiring-graceful-shutdown.md) · [Task 20 →](20-recipe-list-page-search-filter-pagination.md) · [Back to plan](../2026-09-05-recipe-reader-implementation.md) · [Task index](README.md)
