// Package pgstore cài đặt các repository port bằng PostgreSQL (pgx).
package pgstore

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/phuocnguyendev/youpass-share-service/internal/domain"
	"github.com/phuocnguyendev/youpass-share-service/internal/usecase/share"
)

const uniqueViolation = "23505"

type LinkRepository struct{ db *pgxpool.Pool }

func NewLinkRepository(db *pgxpool.Pool) *LinkRepository { return &LinkRepository{db: db} }

var (
	_ share.LinkRepository = (*LinkRepository)(nil)
	_ share.ViewStore      = (*LinkRepository)(nil)
)

const linkColumns = `id, code, owner_id, resource_type, resource_id, status,
	expires_at, deleted_at, view_count, created_at`

func scanLink(row pgx.Row) (*domain.Link, error) {
	var l domain.Link
	err := row.Scan(&l.ID, &l.Code, &l.OwnerID, &l.ResourceType, &l.ResourceID, &l.Status,
		&l.ExpiresAt, &l.DeletedAt, &l.ViewCount, &l.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrLinkNotFound
	}
	if err != nil {
		return nil, err
	}
	return &l, nil
}

func (r *LinkRepository) Insert(ctx context.Context, l *domain.Link) error {
	err := r.db.QueryRow(ctx, `
		INSERT INTO share_links (code, owner_id, resource_type, resource_id, status, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, created_at`,
		l.Code, l.OwnerID, l.ResourceType, l.ResourceID, l.Status, l.ExpiresAt,
	).Scan(&l.ID, &l.CreatedAt)

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
		if pgErr.ConstraintName == "uq_share_links_active_resource" {
			return share.ErrAlreadyShared
		}
		return share.ErrDuplicateCode
	}
	return err
}

func (r *LinkRepository) FindByCode(ctx context.Context, code string) (*domain.Link, error) {
	return scanLink(r.db.QueryRow(ctx,
		`SELECT `+linkColumns+` FROM share_links WHERE code = $1`, code))
}

func (r *LinkRepository) FindByResource(ctx context.Context, rt domain.ResourceType, id int64) (*domain.Link, error) {
	return scanLink(r.db.QueryRow(ctx,
		`SELECT `+linkColumns+` FROM share_links
		 WHERE resource_type = $1 AND resource_id = $2 AND deleted_at IS NULL`, rt, id))
}

func (r *LinkRepository) FindOwned(ctx context.Context, code string, ownerID int64) (*domain.Link, error) {
	return scanLink(r.db.QueryRow(ctx,
		`SELECT `+linkColumns+` FROM share_links
		 WHERE code = $1 AND owner_id = $2 AND deleted_at IS NULL`, code, ownerID))
}

func (r *LinkRepository) UpdateStatus(ctx context.Context, code string, ownerID int64, st domain.Status) (*domain.Link, error) {
	return scanLink(r.db.QueryRow(ctx, `
		UPDATE share_links SET status = $3, updated_at = now()
		WHERE code = $1 AND owner_id = $2 AND deleted_at IS NULL
		RETURNING `+linkColumns, code, ownerID, st))
}

func (r *LinkRepository) SoftDelete(ctx context.Context, code string, ownerID int64) error {
	tag, err := r.db.Exec(ctx, `
		UPDATE share_links SET deleted_at = now(), updated_at = now()
		WHERE code = $1 AND owner_id = $2 AND deleted_at IS NULL`, code, ownerID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrLinkNotFound
	}
	return nil
}

// AddViews cộng dồn view cho nhiều link trong 1 câu lệnh (batch).
func (r *LinkRepository) AddViews(ctx context.Context, deltas map[int64]int64) error {
	ids := make([]int64, 0, len(deltas))
	counts := make([]int64, 0, len(deltas))
	for id, n := range deltas {
		ids = append(ids, id)
		counts = append(counts, n)
	}
	_, err := r.db.Exec(ctx, `
		UPDATE share_links AS s
		SET view_count = s.view_count + v.delta, last_viewed_at = now()
		FROM (SELECT unnest($1::bigint[]) AS id, unnest($2::bigint[]) AS delta) AS v
		WHERE s.id = v.id`, ids, counts)
	return err
}
