package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"

	"construction_db/internal/core/files"
	"construction_db/internal/platform/storagetest"
)

func filePath(id string) string { return "/v1/files/" + id }

// testPDF is a small file that passes the PDF check; salt makes each unique.
func testPDF(salt string) []byte {
	return []byte("%PDF-1.7\n% " + salt + "\n1 0 obj << >> endobj\ntrailer << >>\n%%EOF\n")
}

func fileBody(content []byte, contentType, purpose string, site *string) map[string]any {
	sum := sha256.Sum256(content)
	body := map[string]any{
		"purpose":        purpose,
		"content_sha256": hex.EncodeToString(sum[:]),
		"size_bytes":     len(content),
		"content_type":   contentType,
		"file_name":      "certificate.pdf",
	}
	if site != nil {
		body["site_id"] = *site
	}
	return body
}

func decodeFile(t *testing.T, rec *httptest.ResponseRecorder) files.File {
	t.Helper()
	var v files.File
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode file: %v (body: %s)", err, rec.Body.String())
	}
	return v
}

// uploadFile declares, uploads and completes a PDF for purpose; it returns
// the verified file's id.
func uploadFile(t *testing.T, router http.Handler, token, purpose string, site *string) string {
	t.Helper()
	id := uuid.NewString()
	content := testPDF(id)
	rec := do(t, router, http.MethodPut, filePath(id), token, fileBody(content, "application/pdf", purpose, site))
	wantStatus(t, rec, http.StatusCreated)
	putUpload(t, decodeFile(t, rec).Upload, content)
	wantStatus(t, do(t, router, http.MethodPost, filePath(id)+"/complete", token, nil), http.StatusCreated)
	return id
}

func TestFileUploadAndComplete(t *testing.T) {
	f := newEvidenceFixture(t, "files")
	id := uuid.NewString()
	content := testPDF("first")
	body := fileBody(content, "application/pdf", files.PurposeCompetency, nil)

	rec := do(t, f.router, http.MethodPut, filePath(id), f.supervisor, body)
	wantStatus(t, rec, http.StatusCreated)
	declared := decodeFile(t, rec)
	if declared.Status != files.StatusPending || declared.Upload == nil || declared.Purpose != files.PurposeCompetency {
		t.Fatalf("declared %+v", declared)
	}
	// Completing before the upload changes nothing.
	wantError(t, do(t, f.router, http.MethodPost, filePath(id)+"/complete", f.supervisor, nil), http.StatusConflict, "file not uploaded yet")
	putUpload(t, declared.Upload, content)
	// Only the uploader completes.
	wantError(t, do(t, f.router, http.MethodPost, filePath(id)+"/complete", f.owner, nil), http.StatusNotFound, "file not found")
	rec = do(t, f.router, http.MethodPost, filePath(id)+"/complete", f.supervisor, nil)
	wantStatus(t, rec, http.StatusCreated)
	if v := decodeFile(t, rec); v.Status != files.StatusVerified || v.RetainUntil == nil || v.Upload != nil {
		t.Errorf("completed %+v", v)
	}
	wantStatus(t, do(t, f.router, http.MethodPost, filePath(id)+"/complete", f.supervisor, nil), http.StatusOK)

	// A repeat declare answers the verified file, without an upload.
	rec = do(t, f.router, http.MethodPut, filePath(id), f.supervisor, body)
	wantStatus(t, rec, http.StatusOK)
	if decodeFile(t, rec).Upload != nil {
		t.Error("a verified file got an upload URL")
	}
	wantError(t, do(t, f.router, http.MethodPut, filePath(id), f.supervisor, with(body, "file_name", "other.pdf")),
		http.StatusConflict, "file id already used with different details")

	// The record: the uploader and leads; another org can't tell it exists.
	wantStatus(t, do(t, f.router, http.MethodGet, filePath(id), f.supervisor, nil), http.StatusOK)
	wantStatus(t, do(t, f.router, http.MethodGet, filePath(id), f.owner, nil), http.StatusOK)
	wantError(t, do(t, f.router, http.MethodGet, filePath(id), f.other, nil), http.StatusNotFound, "file not found")
	wantStatus(t, do(t, f.router, http.MethodGet, filePath(id), f.bookkeeper, nil), http.StatusForbidden)
}

func TestFileDeclareRules(t *testing.T) {
	f := newEvidenceFixture(t, "filerules")
	pdf := testPDF("rules")
	put := func(token string, body map[string]any) *httptest.ResponseRecorder {
		return do(t, f.router, http.MethodPut, filePath(uuid.NewString()), token, body)
	}
	org := fileBody(pdf, "application/pdf", files.PurposeOrgDocument, nil)
	site := fileBody(pdf, "application/pdf", files.PurposeSiteDocument, &f.siteA)

	wantError(t, put(f.supervisor, org), http.StatusForbidden, "only an owner or SHE officer can upload this kind of file")
	wantError(t, put(f.supervisor, site), http.StatusForbidden, "only an owner or SHE officer can upload this kind of file")
	wantStatus(t, put(f.supervisor, fileBody(pdf, "application/pdf", files.PurposeMedical, nil)), http.StatusCreated)
	wantStatus(t, put(f.owner, org), http.StatusCreated)
	wantStatus(t, put(f.owner, site), http.StatusCreated)
	wantError(t, put(f.owner, with(org, "site_id", f.siteA)), http.StatusBadRequest,
		"site_id is required for appointment and site_document files, and only for them")
	wantError(t, put(f.owner, fileBody(pdf, "application/pdf", files.PurposeSiteDocument, nil)), http.StatusBadRequest,
		"site_id is required for appointment and site_document files, and only for them")
	wantError(t, put(f.owner, with(site, "site_id", f.otherSite)), http.StatusNotFound, "site not found")
	wantError(t, put(f.owner, with(org, "purpose", "photo")), http.StatusBadRequest,
		"purpose must be competency, medical, appointment, site_document or org_document")
	wantError(t, put(f.owner, with(org, "content_type", "text/html")), http.StatusBadRequest,
		"content_type must be application/pdf, image/jpeg or image/png")
	wantError(t, put(f.owner, with(org, "size_bytes", files.MaxSizeBytes+1)), http.StatusBadRequest, "size_bytes must be 1-20971520")
	wantError(t, put(f.owner, with(org, "content_sha256", "abc")), http.StatusBadRequest, "content_sha256 must be 64 hex characters")
	wantError(t, put(f.owner, with(org, "file_name", " ")), http.StatusBadRequest, "file_name must be 1-200 characters")
	wantError(t, put(f.owner, with(org, "device_id", "")), http.StatusBadRequest, "device_id must be 1-200 bytes")
	wantError(t, do(t, f.router, http.MethodPut, filePath("nope"), f.owner, org), http.StatusBadRequest, "file id must be a UUID")
}

func TestFileCompleteRejects(t *testing.T) {
	f := newEvidenceFixture(t, "filereject")
	declare := func(content []byte, contentType string) string {
		t.Helper()
		id := uuid.NewString()
		wantStatus(t, do(t, f.router, http.MethodPut, filePath(id), f.owner, fileBody(content, contentType, files.PurposeOrgDocument, nil)), http.StatusCreated)
		return id
	}
	writeDirect := func(id string, content []byte) {
		t.Helper()
		_, err := testS3.Admin.PutObject(context.Background(), &s3.PutObjectInput{
			Bucket: aws.String(storagetest.Bucket), Key: aws.String("orgs/" + f.ownerMe.OrgID + "/files/" + id), Body: bytes.NewReader(content),
		})
		if err != nil {
			t.Fatalf("direct put: %v", err)
		}
	}
	reject := func(id, reason string) {
		t.Helper()
		rec := do(t, f.router, http.MethodPost, filePath(id)+"/complete", f.owner, nil)
		wantStatus(t, rec, http.StatusBadRequest)
		var body struct {
			RejectedReason string `json:"rejected_reason"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if body.RejectedReason != reason {
			t.Errorf("rejected_reason %q, want %q (body %s)", body.RejectedReason, reason, rec.Body.String())
		}
	}

	pdf := testPDF("reject")
	id := declare(pdf, "application/pdf")
	writeDirect(id, append(append([]byte{}, pdf...), 'x'))
	reject(id, files.ReasonSizeMismatch)

	id = declare(pdf, "application/pdf")
	other := bytes.Clone(pdf)
	other[len(other)-2] = 'X'
	writeDirect(id, other)
	reject(id, files.ReasonHashMismatch)

	notPDF := []byte("<html>not a pdf</html>")
	id = declare(notPDF, "application/pdf")
	writeDirect(id, notPDF)
	reject(id, files.ReasonWrongType)

	jpg := testImage(t, "jpeg", 40, 30)
	id = declare(jpg, "image/png")
	writeDirect(id, jpg)
	reject(id, files.ReasonWrongType)
	// Rejected for good.
	wantError(t, do(t, f.router, http.MethodPost, filePath(id)+"/complete", f.owner, nil), http.StatusBadRequest, "file was rejected; declare a new file id")

	// A JPEG of any size is accepted for documents.
	big := testImage(t, "jpeg", 3000, 20)
	id = declare(big, "image/jpeg")
	writeDirect(id, big)
	wantStatus(t, do(t, f.router, http.MethodPost, filePath(id)+"/complete", f.owner, nil), http.StatusCreated)
}
