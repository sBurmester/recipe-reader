// web/src/api.ts
//
// Thin fetch wrapper over the Go REST API. Paths are relative, so in dev the
// Vite proxy forwards /api to localhost:8080 and in production the same
// origin serves both the embedded UI and the API.

import { getApiToken, setApiToken } from "./auth";
import type { Category, ImportStatus, Recipe, Unit } from "./types";

/**
 * headers builds the request headers.
 *
 * Content-Type is always application/json, and the backend now requires it on
 * every write: it is not a CORS-"simple" value, so demanding it forces a
 * preflight the server can refuse by origin. Dropping it would make writes
 * from this app fail with 415.
 */
function headers(): HeadersInit {
  const token = getApiToken();
  return {
    "Content-Type": "application/json",
    ...(token ? { Authorization: `Bearer ${token}` } : {}),
  };
}

async function send(path: string, init?: RequestInit): Promise<Response> {
  return fetch(path, { headers: headers(), ...init });
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  let res = await send(path, init);

  // A 401 means the deployment has API_TOKEN set and this browser does not
  // have it yet (or has a stale one). Asking once and retrying keeps the token
  // out of the bundle — it is the operator's credential, entered here rather
  // than compiled in. window.prompt is blunt, but this app has no modal layer
  // and the alternative is a write that silently fails.
  if (res.status === 401) {
    const entered = window.prompt("API-Token erforderlich:", "");
    if (entered) {
      setApiToken(entered);
      res = await send(path, init);
    }
  }

  if (!res.ok) {
    // The API reports failures as {"error": "..."}; fall back to the status
    // text when the body is missing or not JSON.
    const body = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(body.error ?? `request to ${path} failed with ${res.status}`);
  }
  // DELETE answers 204 with an empty body, which res.json() would choke on.
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

export function listRecipes(
  params: SearchParams = {},
): Promise<{ recipes: Recipe[]; total: number }> {
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
