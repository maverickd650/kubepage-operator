package dashboard

import (
	"sync"

	ctrl "sigs.k8s.io/controller-runtime"
)

var widgetLog = ctrl.Log.WithName("dashboard-widget")

// maxPollFailureKeys bounds pollFailureLog's dedupe map the same way
// boundedClientCache bounds the per-target client caches: keys are derived
// from user-editable widget URLs, so over the dashboard pod's indefinite
// lifetime an operator repeatedly retyping a URL must not grow this map
// without limit. On overflow the map is dropped wholesale rather than evicted
// one entry at a time — the only cost of forgetting a key is one duplicate
// log line the next time that target fails.
const maxPollFailureKeys = 256

// pollFailureLog deduplicates widget poll-failure logging. Without it, a
// single misconfigured widget would log every poll cycle (every 15s by
// default) for as long as it stays broken, which is noisy enough that
// operators filter it out — exactly the failure mode that left a broken
// widget showing "Unreachable" in the UI with nothing at all in the pod log.
// Instead, a given target logs when its failure first appears and again only
// when the reason changes or the target recovers.
var pollFailureLog = struct {
	mu   sync.Mutex
	last map[string]string
}{last: map[string]string{}}

// logPollFailure logs reason for key at Error level, but only when it differs
// from the last reason logged for that key (see pollFailureLog). keysAndValues
// are passed through to the logger unchanged, so callers can attach the
// widget type, target URL, and any other context.
//
// key identifies the polled target (typically the redacted request URL); err
// is the underlying cause, msg the package-standard message. Callers must
// never pass a raw URL that could carry a credential in its query string —
// use (*url.URL).Redacted().
func logPollFailure(key string, err error, msg string, keysAndValues ...any) {
	reason := msg
	if err != nil {
		reason += ": " + err.Error()
	}

	pollFailureLog.mu.Lock()
	if pollFailureLog.last[key] == reason {
		pollFailureLog.mu.Unlock()
		return
	}
	if len(pollFailureLog.last) >= maxPollFailureKeys {
		pollFailureLog.last = map[string]string{}
	}
	pollFailureLog.last[key] = reason
	pollFailureLog.mu.Unlock()

	widgetLog.Error(err, msg, keysAndValues...)
}

// clearPollFailure forgets key's last logged reason, so the next failure for
// that target logs again even if it repeats the previous reason. Called on a
// successful poll, which is what makes a widget that breaks, recovers, and
// breaks again the same way log all three times rather than only the first.
func clearPollFailure(key string) {
	pollFailureLog.mu.Lock()
	defer pollFailureLog.mu.Unlock()
	delete(pollFailureLog.last, key)
}
