package catalog

import (
	"context"
	"testing"
)

func TestTheJobsPoolRunsWithoutParallelWorkers(t *testing.T) {
	const url = "postgres://u:p@localhost:5432/cinedikt"

	jobs, err := poolConfig(url, 4, true)
	if err != nil {
		t.Fatal(err)
	}
	if got := jobs.ConnConfig.RuntimeParams["max_parallel_workers_per_gather"]; got != "0" {
		t.Errorf("jobs pool max_parallel_workers_per_gather = %q, want 0", got)
	}
	if jobs.MaxConns != 4 {
		t.Errorf("jobs pool MaxConns = %d, want 4", jobs.MaxConns)
	}

	// The pool readers are served from keeps Postgres's own choice:
	// only the background passes give up parallel workers.
	api, err := poolConfig(url, 10, false)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := api.ConnConfig.RuntimeParams["max_parallel_workers_per_gather"]; ok {
		t.Errorf("API pool sets max_parallel_workers_per_gather = %q, want it left alone", got)
	}

	// Both still find the trigram operator class, whichever schema
	// pg_trgm was installed in.
	for name, cfg := range map[string]map[string]string{"jobs": jobs.ConnConfig.RuntimeParams, "api": api.ConnConfig.RuntimeParams} {
		if got := cfg["search_path"]; got != "meta, public" {
			t.Errorf("%s pool search_path = %q, want \"meta, public\"", name, got)
		}
	}
}

func TestTheJobsPoolReallyTurnsParallelWorkersOff(t *testing.T) {
	url := leaseURL(t)
	s, err := OpenForJobs(context.Background(), url, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var got string
	if err := s.pool.QueryRow(context.Background(), "SHOW max_parallel_workers_per_gather").Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != "0" {
		t.Errorf("on the jobs pool, max_parallel_workers_per_gather = %q, want 0", got)
	}
}
