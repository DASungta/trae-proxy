package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// currentSchemaVersion is the config schema version the current binary supports.
// Increment when adding/removing/renaming config fields.
const currentSchemaVersion = 2

// Migrate detects the current config schema version for the given configPath
// and applies migration functions. Returns whether any changes were made and a
// report of what changed.
func Migrate(configPath string, cfg *Config) (changed bool, report []string) {
	version := loadSchemaVersion(configPath)
	if version >= currentSchemaVersion {
		return false, nil
	}

	if version < 2 {
		report = append(report, migrateV1toV2(cfg))
		changed = true
	}

	if changed {
		_ = saveSchemaVersion(configPath, currentSchemaVersion)
	}

	return changed, report
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
