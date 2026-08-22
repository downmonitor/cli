// Package config resolves settings and credentials once, at start-up.
//
// Precedence is always: command-line flag, then environment variable, then the
// OS keychain (for credentials) or .downmonitor.yml (for defaults). Commands
// never read the environment themselves, which keeps them easy to test.
//
// Credentials are resolved per kind, because a command needs a specific one:
// `check` cannot run with an organisation token, and detecting that locally
// gives a clearer error than a 403 from the API.
package config

import (
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/downmonitor/cli/internal/api"
	"github.com/downmonitor/cli/internal/keychain"
)

// DefaultBaseURL is the production API. DOWNMONITOR_API_URL or --api-url
// override it.
const DefaultBaseURL = "https://app.downmonitor.io/api/v1"

// FileName is the per-project defaults file, looked up from the working
// directory upwards. It is meant to be committed and never holds credentials.
const FileName = ".downmonitor.yml"

// File is the content of .downmonitor.yml.
type File struct {
	Regions    []string          `yaml:"regions"`
	Countries  []string          `yaml:"countries"`
	Continents []string          `yaml:"continents"`
	Count      int               `yaml:"count"`
	Exclude    []string          `yaml:"exclude"`
	Confirm    int               `yaml:"confirm"`
	Timeout    string            `yaml:"timeout"`
	Headers    map[string]string `yaml:"headers"`
	APIURL     string            `yaml:"api_url"`
}

// Credential is a token and a description of where it was found, used in
// error messages.
type Credential struct {
	Token  string
	Source string
}

// Lookup reads a token from the credential store.
type Lookup func(account string) (string, error)

// Config holds everything resolved at start-up.
type Config struct {
	BaseURL string
	Profile string
	File    File

	flag   string
	lookup Lookup
}

// Load resolves the configuration. tokenFlag and profileFlag are the values of
// --key and --profile. lookup reads the keychain; it is injected so tests never
// touch the real one.
func Load(tokenFlag, profileFlag string, lookup Lookup) Config {
	cfg := Config{flag: tokenFlag, lookup: lookup}

	cfg.File = findFile()
	cfg.Profile = firstNonEmpty(profileFlag, os.Getenv("DOWNMONITOR_PROFILE"), "default")
	cfg.BaseURL = firstNonEmpty(os.Getenv("DOWNMONITOR_API_URL"), cfg.File.APIURL, DefaultBaseURL)

	return cfg
}

// For returns the credential of the given kind: --key, then the kind's
// environment variable, then the keychain entry of the current profile.
func (c Config) For(kind api.Kind) (Credential, bool) {
	// Tokens are trimmed: a key pasted with a trailing newline would otherwise
	// produce an invalid Authorization header.
	if flag := strings.TrimSpace(c.flag); flag != "" {
		return Credential{Token: flag, Source: "the --key flag"}, true
	}

	if fromEnv := strings.TrimSpace(os.Getenv(kind.EnvVar())); fromEnv != "" {
		return Credential{Token: fromEnv, Source: kind.EnvVar()}, true
	}

	if c.lookup == nil {
		return Credential{}, false
	}

	if token, err := c.lookup(keychain.Account(c.Profile, string(kind))); err == nil && token != "" {
		return Credential{Token: token, Source: "the stored login"}, true
	}

	// Early versions stored a single entry named after the profile. Use it if
	// its prefix matches the requested kind.
	if token, err := c.lookup(keychain.Account(c.Profile, "")); err == nil && api.KindOf(token) == kind {
		return Credential{Token: token, Source: "the stored login"}, true
	}

	return Credential{}, false
}

// Any returns the first available credential, preferring a Checks API key.
// It is used by commands that accept either kind, such as `regions`.
func (c Config) Any() (api.Kind, Credential, bool) {
	for _, kind := range []api.Kind{api.KindChecks, api.KindOrganisation} {
		if credential, ok := c.For(kind); ok {
			return kind, credential, true
		}
	}

	return "", Credential{}, false
}

// findFile looks for .downmonitor.yml in the working directory and its
// parents, so commands run from a subdirectory still pick up the project
// defaults. A file that cannot be parsed is ignored.
func findFile() File {
	dir, err := os.Getwd()
	if err != nil {
		return File{}
	}

	for {
		if raw, err := os.ReadFile(filepath.Join(dir, FileName)); err == nil {
			var file File
			if yaml.Unmarshal(raw, &file) != nil {
				return File{}
			}

			return file
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return File{}
		}

		dir = parent
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}

	return ""
}
