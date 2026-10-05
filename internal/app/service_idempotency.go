package app

import "context"

func (s *Service) FindIdempotency(ctx context.Context, businessID, key string) (int, []byte, string, bool, error) {
	status, body, hash, found, err := s.repo.FindIdempotency(ctx, businessID, key)
	if err != nil {
		return 0, nil, "", false, &Error{Code: "INTERNAL_ERROR", Message: "Tidak dapat memeriksa idempotency.", Cause: err}
	}
	return status, body, hash, found, nil
}

func (s *Service) StoreIdempotency(ctx context.Context, businessID, actorID, operation, route, key, hash string, status int, body []byte) error {
	if err := s.repo.StoreIdempotency(ctx, businessID, actorID, operation, route, key, hash, status, body); err != nil {
		return &Error{Code: "INTERNAL_ERROR", Message: "Tidak dapat menyimpan idempotency.", Cause: err}
	}
	return nil
}
