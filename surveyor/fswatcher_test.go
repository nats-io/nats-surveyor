package surveyor

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/sirupsen/logrus"
	logtest "github.com/sirupsen/logrus/hooks/test"
)

// injectWatcherErrors sends errors on the watcher's unbuffered Errors channel,
// as fsnotify does when its backend reports one. A send only succeeds if the
// watcher goroutine reads Errors. Every error must be logged as a warning.
//
// fsnotify closes Errors from its own goroutine and nothing orders that with
// these sends, so only call this on a running watcher and wait for an event
// processed afterwards before the watcher is closed.
func injectWatcherErrors(t *testing.T, watcher *fsnotify.Watcher, logs *logtest.Hook) {
	t.Helper()
	injected := []struct {
		err  error
		want string
	}{
		{err: errors.New("injected watcher error"), want: "filesystem watcher error: injected watcher error"},
		{err: fsnotify.ErrEventOverflow, want: "dropped events, config changes may have been missed"},
	}
	for _, inj := range injected {
		select {
		case watcher.Errors <- inj.err:
		case <-time.After(defaultWaitFor):
			t.Fatalf("watcher error %q was not received", inj.err)
		}
		eventually(t, func() (string, error) {
			if !warningLogged(logs, inj.want) {
				return "waiting for the watcher error to be logged", fmt.Errorf("no warning containing %q", inj.want)
			}
			return "", nil
		}, defaultWaitFor, defaultTick)
	}
}

func warningLogged(logs *logtest.Hook, substr string) bool {
	for _, e := range logs.AllEntries() {
		if e.Level == logrus.WarnLevel && strings.Contains(e.Message, substr) {
			return true
		}
	}
	return false
}

// writeConfigFile renames the config into place so that a watcher never reads
// a partly written file.
func writeConfigFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path+".tmp", []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.Rename(path+".tmp", path); err != nil {
		t.Fatalf("rename config: %v", err)
	}
}
