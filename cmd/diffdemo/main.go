// Diffdemo opens Preview Diff (internal/native/diffview) on a real commit in a native window, to look at it.
//
//	go run ./cmd/diffdemo -repo "$SHEAF_BENCH_ROOT/some-repo" [-commit <sha>] [-unified] [-capture shot.png]
//
// Without -commit it takes the newest commit changing a few text files. -capture saves a PNG of the window at the
// screen's scale once the first file is shown, and quits.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"sheaf/internal/native/diffview"
	"sheaf/internal/query"
	"sheaf/internal/session"
)

func main() {
	repo := flag.String("repo", "", "repository directory")
	commit := flag.String("commit", "", "commit hash (default: a recent commit changing a few text files)")
	unified := flag.Bool("unified", false, "one column instead of side by side")
	capture := flag.String("capture", "", "save a PNG of the window to this path and quit")
	flag.Parse()
	if *repo == "" {
		flag.Usage()
		os.Exit(2)
	}
	ctx := context.Background()
	tmp, err := os.MkdirTemp("", "diffdemo")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(tmp)
	s, err := session.Open(ctx, *repo, session.Options{DataDir: filepath.Join(tmp, "data"), CacheDir: filepath.Join(tmp, "cache")})
	if err != nil {
		log.Fatal(err)
	}
	if err := s.Refresh(ctx); err != nil {
		log.Fatal(err)
	}
	k := session.CommitKey{Repo: s.Name, Hash: *commit}
	var files []session.FileChange
	if *commit == "" {
		k, files, err = pick(ctx, s)
	} else {
		files, err = s.Files(ctx, []session.CommitKey{k})
	}
	if err != nil {
		log.Fatal(err)
	}
	detail, _ := s.Commit(ctx, k)
	fmt.Printf("%s %s: %s (%d files)\n", k.Repo, k.Hash, detail.Subject, len(files))

	st := diffview.New(diffview.FilesOf(files), diffview.CommitLoader(s, k))
	if *unified {
		st.Layout = editor.DiffUnified
	}
	view := func(c *ui.Context) { diffview.View(c, st).AutoFocus() }
	mygo.App.WhenReady(func() {
		mygo.App.SetMenu(mygo.NewMenu([]*mygo.MenuItem{{Role: mygo.RoleAppMenu}, {Role: mygo.RoleEditMenu}, {Role: mygo.RoleWindowMenu}}))
		win := mygo.NewWindow(mygo.WindowOptions{Title: detail.Subject, Width: 1200, Height: 760, Content: ui.View(view)})
		if *capture != "" {
			go shoot(win, st, *capture)
		}
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

// pick finds a recent commit that is not a merge and changes 2 to 12 files, a text one by 10 to 300 lines.
func pick(ctx context.Context, s *session.Session) (session.CommitKey, []session.FileChange, error) {
	res, err := s.Query(query.Conditions{})
	if err != nil {
		return session.CommitKey{}, nil, err
	}
	rows, err := s.Rows(res.ID, 0, 300)
	if err != nil {
		return session.CommitKey{}, nil, err
	}
	for _, r := range rows {
		if r.Merge {
			continue
		}
		k := session.CommitKey{Repo: r.Repo, Hash: r.Hash}
		files, err := s.Files(ctx, []session.CommitKey{k})
		if err != nil || len(files) < 2 || len(files) > 12 {
			continue
		}
		for _, f := range files {
			if n := f.Add + f.Del; !f.Binary && f.Status == "M" && n >= 10 && n <= 300 {
				return k, files, nil
			}
		}
	}
	return session.CommitKey{}, nil, fmt.Errorf("no commit changing a few text files in the last %d", len(rows))
}

func shoot(win *mygo.Window, st *diffview.State, path string) {
	for range 200 {
		ready := make(chan bool)
		win.Update(func() { ready <- st.Diff() != nil })
		if <-ready {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	time.Sleep(500 * time.Millisecond) // highlighting finishes a frame's budget at a time
	png, err := win.CapturePage()
	if err == nil {
		err = os.WriteFile(path, png, 0o644)
	}
	if err != nil {
		log.Print(err)
	}
	mygo.App.Quit()
}
