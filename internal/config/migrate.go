package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

// currentSchemaVersion is the config schema version the current binary supports.
// Increment when adding/removing/renaming config fields.
const currentSchemaVersion = 3

// Migrate detects the current config schema version for the given configPath
// and applies migration functions. Returns whether any changes were made and a
// report of what changed.
func Migrate(configPath string, cfg *Config) (changed bool, report []string, err error) {
	if _, statErr := os.Stat(configPath); statErr != nil {
		if os.IsNotExist(statErr) {
			return false, nil, nil
		}
		return false, nil, fmt.Errorf("stat config: %w", statErr)
	}

	version := loadSchemaVersion(configPath)
	if version >= currentSchemaVersion {
		return false, nil, nil
	}

	if version < 2 {
		report = append(report, migrateV1toV2(cfg))
		changed = true
	}

	if version < 3 {
		msg, err := migrateV2toV3(configPath, cfg)
		if err != nil {
			return changed, report, err
		}
		if msg != "" {
			report = append(report, msg)
			changed = true
		}
	}

	if changed {
		if err := saveSchemaVersion(configPath, currentSchemaVersion); err != nil {
			return true, report, err
		}
	}

	return changed, report, nil
}

func loadSchemaVersion(configPath string) int {
	versionPath := filepath.Join(filepath.Dir(configPath), ".schema_version")
	data, err := os.ReadFile(versionPath)
	if err != nil {
		return 1
	}
	v, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 1
	}
	return v
}

func saveSchemaVersion(configPath string, v int) error {
	versionPath := filepath.Join(filepath.Dir(configPath), ".schema_version")
	return os.WriteFile(versionPath, []byte(fmt.Sprintf("%d\n", v)), 0o644)
}

// migrateV1toV2: real_models field was added in v2, TOML loads it as false by default.
func migrateV1toV2(cfg *Config) string {
	return "已迁移到 schema v2：新增字段 real_models（默认 false）"
}

// migrateSourceConfig is used to inspect the raw on-disk config without the
// in-memory normalization that Load() applies.
type migrateSourceConfig struct {
	Upstream  string                       `toml:"upstream"`
	Upstreams map[string]map[string]any    `toml:"upstreams"`
}

func migrateV2toV3(configPath string, cfg *Config) (string, error) {
	var src migrateSourceConfig
	if _, err := toml.DecodeFile(configPath, &src); err != nil {
		return "", fmt.Errorf("failed to inspect config for v3 migration: %w", err)
	}
	if src.Upstream == "" {
		return "", nil
	}

	if len(src.Upstreams) == 0 {
		cfg.Upstreams = map[string]*Upstream{
			"default": {
				URL:      cfg.Upstream,
				Protocol: cfg.UpstreamProtocol,
				Default:  true,
			},
		}
		for _, upstream := range cfg.Upstreams {
			upstream.normalize()
		}
		cfg.defaultUpstream = cfg.Upstreams["default"]
	}

	cfg.Upstream = ""
	cfg.UpstreamProtocol = ""
	if err := SaveV3(configPath, cfg); err != nil {
		return "", fmt.Errorf("failed to save migrated config: %w", err)
	}
	return "migrated to schema v3: legacy upstream fields removed", nil
}
