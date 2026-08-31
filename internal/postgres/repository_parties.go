package postgres

import (
	"context"
	"usahainaja/backend/internal/app"
)

func (r *Repository) ListParties(ctx context.Context, businessID string) ([]app.Party, error) {
	rows, err := r.pool.Query(ctx, `SELECT public_code,party_type,display_name,COALESCE(legal_name,''),status,COALESCE(notes,'') FROM parties WHERE business_id=$1 ORDER BY display_name,public_code`, businessID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]app.Party, 0)
	for rows.Next() {
		var p app.Party
		if err := rows.Scan(&p.Code, &p.PartyType, &p.DisplayName, &p.LegalName, &p.Status, &p.Notes); err != nil {
			return nil, err
		}
		items = append(items, p)
	}
	return items, rows.Err()
}
func (r *Repository) CreateParty(ctx context.Context, businessID, userID string, input app.NewParty) (app.Party, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return app.Party{}, err
	}
	defer rollback(ctx, tx)
	code, err := nextNumber(ctx, tx, businessID, "PARTY")
	if err != nil {
		return app.Party{}, err
	}
	var id string
	err = tx.QueryRow(ctx, `INSERT INTO parties (business_id,public_code,party_type,display_name,legal_name,notes) VALUES ($1,$2,$3,$4,NULLIF($5,''),NULLIF($6,'')) RETURNING id::text`, businessID, code, input.PartyType, input.DisplayName, input.LegalName, input.Notes).Scan(&id)
	if err != nil {
		return app.Party{}, mapError(err)
	}
	for _, v := range input.Relationships {
		if _, err = tx.Exec(ctx, `INSERT INTO party_relationships (party_id,relationship_type) VALUES ($1,$2)`, id, v); err != nil {
			return app.Party{}, err
		}
	}
	for _, v := range input.Contacts {
		if _, err = tx.Exec(ctx, `INSERT INTO party_contacts (party_id,contact_type,label,value,is_primary) VALUES ($1,$2,NULLIF($3,''),$4,$5)`, id, v.Type, v.Label, v.Value, v.IsPrimary); err != nil {
			return app.Party{}, err
		}
	}
	for _, v := range input.Addresses {
		if _, err = tx.Exec(ctx, `INSERT INTO party_addresses (party_id,address_type,label,address,city,province,postal_code,is_primary) VALUES ($1,$2,NULLIF($3,''),$4,NULLIF($5,''),NULLIF($6,''),NULLIF($7,''),$8)`, id, v.Type, v.Label, v.Address, v.City, v.Province, v.PostalCode, v.IsPrimary); err != nil {
			return app.Party{}, err
		}
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit_logs (business_id,actor_user_id,entity_type,entity_id,entity_code,action,after_data) VALUES ($1,$2,'PARTY',$3,$4,'PARTY_CREATED',jsonb_build_object('display_name',$5::text,'party_type',$6::text))`, businessID, userID, id, code, input.DisplayName, input.PartyType); err != nil {
		return app.Party{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return app.Party{}, err
	}
	return app.Party{Code: code, PartyType: input.PartyType, DisplayName: input.DisplayName, LegalName: input.LegalName, Notes: input.Notes, Status: "ACTIVE", Relationships: input.Relationships, Contacts: input.Contacts, Addresses: input.Addresses}, nil
}
