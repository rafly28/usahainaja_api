package httpapi

import (
	"github.com/go-chi/chi/v5"
	"net/http"
	"usahainaja/backend/internal/app"
)

func (a *API) listUnits(w http.ResponseWriter, r *http.Request) {
	items, err := a.service.ListUnits(r.Context(), businessFrom(r.Context()).ID)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, map[string]any{"items": items})
}
func (a *API) createUnit(w http.ResponseWriter, r *http.Request) {
	var input app.CreateUnitInput
	if !decodeJSON(w, r, &input) {
		return
	}
	item, err := a.service.CreateUnit(r.Context(), sessionFrom(r.Context()), businessFrom(r.Context()), input)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	writeData(w, http.StatusCreated, item)
}
func (a *API) updateUnit(w http.ResponseWriter, r *http.Request) {
	var input app.UpdateUnitInput
	if !decodeJSON(w, r, &input) {
		return
	}
	item, err := a.service.UpdateUnit(r.Context(), sessionFrom(r.Context()), businessFrom(r.Context()), chi.URLParam(r, "code"), input)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, item)
}
