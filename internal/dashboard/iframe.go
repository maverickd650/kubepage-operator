package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

func init() {
	Register(widgetTypeIframe, &iframeWidget{})
}

// widgetTypeIframe is the ServiceWidget type embedding a sandboxed <iframe>
// on a service card (homepage's "iframe" widget), rather than polling an
// upstream for Fields to render as stat chips. cards.templ special-cases
// this WidgetType to render the embed instead of the usual stats grid.
//
// Because the embed itself lives in card.Fields (see iframeSrc/iframeHeight
// below), a ServiceEntry.Spec.ShowStats="Hide" on an iframe widget's entry
// hides the iframe too, not just stat chips — pollWidget only populates
// card.Fields when ShowStats is on. Don't set ShowStats=Hide on an entry
// carrying an iframe widget.
const widgetTypeIframe = "iframe"

// Field labels iframeWidget.Poll returns, read back by cards.templ via
// iframeSrc/iframeHeight instead of being rendered as ordinary stat chips.
const (
	labelIframeSrc    = "iframeSrc"
	labelIframeHeight = "iframeHeight"

	iframeDefaultHeight = "300px"

	// iframeSandbox is a fixed, minimal sandbox policy applied to every
	// iframe widget — not configurable per-widget, since the whole point of
	// sandboxing is that the operator (who trusts cfg.URL, the same trust
	// level as any other widget's URL — see ServiceWidget.Secrets' RBAC
	// note) decides the policy, not whatever that URL happens to serve.
	// allow-scripts+allow-same-origin covers the common case of an
	// embedded dashboard needing its own JS; no allow-forms, allow-popups,
	// allow-top-navigation, or allow-modals.
	iframeSandbox = "allow-scripts allow-same-origin"
)

// iframeWidget embeds cfg.URL directly rather than fetching it: an iframe's
// entire point is that the browser loads it, not the dashboard backend, so
// Poll only validates config and never makes an HTTP request of its own.
type iframeWidget struct{}

type iframeConfig struct {
	// Height is any valid CSS length (e.g. "300px", "50vh"); defaults to
	// iframeDefaultHeight.
	Height string `json:"height"`
}

func (iframeWidget) Poll(_ context.Context, _ *http.Client, cfg WidgetConfig) ([]Field, error) {
	if cfg.URL == "" {
		return nil, errors.New("iframe widget: url is required")
	}
	// Defense in depth alongside the page's CSP (server.go's "frame-src
	// https:"), which is what actually stops a browser from loading a
	// javascript:/data: iframe today: reject the scheme here too, so the
	// card errors clearly instead of silently depending on the CSP being a
	// compile-time constant that never regresses.
	if !isHTTPURL(cfg.URL) {
		return nil, errors.New("iframe widget: url must be http:// or https://")
	}

	height := iframeDefaultHeight
	if len(cfg.Config) > 0 {
		var c iframeConfig
		if err := json.Unmarshal(cfg.Config, &c); err != nil {
			return nil, fmt.Errorf("decoding widget config: %w", err)
		}
		if c.Height != "" {
			height = c.Height
		}
	}

	return []Field{
		{Label: labelIframeSrc, Value: cfg.URL},
		{Label: labelIframeHeight, Value: height},
	}, nil
}

// iframeSrc and iframeHeight read back the Fields iframeWidget.Poll produced
// for a Card whose WidgetType is "iframe", for cards.templ to render an
// actual <iframe> instead of the usual stats grid.
func iframeSrc(fields []Field) string    { return fieldValue(fields, labelIframeSrc) }
func iframeHeight(fields []Field) string { return fieldValue(fields, labelIframeHeight) }

func fieldValue(fields []Field, label string) string {
	for _, f := range fields {
		if f.Label == label {
			return f.Value
		}
	}
	return ""
}
