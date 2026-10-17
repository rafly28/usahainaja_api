package postgres

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"

	"usahainaja/backend/internal/app"
)

// stockAdjustmentLine adalah satu baris adjustment yang siap diterapkan ke stok.
type stockAdjustmentLine struct {
	productID    string
	productCode  string
	quantity     string
	baseQuantity string
	unitID       string
	baseUnitID   string
	direction    string
}

// CompleteStockAdjustment menerapkan adjustment DRAFT ke ledger stok.
//
// Perbaikan penting terhadap versi sebelumnya:
//   - movement_type memakai pemetaan alasan yang valid sehingga tidak melanggar
//     CHECK constraint pada stock_movements.
//   - guard stok negatif dihitung di dalam transaksi yang sama sebelum snapshot
//     ditulis, sehingga stok negatif tidak pernah tersimpan.
func (r *Repository) CompleteStockAdjustment(ctx context.Context, businessID, userID, adjustmentNumber string) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer rollback(ctx, tx)

	var adjustmentID, locationID, locationCode, reason string
	err = tx.QueryRow(ctx, `
		SELECT sa.id::text, sa.location_id::text, l.public_code, sa.reason
		FROM stock_adjustments sa
		JOIN locations l ON sa.location_id = l.id
		WHERE sa.business_id = $1 AND sa.adjustment_number = $2 AND sa.status = 'DRAFT'
		FOR UPDATE OF sa`, businessID, adjustmentNumber,
	).Scan(&adjustmentID, &locationID, &locationCode, &reason)
	if err != nil {
		return mapError(err)
	}

	lines, err := r.loadAdjustmentLines(ctx, businessID, adjustmentID)
	if err != nil {
		return err
	}

	movementType := movementTypeForReason(reason)
	for _, line := range lines {
		if err := r.applyAdjustmentLine(ctx, tx, businessID, userID, locationID, adjustmentID, adjustmentNumber, locationCode, movementType, reason, line); err != nil {
			return err
		}
	}

	if _, err := tx.Exec(ctx, `
		UPDATE stock_adjustments SET status = 'COMPLETED', updated_at = now()
		WHERE business_id = $1 AND id = $2`, businessID, adjustmentID); err != nil {
		return mapError(err)
	}

	if err := writeAuditTx(ctx, tx, businessID, userID, "STOCK_ADJUSTMENT", adjustmentID, adjustmentNumber,
		"STOCK_ADJUSTMENT_COMPLETED",
		map[string]any{
			"location_code": locationCode,
			"movement_type": movementType,
			"item_count":    len(lines),
		},
		reason); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// loadAdjustmentLines membaca baris adjustment lengkap dengan basis konversi.
func (r *Repository) loadAdjustmentLines(ctx context.Context, businessID, adjustmentID string) ([]stockAdjustmentLine, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT sai.product_id::text, p.public_code, sai.quantity::text,
		       sai.base_quantity::text, sai.unit_id::text, sai.base_unit_id::text, sai.direction
		FROM stock_adjustment_items sai
		JOIN products p ON sai.product_id = p.id
		WHERE sai.business_id = $1 AND sai.stock_adjustment_id = $2
		ORDER BY sai.id`, businessID, adjustmentID)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()

	lines := make([]stockAdjustmentLine, 0)
	for rows.Next() {
		var line stockAdjustmentLine
		if err := rows.Scan(&line.productID, &line.productCode, &line.quantity,
			&line.baseQuantity, &line.unitID, &line.baseUnitID, &line.direction); err != nil {
			return nil, mapError(err)
		}
		lines = append(lines, line)
	}
	return lines, rows.Err()
}

// applyAdjustmentLine menulis satu movement, memperbarui snapshot, dan mengaudit.
// Pembacaan snapshot serta penulisan terjadi pada transaksi yang sama sehingga
// pemeriksaan stok negatif bersifat atomik.
func (r *Repository) applyAdjustmentLine(ctx context.Context, tx pgx.Tx, businessID, userID, locationID, adjustmentID, adjustmentNumber, locationCode, movementType, reason string, line stockAdjustmentLine) error {
	current, err := currentInventoryQtyTx(ctx, tx, businessID, line.productID, locationID)
	if err != nil {
		return err
	}

	delta := line.baseQuantity
	if line.direction == "OUT" {
		delta = "-" + strings.TrimPrefix(line.baseQuantity, "-")
	}

	resulting, err := addDecimalScaled(current, delta, stockScale)
	if err != nil {
		return err
	}
	if isNegative, err := decimalIsNegative(resulting); err != nil {
		return err
	} else if isNegative {
		return app.ErrInsufficientStock
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO stock_movements (
			business_id, product_id, location_id, movement_type, direction,
			quantity, unit_id, base_quantity, base_unit_id, reference_id, reason, created_by
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
		businessID, line.productID, locationID, movementType, line.direction,
		line.quantity, line.unitID, line.baseQuantity, line.baseUnitID, adjustmentID, reason, userID); err != nil {
		return mapError(err)
	}

	// Guard di atas sudah memastikan hasil akhir tidak negatif; snapshot tetap
	// ditulis dengan delta yang sama agar ledger dan snapshot sinkron.
	if _, err := applyStockDeltaTx(ctx, tx, businessID, line.productID, locationID, line.baseUnitID, delta); err != nil {
		return err
	}

	return writeAuditTx(ctx, tx, businessID, userID, "STOCK_ADJUSTMENT", adjustmentID, adjustmentNumber,
		"STOCK_ADJUSTED",
		map[string]any{
			"product_code":  line.productCode,
			"location_code": locationCode,
			"direction":     line.direction,
			"quantity":      line.quantity,
			"base_quantity": line.baseQuantity,
			"old_quantity":  current,
			"new_quantity":  resulting,
		},
		reason)
}
