package httpapi

import (
	"net/http"
	"usahainaja/backend/internal/app"
)

func (a *API) listUnitConversions(w http.ResponseWriter, r *http.Request) {
	items, err := a.service.ListUnitConversions(r.Context(), businessFrom(r.Context()).ID)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, map[string]any{"items": items})
}
type createUnitConversionRequest struct {
	ProductCode  string      `json:"product_code"`
	FromUnitCode string      `json:"from_unit_code"`
	ToUnitCode   string      `json:"to_unit_code"`
	Multiplier   decimalJSON `json:"multiplier"`
}

func (a *API) createUnitConversion(w http.ResponseWriter, r *http.Request) {
	var request createUnitConversionRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	item, err := a.service.CreateUnitConversion(r.Context(), sessionFrom(r.Context()), businessFrom(r.Context()), app.CreateUnitConversionInput{
		ProductCode:  request.ProductCode,
		FromUnitCode: request.FromUnitCode,
		ToUnitCode:   request.ToUnitCode,
		Multiplier:   string(request.Multiplier),
	})
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	writeData(w, http.StatusCreated, item)
}
