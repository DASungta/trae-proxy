package config

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

func orderedUpstreamNames(upstreams map[string]*Upstream) []string {
	names := make([]string, 0, len(upstreams))
	for name := range upstreams {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		if names[i] == "default" {
			return true
		}
		if names[j] == "default" {
			return false
		}
		return names[i] < names[j]
	})
	return names
}

func formatRawModelValue(v any) (string, error) {
	switch val := v.(type) {
	case string:
		return fmt.Sprintf("%q", val), nil
	case map[string]any:
		parts := make([]string, 0, 2)
		if upstream, _ := val["upstream"].(string); upstream != "" {
			parts = append(parts, fmt.Sprintf("upstream = %q", upstream))
		}
		if model, ok := val["model"].(string); ok {
			parts = append(parts, fmt.Sprintf("model = %q", model))
		}
		return "{ " + strings.Join(parts, ", ") + " }", nil
	case *ModelRoute:
		if val == nil {
			return "", fmt.Errorf("nil model route")
		}
		parts := make([]string, 0, 2)
		if val.Upstream != "" {
			parts = append(parts, fmt.Sprintf("upstream = %q", val.Upstream))
		}
		parts = append(parts, fmt.Sprintf("model = %q", val.Model))
		return "{ " + strings.Join(parts, ", ") + " }", nil
	default:
		return "", fmt.Errorf("unsupported raw model value type %T", v)
	}
}

// SaveV3 writes Config in v3 format:
// - outputs [upstreams.*], NOT old upstream/upstream_protocol fields
// - [models] string values keep original format, inline-table values keep original format
func SaveV3(path string, cfg *Config) error {
	var b strings.Builder

	b.WriteString("# trae-proxy configuration\n\n")
	fmt.Fprintf(&b, "listen = %q\n", cfg.Listen)
	fmt.Fprintf(&b, "hijack = %q\n", cfg.Hijack)
	fmt.Fprintf(&b, "log_level = %q\n", cfg.LogLevel)
	fmt.Fprintf(&b, "log_body = %v\n", cfg.LogBody)
	if cfg.RealModels {
		fmt.Fprintf(&b, "real_models = %v\n", cfg.RealModels)
	}
	b.WriteString("\n")

	for _, name := range orderedUpstreamNames(cfg.Upstreams) {
		upstream := cfg.Upstreams[name]
		if upstream == nil {
			continue
		}
		fmt.Fprintf(&b, "[upstreams.%s]\n", name)
		fmt.Fprintf(&b, "url = %q\n", upstream.URL)
		fmt.Fprintf(&b, "protocol = %q\n", upstream.Protocol)
		if upstream.Default {
			b.WriteString("default = true\n")
		}
		b.WriteString("\n")
	}

	b.WriteString("[models]\n")
	keys := make([]string, 0, len(cfg.RawModels))
	for k := range cfg.RawModels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		formatted, err := formatRawModelValue(cfg.RawModels[k])
		if err != nil {
			return err
		}
		fmt.Fprintf(&b, "%q = %s\n", k, formatted)
	}

	return os.WriteFile(path, []byte(b.String()), 0644)
}
