package dashboard

import (
	"errors"
	"testing"

	"github.com/go-logr/logr/funcr"
)

// captureWidgetLog swaps widgetLog for a recorder and resets pollFailureLog,
// so each test starts from an empty dedupe map and gets back exactly the
// messages logPollFailure emitted.
func captureWidgetLog(t *testing.T) *[]string {
	t.Helper()

	got := new([]string)
	saved := widgetLog
	widgetLog = funcr.New(func(_, args string) {
		*got = append(*got, args)
	}, funcr.Options{})

	pollFailureLog.mu.Lock()
	pollFailureLog.last = map[string]string{}
	pollFailureLog.mu.Unlock()

	t.Cleanup(func() {
		widgetLog = saved
		pollFailureLog.mu.Lock()
		pollFailureLog.last = map[string]string{}
		pollFailureLog.mu.Unlock()
	})
	return got
}

func TestLogPollFailureDeduplicates(t *testing.T) {
	got := captureWidgetLog(t)
	boom := errors.New("boom")

	for range 5 {
		logPollFailure("https://nas.example", boom, "widget request failed")
	}
	if len(*got) != 1 {
		t.Fatalf("repeated identical failure logged %d times, want 1: %v", len(*got), *got)
	}

	// A different reason for the same target is new information.
	logPollFailure("https://nas.example", errors.New("other"), "widget request failed")
	if len(*got) != 2 {
		t.Fatalf("changed failure reason logged %d times total, want 2: %v", len(*got), *got)
	}

	// A different target is tracked independently.
	logPollFailure("https://other.example", boom, "widget request failed")
	if len(*got) != 3 {
		t.Fatalf("second target logged %d times total, want 3: %v", len(*got), *got)
	}
}

func TestLogPollFailureLogsAgainAfterRecovery(t *testing.T) {
	got := captureWidgetLog(t)
	boom := errors.New("boom")

	logPollFailure("https://nas.example", boom, "widget request failed")
	clearPollFailure("https://nas.example")
	logPollFailure("https://nas.example", boom, "widget request failed")

	if len(*got) != 2 {
		t.Errorf("failure after recovery logged %d times, want 2: %v", len(*got), *got)
	}
}

func TestLogPollFailureBoundsDedupeMap(t *testing.T) {
	captureWidgetLog(t)

	for i := range maxPollFailureKeys + 10 {
		logPollFailure(string(rune('a'+i%26))+string(rune(i)), errors.New("boom"), "widget request failed")
	}

	pollFailureLog.mu.Lock()
	size := len(pollFailureLog.last)
	pollFailureLog.mu.Unlock()
	if size > maxPollFailureKeys {
		t.Errorf("dedupe map holds %d keys, want at most %d", size, maxPollFailureKeys)
	}
}
