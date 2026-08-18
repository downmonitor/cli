package api

import "strings"

// Kind identifies one of the two credential types.
//
// A Checks API key pays for checks and cannot read a workspace; an
// organisation token reads one workspace and cannot run checks. `login` grants
// both. The kind can be told from the token prefix, so the CLI can pick the
// right one without a round trip.
type Kind string

// The credential kinds, as named by the API.
const (
	KindChecks       Kind = "checks"
	KindOrganisation Kind = "organisation"
)

// KindOf returns the kind of token based on its prefix, or "" if the prefix
// is not recognized.
func KindOf(token string) Kind {
	switch {
	case strings.HasPrefix(token, "dm_live_"), strings.HasPrefix(token, "dm_test_"):
		return KindChecks
	case strings.HasPrefix(token, "dmt_"):
		return KindOrganisation
	}

	return ""
}

// Other returns the other credential kind.
func (k Kind) Other() Kind {
	if k == KindChecks {
		return KindOrganisation
	}

	return KindChecks
}

// Describe returns a human-readable name, used in messages.
func (k Kind) Describe() string {
	switch k {
	case KindChecks:
		return "a Checks API key (dm_live_…)"
	case KindOrganisation:
		return "an organisation token (dmt_…)"
	}

	return "a credential"
}

// EnvVar returns the environment variable that holds this kind of credential.
func (k Kind) EnvVar() string {
	if k == KindChecks {
		return "DOWNMONITOR_API_KEY"
	}

	return "DOWNMONITOR_TOKEN"
}

// Commands lists what the credential is used for.
func (k Kind) Commands() string {
	if k == KindChecks {
		return "check and watch"
	}

	return "the workspace API (monitors, incidents)"
}
