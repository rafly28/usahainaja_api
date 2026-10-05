package httpapi

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"usahainaja/backend/internal/app"
)

func (a *API) listPurchases(w http.ResponseWriter, r *http.Request) {
	page, limit := paginationParams(r)
	items, total, err := a.service.ListPurchases(r.Context(), businessFrom(r.Context()).ID, page, limit)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, map[string]any{"items": items, "pagination": map[string]int{"page": page, "limit": limit, "total": total}})
}

func (a *API) getPurchase(w http.ResponseWriter, r *http.Request) {
	purchase, err := a.service.GetPurchase(r.Context(), businessFrom(r.Context()).ID, chi.URLParam(r, "number"))
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, purchase)
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
	DiscountTotal   decimalJSON           `json:"discount_total,omitempty"`
	TaxTotal        decimalJSON           `json:"tax_total,omitempty"`
	Notes           string                `json:"notes,omitempty"`
	Items           []purchaseItemRequest `json:"items"`
}

type paymentInputRequest struct {
	CashAccountCode   string      `json:"cash_account_code"`
	PaymentMethodCode string      `json:"payment_method_code,omitempty"`
	Amount            decimalJSON `json:"amount"`
	PaidAt            string      `json:"paid_at,omitempty"`
	ReferenceNumber   string      `json:"reference_number,omitempty"`
	Notes             string      `json:"notes,omitempty"`
}

type receiveItemRequest struct {
	LineNumber       int         `json:"line_number"`
	ReceivedQuantity decimalJSON `json:"received_quantity"`
}

type receivePurchaseRequest struct {
	ReceivedAt      string               `json:"received_at,omitempty"`
	ReferenceNumber string               `json:"reference_number,omitempty"`
	Items           []receiveItemRequest `json:"items"`
}

func toNewPurchaseItems(items []purchaseItemRequest) []app.NewPurchaseItem {
	result := make([]app.NewPurchaseItem, len(items))
	for i, it := range items {
		result[i] = app.NewPurchaseItem{
			ProductCode: it.ProductCode,
			Quantity:    string(it.Quantity),
			UnitPrice:   string(it.UnitPrice),
			Discount:    string(it.Discount),
			Notes:       it.Notes,
		}
	}
	return result
}

func (a *API) createPurchase(w http.ResponseWriter, r *http.Request) {
	var request createPurchaseRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	result, err := a.service.CreatePurchase(r.Context(), sessionFrom(r.Context()), businessFrom(r.Context()), app.NewPurchase{
		LocationCode:    request.LocationCode,
		SupplierCode:    request.SupplierCode,
		ReferenceNumber: request.ReferenceNumber,
		DiscountTotal:   string(request.DiscountTotal),
		TaxTotal:        string(request.TaxTotal),
		Notes:           request.Notes,
		Items:           toNewPurchaseItems(request.Items),
	})
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	writeData(w, http.StatusCreated, result)
}

func (a *API) addPurchaseItem(w http.ResponseWriter, r *http.Request) {
	var request purchaseItemRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	result, err := a.service.AddPurchaseItem(r.Context(), sessionFrom(r.Context()), businessFrom(r.Context()), chi.URLParam(r, "number"), toNewPurchaseItems([]purchaseItemRequest{request})[0])
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	writeData(w, http.StatusCreated, result)
}

func (a *API) updatePurchaseItem(w http.ResponseWriter, r *http.Request) {
	line, err := strconv.Atoi(chi.URLParam(r, "line"))
	if err != nil || line < 1 {
		writeAppError(w, r, &app.Error{Code: "INVALID_REQUEST", Message: "Nomor baris tidak valid."})
		return
	}
	var request purchaseItemRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	result, err := a.service.UpdatePurchaseItem(r.Context(), sessionFrom(r.Context()), businessFrom(r.Context()), chi.URLParam(r, "number"), line, toNewPurchaseItems([]purchaseItemRequest{request})[0])
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, result)
}

func (a *API) deletePurchaseItem(w http.ResponseWriter, r *http.Request) {
	line, err := strconv.Atoi(chi.URLParam(r, "line"))
	if err != nil || line < 1 {
		writeAppError(w, r, &app.Error{Code: "INVALID_REQUEST", Message: "Nomor baris tidak valid."})
		return
	}
	if err := a.service.DeletePurchaseItem(r.Context(), sessionFrom(r.Context()), businessFrom(r.Context()), chi.URLParam(r, "number"), line); err != nil {
		writeAppError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, map[string]string{"status": "DELETED"})
}

func (a *API) orderPurchase(w http.ResponseWriter, r *http.Request) {
	if err := a.service.OrderPurchase(r.Context(), sessionFrom(r.Context()), businessFrom(r.Context()), chi.URLParam(r, "number")); err != nil {
		writeAppError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, map[string]string{"status": "ORDERED"})
}

func (a *API) cancelPurchase(w http.ResponseWriter, r *http.Request) {
	if err := a.service.CancelPurchase(r.Context(), sessionFrom(r.Context()), businessFrom(r.Context()), chi.URLParam(r, "number")); err != nil {
		writeAppError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, map[string]string{"status": "CANCELLED"})
}

func (a *API) receivePurchase(w http.ResponseWriter, r *http.Request) {
	number := chi.URLParam(r, "number")
	if number == "" {
		writeAppError(w, r, &app.Error{Code: "INVALID_REQUEST", Message: "Nomor pembelian tidak valid."})
		return
	}
	var request receivePurchaseRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	items := make([]app.ReceiveItemInput, len(request.Items))
	for i, it := range request.Items {
		items[i] = app.ReceiveItemInput{LineNumber: it.LineNumber, ReceivedQuantity: string(it.ReceivedQuantity)}
	}
	receipt, err := a.service.ReceivePurchase(r.Context(), sessionFrom(r.Context()), businessFrom(r.Context()), number, app.ReceivePurchaseInput{
		ReceivedAt:      request.ReceivedAt,
		ReferenceNumber: request.ReferenceNumber,
		Items:           items,
	})
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	writeData(w, http.StatusCreated, receipt)
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
	result, err := a.service.PayPurchase(r.Context(), sessionFrom(r.Context()), businessFrom(r.Context()), number, app.PaymentInput{
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
	writeData(w, http.StatusCreated, result)
}
