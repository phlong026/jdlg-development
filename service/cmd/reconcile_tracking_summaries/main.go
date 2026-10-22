package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"service/component"
	"service/config"
	"service/tracking"
)

type cliOptions struct {
	envPath   string
	tenant    string
	actor     int64
	owners    []string
	reconcile tracking.SummaryReconcileOptions
	timeout   time.Duration
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "tracking summary reconcile failed: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	options, err := parseOptions(args)
	if err != nil {
		return err
	}
	if options.envPath != "" {
		if err := config.LoadLocalEnvFile(options.envPath); err != nil {
			return fmt.Errorf("load environment file: %w", err)
		}
	}
	if os.Getenv("MYSQL_DSN") == "" {
		return errors.New("MYSQL_DSN is required")
	}
	trackingConfig := tracking.LoadConfig()
	if trackingConfig.Tenant == "" || trackingConfig.Tenant != options.tenant {
		return errors.New("--tenant must equal TRACKING_TENANT")
	}

	appConfig := config.Init()
	db, err := component.InitMySQL(appConfig)
	if err != nil {
		return fmt.Errorf("connect MySQL: %w", err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), options.timeout)
	defer cancel()
	scope, err := loadAuthorizedScope(ctx, db, options.tenant, options.actor, options.owners)
	if err != nil {
		return err
	}
	service := tracking.New(tracking.NewSQLStore(db, 1, 1), trackingConfig)
	result, err := service.ReconcileDeferredSummaries(ctx, scope, options.reconcile)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		return fmt.Errorf("write result: %w", err)
	}
	if result.Failed > 0 {
		return fmt.Errorf("%d session summaries failed to rebuild", result.Failed)
	}
	return nil
}

func loadAuthorizedScope(ctx context.Context, db *sql.DB, tenant string, actor int64, requested []string) (tracking.Scope, error) {
	scope := tracking.Scope{Tenant: tenant, Actor: actor, Owners: requested, Manage: true, Rules: true}
	rows, err := db.QueryContext(ctx, `SELECT t.owner_id
		FROM tracking_access t
		JOIN users u ON u.id=t.user_id
		JOIN management_access a ON a.user_id=t.user_id
		WHERE t.user_id=? AND t.tenant_id=? AND t.enabled=TRUE
		AND t.can_manage=TRUE AND t.can_rules=TRUE
		AND u.user_type='management' AND a.enabled=TRUE
		AND a.scope='platform_admin'`, actor, tenant)
	if err != nil {
		return tracking.Scope{}, fmt.Errorf("read management tracking grants: %w", err)
	}
	defer rows.Close()
	granted := map[string]bool{}
	for rows.Next() {
		var owner string
		if err := rows.Scan(&owner); err != nil {
			return tracking.Scope{}, fmt.Errorf("read management tracking grant: %w", err)
		}
		granted[owner] = true
	}
	if err := rows.Err(); err != nil {
		return tracking.Scope{}, fmt.Errorf("read management tracking grants: %w", err)
	}
	for _, owner := range requested {
		if !granted[owner] {
			return tracking.Scope{}, fmt.Errorf("actor is not authorized for owner %q", owner)
		}
	}
	return scope, nil
}

func parseOptions(args []string) (cliOptions, error) {
	var options cliOptions
	var owners, asOf, afterAt string
	var err error
	flags := flag.NewFlagSet("reconcile_tracking_summaries", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	flags.StringVar(&options.envPath, "env", "", "optional KEY=value environment file")
	flags.StringVar(&options.tenant, "tenant", "", "tracking tenant (must equal TRACKING_TENANT)")
	flags.Int64Var(&options.actor, "actor", 0, "authorized management account ID")
	flags.StringVar(&owners, "owners", "", "comma-separated authorized owner IDs")
	flags.IntVar(&options.reconcile.Limit, "limit", 50, "summary-state rows per page (max 500)")
	flags.BoolVar(&options.reconcile.Apply, "apply", false, "rebuild due candidates; default is dry-run")
	flags.StringVar(&asOf, "as-of", "", "fixed RFC3339 snapshot; default is current UTC time")
	flags.StringVar(&afterAt, "after-at", "", "RFC3339 page cursor from next_at")
	flags.StringVar(&options.reconcile.AfterID, "after-id", "", "page cursor from next_id")
	flags.DurationVar(&options.timeout, "timeout", 10*time.Minute, "whole operation timeout")
	if err := flags.Parse(args); err != nil {
		return options, err
	}
	if flags.NArg() != 0 {
		return options, errors.New("unexpected positional arguments")
	}
	options.tenant = strings.TrimSpace(options.tenant)
	if options.tenant == "" || options.actor <= 0 {
		return options, errors.New("--tenant and a positive --actor are required")
	}
	for _, owner := range strings.Split(owners, ",") {
		owner = strings.TrimSpace(owner)
		if owner != "" && !contains(options.owners, owner) {
			options.owners = append(options.owners, owner)
		}
	}
	if len(options.owners) == 0 {
		return options, errors.New("--owners requires at least one explicit owner")
	}
	if options.timeout <= 0 {
		return options, errors.New("--timeout must be positive")
	}
	if options.reconcile.AsOf, err = parseTimeFlag("--as-of", asOf); err != nil {
		return options, err
	}
	if options.reconcile.AfterAt, err = parseTimeFlag("--after-at", afterAt); err != nil {
		return options, err
	}
	return options, nil
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func parseTimeFlag(name, value string) (time.Time, error) {
	if strings.TrimSpace(value) == "" {
		return time.Time{}, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("%s must be RFC3339: %w", name, err)
	}
	return parsed.UTC(), nil
}
