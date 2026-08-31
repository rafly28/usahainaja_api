package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"usahainaja/backend/internal/app"
)

func (a *API) listCategories(w http.ResponseWriter, r *http.Request) {
	items, err := a.service.ListCategories(r.Context(), businessFrom(r.Context()).ID, r.URL.Query().Get("category_type"))
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, map[string]any{"items": items})
}

func (a *API) createCategory(w http.ResponseWriter, r *http.Request) {
	var input app.CreateCategoryInput
	if !decodeJSON(w, r, &input) {
		return
	}
	item, err := a.service.CreateCategory(r.Context(), sessionFrom(r.Context()), businessFrom(r.Context()), input)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	writeData(w, http.StatusCreated, item)
}

func (a *API) updateCategory(w http.ResponseWriter, r *http.Request) {
	var input app.UpdateCategoryInput
	if !decodeJSON(w, r, &input) {
		return
	}
	item, err := a.service.UpdateCategory(r.Context(), sessionFrom(r.Context()), businessFrom(r.Context()), chi.URLParam(r, "code"), input)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, item)
}
