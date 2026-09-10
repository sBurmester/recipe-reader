// web/src/api.ts
//
// Thin fetch wrapper over the Go REST API. Paths are relative, so in dev the
// Vite proxy forwards /api to localhost:8080 and in production the same
// origin serves both the embedded UI and the API.

import type { Category, ImportStatus, Recipe, Unit } from "./types";

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, {
    headers: { "Content-Type": "application/json" },
    ...init,
  });
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
