package postgres

import (
	"context"
	"fmt"
	"math/big"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func auditTx(ctx context.Context, tx pgx.Tx, businessID, userID, entityType, entityID, entityCode, action, reason string) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO audit_logs (business_id, actor_user_id, entity_type, entity_id, entity_code, action, reason, after_data)
		VALUES ($1, NULLIF($2, '')::uuid, $3, $4::uuid, $5, $6, NULLIF($7, ''), '{}')`,
		businessID, userID, entityType, entityID, entityCode, action, reason)
	return err
}

func parseRat(raw string) (*big.Rat, error) {
	value, ok := new(big.Rat).SetString(strings.TrimSpace(raw))
	if !ok {
		return nil, fmt.Errorf("angka tidak valid: %s", raw)
	}
	return value, nil
}

func subtractDecimal(a, b string) (string, error) {
	ra, err := parseRat(a)
	if err != nil {
		return "", err
	}
	rb, err := parseRat(b)
	if err != nil {
		return "", err
	}
	return new(big.Rat).Sub(ra, rb).FloatString(2), nil
}

func addDecimal(a, b string) (string, error) {
	ra, err := parseRat(a)
	if err != nil {
		return "", err
	}
	rb, err := parseRat(b)
	if err != nil {
		return "", err
	}
	return new(big.Rat).Add(ra, rb).FloatString(2), nil
}

// compareDecimal returns -1, 0, +1 for a<b, a==b, a>b.
func compareDecimal(a, b string) (int, error) {
	ra, err := parseRat(a)
	if err != nil {
		return 0, err
	}
	rb, err := parseRat(b)
	if err != nil {
		return 0, err
	}
	return ra.Cmp(rb), nil
}

func decimalLTE(a, b string) (bool, error) {
	cmp, err := compareDecimal(a, b)
	if err != nil {
		return false, err
	}
	return cmp <= 0, nil
}

func decimalPositive(raw string) (bool, error) {
	value, err := parseRat(raw)
	if err != nil {
		return false, err
	}
	return value.Sign() > 0, nil
}

func isZeroDecimal(raw string) bool {
	value, err := parseRat(raw)
	if err != nil {
		return strings.TrimSpace(raw) == "" || strings.TrimSpace(raw) == "0"
	}
	return value.Sign() == 0
}

func purchasePaidTotalTx(ctx context.Context, tx pgx.Tx, businessID, purchaseID string) (string, error) {
	var paid string
	err := tx.QueryRow(ctx, `
		SELECT COALESCE(SUM(CASE WHEN kind = 'REFUND' THEN -amount ELSE amount END), 0)::text
		FROM payments WHERE business_id = $1 AND purchase_id = $2`, businessID, purchaseID).Scan(&paid)
	if err != nil {
		return "", err
	}
	return paid, nil
}

func purchasePaidTotal(ctx context.Context, pool *pgxpool.Pool, businessID, purchaseID string) (string, error) {
	var paid string
	err := pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(CASE WHEN kind = 'REFUND' THEN -amount ELSE amount END), 0)::text
		FROM payments WHERE business_id = $1 AND purchase_id = $2`, businessID, purchaseID).Scan(&paid)
	if err != nil {
		return "", err
	}
	return paid, nil
}

func salePaidTotalTxPool(ctx context.Context, pool *pgxpool.Pool, businessID, saleID string) (string, error) {
	var paid string
	err := pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(CASE WHEN kind = 'REFUND' THEN -amount ELSE amount END), 0)::text
		FROM payments WHERE business_id = $1 AND sale_id = $2`, businessID, saleID).Scan(&paid)
	if err != nil {
		return "", err
	}
	return paid, nil
}
