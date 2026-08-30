package app

import (
	"context"
	"errors"
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
		fields["items"] = "Minimal satu item harus ada dalam pembelian."
	}

	for i, item := range in.Items {
		q, err := normalizeDecimal(item.Quantity, 4, 14, true)
		if err != nil {
			fields["items"] = "Kuantitas tidak valid pada baris " + string(rune(i+'1'))
		}
		in.Items[i].Quantity = q
		u, err := normalizeDecimal(item.UnitPrice, 2, 16, true)
		if err != nil {
			fields["items"] = "Harga tidak valid pada baris " + string(rune(i+'1'))
		}
		in.Items[i].UnitPrice = u
		d, err := normalizeDecimal(item.Discount, 2, 16, false)
		if err != nil {
			fields["items"] = "Diskon tidak valid pada baris " + string(rune(i+'1'))
		}
		in.Items[i].Discount = d
	}

	if len(fields) != 0 {
		return Purchase{}, validationError(fields)
	}

	in.LocationCode = locationCode
	in.PaymentStatus = paymentStatus
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
