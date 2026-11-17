package session

import (
	"strings"

	"sheaf/internal/index"
)

// releaseParallel bounds concurrent patch comparisons when settling requirements.
const releaseParallel = 4

// releaseTip is the commit of the release branch: origin/<name> first (what was published), then the local branch.
func releaseTip(r *index.Repo, name string) int32 {
	commit, _ := releaseTarget(r, name)
	return commit
}

func releaseTarget(r *index.Repo, name string) (int32, string) {
	for _, name := range []string{"refs/remotes/origin/" + name, "refs/heads/" + name, name} {
		var remote *index.Ref
		for _, ref := range r.Refs {
			switch {
			case ref.Name == name || ref.Name == "refs/heads/"+name || ref.Name == "refs/tags/"+name:
				return ref.Commit, ref.Name
			case remote == nil && strings.HasPrefix(ref.Name, "refs/remotes/") && strings.HasSuffix(ref.Name, "/"+name):
				copy := ref
				remote = &copy
			}
		}
		if remote != nil {
			return remote.Commit, remote.Name
		}
	}
	return -1, ""
}
