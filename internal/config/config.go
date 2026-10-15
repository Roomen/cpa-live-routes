// Package config parses and clamps the plugin configuration.
package config

import (
	"gopkg.in/yaml.v3"
)

// Config is the effective plugin configuration.
type Config struct {
	RetentionMinutes    int               `yaml:"retention_minutes"`
	MaxRecords          int               `yaml:"max_records"`
	ActiveWindowMinutes int               `yaml:"active_window_minutes"`
	AnimationSpeed      string            `yaml:"animation_speed"`
	ReducedAnimation    bool              `yaml:"reduced_animation"`
	InflightTracking    bool              `yaml:"inflight_tracking"`
	Layout              string            `yaml:"layout"`
	RequireKey          bool              `yaml:"require_key"`
	APIKeyLabels        map[string]string `yaml:"api_key_labels"`
}

// Default returns the configuration used when no keys are set.
func Default() Config {
	return Config{
		RetentionMinutes:    30,
		MaxRecords:          5000,
		ActiveWindowMinutes: 10,
		AnimationSpeed:      "1x",
		InflightTracking:    true,
		Layout:              "tabs",
	}
}

// Parse reads the plugin YAML and clamps every value into its range.
func Parse(raw []byte) (Config, error) {
	cfg := Default()
	if len(raw) > 0 {
		if err := yaml.Unmarshal(raw, &cfg); err != nil {
			return Default(), err
		}
	}
	return cfg.Clamp(), nil
}

// Clamp forces out-of-range values back into range.
func (c Config) Clamp() Config {
	d := Default()
	c.RetentionMinutes = clamp(c.RetentionMinutes, 1, 120)
	c.MaxRecords = clamp(c.MaxRecords, 1000, 100000)
	c.ActiveWindowMinutes = min(clamp(c.ActiveWindowMinutes, 1, 60), c.RetentionMinutes)
	switch c.AnimationSpeed {
	case "0.5x", "1x", "2x":
	default:
		c.AnimationSpeed = d.AnimationSpeed
	}
	switch c.Layout {
	case "tabs", "split":
	default:
		c.Layout = d.Layout
	}
	return c
}

func clamp(v, lo, hi int) int {
	return max(lo, min(hi, v))
}

// Field describes one configuration key for the host UI. JSON names follow the
// host's Go field names.
type Field struct {
	Name        string
	Type        string
	Default     any
	EnumValues  []string
	Description string
}

// Fields lists the configuration schema.
func Fields() []Field {
	d := Default()
	return []Field{
		{Name: "retention_minutes", Type: "integer", Default: d.RetentionMinutes, Description: "Minutes of request history kept in memory (1-120)."},
		{Name: "max_records", Type: "integer", Default: d.MaxRecords, Description: "Maximum number of request records kept in memory (1000-100000)."},
		{Name: "active_window_minutes", Type: "integer", Default: d.ActiveWindowMinutes, Description: "Flow view: nodes without requests in this window fade out (1-60, at most the retention)."},
		{Name: "animation_speed", Type: "enum", Default: d.AnimationSpeed, EnumValues: []string{"0.5x", "1x", "2x"}, Description: "Default animation speed of the page."},
		{Name: "reduced_animation", Type: "boolean", Default: d.ReducedAnimation, Description: "Disable flying dots; show link widths and counters only."},
		{Name: "inflight_tracking", Type: "boolean", Default: d.InflightTracking, Description: "Observe requests before completion (queued and routed states). Off: usage replay only."},
		{Name: "layout", Type: "enum", Default: d.Layout, EnumValues: []string{"tabs", "split"}, Description: "Default page layout."},
		{Name: "require_key", Type: "boolean", Default: d.RequireKey, Description: "Require a management-key session token for the data routes. Without it the plugin trusts a loopback Origin/Referer, which non-browser clients can forge: keep it off only when CPA listens on 127.0.0.1."},
		{Name: "api_key_labels", Type: "object", Default: map[string]string{}, Description: "Map of sha256 hex prefix (8+ chars) of a client API key to a display label."},
	}
}
