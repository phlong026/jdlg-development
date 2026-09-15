package main

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	mysqlDriver "github.com/go-sql-driver/mysql"
	"service/component"
	"service/config"
)

const migrationTrackingTable = "app_data_migrations"

//go:embed sql/*.sql
var embeddedMigrations embed.FS

func maybeRunDataMigrationCommand() (bool, error) {
	if len(os.Args) < 2 || strings.TrimSpace(os.Args[1]) != "migrate" {
		return false, nil
	}

	if len(os.Args) == 2 || strings.TrimSpace(os.Args[2]) == "--list" {
		files, err := listEmbeddedMigrationFiles()
		if err != nil {
			return true, err
		}
		log.Println("可执行的数据迁移文件:")
		for _, file := range files {
			log.Printf(" - %s", file)
		}
		return true, nil
	}

	cfg := config.Init()
	db, err := component.InitMySQL(cfg)
	if err != nil {
		return true, fmt.Errorf("初始化 MySQL 失败: %w", err)
	}
	defer db.Close()

	for _, rawName := range os.Args[2:] {
		name := strings.TrimSpace(rawName)
		if name == "" {
			continue
		}
		applied, err := runEmbeddedDataMigration(context.Background(), db, name)
		if err != nil {
			return true, err
		}
		if applied {
			log.Printf("数据迁移已执行: %s", filepath.Base(name))
		} else {
			log.Printf("数据迁移已跳过（此前已执行）: %s", filepath.Base(name))
		}
	}

	return true, nil
}

func listEmbeddedMigrationFiles() ([]string, error) {
	entries, err := embeddedMigrations.ReadDir("sql")
	if err != nil {
		return nil, fmt.Errorf("读取内嵌迁移文件失败: %w", err)
	}

	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := strings.TrimSpace(entry.Name())
		if strings.HasSuffix(strings.ToLower(name), ".sql") {
			files = append(files, name)
		}
	}
	return files, nil
}

func runEmbeddedDataMigration(ctx context.Context, db *sql.DB, name string) (bool, error) {
	migrationName := filepath.Base(strings.TrimSpace(name))
	if migrationName == "" {
		return false, fmt.Errorf("迁移文件名不能为空")
	}

	content, err := embeddedMigrations.ReadFile(filepath.ToSlash(filepath.Join("sql", migrationName)))
	if err != nil {
		return false, fmt.Errorf("读取内嵌迁移文件失败 %s: %w", migrationName, err)
	}

	statements, err := splitSQLStatements(string(content))
	if err != nil {
		return false, fmt.Errorf("解析迁移文件失败 %s: %w", migrationName, err)
	}
	if len(statements) == 0 {
		return false, fmt.Errorf("迁移文件没有可执行 SQL: %s", migrationName)
	}

	if err := ensureMigrationTrackingTable(ctx, db); err != nil {
		return false, err
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("开启迁移事务失败: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if _, err := tx.ExecContext(ctx, fmt.Sprintf("INSERT INTO %s (name) VALUES (?)", migrationTrackingTable), migrationName); err != nil {
		if isDuplicateMigrationError(err) {
			return false, nil
		}
		return false, fmt.Errorf("登记迁移记录失败 %s: %w", migrationName, err)
	}

	for _, statement := range statements {
		if strings.TrimSpace(statement) == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return false, fmt.Errorf("执行迁移 SQL 失败 %s: %w", migrationName, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("提交迁移事务失败 %s: %w", migrationName, err)
	}

	return true, nil
}

func ensureMigrationTrackingTable(ctx context.Context, db *sql.DB) error {
	query := fmt.Sprintf(`
CREATE TABLE IF NOT EXISTS %s (
	name VARCHAR(255) NOT NULL PRIMARY KEY,
	executed_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
`, migrationTrackingTable)

	if _, err := db.ExecContext(ctx, query); err != nil {
		return fmt.Errorf("初始化迁移记录表失败: %w", err)
	}
	return nil
}

func isDuplicateMigrationError(err error) bool {
	var mysqlErr *mysqlDriver.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == 1062
}

func splitSQLStatements(sqlText string) ([]string, error) {
	runes := []rune(sqlText)
	var (
		statements     []string
		current        strings.Builder
		inSingleQuote  bool
		inDoubleQuote  bool
		inBacktick     bool
		inLineComment  bool
		inBlockComment bool
	)

	for i := 0; i < len(runes); i++ {
		ch := runes[i]
		var next rune
		if i+1 < len(runes) {
			next = runes[i+1]
		}

		if inLineComment {
			if ch == '\n' {
				inLineComment = false
			}
			continue
		}

		if inBlockComment {
			if ch == '*' && next == '/' {
				inBlockComment = false
				i++
			}
			continue
		}

		if !inSingleQuote && !inDoubleQuote && !inBacktick {
			if ch == '#' {
				inLineComment = true
				continue
			}
			if ch == '-' && next == '-' {
				var third rune
				if i+2 < len(runes) {
					third = runes[i+2]
				}
				if third == 0 || third == ' ' || third == '\t' || third == '\r' || third == '\n' {
					inLineComment = true
					i++
					continue
				}
			}
			if ch == '/' && next == '*' {
				inBlockComment = true
				i++
				continue
			}
		}

		if ch == '\'' && !inDoubleQuote && !inBacktick {
			if i == 0 || runes[i-1] != '\\' {
				inSingleQuote = !inSingleQuote
			}
		} else if ch == '"' && !inSingleQuote && !inBacktick {
			if i == 0 || runes[i-1] != '\\' {
				inDoubleQuote = !inDoubleQuote
			}
		} else if ch == '`' && !inSingleQuote && !inDoubleQuote {
			inBacktick = !inBacktick
		}

		if ch == ';' && !inSingleQuote && !inDoubleQuote && !inBacktick {
			statement := strings.TrimSpace(current.String())
			if statement != "" {
				statements = append(statements, statement)
			}
			current.Reset()
			continue
		}

		current.WriteRune(ch)
	}

	if inSingleQuote || inDoubleQuote || inBacktick || inBlockComment {
		return nil, fmt.Errorf("SQL 文件存在未闭合的引号或注释")
	}

	statement := strings.TrimSpace(current.String())
	if statement != "" {
		statements = append(statements, statement)
	}

	return statements, nil
}
