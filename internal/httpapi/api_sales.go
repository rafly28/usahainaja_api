package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"usahainaja/backend/internal/app"
)

func (a *API) listSales(w http.ResponseWriter, r *http.Request) {
	businessID := businessFrom(r.Context()).ID
	items, err := a.service.ListSales(r.Context(), businessID)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, items)
}

func (a *API) createSale(w http.ResponseWriter, r *http.Request) {
	var input app.NewSale
	if !decodeJSON(w, r, &input) {
		return
	}
	session := sessionFrom(r.Context())
	businessID := businessFrom(r.Context()).ID
	result, err := a.service.CreateSale(r.Context(), session, businessID, input)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	writeData(w, http.StatusCreated, result)
}

func (a *API) checkoutSale(w http.ResponseWriter, r *http.Request) {
	var input app.PaymentInput
	if !decodeJSON(w, r, &input) {
		return
	}
	session := sessionFrom(r.Context())
	businessID := businessFrom(r.Context()).ID
	receiptNumber := chi.URLParam(r, "number")

	result, err := a.service.CheckoutSale(r.Context(), session, businessID, receiptNumber, input)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, result)
}

func (a *API) voidSale(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Reason string `json:"reason"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	session := sessionFrom(r.Context())
	businessID := businessFrom(r.Context()).ID
	receiptNumber := chi.URLParam(r, "number")

	err := a.service.VoidSale(r.Context(), session, businessID, receiptNumber, input.Reason)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, map[string]bool{"success": true})
}
