package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/sBurmester/recipe-reader/internal/domain"
	"github.com/sBurmester/recipe-reader/internal/repository"
)

// maxRecipeBodyBytes caps a recipe write. An Instagram caption is at most 2,200
// characters, so 1 MiB is several hundred times the largest recipe this service
// ever stores — generous for a hand-entered one, and small enough that a
// multi-gigabyte body cannot be decoded into memory until the process is killed.
const maxRecipeBodyBytes = 1 << 20

// handleListRecipes serves the search endpoint. Every filter is optional and a
// malformed numeric parameter is ignored rather than rejected, so a stale
// bookmark or a half-typed URL still returns results instead of a 400. The
// repository clamps page/page_size, so out-of-range values need no check here.
func (d Deps) handleListRecipes(w http.ResponseWriter, r *http.Request) {
	q := repository.SearchQuery{Text: r.URL.Query().Get("q")}
	if v := r.URL.Query().Get("category_id"); v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			q.CategoryID = &id
		}
	}
	if v := r.URL.Query().Get("status"); v != "" {
		status := domain.RecipeStatus(v)
		q.Status = &status
	}
	if v := r.URL.Query().Get("page"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			q.Page = p
		}
	}
	if v := r.URL.Query().Get("page_size"); v != "" {
		if ps, err := strconv.Atoi(v); err == nil {
			q.PageSize = ps
		}
	}

	recipes, total, err := d.Recipes.Search(r.Context(), q)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "search failed")
		return
	}

	// An empty result must marshal as [] rather than null, so the frontend can
	// map over it without a nil check.
	dtos := make([]RecipeDTO, 0, len(recipes))
	for _, rec := range recipes {
		dtos = append(dtos, toRecipeDTO(rec))
	}
	writeJSON(w, http.StatusOK, map[string]any{"recipes": dtos, "total": total})
}

func (d Deps) handleGetRecipe(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	recipe, err := d.Recipes.GetByID(r.Context(), id)
	if errors.Is(err, repository.ErrNotFound) {
		writeError(w, http.StatusNotFound, "recipe not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "get failed")
		return
	}
	writeJSON(w, http.StatusOK, toRecipeDTO(*recipe))
}

// handleCreateRecipe stores a manually entered recipe. Name and Source are the
// only required fields: Source doubles as the pipeline's duplicate key, so a
// recipe without one could be re-imported forever.
func (d Deps) handleCreateRecipe(w http.ResponseWriter, r *http.Request) {
	var dto RecipeDTO
	if !decodeRecipeBody(w, r, &dto) {
		return
	}
	if dto.Name == "" || dto.Source == "" {
		writeError(w, http.StatusBadRequest, "name and source are required")
		return
	}
	// A hand-entered recipe needs no review — it was typed by the reviewer.
	if dto.Status == "" {
		dto.Status = string(domain.StatusPublished)
	}
	if !domain.RecipeStatus(dto.Status).Valid() {
		writeError(w, http.StatusBadRequest, errInvalidStatus)
		return
	}

	recipe, err := d.dtoToRecipe(r, dto)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve ingredients/categories")
		return
	}
	if err := d.Recipes.Create(r.Context(), recipe); err != nil {
		writeError(w, http.StatusInternalServerError, "create failed")
		return
	}
	writeJSON(w, http.StatusCreated, toRecipeDTO(*recipe))
}

// errInvalidStatus is the 400 message for a status outside the domain's two
// values. Migration 0002 enforces the same rule in the schema; checking here
// as well turns a constraint violation into a message the client can act on.
const errInvalidStatus = `status must be "needs_review" or "published"`

// handleUpdateRecipe replaces a recipe wholesale — the body is the new state,
// not a patch, which is also how the repository writes associations.
//
// Being a replacement is why it requires a name and a legal status rather than
// defaulting them the way create does: an omitted field here means "set it to
// empty", and a PUT without a status used to persist exactly that. Source is
// not required — UpdateRecipe never writes it.
func (d Deps) handleUpdateRecipe(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var dto RecipeDTO
	if !decodeRecipeBody(w, r, &dto) {
		return
	}
	if dto.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if !domain.RecipeStatus(dto.Status).Valid() {
		writeError(w, http.StatusBadRequest, errInvalidStatus)
		return
	}

	recipe, err := d.dtoToRecipe(r, dto)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve ingredients/categories")
		return
	}
	// The path wins over any id in the body, so a copy-pasted payload cannot
	// overwrite a different recipe.
	recipe.ID = id
	if err := d.Recipes.Update(r.Context(), recipe); errors.Is(err, repository.ErrNotFound) {
		writeError(w, http.StatusNotFound, "recipe not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "update failed")
		return
	}
	writeJSON(w, http.StatusOK, toRecipeDTO(*recipe))
}

func (d Deps) handleDeleteRecipe(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := d.Recipes.Delete(r.Context(), id); errors.Is(err, repository.ErrNotFound) {
		writeError(w, http.StatusNotFound, "recipe not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "delete failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// decodeRecipeBody decodes the request body into dto, reading at most
// maxRecipeBodyBytes of it, and reports whether the handler should carry on.
// On failure it has already written the response.
//
// An over-long body answers 413 rather than the 400 a malformed one gets: the
// client sent nothing wrong in kind, only too much of it, and telling the two
// apart is what lets it fix the right thing. http.MaxBytesReader also marks the
// connection to be closed after the response, so a client cannot keep streaming
// the rest of the body into the same socket.
func decodeRecipeBody(w http.ResponseWriter, r *http.Request, dto *RecipeDTO) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxRecipeBodyBytes)
	err := json.NewDecoder(r.Body).Decode(dto)
	if err == nil {
		return true
	}
	if _, tooLarge := errors.AsType[*http.MaxBytesError](err); tooLarge {
		writeError(w, http.StatusRequestEntityTooLarge, "request body exceeds the 1 MiB limit")
		return false
	}
	writeError(w, http.StatusBadRequest, "invalid JSON body")
	return false
}

// dtoToRecipe resolves the DTO's free-text category, ingredient, and unit
// names to lookup rows, creating them on first sight — the same contract the
// import pipeline uses, so a hand-entered recipe and an imported one end up
// sharing the same lookup rows instead of duplicating them.
//
// The resolved names are copied back onto the domain model's read-side fields
// as well. The repository ignores them on write, but it means the response
// built from this value echoes the ingredient and unit names the client sent
// rather than blanks.
func (d Deps) dtoToRecipe(r *http.Request, dto RecipeDTO) (*domain.Recipe, error) {
	recipe := &domain.Recipe{
		Name: dto.Name, Instructions: dto.Instructions, ImageURL: dto.ImageURL,
		Source: dto.Source, Status: domain.RecipeStatus(dto.Status),
	}
	for _, cat := range dto.Categories {
		c, err := d.Lookups.FindOrCreateCategory(r.Context(), cat.Name)
		if err != nil {
			return nil, err
		}
		recipe.Categories = append(recipe.Categories, *c)
	}
	for _, ing := range dto.Ingredients {
		ingredient, err := d.Lookups.FindOrCreateIngredient(r.Context(), ing.Name)
		if err != nil {
			return nil, err
		}
		ri := domain.RecipeIngredient{
			IngredientID:   ingredient.ID,
			IngredientName: ingredient.Name,
			Amount:         ing.Amount,
		}
		if ing.Unit != "" {
			unit, err := d.Lookups.FindOrCreateUnit(r.Context(), ing.Unit)
			if err != nil {
				return nil, err
			}
			ri.UnitID = &unit.ID
			ri.UnitName = unit.Name
		}
		recipe.Ingredients = append(recipe.Ingredients, ri)
	}
	return recipe, nil
}

// parseIDParam reads the {id} wildcard the mux captured from the path.
func parseIDParam(r *http.Request) (int64, error) {
	return strconv.ParseInt(r.PathValue("id"), 10, 64)
}
