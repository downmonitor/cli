package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/downmonitor/cli/internal/api"
	"github.com/downmonitor/cli/internal/config"
	"github.com/downmonitor/cli/internal/exitcode"
	"github.com/downmonitor/cli/internal/render"
	"github.com/downmonitor/cli/internal/target"
)

// retryDelay is the pause between two attempts of --retries.
const retryDelay = 5 * time.Second

// maxRegions is the largest --count the API accepts.
const maxRegions = 24

// failOnPolicies are the accepted values of --fail-on.
var failOnPolicies = []string{"any-region-down", "all-regions-down", "assertion", "tls-expiring", "never"}

// checkFlags holds the flags shared by `check` and `watch`. The request flags
// follow curl's names, so a curl command translates directly.
type checkFlags struct {
	method    string
	headers   []string
	data      string
	jsonBody  string
	basic     string
	bearer    string
	resolve   string
	follow    bool
	noFollow  bool
	timeout   time.Duration
	ipv4      bool
	ipv6      bool
	insecure  bool
	userAgent string

	regions    []string
	countries  []string
	continents []string
	count      int
	exclude    []string
	confirm    int

	expectStatus  string
	expectBody    []string
	expectMissing []string
	expectJSON    []string
	expectHeader  []string
	maxTTFB       int
	maxTotal      int
	maxP95        int
	tlsMinDays    int

	failOn        string
	failOpen      bool
	retries       int
	save          string
	id            string
	baseline      string
	maxRegression string

	// file holds the project defaults from .downmonitor.yml, used for anything
	// no flag sets.
	file config.File
}

func newCheckCommand(a *app) *cobra.Command {
	f := &checkFlags{}

	cmd := &cobra.Command{
		Use:   "check <target>",
		Short: "Check a URL once, from several regions",
		Long: `Check a URL once from Downmonitor's probe network. The exit code is the
verdict: 0 up, 1 down, 3 when nothing could be checked.

` + whereFromHelp + `

The request flags mirror curl: -X, -H, -d, -u, --resolve, -4/-6, --insecure.
Assertions are evaluated by the probes.`,
		Example: `  # Two regions, picked among those in service
  downmonitor check https://api.example.com/health

  # Named regions, a city, or a country
  downmonitor check https://api.example.com -r cdg,nyc --expect-status 200
  downmonitor check https://api.example.com -r paris,singapore
  downmonitor check https://api.example.com --country FR --count 3

  # In CI: three regions must agree, and a Downmonitor outage does not fail the build
  downmonitor check "$DEPLOY_URL" --count 3 --confirm 3 --max-p95 800 --fail-open`,
		Args: func(_ *cobra.Command, args []string) error {
			if f.id != "" && len(args) == 0 {
				return nil
			}

			if len(args) != 1 {
				return exitcode.Usagef("check takes exactly one target, or --id to fetch a previous check")
			}

			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := f.validate(); err != nil {
				return err
			}

			client, err := a.clientFor(api.KindChecks)
			if err != nil {
				return err
			}

			// --id fetches an existing result, which is free.
			if f.id != "" {
				a.spinner.Start("Fetching " + f.id)

				check, err := client.FetchCheck(cmd.Context(), f.id)
				if err != nil {
					return a.forgive(err, f.failOpen)
				}

				return a.finishCheck(check, f)
			}

			f.file = a.cfg.File

			request, err := f.request(args[0])
			if err != nil {
				return err
			}

			a.spinner.Start(checkingText(request))

			check, err := a.runWithRetries(cmd, client, request, f.retries)
			if err != nil {
				return a.forgive(withWhereHints(err, request, f.failOpen), f.failOpen)
			}

			return a.finishCheck(check, f)
		},
	}

	bindCheckFlags(cmd, f)

	return cmd
}

func bindCheckFlags(cmd *cobra.Command, f *checkFlags) {
	flags := cmd.Flags()
	flags.SortFlags = false

	flags.StringVarP(&f.method, "method", "X", "", "HTTP method: GET, POST, PUT, PATCH, DELETE or HEAD")
	flags.StringArrayVarP(&f.headers, "header", "H", nil, `Request header "Name: value" (repeatable)`)
	flags.StringVarP(&f.data, "data", "d", "", "Request body; @file reads it from a file, @- from stdin")
	flags.StringVar(&f.jsonBody, "json", "", "JSON request body; sets Content-Type and Accept, and defaults to POST")
	flags.StringVarP(&f.basic, "basic", "u", "", "HTTP basic auth as user:password")
	flags.StringVar(&f.bearer, "bearer", "", "Bearer token for the target (default $DOWNMONITOR_BEARER)")
	flags.StringVar(&f.resolve, "resolve", "", "host:port:address to connect to instead of resolving the host")
	flags.BoolVar(&f.follow, "follow", true, "Follow redirects")
	flags.BoolVar(&f.noFollow, "no-follow", false, "Do not follow redirects (same as --follow=false)")
	flags.DurationVar(&f.timeout, "timeout", 0, "How long each region waits for the response")
	flags.BoolVarP(&f.ipv4, "ipv4", "4", false, "Use IPv4 only")
	flags.BoolVarP(&f.ipv6, "ipv6", "6", false, "Use IPv6 only")
	flags.BoolVar(&f.insecure, "insecure", false, "Ignore certificate errors (the certificate is still reported)")
	flags.StringVar(&f.userAgent, "user-agent", "", "User-Agent header to send")

	flags.StringSliceVarP(&f.regions, "regions", "r", nil, "Region codes or cities (cdg, paris), or all, tier1, tier2 (default $DOWNMONITOR_REGIONS)")
	flags.StringSliceVarP(&f.countries, "country", "c", nil, "Pick regions in these countries: FR, US, DE…")
	flags.StringSliceVar(&f.continents, "continent", nil, "Pick regions on these continents: EU, NA, SA, AS, OC, AF")
	flags.IntVarP(&f.count, "count", "n", 0, "Number of regions to pick when --regions is not set (default 2)")
	flags.StringSliceVar(&f.exclude, "exclude", nil, "Regions to leave out")
	flags.IntVar(&f.confirm, "confirm", 0, "Number of regions that must agree before the verdict is down")

	flags.StringVar(&f.expectStatus, "expect-status", "", "Expected status: 200, 2xx, 200-204 or a comma-separated list")
	flags.StringArrayVar(&f.expectBody, "expect-body", nil, "Text the body must contain (repeatable)")
	flags.StringArrayVar(&f.expectMissing, "expect-missing", nil, "Text the body must not contain (repeatable)")
	flags.StringArrayVar(&f.expectJSON, "expect-json", nil, `JSONPath assertion, e.g. '$.status == "ok"' (repeatable)`)
	flags.StringArrayVar(&f.expectHeader, "expect-header", nil, `Response header "Name: value" (repeatable)`)
	flags.IntVar(&f.maxTTFB, "max-ttfb", 0, "Fail a region whose time to first byte exceeds this many ms")
	flags.IntVar(&f.maxTotal, "max-total", 0, "Fail a region whose total time exceeds this many ms")
	flags.IntVar(&f.maxP95, "max-p95", 0, "Fail when the p95 across regions exceeds this many ms")
	flags.IntVar(&f.tlsMinDays, "tls-min-days", 0, "Fail when the certificate expires in fewer days than this")

	flags.StringVar(&f.failOn, "fail-on", "any-region-down", "When to exit 1: "+strings.Join(failOnPolicies, ", "))
	flags.BoolVar(&f.failOpen, "fail-open", false, "Exit 0 with a warning instead of 3 when nothing could be checked")
	flags.IntVar(&f.retries, "retries", 0, "Retry a failing check this many times, 5s apart")
	flags.StringVar(&f.save, "save", "", "Also write the full result as JSON to this file")
	flags.StringVar(&f.id, "id", "", "Fetch a previous check by id instead of running one")
	flags.StringVar(&f.baseline, "baseline", "", "A result saved with --save to compare the p95 against")
	flags.StringVar(&f.maxRegression, "max-regression", "", "Allowed p95 regression over --baseline: 50%, 1.5x or 300ms")

	inSection(flags, sectionWhere, "regions", "country", "continent", "count", "exclude")
	inSection(flags, sectionRequest, "method", "header", "data", "json", "basic", "bearer", "resolve",
		"follow", "no-follow", "timeout", "ipv4", "ipv6", "insecure", "user-agent")
	inSection(flags, sectionAssertions, "expect-status", "expect-body", "expect-missing", "expect-json",
		"expect-header", "max-ttfb", "max-total", "max-p95", "tls-min-days")
	inSection(flags, sectionVerdict, "confirm", "fail-on", "fail-open", "retries", "save", "id",
		"baseline", "max-regression")
}

// whereFromHelp documents region selection for `check` and `watch`.
const whereFromHelp = `Where from
  Without --regions, the check runs from 2 regions picked among those with a
  probe in service: on two continents when possible, and the same ones from
  one run to the next.

    --regions cdg,nyc     these regions: a code, a city (paris), all, tier1, tier2
    --country FR,DE       regions picked in these countries
    --continent EU        regions picked on these continents
    --count 3             how many regions to pick

  ` + "`downmonitor regions`" + ` lists the regions in service. The same settings can
  be stored in a .downmonitor.yml at the root of a repository.`

// validate checks the flags that do not depend on the target, before any
// request is made.
func (f *checkFlags) validate() error {
	if !slices.Contains(failOnPolicies, f.failOn) {
		return exitcode.Usagef("--fail-on must be one of %s", strings.Join(failOnPolicies, ", "))
	}

	if f.failOn == "tls-expiring" && f.tlsMinDays <= 0 {
		return exitcode.Usagef("--fail-on tls-expiring needs --tls-min-days")
	}

	if f.retries < 0 {
		return exitcode.Usagef("--retries cannot be negative")
	}

	if f.maxRegression != "" {
		if f.baseline == "" {
			return exitcode.Usagef("--max-regression needs --baseline")
		}

		if _, err := allowance(0, f.maxRegression); err != nil {
			return exitcode.Usagef("%s", err)
		}
	}

	return nil
}

// withWhereHints adds advice to a "no probe available" error. The right
// advice depends on how the regions were selected, which only this command
// knows.
func withWhereHints(err error, request api.CheckRequest, failOpen bool) error {
	var apiErr *api.Error
	if !errors.As(err, &apiErr) || apiErr.Code != "no_probe_available" {
		return err
	}

	var hints []string

	if available := apiErr.Available(); len(available) > 0 {
		hints = append(hints, "In service now: "+strings.Join(available, ", ")+".")
	}

	switch {
	case request.Regions != nil && request.Regions != "auto":
		hints = append(hints, "Leave out --regions to check from any region in service.")
	case len(request.Countries) > 0 || len(request.Continents) > 0:
		hints = append(hints, "Widen the selection: drop --country or --continent, or run `downmonitor regions`.")
	default:
		hints = append(hints, "Try again in a minute, or run `downmonitor regions`.")
	}

	if !failOpen {
		hints = append(hints, "In CI, --fail-open turns this into a warning.")
	}

	return &hintedError{err: err, hints: hints}
}

// checkingText is the spinner text while a check runs.
func checkingText(request api.CheckRequest) string {
	return "Checking " + request.Target + " " + describeWhere(request)
}

func describeWhere(request api.CheckRequest) string {
	switch regions := request.Regions.(type) {
	case []string:
		return "from " + strings.Join(regions, ", ")
	case string:
		if regions != "auto" {
			return "from " + regions + " regions"
		}
	}

	how := "regions"
	if request.Count > 0 {
		how = plural(request.Count, "region")
	}

	if places := append(append([]string{}, request.Countries...), request.Continents...); len(places) > 0 {
		return "from " + how + " in " + strings.Join(places, ", ")
	}

	return "from " + how + " picked automatically"
}

// request builds the body of POST /checks. Anything left unset is omitted so
// the server defaults apply.
func (f *checkFlags) request(raw string) (api.CheckRequest, error) {
	value, kind := target.Normalize(raw)

	if !kind.Served() {
		if kind == target.Unknown {
			return api.CheckRequest{}, exitcode.Usagef(
				"%q is not a valid target: expected an http:// or https:// URL", raw)
		}

		return api.CheckRequest{}, exitcode.Usagef(
			"%s is %s, which is not supported yet: only http and https targets can be checked", raw, kind.Describe())
	}

	request := api.CheckRequest{
		Target:    value,
		Method:    strings.ToUpper(f.method),
		Confirm:   f.confirm,
		Exclude:   f.exclude,
		Resolve:   f.resolve,
		UserAgent: f.userAgent,
	}

	if err := f.where(&request); err != nil {
		return api.CheckRequest{}, err
	}

	if request.Confirm == 0 {
		request.Confirm = f.file.Confirm
	}

	if len(request.Exclude) == 0 {
		request.Exclude = f.file.Exclude
	}

	headers, err := f.requestHeaders()
	if err != nil {
		return api.CheckRequest{}, err
	}

	body, err := f.body()
	if err != nil {
		return api.CheckRequest{}, err
	}

	request.Body = body

	if f.jsonBody != "" && request.Method == "" {
		request.Method = "POST"
	}

	if len(headers) > 0 {
		request.Headers = headers
	}

	if request.Auth, err = f.auth(); err != nil {
		return api.CheckRequest{}, err
	}

	if request.TimeoutSeconds, err = f.timeoutSeconds(); err != nil {
		return api.CheckRequest{}, err
	}

	if !f.follow || f.noFollow {
		request.FollowRedirects = new(bool)
	}

	if f.insecure {
		request.VerifyTLS = new(bool)
	}

	switch {
	case f.ipv4 && f.ipv6:
		return api.CheckRequest{}, exitcode.Usagef("-4 and -6 are mutually exclusive")
	case f.ipv4:
		request.IPVersion = 4
	case f.ipv6:
		request.IPVersion = 6
	}

	if request.Assertions, err = f.assertions(); err != nil {
		return api.CheckRequest{}, err
	}

	return request, nil
}

// requestHeaders merges the headers from the flags with those of
// .downmonitor.yml. Flags win over the file.
func (f *checkFlags) requestHeaders() (map[string]string, error) {
	headers, err := parseHeaders(f.headers)
	if err != nil {
		return nil, err
	}

	if headers == nil {
		headers = map[string]string{}
	}

	for name, value := range f.file.Headers {
		if !hasHeader(headers, name) {
			headers[name] = value
		}
	}

	if f.jsonBody != "" {
		for _, name := range []string{"Content-Type", "Accept"} {
			if !hasHeader(headers, name) {
				headers[name] = "application/json"
			}
		}
	}

	return headers, nil
}

// hasHeader reports whether headers contains name, case-insensitively.
func hasHeader(headers map[string]string, name string) bool {
	for existing := range headers {
		if strings.EqualFold(existing, name) {
			return true
		}
	}

	return false
}

func (f *checkFlags) timeoutSeconds() (int, error) {
	timeout := f.timeout

	if timeout == 0 && f.file.Timeout != "" {
		parsed, err := time.ParseDuration(f.file.Timeout)
		if err != nil {
			return 0, exitcode.Usagef("%s: timeout %q is not a duration such as 10s", config.FileName, f.file.Timeout)
		}

		timeout = parsed
	}

	if timeout < 0 {
		return 0, exitcode.Usagef("--timeout cannot be negative")
	}

	if timeout > 0 && timeout < time.Second {
		return 0, exitcode.Usagef("--timeout must be at least 1s")
	}

	return int(timeout.Seconds()), nil
}

// where sets the regions of the request, or the constraints the API should
// pick them with. The first source that says anything wins as a whole: the
// flags, then $DOWNMONITOR_REGIONS, then .downmonitor.yml. With none of them,
// the API picks among the regions in service.
func (f *checkFlags) where(request *api.CheckRequest) error {
	regions, countries, continents, count := f.regions, f.countries, f.continents, f.count

	if len(regions) > 0 && (len(countries) > 0 || len(continents) > 0) {
		return exitcode.Usagef("--regions names regions, while --country and --continent narrow an automatic pick.\n  Use one or the other")
	}

	namesRegions := len(regions) > 1 || (len(regions) == 1 && !strings.EqualFold(regions[0], "auto"))
	if namesRegions && count > 0 {
		return exitcode.Usagef("--count sets how many regions to pick, but --regions already names them.\n  Drop one of the two")
	}

	if len(regions) == 0 && len(countries) == 0 && len(continents) == 0 {
		if fromEnv := os.Getenv("DOWNMONITOR_REGIONS"); fromEnv != "" {
			regions = strings.Split(fromEnv, ",")
		} else {
			regions, countries, continents = f.file.Regions, f.file.Countries, f.file.Continents
		}
	}

	if count == 0 && len(regions) == 0 {
		count = f.file.Count
	}

	if count < 0 || count > maxRegions {
		return exitcode.Usagef("--count must be between 1 and %d", maxRegions)
	}

	regions = trimmed(regions)

	switch {
	case len(regions) == 1 && isRegionGroup(regions[0]):
		request.Regions = strings.ToLower(regions[0])
	case len(regions) > 0:
		request.Regions = regions
	}

	for _, country := range trimmed(countries) {
		if len(country) != 2 {
			return exitcode.Usagef("--country takes two-letter codes such as FR, US or DE, not %q", country)
		}

		request.Countries = append(request.Countries, strings.ToUpper(country))
	}

	for _, continent := range trimmed(continents) {
		code, ok := continentCode(continent)
		if !ok {
			return exitcode.Usagef("--continent takes EU, NA, SA, AS, OC or AF (or their names), not %q", continent)
		}

		request.Continents = append(request.Continents, code)
	}

	if request.Regions == nil || request.Regions == "auto" {
		request.Count = count
	}

	return nil
}

func trimmed(values []string) []string {
	out := make([]string, 0, len(values))

	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			out = append(out, value)
		}
	}

	return out
}

var continents = map[string]string{
	"eu": "EU", "europe": "EU",
	"na": "NA", "northamerica": "NA", "america": "NA",
	"sa": "SA", "southamerica": "SA", "latam": "SA",
	"as": "AS", "asia": "AS",
	"oc": "OC", "oceania": "OC", "australia": "OC",
	"af": "AF", "africa": "AF",
}

var continentKey = strings.NewReplacer("-", "", "_", "", " ", "", ".", "")

// continentCode maps a continent code or name to its two-letter code.
func continentCode(value string) (string, bool) {
	code, ok := continents[continentKey.Replace(strings.ToLower(value))]

	return code, ok
}

func isRegionGroup(value string) bool {
	switch strings.ToLower(value) {
	case "all", "tier1", "tier2", "auto":
		return true
	}

	return false
}

// body returns the request body from -d or --json.
func (f *checkFlags) body() (string, error) {
	switch {
	case f.jsonBody != "" && f.data != "":
		return "", exitcode.Usagef("-d and --json cannot be combined")
	case f.jsonBody != "":
		return readValue(f.jsonBody)
	case f.data != "":
		return readValue(f.data)
	}

	return "", nil
}

// readValue returns value, or the content of a file when value is @path, or
// stdin when it is @-.
func readValue(value string) (string, error) {
	path, isFile := strings.CutPrefix(value, "@")
	if !isFile {
		return value, nil
	}

	if path == "-" {
		raw, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", exitcode.Usagef("could not read the body from stdin: %w", err)
		}

		return string(raw), nil
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return "", exitcode.Usagef("could not read the body: %w", err)
	}

	return string(raw), nil
}

// auth returns the authentication for the target, from -u or --bearer. The
// bearer token defaults to $DOWNMONITOR_BEARER so it can stay out of the shell
// history.
func (f *checkFlags) auth() (*api.Auth, error) {
	if f.basic != "" {
		user, password, found := strings.Cut(f.basic, ":")
		if !found {
			return nil, exitcode.Usagef("-u takes user:password")
		}

		return &api.Auth{Type: "basic", Username: user, Password: password}, nil
	}

	token := f.bearer
	if token == "" {
		token = os.Getenv("DOWNMONITOR_BEARER")
	}

	if token == "" {
		return nil, nil
	}

	return &api.Auth{Type: "bearer", Token: token}, nil
}

func (f *checkFlags) assertions() (*api.Assertions, error) {
	assertions := &api.Assertions{
		Status:       f.expectStatus,
		BodyContains: f.expectBody,
		BodyMissing:  f.expectMissing,
		MaxTTFBMs:    f.maxTTFB,
		MaxTotalMs:   f.maxTotal,
		TLSMinDays:   f.tlsMinDays,
	}

	headers, err := parseHeaders(f.expectHeader)
	if err != nil {
		return nil, err
	}

	assertions.Headers = headers

	for _, rule := range f.expectJSON {
		parsed, err := parseJSONAssertion(rule)
		if err != nil {
			return nil, err
		}

		assertions.JSON = append(assertions.JSON, parsed)
	}

	empty := assertions.Status == "" && len(assertions.BodyContains) == 0 && len(assertions.BodyMissing) == 0 &&
		len(assertions.JSON) == 0 && len(assertions.Headers) == 0 &&
		assertions.MaxTTFBMs == 0 && assertions.MaxTotalMs == 0 && assertions.TLSMinDays == 0
	if empty {
		return nil, nil
	}

	return assertions, nil
}

// parseHeaders parses "Name: value" pairs.
func parseHeaders(values []string) (map[string]string, error) {
	if len(values) == 0 {
		return nil, nil
	}

	headers := make(map[string]string, len(values))

	for _, value := range values {
		name, content, found := strings.Cut(value, ":")
		if name = strings.TrimSpace(name); !found || name == "" {
			return nil, exitcode.Usagef(`invalid header %q: expected "Name: value"`, value)
		}

		headers[name] = strings.TrimSpace(content)
	}

	return headers, nil
}

// jsonOperators are the operators of --expect-json. When several match, the
// leftmost wins, and the longest among those starting at the same position.
var jsonOperators = []string{"==", "!=", ">=", "<=", ">", "<", " contains ", " not exists", " exists"}

// parseJSONAssertion parses rules such as `$.order.state == "pending"`,
// `$.items exists` or a bare `$.id`, which means exists.
func parseJSONAssertion(rule string) (api.JSONAssertion, error) {
	trimmed := strings.TrimSpace(rule)
	index, operator := -1, ""

	for _, candidate := range jsonOperators {
		at := strings.Index(trimmed, candidate)
		if at <= 0 {
			continue
		}

		if index == -1 || at < index || (at == index && len(candidate) > len(operator)) {
			index, operator = at, candidate
		}
	}

	if index == -1 {
		if strings.HasPrefix(trimmed, "$") && !strings.ContainsAny(trimmed, " \t") {
			return api.JSONAssertion{Path: trimmed, Op: "exists"}, nil
		}

		return api.JSONAssertion{}, exitcode.Usagef(
			`invalid --expect-json %q: expected a path and a rule, such as '$.status == "ok"' or '$.items exists'`, rule)
	}

	op := strings.TrimSpace(operator)
	assertion := api.JSONAssertion{Path: strings.TrimSpace(trimmed[:index]), Op: op}
	rest := strings.TrimSpace(trimmed[index+len(operator):])

	if !strings.HasPrefix(assertion.Path, "$") {
		return api.JSONAssertion{}, exitcode.Usagef("invalid --expect-json %q: the path must start with $", rule)
	}

	if op == "exists" || op == "not exists" {
		if rest != "" {
			return api.JSONAssertion{}, exitcode.Usagef("invalid --expect-json %q: %s takes no value", rule, op)
		}

		return assertion, nil
	}

	if rest == "" {
		return api.JSONAssertion{}, exitcode.Usagef("invalid --expect-json %q: missing a value after %s", rule, op)
	}

	assertion.Value = jsonValue(rest)

	return assertion, nil
}

// jsonValue keeps the type of the value as written: a quoted string stays a
// string, numbers, booleans and null are decoded, and anything else is sent as
// a string.
func jsonValue(raw string) any {
	if len(raw) >= 2 && (raw[0] == '"' || raw[0] == '\'') && raw[len(raw)-1] == raw[0] {
		return raw[1 : len(raw)-1]
	}

	switch raw {
	case "true":
		return true
	case "false":
		return false
	case "null":
		return nil
	}

	if number, err := strconv.ParseFloat(raw, 64); err == nil {
		return number
	}

	return raw
}

// runWithRetries runs the check, and runs it again up to retries times while
// it does not pass. Only verdicts are retried: an API error is returned at
// once.
func (a *app) runWithRetries(cmd *cobra.Command, client *api.Client, request api.CheckRequest, retries int) (*api.Check, error) {
	for attempt := 0; ; attempt++ {
		check, err := client.Check(cmd.Context(), request)
		if err != nil {
			return nil, err
		}

		if check.Passed || attempt >= retries {
			return check, nil
		}

		a.warn(fmt.Sprintf("%s did not pass (%s), retrying in %s (%d of %d)",
			check.Target, check.Verdict, retryDelay, attempt+1, retries))

		timer := time.NewTimer(retryDelay)

		select {
		case <-cmd.Context().Done():
			timer.Stop()

			return check, nil
		case <-timer.C:
		}

		a.spinner.Start(checkingText(request))
	}
}

// finishCheck prints the result, saves it if requested, and returns the error
// that carries the exit code.
func (a *app) finishCheck(check *api.Check, f *checkFlags) error {
	a.spinner.Stop()

	if err := render.Check(a.stdout, a.output, check, a.renderOptions()); err != nil {
		return err
	}

	if f.save != "" {
		if err := saveCheck(check, f.save); err != nil {
			return err
		}
	}

	return f.verdict(check)
}

func saveCheck(check *api.Check, path string) error {
	encoded, err := json.MarshalIndent(check, "", "  ")
	if err != nil {
		return exitcode.Usagef("could not encode the result: %w", err)
	}

	if err := os.WriteFile(path, append(encoded, '\n'), 0o644); err != nil {
		return exitcode.Usagef("could not save the result: %w", err)
	}

	return nil
}

// verdict applies --fail-on and the client-side thresholds to the result. It
// never overrides the server's verdict; it only decides whether that verdict
// fails the command.
func (f *checkFlags) verdict(check *api.Check) error {
	if err := f.regression(check); err != nil {
		return err
	}

	if f.maxP95 > 0 && check.Summary.P95Ms != nil && *check.Summary.P95Ms > f.maxP95 {
		return exitcode.Verdictf("p95 %dms is above the %dms allowed", *check.Summary.P95Ms, f.maxP95)
	}

	var failed bool

	switch f.failOn {
	case "never":
		failed = false
	case "all-regions-down":
		failed = check.Summary.Measured > 0 && check.Summary.Down == check.Summary.Measured
	case "assertion":
		failed = len(check.FailedAssertions) > 0
	case "tls-expiring":
		failed = check.TLS != nil && check.TLS.DaysLeft != nil && *check.TLS.DaysLeft < f.tlsMinDays
	default:
		// any-region-down: the server's verdict, which already accounts for
		// --confirm and failed assertions.
		failed = !check.Passed
	}

	if !failed {
		return nil
	}

	return exitcode.Verdictf("%s is %s", check.Target, check.Verdict)
}

// regression compares the p95 with the one of a saved baseline. A missing
// baseline file is not an error, so the first run of a pipeline can create it.
func (f *checkFlags) regression(check *api.Check) error {
	if f.baseline == "" || f.maxRegression == "" {
		return nil
	}

	raw, err := os.ReadFile(f.baseline)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}

	if err != nil {
		return exitcode.Usagef("could not read the baseline: %w", err)
	}

	var previous api.Check
	if err := json.Unmarshal(raw, &previous); err != nil {
		return exitcode.Usagef("%s is not a saved check result: %w", f.baseline, err)
	}

	if previous.Summary.P95Ms == nil || check.Summary.P95Ms == nil {
		return nil
	}

	before, now := float64(*previous.Summary.P95Ms), float64(*check.Summary.P95Ms)

	allowed, err := allowance(before, f.maxRegression)
	if err != nil {
		return exitcode.Usagef("%s", err)
	}

	if now <= allowed {
		return nil
	}

	return exitcode.Verdictf("p95 went from %.0fms to %.0fms, more than the %s allowed", before, now, f.maxRegression)
}

// allowance returns the highest acceptable p95 given a baseline and a
// --max-regression of the form 50%, 1.5x or 300ms.
func allowance(baseline float64, spec string) (float64, error) {
	spec = strings.TrimSpace(spec)
	invalid := fmt.Errorf("invalid --max-regression %q: expected a percentage (50%%), a factor (1.5x) or a duration in ms (300ms)", spec)

	parse := func(suffix string) (float64, bool) {
		value, err := strconv.ParseFloat(strings.TrimSuffix(spec, suffix), 64)

		return value, err == nil && value >= 0
	}

	switch {
	case strings.HasSuffix(spec, "%"):
		if percent, ok := parse("%"); ok {
			return baseline * (1 + percent/100), nil
		}
	case strings.HasSuffix(spec, "ms"):
		if extra, ok := parse("ms"); ok {
			return baseline + extra, nil
		}
	case strings.HasSuffix(spec, "x"):
		if factor, ok := parse("x"); ok {
			return baseline * factor, nil
		}
	}

	return 0, invalid
}
