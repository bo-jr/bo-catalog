// Command catalog answers GET /items/{sku} from Postgres. Everything except the
// query and the schema comes from bo-service-kit.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/bo-jr/bo-service-kit/httpx"

	"github.com/bo-jr/bo-catalog/internal/migrate"
)

// service is the in-cluster name: the metric `service` label, log field and
// OTel service.name. Never the bo- prefixed repo name.
const service = "catalog"

// EnvDatabaseURL holds the libpq URI. In the cluster it comes from the `uri`
// key of the CNPG-generated catalog-db-app Secret (Phase 7: via ESO).
const EnvDatabaseURL = "DATABASE_URL"

type item struct {
	SKU            string `json:"sku"`
	Name           string `json:"name"`
	UnitPriceCents int64  `json:"unit_price_cents"`
}

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	dsn := os.Getenv(EnvDatabaseURL)
	if dsn == "" {
		return fmt.Errorf("%s is required", EnvDatabaseURL)
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		// Never echo the DSN: it carries the password.
		return fmt.Errorf("%s: unparseable", EnvDatabaseURL)
	}
	cfg.MaxConns = 4
	cfg.ConnConfig.ConnectTimeout = 3 * time.Second

	srv, err := httpx.New(ctx, service)
	if err != nil {
		return err
	}
	log := srv.Logger()

	// Lazy: no connection is made until first use, so a database that is
	// still starting does not stop the process from coming up.
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return err
	}
	defer pool.Close()

	mctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var migrated atomic.Bool
	go migrateUntilDone(mctx, pool, &migrated, log)

	// Ready means: the schema this binary expects is in place AND the database
	// answers right now. Not a static 200.
	srv.AddCheck(httpx.Check{Name: "db", Fn: func(ctx context.Context) error {
		if !migrated.Load() {
			return errors.New("migrations pending")
		}
		return pool.Ping(ctx)
	}})

	tracer := otel.Tracer("github.com/bo-jr/bo-catalog")
	srv.Handle("GET /items/{sku}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sku := r.PathValue("sku")
		it, err := lookup(r.Context(), tracer, pool, sku)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown sku", "sku": sku})
		case err != nil:
			log.ErrorContext(r.Context(), "lookup failed", "sku", sku, "err", err.Error())
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "lookup failed"})
		default:
			writeJSON(w, http.StatusOK, it)
		}
	}))

	return srv.Run(ctx)
}

func lookup(ctx context.Context, tracer trace.Tracer, pool *pgxpool.Pool, sku string) (item, error) {
	ctx, span := tracer.Start(ctx, "SELECT items",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("db.system.name", "postgresql"),
			attribute.String("db.operation.name", "SELECT"),
			attribute.String("db.collection.name", "items"),
		))
	defer span.End()

	var it item
	err := pool.QueryRow(ctx,
		"SELECT sku, name, unit_price_cents FROM items WHERE sku = $1", sku,
	).Scan(&it.SKU, &it.Name, &it.UnitPriceCents)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		span.RecordError(err)
		span.SetStatus(codes.Error, "query failed")
	}
	return it, err
}

// migrateUntilDone retries with backoff until the schema is applied. A fresh
// CNPG Cluster takes tens of seconds to accept connections; crash-looping
// through that would only add restart noise.
func migrateUntilDone(ctx context.Context, pool *pgxpool.Pool, done *atomic.Bool, log *slog.Logger) {
	backoff := time.Second
	for {
		applied, err := migrate.Apply(ctx, pool)
		if err == nil {
			log.Info("migrations complete", "applied", applied)
			done.Store(true)
			return
		}
		if ctx.Err() != nil {
			return
		}
		log.Warn("migrations not applied yet; retrying", "err", err.Error(), "retry_in", backoff.String())
		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return
		}
		backoff = min(backoff*2, 15*time.Second)
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
