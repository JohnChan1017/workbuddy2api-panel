package main

import (
	"bytes"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStartupConfigCreationDoesNotLogKey(t *testing.T) {
	t.Setenv("WB2A_API_KEY", "")
	var logs bytes.Buffer
	old := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(old) })
	path := filepath.Join(t.TempDir(), "config.json")
	cfg, err := loadStartupConfig(path, WriteDefault)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.APIKey == "" {
		t.Fatal("generated config must enable authentication")
	}
	if strings.Contains(logs.String(), cfg.APIKey) {
		t.Fatal("startup log contains the generated API key")
	}
	if !strings.Contains(logs.String(), path) {
		t.Fatal("startup log must identify the config file")
	}
}

func TestStartupConfigCreationFailureStopsStartup(t *testing.T) {
	// Even an env key must not hide a config write failure.
	for _, envKey := range []string{"", "test-env-key"} {
		t.Run(envKey, func(t *testing.T) {
			t.Setenv("WB2A_API_KEY", envKey)
			writeErr := errors.New("simulated read-only config directory")
			cfg, err := loadStartupConfig(filepath.Join(t.TempDir(), "missing.json"), func(string) (string, error) {
				return "", writeErr
			})
			if cfg != nil || !errors.Is(err, writeErr) {
				t.Fatalf("must return the write failure, got cfg present=%t err=%v", cfg != nil, err)
			}
		})
	}
}

func TestStartupConfigReloadFailureStopsStartup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	cfg, err := loadStartupConfig(path, func(path string) (string, error) {
		return "", os.WriteFile(path, []byte("invalid json"), 0o600)
	})
	if cfg != nil || err == nil {
		t.Fatal("must not fall back to defaults when reloading fails")
	}
}

func TestStartupConfigPreservesExistingConfig(t *testing.T) {
	t.Setenv("WB2A_API_KEY", "")
	for _, content := range []string{`{"api_key":"existing-test-key"}`, "invalid json"} {
		path := filepath.Join(t.TempDir(), "config.json")
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		cfg, err := loadStartupConfig(path, func(string) (string, error) {
			t.Fatal("must not recreate an existing config")
			return "", nil
		})
		if content == "invalid json" {
			if err == nil {
				t.Fatal("invalid config must fail")
			}
		} else if err != nil || cfg.APIKey != "existing-test-key" {
			t.Fatal("existing config must be loaded unchanged")
		}
	}
}
