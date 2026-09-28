package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestAutoStartupMigrationsExist 确保白名单里的文件名都真实存在于内嵌 FS 中，
// 防止改名或拼写错误导致启动时静默跳过（fail-soft 会吞掉这个错误）。
func TestAutoStartupMigrationsExist(t *testing.T) {
	files, err := listEmbeddedMigrationFiles()
	if err != nil {
		t.Fatalf("读取内嵌迁移文件失败: %v", err)
	}
	existing := make(map[string]bool, len(files))
	for _, f := range files {
		existing[f] = true
	}

	for _, name := range autoStartupMigrations {
		if !existing[name] {
			t.Errorf("白名单中的迁移不存在于内嵌 FS: %s（可能已改名或拼写错误）", name)
		}
	}
}

// TestAutoStartupMigrationsAreSafe 守卫测试：启动时自动执行的迁移必须是纯增量结构变更。
// 历史上多数迁移是一次性数据手术（删重、合并账号、清理模块）或回滚脚本，
// 一旦被误加入白名单，会在服务启动时无人工确认地执行 —— 因此在此设硬门槛拦截。
func TestAutoStartupMigrationsAreSafe(t *testing.T) {
	// 明确禁止的破坏性关键字（按语句匹配，已剔除注释）
	destructive := []string{"DROP ", "TRUNCATE ", "DELETE "}

	// 回滚脚本命名约定：_down.sql 表示拆除/回退，绝不能自动执行
	forbiddenNameSuffixes := []string{"_down.sql"}

	for _, name := range autoStartupMigrations {
		t.Run(name, func(t *testing.T) {
			lowerName := strings.ToLower(name)
			for _, suffix := range forbiddenNameSuffixes {
				if strings.HasSuffix(lowerName, suffix) {
					t.Fatalf("回滚脚本不得加入启动白名单: %s", name)
				}
			}

			content, err := embeddedMigrations.ReadFile(filepath.ToSlash(filepath.Join("sql", name)))
			if err != nil {
				t.Fatalf("读取内嵌迁移失败 %s: %v", name, err)
			}

			// 复用生产同一套解析器，自动剔除注释，避免注释里的关键字造成误判
			statements, err := splitSQLStatements(string(content))
			if err != nil {
				t.Fatalf("解析迁移 SQL 失败 %s: %v", name, err)
			}

			for _, statement := range statements {
				upper := strings.ToUpper(statement)
				for _, kw := range destructive {
					if strings.Contains(upper, kw) {
						t.Errorf("启动白名单迁移含破坏性语句 %q，须改为人工执行: %s\n语句片段: %.200s",
							strings.TrimSpace(kw), name, statement)
					}
				}
			}
		})
	}
}
