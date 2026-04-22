package main

import (
	"fmt"
	"strings"
)

type migrationNote struct {
	RequiresReinit bool
	Steps          []string
	Reason         string
}

// versionMigrations maps from-version prefix → migration note shown after update.
var versionMigrations = map[string]migrationNote{
	"v0.3": {
		RequiresReinit: true,
		Steps: []string{
			"trae-proxy stop",
			"trae-proxy init   # 重新申请权限（不再需要 sudo）",
			"trae-proxy start -d",
		},
		Reason: "v0.4.0 移除了 sudo 依赖，需要重新初始化权限配置。",
	},
	"v0.4": {
		RequiresReinit: false,
		Steps: []string{
			"trae-proxy update",
			"trae-proxy restart",
			"如使用旧版 upstream/upstream_protocol 配置，可运行 update 后自动迁移为 [upstreams.default]",
		},
		Reason: "v0.5.0 引入多上游路由与 schema v3，旧配置会在升级后自动迁移。",
	},
}

// PrintMigrationGuide prints migration steps if upgrading from oldVersion to newVersion
// requires manual intervention.
func PrintMigrationGuide(oldVersion, newVersion string) {
	for prefix, note := range versionMigrations {
		if strings.HasPrefix(oldVersion, prefix) {
			fmt.Println()
			fmt.Println("╔══════════════════════════════════════════╗")
			fmt.Println("║         升级后需要手动操作               ║")
			fmt.Println("╠══════════════════════════════════════════╣")
			fmt.Printf("║  从 %s 升级到 %s\n", oldVersion, newVersion)
			fmt.Printf("║  原因: %s\n", note.Reason)
			fmt.Println("║")
			fmt.Println("║  请依次执行以下命令:")
			for i, step := range note.Steps {
				fmt.Printf("║    %d. %s\n", i+1, step)
			}
			fmt.Println("╚══════════════════════════════════════════╝")
			fmt.Println()
			return
		}
	}
}
