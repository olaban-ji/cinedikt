// Package catalog reads the IMDb non-commercial datasets into the shape
// the map is served from. The files are the whole source: nothing here
// calls out to an API, and a reader's request never reaches this code.
//
// https://developer.imdb.com/non-commercial-datasets/
package catalog

import (
	"bufio"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Null is how the datasets spell a missing value.
const Null = `\N`

// A row can be long: title.principals carries a JSON character list, and
// name.basics carries a filmography. This is well clear of both and
// still bounds a corrupt file.
const maxRowBytes = 1 << 20

// Reader walks one dataset file. The first line names the columns, and
// every row after it is read by column name rather than by position, so
// a column appearing or moving upstream does not silently shift the
// meaning of the one beside it.
type Reader struct {
	gz      *gzip.Reader
	scanner *bufio.Scanner
	index   map[string]int
	columns []string
	fields  []string
	line    int
}

// NewReader reads the gzip stream and its header.
func NewReader(r io.Reader) (*Reader, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("catalog: open gzip: %w", err)
	}
	sc := bufio.NewScanner(gz)
	sc.Buffer(make([]byte, 0, 64*1024), maxRowBytes)
	if !sc.Scan() {
		if err := sc.Err(); err != nil {
			return nil, fmt.Errorf("catalog: read header: %w", err)
		}
		return nil, errors.New("catalog: file is empty")
	}
	columns := strings.Split(sc.Text(), "\t")
	index := make(map[string]int, len(columns))
	for i, name := range columns {
		index[name] = i
	}
	return &Reader{gz: gz, scanner: sc, index: index, columns: columns, line: 1}, nil
}

// Require reports an error naming every column the caller needs that the
// file does not have. Checked once, before a single row is read, so a
// changed file fails the import rather than loading as nulls.
func (r *Reader) Require(names ...string) error {
	var missing []string
	for _, name := range names {
		if _, ok := r.index[name]; !ok {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("catalog: file has no column %s (it has %s)",
			strings.Join(missing, ", "), strings.Join(r.columns, ", "))
	}
	return nil
}

// Next advances to the next row. It reports false at the end of the
// file; Err says whether that end was the real one.
func (r *Reader) Next() bool {
	if !r.scanner.Scan() {
		return false
	}
	r.line++
	r.fields = strings.Split(r.scanner.Text(), "\t")
	return true
}

// Err is the read error, if the walk stopped for a reason other than the
// end of the file. A gzip cut short lands here, which is what makes a
// truncated download fail the load instead of publishing a short table.
func (r *Reader) Err() error {
	if err := r.scanner.Err(); err != nil {
		return fmt.Errorf("catalog: line %d: %w", r.line, err)
	}
	return nil
}

// Close releases the gzip reader.
func (r *Reader) Close() error { return r.gz.Close() }

// Text is the column's value, with `\N` read as empty.
func (r *Reader) Text(name string) string {
	i, ok := r.index[name]
	if !ok || i >= len(r.fields) {
		return ""
	}
	if v := r.fields[i]; v != Null {
		return v
	}
	return ""
}

// Int is the column as a number, and false when it is absent, `\N`, or
// not a number. IMDb writes stray values in year columns from time to
// time; those become null rather than failing the row.
func (r *Reader) Int(name string) (int, bool) {
	v := r.Text(name)
	if v == "" {
		return 0, false
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, false
	}
	return n, true
}

// Bool reads IMDb's 0/1 flags.
func (r *Reader) Bool(name string) bool { return r.Text(name) == "1" }

// List splits a comma-separated column, such as genres or directors.
// An absent value is an empty slice, never a slice holding one empty
// string, so a caller can take its length at face value.
func (r *Reader) List(name string) []string {
	v := r.Text(name)
	if v == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
