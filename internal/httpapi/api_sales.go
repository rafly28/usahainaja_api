package httpapi

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"usahainaja/backend/internal/app"
)

func (a *API) listSales(w http.ResponseWriter, r *http.Request) {
	page, limit := paginationParams(r)
	items, total, err := a.service.ListSales(r.Context(), businessFrom(r.Context()).ID, page, limit)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, map[string]any{"items": items, "pagination": map[string]int{"page": page, "limit": limit, "total": total}})
}

func (a *API) getSale(w http.ResponseWriter, r *http.Request) {
	sale, err := a.service.GetSale(r.Context(), businessFrom(r.Context()).ID, chi.URLParam(r, "number"))
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, sale)
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
	DiscountTotal decimalJSON       `json:"discount_total,omitempty"`
	TaxTotal      decimalJSON       `json:"tax_total,omitempty"`
	Notes         string            `json:"notes,omitempty"`
	Items         []saleItemRequest `json:"items"`
}

func toNewSaleItems(items []saleItemRequest) []app.NewSaleItem {
	result := make([]app.NewSaleItem, len(items))
	for i, it := range items {
		result[i] = app.NewSaleItem{
			ProductCode: it.ProductCode,
			Quantity:    string(it.Quantity),
			UnitPrice:   string(it.UnitPrice),
			Discount:    string(it.Discount),
			Notes:       it.Notes,
		}
	}
	return result
}

func (a *API) createSale(w http.ResponseWriter, r *http.Request) {
	var request createSaleRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	result, err := a.service.CreateSale(r.Context(), sessionFrom(r.Context()), businessFrom(r.Context()), app.NewSale{
		LocationCode:  request.LocationCode,
		CustomerCode:  request.CustomerCode,
		DiscountTotal: string(request.DiscountTotal),
		TaxTotal:      string(request.TaxTotal),
		Notes:         request.Notes,
		Items:         toNewSaleItems(request.Items),
	})
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	writeData(w, http.StatusCreated, result)
}

func (a *API) addSaleItem(w http.ResponseWriter, r *http.Request) {
	var request saleItemRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	result, err := a.service.AddSaleItem(r.Context(), sessionFrom(r.Context()), businessFrom(r.Context()), chi.URLParam(r, "number"), toNewSaleItems([]saleItemRequest{request})[0])
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	writeData(w, http.StatusCreated, result)
}

func (a *API) updateSaleItem(w http.ResponseWriter, r *http.Request) {
	line, err := strconv.Atoi(chi.URLParam(r, "line"))
	if err != nil || line < 1 {
		writeAppError(w, r, &app.Error{Code: "INVALID_REQUEST", Message: "Nomor baris tidak valid."})
		return
	}
	var request saleItemRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	result, err := a.service.UpdateSaleItem(r.Context(), sessionFrom(r.Context()), businessFrom(r.Context()), chi.URLParam(r, "number"), line, toNewSaleItems([]saleItemRequest{request})[0])
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, result)
}

func (a *API) deleteSaleItem(w http.ResponseWriter, r *http.Request) {
	line, err := strconv.Atoi(chi.URLParam(r, "line"))
	if err != nil || line < 1 {
		writeAppError(w, r, &app.Error{Code: "INVALID_REQUEST", Message: "Nomor baris tidak valid."})
		return
	}
	if err := a.service.DeleteSaleItem(r.Context(), sessionFrom(r.Context()), businessFrom(r.Context()), chi.URLParam(r, "number"), line); err != nil {
		writeAppError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, map[string]string{"status": "DELETED"})
}

func (a *API) cancelSale(w http.ResponseWriter, r *http.Request) {
	if err := a.service.CancelSale(r.Context(), sessionFrom(r.Context()), businessFrom(r.Context()), chi.URLParam(r, "number")); err != nil {
		writeAppError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, map[string]string{"status": "CANCELLED"})
}

func (a *API) checkoutSale(w http.ResponseWriter, r *http.Request) {
	var request paymentInputRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	result, err := a.service.CheckoutSale(r.Context(), sessionFrom(r.Context()), businessFrom(r.Context()), chi.URLParam(r, "number"), app.PaymentInput{
		CashAccountCode: request.CashAccountCode,
		PaymentMethod:   request.PaymentMethodCode,
		Amount:          string(request.Amount),
		PaidAt:          request.PaidAt,
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
	receiptNumber := chi.URLParam(r, "number")

	err := a.service.VoidSale(r.Context(), session, businessFrom(r.Context()), receiptNumber, input.Reason)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, map[string]string{"status": "VOIDED", "receipt_number": receiptNumber})
}
