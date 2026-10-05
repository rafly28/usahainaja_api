package app

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"
)

var paymentMethods = map[string]bool{
	"CASH": true, "TRANSFER": true, "DEBIT_CARD": true, "CREDIT_CARD": true, "EWALLET": true,
}

func requirePurchaseRole(role string) *Error {
	if !oneOf(role, "OWNER", "ADMIN") {
		return &Error{Code: "FORBIDDEN", Message: "Anda tidak memiliki izin untuk mengelola pembelian."}
	}
	return nil
}

func (s *Service) ListPurchases(ctx context.Context, businessID string, page, limit int) ([]Purchase, int, error) {
	items, total, err := s.repo.ListPurchases(ctx, businessID, page, limit)
	if err != nil {
		return nil, 0, &Error{Code: "INTERNAL_ERROR", Message: "Tidak dapat memuat pembelian.", Cause: err}
	}
	return items, total, nil
}

func (s *Service) GetPurchase(ctx context.Context, businessID, purchaseNumber string) (Purchase, error) {
	purchase, err := s.repo.GetPurchase(ctx, businessID, purchaseNumber)
	if errors.Is(err, ErrNotFound) {
		return Purchase{}, &Error{Code: "NOT_FOUND", Message: "Pembelian tidak ditemukan."}
	}
	if err != nil {
		return Purchase{}, &Error{Code: "INTERNAL_ERROR", Message: "Tidak dapat memuat pembelian.", Cause: err}
	}
	return purchase, nil
}

func normalizePurchaseItems(items []NewPurchaseItem) ([]NewPurchaseItem, map[string]string) {
	fields := map[string]string{}
	for i, item := range items {
		q, err := normalizeDecimal(item.Quantity, 4, 14, true)
		if err != nil {
			fields["items"] = "Kuantitas tidak valid pada baris " + strconv.Itoa(i+1)
		} else {
			items[i].Quantity = q
		}
		u, err := normalizeDecimal(item.UnitPrice, 2, 16, true)
		if err != nil {
			fields["items"] = "Harga tidak valid pada baris " + strconv.Itoa(i+1)
		} else {
			items[i].UnitPrice = u
		}
		if strings.TrimSpace(item.Discount) == "" {
			items[i].Discount = "0"
		}
		d, err := normalizeDecimal(items[i].Discount, 2, 16, false)
		if err != nil {
			fields["items"] = "Diskon tidak valid pada baris " + strconv.Itoa(i+1)
		} else {
			items[i].Discount = d
		}
		if strings.TrimSpace(item.ProductCode) == "" {
			fields["items"] = "Produk wajib diisi pada baris " + strconv.Itoa(i+1)
		}
	}
	return items, fields
}

func (s *Service) CreatePurchase(ctx context.Context, session Session, business BusinessContext, in NewPurchase) (Purchase, error) {
	if err := requirePurchaseRole(business.Role); err != nil {
		return Purchase{}, err
	}
	fields := map[string]string{}
	locationCode := strings.TrimSpace(in.LocationCode)
	if locationCode == "" {
		fields["location_code"] = "Lokasi wajib diisi."
	}
	discountTotal, err := decimalOrZero(in.DiscountTotal, 2, 16)
	if err != nil {
		fields["discount_total"] = err.Error()
	}
	taxTotal, err := decimalOrZero(in.TaxTotal, 2, 16)
	if err != nil {
		fields["tax_total"] = err.Error()
	}
	if len(in.Items) == 0 {
		fields["items"] = "Minimal satu item harus ada dalam pembelian."
	}
	items, itemFields := normalizePurchaseItems(in.Items)
	for k, v := range itemFields {
		fields[k] = v
	}
	in.Items = items
	if len(fields) != 0 {
		return Purchase{}, validationError(fields)
	}

	in.LocationCode = locationCode
	in.PaymentStatus = "UNPAID" // drafts always start unpaid
	in.DiscountTotal = discountTotal
	in.TaxTotal = taxTotal

	purchase, err := s.repo.CreatePurchase(ctx, business.ID, session.UserID, in)
	if errors.Is(err, ErrNotFound) {
		return Purchase{}, &Error{Code: "NOT_FOUND", Message: "Data terkait (Lokasi, Pemasok, atau Produk) tidak ditemukan."}
	}
	if err != nil {
		return Purchase{}, &Error{Code: "INTERNAL_ERROR", Message: "Tidak dapat memproses pembelian.", Cause: err}
	}
	return purchase, nil
}

func (s *Service) AddPurchaseItem(ctx context.Context, session Session, business BusinessContext, purchaseNumber string, item NewPurchaseItem) (PurchaseItem, error) {
	if err := requirePurchaseRole(business.Role); err != nil {
		return PurchaseItem{}, err
	}
	items, fields := normalizePurchaseItems([]NewPurchaseItem{item})
	if len(fields) != 0 {
		return PurchaseItem{}, validationError(fields)
	}
	result, err := s.repo.AddPurchaseItem(ctx, business.ID, session.UserID, purchaseNumber, items[0])
	return appResult(result, err)
}

func (s *Service) UpdatePurchaseItem(ctx context.Context, session Session, business BusinessContext, purchaseNumber string, lineNumber int, item NewPurchaseItem) (PurchaseItem, error) {
	if err := requirePurchaseRole(business.Role); err != nil {
		return PurchaseItem{}, err
	}
	items, fields := normalizePurchaseItems([]NewPurchaseItem{item})
	if len(fields) != 0 {
		return PurchaseItem{}, validationError(fields)
	}
	result, err := s.repo.UpdatePurchaseItem(ctx, business.ID, session.UserID, purchaseNumber, lineNumber, items[0])
	return appResult(result, err)
}

func appResult[T any](result T, err error) (T, error) {
	if errors.Is(err, ErrNotFound) {
		var zero T
		return zero, &Error{Code: "NOT_FOUND", Message: "Data tidak ditemukan."}
	}
	if err != nil {
		var appErr *Error
		if errors.As(err, &appErr) {
			var zero T
			return zero, err
		}
		var zero T
		return zero, &Error{Code: "INTERNAL_ERROR", Message: "Operasi gagal.", Cause: err}
	}
	return result, nil
}

func (s *Service) DeletePurchaseItem(ctx context.Context, session Session, business BusinessContext, purchaseNumber string, lineNumber int) error {
	if err := requirePurchaseRole(business.Role); err != nil {
		return err
	}
	err := s.repo.DeletePurchaseItem(ctx, business.ID, session.UserID, purchaseNumber, lineNumber)
	if errors.Is(err, ErrNotFound) {
		return &Error{Code: "NOT_FOUND", Message: "Item tidak ditemukan."}
	}
	if err != nil {
		var appErr *Error
		if errors.As(err, &appErr) {
			return err
		}
		return &Error{Code: "INTERNAL_ERROR", Message: "Tidak dapat menghapus item.", Cause: err}
	}
	return nil
}

func (s *Service) OrderPurchase(ctx context.Context, session Session, business BusinessContext, purchaseNumber string) error {
	if err := requirePurchaseRole(business.Role); err != nil {
		return err
	}
	err := s.repo.OrderPurchase(ctx, business.ID, session.UserID, purchaseNumber)
	return mapPurchaseRepoError(err, "Tidak dapat mengorder pembelian.")
}

func (s *Service) CancelPurchase(ctx context.Context, session Session, business BusinessContext, purchaseNumber string) error {
	if err := requirePurchaseRole(business.Role); err != nil {
		return err
	}
	err := s.repo.CancelPurchase(ctx, business.ID, session.UserID, purchaseNumber)
	return mapPurchaseRepoError(err, "Tidak dapat membatalkan pembelian.")
}

func mapPurchaseRepoError(err error, fallback string) error {
	if errors.Is(err, ErrNotFound) {
		return &Error{Code: "NOT_FOUND", Message: "Pesanan tidak ditemukan."}
	}
	if err != nil {
		var appErr *Error
		if errors.As(err, &appErr) {
			return err
		}
		return &Error{Code: "INTERNAL_ERROR", Message: fallback, Cause: err}
	}
	return nil
}

func normalizePaymentInput(in PaymentInput) (PaymentInput, map[string]string) {
	fields := map[string]string{}
	in.CashAccountCode = strings.TrimSpace(in.CashAccountCode)
	if in.CashAccountCode == "" {
		fields["cash_account_code"] = "Akun kas wajib diisi."
	}
	amount, err := normalizeDecimal(in.Amount, 2, 16, true)
	if err != nil {
		fields["amount"] = "Nominal pembayaran tidak valid."
	} else {
		in.Amount = amount
	}
	method := strings.ToUpper(strings.TrimSpace(in.PaymentMethod))
	if method == "" {
		method = "CASH"
	}
	if !paymentMethods[method] {
		fields["payment_method_code"] = "Metode pembayaran tidak didukung."
	} else {
		in.PaymentMethod = method
	}
	if strings.TrimSpace(in.PaidAt) != "" {
		if _, err := time.Parse(time.RFC3339, strings.TrimSpace(in.PaidAt)); err != nil {
			fields["paid_at"] = "Format waktu tidak valid (RFC3339)."
		} else {
			in.PaidAt = strings.TrimSpace(in.PaidAt)
		}
	}
	in.ReferenceNumber = strings.TrimSpace(in.ReferenceNumber)
	in.Notes = strings.TrimSpace(in.Notes)
	return in, fields
}

func (s *Service) ReceivePurchase(ctx context.Context, session Session, business BusinessContext, purchaseNumber string, in ReceivePurchaseInput) (PurchaseReceipt, error) {
	if err := requirePurchaseRole(business.Role); err != nil {
		return PurchaseReceipt{}, err
	}
	fields := map[string]string{}
	clean := ReceivePurchaseInput{
		ReceivedAt:      strings.TrimSpace(in.ReceivedAt),
		ReferenceNumber: strings.TrimSpace(in.ReferenceNumber),
	}
	if clean.ReceivedAt != "" {
		if _, err := time.Parse(time.RFC3339, clean.ReceivedAt); err != nil {
			fields["received_at"] = "Format waktu tidak valid (RFC3339)."
		}
	}
	if len(in.Items) == 0 {
		fields["items"] = "Minimal satu item diterima."
	}
	for i, item := range in.Items {
		if item.LineNumber < 1 {
			fields["items"] = "Nomor baris tidak valid pada baris " + strconv.Itoa(i+1)
			continue
		}
		qty, err := normalizeDecimal(item.ReceivedQuantity, 4, 14, true)
		if err != nil {
			fields["items"] = "Kuantitas terima tidak valid pada baris " + strconv.Itoa(i+1)
			continue
		}
		clean.Items = append(clean.Items, ReceiveItemInput{LineNumber: item.LineNumber, ReceivedQuantity: qty})
	}
	if len(fields) != 0 {
		return PurchaseReceipt{}, validationError(fields)
	}
	receipt, err := s.repo.ReceivePurchase(ctx, business.ID, purchaseNumber, session.UserID, clean)
	if errors.Is(err, ErrNotFound) {
		return PurchaseReceipt{}, &Error{Code: "NOT_FOUND", Message: "Pesanan atau item tidak ditemukan."}
	}
	if err != nil {
		var appErr *Error
		if errors.As(err, &appErr) {
			return PurchaseReceipt{}, err
		}
		return PurchaseReceipt{}, &Error{Code: "INTERNAL_ERROR", Message: "Gagal memproses penerimaan barang.", Cause: err}
	}
	return receipt, nil
}

func (s *Service) PayPurchase(ctx context.Context, session Session, business BusinessContext, purchaseNumber string, in PaymentInput) (Payment, error) {
	if err := requirePurchaseRole(business.Role); err != nil {
		return Payment{}, err
	}
	in, fields := normalizePaymentInput(in)
	if len(fields) != 0 {
		return Payment{}, validationError(fields)
	}
	payment, err := s.repo.RecordPurchasePayment(ctx, business.ID, purchaseNumber, session.UserID, in)
	if errors.Is(err, ErrNotFound) {
		return Payment{}, &Error{Code: "NOT_FOUND", Message: "Pesanan atau akun kas tidak ditemukan."}
	}
	if err != nil {
		var appErr *Error
		if errors.As(err, &appErr) {
			return Payment{}, err
		}
		return Payment{}, &Error{Code: "INTERNAL_ERROR", Message: "Gagal mencatat pembayaran.", Cause: err}
	}
	return payment, nil
}
