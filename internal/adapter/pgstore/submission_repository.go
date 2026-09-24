package pgstore

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/phuocnguyendev/youpass-share-service/internal/domain"
	"github.com/phuocnguyendev/youpass-share-service/internal/usecase/share"
	"github.com/phuocnguyendev/youpass-share-service/internal/usecase/submission"
)

type SubmissionRepository struct{ db *pgxpool.Pool }

func NewSubmissionRepository(db *pgxpool.Pool) *SubmissionRepository {
	return &SubmissionRepository{db: db}
}

var (
	_ share.ContentReader   = (*SubmissionRepository)(nil)
	_ submission.Repository = (*SubmissionRepository)(nil)
)

func (r *SubmissionRepository) IsOwner(ctx context.Context, rt domain.ResourceType, id, userID int64) (bool, error) {
	var ok bool
	err := r.db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM submissions
			WHERE id = $1 AND type = $2 AND user_id = $3 AND deleted_at IS NULL
		)`, id, rt, userID).Scan(&ok)
	return ok, err
}

func (r *SubmissionRepository) Snapshot(ctx context.Context, rt domain.ResourceType, id int64) (*domain.Snapshot, error) {
	var s domain.Snapshot
	err := r.db.QueryRow(ctx, `
		SELECT u.display_name, s.title, s.content,
		       COALESCE(s.band_score, 0), COALESCE(s.feedback, '')
		FROM submissions s
		JOIN users u ON u.id = s.user_id
		WHERE s.id = $1 AND s.type = $2
		  AND s.deleted_at IS NULL AND u.status = 'active'`, id, rt,
	).Scan(&s.OwnerName, &s.Title, &s.Content, &s.BandScore, &s.Feedback)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrSubmissionNotFound
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *SubmissionRepository) SoftDelete(ctx context.Context, userID, id int64) (domain.ResourceType, error) {
	var rt domain.ResourceType
	err := r.db.QueryRow(ctx, `
		UPDATE submissions SET deleted_at = now()
		WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL
		RETURNING type`, id, userID).Scan(&rt)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", domain.ErrSubmissionNotFound
	}
	return rt, err
}
