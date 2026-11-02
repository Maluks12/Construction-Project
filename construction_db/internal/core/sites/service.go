package sites

import (
	"construction_db/internal/core/auth"
	"construction_db/internal/platform/database"
	"construction_db/internal/regimes"
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Service struct {
	pool          *pgxpool.Pool
	regimes       *regimes.Registry
	defaultRegime string
}

// defaultRegime is used for sites created with POST /v1/sites.
func NewService(pool *pgxpool.Pool, reg *regimes.Registry, defaultRegime string) *Service {
	return &Service{pool: pool, regimes: reg, defaultRegime: defaultRegime}
}

func (s *Service) inTenant(ctx context.Context, orgID, userID string, fn func(pgx.Tx) error) error {
	return database.WithTenant(ctx, s.pool, database.Tenant{OrgID: orgID, UserID: userID}, fn)
}

func (s *Service) List(ctx context.Context, orgID string, userID, role string) ([]Site, error) {
	var sites []Site
	err := s.inTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		var err error
		if role == auth.RoleSupervisor {
			sites, err = ListAssignedSites(ctx, tx, orgID, userID)
		} else {
			sites, err = ListSites(ctx, tx, orgID)
		}
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("list sites: %w", err)
	}
	return sites, nil
}

func (s *Service) Get(ctx context.Context, orgID, userID, role, siteID string) (*Site, error) {
	var site *Site
	err := s.inTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		var err error
		site, err = s.get(ctx, tx, orgID, userID, role, siteID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return site, nil
}

// get applies supervisor scoping inside an existing tenant transaction.
func (s *Service) get(ctx context.Context, tx pgx.Tx, orgID, userID, role, siteID string) (*Site, error) {
	var site *Site
	var err error
	if role == auth.RoleSupervisor {
		site, err = GetAssignedSite(ctx, tx, orgID, userID, siteID)
	} else {
		site, err = GetSite(ctx, tx, orgID, siteID)
	}

	if isNotFound(err) {
		return nil, ErrSiteNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get site: %w", err)
	}
	return site, nil
}

// GetInTx is Get inside the caller's tenant transaction, so other features
// apply the same supervisor scoping as GET /v1/sites/:id.
func (s *Service) GetInTx(ctx context.Context, tx pgx.Tx, orgID, userID, role, siteID string) (*Site, error) {
	return s.get(ctx, tx, orgID, userID, role, siteID)
}

// Regime is the one place that resolves a site's regime data. It reads
// sites.regime_id, never the organisation, and applies the same scoping as Get.
func (s *Service) Regime(ctx context.Context, orgID, userID, role, siteID string) (*regimes.DataSet, error) {
	var code string
	err := s.inTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		if _, err := s.get(ctx, tx, orgID, userID, role, siteID); err != nil {
			return err
		}
		var err error
		code, err = GetSiteRegimeCode(ctx, tx, orgID, siteID)
		if isNotFound(err) {
			return ErrSiteNotFound
		}
		return err
	})
	if errors.Is(err, ErrSiteNotFound) {
		return nil, err
	}
	if err != nil {
		return nil, fmt.Errorf("site regime: %w", err)
	}

	ds, ok := s.regimes.Get(code)
	if !ok {
		return nil, fmt.Errorf("site regime: no data set for %q", code)
	}
	return ds, nil
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
	var site *Site
	err := s.inTenant(ctx, orgID, "", func(tx pgx.Tx) error {
		var err error
		site, err = CreateSite(ctx, tx, orgID, s.defaultRegime, req.Name, req.Address)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("create site: %w", err)
	}
	return site, nil
}

func (s *Service) Update(ctx context.Context, orgID, siteID string, req UpdateSiteRequest) (*Site, error) {
	var site *Site
	err := s.inTenant(ctx, orgID, "", func(tx pgx.Tx) error {
		var err error
		site, err = UpdateSite(ctx, tx, orgID, siteID, req.Name, req.Address)
		return err
	})
	if isNotFound(err) {
		return nil, ErrSiteNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("update site: %w", err)
	}

	return site, nil
}

func (s *Service) Archive(ctx context.Context, orgID, siteID string) error {
	var archived bool
	err := s.inTenant(ctx, orgID, "", func(tx pgx.Tx) error {
		var err error
		archived, err = ArchiveSite(ctx, tx, orgID, siteID)
		return err
	})
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
	var members []SiteMember
	err := s.inTenant(ctx, orgID, "", func(tx pgx.Tx) error {
		_, err := GetSite(ctx, tx, orgID, siteID)
		if isNotFound(err) {
			return ErrSiteNotFound
		}
		if err != nil {
			return err
		}

		members, err = ListSiteMembers(ctx, tx, orgID, siteID)
		return err
	})
	if errors.Is(err, ErrSiteNotFound) {
		return nil, err
	}
	if err != nil {
		return nil, fmt.Errorf("list site members: %w", err)
	}

	return members, nil
}

func (s *Service) AddMember(ctx context.Context, orgID, siteID, userID string) error {
	err := s.inTenant(ctx, orgID, "", func(tx pgx.Tx) error {
		_, err := GetSite(ctx, tx, orgID, siteID)
		if isNotFound(err) {
			return ErrSiteNotFound
		}
		if err != nil {
			return err
		}

		err = AddSiteMember(ctx, tx, orgID, siteID, userID)
		if isForeignKeyViolation(err) || isNotFound(err) {
			return ErrUserNotFound
		}
		return err
	})
	if errors.Is(err, ErrSiteNotFound) || errors.Is(err, ErrUserNotFound) {
		return err
	}
	if err != nil {
		return fmt.Errorf("add site members: %w", err)
	}

	return nil
}

func (s *Service) RemoveMember(ctx context.Context, orgID, siteID, userID string) error {
	var removed bool
	err := s.inTenant(ctx, orgID, "", func(tx pgx.Tx) error {
		var err error
		removed, err = RemoveSiteMember(ctx, tx, orgID, siteID, userID)
		return err
	})
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
	var locations []Location
	err := s.inTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		if _, err := s.get(ctx, tx, orgID, userID, role, siteID); err != nil {
			return err
		}
		var err error
		locations, err = ListLocations(ctx, tx, orgID, siteID)
		return err
	})
	if errors.Is(err, ErrSiteNotFound) {
		return nil, err
	}
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

	var location *Location
	err := s.inTenant(ctx, orgID, "", func(tx pgx.Tx) error {
		_, err := GetSite(ctx, tx, orgID, siteID)
		if isNotFound(err) {
			return ErrSiteNotFound
		}
		if err != nil {
			return err
		}

		location, err = CreateLocation(ctx, tx, orgID, siteID, req.ParentID, req.Kind, name)
		if isNotFound(err) {
			return ErrParentNotFound
		}
		if isUniqueViolation(err, "site_locations_sibling_name_unique") {
			return ErrLocationNameTaken
		}
		return err
	})
	if errors.Is(err, ErrSiteNotFound) || errors.Is(err, ErrParentNotFound) || errors.Is(err, ErrLocationNameTaken) {
		return nil, err
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

	var location *Location
	err := s.inTenant(ctx, orgID, "", func(tx pgx.Tx) error {
		var err error
		location, err = RenameLocation(ctx, tx, orgID, siteID, locationID, name)
		return err
	})

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
	err := s.inTenant(ctx, orgID, "", func(tx pgx.Tx) error {
		hasChildren, err := HasActiveChildren(ctx, tx, orgID, siteID, locationID)
		if isNotFound(err) {
			return ErrLocationNotFound
		}
		if err != nil {
			return err
		}

		if hasChildren {
			return ErrLocationHasChildren
		}

		archived, err := ArchiveLocation(ctx, tx, orgID, siteID, locationID)
		if err != nil {
			return err
		}
		if !archived {
			return ErrLocationNotFound
		}
		return nil
	})
	if errors.Is(err, ErrLocationNotFound) || errors.Is(err, ErrLocationHasChildren) {
		return err
	}
	if err != nil {
		return fmt.Errorf("archive location: %w", err)
	}

	return nil
}
