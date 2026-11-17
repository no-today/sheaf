package session

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestRealNewcomerDiscovery(t *testing.T) {
	root := os.Getenv("SHEAF_BENCH_ROOT")
	if root == "" {
		t.Skip("SHEAF_BENCH_ROOT not set")
	}
	ctx := context.Background()
	s, err := Open(ctx, root, Options{DataDir: t.TempDir(), CacheDir: t.TempDir(), Now: time.Now})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.SetRequirement(`YT-?\d+`); err != nil {
		t.Fatal(err)
	}
	if err := s.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	candidates, err := s.RequirementCandidates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	cold := time.Since(start)
	start = time.Now()
	again, err := s.RequirementCandidates(ctx)
	if err != nil || len(again) != len(candidates) || len(candidates) == 0 {
		t.Fatalf("candidates=%d repeat=%d error=%v", len(candidates), len(again), err)
	}
	t.Logf("pattern=YT-?\\d+ candidates=%d cold=%v cached=%v", len(candidates), cold, time.Since(start))
	for _, candidate := range candidates[:min(8, len(candidates))] {
		if err := s.Track(candidate.ID); err != nil {
			t.Fatal(err)
		}
	}
	start = time.Now()
	requirements, err := s.Requirements(ctx)
	if err != nil || len(requirements) != min(8, len(candidates)) {
		t.Fatalf("tracked=%d error=%v", len(requirements), err)
	}
	t.Logf("tracked=%d cold=%v", len(requirements), time.Since(start))
}
