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

type purchaseItemRequest struct {
	ProductCode string      `json:"product_code"`
	Quantity    decimalJSON `json:"quantity"`
	UnitPrice   decimalJSON `json:"unit_price"`
	Discount    decimalJSON `json:"discount"`
	Notes       string      `json:"notes,omitempty"`
}

type createPurchaseRequest struct {
	LocationCode    string                `json:"location_code"`
	SupplierCode    string                `json:"supplier_code"`
	ReferenceNumber string                `json:"reference_number,omitempty"`
	PaymentStatus   string                `json:"payment_status,omitempty"`
	DiscountTotal   decimalJSON           `json:"discount_total,omitempty"`
	TaxTotal        decimalJSON           `json:"tax_total,omitempty"`
	Notes           string                `json:"notes,omitempty"`
	Items           []purchaseItemRequest `json:"items"`
}

type paymentInputRequest struct {
	CashAccountCode string      `json:"cash_account_code"`
	Amount          decimalJSON `json:"amount"`
	ReferenceNumber string      `json:"reference_number,omitempty"`
	Notes           string      `json:"notes,omitempty"`
}

func (a *API) createPurchase(w http.ResponseWriter, r *http.Request) {
	var request createPurchaseRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	session := sessionFrom(r.Context())
	businessID := businessFrom(r.Context()).ID

	items := make([]app.NewPurchaseItem, len(request.Items))
	for i, it := range request.Items {
		items[i] = app.NewPurchaseItem{
			ProductCode: it.ProductCode,
			Quantity:    string(it.Quantity),
			UnitPrice:   string(it.UnitPrice),
			Discount:    string(it.Discount),
			Notes:       it.Notes,
		}
	}

	result, err := a.service.CreatePurchase(r.Context(), session, businessID, app.NewPurchase{
		LocationCode:    request.LocationCode,
		SupplierCode:    request.SupplierCode,
		ReferenceNumber: request.ReferenceNumber,
		PaymentStatus:   request.PaymentStatus,
		DiscountTotal:   string(request.DiscountTotal),
		TaxTotal:        string(request.TaxTotal),
		Notes:           request.Notes,
		Items:           items,
	})
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
	var request paymentInputRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	session := sessionFrom(r.Context())
	businessID := businessFrom(r.Context()).ID

	result, err := a.service.PayPurchase(r.Context(), session, businessID, number, app.PaymentInput{
		CashAccountCode: request.CashAccountCode,
		Amount:          string(request.Amount),
		ReferenceNumber: request.ReferenceNumber,
		Notes:           request.Notes,
	})
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	writeData(w, http.StatusCreated, result)
}
