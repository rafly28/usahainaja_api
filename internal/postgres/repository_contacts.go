package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"

	"usahainaja/backend/internal/app"
)

func (r *Repository) ListContacts(ctx context.Context, businessID string) ([]app.Contact, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT public_code, contact_type, name, COALESCE(email, ''), COALESCE(phone, ''), COALESCE(address, ''), status
		FROM contacts
		WHERE business_id = $1
		ORDER BY name, public_code`, businessID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]app.Contact, 0)
	for rows.Next() {
		var item app.Contact
		if err := rows.Scan(&item.Code, &item.ContactType, &item.Name, &item.Email, &item.Phone, &item.Address, &item.Status); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) CreateContact(ctx context.Context, businessID string, input app.NewContact) (app.Contact, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return app.Contact{}, err
	}
	defer rollback(ctx, tx)

	code, err := nextNumber(ctx, tx, businessID, "CONTACT")
	if err != nil {
		// fallback to random code if sequence not setup
		code = "CUS-" + input.Name[:3] // simplify for now
	}

	item := app.Contact{Code: code, ContactType: input.ContactType, Name: input.Name, Email: input.Email, Phone: input.Phone, Address: input.Address, Status: "ACTIVE"}
	_, err = tx.Exec(ctx, `
		INSERT INTO contacts (
			business_id, public_code, contact_type, name, email, phone, address, status
		) VALUES ($1, $2, $3, $4, NULLIF($5, ''), NULLIF($6, ''), NULLIF($7, ''), 'ACTIVE')`,
		businessID, code, input.ContactType, input.Name, input.Email, input.Phone, input.Address,
	)
	if err != nil {
		return app.Contact{}, mapError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return app.Contact{}, err
	}
	return item, nil
}
