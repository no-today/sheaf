package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"sheaf/internal/appdir"
	"sheaf/internal/session"
)

// A lock held elsewhere past the wait is reported as busy, in the user's language rather than a fixed one.
func TestLockTimeoutIsWorkspaceBusy(t *testing.T) {
	data, root := t.TempDir(), t.TempDir()
	unlock, err := appdir.Lock(context.Background(), data, session.WorkspaceID(root))
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := (&Workspace{}).lockWorkspace(ctx, data, root); !errors.Is(err, errWorkspaceBusy) {
		t.Fatalf("lock held elsewhere: %v", err)
	}
	for lang, tx := range menuTexts {
		if tx.busy == "" {
			t.Errorf("%s has no busy text", lang)
		}
	}
}
