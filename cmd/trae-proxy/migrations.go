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
