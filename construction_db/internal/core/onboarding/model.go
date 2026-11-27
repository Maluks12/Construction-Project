// Package onboarding is the first-run set-up (ONBOARD): certificates,
// medicals and company documents in bulk (D11), and the wizard's progress
// (D12). Both are for owners and SHE officers at the dashboard, online only.
package onboarding

import (
	"context"
	"errors"
	"time"

	"construction_db/internal/core/documents"
	"construction_db/internal/core/workers"
)

// ---- Bulk documents (D11) ----

const (
	KindCompetency    = "competency"
	KindMedical       = "medical"
	KindVaultDocument = "vault_document"

	MaxDocuments = 100
)

var Kinds = []string{KindCompetency, KindMedical, KindVaultDocument}

var (
	ErrBatchSize     = errors.New("items must hold 1-100 documents")
	ErrBadDryRun     = errors.New("dry_run must be true or false")
	ErrBadKind       = errors.New("kind must be competency, medical or vault_document")
	ErrPayload       = errors.New("a competency item carries competency and worker_id, a medical item medical and worker_id, a vault_document item document; nothing else")
	ErrRepeatedID    = errors.New("this id is used by another item of the batch")
	ErrHasProblems   = errors.New("some documents have problems; nothing was saved")
	ErrFilesNotReady = errors.New("some files aren't verified yet; complete them, then send the batch again")
)

// Sentinels are every error text these endpoints send besides the guards'
// and each item's own (onboarding.md quotes each).
var Sentinels = []error{ErrBatchSize, ErrBadDryRun, ErrBadKind, ErrPayload, ErrRepeatedID, ErrHasProblems, ErrFilesNotReady}

// DocumentItem is one record of the batch: its kind, the id the dashboard
// chose for it, and the same body as its own endpoint takes.
type DocumentItem struct {
	Kind       string                     `json:"kind"`
	ID         string                     `json:"id"`
	WorkerID   *string                    `json:"worker_id"`
	Competency *workers.CompetencyRequest `json:"competency"`
	Medical    *workers.MedicalRequest    `json:"medical"`
	Document   *documents.CreateRequest   `json:"document"`
}

type DocumentsRequest struct {
	Items []DocumentItem `json:"items"`
}

// ItemResult is one item saved, or (dry run) that would be: created false
// for a repeat of a record already saved under that id.
type ItemResult struct {
	Index   int    `json:"index"`
	Kind    string `json:"kind"`
	ID      string `json:"id"`
	Created bool   `json:"created"`
}

// ItemProblem is one item refused: its index in items, the field when the
// problem has one, and the text its own endpoint would send.
type ItemProblem struct {
	Index int     `json:"index"`
	Field *string `json:"field"`
	Error string  `json:"error"`
}

// DocumentsResult answers a batch, a dry run and a repeat alike. Error is
// set only when nothing was saved because of problems.
type DocumentsResult struct {
	DryRun   bool          `json:"dry_run"`
	Created  int           `json:"created"`
	Repeated int           `json:"repeated"`
	Items    []ItemResult  `json:"items"`
	Problems []ItemProblem `json:"problems"`
	Error    string        `json:"error,omitempty"`
}

// ---- Progress (D12) ----

const (
	StepSites                = "sites"
	StepTeam                 = "team"
	StepCompanyDocuments     = "company_documents"
	StepWorkers              = "workers"
	StepLocations            = "locations"
	StepWorkerDocuments      = "worker_documents"
	StepHSFile               = "hs_file"
	StepRequiredAppointments = "required_appointments"
	StepSiteWork             = "site_work"
)

// StepKeys are every step a progress may list, core first; an edition's
// appear only for sites of that edition.
var StepKeys = []string{StepSites, StepTeam, StepCompanyDocuments, StepWorkers, StepLocations, StepWorkerDocuments,
	StepHSFile, StepRequiredAppointments, StepSiteWork}

// Gap is something a step still needs: a document type, an appointment, on
// a site or the organisation's.
type Gap struct {
	SiteID  *string `json:"site_id"`
	TypeKey *string `json:"type_key"`
	Label   string  `json:"label"`
	Note    *string `json:"note"`
}

// Step is one item of the wizard. Required steps make the set-up complete;
// the others show with a note saying when they apply.
type Step struct {
	Key      string  `json:"key"`
	Required bool    `json:"required"`
	Done     bool    `json:"done"`
	Count    int     `json:"count"`
	Total    *int    `json:"total"`
	Note     *string `json:"note"`
	Gaps     []Gap   `json:"gaps"`
}

type Progress struct {
	Complete bool   `json:"complete"`
	Steps    []Step `json:"steps"`
}

// SiteRef is a live site as the steps need it.
type SiteRef struct {
	ID, Name, RegimeCode, Edition string
}

// StepContext is what a contributor computes its steps from.
type StepContext struct {
	OrgID, UserID, Role string
	Sites               []SiteRef // not archived
	Now                 time.Time
}

// Contributor is an edition's steps (found by a type assertion in
// app.NewRouter, like reports.Contributor).
type Contributor interface {
	OnboardingSteps(ctx context.Context, sc StepContext) ([]Step, error)
}
