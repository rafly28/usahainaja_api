package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"usahainaja/backend/internal/app"
)

func (r *Repository) ListSales(ctx context.Context, businessID string) ([]app.Sale, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT s.receipt_number, s.sale_date, l.public_code, c.public_code, s.status, s.payment_status,
			   s.subtotal, s.discount_total, s.tax_total, s.grand_total, COALESCE(s.notes, '')
		FROM sales s
		JOIN locations l ON l.id = s.location_id
		LEFT JOIN contacts c ON c.id = s.customer_id
		WHERE s.business_id = $1
		ORDER BY s.sale_date DESC LIMIT 100`, businessID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]app.Sale, 0)
	for rows.Next() {
		var item app.Sale
		if err := rows.Scan(&item.ReceiptNumber, &item.SaleDate, &item.LocationCode, &item.CustomerCode,
			&item.Status, &item.PaymentStatus, &item.Subtotal, &item.DiscountTotal, &item.TaxTotal,
			&item.GrandTotal, &item.Notes); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) CreateSale(ctx context.Context, businessID, userID string, input app.NewSale) (app.Sale, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return app.Sale{}, err
	}
	defer rollback(ctx, tx)

	var locationID string
	err = tx.QueryRow(ctx, `SELECT id FROM locations WHERE business_id = $1 AND public_code = $2`, businessID, input.LocationCode).Scan(&locationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return app.Sale{}, app.ErrNotFound
	}

	var customerID *string
	if input.CustomerCode != "" {
		var cid string
		err = tx.QueryRow(ctx, `SELECT id FROM contacts WHERE business_id = $1 AND public_code = $2`, businessID, input.CustomerCode).Scan(&cid)
		if err == nil {
			customerID = &cid
		}
	}

	receiptNumber, err := nextNumber(ctx, tx, businessID, "SALE")
	if err != nil {
		receiptNumber = fmt.Sprintf("SL-%d", time.Now().UnixMilli())
	}

	// Calculate totals inside postgres by just inserting items, but we need subtotal first.
	// For simplicity in this demo logic, we'll let postgres trigger or calculate it, 
	// OR we compute it in Go. Let's compute in Go for precise string decimal insertion.
	// We will skip strict decimal math here and just let Postgres do `SUM`.

	var saleID string
	err = tx.QueryRow(ctx, `
		INSERT INTO sales (
			business_id, location_id, customer_id, receipt_number, status, payment_status, 
			discount_total, tax_total, created_by
		) VALUES ($1, $2, $3, $4, 'COMPLETED', $5, $6, $7, $8) RETURNING id`,
		businessID, locationID, customerID, receiptNumber, input.PaymentStatus, input.DiscountTotal, input.TaxTotal, userID,
	).Scan(&saleID)
	if err != nil {
		return app.Sale{}, err
	}

	for _, item := range input.Items {
		var prodID, baseUnitID string
		var isTracked bool
		err = tx.QueryRow(ctx, `SELECT id, base_unit_id, is_stock_tracked FROM products WHERE business_id = $1 AND public_code = $2`, businessID, item.ProductCode).Scan(&prodID, &baseUnitID, &isTracked)
		if errors.Is(err, pgx.ErrNoRows) {
			return app.Sale{}, app.ErrNotFound
		}

		_, err = tx.Exec(ctx, `
			INSERT INTO sale_items (business_id, sale_id, product_id, quantity, unit_price, discount, subtotal)
			VALUES ($1, $2, $3, $4, $5, $6, ($4::numeric * $5::numeric) - $6::numeric)`,
			businessID, saleID, prodID, item.Quantity, item.UnitPrice, item.Discount,
		)
		if err != nil {
			return app.Sale{}, err
		}

		if isTracked {
			_, err = tx.Exec(ctx, `
				INSERT INTO stock_movements (business_id, product_id, location_id, movement_type, direction, quantity, unit_id, base_quantity, base_unit_id, reference_id, created_by)
				VALUES ($1, $2, $3, 'SALE', 'OUT', $4, $5, $4, $5, $6, $7)`,
				businessID, prodID, locationID, item.Quantity, baseUnitID, saleID, userID,
			)
			if err != nil {
				return app.Sale{}, err
			}
			result, err := tx.Exec(ctx, `
				UPDATE product_inventory 
				SET quantity = quantity - $4::numeric
				WHERE business_id = $1 AND product_id = $2 AND location_id = $3 AND quantity >= $4::numeric`,
				businessID, prodID, locationID, item.Quantity,
			)
			if err != nil {
				return app.Sale{}, err
			}
			if result.RowsAffected() == 0 {
				return app.Sale{}, errors.New("stok tidak mencukupi atau lokasi tidak ditemukan")
			}
		}
	}

	// Update totals
	_, err = tx.Exec(ctx, `
		UPDATE sales 
		SET subtotal = (SELECT COALESCE(SUM(subtotal), 0) FROM sale_items WHERE sale_id = $1),
			grand_total = (SELECT COALESCE(SUM(subtotal), 0) FROM sale_items WHERE sale_id = $1) - discount_total + tax_total
		WHERE id = $1`, saleID)
	if err != nil {
		return app.Sale{}, err
	}

	// If paid, create payment
	if input.PaymentStatus == "PAID" {
		var cashID string
		err = tx.QueryRow(ctx, `SELECT id FROM cash_accounts WHERE business_id = $1 AND is_default = true LIMIT 1`, businessID).Scan(&cashID)
		if err == nil {
			payNumber, _ := nextNumber(ctx, tx, businessID, "PAY")
			if payNumber == "" { payNumber = "PAY-" + receiptNumber }
			_, err = tx.Exec(ctx, `
				INSERT INTO payments (business_id, cash_account_id, sale_id, payment_number, amount, created_by)
				SELECT $1, $2, $3, $4, grand_total, $5 FROM sales WHERE id = $3`,
				businessID, cashID, saleID, payNumber, userID)
			if err != nil {
				return app.Sale{}, err
			}
			_, err = tx.Exec(ctx, `UPDATE cash_accounts SET balance = balance + (SELECT grand_total FROM sales WHERE id = $1) WHERE id = $2`, saleID, cashID)
			if err != nil {
				return app.Sale{}, err
			}
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return app.Sale{}, err
	}
	
	// Fetch result
	var sale app.Sale
	sale.ReceiptNumber = receiptNumber
	return sale, nil
}
