package app

import (
	"context"
	"errors"
	"strconv"
	"strings"
)

func (s *Service) ListSales(ctx context.Context, businessID string) ([]Sale, error) {
	items, err := s.repo.ListSales(ctx, businessID)
	if err != nil {
		return nil, &Error{Code: "INTERNAL_ERROR", Message: "Tidak dapat memuat penjualan.", Cause: err}
	}
	return items, nil
}

func (s *Service) CreateSale(ctx context.Context, session Session, businessID string, in NewSale) (Sale, error) {
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
		fields["payment_status"] = "Status pembayaran tidak valid."
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
		fields["items"] = "Minimal satu item harus ada dalam penjualan."
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
		return Sale{}, validationError(fields)
	}

	in.LocationCode = locationCode
	in.PaymentStatus = paymentStatus
	in.DiscountTotal = discountTotal
	in.TaxTotal = taxTotal
	// Subtotal & GrandTotal calculated in repo for safety, but we let repo do the math.

	sale, err := s.repo.CreateSale(ctx, businessID, session.UserID, in)
	if errors.Is(err, ErrNotFound) {
		return Sale{}, &Error{Code: "NOT_FOUND", Message: "Data terkait (Lokasi, Pelanggan, atau Produk) tidak ditemukan."}
	}
	if err != nil {
		return Sale{}, &Error{Code: "INTERNAL_ERROR", Message: "Tidak dapat memproses penjualan.", Cause: err}
	}
	return sale, nil
}

func (s *Service) CheckoutSale(ctx context.Context, session Session, businessID, receiptNumber string, paymentInput PaymentInput) (Sale, error) {
	fields := map[string]string{}
	paymentInput.CashAccountCode = strings.TrimSpace(paymentInput.CashAccountCode)
	if paymentInput.CashAccountCode == "" {
		fields["cash_account_code"] = "Akun kas harus diisi."
	}
	amount, err := decimalOrZero(paymentInput.Amount, 2, 16)
	if err != nil {
		fields["amount"] = "Jumlah pembayaran tidak valid."
	}
	paymentInput.Amount = amount

	if len(fields) != 0 {
		return Sale{}, validationError(fields)
	}

	sale, err := s.repo.CheckoutSale(ctx, businessID, session.UserID, receiptNumber, paymentInput)
	if errors.Is(err, ErrNotFound) {
		return Sale{}, &Error{Code: "NOT_FOUND", Message: "Penjualan tidak ditemukan."}
	}
	if err != nil {
		var appErr *Error
		if errors.As(err, &appErr) {
			return Sale{}, err
		}
		return Sale{}, &Error{Code: "INTERNAL_ERROR", Message: "Tidak dapat checkout penjualan.", Cause: err}
	}
	return sale, nil
}

func (s *Service) VoidSale(ctx context.Context, session Session, businessID, receiptNumber, reason string) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return validationError(map[string]string{"reason": "Alasan pembatalan harus diisi."})
	}
	
	err := s.repo.VoidSale(ctx, businessID, session.UserID, receiptNumber, reason)
	if errors.Is(err, ErrNotFound) {
		return &Error{Code: "NOT_FOUND", Message: "Penjualan tidak ditemukan."}
	}
	if err != nil {
		var appErr *Error
		if errors.As(err, &appErr) {
			return err
		}
		return &Error{Code: "INTERNAL_ERROR", Message: "Tidak dapat membatalkan penjualan.", Cause: err}
	}
	return nil
}
