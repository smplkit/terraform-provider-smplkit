package provider

import (
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/types"
	smplkit "github.com/smplkit/go-sdk/v3"
)

func TestJobHTTPConfigFromModel_FullValues(t *testing.T) {
	body := `{"scope":"all"}`
	m := &jobConfigurationModel{
		URL:           types.StringValue("https://api.example.com/run"),
		Method:        types.StringValue("PUT"),
		SuccessStatus: types.StringValue("200"),
		Timeout:       types.Int64Value(45),
		Body:          types.StringValue(body),
		TLSVerify:     types.BoolValue(false),
		CACert:        types.StringValue("-----BEGIN CERT-----"),
		Headers: []jobHeader{
			{Name: types.StringValue("Authorization"), Value: types.StringValue("Bearer x")},
		},
	}
	cfg := jobHTTPConfigFromModel(m)
	if cfg.URL != "https://api.example.com/run" {
		t.Errorf("url: %q", cfg.URL)
	}
	if cfg.Method != smplkit.JobHttpMethodPut {
		t.Errorf("method: %q", cfg.Method)
	}
	if cfg.SuccessStatus != "200" {
		t.Errorf("success_status: %q", cfg.SuccessStatus)
	}
	if cfg.Timeout != 45 {
		t.Errorf("timeout: %d", cfg.Timeout)
	}
	if cfg.Body == nil || *cfg.Body != body {
		t.Errorf("body: %v", cfg.Body)
	}
	if cfg.TlsVerify == nil || *cfg.TlsVerify != false {
		t.Errorf("tls_verify: %v", cfg.TlsVerify)
	}
	if cfg.CaCert == nil || *cfg.CaCert != "-----BEGIN CERT-----" {
		t.Errorf("ca_cert: %v", cfg.CaCert)
	}
	if len(cfg.Headers) != 1 || cfg.Headers["Authorization"] != "Bearer x" {
		t.Errorf("headers: %+v", cfg.Headers)
	}
}

func TestJobHTTPConfigFromModel_UnsetFieldsLeftZero(t *testing.T) {
	// Unknown/null Optional+Computed fields must be left zero so the server
	// applies its own defaults rather than the provider forcing a value.
	m := &jobConfigurationModel{
		URL:           types.StringValue("https://x.test"),
		Method:        types.StringNull(),
		SuccessStatus: types.StringUnknown(),
		Timeout:       types.Int64Unknown(),
		Body:          types.StringNull(),
		TLSVerify:     types.BoolNull(),
		CACert:        types.StringNull(),
	}
	cfg := jobHTTPConfigFromModel(m)
	if cfg.Method != "" {
		t.Errorf("method should be empty, got %q", cfg.Method)
	}
	if cfg.SuccessStatus != "" {
		t.Errorf("success_status should be empty, got %q", cfg.SuccessStatus)
	}
	if cfg.Timeout != 0 {
		t.Errorf("timeout should be zero, got %d", cfg.Timeout)
	}
	if cfg.Body != nil || cfg.TlsVerify != nil || cfg.CaCert != nil {
		t.Errorf("body/tls/ca should be nil: %v %v %v", cfg.Body, cfg.TlsVerify, cfg.CaCert)
	}
	if len(cfg.Headers) != 0 {
		t.Errorf("headers should be empty: %+v", cfg.Headers)
	}
}

func TestJobHTTPConfigFromModel_Nil(t *testing.T) {
	cfg := jobHTTPConfigFromModel(nil)
	if cfg.URL != "" || cfg.Method != "" || len(cfg.Headers) != 0 {
		t.Errorf("nil model should yield zero config: %+v", cfg)
	}
}

func TestJobConfigurationModelFromHTTP_AppliesDocumentedDefaults(t *testing.T) {
	// An otherwise-empty server config lands on the documented defaults so
	// an omitted Optional+Computed field doesn't read back as empty/zero.
	cfg := jobConfigurationModelFromHTTP(smplkit.HttpConfig{URL: "https://x.test"})
	if cfg.Method.ValueString() != "POST" {
		t.Errorf("method default: %q", cfg.Method.ValueString())
	}
	if cfg.SuccessStatus.ValueString() != "2xx" {
		t.Errorf("success_status default: %q", cfg.SuccessStatus.ValueString())
	}
	if cfg.Timeout.ValueInt64() != 30 {
		t.Errorf("timeout default: %d", cfg.Timeout.ValueInt64())
	}
	if cfg.TLSVerify.ValueBool() != true {
		t.Errorf("tls_verify default: %v", cfg.TLSVerify.ValueBool())
	}
	if !cfg.Body.IsNull() || !cfg.CACert.IsNull() {
		t.Errorf("body/ca_cert should be null when absent")
	}
	if len(cfg.Headers) != 0 {
		t.Errorf("headers should be empty")
	}
}

func TestJobConfigurationModelFromHTTP_PreservesConcreteValues(t *testing.T) {
	body := "payload"
	tlsOff := false
	ca := "PEM"
	cfg := jobConfigurationModelFromHTTP(smplkit.HttpConfig{
		URL:           "https://x.test",
		Method:        smplkit.JobHttpMethodGet,
		SuccessStatus: "5xx",
		Timeout:       10,
		Body:          &body,
		TlsVerify:     &tlsOff,
		CaCert:        &ca,
		Headers:       map[string]string{"A": "b"},
	})
	if cfg.Method.ValueString() != "GET" || cfg.SuccessStatus.ValueString() != "5xx" || cfg.Timeout.ValueInt64() != 10 {
		t.Errorf("scalars not preserved: %+v", cfg)
	}
	if cfg.Body.ValueString() != "payload" || cfg.TLSVerify.ValueBool() != false || cfg.CACert.ValueString() != "PEM" {
		t.Errorf("optionals not preserved: body=%q tls=%v ca=%q", cfg.Body.ValueString(), cfg.TLSVerify.ValueBool(), cfg.CACert.ValueString())
	}
	if len(cfg.Headers) != 1 || cfg.Headers[0].Name.ValueString() != "A" || cfg.Headers[0].Value.ValueString() != "b" {
		t.Errorf("headers not preserved: %+v", cfg.Headers)
	}
}

func TestBuildJobOptions(t *testing.T) {
	m := &jobResourceModel{
		Environments: map[string]jobEnvOverride{
			"production": {Enabled: types.BoolValue(true)},
			"staging":    {Enabled: types.BoolValue(false)},
		},
		Description:       types.StringValue("nightly warm"),
		RetryPolicy:       types.StringValue("aggressive-retry"),
		ConcurrencyPolicy: types.StringValue("ALLOW"),
	}
	job := &smplkit.Job{}
	for _, opt := range buildJobOptions(m) {
		opt(job)
	}
	if !job.Environments["production"].Enabled {
		t.Errorf("production enablement option not applied: %v", job.Environments)
	}
	if job.Environments["staging"].Enabled {
		t.Errorf("staging should be disabled: %v", job.Environments)
	}
	if job.Description == nil || *job.Description != "nightly warm" {
		t.Errorf("description option not applied: %v", job.Description)
	}
	if job.RetryPolicy != "aggressive-retry" {
		t.Errorf("retry_policy option not applied: %q", job.RetryPolicy)
	}
	if job.ConcurrencyPolicy != "ALLOW" {
		t.Errorf("concurrency_policy option not applied: %q", job.ConcurrencyPolicy)
	}
}

func TestBuildJobOptions_OmitsNullDescriptionAndEmptyEnvironments(t *testing.T) {
	m := &jobResourceModel{
		Description:       types.StringNull(),
		RetryPolicy:       types.StringNull(),
		ConcurrencyPolicy: types.StringValue("ALLOW"),
	}
	// A null description must leave the SDK default (nil) intact rather than
	// setting an empty string, and an absent environments map must not emit
	// a WithJobEnvironments option.
	job := &smplkit.Job{}
	for _, opt := range buildJobOptions(m) {
		opt(job)
	}
	if job.Description != nil {
		t.Errorf("null description should not be applied, got %v", *job.Description)
	}
	if job.RetryPolicy != "" {
		t.Errorf("null retry_policy should not be applied, got %q", job.RetryPolicy)
	}
	if job.Environments != nil {
		t.Errorf("absent environments map should not be applied, got %v", job.Environments)
	}
}

func TestJobEnvironmentsFromModel(t *testing.T) {
	envs := jobEnvironmentsFromModel(map[string]jobEnvOverride{
		"production": {
			Enabled:       types.BoolValue(true),
			Schedule:      types.StringValue("*/15 * * * *"),
			Timezone:      types.StringValue("America/New_York"),
			RetryPolicy:   types.StringValue("aggressive-retry"),
			Configuration: &jobConfigurationModel{URL: types.StringValue("https://prod.test")},
		},
		"staging": {Enabled: types.BoolValue(false), Schedule: types.StringNull(), Timezone: types.StringNull(), RetryPolicy: types.StringNull()},
	})
	if !envs["production"].Enabled {
		t.Errorf("production should be enabled: %+v", envs)
	}
	if envs["production"].Schedule != "*/15 * * * *" {
		t.Errorf("production per-env schedule override not converted: %q", envs["production"].Schedule)
	}
	if envs["production"].Timezone != "America/New_York" {
		t.Errorf("production per-env timezone override not converted: %q", envs["production"].Timezone)
	}
	if envs["production"].RetryPolicy != "aggressive-retry" {
		t.Errorf("production per-env retry_policy override not converted: %q", envs["production"].RetryPolicy)
	}
	// Per-environment overrides are flat leaves since ADR-056; the nested config
	// model flattens onto the environment's URL leaf.
	if envs["production"].URL != "https://prod.test" {
		t.Errorf("production config override not converted: %+v", envs["production"])
	}
	if envs["staging"].Enabled || envs["staging"].URL != "" {
		t.Errorf("staging should be disabled with no override: %+v", envs["staging"])
	}
	if envs["staging"].Schedule != "" {
		t.Errorf("staging null schedule should convert to empty (inherit base): %q", envs["staging"].Schedule)
	}
	if envs["staging"].Timezone != "" {
		t.Errorf("staging null timezone should convert to empty (inherit base): %q", envs["staging"].Timezone)
	}
	if envs["staging"].RetryPolicy != "" {
		t.Errorf("staging null retry_policy should convert to empty (inherit base): %q", envs["staging"].RetryPolicy)
	}
	if jobEnvironmentsFromModel(nil) != nil {
		t.Error("nil model map should convert to nil")
	}
}

func TestApplyJobToModel(t *testing.T) {
	desc := "d"
	ver := 3
	kind := smplkit.JobKindRecurring
	nextRun := time.Date(2026, 7, 1, 2, 0, 0, 0, time.UTC)
	job := &smplkit.Job{
		ID:                "nightly",
		Name:              "Nightly",
		Description:       &desc,
		Kind:              &kind,
		Type:              "http",
		Schedule:          "0 2 * * *",
		Timezone:          "Europe/London",
		RetryPolicy:       "aggressive-retry",
		ConcurrencyPolicy: "ALLOW",
		Environments: map[string]*smplkit.JobEnvironment{
			"production": {
				Enabled:     true,
				Schedule:    "*/15 * * * *",
				Timezone:    "America/New_York",
				RetryPolicy: "prod-retry",
				URL:         "https://prod.test",
				NextRunAt:   &nextRun,
			},
			"staging": {Enabled: false},
		},
		Configuration: smplkit.HttpConfig{URL: "https://x.test", Method: smplkit.JobHttpMethodPost},
		Version:       &ver,
	}
	var m jobResourceModel
	applyJobToModel(job, &m)
	if m.ID.ValueString() != "nightly" || m.Name.ValueString() != "Nightly" || m.Description.ValueString() != "d" {
		t.Errorf("scalars: %+v", m)
	}
	if m.Enabled.ValueBool() != true || m.Kind.ValueString() != "recurring" || m.Type.ValueString() != "http" || m.ConcurrencyPolicy.ValueString() != "ALLOW" {
		t.Errorf("flags: %+v", m)
	}
	if m.Schedule.ValueString() != "0 2 * * *" || m.Timezone.ValueString() != "Europe/London" {
		t.Errorf("base schedule/timezone not projected: schedule=%q timezone=%q", m.Schedule.ValueString(), m.Timezone.ValueString())
	}
	if m.RetryPolicy.ValueString() != "aggressive-retry" {
		t.Errorf("base retry_policy not projected: %q", m.RetryPolicy.ValueString())
	}
	if m.Configuration == nil || m.Configuration.URL.ValueString() != "https://x.test" {
		t.Errorf("configuration: %+v", m.Configuration)
	}
	prod, ok := m.Environments["production"]
	if !ok || !prod.Enabled.ValueBool() {
		t.Errorf("production environment not projected: %+v", m.Environments)
	}
	if prod.Configuration == nil || prod.Configuration.URL.ValueString() != "https://prod.test" {
		t.Errorf("production config override not projected: %+v", prod.Configuration)
	}
	if prod.Schedule.ValueString() != "*/15 * * * *" {
		t.Errorf("production per-env schedule not projected: %q", prod.Schedule.ValueString())
	}
	if prod.Timezone.ValueString() != "America/New_York" {
		t.Errorf("production per-env timezone not projected: %q", prod.Timezone.ValueString())
	}
	if prod.RetryPolicy.ValueString() != "prod-retry" {
		t.Errorf("production per-env retry_policy not projected: %q", prod.RetryPolicy.ValueString())
	}
	if prod.NextRunAt.ValueString() != "2026-07-01T02:00:00Z" {
		t.Errorf("production per-env next_run_at not projected: %q", prod.NextRunAt.ValueString())
	}
	// An environment with no per-env schedule / next-run must project to null,
	// not an empty string, so it round-trips cleanly against an omitted config.
	stg, ok := m.Environments["staging"]
	if !ok {
		t.Fatalf("staging environment not projected: %+v", m.Environments)
	}
	if !stg.Schedule.IsNull() {
		t.Errorf("staging schedule should be null when unset, got %q", stg.Schedule.ValueString())
	}
	if !stg.Timezone.IsNull() {
		t.Errorf("staging timezone should be null when unset, got %q", stg.Timezone.ValueString())
	}
	if !stg.RetryPolicy.IsNull() {
		t.Errorf("staging retry_policy should be null when unset, got %q", stg.RetryPolicy.ValueString())
	}
	if !stg.NextRunAt.IsNull() {
		t.Errorf("staging next_run_at should be null when unset, got %q", stg.NextRunAt.ValueString())
	}
	if m.Version.ValueInt64() != 3 {
		t.Errorf("version: %d", m.Version.ValueInt64())
	}
}

// newTestJobResource builds a jobResource backed by a real (offline) SmplClient.
// NewClient does no network I/O, so this is hermetic; newJobFromSchedule only
// constructs an unsaved Job and never calls Save.
func newTestJobResource(t *testing.T) *jobResource {
	t.Helper()
	client, err := smplkit.NewClient(smplkit.Config{APIKey: "test-key"})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return &jobResource{client: client}
}

// kindFromConstructedSchedule classifies the SDK constructor a given
// `job.Schedule` string came from. `Job.Kind` is server-derived and nil on an
// unsaved job, so on the construction path the schedule string is the signal:
// empty → manual (NewManualJob), an RFC3339 datetime → one-off (Schedule), and
// anything else → recurring (NewRecurringJob). This mirrors how the server
// later derives Kind, letting the test assert which constructor was chosen.
func kindFromConstructedSchedule(schedule string) smplkit.JobKind {
	if schedule == "" {
		return smplkit.JobKindManual
	}
	if _, err := time.Parse(time.RFC3339, schedule); err == nil {
		return smplkit.JobKindOneOff
	}
	return smplkit.JobKindRecurring
}

func TestNewJobFromSchedule(t *testing.T) {
	cfg := smplkit.HttpConfig{URL: "https://x.test"}
	r := newTestJobResource(t)

	cases := []struct {
		name         string
		schedule     types.String
		timezone     types.String
		wantKind     smplkit.JobKind
		wantSchedule string // exact match; "" means only the resolved kind is asserted (one-off `now`)
		wantTimezone string // expected job.Timezone; "" means unset/empty
	}{
		{
			name:         "null schedule yields a manual job",
			schedule:     types.StringNull(),
			wantKind:     smplkit.JobKindManual,
			wantSchedule: "",
		},
		{
			name:         "unknown schedule yields a manual job",
			schedule:     types.StringUnknown(),
			wantKind:     smplkit.JobKindManual,
			wantSchedule: "",
		},
		{
			name:         "empty schedule yields a manual job",
			schedule:     types.StringValue(""),
			wantKind:     smplkit.JobKindManual,
			wantSchedule: "",
		},
		{
			name:         "cron schedule yields a recurring job",
			schedule:     types.StringValue("0 2 * * *"),
			wantKind:     smplkit.JobKindRecurring,
			wantSchedule: "0 2 * * *",
		},
		{
			name:         "recurring job carries a base timezone when set",
			schedule:     types.StringValue("0 2 * * *"),
			timezone:     types.StringValue("America/New_York"),
			wantKind:     smplkit.JobKindRecurring,
			wantSchedule: "0 2 * * *",
			wantTimezone: "America/New_York",
		},
		{
			name:         "null timezone leaves the job timezone empty (UTC)",
			schedule:     types.StringValue("0 2 * * *"),
			timezone:     types.StringNull(),
			wantKind:     smplkit.JobKindRecurring,
			wantSchedule: "0 2 * * *",
			wantTimezone: "",
		},
		{
			name:         "rfc3339 datetime yields a one-off job",
			schedule:     types.StringValue("2030-01-02T03:04:05Z"),
			wantKind:     smplkit.JobKindOneOff,
			wantSchedule: "2030-01-02T03:04:05Z",
		},
		{
			name:         "literal now yields a one-off job",
			schedule:     types.StringValue("now"),
			wantKind:     smplkit.JobKindOneOff,
			wantSchedule: "", // resolved to time.Now(), not compared verbatim
		},
		{
			name:         "case-insensitive NOW yields a one-off job",
			schedule:     types.StringValue("NOW"),
			wantKind:     smplkit.JobKindOneOff,
			wantSchedule: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := &jobResourceModel{
				ID:       types.StringValue("job-id"),
				Name:     types.StringValue("Job"),
				Schedule: tc.schedule,
				Timezone: tc.timezone,
			}
			job := r.newJobFromSchedule(data, cfg)
			if job.ID != "job-id" || job.Name != "Job" {
				t.Errorf("id/name not threaded: id=%q name=%q", job.ID, job.Name)
			}
			if job.Configuration.URL != "https://x.test" {
				t.Errorf("configuration not threaded: %+v", job.Configuration)
			}
			// Job.Kind is server-derived (nil on an unsaved job), so classify by
			// the schedule string the constructor produced.
			if got := kindFromConstructedSchedule(job.Schedule); got != tc.wantKind {
				t.Errorf("constructed kind = %q (schedule=%q), want %q", got, job.Schedule, tc.wantKind)
			}
			if tc.wantSchedule != "" && job.Schedule != tc.wantSchedule {
				t.Errorf("schedule = %q, want %q", job.Schedule, tc.wantSchedule)
			}
			if job.Timezone != tc.wantTimezone {
				t.Errorf("timezone = %q, want %q", job.Timezone, tc.wantTimezone)
			}
		})
	}
}
