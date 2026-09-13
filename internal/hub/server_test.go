package hub

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/seesize/seesize/internal/model"
)

func TestHeartbeatAndList(t *testing.T) {
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	store := NewStore(30 * time.Second)
	server, err := NewServer("test-secret", store)
	if err != nil {
		t.Fatal(err)
	}
	server.now = func() time.Time { return now }

	heartbeat := model.Heartbeat{
		ProtocolVersion: model.ProtocolVersion,
		AgentID:         "server-1",
		Hostname:        "demo-server",
		OS:              "linux",
		CollectedAt:     now,
	}
	body, _ := json.Marshal(heartbeat)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/agents/heartbeat", bytes.NewReader(body))
	request.Header.Set("Authorization", "Bearer test-secret")
	request.RemoteAddr = "203.0.113.5:12345"
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusAccepted {
		t.Fatalf("heartbeat status = %d, body=%s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, "/api/v1/servers", nil)
	response = httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("list status = %d", response.Code)
	}
	var result struct {
		Servers []model.Server `json:"servers"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Servers) != 1 || result.Servers[0].ObservedIP != "203.0.113.5" || !result.Servers[0].Online {
		t.Fatalf("unexpected servers: %#v", result.Servers)
	}
}

func TestHeartbeatRejectsInvalidToken(t *testing.T) {
	server, _ := NewServer("test-secret", NewStore(time.Minute))
	request := httptest.NewRequest(http.MethodPost, "/api/v1/agents/heartbeat", bytes.NewBufferString("{}"))
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func TestStoreMarksServerOffline(t *testing.T) {
	now := time.Now().UTC()
	store := NewStore(10 * time.Second)
	if err := store.Upsert(context.Background(), model.Heartbeat{AgentID: "one", Hostname: "one", CollectedAt: now}, "127.0.0.1", now); err != nil {
		t.Fatal(err)
	}
	servers, err := store.List(context.Background(), now.Add(11*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if len(servers) != 1 || servers[0].Online {
		t.Fatalf("server should be offline: %#v", servers)
	}
}

func TestHistoryEndpoint(t *testing.T) {
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	store := NewStore(time.Minute)
	if err := store.Upsert(context.Background(), model.Heartbeat{
		AgentID: "server-1", Hostname: "demo", CollectedAt: now,
		Metrics: model.Metrics{CPUPercent: 42.5},
	}, "127.0.0.1", now); err != nil {
		t.Fatal(err)
	}
	server, _ := NewServer("test-secret", store)
	server.now = func() time.Time { return now }
	request := httptest.NewRequest(http.MethodGet, "/api/v1/servers/server-1/metrics", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("history status = %d, body=%s", response.Code, response.Body.String())
	}
	var result struct {
		Samples []model.MetricSample `json:"samples"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Samples) != 1 || result.Samples[0].Metrics.CPUPercent != 42.5 {
		t.Fatalf("unexpected samples: %#v", result.Samples)
	}
}
