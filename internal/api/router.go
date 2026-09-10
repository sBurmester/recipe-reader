// Package api exposes the recipe collection over HTTP. It owns no business
// logic of its own: handlers translate between JSON DTOs and the domain
// models, and delegate every decision to the repositories and the import
// worker injected through Deps. Keeping the transport layer that thin is what
// lets the handlers be tested against a real Postgres without a live server.
package api

import (
	"net/http"

	"github.com/sBurmester/recipe-reader/internal/pipeline"
	"github.com/sBurmester/recipe-reader/internal/repository"
)

// Deps carries everything the handlers need. Handler methods hang off this
// struct rather than off a server type so each one stays a plain
// http.HandlerFunc that a test can reach through the router.
//
// Worker may be nil when the import pipeline is not configured; the import
// handlers report that as 503 rather than panicking.
type Deps struct {
	Recipes repository.RecipeRepository
	Lookups repository.LookupRepository
	Worker  *pipeline.Worker
}

// NewRouter builds the API handler: a method-and-path ServeMux wrapped in the
// CORS, panic-recovery, and logging middleware, outermost first. Recovery sits
// inside CORS so that a 500 produced by a panic still carries the CORS headers
// the browser needs in order to read it.
func NewRouter(deps Deps) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/healthz", handleHealth)

	mux.HandleFunc("GET /api/recipes", deps.handleListRecipes)
	mux.HandleFunc("POST /api/recipes", deps.handleCreateRecipe)
	mux.HandleFunc("GET /api/recipes/{id}", deps.handleGetRecipe)
	mux.HandleFunc("PUT /api/recipes/{id}", deps.handleUpdateRecipe)
	mux.HandleFunc("DELETE /api/recipes/{id}", deps.handleDeleteRecipe)

	mux.HandleFunc("GET /api/categories", deps.handleListCategories)
	mux.HandleFunc("GET /api/units", deps.handleListUnits)
	mux.HandleFunc("GET /api/ingredients", deps.handleListIngredients)

	mux.HandleFunc("POST /api/import/run", deps.handleImportRun)
	mux.HandleFunc("GET /api/import/status", deps.handleImportStatus)

	return withCORS(withRecovery(withLogging(mux)))
}

// handleHealth answers a liveness probe. It deliberately touches no
// dependency, so it stays a signal that the process is up and serving rather
// than a database check.
func handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
