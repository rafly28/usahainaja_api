package app

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Alasan yang boleh dipilih pengguna untuk adjustment manual dan opname.
// OPENING_BALANCE tidak termasuk karena hanya dipakai sistem untuk stok awal.
var adjustmentReasons = map[string]struct{}{
	"CORRECTION": {},
	"SPOILAGE":   {},
	"DAMAGE":     {},
	"LOST":       {},
	"OTHER":      {},
}

// movementTypes adalah nilai movement_type yang valid pada stock_movements.
// Filter memakai daftar ini agar query tidak pernah menolak tipe di luar enum.
var movementTypes = map[string]struct{}{
	"PURCHASE":        {},
	"SALE":            {},
	"ADJUSTMENT":      {},
	"SPOILAGE":        {},
	"DAMAGE":          {},
	"TRANSFER":        {},
	"RETURN":          {},
	"OPENING_BALANCE": {},
}

// OpeningStockInput menerima batch item sesuai kontrak opening stock.
type OpeningStockInput struct {
	LocationCode string
	Reason       string
	Items        []OpeningStockItemInput
}

type OpeningStockItemInput struct {
	ProductCode string
	Quantity    string
	UnitSymbol  string
}

// CreateOpeningStock mencatat stok awal batch untuk satu lokasi.
func (s *Service) CreateOpeningStock(ctx context.Context, session Session, businessID string, in OpeningStockInput) (OpeningStock, error) {
	locationCode := strings.ToUpper(strings.TrimSpace(in.LocationCode))
	if locationCode == "" {
		locationCode = "LOC-DEFAULT"
	}
	fields := map[string]string{}
	if !codePattern.MatchString(locationCode) {
		fields["location_code"] = "Kode lokasi tidak valid."
	}
	if len(in.Items) == 0 {
		fields["items"] = "Minimal 1 barang."
	}
	reason := strings.TrimSpace(in.Reason)
	if utf8.RuneCountInString(reason) > 500 {
		fields["reason"] = "Alasan maksimal 500 karakter."
	}

	items := make([]NewOpeningStockItem, 0, len(in.Items))
	seen := make(map[string]struct{}, len(in.Items))
	for index, item := range in.Items {
		key := "items[" + strconv.Itoa(index) + "]"
		productCode := strings.ToUpper(strings.TrimSpace(item.ProductCode))
		if !codePattern.MatchString(productCode) {
			fields[key+".product_code"] = "Kode produk tidak valid."
		}
		if _, duplicate := seen[productCode]; duplicate {
			fields[key+".product_code"] = "Produk sudah ada di daftar."
		}
		seen[productCode] = struct{}{}

		quantity, err := normalizeDecimal(item.Quantity, 4, 14, true)
		if err != nil {
			fields[key+".quantity"] = err.Error()
		}
		items = append(items, NewOpeningStockItem{
			ProductCode: productCode,
			Quantity:    quantity,
			UnitSymbol:  strings.TrimSpace(item.UnitSymbol),
		})
	}
	if len(fields) != 0 {
		return OpeningStock{}, validationError(fields)
	}

	result, err := s.repo.CreateOpeningStock(ctx, businessID, session.UserID, NewOpeningStock{
		LocationCode: locationCode, Reason: reason, Items: items,
	})
	if errors.Is(err, ErrConflict) {
		// Stok awal hanya boleh dicatat satu kali per produk dan lokasi.
		return OpeningStock{}, &Error{
			Code:    "OPENING_STOCK_ALREADY_RECORDED",
			Message: "Stok awal untuk produk dan lokasi ini sudah pernah dicatat.",
		}
	}
	if err != nil {
		return OpeningStock{}, mapInventoryError(err, "Tidak dapat mencatat stok awal.")
	}
	return result, nil
}

// ListInventoryProducts membaca stok dengan filter dan pagination.
func (s *Service) ListInventoryProducts(ctx context.Context, businessID string, filter InventoryFilter) ([]InventoryProduct, int, error) {
	filter.Search = strings.TrimSpace(filter.Search)
	filter.LocationCode = strings.ToUpper(strings.TrimSpace(filter.LocationCode))
	items, total, err := s.repo.ListInventoryProducts(ctx, businessID, filter)
	if err != nil {
		return nil, 0, &Error{Code: "INTERNAL_ERROR", Message: "Tidak dapat memuat inventaris.", Cause: err}
	}
	return items, total, nil
}

// ListStockMovements membaca ledger pergerakan stok dengan filter dan pagination.
func (s *Service) ListStockMovements(ctx context.Context, businessID string, filter StockMovementFilter) ([]StockMovement, int, error) {
	fields := map[string]string{}
	filter.ProductCode = strings.ToUpper(strings.TrimSpace(filter.ProductCode))
	filter.LocationCode = strings.ToUpper(strings.TrimSpace(filter.LocationCode))
	filter.MovementType = strings.ToUpper(strings.TrimSpace(filter.MovementType))

	if _, ok := movementTypes[filter.MovementType]; filter.MovementType != "" && !ok {
		fields["movement_type"] = "Jenis pergerakan tidak dikenal."
	}
	dateFrom, err := normalizeDateFilter(filter.DateFrom)
	if err != nil {
		fields["date_from"] = err.Error()
	}
	dateTo, err := normalizeDateFilter(filter.DateTo)
	if err != nil {
		fields["date_to"] = err.Error()
	}
	filter.DateFrom = dateFrom
	filter.DateTo = dateTo
	if len(fields) != 0 {
		return nil, 0, validationError(fields)
	}

	movements, total, err := s.repo.ListStockMovements(ctx, businessID, filter)
	if err != nil {
		return nil, 0, &Error{Code: "INTERNAL_ERROR", Message: "Tidak dapat memuat riwayat pergerakan stok.", Cause: err}
	}
	return movements, total, nil
}

// ListStockAdjustments membaca daftar adjustment stok.
func (s *Service) ListStockAdjustments(ctx context.Context, businessID string, filter StockAdjustmentFilter) ([]StockAdjustment, int, error) {
	fields := map[string]string{}
	filter.Status = strings.ToUpper(strings.TrimSpace(filter.Status))
	filter.Kind = strings.ToUpper(strings.TrimSpace(filter.Kind))

	if filter.Status != "" && !oneOf(filter.Status, "DRAFT", "COMPLETED", "CANCELLED") {
		fields["status"] = "Status adjustment tidak dikenal."
	}
	if filter.Kind != "" && !oneOf(filter.Kind, "MANUAL", "OPNAME", "OPENING_BALANCE") {
		fields["adjustment_kind"] = "Jenis adjustment tidak dikenal."
	}
	if len(fields) != 0 {
		return nil, 0, validationError(fields)
	}

	adjustments, total, err := s.repo.ListStockAdjustments(ctx, businessID, filter)
	if err != nil {
		return nil, 0, &Error{Code: "INTERNAL_ERROR", Message: "Tidak dapat memuat daftar penyesuaian stok.", Cause: err}
	}
	return adjustments, total, nil
}

// GetStockAdjustment membaca detail satu adjustment beserta baris dan jejak opname.
func (s *Service) GetStockAdjustment(ctx context.Context, businessID, adjustmentNumber string) (StockAdjustment, error) {
	number := strings.ToUpper(strings.TrimSpace(adjustmentNumber))
	if !codePattern.MatchString(number) {
		return StockAdjustment{}, &Error{Code: "NOT_FOUND", Message: "Penyesuaian stok tidak ditemukan."}
	}
	adjustment, err := s.repo.GetStockAdjustment(ctx, businessID, number)
	if errors.Is(err, ErrNotFound) {
		return StockAdjustment{}, &Error{Code: "NOT_FOUND", Message: "Penyesuaian stok tidak ditemukan."}
	}
	if err != nil {
		return StockAdjustment{}, &Error{Code: "INTERNAL_ERROR", Message: "Tidak dapat memuat penyesuaian stok.", Cause: err}
	}
	return adjustment, nil
}

// CreateStockAdjustment membuat adjustment manual berstatus DRAFT.
func (s *Service) CreateStockAdjustment(ctx context.Context, session Session, businessID string, input NewStockAdjustment) (StockAdjustment, error) {
	items, reason, fields := validateAdjustmentPayload(input.LocationCode, input.Reason, input.Notes, input.Items)
	if len(fields) != 0 {
		return StockAdjustment{}, validationError(fields)
	}

	result, err := s.repo.CreateStockAdjustment(ctx, businessID, session.UserID, NewStockAdjustment{
		LocationCode: strings.ToUpper(strings.TrimSpace(input.LocationCode)),
		Reason:       reason,
		Notes:        strings.TrimSpace(input.Notes),
		Items:        items,
	})
	if err != nil {
		return StockAdjustment{}, mapInventoryError(err, "Tidak dapat membuat penyesuaian stok.")
	}
	return result, nil
}

// CreateStockOpname membuat adjustment hasil hitung fisik untuk satu lokasi.
func (s *Service) CreateStockOpname(ctx context.Context, session Session, businessID string, input NewStockOpname) (StockAdjustment, error) {
	locationCode := strings.ToUpper(strings.TrimSpace(input.LocationCode))
	if locationCode == "" {
		locationCode = "LOC-DEFAULT"
	}
	fields := map[string]string{}
	if !codePattern.MatchString(locationCode) {
		fields["location_code"] = "Kode lokasi tidak valid."
	}
	if len(input.Items) == 0 {
		fields["items"] = "Minimal 1 barang."
	}
	reason := strings.ToUpper(strings.TrimSpace(input.Reason))
	if reason == "" {
		reason = "CORRECTION"
	}
	if _, ok := adjustmentReasons[reason]; !ok {
		fields["reason"] = "Alasan opname tidak didukung."
	}
	if utf8.RuneCountInString(strings.TrimSpace(input.Notes)) > 500 {
		fields["notes"] = "Keterangan maksimal 500 karakter."
	}

	items := make([]NewStockOpnameItem, 0, len(input.Items))
	seen := make(map[string]struct{}, len(input.Items))
	for index, item := range input.Items {
		key := "items[" + strconv.Itoa(index) + "]"
		productCode := strings.ToUpper(strings.TrimSpace(item.ProductCode))
		if !codePattern.MatchString(productCode) {
			fields[key+".product_code"] = "Kode produk tidak valid."
		}
		if _, duplicate := seen[productCode]; duplicate {
			fields[key+".product_code"] = "Produk sudah ada di daftar."
		}
		seen[productCode] = struct{}{}

		// Kuantitas hasil hitung boleh nol karena barang memang habis di lapangan.
		counted, err := normalizeDecimal(item.CountedQuantity, 4, 14, false)
		if err != nil {
			fields[key+".counted_quantity"] = err.Error()
		}
		items = append(items, NewStockOpnameItem{
			ProductCode:     productCode,
			CountedQuantity: counted,
			UnitSymbol:      strings.TrimSpace(item.UnitSymbol),
		})
	}
	if len(fields) != 0 {
		return StockAdjustment{}, validationError(fields)
	}

	result, err := s.repo.CreateStockOpname(ctx, businessID, session.UserID, NewStockOpname{
		LocationCode: locationCode, Reason: reason, Notes: strings.TrimSpace(input.Notes), Items: items,
	})
	if errors.Is(err, ErrNoStockDifference) {
		return StockAdjustment{}, &Error{
			Code:    "NO_STOCK_DIFFERENCE",
			Message: "Tidak ada selisih antara hasil hitung fisik dan stok sistem.",
		}
	}
	if err != nil {
		return StockAdjustment{}, mapInventoryError(err, "Tidak dapat membuat opname stok.")
	}
	return result, nil
}

// CompleteStockAdjustment menerapkan adjustment DRAFT ke ledger stok.
func (s *Service) CompleteStockAdjustment(ctx context.Context, session Session, businessID, adjustmentNumber string) error {
	err := s.repo.CompleteStockAdjustment(ctx, businessID, session.UserID, adjustmentNumber)
	if errors.Is(err, ErrInsufficientStock) {
		return &Error{Code: "INSUFFICIENT_STOCK", Message: "Stok tidak mencukupi untuk dikeluarkan."}
	}
	if errors.Is(err, ErrNotFound) {
		return &Error{Code: "NOT_FOUND", Message: "Penyesuaian stok tidak ditemukan atau sudah selesai."}
	}
	if err != nil {
		return &Error{Code: "INTERNAL_ERROR", Message: "Tidak dapat menyelesaikan penyesuaian stok.", Cause: err}
	}
	return nil
}

// validateAdjustmentPayload memvalidasi lokasi, alasan, notes, dan baris adjustment manual.
func validateAdjustmentPayload(locationCode, reasonInput, notes string, items []NewStockAdjustmentItem) ([]NewStockAdjustmentItem, string, map[string]string) {
	locationCode = strings.ToUpper(strings.TrimSpace(locationCode))
	if locationCode == "" {
		locationCode = "LOC-DEFAULT"
	}
	fields := map[string]string{}
	if !codePattern.MatchString(locationCode) {
		fields["location_code"] = "Kode lokasi tidak valid."
	}
	if len(items) == 0 {
		fields["items"] = "Minimal 1 barang."
	}

	reason := strings.ToUpper(strings.TrimSpace(reasonInput))
	if reason == "" {
		reason = "CORRECTION"
	}
	if _, ok := adjustmentReasons[reason]; !ok {
		fields["reason"] = "Alasan penyesuaian tidak didukung."
	}
	notes = strings.TrimSpace(notes)
	if utf8.RuneCountInString(notes) > 500 {
		fields["notes"] = "Keterangan maksimal 500 karakter."
	}

	validated := make([]NewStockAdjustmentItem, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for index, item := range items {
		key := "items[" + strconv.Itoa(index) + "]"
		item.ProductCode = strings.ToUpper(strings.TrimSpace(item.ProductCode))
		if !codePattern.MatchString(item.ProductCode) {
			fields[key+".product_code"] = "Kode produk tidak valid."
		}
		if _, duplicate := seen[item.ProductCode]; duplicate {
			fields[key+".product_code"] = "Produk sudah ada di daftar."
		}
		seen[item.ProductCode] = struct{}{}

		item.Direction = strings.ToUpper(strings.TrimSpace(item.Direction))
		if item.Direction != "IN" && item.Direction != "OUT" {
			fields[key+".direction"] = "Arah harus IN atau OUT."
		}
		quantity, err := normalizeDecimal(item.Quantity, 4, 14, true)
		if err != nil {
			fields[key+".quantity"] = err.Error()
		}
		item.Quantity = quantity
		item.UnitSymbol = strings.TrimSpace(item.UnitSymbol)
		validated = append(validated, item)
	}
	return validated, reason, fields
}

// normalizeDateFilter memvalidasi filter tanggal format YYYY-MM-DD.
func normalizeDateFilter(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if _, err := time.Parse("2006-01-02", raw); err != nil {
		return "", errors.New("Tanggal harus berformat YYYY-MM-DD.")
	}
	return raw, nil
}

// mapInventoryError memetakan error repository menjadi error aplikasi yang konsisten.
func mapInventoryError(err error, fallbackMessage string) error {
	switch {
	case errors.Is(err, ErrNotFound):
		return &Error{Code: "PRODUCT_OR_LOCATION_NOT_FOUND", Message: "Produk atau lokasi tidak ditemukan pada bisnis aktif."}
	case errors.Is(err, ErrForbidden):
		return &Error{Code: "STOCK_TRACKING_DISABLED", Message: "Stok tidak dilacak untuk produk ini."}
	case errors.Is(err, ErrUnitConversion):
		return &Error{
			Code:    "UNIT_CONVERSION_NOT_FOUND",
			Message: "Konversi unit untuk produk ini belum diatur. Gunakan unit dasar produk atau tambahkan konversi.",
		}
	case errors.Is(err, ErrConflict):
		return &Error{Code: "CONFLICT", Message: "Stok awal untuk produk dan lokasi ini sudah pernah dicatat."}
	default:
		return &Error{Code: "INTERNAL_ERROR", Message: fallbackMessage, Cause: err}
	}
}
