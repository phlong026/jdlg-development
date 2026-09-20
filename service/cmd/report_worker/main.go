// Command report_worker processes queued reports independently of HTTP traffic.
package main

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	mysql "github.com/go-sql-driver/mysql"
	"service/component"
	"service/config"
	"service/route"
)

func main() {
	if err := run(); err != nil {
		log.Fatal("[report-worker] startup or shutdown failed: ", err)
	}
}

func run() error {
	if err := config.LoadLocalEnv(); err != nil {
		return errors.New("cannot load local environment")
	}
	logger, err := component.InitDailyAppLogger()
	if err != nil {
		return err
	}
	defer logger.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	cfg := config.Init()
	dsn, err := mysql.ParseDSN(cfg.MySQL.DSN)
	if err != nil || dsn.DBName == "" {
		return errors.New("invalid MYSQL_DSN or missing database")
	}
	dsn.ParseTime = true
	if dsn.Timeout == 0 {
		dsn.Timeout = 10 * time.Second
	}
	db, err := sql.Open("mysql", dsn.FormatDSN())
	if err != nil {
		return errors.New("cannot initialize database client")
	}
	defer db.Close()
	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)
	check, cancel := context.WithTimeout(ctx, 10*time.Second)
	err = db.PingContext(check)
	cancel()
	if err != nil {
		return errors.New("database connection unavailable")
	}
	module, err := route.NewReportModule(ctx, db, cfg)
	if err != nil {
		return err
	}
	if module == nil {
		return errors.New("REPORT_ENABLED must be true")
	}
	log.Printf("[report-worker] started concurrency=%d", module.WorkerConcurrency)
	module.Run(ctx)
	log.Printf("[report-worker] stopped")
	return nil
}
