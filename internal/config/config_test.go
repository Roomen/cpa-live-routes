package config

import (
	"reflect"
	"testing"
)

func TestDefaults(t *testing.T) {
	cfg, err := Parse(nil)
	if err != nil || !reflect.DeepEqual(cfg, Default()) {
		t.Fatalf("%+v %v", cfg, err)
	}
	if cfg.RetentionMinutes != 30 || cfg.MaxRecords != 5000 || cfg.ActiveWindowMinutes != 10 || cfg.AnimationSpeed != "1x" ||
		cfg.Layout != "tabs" || !cfg.InflightTracking || !cfg.RequireKey || cfg.ReducedAnimation {
		t.Fatalf("defaults: %+v", cfg)
	}
}

func TestClamp(t *testing.T) {
	cases := []struct {
		yaml  string
		check func(Config) bool
	}{
		{"retention_minutes: 0", func(c Config) bool { return c.RetentionMinutes == 1 && c.ActiveWindowMinutes == 1 }},
		{"retention_minutes: 999", func(c Config) bool { return c.RetentionMinutes == 120 }},
		{"max_records: 5", func(c Config) bool { return c.MaxRecords == 1000 }},
		{"max_records: 9999999", func(c Config) bool { return c.MaxRecords == 100000 }},
		{"active_window_minutes: 500\nretention_minutes: 20", func(c Config) bool { return c.ActiveWindowMinutes == 20 }},
		{"active_window_minutes: -4", func(c Config) bool { return c.ActiveWindowMinutes == 1 }},
		{"animation_speed: 3x\nlayout: grid", func(c Config) bool { return c.AnimationSpeed == "1x" && c.Layout == "tabs" }},
		{"animation_speed: 0.5x\nlayout: split\nrequire_key: true\ninflight_tracking: false\nenabled: true\npriority: 3",
			func(c Config) bool {
				return c.AnimationSpeed == "0.5x" && c.Layout == "split" && c.RequireKey && !c.InflightTracking
			}},
		{"api_key_labels:\n  abcdef0123: laptop", func(c Config) bool { return c.APIKeyLabels["abcdef0123"] == "laptop" }},
	}
	for _, c := range cases {
		cfg, err := Parse([]byte(c.yaml))
		if err != nil || !c.check(cfg) {
			t.Errorf("%q -> %+v, %v", c.yaml, cfg, err)
		}
	}
	if _, err := Parse([]byte("max_records: [1")); err == nil {
		t.Error("expected a YAML error")
	}
}

func TestFields(t *testing.T) {
	if got := len(Fields()); got != 8 {
		t.Fatalf("fields = %d", got)
	}
}

func TestLegacyRequireKeyCannotDisableAuthentication(t *testing.T) {
	cfg, err := Parse([]byte("require_key: false"))
	if err != nil || !cfg.RequireKey {
		t.Fatalf("legacy setting must preserve mandatory authentication")
	}
	for _, field := range Fields() {
		if field.Name == "require_key" {
			t.Fatal("deprecated setting exposed in configuration UI")
		}
	}
}
