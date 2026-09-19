package api

import "net/http"

// The lookup endpoints return bare JSON arrays rather than an envelope: they
// are unpaginated reference data that the frontend loads once to fill its
// pickers, so there is no total or cursor worth carrying. Each builds its
// slice with make(..., 0, n) so an empty table marshals as [] and not null.

func (d Deps) handleListCategories(w http.ResponseWriter, r *http.Request) {
	categories, err := d.Lookups.ListCategories(r.Context())
	if err != nil {
		writeInternalError(w, r, "failed to list categories", err)
		return
	}
	dtos := make([]CategoryDTO, 0, len(categories))
	for _, c := range categories {
		dtos = append(dtos, CategoryDTO{ID: c.ID, Name: c.Name})
	}
	writeJSON(w, http.StatusOK, dtos)
}

func (d Deps) handleListUnits(w http.ResponseWriter, r *http.Request) {
	units, err := d.Lookups.ListUnits(r.Context())
	if err != nil {
		writeInternalError(w, r, "failed to list units", err)
		return
	}
	dtos := make([]UnitDTO, 0, len(units))
	for _, u := range units {
		dtos = append(dtos, UnitDTO{ID: u.ID, Name: u.Name})
	}
	writeJSON(w, http.StatusOK, dtos)
}

func (d Deps) handleListIngredients(w http.ResponseWriter, r *http.Request) {
	ingredients, err := d.Lookups.ListIngredients(r.Context())
	if err != nil {
		writeInternalError(w, r, "failed to list ingredients", err)
		return
	}
	dtos := make([]IngredientLookupDTO, 0, len(ingredients))
	for _, i := range ingredients {
		dtos = append(dtos, IngredientLookupDTO{ID: i.ID, Name: i.Name})
	}
	writeJSON(w, http.StatusOK, dtos)
}
