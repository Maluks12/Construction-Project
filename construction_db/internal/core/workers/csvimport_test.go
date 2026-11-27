package workers

import (
	"errors"
	"strings"
	"testing"
)

func names(p *ParsedCSV) string {
	var out []string
	for i, w := range p.Rows {
		s := itoa(p.Numbers[i]) + ":" + w.Name
		for _, v := range []*string{w.Firm, w.ClockNumber, w.Trade} {
			if v == nil {
				s += "|-"
			} else {
				s += "|" + *v
			}
		}
		out = append(out, s)
	}
	return strings.Join(out, ",")
}

// D9: what South African Excel writes.
func TestParseCSV(t *testing.T) {
	win := func(s string) []byte { // Windows-1252 for the few letters used here
		r := strings.NewReplacer("ë", "\xeb", "é", "\xe9", "ê", "\xea")
		return []byte(r.Replace(s))
	}
	for _, tc := range []struct {
		name, enc, sep, want string
		body                 []byte
	}{
		{"CSV UTF-8 with a BOM and CRLF", "utf-8", ",", "2:Sipho Dlamini|Bouwerk|C12|Rigger,3:Gërhard Esterhuyse|-|-|-",
			[]byte("\xef\xbb\xbfName,Firm,Clock No.,Trade\r\nSipho Dlamini,Bouwerk,C12,Rigger\r\nGërhard Esterhuyse,,,\r\n")},
		{"plain CSV in Windows-1252 with semicolons", "windows-1252", ";", "2:Gërhard Esterhuyse|Bouwerk Mé|-|-,3:Thabo Mokoena|-|-|Rêkenaar",
			win("Full name;Company;Employee number;Occupation\r\nGërhard Esterhuyse;Bouwerk Mé;;\r\nThabo Mokoena;;;Rêkenaar\r\n")},
		{"quoted cells, a comma inside, Excel's trailing blank rows", "utf-8", ",", "2:Dlamini, S.|A \"B\" Co|-|-,4:Last|-|-|-",
			[]byte("name,firm\n\"Dlamini, S.\",\"A \"\"B\"\" Co\"\n,\nLast,\n,\n,\n")},
		{"a blank header column and fewer cells", "utf-8", ";", "2:Only Name|-|-|-",
			[]byte("name;;trade\nOnly Name\n")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, err := ParseCSV(tc.body)
			if err != nil {
				t.Fatal(err)
			}
			if p.Encoding != tc.enc || p.Separator != tc.sep || names(p) != tc.want {
				t.Errorf("got %s %s %s\nwant %s %s %s", p.Encoding, p.Separator, names(p), tc.enc, tc.sep, tc.want)
			}
		})
	}

	for _, tc := range []struct {
		name string
		body string
		want error
	}{
		{"empty", "\xef\xbb\xbf \r\n", ErrEmptyFile},
		{"only a header", "name,firm\r\n", ErrNoWorkers},
		{"no name column", "firm,trade\nX,Y\n", ErrNoNameColumn},
		{"unknown column", "name,id_number\nX,8001015009087\n", ErrUnknownColumn},
		{"a column twice", "name,company,firm\nX,Y,Z\n", ErrRepeatedColumn},
		{"a byte Windows-1252 leaves undefined", "name\nBad\x81Byte\n", ErrNotText},
		{"a NUL", "name\nBad\x00Name\n", ErrNotText},
		{"a control character", "name\nBad\x07Name\n", ErrNotText},
		{"a bare quote", "name\nBad \"Name\n", ErrBadCSV},
		{"too many rows", "name\n" + strings.Repeat("x\n", 0) + rowsOf(MaxImportRows+1), ErrTooManyRows},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ParseCSV([]byte(tc.body)); !errors.Is(err, tc.want) {
				t.Errorf("got %v, want %v", err, tc.want)
			}
		})
	}
	if _, err := ParseCSV([]byte("name\n" + rowsOf(MaxImportRows))); err != nil {
		t.Errorf("%d rows: %v", MaxImportRows, err)
	}

	p, err := ParseCSV([]byte("name,firm\nA,B,extra\nC,D,\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Problems) != 1 || p.Problems[0].Row != 2 || p.Problems[0].Error != MsgExtraCells {
		t.Errorf("extra cells: %+v", p.Problems)
	}
	var fe *FileError
	if _, err := ParseCSV([]byte("name,Shoe Size\nX,9\n")); !errors.As(err, &fe) || fe.Column == nil || *fe.Column != "Shoe Size" {
		t.Errorf("unknown column names it: %v", err)
	}
	if _, err := ParseCSV([]byte("name\nok\nbad \"x\n")); !errors.As(err, &fe) || fe.Row == nil || *fe.Row != 3 {
		t.Errorf("bad CSV names its row: %v", err)
	}
}

func rowsOf(n int) string {
	var b strings.Builder
	for i := range n {
		b.WriteString("Worker " + itoa(i+1) + "\n")
	}
	return b.String()
}
