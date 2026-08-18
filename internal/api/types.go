package api

// The types below mirror the public API schema. Fields may be added to the API
// over time but are never renamed or removed within a contract version.

// Check is the response of POST /checks, printed unchanged by `-o json`.
type Check struct {
	ID               string          `json:"id"`
	Object           string          `json:"object"`
	Type             string          `json:"type"`
	Target           string          `json:"target"`
	Verdict          string          `json:"verdict"`
	Passed           bool            `json:"passed"`
	CheckedAt        string          `json:"checked_at"`
	BilledChecks     int             `json:"billed_checks"`
	CachedUntil      string          `json:"cached_until"`
	Summary          Summary         `json:"summary"`
	TLS              *TLS            `json:"tls"`
	FailedAssertions []Assertion     `json:"failed_assertions"`
	Request          map[string]any  `json:"request"`
	Regions          []RegionReading `json:"regions"`
}

// Summary aggregates the readings of every region.
type Summary struct {
	Regions     int  `json:"regions"`
	Measured    int  `json:"measured"`
	NotMeasured int  `json:"not_measured"`
	Up          int  `json:"up"`
	Slow        int  `json:"slow"`
	Down        int  `json:"down"`
	Confirm     int  `json:"confirm"`
	P50Ms       *int `json:"p50_ms"`
	P95Ms       *int `json:"p95_ms"`
	Redirects   *int `json:"redirects"`
}

// RegionReading is the result from one region. When Measured is false the
// region produced no reading: it says nothing about the target and is not
// billed.
type RegionReading struct {
	Region          string      `json:"region"`
	CatalogueRegion string      `json:"catalogue_region"`
	Name            string      `json:"name"`
	Country         string      `json:"country"`
	Continent       string      `json:"continent"`
	Network         *string     `json:"network"`
	Measured        bool        `json:"measured"`
	Verdict         *string     `json:"verdict"`
	StatusCode      *int        `json:"status_code"`
	Timings         *Timings    `json:"timings"`
	ServerIP        *string     `json:"server_ip"`
	Protocol        *string     `json:"protocol"`
	FinalURL        *string     `json:"final_url"`
	Redirects       *int        `json:"redirects"`
	TimedOut        bool        `json:"timed_out"`
	Assertions      []Assertion `json:"assertions"`
	TLS             *TLS        `json:"tls"`
	Error           *ReadingErr `json:"error"`
	CheckedAt       *string     `json:"checked_at"`
	SpecVersion     string      `json:"spec_version"`
}

// Timings are the phases of one request, in milliseconds. TTFBMs and
// TotalMs are measured from the start of the request.
type Timings struct {
	DNSMs      *int `json:"dns_ms"`
	TCPMs      *int `json:"tcp_ms"`
	TLSMs      *int `json:"tls_ms"`
	TTFBMs     *int `json:"ttfb_ms"`
	TransferMs *int `json:"transfer_ms"`
	TotalMs    *int `json:"total_ms"`
}

// ReadingErr describes why a request to the target failed, as reported by the
// probe. Code "not_measured" means the region produced no reading.
type ReadingErr struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Assertion is the outcome of one assertion in one region.
type Assertion struct {
	Region   string `json:"region,omitempty"`
	Type     string `json:"type"`
	Expected string `json:"expected"`
	Actual   string `json:"actual"`
	Passed   bool   `json:"passed"`
	Message  string `json:"message,omitempty"`
}

// TLS describes the certificate presented by the target.
type TLS struct {
	Issuer     string `json:"issuer"`
	Subject    string `json:"subject"`
	NotAfter   string `json:"not_after"`
	DaysLeft   *int   `json:"days_left"`
	Version    string `json:"version"`
	CipherName string `json:"cipher"`
}

// CheckRequest is the body of POST /checks. Empty fields are omitted so the
// server defaults apply.
//
// Regions is either a list of region codes or cities, or a group name such as
// "all" or "tier1".
type CheckRequest struct {
	Target          string            `json:"target"`
	Regions         any               `json:"regions,omitempty"`
	Countries       []string          `json:"countries,omitempty"`
	Continents      []string          `json:"continents,omitempty"`
	Count           int               `json:"count,omitempty"`
	Exclude         []string          `json:"exclude,omitempty"`
	Confirm         int               `json:"confirm,omitempty"`
	Method          string            `json:"method,omitempty"`
	Body            string            `json:"body,omitempty"`
	Headers         map[string]string `json:"headers,omitempty"`
	Auth            *Auth             `json:"auth,omitempty"`
	TimeoutSeconds  int               `json:"timeout_seconds,omitempty"`
	FollowRedirects *bool             `json:"follow_redirects,omitempty"`
	VerifyTLS       *bool             `json:"verify_tls,omitempty"`
	IPVersion       int               `json:"ip_version,omitempty"`
	Resolve         string            `json:"resolve,omitempty"`
	UserAgent       string            `json:"user_agent,omitempty"`
	Assertions      *Assertions       `json:"assertions,omitempty"`
}

// Auth is the authentication sent to the target (not to Downmonitor).
type Auth struct {
	Type     string `json:"type"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	Token    string `json:"token,omitempty"`
}

// Assertions are evaluated by the probe against the response.
type Assertions struct {
	Status       string            `json:"status,omitempty"`
	BodyContains []string          `json:"body_contains,omitempty"`
	BodyMissing  []string          `json:"body_missing,omitempty"`
	JSON         []JSONAssertion   `json:"json,omitempty"`
	Headers      map[string]string `json:"headers,omitempty"`
	MaxTTFBMs    int               `json:"max_ttfb_ms,omitempty"`
	MaxTotalMs   int               `json:"max_total_ms,omitempty"`
	TLSMinDays   int               `json:"tls_min_days,omitempty"`
}

// JSONAssertion compares the value at a JSONPath with Value using Op.
type JSONAssertion struct {
	Path  string `json:"path"`
	Op    string `json:"op"`
	Value any    `json:"value,omitempty"`
}

// Region is one entry of GET /regions.
type Region struct {
	Code          string   `json:"code"`
	Region        string   `json:"region"`
	Name          string   `json:"name"`
	Country       string   `json:"country"`
	CountryName   string   `json:"country_name"`
	Continent     string   `json:"continent"`
	ContinentName string   `json:"continent_name"`
	Tier          int      `json:"tier"`
	Probes        int      `json:"probes"`
	Networks      []string `json:"networks"`
	Healthy       bool     `json:"healthy"`
}

// Regions is the response of GET /regions. Default lists the regions a check
// without --regions would currently use; it changes as probes come and go.
type Regions struct {
	Data         []Region `json:"data"`
	Default      []string `json:"default"`
	DefaultCount int      `json:"default_count"`
	Count        int      `json:"count"`
}

// Me is the response of GET /me.
type Me struct {
	Token struct {
		ID         string   `json:"id"`
		Name       string   `json:"name"`
		Type       string   `json:"type"`
		Prefix     string   `json:"prefix"`
		Abilities  []string `json:"abilities"`
		Monitor    *string  `json:"monitor"`
		CreatedAt  *string  `json:"created_at"`
		LastUsedAt *string  `json:"last_used_at"`
		ExpiresAt  *string  `json:"expires_at"`
	} `json:"token"`
	Workspace struct {
		Name string `json:"name"`
	} `json:"workspace"`
	User struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	} `json:"user"`
	Checks *struct {
		Period    string `json:"period"`
		Used      int    `json:"used"`
		Limit     int    `json:"limit"`
		Remaining int    `json:"remaining"`
	} `json:"checks"`
}

// DeviceAuthorization is the response of POST /auth/device.
type DeviceAuthorization struct {
	DeviceCode              string   `json:"device_code"`
	UserCode                string   `json:"user_code"`
	VerificationURI         string   `json:"verification_uri"`
	VerificationURIComplete string   `json:"verification_uri_complete"`
	ExpiresIn               int      `json:"expires_in"`
	Interval                int      `json:"interval"`
	Grants                  []string `json:"grants"`
}

// GrantedToken is one credential issued by a completed login.
type GrantedToken struct {
	Type      Kind     `json:"type"`
	Token     string   `json:"token"`
	Abilities []string `json:"abilities"`
	ExpiresAt *string  `json:"expires_at"`
}

// CollectedToken is the response of POST /auth/device/token once the login is
// approved. Token and Type hold the primary credential; Tokens holds every
// credential the login granted.
type CollectedToken struct {
	Token     string         `json:"token"`
	Type      Kind           `json:"type"`
	Name      string         `json:"name"`
	Tokens    []GrantedToken `json:"tokens"`
	Workspace struct {
		Name string `json:"name"`
	} `json:"workspace"`
	User struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	} `json:"user"`
	ExpiresAt *string `json:"expires_at"`
}

// Granted returns every credential issued by the login. Older servers only
// return the primary one.
func (c *CollectedToken) Granted() []GrantedToken {
	if len(c.Tokens) > 0 {
		return c.Tokens
	}

	return []GrantedToken{{Type: c.Type, Token: c.Token}}
}
