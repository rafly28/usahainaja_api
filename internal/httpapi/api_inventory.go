package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"usahainaja/backend/internal/app"
)

type openingStockRequest struct {
	LocationCode string                  `json:"location_code"`
	Reason       string                  `json:"reason"`
	Items        []openingStockItemInput `json:"items"`
}

type openingStockItemInput struct {
	ProductCode string      `json:"product_code"`
	Quantity    decimalJSON `json:"quantity"`
	UnitSymbol  string      `json:"unit_symbol,omitempty"`
}

func (a *API) openingStock(w http.ResponseWriter, r *http.Request) {
	var request openingStockRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	items := make([]app.OpeningStockItemInput, 0, len(request.Items))
	for _, item := range request.Items {
		items = append(items, app.OpeningStockItemInput{
			ProductCode: item.ProductCode,
			Quantity:    string(item.Quantity),
			UnitSymbol:  item.UnitSymbol,
		})
	}
	result, err := a.service.CreateOpeningStock(r.Context(), sessionFrom(r.Context()), businessFrom(r.Context()).ID, app.OpeningStockInput{
		LocationCode: request.LocationCode,
		Reason:       request.Reason,
		Items:        items,
	})
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	writeData(w, http.StatusCreated, result)
}

func (a *API) listInventoryProducts(w http.ResponseWriter, r *http.Request) {
	page, limit := paginationParams(r)
	query := r.URL.Query()
	items, total, err := a.service.ListInventoryProducts(r.Context(), businessFrom(r.Context()).ID, app.InventoryFilter{
		Search:       query.Get("search"),
		LocationCode: query.Get("location"),
		LowStockOnly: query.Get("low_stock") == "true",
		Page:         page,
		Limit:        limit,
	})
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, map[string]any{
		"items":      items,
		"pagination": map[string]int{"page": page, "limit": limit, "total": total},
	})
}

func (a *API) listStockMovements(w http.ResponseWriter, r *http.Request) {
	page, limit := paginationParams(r)
	query := r.URL.Query()
	movements, total, err := a.service.ListStockMovements(r.Context(), businessFrom(r.Context()).ID, app.StockMovementFilter{
		ProductCode:  query.Get("product_public_code"),
		LocationCode: query.Get("location"),
		MovementType: query.Get("movement_type"),
		DateFrom:     query.Get("date_from"),
		DateTo:       query.Get("date_to"),
		Page:         page,
		Limit:        limit,
	})
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, map[string]any{
		"movements":  movements,
		"pagination": map[string]int{"page": page, "limit": limit, "total": total},
	})
}

func (a *API) listStockAdjustments(w http.ResponseWriter, r *http.Request) {
	page, limit := paginationParams(r)
	query := r.URL.Query()
	adjustments, total, err := a.service.ListStockAdjustments(r.Context(), businessFrom(r.Context()).ID, app.StockAdjustmentFilter{
		Status: query.Get("status"),
		Kind:   query.Get("adjustment_kind"),
		Page:   page,
		Limit:  limit,
	})
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, map[string]any{
		"items":      adjustments,
		"pagination": map[string]int{"page": page, "limit": limit, "total": total},
	})
}

func (a *API) getStockAdjustment(w http.ResponseWriter, r *http.Request) {
	adjustment, err := a.service.GetStockAdjustment(r.Context(), businessFrom(r.Context()).ID, chi.URLParam(r, "number"))
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, adjustment)
}

func (a *API) createStockAdjustment(w http.ResponseWriter, r *http.Request) {
	var in app.NewStockAdjustment
	if !decodeJSON(w, r, &in) {
		return
	}
	result, err := a.service.CreateStockAdjustment(r.Context(), sessionFrom(r.Context()), businessFrom(r.Context()).ID, in)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	writeData(w, http.StatusCreated, result)
}

func (a *API) completeStockAdjustment(w http.ResponseWriter, r *http.Request) {
	err := a.service.CompleteStockAdjustment(r.Context(), sessionFrom(r.Context()), businessFrom(r.Context()).ID, chi.URLParam(r, "number"))
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, map[string]string{"status": "COMPLETED"})
}

func (a *API) createStockOpname(w http.ResponseWriter, r *http.Request) {
	var in app.NewStockOpname
	if !decodeJSON(w, r, &in) {
		return
	}
	result, err := a.service.CreateStockOpname(r.Context(), sessionFrom(r.Context()), businessFrom(r.Context()).ID, in)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	writeData(w, http.StatusCreated, result)
}

func (a *API) listStockOpnames(w http.ResponseWriter, r *http.Request) {
	page, limit := paginationParams(r)
	adjustments, total, err := a.service.ListStockAdjustments(r.Context(), businessFrom(r.Context()).ID, app.StockAdjustmentFilter{
		Kind:  "OPNAME",
		Page:  page,
		Limit: limit,
	})
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, map[string]any{
		"items":      adjustments,
		"pagination": map[string]int{"page": page, "limit": limit, "total": total},
	})
}

func (a *API) getStockOpname(w http.ResponseWriter, r *http.Request) {
	adjustment, err := a.service.GetStockAdjustment(r.Context(), businessFrom(r.Context()).ID, chi.URLParam(r, "number"))
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, adjustment)
}

// completeStockOpname memakai alur complete yang sama dengan adjustment biasa
// karena opname disimpan pada tabel stock_adjustments dengan kind OPNAME.
func (a *API) completeStockOpname(w http.ResponseWriter, r *http.Request) {
	a.completeStockAdjustment(w, r)
}

// saleReceipt dan purchaseReceipt menyajikan dokumen transaksi memakai nomor publik.
func (a *API) saleReceipt(w http.ResponseWriter, r *http.Request) {
	sale, err := a.service.GetSale(r.Context(), businessFrom(r.Context()).ID, chi.URLParam(r, "number"))
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, map[string]any{"receipt": sale})
}

func (a *API) purchaseReceipt(w http.ResponseWriter, r *http.Request) {
	purchase, err := a.service.GetPurchase(r.Context(), businessFrom(r.Context()).ID, chi.URLParam(r, "number"))
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, map[string]any{"receipt": purchase})
}
