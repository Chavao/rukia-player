package app

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Chavao/rukia-player/internal/auth"
	"github.com/Chavao/rukia-player/internal/ui"
	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/oauth2"
)

func TestConfiguredEnginePreservesMigratedIdentityAcrossCacheChanges(t *testing.T) {
	for _, customCache := range []bool{false, true} {
		t.Run(fmt.Sprintf("custom_cache=%t", customCache), func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			t.Setenv("XDG_CACHE_HOME", "")
			if customCache {
				t.Setenv("XDG_CACHE_HOME", t.TempDir())
			}
			path, err := auth.GetConfigPath()
			if err != nil {
				t.Fatal(err)
			}
			// An actual pre-migration config must keep its other persisted fields.
			if err := os.WriteFile(path, []byte(`{"volume":0,"last_playlist":"old-playlist","token":{"access_token":"saved-token","refresh_token":"saved-refresh"}}`), 0600); err != nil {
				t.Fatal(err)
			}
			cfg, err := auth.LoadConfig()
			if err != nil {
				t.Fatal(err)
			}
			home, err := os.UserHomeDir()
			if err != nil {
				t.Fatal(err)
			}
			// Independent oracle copied from released main, not LegacyDeviceID.
			sum := sha1.Sum([]byte("rukia-player-device-" + filepath.Join(home, ".cache", "rukia", "librespot")))
			want := hex.EncodeToString(sum[:])
			engine, err := configuredEngine(cfg)
			if err != nil {
				t.Fatal(err)
			}
			if engine.DeviceName() != "rukia-player" || cfg.DeviceID != want {
				t.Fatalf("upgrade: name=%q ID=%q want=%q", engine.DeviceName(), cfg.DeviceID, want)
			}
			for range 2 {
				t.Setenv("XDG_CACHE_HOME", t.TempDir())
				loaded, err := auth.LoadConfig()
				if err != nil {
					t.Fatal(err)
				}
				if _, err := configuredEngine(loaded); err != nil {
					t.Fatal(err)
				}
				if loaded.DeviceID != want {
					t.Fatalf("cache relocation replaced migrated ID: got=%q want=%q", loaded.DeviceID, want)
				}
				if loaded.Volume != 0 || loaded.LastPlaylist != "old-playlist" || loaded.Token == nil || loaded.Token.AccessToken != "saved-token" || loaded.Token.RefreshToken != "saved-refresh" {
					t.Fatal("device identity upgrade changed existing config fields")
				}
			}
		})
	}
}

func TestConfiguredEnginePreservesExistingDeviceID(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	cfg := auth.DefaultConfig()
	cfg.DeviceID = strings.Repeat("a", 40)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	path, err := auth.GetConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// A saved identity wins even when both its potential migration inputs change.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	loaded, err := auth.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := configuredEngine(loaded); err != nil {
		t.Fatal(err)
	}
	if loaded.DeviceID != cfg.DeviceID {
		t.Fatal("startup replaced an existing persisted identity")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("startup rewrote an already migrated configuration")
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
