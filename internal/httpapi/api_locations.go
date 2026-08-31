package httpapi

import (
	"github.com/go-chi/chi/v5"
	"net/http"
	"usahainaja/backend/internal/app"
)

func (a *API) listLocations(w http.ResponseWriter, r *http.Request) {
	items, err := a.service.ListLocations(r.Context(), businessFrom(r.Context()).ID)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, map[string]any{"items": items})
}
func (a *API) createLocation(w http.ResponseWriter, r *http.Request) {
	var input app.CreateLocationInput
	if !decodeJSON(w, r, &input) {
		return
	}
	item, err := a.service.CreateLocation(r.Context(), sessionFrom(r.Context()), businessFrom(r.Context()), input)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	writeData(w, http.StatusCreated, item)
}
func (a *API) updateLocation(w http.ResponseWriter, r *http.Request) {
	var input app.UpdateLocationInput
	if !decodeJSON(w, r, &input) {
		return
	}
	item, err := a.service.UpdateLocation(r.Context(), sessionFrom(r.Context()), businessFrom(r.Context()), chi.URLParam(r, "code"), input)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, item)
}
