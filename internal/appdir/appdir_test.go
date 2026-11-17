package appdir

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, tc := range []struct {
		name, override, want string
		dev                  bool
	}{
		{"release", "", filepath.Join(home, ".sheaf"), false},
		{"development", "", filepath.Join(home, ".sheaf-dev"), true},
		{"release override", filepath.Join(home, "custom"), filepath.Join(home, "custom"), false},
		{"development override", "relative-home", "relative-home", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("SHEAF_HOME", tc.override)
			if got := Root(tc.dev); got != tc.want {
				t.Errorf("Root(%v) = %q, want %q", tc.dev, got, tc.want)
			}
			if Data(tc.want) != tc.want || Cache(tc.want) != filepath.Join(tc.want, "cache") {
				t.Fatal("unexpected data/cache layout")
			}
		})
	}
}

func TestDev(t *testing.T) {
	for _, tc := range []struct {
		name string
		want bool
	}{
		{"Sheaf", false}, {"sheaf", false}, {"", false},
		{"Sheaf Dev", true}, {"sheaf-dev", true}, {"/tmp/sheaf-dev", true},
		{"Sheaf Development", false}, {"sheaf-dev-extra", false}, {"sheaf-Dev", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Dev(tc.name); got != tc.want {
				t.Errorf("Dev(%q) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}

func TestWorkspaceLockHelper(t *testing.T) {
	if os.Getenv("SHEAF_LOCK_HELPER") != "1" {
		return
	}
	if _, err := Lock(t.Context(), os.Getenv("SHEAF_HOME"), "one"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stdout.WriteString("locked\n"); err != nil {
		t.Fatal(err)
	}
	var b [1]byte
	os.Stdin.Read(b[:])
	os.Exit(0) // No unlock: process termination must release the kernel lock.
}

func TestWorkspaceLocks(t *testing.T) {
	root := t.TempDir()
	t.Setenv("SHEAF_HOME", root)
	t.Setenv("SHEAF_LOCK_HELPER", "1")
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestWorkspaceLockHelper$")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { cmd.Process.Kill(); cmd.Wait() }()
	if line, err := bufio.NewReader(stdout).ReadString('\n'); err != nil || line != "locked\n" {
		t.Fatalf("helper readiness = %q, %v: %s", line, err, &stderr)
	}
	if unlock, err := TryLock(ctx, root, "one"); !errors.Is(err, ErrBusy) || unlock != nil {
		t.Fatalf("TryLock held workspace = %v (unlock present: %v)", err, unlock != nil)
	}
	other, err := TryLock(ctx, root, "two")
	if err != nil {
		t.Fatalf("different workspace blocked: %v", err)
	}
	other()
	timed, stop := context.WithTimeout(ctx, 60*time.Millisecond)
	defer stop()
	if unlock, err := Lock(timed, root, "one"); !errors.Is(err, context.DeadlineExceeded) || unlock != nil {
		t.Fatalf("Lock timeout = %v (unlock present: %v)", err, unlock != nil)
	}
	type result struct {
		unlock func()
		err    error
	}
	waiting := make(chan result, 1)
	go func() {
		unlock, err := Lock(ctx, root, "one")
		waiting <- result{unlock, err}
	}()
	select {
	case got := <-waiting:
		if got.unlock != nil {
			got.unlock()
		}
		t.Fatalf("Lock did not wait: %v", got.err)
	case <-time.After(40 * time.Millisecond):
	}
	if err := stdin.Close(); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("helper exit: %v: %s", err, &stderr)
	}
	got := <-waiting
	if got.err != nil {
		t.Fatalf("Lock after process exit: %v", got.err)
	}
	got.unlock()
	got.unlock()
	if _, err := os.Stat(filepath.Join(root, "workspaces", "one", "lock")); err != nil {
		t.Fatal("unlock removed lock file:", err)
	}
	unlock, err := TryLock(ctx, root, "one")
	if err != nil {
		t.Fatalf("stale lock file blocks acquisition: %v", err)
	}
	unlock()
	cancelled, stop := context.WithCancel(ctx)
	stop()
	for _, acquire := range []func(context.Context, string, string) (func(), error){Lock, TryLock} {
		if unlock, err := acquire(cancelled, root, "one"); !errors.Is(err, context.Canceled) || unlock != nil {
			t.Fatalf("canceled acquisition = %v", err)
		}
	}
}
