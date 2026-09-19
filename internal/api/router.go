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

	// Version is the build stamp reported by GET /api/healthz. Empty in tests
	// and in a plain `go build`, where the field is simply omitted from the
	// response rather than reported as an empty string.
	Version string
}

// NewRouter builds the API handler: a method-and-path ServeMux wrapped in the
// CORS, panic-recovery, logging, authentication, and JSON-write middleware,
// outermost first. Recovery sits inside CORS so that a 500 produced by a panic
// still carries the CORS headers the browser needs in order to read it, and
// both guards sit inside logging so a rejected write is still logged.
func NewRouter(deps Deps, sec Security) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/healthz", deps.handleHealth)

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

	return withCORS(sec.AllowedOrigins, withRecovery(withLogging(withAuth(sec.Token, withJSONWrites(mux)))))
}

// handleHealth answers a liveness probe. It deliberately touches no
// dependency, so it stays a signal that the process is up and serving rather
// than a database check.
//
// It also reports the build, which makes a running instance self-identifying
// over HTTP: for something that ships as one binary in one image, "what is
// actually deployed" otherwise has no answer short of hashing the file.
func (d Deps) handleHealth(w http.ResponseWriter, _ *http.Request) {
	resp := map[string]string{"status": "ok"}
	if d.Version != "" {
		resp["version"] = d.Version
	}
	writeJSON(w, http.StatusOK, resp)
}
