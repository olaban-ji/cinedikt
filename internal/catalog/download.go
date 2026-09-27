package catalog

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"cinedikt/internal/notify"
)

// DownloadTimeout bounds one file. title.principals is the better part
// of a gigabyte, so this is generous; it is here to end a stalled
// connection, not to hurry a slow one.
const DownloadTimeout = 30 * time.Minute

// Download streams the five files to dir and returns their paths.
//
// Each one is written to a temporary name and moved into place, so a
// partial file is never mistaken for a complete one. Nothing is held in
// memory: a gigabyte through a buffer would be a gigabyte of resident
// memory for no reason.
//
// sink, if set, hears how far each file has got: step 1 of the import's
// four, file i of n.
func Download(ctx context.Context, client *http.Client, dir string, files []File, logger *slog.Logger, sink notify.Sink) (map[File]string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("catalog: make %s: %w", dir, err)
	}
	paths := make(map[File]string, len(files))
	for i, f := range files {
		logger.Info("downloading", "file", f, "of", fmt.Sprintf("%d/%d", i+1, len(files)))
		base := notify.Event{Job: notify.JobImport, Step: 1, Steps: 4, Phase: notify.PhaseDownload,
			File: i + 1, Files: len(files), Noun: noun(f)}
		// Said once as the file starts, so the board moves on to it
		// straight away rather than at its first tick.
		report(sink, withKind(base, notify.Progress))
		path, err := download(ctx, client, dir, f, logger, sink, base)
		if err != nil {
			// Whatever landed is not a generation, so none of it is kept.
			Discard(paths)
			return nil, err
		}
		paths[f] = path
	}
	return paths, nil
}

// noun is what a file holds, in the word a person would use.
func noun(f File) string {
	switch f {
	case TitleBasics:
		return "films"
	case NameBasics:
		return "people"
	case TitlePrincipals:
		return "credits"
	case TitleCrew:
		return "directors"
	case TitleRatings:
		return "ratings"
	}
	return string(f)
}

func withKind(e notify.Event, k notify.Kind) notify.Event {
	e.Kind = k
	return e
}

func download(ctx context.Context, client *http.Client, dir string, f File, logger *slog.Logger, sink notify.Sink, base notify.Event) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, DownloadTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.URL(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", UserAgent)
	resp, err := client.Do(req)
	if err != nil {
		return "", &IMDbError{Method: "GET", File: f, Err: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", &IMDbError{Method: "GET", File: f, Status: resp.StatusCode}
	}

	final := filepath.Join(dir, string(f)+".tsv.gz")
	tmp, err := os.CreateTemp(dir, string(f)+".*.part")
	if err != nil {
		return "", fmt.Errorf("catalog: open temp for %s: %w", f, err)
	}
	// A gigabyte over a slow line is minutes of nothing to look at.
	track := newByteProgress(logger, "downloading "+string(f), resp.ContentLength)
	track.watch(sink, base)
	body := &countingReader{r: resp.Body, each: track.step}
	written, err := io.Copy(tmp, body)
	track.done(written)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp.Name())
		// A body that stopped arriving is the host's fault, and says so
		// by its type. A file that would not write is ours.
		var netErr net.Error
		if errors.As(err, &netErr) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, context.DeadlineExceeded) {
			return "", &IMDbError{Err: fmt.Errorf("write %s: %w", f, err)}
		}
		return "", fmt.Errorf("catalog: write %s: %w", f, err)
	}
	// A connection cut mid-file gives a short body with no error. The
	// length the server promised is the only way to notice here; a gzip
	// that ends early is caught again when it is read.
	if want := resp.ContentLength; want > 0 && written != want {
		os.Remove(tmp.Name())
		return "", &IMDbError{Err: fmt.Errorf("%s is %d bytes, expected %d", f, written, want)}
	}
	if err := os.Rename(tmp.Name(), final); err != nil {
		os.Remove(tmp.Name())
		return "", fmt.Errorf("catalog: move %s into place: %w", f, err)
	}
	return final, nil
}

// Discard removes downloaded files. Called when the set turns out to be
// a mixture, or when the import fails: a half-generation on disk is
// worse than no generation, because the next run might trust it.
func Discard(paths map[File]string) {
	for _, p := range paths {
		os.Remove(p)
	}
}

// OpenFile opens a downloaded file for reading.
func OpenFile(path string) (*os.File, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("catalog: open %s: %w", path, err)
	}
	return f, nil
}
