package httpapi

import (
	"net/http"

	"usahainaja/backend/internal/app"
)

func (a *API) listContacts(w http.ResponseWriter, r *http.Request) {
	businessID := businessFrom(r.Context()).ID
	items, err := a.service.ListContacts(r.Context(), businessID)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, items)
}

func (a *API) createContact(w http.ResponseWriter, r *http.Request) {
	var input app.CreateContactInput
	if !decodeJSON(w, r, &input) {
		return
	}
	businessID := businessFrom(r.Context()).ID
	result, err := a.service.CreateContact(r.Context(), businessID, input)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	writeData(w, http.StatusCreated, result)
}
