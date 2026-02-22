package nhl

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCareerGoals_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/player/8471214/landing" && r.URL.Path != "/landing" {
			t.Logf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"careerTotals":{"regularSeason":{"goals":919}}}`))
	}))
	defer server.Close()

	c := &Client{
		httpClient: server.Client(),
		baseURL:    server.URL + "/v1/player/8471214/landing",
	}
	ctx := context.Background()

	goals, err := c.CareerGoals(ctx)
	if err != nil {
		t.Fatalf("CareerGoals: %v", err)
	}
	if goals != 919 {
		t.Errorf("goals = %d; want 919", goals)
	}
}

func TestCareerGoals_Non200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("server error"))
	}))
	defer server.Close()

	c := &Client{
		httpClient: server.Client(),
		baseURL:    server.URL,
	}
	ctx := context.Background()

	goals, err := c.CareerGoals(ctx)
	if err == nil {
		t.Fatal("expected error")
	}
	if goals != 0 {
		t.Errorf("goals = %d; want 0 on error", goals)
	}
	if err.Error() != "nhl api status 500: server error" {
		t.Errorf("err = %v", err)
	}
}

func TestCareerGoals_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{invalid`))
	}))
	defer server.Close()

	c := &Client{
		httpClient: server.Client(),
		baseURL:    server.URL,
	}
	ctx := context.Background()

	_, err := c.CareerGoals(ctx)
	if err == nil {
		t.Fatal("expected decode error")
	}
}

func TestNewClient_BaseURL(t *testing.T) {
	c := NewClient()
	if c.baseURL != "https://api-web.nhle.com/v1/player/8471214/landing" {
		t.Errorf("baseURL = %s", c.baseURL)
	}
	if c.httpClient == nil {
		t.Error("httpClient is nil")
	}
}
