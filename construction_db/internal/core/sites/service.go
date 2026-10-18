package sites

import (
	"construction_db/internal/core/auth"
	"context"
	"errors"
	"fmt"
	"strings"

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

func (s *Service) List(ctx context.Context, orgID string, userID, role string) ([]Site, error) {
	var sites []Site
	var err error

	if role == auth.RoleSupervisor {
		sites, err = ListAssignedSites(ctx, s.pool, orgID, userID)
	} else {
		sites, err = ListSites(ctx, s.pool, orgID)
	}

	if err != nil {
		return nil, fmt.Errorf("list sites: %w", err)
	}
	return sites, nil
}

func (s *Service) Get(ctx context.Context, orgID, userID, role, siteID string) (*Site, error) {
	var site *Site
	var err error

	if role == auth.RoleSupervisor {
		site, err = GetAssignedSite(ctx, s.pool, orgID, userID, siteID)
	} else {
		site, err = GetSite(ctx, s.pool, orgID, siteID)
	}

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

func isUniqueViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) &&
		pgErr.Code == "23505" &&
		pgErr.ConstraintName == constraint
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

func (s *Service) ListMembers(ctx context.Context, orgID, siteID string) ([]SiteMember, error) {

	_, err := GetSite(ctx, s.pool, orgID, siteID)
	if isNotFound(err) {
		return nil, ErrSiteNotFound
	}

	if err != nil {
		return nil, fmt.Errorf("list site members: %w", err)
	}

	members, err := ListSiteMembers(ctx, s.pool, orgID, siteID)

	if err != nil {
		return nil, fmt.Errorf("list site members: %w", err)
	}

	return members, nil
}

func (s *Service) AddMember(ctx context.Context, orgID, siteID, userID string) error {
	_, err := GetSite(ctx, s.pool, orgID, siteID)
	if isNotFound(err) {
		return ErrSiteNotFound
	}

	if err != nil {
		return fmt.Errorf("add site members: %w", err)
	}

	err = AddSiteMember(ctx, s.pool, orgID, siteID, userID)
	if isForeignKeyViolation(err) || isNotFound(err) {
		return ErrUserNotFound
	}

	if err != nil {
		return fmt.Errorf("add site members: %w", err)
	}

	return nil
}

func (s *Service) RemoveMember(ctx context.Context, orgID, siteID, userID string) error {
	removed, err := RemoveSiteMember(ctx, s.pool, orgID, siteID, userID)
	if isNotFound(err) {
		return ErrSiteNotFound
	}

	if err != nil {
		return fmt.Errorf("remove site member: %w", err)
	}

	if !removed {
		return ErrUserNotFound
	}
	return nil
}

func isForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23503"
}

func (s *Service) ListLocations(ctx context.Context, orgID, userID, role, siteID string) ([]Location, error) {
	if _, err := s.Get(ctx, orgID, userID, role, siteID); err != nil {
		return nil, err
	}

	locations, err := ListLocations(ctx, s.pool, orgID, siteID)

	if err != nil {
		return nil, fmt.Errorf("list locations: %w", err)
	}
	return locations, nil

}

func (s *Service) CreateLocation(ctx context.Context, orgID, siteID string, req CreateLocationRequest) (*Location, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, ErrBlankName
	}

	_, err := GetSite(ctx, s.pool, orgID, siteID)
	if isNotFound(err) {
		return nil, ErrSiteNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("create location: %w", err)
	}

	location, err := CreateLocation(ctx, s.pool, orgID, siteID, req.ParentID, req.Kind, name)
	if isNotFound(err) {
		return nil, ErrParentNotFound
	}
	if isUniqueViolation(err, "site_locations_sibling_name_unique") {
		return nil, ErrLocationNameTaken
	}
	if err != nil {
		return nil, fmt.Errorf("create location: %w", err)
	}
	return location, nil
}

func (s *Service) RenameLocation(ctx context.Context, orgID, siteID, locationID string, req UpdateLocationRequest) (*Location, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, ErrBlankName
	}

	location, err := RenameLocation(ctx, s.pool, orgID, siteID, locationID, name)

	if isNotFound(err) {
		return nil, ErrLocationNotFound
	}

	if isUniqueViolation(err, "site_locations_sibling_name_unique") {
		return nil, ErrLocationNameTaken
	}

	if err != nil {
		return nil, fmt.Errorf("rename location: %w", err)
	}

	return location, nil
}

func (s *Service) ArchiveLocation(ctx context.Context, orgID, siteID, locationID string) error {
	hasChildren, err := HasActiveChildren(ctx, s.pool, orgID, siteID, locationID)
	if isNotFound(err) {
		return ErrLocationNotFound
	}

	if err != nil {
		return fmt.Errorf("archive location: %w", err)
	}

	if hasChildren {
		return ErrLocationHasChildren
	}

	archived, err := ArchiveLocation(ctx, s.pool, orgID, siteID, locationID)
	if err != nil {
		return fmt.Errorf("archive location: %w", err)
	}

	if !archived {
		return ErrLocationNotFound
	}

	return nil
}
