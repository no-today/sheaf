package cli

import (
	"bytes"
	"strings"
	"testing"

	"sheaf/internal/session"
)

func human(t *testing.T, want int, args ...string) string {
	t.Helper()
	var stdout, stderr bytes.Buffer
	if code := Main(append([]string{"sheaf"}, args...), &stdout, &stderr); code != want {
		t.Fatalf("%v: exit %d, want %d\n%s\n%s", args, code, want, &stdout, &stderr)
	}
	return stdout.String()
}

func TestHumanStatusAndCheckSummaries(t *testing.T) {
	root, repo := setup(t)
	repo.Git("checkout", "-qb", "PAY-142")
	first := repo.Commit("PAY-142 payment", map[string]string{"payment": "yes\n"})
	second := repo.Commit("PAY-142 followup", map[string]string{"followup": "yes\n"})
	invoke(t, 0, "--workspace", root, "track", "PAY-142")
	for _, tc := range []struct {
		args    []string
		code    int
		missing bool
	}{
		{[]string{"status"}, 0, false},
		{[]string{"check"}, 1, false},
		{[]string{"check", "PAY-142"}, 1, true},
		{[]string{"check", "--stage", "release"}, 1, true},
		{[]string{"check", "PAY-142", "--stage", "release"}, 1, true},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			out := human(t, tc.code, append([]string{"--workspace", root}, tc.args...)...)
			for _, text := range []string{"PAY-142 followup | awaiting release | archived: false | repos: 1 | uncommitted: 0", "repo | next=release missing=2"} {
				if !strings.Contains(out, text) {
					t.Fatalf("missing %q:\n%s", text, out)
				}
			}
			for _, text := range []string{root, "Flow:", "refs/", "worktree:", "covered=", "Read commits:", first, second} {
				if strings.Contains(out, text) {
					t.Fatalf("summary contains detail %q:\n%s", text, out)
				}
			}
			for _, sha := range []string{first, second} {
				if strings.Contains(out, sha[:7]) != tc.missing {
					t.Fatalf("unexpected commit detail:\n%s", out)
				}
			}
		})
	}
	show := human(t, 0, "--workspace", root, "show", "PAY-142")
	for _, text := range []string{repo.Dir, "Flow: release → production (master)", "refs/heads/release", first, second} {
		if !strings.Contains(show, text) {
			t.Fatalf("show lost %q:\n%s", text, show)
		}
	}
	out := invoke(t, 0, "--workspace", root, "status")
	status := decode[statusResult](t, out.Result)
	if len(status.Items[0].Rows[0].Stages[0].Missing) != 2 || len(status.Items[0].Rows[0].Stages[0].SHA) != 40 {
		t.Fatalf("JSON lost stage evidence: %+v", status)
	}
}

func TestHumanNotFoundHasOnlyErrorAndNextStep(t *testing.T) {
	root, _ := setup(t)
	for _, command := range []string{"show", "commits", "check"} {
		out := human(t, 3, "--workspace", root, command, "PAY-999")
		if !strings.HasPrefix(out, "not_found:") || !strings.Contains(out, "not found in local history or working directories") || !strings.Contains(out, "Next:") {
			t.Fatal(out)
		}
		for _, text := range []string{"Workspace:", "Flow:", "Tracked:", "repos:", "Observed:", "Read commits:", "No locatable records"} {
			if strings.Contains(out, text) {
				t.Fatalf("not-found error contains report %q:\n%s", text, out)
			}
		}
	}
}

func TestHumanShowOmitsEmptyFieldsAndFormatsFlow(t *testing.T) {
	e := envelope{
		Scope:    scope{Command: "show", ObservedAt: 1, Repositories: []string{"repo"}},
		Problems: []session.Problem{{Code: session.ProblemNoEvidence}},
		Result: session.ReqReport{
			ReqSummary: session.ReqSummary{ID: "PAY-142", Progress: session.StageUnknown, Repos: 1,
				Gaps: []session.RepoGap{{Repo: "repo", Unknown: session.ProblemNoEvidence}}},
			Flow: []session.MergeStep{{Name: "release", Branch: "release"}, {Name: "master"}},
			Rows: []session.RowReport{{Repo: "repo", Path: "/workspace/repo", Stages: []session.StageCheck{{Name: "release", Applies: true}, {Name: "master"}}}},
		},
	}
	var out bytes.Buffer
	if err := render(&out, e, 2); err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"Flow: release → master", "PAY-142 | unknown", "repo | cannot determine: no_evidence", "release: target unavailable", "master: not applicable", "Problem no_evidence"} {
		if !strings.Contains(out.String(), text) {
			t.Fatalf("missing %q:\n%s", text, &out)
		}
	}
	for _, text := range []string{"worktree:", "branch:", "excluded:", "repo:", "stage:", "unknown=", "next=", "()", " | \n", "  (local fallback)"} {
		if strings.Contains(out.String(), text) {
			t.Fatalf("empty field %q:\n%s", text, &out)
		}
	}
}

func TestHumanCheckMissingCommitsUseRequestedGap(t *testing.T) {
	first, second := strings.Repeat("a", 40), strings.Repeat("b", 40)
	r := session.ReqReport{
		ReqSummary: session.ReqSummary{ID: "PAY-142", Progress: "merge:1", Gaps: []session.RepoGap{{Repo: "repo", Next: "production", Missing: 1}}},
		Flow:       []session.MergeStep{{Name: "release"}, {Name: "production"}},
		Rows: []session.RowReport{{Repo: "repo", Stages: []session.StageCheck{
			{Name: "release", Applies: true, Missing: []session.CommitRef{{SHA: first, Subject: "earlier stage"}}},
			{Name: "production", Applies: true, Missing: []session.CommitRef{{SHA: second, Subject: "next stage"}}},
		}}},
	}
	e := envelope{Complete: true, Scope: scope{Command: "check", Requirement: r.ID}, Result: checkResult{Items: []session.ReqReport{r}}}
	var out bytes.Buffer
	if err := render(&out, e, 1); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), first[:7]) || !strings.Contains(out.String(), second[:7]+" — next stage") {
		t.Fatal(&out)
	}
	// Diagnostic details can contain paths and full hashes; overview output keeps only their reason and object.
	e.Scope.Requirement = ""
	e.Problems = []session.Problem{{Code: session.ProblemGitRead, Repo: "repo", Detail: "/workspace/repo " + first}}
	out.Reset()
	if err := render(&out, e, 2); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "/workspace") || strings.Contains(out.String(), first) || !strings.Contains(out.String(), "Problem git_read | repo: repo") {
		t.Fatal(&out)
	}
}
