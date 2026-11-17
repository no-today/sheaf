// Package appdir locates shared app data and locks workspaces.
package appdir

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

func Root(dev bool) string {
	if root := os.Getenv("SHEAF_HOME"); root != "" {
		return root
	}
	home, _ := os.UserHomeDir()
	if dev {
		return filepath.Join(home, ".sheaf-dev")
	}
	return filepath.Join(home, ".sheaf")
}

func Dev(name string) bool {
	return strings.HasSuffix(name, " Dev") || strings.HasSuffix(name, "-dev")
}

func Data(root string) string  { return root }
func Cache(root string) string { return filepath.Join(root, "cache") }

var ErrBusy = errors.New("workspace is busy")

func Lock(ctx context.Context, dataDir, workspaceID string) (unlock func(), err error) {
	return lock(ctx, dataDir, workspaceID, true)
}

func TryLock(ctx context.Context, dataDir, workspaceID string) (unlock func(), err error) {
	return lock(ctx, dataDir, workspaceID, false)
}

func lock(ctx context.Context, dataDir, workspaceID string, wait bool) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path := filepath.Join(dataDir, "workspaces", workspaceID, "lock")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create workspace lock directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open workspace lock: %w", err)
	}
	acquired := false
	defer func() {
		if !acquired {
			file.Close()
		}
	}()
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			acquired = true
			var once sync.Once
			return func() { once.Do(func() { file.Close() }) }, nil
		}
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			return nil, fmt.Errorf("lock workspace: %w", err)
		}
		if !wait {
			return nil, ErrBusy
		}
		timer := time.NewTimer(20 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}
