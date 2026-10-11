package component

import (
	"database/sql"
	"fmt"
	"log"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"service/config"
)

var DB *sql.DB

// InitMySQL 初始化MySQL连接
func InitMySQL(cfg *config.Config) (*sql.DB, error) {
	// 强制 DSN 包含 parseTime=true，避免 datetime 列扫描到 *time.Time 报错
	dsn := ensureParseTimeDSN(cfg.MySQL.DSN)

	// 解析DSN以提取数据库名
	dbName, dsnWithoutDB := parseDSN(dsn)

	// 如果指定了数据库名，先尝试创建数据库（如果不存在）
	if dbName != "" {
		if err := ensureDatabase(ensureParseTimeDSN(dsnWithoutDB), dbName); err != nil {
			return nil, fmt.Errorf("创建数据库失败: %v", err)
		}
	}

	// 连接到目标数据库
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}

	// 设置连接池参数
	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(time.Hour)

	if err := db.Ping(); err != nil {
		return nil, err
	}

	DB = db
	log.Println("MySQL 连接成功")
	return db, nil
}

// ensureParseTimeDSN 确保 DSN 包含 parseTime=true 和 loc=Local，避免 datetime 列 Scan 报错
func ensureParseTimeDSN(dsn string) string {
	dsnLower := strings.ToLower(dsn)
	if strings.Contains(dsnLower, "parsetime=true") {
		return dsn
	}
	if strings.Contains(dsn, "?") {
		return dsn + "&parseTime=true&loc=Local"
	}
	return dsn + "?parseTime=true&loc=Local"
}

// parseDSN 解析DSN，返回数据库名和去掉数据库名的DSN
func parseDSN(dsn string) (string, string) {
	// DSN格式: user:password@tcp(host:port)/database?params
	parts := strings.Split(dsn, "/")
	if len(parts) < 2 {
		return "", dsn
	}
	
	// 提取数据库名（去掉查询参数）
	dbPart := strings.Split(parts[1], "?")[0]
	if dbPart == "" {
		return "", dsn
	}
	
	// 构建不带数据库名的DSN（连接到mysql系统数据库）
	dsnWithoutDB := parts[0] + "/mysql"
	if strings.Contains(parts[1], "?") {
		queryParams := strings.Split(parts[1], "?")[1]
		dsnWithoutDB += "?" + queryParams
	}
	
	return dbPart, dsnWithoutDB
}

// ensureDatabase 确保数据库存在，如果不存在则创建
func ensureDatabase(dsnWithoutDB, dbName string) error {
	// 连接到mysql系统数据库
	db, err := sql.Open("mysql", dsnWithoutDB)
	if err != nil {
		return err
	}
	defer db.Close()

	// 检查数据库是否存在
	var exists int
	query := "SELECT 1 FROM information_schema.SCHEMATA WHERE SCHEMA_NAME = ?"
	err = db.QueryRow(query, dbName).Scan(&exists)
	if err == nil {
		// 数据库已存在
		return nil
	}
	if err != sql.ErrNoRows {
		// 查询出错
		return err
	}

	// 数据库不存在，创建它
	createSQL := fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci", dbName)
	_, err = db.Exec(createSQL)
	if err != nil {
		return fmt.Errorf("创建数据库 %s 失败: %v", dbName, err)
	}
	
	log.Printf("数据库 %s 创建成功", dbName)
	return nil
}

// GetDB 获取数据库连接
func GetDB() *sql.DB {
	return DB
}
