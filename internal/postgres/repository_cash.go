package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"

	"usahainaja/backend/internal/app"
)

func (r *Repository) ListCashAccounts(ctx context.Context, businessID string) ([]app.CashAccount, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT public_code, name, account_type, balance, is_default, status
		FROM cash_accounts
		WHERE business_id = $1
		ORDER BY is_default DESC, name`, businessID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]app.CashAccount, 0)
	for rows.Next() {
		var item app.CashAccount
		if err := rows.Scan(&item.Code, &item.Name, &item.AccountType, &item.Balance, &item.IsDefault, &item.Status); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) CreateCashAccount(ctx context.Context, businessID string, input app.NewCashAccount) (app.CashAccount, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return app.CashAccount{}, err
	}
	defer rollback(ctx, tx)

	if input.IsDefault {
		_, err = tx.Exec(ctx, `UPDATE cash_accounts SET is_default = false WHERE business_id = $1`, businessID)
		if err != nil {
			return app.CashAccount{}, err
		}
	} else {
		// If it's the first account, make it default automatically.
		var count int
		_ = tx.QueryRow(ctx, `SELECT COUNT(*) FROM cash_accounts WHERE business_id = $1`, businessID).Scan(&count)
		if count == 0 {
			input.IsDefault = true
		}
	}

	code, err := nextNumber(ctx, tx, businessID, "CASH")
	if err != nil {
		code = "CSH-" + input.Name[:3]
	}

	item := app.CashAccount{Code: code, Name: input.Name, AccountType: input.AccountType, Balance: input.Balance, IsDefault: input.IsDefault, Status: "ACTIVE"}
	_, err = tx.Exec(ctx, `
		INSERT INTO cash_accounts (
			business_id, public_code, name, account_type, balance, is_default, status
		) VALUES ($1, $2, $3, $4, $5, $6, 'ACTIVE')`,
		businessID, code, input.Name, input.AccountType, input.Balance, input.IsDefault,
	)
	if err != nil {
		return app.CashAccount{}, mapError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return app.CashAccount{}, err
	}
	return item, nil
}
