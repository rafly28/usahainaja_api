package app

import (
	"context"
	"errors"
	"math/big"
	"strconv"
	"strings"
)

func requireSaleWriteRole(role string) *Error {
	if !oneOf(role, "OWNER", "ADMIN", "CASHIER") {
		return &Error{Code: "FORBIDDEN", Message: "Anda tidak memiliki izin untuk mengelola penjualan."}
	}
	return nil
}

func requireSaleAdminRole(role string) *Error {
	if !oneOf(role, "OWNER", "ADMIN") {
		return &Error{Code: "FORBIDDEN", Message: "Hanya Owner/Admin yang dapat melakukan tindakan ini."}
	}
	return nil
}

func (s *Service) ListSales(ctx context.Context, businessID string, page, limit int) ([]Sale, int, error) {
	items, total, err := s.repo.ListSales(ctx, businessID, page, limit)
	if err != nil {
		return nil, 0, &Error{Code: "INTERNAL_ERROR", Message: "Tidak dapat memuat penjualan.", Cause: err}
	}
	return items, total, nil
}

func (s *Service) GetSale(ctx context.Context, businessID, receiptNumber string) (Sale, error) {
	sale, err := s.repo.GetSale(ctx, businessID, receiptNumber)
	if errors.Is(err, ErrNotFound) {
		return Sale{}, &Error{Code: "NOT_FOUND", Message: "Penjualan tidak ditemukan."}
	}
	if err != nil {
		return Sale{}, &Error{Code: "INTERNAL_ERROR", Message: "Tidak dapat memuat penjualan.", Cause: err}
	}
	return sale, nil
}

func normalizeSaleItems(items []NewSaleItem) ([]NewSaleItem, map[string]string) {
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
		// line discount must not exceed line gross
		if _, ok := fields["items"]; !ok {
			qty, _ := new(big.Rat).SetString(items[i].Quantity)
			price, _ := new(big.Rat).SetString(items[i].UnitPrice)
			disc, _ := new(big.Rat).SetString(items[i].Discount)
			if qty != nil && price != nil && disc != nil {
				gross := new(big.Rat).Mul(qty, price)
				if disc.Cmp(gross) > 0 {
					fields["items"] = "Diskon melebihi total baris " + strconv.Itoa(i+1)
				}
			}
		}
	}
	return items, fields
}

func (s *Service) CreateSale(ctx context.Context, session Session, business BusinessContext, in NewSale) (Sale, error) {
	if err := requireSaleWriteRole(business.Role); err != nil {
		return Sale{}, err
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
	items, itemFields := normalizeSaleItems(in.Items)
	for k, v := range itemFields {
		fields[k] = v
	}
	in.Items = items
	if len(fields) != 0 {
		return Sale{}, validationError(fields)
	}

	in.LocationCode = locationCode
	in.PaymentStatus = "UNPAID" // drafts always start unpaid; checkout marks PAID
	in.DiscountTotal = discountTotal
	in.TaxTotal = taxTotal

	sale, err := s.repo.CreateSale(ctx, business.ID, session.UserID, in)
	if errors.Is(err, ErrNotFound) {
		return Sale{}, &Error{Code: "NOT_FOUND", Message: "Data terkait (Lokasi, Pelanggan, atau Produk) tidak ditemukan."}
	}
	if err != nil {
		return Sale{}, &Error{Code: "INTERNAL_ERROR", Message: "Tidak dapat memproses penjualan.", Cause: err}
	}
	return sale, nil
}

func (s *Service) AddSaleItem(ctx context.Context, session Session, business BusinessContext, receiptNumber string, item NewSaleItem) (SaleItem, error) {
	if err := requireSaleWriteRole(business.Role); err != nil {
		return SaleItem{}, err
	}
	items, fields := normalizeSaleItems([]NewSaleItem{item})
	if len(fields) != 0 {
		return SaleItem{}, validationError(fields)
	}
	result, err := s.repo.AddSaleItem(ctx, business.ID, session.UserID, receiptNumber, items[0])
	return appResult(result, err)
}

func (s *Service) UpdateSaleItem(ctx context.Context, session Session, business BusinessContext, receiptNumber string, lineNumber int, item NewSaleItem) (SaleItem, error) {
	if err := requireSaleWriteRole(business.Role); err != nil {
		return SaleItem{}, err
	}
	items, fields := normalizeSaleItems([]NewSaleItem{item})
	if len(fields) != 0 {
		return SaleItem{}, validationError(fields)
	}
	result, err := s.repo.UpdateSaleItem(ctx, business.ID, session.UserID, receiptNumber, lineNumber, items[0])
	return appResult(result, err)
}

func (s *Service) DeleteSaleItem(ctx context.Context, session Session, business BusinessContext, receiptNumber string, lineNumber int) error {
	if err := requireSaleWriteRole(business.Role); err != nil {
		return err
	}
	err := s.repo.DeleteSaleItem(ctx, business.ID, session.UserID, receiptNumber, lineNumber)
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

func (s *Service) CancelSale(ctx context.Context, session Session, business BusinessContext, receiptNumber string) error {
	if err := requireSaleAdminRole(business.Role); err != nil {
		return err
	}
	err := s.repo.CancelSale(ctx, business.ID, session.UserID, receiptNumber)
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

func (s *Service) CheckoutSale(ctx context.Context, session Session, business BusinessContext, receiptNumber string, paymentInput PaymentInput) (Sale, error) {
	if err := requireSaleWriteRole(business.Role); err != nil {
		return Sale{}, err
	}
	paymentInput, fields := normalizePaymentInput(paymentInput)
	if len(fields) != 0 {
		return Sale{}, validationError(fields)
	}
	// POS v1 only supports full payment at checkout.
	sale, err := s.repo.CheckoutSale(ctx, business.ID, session.UserID, receiptNumber, paymentInput)
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

func (s *Service) VoidSale(ctx context.Context, session Session, business BusinessContext, receiptNumber, reason string) error {
	if err := requireSaleAdminRole(business.Role); err != nil {
		return err
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return validationError(map[string]string{"reason": "Alasan pembatalan harus diisi."})
	}

	err := s.repo.VoidSale(ctx, business.ID, session.UserID, receiptNumber, reason)
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
