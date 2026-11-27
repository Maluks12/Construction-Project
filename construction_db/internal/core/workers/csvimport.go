package workers

import (
	"bytes"
	"encoding/csv"
	"errors"
	"io"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"
)

// The worker register's CSV import (ONBOARD D9). What South African Excel
// writes: "CSV UTF-8" (with a BOM) or plain "CSV" (Windows-1252, no BOM),
// CRLF line ends, and a comma or, where the list separator is a semicolon
// (comma-decimal locales), a semicolon. There are no date columns: a
// competency or medical needs its certificate, so those come through the
// bulk documents (onboarding.md).

const (
	MaxImportRows = 5000

	EncodingUTF8        = "utf-8"
	EncodingWindows1252 = "windows-1252"

	// MsgSameAsRow starts CheckRows' message for a repeated identity key.
	MsgSameAsRow = "same name, firm and clock number as row "
	// MsgExtraCells: a row with a non-blank cell beyond the header's columns.
	MsgExtraCells = "this row has more cells than the header has columns"
)

var (
	ErrNotCSV         = errors.New("send the file as text/csv")
	ErrEmptyFile      = errors.New("the file is empty")
	ErrNotText        = errors.New("the file has characters that aren't text; save it from Excel as CSV UTF-8")
	ErrNoNameColumn   = errors.New("the header needs a name column")
	ErrUnknownColumn  = errors.New("the header has a column that isn't name, firm, clock_number or trade")
	ErrRepeatedColumn = errors.New("the header names a column twice")
	ErrBadCSV         = errors.New("the file isn't valid CSV: check the quotes in this row")
	ErrNoWorkers      = errors.New("the file has no workers under its header")
	ErrTooManyRows    = errors.New("at most 5000 workers per import; split the file")
)

// FileError is a problem with the whole file, naming the column or the row
// where it was found.
type FileError struct {
	Err    error
	Column *string
	Row    *int
}

func (e *FileError) Error() string { return e.Err.Error() }
func (e *FileError) Unwrap() error { return e.Err }

// ImportContentTypes are the Content-Types an import takes: text/csv, and
// what Windows with Excel reports for a .csv file (review R6). A real .xls
// sent as the latter is refused as not text.
var ImportContentTypes = []string{"text/csv", "application/vnd.ms-excel"}

// ImportColumns are the columns a file may have, by their canonical name.
var ImportColumns = []string{"name", "firm", "clock_number", "trade"}

// columnAliases maps a normalised header cell to its column.
var columnAliases = map[string]string{
	"name": "name", "full_name": "name", "names": "name", "worker": "name", "worker_name": "name",
	"firm": "firm", "company": "firm", "employer": "firm", "contractor": "firm", "subcontractor": "firm",
	"clock_number": "clock_number", "clock_no": "clock_number", "clock_num": "clock_number", "clock": "clock_number",
	"employee_number": "clock_number", "employee_no": "clock_number", "staff_number": "clock_number", "staff_no": "clock_number",
	"trade": "trade", "occupation": "trade", "job_title": "trade",
}

// normaliseHeader: "Clock No." and "clock-number " both become clock_no,
// clock_number.
func normaliseHeader(s string) string {
	var b strings.Builder
	under := false
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		switch {
		case r == ' ' || r == '-' || r == '_' || r == '.' || r == '/':
			under = b.Len() > 0
		default:
			if under {
				b.WriteByte('_')
				under = false
			}
			b.WriteRune(r)
		}
	}
	return b.String()
}

// ParsedCSV is a file read into workers, before the register is consulted.
type ParsedCSV struct {
	Encoding  string
	Separator string
	Columns   []string // canonical, in the file's order; "" for an ignored blank column
	Rows      []WorkerRequest
	Numbers   []int // Rows[i]'s row number: the line its record starts on
	Problems  []RowProblem
}

// windows1252Undefined are the bytes Windows-1252 leaves without a
// character (x/text decodes them to U+FFFD).
var windows1252Undefined = []byte{0x81, 0x8D, 0x8F, 0x90, 0x9D}

// decodeText returns body as UTF-8 text and the encoding it was read in.
func decodeText(body []byte) (string, string, error) {
	body = bytes.TrimPrefix(body, []byte{0xEF, 0xBB, 0xBF})
	enc := EncodingUTF8
	if !utf8.Valid(body) {
		for _, b := range body {
			if bytes.IndexByte(windows1252Undefined, b) >= 0 {
				return "", "", ErrNotText
			}
		}
		decoded, err := charmap.Windows1252.NewDecoder().Bytes(body)
		if err != nil {
			return "", "", ErrNotText
		}
		body, enc = decoded, EncodingWindows1252
	}
	text := string(body)
	for _, r := range text {
		if (r < 0x20 && r != '\t' && r != '\r' && r != '\n') || (r >= 0x7f && r <= 0x9f) ||
			(r == utf8.RuneError && enc == EncodingWindows1252) {
			return "", "", ErrNotText
		}
	}
	return text, enc, nil
}

// separatorOf picks the header line's separator: a semicolon if it has more
// of them than commas (outside quotes), else a comma.
func separatorOf(text string) rune {
	commas, semis, quoted := 0, 0, false
	for _, r := range text {
		switch {
		case r == '"':
			quoted = !quoted
		case quoted:
		case r == '\n':
			if semis > commas {
				return ';'
			}
			return ','
		case r == ',':
			commas++
		case r == ';':
			semis++
		}
	}
	if semis > commas {
		return ';'
	}
	return ','
}

// ParseCSV reads a worker register file (D9). File-level problems are a
// *FileError or a sentinel; a row's are in Problems, alongside what
// ValidateWorker and CheckRows will add.
func ParseCSV(body []byte) (*ParsedCSV, error) {
	if len(bytes.TrimSpace(bytes.TrimPrefix(body, []byte{0xEF, 0xBB, 0xBF}))) == 0 {
		return nil, ErrEmptyFile
	}
	text, enc, err := decodeText(body)
	if err != nil {
		return nil, err
	}
	sep := separatorOf(text)
	r := csv.NewReader(strings.NewReader(text))
	r.Comma = sep
	r.FieldsPerRecord = -1

	header, err := r.Read()
	if err != nil {
		return nil, csvError(err)
	}
	p := &ParsedCSV{Encoding: enc, Separator: string(sep), Rows: []WorkerRequest{}, Numbers: []int{}, Problems: []RowProblem{}}
	seen := map[string]bool{}
	for _, cell := range header {
		h := normaliseHeader(cell)
		if h == "" {
			p.Columns = append(p.Columns, "")
			continue
		}
		col, ok := columnAliases[h]
		if !ok {
			name := strings.TrimSpace(cell)
			return nil, &FileError{Err: ErrUnknownColumn, Column: &name}
		}
		if seen[col] {
			return nil, &FileError{Err: ErrRepeatedColumn, Column: &col}
		}
		seen[col] = true
		p.Columns = append(p.Columns, col)
	}
	if !seen["name"] {
		return nil, ErrNoNameColumn
	}

	for {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, csvError(err)
		}
		line, _ := r.FieldPos(0)
		blank := true
		for _, cell := range rec {
			blank = blank && strings.TrimSpace(cell) == ""
		}
		if blank {
			continue // Excel's trailing ";;;" rows
		}
		if len(p.Rows) == MaxImportRows {
			return nil, ErrTooManyRows
		}
		var w WorkerRequest
		for i, cell := range rec {
			v := strings.TrimSpace(cell)
			if i >= len(p.Columns) {
				if v != "" {
					p.Problems = append(p.Problems, RowProblem{Row: line, FieldProblem: FieldProblem{Field: "", Error: MsgExtraCells}})
				}
				continue
			}
			var ptr *string
			if v != "" {
				ptr = &v
			}
			switch p.Columns[i] {
			case "name":
				w.Name = v
			case "firm":
				w.Firm = ptr
			case "clock_number":
				w.ClockNumber = ptr
			case "trade":
				w.Trade = ptr
			}
		}
		p.Rows = append(p.Rows, w)
		p.Numbers = append(p.Numbers, line)
	}
	if len(p.Rows) == 0 {
		return nil, ErrNoWorkers
	}
	return p, nil
}

func csvError(err error) error {
	var pe *csv.ParseError
	if errors.As(err, &pe) {
		row := pe.StartLine
		return &FileError{Err: ErrBadCSV, Row: &row}
	}
	return err
}
