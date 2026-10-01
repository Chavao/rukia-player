package app

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Chavao/rukia-player/internal/auth"
	"github.com/Chavao/rukia-player/internal/player"
	"github.com/Chavao/rukia-player/internal/ui"
	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/oauth2"
)

func TestConfiguredEnginePreservesMigratedIdentityAcrossCacheChanges(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	cfg := auth.DefaultConfig()
	legacyID := player.LegacyDeviceID()
	engine, err := configuredEngine(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if engine.DeviceName() != "rukia" {
		t.Fatalf("device name = %q", engine.DeviceName())
	}
	loaded, err := auth.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.DeviceID != legacyID {
		t.Fatalf("migrated ID = %q, want %q", loaded.DeviceID, legacyID)
	}
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	if player.LegacyDeviceID() == legacyID {
		t.Fatal("test cache change did not change legacy identity")
	}
	if _, err := configuredEngine(loaded); err != nil {
		t.Fatal(err)
	}
	reloaded, err := auth.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.DeviceID != legacyID {
		t.Fatalf("cache change replaced migrated ID: %q", reloaded.DeviceID)
	}
}

func TestConfiguredEngineRejectsIdentityPersistenceFailure(t *testing.T) {
	blocked := filepath.Join(t.TempDir(), "config-file")
	if err := os.WriteFile(blocked, nil, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", blocked)
	cfg := auth.DefaultConfig()
	engine, err := configuredEngine(cfg)
	if err == nil || engine != nil || cfg.DeviceID != "" {
		t.Fatalf("failed migration: engine=%v err=%v ID=%q", engine, err, cfg.DeviceID)
	}
}

type tokenTransport func(*http.Request) (*http.Response, error)

func (f tokenTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

// warningObserver stops the actual Bubble Tea loop once the warning has passed
// through Update and is visible in the application's model.
type warningObserver struct {
	*ui.Model
	displayed bool
}

func (m *warningObserver) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	_, cmd := m.Model.Update(msg)
	if strings.Contains(m.View(), "failed to save refreshed Spotify token") {
		m.displayed = true
		return m, tea.Quit
	}
	return m, cmd
}

func TestRuntimeOAuthPersistenceWarningReachesUI(t *testing.T) {
	blocked := filepath.Join(t.TempDir(), "config-file")
	if err := os.WriteFile(blocked, nil, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", blocked)
	cfg := auth.DefaultConfig()
	cfg.Token = &oauth2.Token{AccessToken: "expired", RefreshToken: "refresh", Expiry: time.Now().Add(-time.Hour)}
	flow := auth.NewOAuthFlow(cfg)
	calls := 0
	httpClient := &http.Client{Transport: tokenTransport(func(req *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"access_token":"refreshed","token_type":"Bearer","expires_in":3600}`)),
			Request:    req,
		}, nil
	})}
	ctx, cancel := context.WithTimeout(context.WithValue(context.Background(), oauth2.HTTPClient, httpClient), 3*time.Second)
	defer cancel()
	_, source := flow.Client(ctx, cfg.CurrentToken())
	if token, err := source.Token(); err != nil || token.AccessToken != "refreshed" {
		t.Fatalf("runtime refresh: token=%v err=%v", token, err)
	}
	if calls != 1 {
		t.Fatalf("token endpoint calls = %d, want 1", calls)
	}
	// The warning was emitted before the TUI existed and must still be delivered.
	model := ui.NewModel(nil, nil, nil, nil, "")
	model.SetWarningChannel(ctx, flow.Warnings())
	model.Update(tea.WindowSizeMsg{Width: 240, Height: 24})
	observer := &warningObserver{Model: model}
	program := tea.NewProgram(observer, tea.WithContext(ctx), tea.WithInput(nil), tea.WithOutput(io.Discard), tea.WithoutRenderer())
	if _, err := program.Run(); err != nil {
		t.Fatal(err)
	}
	if !observer.displayed {
		t.Fatal("token persistence warning did not appear in the UI")
	}
	var reminder bytes.Buffer
	if err := printWarning(&reminder, flow.LatestWarning()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reminder.String(), "failed to save refreshed Spotify token") {
		t.Fatal("UI delivery consumed the final persistence reminder")
	}
}
