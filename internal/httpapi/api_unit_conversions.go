package httpapi

import (
	"net/http"
	"usahainaja/backend/internal/app"
)

func (a *API) listUnitConversions(w http.ResponseWriter, r *http.Request) {
	items, err := a.service.ListUnitConversions(r.Context(), businessFrom(r.Context()).ID)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, map[string]any{"items": items})
}
func (a *API) createUnitConversion(w http.ResponseWriter, r *http.Request) {
	var input app.CreateUnitConversionInput
	if !decodeJSON(w, r, &input) {
		return
	}
	item, err := a.service.CreateUnitConversion(r.Context(), sessionFrom(r.Context()), businessFrom(r.Context()), input)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	writeData(w, http.StatusCreated, item)
}
