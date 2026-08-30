package httpapi

import (
	"net/http"

	"usahainaja/backend/internal/app"
)

func (a *API) listPurchases(w http.ResponseWriter, r *http.Request) {
	businessID := businessFrom(r.Context()).ID
	items, err := a.service.ListPurchases(r.Context(), businessID)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, items)
}

func (a *API) createPurchase(w http.ResponseWriter, r *http.Request) {
	var input app.NewPurchase
	if !decodeJSON(w, r, &input) {
		return
	}
	session := sessionFrom(r.Context())
	businessID := businessFrom(r.Context()).ID
	result, err := a.service.CreatePurchase(r.Context(), session, businessID, input)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	writeData(w, http.StatusCreated, result)
}
