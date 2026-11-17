package session

import (
	"bytes"
	"context"
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"sheaf/internal/discover"
	"sheaf/internal/testutil"
)

func TestCompletionData(t *testing.T) {
	t.Setenv("SHEAF_HOME", t.TempDir())
	data := os.Getenv("SHEAF_HOME")
	cache, root := filepath.Join(data, "cache"), t.TempDir()
	configFile := filepath.Join(data, "workspaces", WorkspaceID(root), "workspace.json")
	scanFile := filepath.Join(cache, "workspaces", WorkspaceID(root), "scan.json")
	writeJSON := func(path string, value any) {
		t.Helper()
		b, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeJSON(configFile, Config{Root: root, Tracked: []string{"PAY-9", "PAY-142", "PAY-200"},
		Archived: []string{"PAY-9", "PAY-999"}, RequirementNames: map[string]string{"PAY-142": "Capture payment"},
		MergeFlow: []MergeStep{{Name: "release", Branch: "release"}, {Name: "production", Branch: "master"}}, Ignored: []string{"ignored"}})
	writeJSON(scanFile, []discover.Repo{
		{Name: "web", Dir: filepath.Join(root, "web")},
		{Name: "api", Dir: filepath.Join(root, "api")},
		{Name: "api", Dir: filepath.Join(root, "api")},
		{Name: "ignored", Dir: filepath.Join(root, "ignored")},
		{Name: "nested", Dir: filepath.Join(root, "ignored", "nested")},
	})
	// A corrupt unrelated file must not affect this reader.
	if err := os.WriteFile(filepath.Join(data, "recent.json"), []byte("broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot := func() map[string]string {
		files := map[string]string{}
		if err := filepath.WalkDir(data, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				files[path] = "directory"
			} else {
				b, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				files[path] = string(b)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		return files
	}
	before := snapshot()
	// Removing all executables makes accidental Git reads fail the metadata assertion.
	t.Setenv("PATH", t.TempDir())
	got, err := CompletionData(data, cache, root)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got.Requirements, []CompletionRequirement{{ID: "PAY-142", Title: "Capture payment"}, {ID: "PAY-200"}, {ID: "PAY-9", Archived: true}}) ||
		!slices.Equal(got.Stages, []string{"release", "production"}) || !slices.Equal(got.Repositories, []string{"api", "web"}) {
		t.Fatalf("unexpected completion data: %+v", got)
	}
	if !maps.Equal(before, snapshot()) {
		t.Fatal("completion data reader wrote app files")
	}
	for _, file := range []string{configFile, scanFile} {
		t.Run(filepath.Base(file), func(t *testing.T) {
			b, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(file, []byte("broken"), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := CompletionData(data, cache, root); err == nil {
				t.Fatal("malformed JSON was accepted")
			}
			if err := os.Remove(file); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(file, 0o755); err != nil {
				t.Fatal(err)
			}
			if _, err := CompletionData(data, cache, root); err == nil {
				t.Fatal("unreadable file was accepted")
			}
			if err := os.Remove(file); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(file, b, 0o600); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCompletionDataMissing(t *testing.T) {
	t.Setenv("SHEAF_HOME", t.TempDir())
	data := filepath.Join(os.Getenv("SHEAF_HOME"), "absent")
	got, err := CompletionData(data, filepath.Join(data, "cache"), t.TempDir())
	if err != nil || len(got.Requirements)+len(got.Repositories) != 0 || !slices.Equal(got.Stages, []string{"main"}) {
		t.Fatalf("missing files: %+v, %v", got, err)
	}
	if _, err := os.Stat(data); !os.IsNotExist(err) {
		t.Fatalf("reader created data directory: %v", err)
	}
}

func reportFixture(t *testing.T) (string, *testutil.Repo) {
	t.Helper()
	testutil.Isolate(t)
	t.Setenv("SHEAF_HOME", t.TempDir())
	root := t.TempDir()
	r := testutil.NewRepo(t, filepath.Join(root, "repo"))
	r.Commit("init", map[string]string{"base": "base\n"})
	r.Git("branch", "release")
	return root, r
}

func openReport(t *testing.T, root string) *Session {
	t.Helper()
	home := os.Getenv("SHEAF_HOME")
	s, err := Open(t.Context(), root, Options{DataDir: home, CacheDir: filepath.Join(home, "cache"), NoAutoRefresh: true,
		Now: func() time.Time { return time.Unix(2_000_000_000, 0) }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if err := s.SetMergeFlow([]MergeStep{{Name: "release", Branch: "release"}, {Name: "production", Branch: "master"}}); err != nil {
		t.Fatal(err)
	}
	return s
}

func mustReport(t *testing.T, s *Session, id string) ReqReport {
	t.Helper()
	r, err := s.Report(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func mustPage(t *testing.T, s *Session, id string) CommitPage {
	t.Helper()
	page, err := s.ReqCommits(t.Context(), id, "", "", 50)
	if err != nil {
		t.Fatal(err)
	}
	return page
}

func requireProblem(t *testing.T, problems []Problem, code, repo string) {
	t.Helper()
	if !slices.ContainsFunc(problems, func(p Problem) bool { return p.Code == code && (repo == "" || p.Repo == repo) }) {
		t.Fatalf("missing %s in %s: %+v", code, repo, problems)
	}
}

func TestSettleRequirementWorktrees(t *testing.T) {
	for _, tc := range []struct {
		name string
		ids  []string
		want []string
	}{
		{"all tracked", nil, []string{"PAY-142", "PAY-200"}},
		{"selected", []string{"PAY-142"}, []string{"PAY-142"}},
		{"empty", []string{}, []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, r := reportFixture(t)
			r.Git("checkout", "-qb", "feature/pay-142")
			r.Commit("PAY-142 selected", map[string]string{"selected": "yes\n"})
			other := testutil.NewRepo(t, filepath.Join(root, "other"))
			other.Commit("init", map[string]string{"base": "yes\n"})
			other.Git("branch", "release")
			other.Git("checkout", "-qb", "PAY-200")
			other.Commit("PAY-200 other", map[string]string{"other": "yes\n"})
			unrelated := filepath.Join(t.TempDir(), "unrelated")
			r.Git("worktree", "add", "-b", "maintenance", unrelated, "master")
			for _, dir := range []string{r.Dir, other.Dir, unrelated} {
				if err := os.WriteFile(filepath.Join(dir, "pending"), []byte("work\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(other.Dir, "pending-two"), []byte("work\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			s := openReport(t, root)
			for _, id := range []string{"PAY-142", "PAY-200"} {
				if err := s.Track(id); err != nil {
					t.Fatal(err)
				}
			}
			if err := s.ArchiveRequirement("PAY-200", true); err != nil {
				t.Fatal(err)
			}
			if problems := s.Settle(t.Context(), tc.ids); len(problems) != 0 {
				t.Fatal(problems)
			}
			patches := 0
			dirty := map[string]int{}
			for _, event := range s.rec.Events() {
				switch event.Source {
				case "releaseCheck", "remerge":
					patches++
					if tc.ids != nil && event.Dir != r.Dir {
						t.Fatalf("classified unrelated requirement: %+v", event)
					}
				case "dirtyCount":
					dirty[event.Dir]++
				}
			}
			wantDirty := map[string]int{}
			for _, wt := range []struct {
				id, dir string
				count   int
			}{{"PAY-142", r.Dir, 1}, {"PAY-200", other.Dir, 2}} {
				if slices.Contains(tc.want, wt.id) {
					wantDirty[wt.dir] = 1
					if report := mustReport(t, s, wt.id); report.Uncommitted != wt.count {
						t.Fatalf("%s: uncommitted = %d, want %d", wt.id, report.Uncommitted, wt.count)
					}
				}
			}
			if !maps.Equal(dirty, wantDirty) {
				t.Fatalf("dirty commands = %v, want %v (unrelated worktree: %s)", dirty, wantDirty, unrelated)
			}
			if len(tc.want) > 0 && patches == 0 {
				t.Fatal("no patch classification commands")
			}
			if tc.ids != nil && s.reqs.ok {
				t.Fatal("selected settle computed all requirements")
			}
		})
	}
}

func TestReportWithFiles(t *testing.T) {
	for _, dirty := range []bool{false, true} {
		t.Run(map[bool]string{false: "clean", true: "dirty"}[dirty], func(t *testing.T) {
			root, r := reportFixture(t)
			r.Git("checkout", "-qb", "PAY-142")
			r.Commit("PAY-142 files", map[string]string{"a-staged": "initial\n", "b-unstaged": "initial\n"})
			want := []UncommittedFile{}
			if dirty {
				for _, path := range []string{"a-staged", "b-unstaged", "c-untracked"} {
					if err := os.WriteFile(filepath.Join(r.Dir, path), []byte("changed\n"), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				r.Git("add", "a-staged")
				want = []UncommittedFile{
					{Worktree: r.Dir, Path: "a-staged", Area: "staged", Status: "M"},
					{Worktree: r.Dir, Path: "b-unstaged", Area: "unstaged", Status: "M"},
					{Worktree: r.Dir, Path: "c-untracked", Area: "untracked", Status: "A"},
				}
			}
			s := openReport(t, root)
			if problems := s.Settle(t.Context(), []string{"PAY-142"}); len(problems) > 0 {
				t.Fatal(problems)
			}
			start := s.rec.LastID()
			plain := mustReport(t, s, "PAY-142")
			plainEvents := s.rec.LastID() - start
			if len(plain.Rows) != 1 || plain.Rows[0].UncommittedFiles != nil {
				t.Fatalf("default report: %+v", plain.Rows)
			}
			for range 2 {
				start = s.rec.LastID()
				report, err := s.ReportWithFiles(t.Context(), "PAY-142")
				if err != nil || len(report.Problems) > 0 || len(report.Rows) != 1 {
					t.Fatalf("report: %+v, %v", report, err)
				}
				row := report.Rows[0]
				if row.Uncommitted != len(want) || row.Worktree != r.Dir || row.UncommittedFiles == nil ||
					*row.UncommittedFiles == nil || !slices.Equal(*row.UncommittedFiles, want) {
					t.Fatalf("row: %+v, want files %+v", row, want)
				}
				if events := s.rec.LastID() - start; events != plainEvents {
					t.Fatalf("files ran %d Git commands; plain ran %d", events, plainEvents)
				}
				withJSON, err := json.Marshal(report)
				if err != nil || !dirty && !bytes.Contains(withJSON, []byte(`"uncommittedFiles":[]`)) {
					t.Fatalf("clean files must be []: %s, %v", withJSON, err)
				}
				report.Rows[0].UncommittedFiles = nil
				gotJSON, _ := json.Marshal(report)
				plainJSON, _ := json.Marshal(plain)
				if !bytes.Equal(gotJSON, plainJSON) {
					t.Fatalf("files changed other fields:\n%s\n%s", gotJSON, plainJSON)
				}
			}
			start = s.rec.LastID()
			s.dirty.invalidate([]string{r.Dir})
			missing, err := s.ReportWithFiles(t.Context(), "PAY-142")
			if err != nil {
				t.Fatal(err)
			}
			requireProblem(t, missing.Problems, ProblemGitRead, "repo")
			if s.rec.LastID()-start != plainEvents {
				t.Fatal("missing cached status ran extra Git commands")
			}
		})
	}
}

func TestReportWithFilesMultipleWorktrees(t *testing.T) {
	for _, both := range []bool{false, true} {
		t.Run(map[bool]string{false: "one entry each", true: "staged and unstaged"}[both], func(t *testing.T) {
			root, r := reportFixture(t)
			r.Git("checkout", "-qb", "PAY-142")
			r.Commit("PAY-142 work", map[string]string{"work": "original\n"})
			wt := filepath.Join(t.TempDir(), "outside")
			r.Git("worktree", "add", "-qb", "PAY-142-second", wt)
			canonicalWT, err := filepath.EvalSymlinks(wt)
			if err != nil {
				t.Fatal(err)
			}
			for _, dir := range []string{r.Dir, wt} {
				if err := os.WriteFile(filepath.Join(dir, "work"), []byte("changed\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			want := []UncommittedFile{
				{Worktree: canonicalWT, Path: "work", Area: "unstaged", Status: "M"},
				{Worktree: r.Dir, Path: "work", Area: "unstaged", Status: "M"},
			}
			if both {
				r.Git("add", "work")
				if err := os.WriteFile(filepath.Join(r.Dir, "work"), []byte("changed again\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				want = append(want, UncommittedFile{Worktree: r.Dir, Path: "work", Area: "staged", Status: "M"})
			}
			slices.SortFunc(want, func(a, b UncommittedFile) int {
				return strings.Compare(a.Worktree+"\x00"+a.Path+"\x00"+a.Area, b.Worktree+"\x00"+b.Path+"\x00"+b.Area)
			})
			s := openReport(t, root)
			if problems := s.Settle(t.Context(), []string{"PAY-142"}); len(problems) > 0 {
				t.Fatal(problems)
			}
			plain := mustReport(t, s, "PAY-142")
			start := s.rec.LastID()
			rpt, err := s.ReportWithFiles(t.Context(), "PAY-142")
			if err != nil || len(rpt.Rows) != 1 || rpt.Uncommitted != 2 || len(rpt.Problems) != 0 {
				t.Fatalf("report: %+v, %v", rpt, err)
			}
			if s.rec.LastID() != start {
				t.Fatal("file lists ran extra Git commands")
			}
			row := rpt.Rows[0]
			if row.Uncommitted != 2 || row.UncommittedFiles == nil || !slices.Equal(*row.UncommittedFiles, want) {
				t.Fatalf("files: %+v; want %+v", row, want)
			}
			if !both && len(*row.UncommittedFiles) != row.Uncommitted {
				t.Fatal("one entry per file must match the count")
			}
			paths := map[string]bool{}
			for _, file := range *row.UncommittedFiles {
				paths[file.Worktree+"\x00"+file.Path] = true
			}
			if len(paths) != row.Uncommitted {
				t.Fatalf("unique worktree/path count = %d, uncommitted = %d", len(paths), row.Uncommitted)
			}
			rpt.Rows[0].UncommittedFiles = nil
			if !reflect.DeepEqual(rpt.Rows, plain.Rows) {
				t.Fatalf("--files changed existing row fields:\n%+v\n%+v", rpt.Rows, plain.Rows)
			}
		})
	}
}

func TestReportWithFilesNoNamedWorktree(t *testing.T) {
	root, r := reportFixture(t)
	r.Commit("PAY-142 landed", map[string]string{"work": "original\n"})
	s := openReport(t, root)
	if problems := s.Settle(t.Context(), []string{"PAY-142"}); len(problems) > 0 {
		t.Fatal(problems)
	}
	plain := mustReport(t, s, "PAY-142")
	rpt, err := s.ReportWithFiles(t.Context(), "PAY-142")
	if err != nil || len(rpt.Rows) != 1 || len(rpt.Problems) != 0 {
		t.Fatalf("report: %+v, %v", rpt, err)
	}
	list := rpt.Rows[0].UncommittedFiles
	if list == nil || *list == nil || len(*list) != 0 {
		t.Fatalf("no named worktree must have [] files: %+v", list)
	}
	rpt.Rows[0].UncommittedFiles = nil
	if !reflect.DeepEqual(rpt.Rows, plain.Rows) {
		t.Fatalf("--files changed existing row fields:\n%+v\n%+v", rpt.Rows, plain.Rows)
	}
}

func TestSettleIndex(t *testing.T) {
	root, r := reportFixture(t)
	r.Git("checkout", "-qb", "PAY-142")
	r.Commit("PAY-142 work", map[string]string{"work": "yes\n"})
	s := openReport(t, root)
	s.Track("PAY-142")
	if problems := s.SettleIndex(t.Context()); len(problems) != 0 {
		t.Fatal(problems)
	}
	if !s.Info().State.Ready {
		t.Fatal("index not ready")
	}
	for _, event := range s.rec.Events() {
		if event.Source == "dirtyCount" || event.Source == "releaseCheck" || event.Source == "remerge" {
			t.Fatalf("index settle did extra work: %+v", event)
		}
	}
}

func TestReportA05ArchivedReadDoesNotChangeLists(t *testing.T) {
	root, r := reportFixture(t)
	r.Git("checkout", "-qb", "PAY-142")
	sha := r.Commit("PAY-142 refund", map[string]string{"refund": "yes\n"})
	s := openReport(t, root)
	if err := s.Track("PAY-142"); err != nil {
		t.Fatal(err)
	}
	if err := s.ArchiveRequirement("PAY-142", true); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(s.cfgPath)
	if problems := s.Settle(t.Context(), nil); len(problems) != 0 {
		t.Fatal(problems)
	}
	report := mustReport(t, s, " pay-142 ")
	if !report.Found || !report.Tracked || !report.Archived || report.Progress != StagePending || report.Title != "refund" {
		t.Fatalf("%+v", report)
	}
	page := mustPage(t, s, "PAY-142")
	if len(page.Items) != 1 || page.Items[0].SHA != sha {
		t.Fatal(page)
	}
	after, _ := os.ReadFile(s.cfgPath)
	if !bytes.Equal(before, after) {
		t.Fatalf("query changed config:\n%s", after)
	}
	active, err := s.Overview(t.Context(), ScopeActive, "")
	if err != nil || len(active.Items) != 0 {
		t.Fatalf("%+v %v", active, err)
	}
	archived, err := s.Overview(t.Context(), ScopeArchived, "REFUND")
	if err != nil || len(archived.Items) != 1 {
		t.Fatalf("%+v %v", archived, err)
	}
}

func TestReportA06UntrackedOldOtherAuthor(t *testing.T) {
	root, r := reportFixture(t)
	sha := r.Commit("OLD-1001 historical", map[string]string{"old": "old\n"}, testutil.Author{Name: "Other", Email: "other@example.com"})
	s := openReport(t, root)
	if problems := s.Settle(t.Context(), nil); len(problems) != 0 {
		t.Fatal(problems)
	}
	candidates, err := s.RequirementCandidates(t.Context())
	if err != nil || slices.ContainsFunc(candidates, func(c Candidate) bool { return c.ID == "OLD-1001" }) {
		t.Fatalf("%+v %v", candidates, err)
	}
	before := s.ConfigJSON()
	report := mustReport(t, s, "OLD-1001")
	page := mustPage(t, s, "OLD-1001")
	if report.Tracked || !report.Found || len(page.Items) != 1 || page.Items[0].SHA != sha || page.Items[0].Email != "other@example.com" {
		t.Fatalf("%+v %+v", report, page)
	}
	if !bytes.Equal(before, s.ConfigJSON()) || len(s.Tracked()) != 0 {
		t.Fatal("temporary query changed config")
	}
}

func TestReportA07LandedHistoryIncludesBaseline(t *testing.T) {
	root, r := reportFixture(t)
	sha := r.Commit("PAY-142 already in main", map[string]string{"done": "done\n"})
	s := openReport(t, root)
	s.Settle(t.Context(), nil)
	report := mustReport(t, s, "PAY-142")
	page := mustPage(t, s, "PAY-142")
	if report.Progress != StageLanded || len(page.Items) != 1 || page.Items[0].SHA != sha || len(report.Gaps) != 0 {
		t.Fatalf("%+v %+v", report, page)
	}
	if check := report.Rows[0].Stages[1]; len(check.Covered) != 1 || check.Covered[0].By != CoverAncestor {
		t.Fatal(check)
	}
}

func TestReportA08DeletedMergedBranchKeepsHistory(t *testing.T) {
	root, r := reportFixture(t)
	r.Git("checkout", "-qb", "PAY-142")
	sha := r.Commit("PAY-142 work", map[string]string{"work": "yes\n"})
	r.Git("checkout", "-q", "master")
	r.Merge("PAY-142", "integrate feature")
	r.Git("branch", "-D", "PAY-142")
	s := openReport(t, root)
	s.Settle(t.Context(), nil)
	report := mustReport(t, s, "PAY-142")
	page := mustPage(t, s, "PAY-142")
	if !report.Rows[0].Deleted || report.Progress != StageLanded || len(page.Items) != 1 || page.Items[0].SHA != sha {
		t.Fatalf("%+v %+v", report, page)
	}
	if diff := r.Git("show", "--format=", page.Items[0].SHA); !strings.Contains(diff, "+yes") {
		t.Fatal(diff)
	}
	if page.Items[0].Path != r.Dir || len(page.Items[0].Parents) != 1 {
		t.Fatal(page)
	}
}

func TestReportA09DeletedUnmergedAndNoOwnEvidence(t *testing.T) {
	for _, noOwn := range []bool{false, true} {
		t.Run(map[bool]string{false: "unmerged", true: "no own commits"}[noOwn], func(t *testing.T) {
			root, r := reportFixture(t)
			r.Git("checkout", "-qb", "PAY-142")
			if noOwn {
				r.Commit("unrelated", map[string]string{"own": "own\n"})
				r.Git("checkout", "-qb", "integration", "master")
				r.Merge("PAY-142", "Merge PAY-142")
			} else {
				r.Commit("PAY-142 own", map[string]string{"own": "own\n"})
			}
			r.Git("tag", "saved")
			r.Git("checkout", "-q", "master")
			r.Git("branch", "-D", "PAY-142")
			if noOwn {
				r.Git("branch", "-D", "integration")
			}
			s := openReport(t, root)
			if err := s.Track("PAY-142"); err != nil {
				t.Fatal(err)
			}
			s.Settle(t.Context(), nil)
			report := mustReport(t, s, "PAY-142")
			want := StagePending
			if noOwn {
				want = StageUnknown
				requireProblem(t, report.Problems, ProblemNoEvidence, "repo")
			}
			if report.Progress != want || !report.Rows[0].Deleted || len(report.Gaps) != 1 {
				t.Fatalf("%+v", report)
			}
			list, err := s.Requirements(t.Context())
			if err != nil || list[0].Progress != report.Progress {
				t.Fatalf("%+v %v", list, err)
			}
		})
	}
}

func TestReportA10PrefixNumbers(t *testing.T) {
	root, r := reportFixture(t)
	sha := r.Commit("PAY-14 short", map[string]string{"short": "yes\n"})
	r.Commit("PAY-142 long", map[string]string{"long": "yes\n"})
	s := openReport(t, root)
	s.Track("PAY-14")
	s.Track("PAY-142")
	s.Settle(t.Context(), nil)
	report := mustReport(t, s, "PAY-14")
	page := mustPage(t, s, "PAY-14")
	if report.Title != "short" || len(page.Items) != 1 || page.Items[0].SHA != sha {
		t.Fatalf("%+v %+v", report, page)
	}
	overview, err := s.Overview(t.Context(), ScopeAll, "pay-14")
	if err != nil || len(overview.Items) != 1 || overview.Items[0].ID != "PAY-14" {
		t.Fatalf("%+v %v", overview, err)
	}
}

func TestReportA11CustomFlowAndRepoExceptions(t *testing.T) {
	root, r := reportFixture(t)
	r.Git("checkout", "-qb", "PAY-142")
	r.Commit("PAY-142 work", map[string]string{"work": "yes\n"})
	r.Git("checkout", "-q", "master")
	r.Git("branch", "custom-base")
	other := testutil.NewRepo(t, filepath.Join(root, "other"))
	other.Commit("PAY-142 another", map[string]string{"other": "yes\n"})
	other.Git("branch", "release")
	s := openReport(t, root)
	flow := []MergeStep{{Name: "review", Branch: "missing-middle"}, {Name: "qa", Branch: "release"}, {Name: "ship"}}
	if err := s.SetMergeFlow(flow); err != nil {
		t.Fatal(err)
	}
	if err := s.SetBase("repo", "custom-base"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetRepoBaselineOnly("other", true); err != nil {
		t.Fatal(err)
	}
	s.Track("PAY-142")
	if problems := s.Settle(t.Context(), nil); len(problems) != 0 {
		t.Fatal(problems)
	}
	report := mustReport(t, s, "PAY-142")
	if !slices.Equal(report.Flow, flow) || report.Progress != MergeStage(1) || report.Repos != 2 {
		t.Fatalf("%+v", report)
	}
	for _, row := range report.Rows {
		if row.Repo == "other" {
			if row.Stages[0].Skip != SkipBaselineOnly || row.Stages[1].Skip != SkipBaselineOnly || row.Stages[0].Applies {
				t.Fatal(row)
			}
		} else if row.Stages[0].Skip != SkipNoTarget || row.Stages[0].Applies || row.Stages[2].Ref != "refs/heads/custom-base" {
			t.Fatal(row)
		}
	}
	list, err := s.Requirements(t.Context())
	if err != nil || list[0].Progress != report.Progress {
		t.Fatalf("%+v %v", list, err)
	}
}

func TestReportA12ExplicitStageDoesNotUseLaterCoverage(t *testing.T) {
	root, r := reportFixture(t)
	r.Git("checkout", "-qb", "PAY-142")
	sha := r.Commit("PAY-142 work", map[string]string{"work": "yes\n"})
	r.Git("checkout", "-q", "master")
	r.Merge("PAY-142", "integrate feature")
	s := openReport(t, root)
	s.Settle(t.Context(), nil)
	report := mustReport(t, s, "PAY-142")
	check, err := s.StageCheck(t.Context(), "PAY-142", "release")
	if err != nil || report.Progress != StageLanded || check.Progress != StageLanded || len(report.Gaps) != 0 || len(check.Gaps) != 1 || check.Gaps[0].Next != "release" || check.Gaps[0].Missing != 1 {
		t.Fatalf("default=%+v direct=%+v %v", report, check, err)
	}
	if stages := check.Rows[0].Stages; len(stages) != 1 || len(stages[0].Covered) != 0 || stages[0].Missing[0].SHA != sha {
		t.Fatal(stages)
	}
	if _, err := s.StageCheck(t.Context(), "PAY-142", "unknown"); err == nil {
		t.Fatal("unknown stage accepted")
	}
}

func TestReportA13ActualTargetRefAndSHA(t *testing.T) {
	for _, mode := range []string{"origin", "local", "other remote", "explicit ref", "tag"} {
		t.Run(mode, func(t *testing.T) {
			root, r := reportFixture(t)
			base := r.Git("rev-parse", "master")
			sha := r.Commit("PAY-142 work", map[string]string{"work": "yes\n"})
			wantRef, wantSHA, remote, branch := "refs/heads/master", sha, false, "master"
			switch mode {
			case "origin":
				wantRef, wantSHA, remote = "refs/remotes/origin/master", base, true
				r.Git("update-ref", wantRef, base)
			case "other remote":
				wantRef, remote = "refs/remotes/upstream/master", true
				r.Git("update-ref", wantRef, sha)
				r.Git("checkout", "--detach", "-q")
				r.Git("branch", "-D", "master")
			case "explicit ref":
				wantRef, remote, branch = "refs/remotes/upstream/main", true, "refs/remotes/upstream/main"
				r.Git("update-ref", wantRef, sha)
			case "tag":
				wantRef, branch = "refs/tags/target", "target"
				r.Git("tag", "target", sha)
			}
			s := openReport(t, root)
			if err := s.SetMergeFlow([]MergeStep{{Name: "ship", Branch: branch}}); err != nil {
				t.Fatal(err)
			}
			s.Settle(t.Context(), nil)
			check := mustReport(t, s, "PAY-142").Rows[0].Stages[0]
			if check.Ref != wantRef || check.SHA != wantSHA || check.Remote != remote {
				t.Fatalf("%+v want %s %s remote=%v", check, wantRef, wantSHA, remote)
			}
			s.mu.Lock()
			ix := s.eng.Index()
			s.mu.Unlock()
			if tip := releaseTip(ix.Repos[0], branch); ix.Repos[0].Oid(tip).String() != check.SHA {
				t.Fatal("report and planStage selected different commits")
			}
		})
	}
}

func TestReportA14CherryPickShowsPatchEvidence(t *testing.T) {
	root, r := reportFixture(t)
	r.Git("checkout", "-qb", "PAY-142")
	sha := r.Commit("PAY-142 work", map[string]string{"work": "yes\n"})
	r.Git("checkout", "-q", "master")
	r.Commit("advance", map[string]string{"advance": "yes\n"})
	r.Git("cherry-pick", "--no-commit", sha)
	r.Commit("applied change", nil)
	s := openReport(t, root)
	s.Track("PAY-142")
	if problems := s.Settle(t.Context(), nil); len(problems) != 0 {
		t.Fatal(problems)
	}
	report := mustReport(t, s, "PAY-142")
	if report.Progress != StageLanded || len(report.Gaps) != 0 {
		t.Fatal(report)
	}
	covered := report.Rows[0].Stages[1].Covered
	if len(covered) != 1 || covered[0].SHA != sha || covered[0].By != CoverPatch {
		t.Fatal(covered)
	}
}

func TestReportA15ClassificationAndReadFailures(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "classified", true: "unreadable"}[fail], func(t *testing.T) {
			root, r := reportFixture(t)
			r.Git("checkout", "-qb", "PAY-142")
			r.Commit("unrelated", map[string]string{"side": "yes\n"})
			r.Git("checkout", "-qb", "integration", "master")
			r.Merge("PAY-142", "Merge PAY-142")
			r.Git("checkout", "-q", "master")
			r.Git("branch", "-D", "PAY-142")
			s := openReport(t, root)
			s.Track("PAY-142")
			if err := s.Refresh(t.Context()); err != nil {
				t.Fatal(err)
			}
			s.bg.Wait()
			if fail {
				if err := os.Rename(filepath.Join(r.Dir, ".git"), filepath.Join(r.Dir, "hidden-git")); err != nil {
					t.Fatal(err)
				}
				report := mustReport(t, s, "PAY-142")
				requireProblem(t, report.Problems, ProblemUnclassifiedMerge, "repo")
				requireProblem(t, report.Problems, ProblemGitRead, "repo")
				if report.Progress == StageLanded {
					t.Fatal(report)
				}
			} else {
				problems := s.Settle(t.Context(), nil)
				if slices.ContainsFunc(problems, func(p Problem) bool { return p.Code == ProblemUnclassifiedMerge }) {
					t.Fatal(problems)
				}
				if report := mustReport(t, s, "PAY-142"); report.Progress != StageUnknown {
					t.Fatal(report)
				}
			}
		})
	}
	t.Run("patch comparison failed without hiding another repo", func(t *testing.T) {
		root, r := reportFixture(t)
		r.Git("checkout", "-qb", "PAY-142")
		r.Commit("PAY-142 work", map[string]string{"work": "yes\n"})
		other := testutil.NewRepo(t, filepath.Join(root, "other"))
		other.Commit("init", map[string]string{"base": "yes\n"})
		other.Git("checkout", "-qb", "PAY-142")
		other.Commit("PAY-142 other work", map[string]string{"work": "yes\n"})
		s := openReport(t, root)
		if err := s.Refresh(t.Context()); err != nil {
			t.Fatal(err)
		}
		s.bg.Wait()
		if err := os.Rename(filepath.Join(r.Dir, ".git"), filepath.Join(r.Dir, "hidden-git")); err != nil {
			t.Fatal(err)
		}
		report := mustReport(t, s, "PAY-142")
		requireProblem(t, report.Problems, ProblemPatchPending, "repo")
		requireProblem(t, report.Problems, ProblemGitRead, "repo")
		if len(report.Rows) != 2 || len(report.Gaps) != 2 {
			t.Fatal(report)
		}
	})
	t.Run("cancellation", func(t *testing.T) {
		root, _ := reportFixture(t)
		s := openReport(t, root)
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		requireProblem(t, s.Settle(ctx, nil), ProblemGitRead, "")
	})
}

func TestReportA16MissingFinalTarget(t *testing.T) {
	root, r := reportFixture(t)
	r.Git("checkout", "-qb", "PAY-142")
	r.Commit("PAY-142 work", map[string]string{"work": "yes\n"})
	r.Git("checkout", "-q", "release")
	r.Merge("PAY-142", "integrate feature")
	s := openReport(t, root)
	s.SetMergeFlow([]MergeStep{{Name: "release", Branch: "release"}, {Name: "ship", Branch: "absent"}})
	s.Track("PAY-142")
	if problems := s.Settle(t.Context(), nil); len(problems) != 0 {
		t.Fatal(problems)
	}
	report := mustReport(t, s, "PAY-142")
	if report.Progress == StageLanded || report.Rows[0].Stages[1].SHA != "" || !report.Rows[0].Stages[1].Applies || len(report.Gaps) != 1 || report.Gaps[0].Unknown != ProblemFinalMissing {
		t.Fatal(report)
	}
	requireProblem(t, report.Problems, ProblemFinalMissing, "repo")
}

func TestReportA18BodiesAndStablePagination(t *testing.T) {
	root, r := reportFixture(t)
	first := r.Commit("PAY-142 first\n\nBody line one.\n\n正文末行。\n", map[string]string{"first": "yes\n"})
	second := r.Commit("PAY-142 second", map[string]string{"second": "yes\n"})
	other := testutil.NewRepo(t, filepath.Join(root, "another"))
	other.Commit("init", map[string]string{"base": "yes\n"})
	other.Commit("PAY-142 tied first\n\nAnother body", map[string]string{"first": "yes\n"})
	other.Commit("PAY-142 tied second", map[string]string{"second": "yes\n"})
	s := openReport(t, root)
	s.Settle(t.Context(), nil)
	var items []CommitItem
	cursor := ""
	for {
		page, err := s.ReqCommits(t.Context(), "PAY-142", "", cursor, 1)
		if err != nil || len(page.Items) != 1 {
			t.Fatalf("%+v %v", page, err)
		}
		items = append(items, page.Items...)
		if page.Next == "" {
			break
		}
		if len(items) > 4 || page.Next == cursor {
			t.Fatal("pagination did not advance")
		}
		cursor = page.Next
	}
	if len(items) != 4 {
		t.Fatal(items)
	}
	for i, item := range items {
		if item.Body == nil || item.BodyErr != "" {
			t.Fatal(item)
		}
		if i > 0 && compareCursor(cursorOf(items[i-1]), cursorOf(item)) >= 0 {
			t.Fatal(items)
		}
		if item.SHA == first && *item.Body != "Body line one.\n\n正文末行。\n" {
			t.Fatalf("body=%q", *item.Body)
		}
		if item.SHA == second && *item.Body != "" {
			t.Fatalf("empty body=%q", *item.Body)
		}
	}
	filtered, err := s.ReqCommits(t.Context(), "PAY-142", "repo", "", 50)
	if err != nil || len(filtered.Items) != 2 {
		t.Fatalf("%+v %v", filtered, err)
	}
	batchCalls := 0
	for _, e := range s.rec.Events() {
		if len(e.Args) > 0 && e.Args[0] == "cat-file" {
			batchCalls++
		}
	}
	if batchCalls != 5 {
		t.Fatalf("%d batch calls, want one per repo per page (5)", batchCalls)
	}
	for _, tc := range []struct {
		cursor string
		limit  int
	}{{"!invalid", 1}, {"", -1}} {
		if _, err := s.ReqCommits(t.Context(), "PAY-142", "", tc.cursor, tc.limit); err == nil {
			t.Fatal("invalid pagination accepted")
		}
	}
	if err := os.Remove(filepath.Join(r.Dir, ".git", "objects", first[:2], first[2:])); err != nil {
		t.Fatal(err)
	}
	broken := mustPage(t, s, "PAY-142")
	for _, item := range broken.Items {
		if item.SHA == first && (item.Body != nil || item.BodyErr == "") {
			t.Fatal(item)
		}
		if item.SHA == second && (item.Body == nil || item.BodyErr != "") {
			t.Fatal(item)
		}
	}
}

func TestReportA20NoObjectsAndNoEvidence(t *testing.T) {
	root, _ := reportFixture(t)
	s := openReport(t, root)
	s.Settle(t.Context(), nil)
	empty, err := s.Overview(t.Context(), ScopeActive, "")
	if err != nil || empty.Items == nil || len(empty.Items) != 0 {
		t.Fatalf("%+v %v", empty, err)
	}
	untracked := mustReport(t, s, "PAY-142")
	if untracked.Found || untracked.Tracked || len(untracked.Problems) != 0 {
		t.Fatal(untracked)
	}
	s.Track("PAY-142")
	if problems := s.Settle(t.Context(), nil); len(problems) != 0 {
		t.Fatal(problems)
	}
	tracked := mustReport(t, s, "PAY-142")
	if tracked.Found || !tracked.Tracked || tracked.Progress == StageLanded {
		t.Fatal(tracked)
	}
	requireProblem(t, tracked.Problems, ProblemNoEvidence, "")
	if page := mustPage(t, s, "PAY-142"); len(page.Items) != 0 || page.Next != "" {
		t.Fatal(page)
	}
}

func TestReportUncommittedBeforeFirstCommitAndAllWorktrees(t *testing.T) {
	root, r := reportFixture(t)
	r.Git("checkout", "-qb", "PAY-142")
	r.Git("branch", "PAY-142-second")
	wt := filepath.Join(t.TempDir(), "outside")
	r.Git("worktree", "add", "-q", wt, "PAY-142-second")
	for _, dir := range []string{r.Dir, wt} {
		if err := os.WriteFile(filepath.Join(dir, "dirty"), []byte("dirty\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	s := openReport(t, root)
	s.Track("PAY-142")
	if problems := s.Settle(t.Context(), nil); len(problems) != 0 {
		t.Fatal(problems)
	}
	report := mustReport(t, s, "PAY-142")
	if !report.Found || report.Progress != StageDev || report.Uncommitted != 2 || report.Rows[0].Uncommitted != 2 || len(report.Gaps) == 0 {
		t.Fatal(report)
	}
}

func TestScopeIDs(t *testing.T) {
	root, _ := reportFixture(t)
	s := openReport(t, root)
	for _, id := range []string{"PAY-200", "PAY-142", "DONE-1001", "NONE-1001"} {
		if err := s.Track(id); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"PAY-200", "DONE-1001"} {
		if err := s.ArchiveRequirement(id, true); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		scope Scope
		want  []string
	}{
		{ScopeActive, []string{"PAY-142", "NONE-1001"}},
		{ScopeArchived, []string{"PAY-200", "DONE-1001"}},
		{ScopeAll, s.Tracked()},
		{"", []string{"PAY-142", "NONE-1001"}},
		{"invalid", nil},
	} {
		t.Run(string(tc.scope), func(t *testing.T) {
			ids, err := s.ScopeIDs(tc.scope)
			if (err != nil) != (tc.scope == "invalid") || !slices.Equal(ids, tc.want) {
				t.Fatalf("ScopeIDs(%q) = %v, %v; want %v", tc.scope, ids, err, tc.want)
			}
			if len(ids) > 0 {
				ids[0] = "changed"
				if slices.Contains(s.Tracked(), "changed") {
					t.Fatal("ScopeIDs aliases tracked configuration")
				}
			}
		})
	}
	for _, id := range []string{"PAY-142", "NONE-1001"} {
		if err := s.ArchiveRequirement(id, true); err != nil {
			t.Fatal(err)
		}
	}
	if ids, err := s.ScopeIDs(ScopeActive); err != nil || ids == nil || len(ids) != 0 {
		t.Fatalf("empty scope = %v, %v; want non-nil empty IDs", ids, err)
	}
}

func TestReportOverviewScopeOrderingAndExcluded(t *testing.T) {
	root, r := reportFixture(t)
	r.Commit("DONE-1001 landed", map[string]string{"done": "yes\n"})
	r.Git("checkout", "-qb", "PAY-142")
	r.Commit("PAY-142 refund", map[string]string{"work": "yes\n"})
	ignored := testutil.NewRepo(t, filepath.Join(root, "ignored"))
	ignored.Commit("PAY-142 ignored", map[string]string{"file": "yes\n"})
	s := openReport(t, root)
	s.Track("DONE-1001")
	s.Track("PAY-142")
	s.Track("NONE-1001")
	s.SetIgnored([]string{"ignored"})
	s.Settle(t.Context(), nil)
	out, err := s.Overview(t.Context(), ScopeAll, "")
	if err != nil || len(out.Items) != 3 || out.Items[0].ID != "NONE-1001" || out.Items[1].ID != "PAY-142" || out.Items[2].ID != "DONE-1001" || !slices.Equal(out.Excluded, []string{"ignored"}) || out.Items[1].Repos != 1 || out.ObservedAt != 2_000_000_000 {
		t.Fatalf("%+v %v", out, err)
	}
	if _, err := s.Overview(t.Context(), "invalid", ""); err == nil {
		t.Fatal("invalid scope accepted")
	}
}

func TestReportConfigValidationAndRoundTrip(t *testing.T) {
	root, _ := reportFixture(t)
	s := openReport(t, root)
	s.Track("PAY-142")
	s.ArchiveRequirement("PAY-142", true)
	before := s.ConfigJSON()
	var cfg Config
	if err := json.Unmarshal(before, &cfg); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		change func(*Config)
	}{
		{"bad pattern", func(c *Config) { c.Requirement = "[" }},
		{"empty flow", func(c *Config) { c.MergeFlow = nil }},
		{"duplicate stage", func(c *Config) {
			c.MergeFlow = []MergeStep{{Name: "same", Branch: "one"}, {Name: "same", Branch: "two"}}
		}},
		{"duplicate target", func(c *Config) { c.MergeFlow = []MergeStep{{Name: "one", Branch: "one"}, {Name: "two", Branch: "one"}} }},
		{"invalid tracked", func(c *Config) { c.Tracked = []string{"bad"} }},
		{"duplicate tracked", func(c *Config) { c.Tracked = []string{"PAY-142", "PAY-142"} }},
		{"invalid archive", func(c *Config) { c.Archived = []string{"bad"} }},
		{"duplicate archive", func(c *Config) { c.Archived = []string{"PAY-142", "PAY-142"} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var next Config
			json.Unmarshal(before, &next)
			tc.change(&next)
			b, _ := json.Marshal(next)
			diskBefore, _ := os.ReadFile(s.cfgPath)
			if err := s.ApplyConfig(b); err == nil {
				t.Fatal("invalid config accepted")
			}
			diskAfter, _ := os.ReadFile(s.cfgPath)
			if !bytes.Equal(before, s.ConfigJSON()) || !bytes.Equal(diskBefore, diskAfter) {
				t.Fatal("failed validation changed config")
			}
		})
	}
	cfg.Root = "/another/workspace"
	cfg.RequirementNames = map[string]string{"PAY-142": "Refund"}
	cfg.Ignored = []string{"repo"}
	cfg.BaselineOnly = []string{"repo"}
	cfg.Bases = map[string]string{"repo": "master"}
	b, _ := json.Marshal(cfg)
	if err := s.ApplyConfig(b); err != nil {
		t.Fatal(err)
	}
	s.bg.Wait()
	var actual Config
	json.Unmarshal(s.ConfigJSON(), &actual)
	if actual.Root != root || actual.RequirementNames["PAY-142"] != "Refund" || !slices.Equal(actual.Ignored, cfg.Ignored) {
		t.Fatal(actual)
	}
	if err := s.ApplyConfig(s.ConfigJSON()); err != nil {
		t.Fatal(err)
	}
}

func TestReportConfigTargetBranches(t *testing.T) {
	root, _ := reportFixture(t)
	s := openReport(t, root)
	for _, branch := range []string{
		"", "release", "release/v1", "feature/退款", "release-v1.2", "topic@team", "topic.locked",
	} {
		t.Run("valid "+branch, func(t *testing.T) {
			var cfg Config
			if err := json.Unmarshal(s.ConfigJSON(), &cfg); err != nil {
				t.Fatal(err)
			}
			cfg.MergeFlow = []MergeStep{{Name: "production", Branch: branch}}
			b, err := json.Marshal(cfg)
			if err != nil {
				t.Fatal(err)
			}
			if err := s.ApplyConfig(b); err != nil {
				t.Fatalf("valid target %q rejected: %v", branch, err)
			}
		})
	}
	before := s.ConfigJSON()
	diskBefore, err := os.ReadFile(s.cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, branch := range []string{
		"release candidate", "release\tcandidate", "release\ncandidate", "release\u00a0candidate",
		"topic~1", "topic^", "topic:part", "topic?", "topic*", "topic[1", `topic\part`,
		"topic..part", "topic@{1}", "-topic", "/topic", "topic/", "topic.", "topic.lock", "@",
		"topic//part", ".topic", "topic/.part", "topic.lock/part", "topic\x00part", "topic\x1fpart", "topic\x7fpart",
	} {
		t.Run("invalid "+branch, func(t *testing.T) {
			var cfg Config
			if err := json.Unmarshal(before, &cfg); err != nil {
				t.Fatal(err)
			}
			cfg.MergeFlow = []MergeStep{{Name: "production", Branch: branch}}
			b, err := json.Marshal(cfg)
			if err != nil {
				t.Fatal(err)
			}
			if err := s.ApplyConfig(b); err == nil {
				t.Fatalf("invalid target %q accepted", branch)
			}
			diskAfter, err := os.ReadFile(s.cfgPath)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, s.ConfigJSON()) || !bytes.Equal(diskBefore, diskAfter) {
				t.Fatal("failed target validation changed config")
			}
		})
	}
}

func TestReportConfigUntrackedArchiveRoundTrip(t *testing.T) {
	root, _ := reportFixture(t)
	s := openReport(t, root)
	if err := s.Track("PAY-142"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetRequirementName("PAY-142", "Refund"); err != nil {
		t.Fatal(err)
	}
	if err := s.ArchiveRequirement("PAY-142", true); err != nil {
		t.Fatal(err)
	}
	if err := s.Untrack("PAY-142"); err != nil {
		t.Fatal(err)
	}
	before := s.ConfigJSON()
	if err := s.ApplyConfig(before); err != nil {
		t.Fatalf("exported config cannot be applied: %v", err)
	}
	if !bytes.Equal(before, s.ConfigJSON()) || len(s.Tracked()) != 0 {
		t.Fatal("config round trip changed untracked archive")
	}
	if err := s.Track("PAY-142"); err != nil {
		t.Fatal(err)
	}
	var cfg Config
	if err := json.Unmarshal(s.ConfigJSON(), &cfg); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(cfg.Archived, "PAY-142") || cfg.RequirementNames["PAY-142"] != "Refund" {
		t.Fatalf("retracking lost archive or name: %+v", cfg)
	}
}

func TestReportCanonicalRoot(t *testing.T) {
	t.Setenv("SHEAF_HOME", t.TempDir())
	base := t.TempDir()
	real := filepath.Join(base, "real")
	alias := filepath.Join(base, "alias")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, alias); err != nil {
		t.Fatal(err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(cwd, alias)
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{"config", "recent", "open", "none"} {
		t.Run(source, func(t *testing.T) {
			for _, recorded := range []string{real, alias} {
				data := t.TempDir()
				var path string
				var value any
				switch source {
				case "config":
					path = filepath.Join(data, "workspaces", WorkspaceID(recorded), "workspace.json")
					value = Config{Root: recorded}
				case "recent":
					path = filepath.Join(data, "recent.json")
					value = map[string]any{"recent": []map[string]string{{"root": recorded}}}
				case "open":
					path = filepath.Join(data, "recent.json")
					value = map[string]any{"open": []string{recorded}}
				}
				if path != "" {
					if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
						t.Fatal(err)
					}
					b, err := json.Marshal(value)
					if err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(path, b, 0o644); err != nil {
						t.Fatal(err)
					}
				}
				for _, input := range []string{real, alias, relative, filepath.Join(real, "child"), real + "-other"} {
					want, err := filepath.Abs(input)
					if err != nil {
						t.Fatal(err)
					}
					if source != "none" && (input == real || input == alias || input == relative) {
						want = recorded
					}
					if got := CanonicalRoot(data, input); got != want {
						t.Errorf("CanonicalRoot(%q), recorded %q = %q, want %q", input, recorded, got, want)
					}
				}
			}
		})
	}
}

func TestReportCleanNamedWorktreeWithoutCommits(t *testing.T) {
	root, r := reportFixture(t)
	r.Git("checkout", "-qb", "PAY-142")
	s := openReport(t, root)
	if err := s.Track("PAY-142"); err != nil {
		t.Fatal(err)
	}
	s.Settle(t.Context(), nil)
	report := mustReport(t, s, "PAY-142")
	if !report.Found || !report.Tracked || report.Uncommitted != 0 || len(mustPage(t, s, "PAY-142").Items) != 0 {
		t.Fatalf("unexpected locating evidence: %+v", report)
	}
	requireProblem(t, report.Problems, ProblemNoEvidence, "")
	out, err := s.Overview(t.Context(), ScopeActive, "")
	if err != nil {
		t.Fatal(err)
	}
	requireProblem(t, out.Problems, ProblemNoEvidence, "")
	if err := s.Untrack("PAY-142"); err != nil {
		t.Fatal(err)
	}
	if report := mustReport(t, s, "PAY-142"); !report.Found || len(report.Problems) != 0 {
		t.Fatalf("unexpected untracked report: %+v", report)
	}
}

func TestReportDevGapUsesFirstMissingApplicableStage(t *testing.T) {
	for _, target := range []string{"release", "production", "baseline only", "missing target", "fully covered"} {
		t.Run(target, func(t *testing.T) {
			root, r := reportFixture(t)
			r.Git("checkout", "-qb", "PAY-142")
			sha := r.Commit("PAY-142 refund", map[string]string{"refund": "yes\n"})
			if target == "production" || target == "fully covered" {
				r.Git("branch", "-f", "release", sha)
			}
			if target == "fully covered" {
				r.Git("branch", "-f", "master", sha)
			}
			if target == "missing target" {
				r.Git("branch", "-D", "release")
			}
			if err := os.WriteFile(filepath.Join(r.Dir, "dirty"), []byte("dirty\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			s := openReport(t, root)
			if target == "baseline only" {
				if err := s.SetRepoBaselineOnly("repo", true); err != nil {
					t.Fatal(err)
				}
			}
			if err := s.Track("PAY-142"); err != nil {
				t.Fatal(err)
			}
			s.Settle(t.Context(), nil)
			report := mustReport(t, s, "PAY-142")
			if report.Progress != StageDev || len(report.Gaps) != 1 {
				t.Fatalf("unexpected progress: %+v", report)
			}
			want, missing := target, 1
			if target == "baseline only" || target == "missing target" {
				want = "production"
			}
			if target == "fully covered" {
				want, missing = "", 0
			}
			gap := report.Gaps[0]
			if gap.Next != want || gap.Missing != missing || gap.Uncommitted != 1 || gap.Unknown != "" {
				t.Fatalf("gap = %+v, want next=%q missing=%d", gap, want, missing)
			}
		})
	}
}

func TestReportFindWorkspaceRootsRecentsScansAndAmbiguity(t *testing.T) {
	t.Setenv("SHEAF_HOME", t.TempDir())
	data, cache := os.Getenv("SHEAF_HOME"), t.TempDir()
	parent := t.TempDir()
	nested := filepath.Join(parent, "nested")
	outside := t.TempDir()
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, root := range []string{parent, nested} {
		path := filepath.Join(data, "workspaces", WorkspaceID(root), "workspace.json")
		os.MkdirAll(filepath.Dir(path), 0o755)
		cfg := Config{Root: root}
		if root == nested {
			cfg.Root = ""
		}
		b, _ := json.Marshal(cfg)
		os.WriteFile(path, b, 0o644)
	}
	b, _ := json.Marshal(map[string]any{"recent": []map[string]string{{"root": nested}}})
	os.WriteFile(filepath.Join(data, "recent.json"), b, 0o644)
	saveScan(filepath.Join(cache, "workspaces", WorkspaceID(nested), "scan.json"), []discover.Repo{{Name: "repo", Dir: nested, Worktrees: []discover.Worktree{{Path: outside}}}})
	for _, tc := range []struct {
		dir  string
		want []string
	}{{nested, []string{parent, nested}}, {outside, []string{nested}}, {parent, []string{parent}}, {parent + "-other", []string{}}} {
		got, err := FindWorkspace(data, cache, tc.dir)
		slices.Sort(tc.want)
		if err != nil || !slices.Equal(got, tc.want) {
			t.Fatalf("%s: %v want %v err=%v", tc.dir, got, tc.want, err)
		}
		known, err := KnownWorkspaces(data, cache, tc.dir)
		if err != nil || len(known) != 2 {
			t.Fatalf("known workspaces: %+v, %v", known, err)
		}
		for _, item := range known {
			if item.Current != slices.Contains(tc.want, item.Root) {
				t.Fatalf("current differs from FindWorkspace: %+v, want %v", item, tc.want)
			}
		}
	}
}

func TestKnownWorkspaces(t *testing.T) {
	t.Setenv("SHEAF_HOME", t.TempDir())
	data := os.Getenv("SHEAF_HOME")
	cache, base := t.TempDir(), t.TempDir()
	configured, recent, missing := filepath.Join(base, "a-configured"), filepath.Join(base, "b-recent"), filepath.Join(base, "c-missing")
	for _, root := range []string{configured, recent, missing} {
		if err := os.Mkdir(root, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Remove(missing); err != nil {
		t.Fatal(err)
	}
	configFile := filepath.Join(data, "workspaces", WorkspaceID(configured), "workspace.json")
	if err := os.MkdirAll(filepath.Dir(configFile), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg, err := json.Marshal(Config{Root: configured, Tracked: []string{"PAY-1", "PAY-2", "PAY-3"}, Archived: []string{"PAY-2", "PAY-4"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configFile, cfg, 0o600); err != nil {
		t.Fatal(err)
	}
	recents, err := json.Marshal(map[string]any{"recent": []map[string]string{{"root": recent}, {"root": configured}}, "open": []string{missing, recent}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(data, "recent.json"), recents, 0o600); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	saveScan(filepath.Join(cache, "workspaces", WorkspaceID(configured), "scan.json"), []discover.Repo{{Dir: t.TempDir(), Worktrees: []discover.Worktree{{Path: outside}}}})
	want := []KnownWorkspace{
		{Root: configured, Name: "a-configured", ID: WorkspaceID(configured), Exists: true, Tracked: 2, Archived: 1, ConfigFile: configFile},
		{Root: recent, Name: "b-recent", ID: WorkspaceID(recent), Exists: true},
		{Root: missing, Name: "c-missing", ID: WorkspaceID(missing)},
	}
	for _, tc := range []struct {
		cwd     string
		current int
	}{{base, -1}, {configured, 0}, {recent, 1}, {outside, 0}} {
		for i := range want {
			want[i].Current = i == tc.current
		}
		got, err := KnownWorkspaces(data, cache, tc.cwd)
		if err != nil || !slices.Equal(got, want) {
			t.Fatalf("cwd %s: got %+v, want %+v, err %v", tc.cwd, got, want, err)
		}
	}
	for _, mode := range []string{"malformed", "unreadable"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("SHEAF_HOME", data)
			if err := os.Remove(configFile); err != nil {
				t.Fatal(err)
			}
			if mode == "malformed" {
				if err := os.WriteFile(configFile, []byte(`{"root":`), 0o600); err != nil {
					t.Fatal(err)
				}
			} else if err := os.Mkdir(configFile, 0o700); err != nil {
				t.Fatal(err)
			}
			got, err := KnownWorkspaces(data, cache, configured)
			want[0].Tracked, want[0].Archived = 0, 0
			if err != nil || !slices.Equal(got, want) {
				t.Fatalf("got %+v, want %+v, err %v", got, want, err)
			}
		})
	}
}

func TestReportOpenPersistsRootAndHonorsReadOnlyCallback(t *testing.T) {
	root, _ := reportFixture(t)
	for _, readOnly := range []bool{false, true} {
		data := t.TempDir()
		opts := Options{DataDir: data, CacheDir: t.TempDir(), NoAutoRefresh: true}
		if readOnly {
			opts.CacheWrite = func(func()) {}
		}
		s, err := Open(t.Context(), root, opts)
		if err != nil {
			t.Fatal(err)
		}
		s.Close()
		b, err := os.ReadFile(s.cfgPath)
		if readOnly {
			if !os.IsNotExist(err) {
				t.Fatalf("query without lock wrote config: %s %v", b, err)
			}
			continue
		}
		var cfg Config
		json.Unmarshal(b, &cfg)
		if err != nil || cfg.Root != root {
			t.Fatalf("%+v %v", cfg, err)
		}
		roots, err := FindWorkspace(data, opts.CacheDir, root)
		if err != nil || !slices.Equal(roots, []string{root}) {
			t.Fatalf("%v %v", roots, err)
		}
	}
}

func TestCompletionDataImpliedFlow(t *testing.T) {
	t.Setenv("SHEAF_HOME", t.TempDir())
	data, root := os.Getenv("SHEAF_HOME"), t.TempDir()
	file := filepath.Join(data, "workspaces", WorkspaceID(root), "workspace.json")
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(`{"release":"origin/release"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := CompletionData(data, filepath.Join(data, "cache"), root)
	if err != nil || !slices.Equal(got.Stages, []string{"origin/release", "master"}) {
		t.Fatalf("stages without a saved flow: %+v, %v", got.Stages, err)
	}
}
