package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

func init() {
	Register("portainer", &portainerWidget{})
}

// portainerWidget polls one Portainer-managed Docker environment's container
// list. Secrets["apiKey"] is a Portainer API key, sent as the "X-API-Key"
// header; config.endpointId selects which Portainer "environment" (Docker
// endpoint) to query, since one Portainer instance can manage several.
type portainerWidget struct{}

const (
	labelRunning = "Running"
	labelStopped = "Stopped"
)

type portainerConfig struct {
	EndpointID int `json:"endpointId"`
}

type portainerContainer struct {
	State string `json:"State"`
}

func (portainerWidget) Poll(ctx context.Context, httpClient *http.Client, cfg WidgetConfig) ([]Field, error) {
	if cfg.URL == "" {
		return nil, errors.New("portainer widget: url is required")
	}

	var portainerCfg portainerConfig
	if len(cfg.Config) > 0 {
		if err := json.Unmarshal(cfg.Config, &portainerCfg); err != nil {
			return nil, fmt.Errorf("decoding widget config: %w", err)
		}
	}
	if portainerCfg.EndpointID <= 0 {
		return nil, errors.New("portainer widget: config.endpointId is required")
	}

	endpoint := fmt.Sprintf("%s/api/endpoints/%d/docker/containers/json?all=1", strings.TrimRight(cfg.URL, "/"), portainerCfg.EndpointID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	req.Header.Set(headerXAPIKey, cfg.Secrets[secretAPIKey])

	var containers []portainerContainer
	if fields, err := doJSONRequest(httpClient, req, &containers); fields != nil || err != nil {
		return fields, err
	}

	running, stopped := 0, 0
	for _, c := range containers {
		if c.State == "running" {
			running++
		} else {
			stopped++
		}
	}

	return []Field{
		{Label: labelRunning, Value: fmt.Sprintf("%d", running)},
		{Label: labelStopped, Value: fmt.Sprintf("%d", stopped)},
	}, nil
}

func (portainerWidget) Sample(WidgetConfig) []Field {
	return []Field{
		{Label: labelRunning, Value: "14"},
		{Label: labelStopped, Value: "2"},
	}
}
