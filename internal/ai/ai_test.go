package ai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dirirahmed/pc-sentinel/internal/analyzer"
	"github.com/dirirahmed/pc-sentinel/internal/models"
)

const testKey = "sk-ant-test-key"

func sampleContext() Context {
	return Context{
		System: SystemInfo{CPUModel: "Test CPU", LogicalCores: 8},
		Analysis: analyzer.PerformanceReport{
			Status: "warning",
			Findings: []analyzer.Finding{{
				Rule: "cpu_bottleneck", Severity: models.SeverityWarning, Title: "Possible CPU bottleneck",
				Evidence: "CPU averaged 91% over the last 5 minutes while GPU usage averaged 48%.",
			}},
		},
	}
}

// fakeAnthropic records the last request and replies with status/body.
func fakeAnthropic(t *testing.T, status int, body string, got *apiRequest, headers *http.Header) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/messages" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if headers != nil {
			*headers = r.Header.Clone()
		}
		raw, _ := io.ReadAll(r.Body)
		if got != nil {
			if err := json.Unmarshal(raw, got); err != nil {
				t.Errorf("request body is not JSON: %v", err)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestDisabledWithoutKey(t *testing.T) {
	t.Setenv(EnvAPIKey, "  ")
	c := New(ConfigFromEnv())
	if c.Enabled() {
		t.Fatal("client must be disabled without an API key")
	}
	if _, err := c.Ask(context.Background(), Question{Text: "Why is my PC slow?"}); !errors.Is(err, ErrDisabled) {
		t.Fatalf("err = %v, want ErrDisabled", err)
	}
}

func TestConfigFromEnv(t *testing.T) {
	t.Setenv(EnvAPIKey, testKey)
	t.Setenv(EnvModel, "claude-haiku-4-5")
	c := New(ConfigFromEnv())
	if !c.Enabled() || c.Model() != "claude-haiku-4-5" {
		t.Errorf("enabled=%v model=%q", c.Enabled(), c.Model())
	}
	t.Setenv(EnvModel, "")
	if m := New(ConfigFromEnv()).Model(); m != DefaultModel {
		t.Errorf("default model = %q", m)
	}
	if u := New(ConfigFromEnv()).cfg.BaseURL; u != defaultBaseURL {
		t.Errorf("default base URL = %q", u)
	}
	t.Setenv(EnvBaseURL, "https://gateway.example/anthropic")
	if u := New(ConfigFromEnv()).cfg.BaseURL; u != "https://gateway.example/anthropic" {
		t.Errorf("base URL override = %q", u)
	}
}

func TestAskSendsGroundedRequestAndParsesAnswer(t *testing.T) {
	var got apiRequest
	var hdr http.Header
	srv := fakeAnthropic(t, 200, `{"model":"claude-test","stop_reason":"end_turn","content":[
		{"type":"text","text":"Your CPU is the bottleneck."},{"type":"text","text":"- Close background apps."}]}`, &got, &hdr)
	c := New(Config{APIKey: testKey, Model: "claude-test", BaseURL: srv.URL})

	ans, err := c.Ask(context.Background(), Question{
		Text:    "Is my PC CPU or GPU limited?",
		History: []Turn{{Role: "user", Content: "Why is my PC slow?"}, {Role: "assistant", Content: "CPU load is high."}},
		Data:    sampleContext(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if ans.Text != "Your CPU is the bottleneck.\n\n- Close background apps." || ans.Model != "claude-test" {
		t.Errorf("answer = %+v", ans)
	}
	if hdr.Get("x-api-key") != testKey || hdr.Get("anthropic-version") != apiVersion {
		t.Errorf("auth headers missing: %v", hdr)
	}
	if got.Model != "claude-test" || got.MaxTokens <= 0 || !strings.Contains(got.System, "Never invent measurements") {
		t.Errorf("request = model %q max_tokens %d", got.Model, got.MaxTokens)
	}
	if len(got.Messages) != 3 || got.Messages[0].Role != "user" || got.Messages[1].Role != "assistant" || got.Messages[2].Role != "user" {
		t.Fatalf("messages = %+v", got.Messages)
	}
	last := got.Messages[2].Content
	for _, want := range []string{"<pc_sentinel_data>", "Possible CPU bottleneck", "GPU usage averaged 48%", "Question: Is my PC CPU or GPU limited?"} {
		if !strings.Contains(last, want) {
			t.Errorf("final message missing %q", want)
		}
	}
}

func TestAskMapsAPIErrors(t *testing.T) {
	cases := []struct {
		status int
		body   string
		want   string
	}{
		{401, `{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`, "rejected the API key"},
		{429, `{"type":"error","error":{"type":"rate_limit_error","message":"slow down"}}`, "rate limit"},
		{529, `{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`, "overloaded_error"},
		{500, `not json`, "Anthropic API error 500"},
	}
	for _, tc := range cases {
		srv := fakeAnthropic(t, tc.status, tc.body, nil, nil)
		_, err := New(Config{APIKey: testKey, BaseURL: srv.URL}).Ask(context.Background(), Question{Text: "hi"})
		var apiErr *APIError
		if !errors.As(err, &apiErr) || apiErr.Status != tc.status || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("status %d: err = %v, want it to mention %q", tc.status, err, tc.want)
		}
		if strings.Contains(err.Error(), testKey) {
			t.Error("error message must never contain the API key")
		}
	}
}

func TestAskRejectsEmptyAnswer(t *testing.T) {
	srv := fakeAnthropic(t, 200, `{"content":[]}`, nil, nil)
	if _, err := New(Config{APIKey: testKey, BaseURL: srv.URL}).Ask(context.Background(), Question{Text: "hi"}); err == nil {
		t.Fatal("expected an error for an empty answer")
	}
}

func TestAskMarksTruncatedAnswers(t *testing.T) {
	srv := fakeAnthropic(t, 200, `{"stop_reason":"max_tokens","content":[{"type":"text","text":"Partial"}]}`, nil, nil)
	ans, err := New(Config{APIKey: testKey, BaseURL: srv.URL}).Ask(context.Background(), Question{Text: "hi"})
	if err != nil || !strings.HasSuffix(ans.Text, "(Answer cut short.)") {
		t.Errorf("ans = %+v, err = %v", ans, err)
	}
}

func TestAskHonoursContextTimeout(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-block }))
	defer srv.Close()
	defer close(block)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := New(Config{APIKey: testKey, BaseURL: srv.URL}).Ask(ctx, Question{Text: "hi"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want deadline exceeded", err)
	}
}
