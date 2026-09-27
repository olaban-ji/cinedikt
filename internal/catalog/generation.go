package catalog

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"time"
)

// File is one of the five datasets the catalog is built from. Nothing
// else is downloaded: title.akas and title.episode are out of scope, and
// knownForTitles is a handful of highlight titles rather than a
// filmography, so name.basics is read for names alone.
type File string

const (
	TitleBasics     File = "title.basics"
	NameBasics      File = "name.basics"
	TitlePrincipals File = "title.principals"
	TitleCrew       File = "title.crew"
	TitleRatings    File = "title.ratings"
)

// Files is the set an import needs, in no particular order.
var Files = []File{TitleBasics, NameBasics, TitlePrincipals, TitleCrew, TitleRatings}

const datasetBase = "https://datasets.imdbws.com/"

// URL is where the file is published.
func (f File) URL() string { return datasetBase + string(f) + ".tsv.gz" }

// Stamp is what a HEAD says about one file. LastModified is the only
// field that decides anything; the other two are kept beside it for the
// log, because a body that changed under an unchanged Last-Modified is
// worth seeing even though it must not start a run.
type Stamp struct {
	LastModified time.Time
	ETag         string
	Length       int64
}

// Generation is the five stamps that were read together. A catalog is
// built from one generation and never from a mixture, so a new
// principals file is never applied on top of yesterday's titles.
type Generation map[File]Stamp

// Head reads the five stamps. Every file has to answer with a
// Last-Modified that parses: a missing or unreadable one is not a reason
// to guess, and the hour is skipped instead.
func Head(ctx context.Context, client *http.Client, files []File) (Generation, error) {
	gen := make(Generation, len(files))
	for _, f := range files {
		stamp, err := head(ctx, client, f)
		if err != nil {
			return nil, err
		}
		gen[f] = stamp
	}
	return gen, nil
}

func head(ctx context.Context, client *http.Client, f File) (Stamp, error) {
	return headAt(ctx, client, f.URL(), f)
}

// headAt is head against a given address, so a test can answer for the
// dataset host. `f` is only there to name the file in an error.
func headAt(ctx context.Context, client *http.Client, url string, f File) (Stamp, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		return Stamp{}, err
	}
	// A server is owed a way to tell who is asking; IMDb publishes these
	// files for exactly this and a nameless client is a rude one.
	req.Header.Set("User-Agent", UserAgent)
	resp, err := client.Do(req)
	if err != nil {
		return Stamp{}, &IMDbError{Method: "HEAD", File: f, Err: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Stamp{}, &IMDbError{Method: "HEAD", File: f, Status: resp.StatusCode}
	}
	raw := resp.Header.Get("Last-Modified")
	if raw == "" {
		return Stamp{}, fmt.Errorf("catalog: %s has no Last-Modified", f)
	}
	// HTTP dates are GMT by definition, and http.ParseTime reads the
	// three formats RFC 9110 allows.
	when, err := http.ParseTime(raw)
	if err != nil {
		return Stamp{}, fmt.Errorf("catalog: %s Last-Modified %q: %w", f, raw, err)
	}
	length, _ := strconv.ParseInt(resp.Header.Get("Content-Length"), 10, 64)
	return Stamp{LastModified: when.UTC(), ETag: resp.Header.Get("ETag"), Length: length}, nil
}

// UserAgent names this client to the dataset host.
const UserAgent = "cinedikt-catalog/1 (+https://cinedikt.com)"

// Ready reports whether `next` is a whole new generation compared with
// the one last published, and says why when it is not.
//
// Every file has to have moved. One file arriving ahead of the others is
// half a generation, and half a generation loaded on top of the rest is
// a catalog whose credits and titles disagree. The hourly check simply
// waits: the rest of the set lands within a minute or two of the first.
//
// An empty `published` is the first run, which needs only five stamps
// that parsed.
func Ready(published, next Generation, files []File) (bool, string) {
	if len(next) != len(files) {
		return false, fmt.Sprintf("read %d of %d files", len(next), len(files))
	}
	if len(published) == 0 {
		return true, "first run: the catalog is empty"
	}
	var waiting, moved []string
	for _, f := range files {
		was, known := published[f]
		if !known {
			// A file the last publish never recorded. Treat it as new
			// rather than as unchanged, so adding a file to the set does
			// not need a hand-edited meta row.
			moved = append(moved, string(f))
			continue
		}
		if next[f].LastModified.After(was.LastModified) {
			moved = append(moved, string(f))
			continue
		}
		waiting = append(waiting, string(f))
	}
	if len(waiting) > 0 {
		sort.Strings(waiting)
		sort.Strings(moved)
		return false, fmt.Sprintf("%d of %d files have moved (%v); still on the published stamp: %v",
			len(moved), len(files), moved, waiting)
	}
	return true, fmt.Sprintf("all %d files have moved", len(files))
}

// Same reports whether two readings of the set describe the same files,
// by Last-Modified. The downloader reads the set again once every file
// is on disk: if anything moved while it was fetching, the five it holds
// are a mixture and the attempt is thrown away.
func Same(a, b Generation, files []File) (bool, File) {
	for _, f := range files {
		if !a[f].LastModified.Equal(b[f].LastModified) {
			return false, f
		}
	}
	return true, ""
}

// PollInterval is how often the set is checked. The files are rebuilt
// once a day, so this is not about catching the moment they land — it is
// about not waiting most of a day after they have.
const PollInterval = time.Hour
