package httpapi

import (
	"net/http"
	"usahainaja/backend/internal/app"
)

func (a *API) listParties(w http.ResponseWriter, r *http.Request) {
	items, err := a.service.ListParties(r.Context(), businessFrom(r.Context()).ID)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, map[string]any{"items": items})
}
func (a *API) createParty(w http.ResponseWriter, r *http.Request) {
	var input app.CreatePartyInput
	if !decodeJSON(w, r, &input) {
		return
	}
	item, err := a.service.CreateParty(r.Context(), sessionFrom(r.Context()), businessFrom(r.Context()), input)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	writeData(w, http.StatusCreated, item)
}
