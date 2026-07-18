package dashboard

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

func init() {
	Register("homeassistant", &homeassistantWidget{})
}

const (
	labelPeopleHome = "People Home"
	labelLightsOn   = "Lights On"
	labelSwitchesOn = "Switches On"
)

// homeassistantWidget polls Home Assistant's template API
// (POST /api/template) three times, matching gethomepage/homepage's default
// homeassistant widget fields: how many people are home, how many lights are
// on, and how many switches are on. Each call's Jinja template renders a
// plain-text "<on> / <total>" string, which /api/template returns verbatim
// as the response body (not JSON). Secrets["token"] is a Home Assistant
// long-lived access token, sent as a Bearer token.
type homeassistantWidget struct{}

// homeassistantTemplates pairs each field's label with the upstream Jinja
// template that produces its "<on> / <total>" value, in display order.
var homeassistantTemplates = []struct {
	label    string
	template string
}{
	{labelPeopleHome, `{{ states.person|selectattr('state','equalto','home')|list|length }} / {{ states.person|list|length }}`},
	{labelLightsOn, `{{ states.light|selectattr('state','equalto','on')|list|length }} / {{ states.light|list|length }}`},
	{labelSwitchesOn, `{{ states.switch|selectattr('state','equalto','on')|list|length }} / {{ states.switch|list|length }}`},
}

func (homeassistantWidget) Poll(ctx context.Context, httpClient *http.Client, cfg WidgetConfig) ([]Field, error) {
	if cfg.URL == "" {
		return nil, errors.New("homeassistant widget: url is required")
	}
	token := cfg.Secrets["token"]

	fields := make([]Field, 0, len(homeassistantTemplates))
	for _, t := range homeassistantTemplates {
		value, statusFields, err := homeassistantRenderTemplate(ctx, httpClient, cfg.URL, token, t.template)
		if statusFields != nil || err != nil {
			return statusFields, err
		}
		fields = append(fields, Field{Label: t.label, Value: value})
	}
	return fields, nil
}

func (homeassistantWidget) Sample(WidgetConfig) []Field {
	return []Field{
		{Label: labelPeopleHome, Value: "2 / 4"},
		{Label: labelLightsOn, Value: "5 / 12"},
		{Label: labelSwitchesOn, Value: "1 / 3"},
	}
}

// homeassistantRenderTemplate POSTs template to cfg.URL+"/api/template" and
// returns its trimmed plain-text result. Mirrors doJSONRequest's
// Field/error convention (see httpwidget.go) since /api/template's response
// isn't JSON, so fetchJSON/doJSONRequest don't apply: a transport failure or
// non-200 response is reported as a "Status" Field with a nil error, and a
// nil Field slice with a nil error means value is populated.
func homeassistantRenderTemplate(ctx context.Context, httpClient *http.Client, baseURL, token, template string) (value string, statusFields []Field, err error) {
	body, err := json.Marshal(struct {
		Template string `json:"template"`
	}{Template: template})
	if err != nil {
		return "", nil, fmt.Errorf("encoding request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+"/api/template", bytes.NewReader(body))
	if err != nil {
		return "", nil, fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", []Field{{Label: labelStatus, Value: statusUnreach}}, nil
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", []Field{{Label: labelStatus, Value: fmt.Sprintf("HTTP %d", resp.StatusCode)}}, nil
	}

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxWidgetResponseBytes))
	if err != nil {
		return "", nil, fmt.Errorf("reading response: %w", err)
	}
	return strings.TrimSpace(string(respBody)), nil, nil
}
