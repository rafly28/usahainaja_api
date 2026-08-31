package postgres

import (
	"context"

	"usahainaja/backend/internal/app"
)

func (r *Repository) ListBusinessMembers(ctx context.Context, businessID string) ([]app.BusinessMember, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT u.public_code,u.name,u.email,roles.code,bm.status
		FROM business_members bm
		JOIN users u ON u.id=bm.user_id
		JOIN roles ON roles.id=bm.role_id AND roles.business_id=bm.business_id
		WHERE bm.business_id=$1
		ORDER BY CASE roles.code WHEN 'OWNER' THEN 0 WHEN 'ADMIN' THEN 1 ELSE 2 END,u.name,u.public_code`, businessID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]app.BusinessMember, 0)
	for rows.Next() {
		var item app.BusinessMember
		if err := rows.Scan(&item.UserCode, &item.Name, &item.Email, &item.Role, &item.Status); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) InviteBusinessMember(ctx context.Context, businessID, actorID, email, roleCode string) (app.BusinessMember, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return app.BusinessMember{}, err
	}
	defer rollback(ctx, tx)
	var userID, userCode, name, resolvedEmail, roleID string
	err = tx.QueryRow(ctx, `SELECT id::text,public_code,name,email FROM users WHERE lower(email)=lower($1) AND status='ACTIVE'`, email).Scan(&userID, &userCode, &name, &resolvedEmail)
	if err != nil {
		return app.BusinessMember{}, mapError(err)
	}
	err = tx.QueryRow(ctx, `SELECT id::text FROM roles WHERE business_id=$1 AND code=$2`, businessID, roleCode).Scan(&roleID)
	if err != nil {
		return app.BusinessMember{}, mapError(err)
	}
	_, err = tx.Exec(ctx, `INSERT INTO business_members (business_id,user_id,role_id,status) VALUES ($1,$2,$3,'INVITED')`, businessID, userID, roleID)
	if err != nil {
		return app.BusinessMember{}, mapError(err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit_logs (business_id,actor_user_id,entity_type,entity_id,entity_code,action,after_data) VALUES ($1,$2,'BUSINESS_MEMBER',$3,$4,'MEMBER_INVITED',jsonb_build_object('role',$5::text,'email',$6::text))`, businessID, actorID, userID, userCode, roleCode, resolvedEmail); err != nil {
		return app.BusinessMember{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return app.BusinessMember{}, err
	}
	return app.BusinessMember{UserCode: userCode, Name: name, Email: resolvedEmail, Role: roleCode, Status: "INVITED"}, nil
}

func (r *Repository) UpdateBusinessMember(ctx context.Context, businessID, actorID, userCode, roleCode, status string) (app.BusinessMember, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return app.BusinessMember{}, err
	}
	defer rollback(ctx, tx)
	var memberID, userID, currentRole, currentStatus, name, email string
	err = tx.QueryRow(ctx, `
		SELECT bm.id::text,bm.user_id::text,roles.code,bm.status,u.name,u.email
		FROM business_members bm JOIN users u ON u.id=bm.user_id JOIN roles ON roles.id=bm.role_id AND roles.business_id=bm.business_id
		WHERE bm.business_id=$1 AND u.public_code=$2 FOR UPDATE`, businessID, userCode).Scan(&memberID, &userID, &currentRole, &currentStatus, &name, &email)
	if err != nil {
		return app.BusinessMember{}, mapError(err)
	}
	if currentRole == "OWNER" && (roleCode != "OWNER" || status != "ACTIVE") {
		var ownerCount int
		if err = tx.QueryRow(ctx, `SELECT count(*) FROM business_members bm JOIN roles ON roles.id=bm.role_id AND roles.business_id=bm.business_id WHERE bm.business_id=$1 AND bm.status='ACTIVE' AND roles.code='OWNER'`, businessID).Scan(&ownerCount); err != nil {
			return app.BusinessMember{}, err
		}
		if ownerCount <= 1 {
			return app.BusinessMember{}, app.ErrForbidden
		}
	}
	var roleID string
	if err = tx.QueryRow(ctx, `SELECT id::text FROM roles WHERE business_id=$1 AND code=$2`, businessID, roleCode).Scan(&roleID); err != nil {
		return app.BusinessMember{}, mapError(err)
	}
	_, err = tx.Exec(ctx, `UPDATE business_members SET role_id=$3,status=$4,joined_at=CASE WHEN $4='ACTIVE' THEN COALESCE(joined_at,now()) ELSE joined_at END,updated_at=now() WHERE id=$1 AND business_id=$2`, memberID, businessID, roleID, status)
	if err != nil {
		return app.BusinessMember{}, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit_logs (business_id,actor_user_id,entity_type,entity_id,entity_code,action,before_data,after_data) VALUES ($1,$2,'BUSINESS_MEMBER',$3,$4,'MEMBER_UPDATED',jsonb_build_object('role',$5::text,'status',$6::text),jsonb_build_object('role',$7::text,'status',$8::text))`, businessID, actorID, userID, userCode, currentRole, currentStatus, roleCode, status); err != nil {
		return app.BusinessMember{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return app.BusinessMember{}, err
	}
	return app.BusinessMember{UserCode: userCode, Name: name, Email: email, Role: roleCode, Status: status}, nil
}
