package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"

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

func (a *API) receivePurchase(w http.ResponseWriter, r *http.Request) {
	number := chi.URLParam(r, "number")
	if number == "" {
		writeAppError(w, r, &app.Error{Code: "INVALID_REQUEST", Message: "Nomor pembelian tidak valid."})
		return
	}
	session := sessionFrom(r.Context())
	businessID := businessFrom(r.Context()).ID

	err := a.service.ReceivePurchase(r.Context(), session, businessID, number)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, map[string]string{"status": "COMPLETED"})
}

func (a *API) payPurchase(w http.ResponseWriter, r *http.Request) {
	number := chi.URLParam(r, "number")
	if number == "" {
		writeAppError(w, r, &app.Error{Code: "INVALID_REQUEST", Message: "Nomor pembelian tidak valid."})
		return
	}
	var input app.PaymentInput
	if !decodeJSON(w, r, &input) {
		return
	}
	session := sessionFrom(r.Context())
	businessID := businessFrom(r.Context()).ID

	result, err := a.service.PayPurchase(r.Context(), session, businessID, number, input)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	writeData(w, http.StatusCreated, result)
}
