package session

import (
	"os"
	"path/filepath"
	"testing"

	"sheaf/internal/testutil"
)

func reportBenchmark(b *testing.B) (*Session, string) {
	b.Helper()
	root := os.Getenv("SHEAF_BENCH_ROOT")
	if root == "" {
		b.Skip("SHEAF_BENCH_ROOT not set")
	}
	testutil.Isolate(b)
	home := b.TempDir()
	b.Setenv("SHEAF_HOME", home)
	s, err := Open(b.Context(), root, Options{DataDir: home, CacheDir: filepath.Join(home, "cache"), NoAutoRefresh: true})
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(s.Close)
	if problems := s.Settle(b.Context(), nil); len(problems) != 0 {
		b.Fatalf("initial settle: %+v", problems)
	}
	s.mu.Lock()
	ix := s.eng.Index()
	s.mu.Unlock()
	re, _ := compileRequirement("")
	id := os.Getenv("SHEAF_BENCH_TAG")
	tracked := map[string]bool{}
	for _, c := range ix.Order {
		for _, found := range re.FindAllString(ix.Repos[c.Repo].Subject[c.I], -1) {
			if len(tracked) < 10 && !tracked[found] {
				if err := s.Track(found); err != nil {
					b.Fatal(err)
				}
				tracked[found] = true
			}
			if id == "" {
				id = found
			}
		}
		if len(tracked) >= 10 && id != "" {
			break
		}
	}
	if id == "" {
		b.Skip("no requirement matches the default pattern; set SHEAF_BENCH_TAG")
	}
	if err := s.Track(id); err != nil {
		b.Fatal(err)
	}
	if problems := s.Settle(b.Context(), nil); len(problems) != 0 {
		b.Fatalf("warm settle: %+v", problems)
	}
	if _, err := s.Report(b.Context(), id); err != nil {
		b.Fatal(err)
	}
	if _, err := s.ReqCommits(b.Context(), id, "", "", 50); err != nil {
		b.Fatal(err)
	}
	b.Logf("root=%s, tracked=%d, requirement=%s, repos=%d, commits=%d", root, len(s.Tracked()), id, len(ix.Repos), ix.Commits())
	return s, id
}

func BenchmarkReportSettleOverview(b *testing.B) {
	s, _ := reportBenchmark(b)
	b.ResetTimer()
	for range b.N {
		if problems := s.Settle(b.Context(), nil); len(problems) != 0 {
			b.Fatalf("settle: %+v", problems)
		}
		if _, err := s.Overview(b.Context(), ScopeActive, ""); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkReportDetail(b *testing.B) {
	s, id := reportBenchmark(b)
	b.ResetTimer()
	for range b.N {
		if _, err := s.Report(b.Context(), id); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkReportReqCommits(b *testing.B) {
	s, id := reportBenchmark(b)
	b.ResetTimer()
	for range b.N {
		if _, err := s.ReqCommits(b.Context(), id, "", "", 50); err != nil {
			b.Fatal(err)
		}
	}
}
