package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

func (r *Repository) FindIdempotency(ctx context.Context, businessID, key string) (int, []byte, string, bool, error) {
	var status int
	var body []byte
	var hash string
	err := r.pool.QueryRow(ctx, `
		SELECT response_status, response_body, request_hash
		FROM idempotency_keys WHERE business_id = $1 AND idempotency_key = $2`, businessID, key).Scan(&status, &body, &hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil, "", false, nil
	}
	if err != nil {
		return 0, nil, "", false, err
	}
	return status, body, hash, true, nil
}

func (r *Repository) StoreIdempotency(ctx context.Context, businessID, actorID, operation, route, key, hash string, status int, body []byte) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO idempotency_keys (business_id, actor_user_id, operation, route, idempotency_key, request_hash, response_status, response_body)
		VALUES ($1, NULLIF($2, '')::uuid, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (business_id, idempotency_key) DO NOTHING`,
		businessID, actorID, operation, route, key, hash, status, body)
	return err
}
