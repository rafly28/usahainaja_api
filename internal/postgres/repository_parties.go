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
		if err := r.loadPartyDetails(ctx, businessID, &p); err != nil {
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

func (r *Repository) UpdateParty(ctx context.Context, businessID, userID, code, status string, input app.NewParty) (app.Party, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return app.Party{}, err
	}
	defer rollback(ctx, tx)

	var id string
	err = tx.QueryRow(ctx, `UPDATE parties SET party_type=$3,display_name=$4,legal_name=NULLIF($5,''),notes=NULLIF($6,''),status=$7,updated_at=now() WHERE business_id=$1 AND public_code=$2 RETURNING id::text`, businessID, code, input.PartyType, input.DisplayName, input.LegalName, input.Notes, status).Scan(&id)
	if err != nil {
		return app.Party{}, mapError(err)
	}
	if _, err = tx.Exec(ctx, `DELETE FROM party_relationships WHERE party_id=$1`, id); err != nil {
		return app.Party{}, err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM party_contacts WHERE party_id=$1`, id); err != nil {
		return app.Party{}, err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM party_addresses WHERE party_id=$1`, id); err != nil {
		return app.Party{}, err
	}
	for _, value := range input.Relationships {
		if _, err = tx.Exec(ctx, `INSERT INTO party_relationships (party_id,relationship_type) VALUES ($1,$2)`, id, value); err != nil {
			return app.Party{}, err
		}
	}
	for _, value := range input.Contacts {
		if _, err = tx.Exec(ctx, `INSERT INTO party_contacts (party_id,contact_type,label,value,is_primary) VALUES ($1,$2,NULLIF($3,''),$4,$5)`, id, value.Type, value.Label, value.Value, value.IsPrimary); err != nil {
			return app.Party{}, err
		}
	}
	for _, value := range input.Addresses {
		if _, err = tx.Exec(ctx, `INSERT INTO party_addresses (party_id,address_type,label,address,city,province,postal_code,is_primary) VALUES ($1,$2,NULLIF($3,''),$4,NULLIF($5,''),NULLIF($6,''),NULLIF($7,''),$8)`, id, value.Type, value.Label, value.Address, value.City, value.Province, value.PostalCode, value.IsPrimary); err != nil {
			return app.Party{}, err
		}
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit_logs (business_id,actor_user_id,entity_type,entity_id,entity_code,action,after_data) VALUES ($1,$2,'PARTY',$3,$4,'PARTY_UPDATED',jsonb_build_object('display_name',$5::text,'party_type',$6::text,'status',$7::text))`, businessID, userID, id, code, input.DisplayName, input.PartyType, status); err != nil {
		return app.Party{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return app.Party{}, err
	}
	return app.Party{Code: code, PartyType: input.PartyType, DisplayName: input.DisplayName, LegalName: input.LegalName, Notes: input.Notes, Status: status, Relationships: input.Relationships, Contacts: input.Contacts, Addresses: input.Addresses}, nil
}

func (r *Repository) loadPartyDetails(ctx context.Context, businessID string, party *app.Party) error {
	var partyID string
	if err := r.pool.QueryRow(ctx, `SELECT id::text FROM parties WHERE business_id=$1 AND public_code=$2`, businessID, party.Code).Scan(&partyID); err != nil {
		return mapError(err)
	}
	relationships, err := r.pool.Query(ctx, `SELECT relationship_type FROM party_relationships WHERE party_id=$1 ORDER BY relationship_type`, partyID)
	if err != nil {
		return err
	}
	defer relationships.Close()
	party.Relationships = make([]string, 0)
	for relationships.Next() {
		var value string
		if err := relationships.Scan(&value); err != nil {
			return err
		}
		party.Relationships = append(party.Relationships, value)
	}
	if err := relationships.Err(); err != nil {
		return err
	}
	contacts, err := r.pool.Query(ctx, `SELECT contact_type,COALESCE(label,''),value,is_primary FROM party_contacts WHERE party_id=$1 ORDER BY is_primary DESC,created_at`, partyID)
	if err != nil {
		return err
	}
	defer contacts.Close()
	party.Contacts = make([]app.PartyContact, 0)
	for contacts.Next() {
		var value app.PartyContact
		if err := contacts.Scan(&value.Type, &value.Label, &value.Value, &value.IsPrimary); err != nil {
			return err
		}
		party.Contacts = append(party.Contacts, value)
	}
	return contacts.Err()
}
