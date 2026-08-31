package app

import (
	"context"
	"errors"
	"strconv"
	"strings"
)

func (s *Service) ListPurchases(ctx context.Context, businessID string) ([]Purchase, error) {
	items, err := s.repo.ListPurchases(ctx, businessID)
	if err != nil {
		return nil, &Error{Code: "INTERNAL_ERROR", Message: "Tidak dapat memuat pembelian.", Cause: err}
	}
	return items, nil
}

func (s *Service) CreatePurchase(ctx context.Context, session Session, businessID string, in NewPurchase) (Purchase, error) {
	fields := map[string]string{}
	locationCode := strings.TrimSpace(in.LocationCode)
	if locationCode == "" {
		locationCode = "LOC-DEFAULT"
	}
	paymentStatus := strings.ToUpper(strings.TrimSpace(in.PaymentStatus))
	if paymentStatus == "" {
		paymentStatus = "PAID"
	}
	if !oneOf(paymentStatus, "UNPAID", "PARTIAL", "PAID") {
		// Just force it to UNPAID for draft, or accept it but it won't be used for initial state.
		// Wait, T04 says CreatePurchase should create a DRAFT. So payment must be UNPAID initially.
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

	for i, item := range in.Items {
		q, err := normalizeDecimal(item.Quantity, 4, 14, true)
		if err != nil {
			fields["items"] = "Kuantitas tidak valid pada baris " + strconv.Itoa(i+1)
		}
		in.Items[i].Quantity = q
		u, err := normalizeDecimal(item.UnitPrice, 2, 16, true)
		if err != nil {
			fields["items"] = "Harga tidak valid pada baris " + strconv.Itoa(i+1)
		}
		in.Items[i].UnitPrice = u
		d, err := normalizeDecimal(item.Discount, 2, 16, false)
		if err != nil {
			fields["items"] = "Diskon tidak valid pada baris " + strconv.Itoa(i+1)
		}
		in.Items[i].Discount = d
	}

	if len(fields) != 0 {
		return Purchase{}, validationError(fields)
	}

	in.LocationCode = locationCode
	in.PaymentStatus = "UNPAID" // Force UNPAID for drafts
	in.DiscountTotal = discountTotal
	in.TaxTotal = taxTotal

	purchase, err := s.repo.CreatePurchase(ctx, businessID, session.UserID, in)
	if errors.Is(err, ErrNotFound) {
		return Purchase{}, &Error{Code: "NOT_FOUND", Message: "Data terkait (Lokasi, Pemasok, atau Produk) tidak ditemukan."}
	}
	if err != nil {
		return Purchase{}, &Error{Code: "INTERNAL_ERROR", Message: "Tidak dapat memproses pembelian.", Cause: err}
	}
	return purchase, nil
}

func (s *Service) ReceivePurchase(ctx context.Context, session Session, businessID, purchaseNumber string) error {
	err := s.repo.ReceivePurchase(ctx, businessID, purchaseNumber, session.UserID)
	if errors.Is(err, ErrNotFound) {
		return &Error{Code: "NOT_FOUND", Message: "Pesanan tidak ditemukan atau sudah tidak dalam status DRAFT."}
	}
	if err != nil {
		return &Error{Code: "INTERNAL_ERROR", Message: "Gagal memproses penerimaan barang.", Cause: err}
	}
	return nil
}

func (s *Service) PayPurchase(ctx context.Context, session Session, businessID, purchaseNumber string, in PaymentInput) (Payment, error) {
	fields := map[string]string{}
	if strings.TrimSpace(in.CashAccountCode) == "" {
		fields["cash_account_code"] = "Akun kas wajib diisi."
	}
	amount, err := normalizeDecimal(in.Amount, 2, 16, true)
	if err != nil {
		fields["amount"] = "Nominal pembayaran tidak valid."
	}
	in.Amount = amount

	if len(fields) != 0 {
		return Payment{}, validationError(fields)
	}

	payment, err := s.repo.RecordPurchasePayment(ctx, businessID, purchaseNumber, session.UserID, in)
	if errors.Is(err, ErrNotFound) {
		return Payment{}, &Error{Code: "NOT_FOUND", Message: "Pesanan atau akun kas tidak ditemukan."}
	}
	if err != nil {
		return Payment{}, &Error{Code: "INTERNAL_ERROR", Message: "Gagal mencatat pembayaran.", Cause: err}
	}
	return payment, nil
}
