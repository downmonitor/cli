package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/downmonitor/cli/internal/api"
)

func noKeychain(string) (string, error) { return "", errors.New("none") }

// keychainWith returns a Lookup backed by a map of account to token.
func keychainWith(entries map[string]string) func(string) (string, error) {
	return func(account string) (string, error) {
		if token, ok := entries[account]; ok {
			return token, nil
		}

		return "", errors.New("not found")
	}
}

func clearEnvironment(t *testing.T) {
	t.Helper()

	t.Setenv("DOWNMONITOR_API_KEY", "")
	t.Setenv("DOWNMONITOR_TOKEN", "")
	t.Setenv("DOWNMONITOR_PROFILE", "")
	t.Setenv("DOWNMONITOR_API_URL", "")
}

func TestCredentialPrecedence(t *testing.T) {
	clearEnvironment(t)
	t.Setenv("DOWNMONITOR_API_KEY", "dm_live_environment")

	stored := keychainWith(map[string]string{"default:checks": "dm_live_keychain"})

	credential, ok := Load("dm_live_flag", "", stored).For(api.KindChecks)
	if !ok || credential.Token != "dm_live_flag" || credential.Source != "the --key flag" {
		t.Errorf("token = %q from %q", credential.Token, credential.Source)
	}

	credential, ok = Load("", "", stored).For(api.KindChecks)
	if !ok || credential.Token != "dm_live_environment" || credential.Source != "DOWNMONITOR_API_KEY" {
		t.Errorf("token = %q from %q", credential.Token, credential.Source)
	}

	t.Setenv("DOWNMONITOR_API_KEY", "")

	credential, ok = Load("", "", stored).For(api.KindChecks)
	if !ok || credential.Token != "dm_live_keychain" || credential.Source != "the stored login" {
		t.Errorf("token = %q from %q", credential.Token, credential.Source)
	}
}

func TestEachKindHasItsOwnSource(t *testing.T) {
	clearEnvironment(t)
	t.Setenv("DOWNMONITOR_API_KEY", "dm_live_environment")
	t.Setenv("DOWNMONITOR_TOKEN", "dmt_environment")

	cfg := Load("", "", keychainWith(map[string]string{
		"default:checks":       "dm_live_keychain",
		"default:organisation": "dmt_keychain",
	}))

	checks, _ := cfg.For(api.KindChecks)
	organisation, _ := cfg.For(api.KindOrganisation)

	if checks.Token != "dm_live_environment" || organisation.Token != "dmt_environment" {
		t.Errorf("checks = %q, organisation = %q", checks.Token, organisation.Token)
	}

	clearEnvironment(t)

	checks, _ = cfg.For(api.KindChecks)
	organisation, _ = cfg.For(api.KindOrganisation)

	if checks.Token != "dm_live_keychain" || organisation.Token != "dmt_keychain" {
		t.Errorf("checks = %q, organisation = %q", checks.Token, organisation.Token)
	}
}

func TestMissingKindDoesNotFallBackToTheOther(t *testing.T) {
	clearEnvironment(t)

	// Only an organisation token is stored: it must not be returned as a
	// Checks API key.
	cfg := Load("", "", keychainWith(map[string]string{"default:organisation": "dmt_keychain"}))

	if _, ok := cfg.For(api.KindChecks); ok {
		t.Error("a Checks API key was found where there is none")
	}

	if _, ok := cfg.For(api.KindOrganisation); !ok {
		t.Error("the organisation token was not found")
	}
}

func TestLegacyKeychainEntry(t *testing.T) {
	clearEnvironment(t)

	// Legacy single entry: used only for the kind its prefix matches.
	cfg := Load("", "", keychainWith(map[string]string{"default": "dmt_from-the-old-shape"}))

	if credential, ok := cfg.For(api.KindOrganisation); !ok || credential.Token != "dmt_from-the-old-shape" {
		t.Errorf("the old entry was not read: %q", credential.Token)
	}

	if _, ok := cfg.For(api.KindChecks); ok {
		t.Error("an organisation token was offered as a Checks API key")
	}
}

func TestAnyPrefersChecksKey(t *testing.T) {
	clearEnvironment(t)

	cfg := Load("", "", keychainWith(map[string]string{
		"default:checks":       "dm_live_keychain",
		"default:organisation": "dmt_keychain",
	}))

	kind, credential, ok := cfg.Any()
	if !ok || kind != api.KindChecks || credential.Token != "dm_live_keychain" {
		t.Errorf("Any = %q, %q", kind, credential.Token)
	}

	cfg = Load("", "", keychainWith(map[string]string{"default:organisation": "dmt_keychain"}))

	kind, credential, ok = cfg.Any()
	if !ok || kind != api.KindOrganisation || credential.Token != "dmt_keychain" {
		t.Errorf("Any = %q, %q", kind, credential.Token)
	}
}

func TestDefaultsWithoutCredentials(t *testing.T) {
	clearEnvironment(t)

	cfg := Load("", "", noKeychain)

	if _, ok := cfg.For(api.KindChecks); ok {
		t.Error("a credential was found where there is none")
	}

	if cfg.BaseURL != DefaultBaseURL {
		t.Errorf("BaseURL = %q", cfg.BaseURL)
	}

	if cfg.Profile != "default" {
		t.Errorf("Profile = %q", cfg.Profile)
	}
}

func TestProfileFromFlagOrEnvironment(t *testing.T) {
	clearEnvironment(t)
	t.Setenv("DOWNMONITOR_PROFILE", "staging")

	if cfg := Load("", "", noKeychain); cfg.Profile != "staging" {
		t.Errorf("Profile = %q", cfg.Profile)
	}

	if cfg := Load("", "production", noKeychain); cfg.Profile != "production" {
		t.Errorf("Profile = %q", cfg.Profile)
	}
}

func TestProfileSelectsTheKeychainEntry(t *testing.T) {
	clearEnvironment(t)

	cfg := Load("", "staging", keychainWith(map[string]string{
		"default:checks": "dm_live_default",
		"staging:checks": "dm_live_staging",
	}))

	if credential, _ := cfg.For(api.KindChecks); credential.Token != "dm_live_staging" {
		t.Errorf("token = %q, want the staging one", credential.Token)
	}
}

func TestFileIsFoundInParentDirectory(t *testing.T) {
	clearEnvironment(t)

	root := t.TempDir()
	deep := filepath.Join(root, "services", "api")

	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}

	contents := "regions:\n  - cdg\n  - nyc\ntimeout: 20s\napi_url: https://staging.example/api/v1\n"

	if err := os.WriteFile(filepath.Join(root, FileName), []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Chdir(deep)

	cfg := Load("", "", noKeychain)

	if len(cfg.File.Regions) != 2 || cfg.File.Regions[0] != "cdg" {
		t.Errorf("regions = %v", cfg.File.Regions)
	}

	// The environment wins over the file.
	if cfg.BaseURL != "https://staging.example/api/v1" {
		t.Errorf("BaseURL = %q", cfg.BaseURL)
	}

	t.Setenv("DOWNMONITOR_API_URL", "https://from-environment/api/v1")

	if cfg := Load("", "", noKeychain); cfg.BaseURL != "https://from-environment/api/v1" {
		t.Errorf("BaseURL = %q, the environment must win", cfg.BaseURL)
	}
}

func TestInvalidFileIsIgnored(t *testing.T) {
	clearEnvironment(t)

	root := t.TempDir()

	if err := os.WriteFile(filepath.Join(root, FileName), []byte("regions: [oops\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Chdir(root)

	cfg := Load("", "", noKeychain)

	if len(cfg.File.Regions) != 0 {
		t.Errorf("a broken file was read: %v", cfg.File.Regions)
	}

	if cfg.BaseURL != DefaultBaseURL {
		t.Errorf("BaseURL = %q", cfg.BaseURL)
	}
}
