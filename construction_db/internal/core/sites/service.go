package sites

import (
	"construction_db/internal/core/auth"
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Service struct {
	pool *pgxpool.Pool
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool}
}

func (s *Service) List(ctx context.Context, orgID string) ([]Site, error) {
	sites, err := ListSites(ctx, s.pool, orgID)
	if err != nil {
		return nil, fmt.Errorf("list sites: %w", err)
	}
	return sites, nil
}

func (s *Service) Get(ctx context.Context, orgID, siteID string) (*Site, error) {
	site, err := GetSite(ctx, s.pool, orgID, siteID)
	if isNotFound(err) {
		return nil, ErrSiteNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get site: %w", err)
	}
	return site, nil
}

func isNotFound(err error) bool {
	if errors.Is(err, pgx.ErrNoRows) {
		return true
	}
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "22P02"
}

func (s *Service) Create(ctx context.Context, orgID string, req CreateSiteRequest) (*Site, error) {
	site, err := CreateSite(ctx, s.pool, orgID, auth.RegimeConstruction, req.Name, req.Address)
	if err != nil {
		return nil, fmt.Errorf("create site: %w", err)
	}
	return site, nil
}

func (s *Service) Update(ctx context.Context, orgID, siteID string, req UpdateSiteRequest) (*Site, error) {
	site, err := UpdateSite(ctx, s.pool, orgID, siteID, req.Name, req.Address)
	if isNotFound(err) {
		return nil, ErrSiteNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("update site: %w", err)
	}

	return site, nil
}

func (s *Service) Archive(ctx context.Context, orgID, siteID string) error {
	archived, err := ArchiveSite(ctx, s.pool, orgID, siteID)
	if isNotFound(err) {
		return ErrSiteNotFound
	}

	if err != nil {
		return fmt.Errorf("archive site: %w", err)
	}

	if !archived {
		return ErrSiteNotFound
	}

	return nil
}
