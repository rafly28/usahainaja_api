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

type saleItemRequest struct {
	ProductCode string      `json:"product_code"`
	Quantity    decimalJSON `json:"quantity"`
	UnitPrice   decimalJSON `json:"unit_price"`
	Discount    decimalJSON `json:"discount"`
	Notes       string      `json:"notes,omitempty"`
}

type createSaleRequest struct {
	LocationCode  string            `json:"location_code"`
	CustomerCode  string            `json:"customer_code"`
	PaymentStatus string            `json:"payment_status,omitempty"`
	DiscountTotal decimalJSON       `json:"discount_total,omitempty"`
	TaxTotal      decimalJSON       `json:"tax_total,omitempty"`
	Notes         string            `json:"notes,omitempty"`
	Items         []saleItemRequest `json:"items"`
}

func (a *API) createSale(w http.ResponseWriter, r *http.Request) {
	var request createSaleRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	session := sessionFrom(r.Context())
	businessID := businessFrom(r.Context()).ID

	items := make([]app.NewSaleItem, len(request.Items))
	for i, it := range request.Items {
		items[i] = app.NewSaleItem{
			ProductCode: it.ProductCode,
			Quantity:    string(it.Quantity),
			UnitPrice:   string(it.UnitPrice),
			Discount:    string(it.Discount),
			Notes:       it.Notes,
		}
	}

	result, err := a.service.CreateSale(r.Context(), session, businessID, app.NewSale{
		LocationCode:  request.LocationCode,
		CustomerCode:  request.CustomerCode,
		PaymentStatus: request.PaymentStatus,
		DiscountTotal: string(request.DiscountTotal),
		TaxTotal:      string(request.TaxTotal),
		Notes:         request.Notes,
		Items:         items,
	})
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	writeData(w, http.StatusCreated, result)
}

func (a *API) checkoutSale(w http.ResponseWriter, r *http.Request) {
	var request paymentInputRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	session := sessionFrom(r.Context())
	businessID := businessFrom(r.Context()).ID
	receiptNumber := chi.URLParam(r, "number")

	result, err := a.service.CheckoutSale(r.Context(), session, businessID, receiptNumber, app.PaymentInput{
		CashAccountCode: request.CashAccountCode,
		Amount:          string(request.Amount),
		ReferenceNumber: request.ReferenceNumber,
		Notes:           request.Notes,
	})
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
