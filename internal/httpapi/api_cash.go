package httpapi

import (
	"net/http"

	"usahainaja/backend/internal/app"
)

func (a *API) listCashAccounts(w http.ResponseWriter, r *http.Request) {
	businessID := businessFrom(r.Context()).ID
	items, err := a.service.ListCashAccounts(r.Context(), businessID)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, items)
}

func (a *API) createCashAccount(w http.ResponseWriter, r *http.Request) {
	var input app.CreateCashAccountInput
	if !decodeJSON(w, r, &input) {
		return
	}
	businessID := businessFrom(r.Context()).ID
	result, err := a.service.CreateCashAccount(r.Context(), businessID, input)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	writeData(w, http.StatusCreated, result)
}
