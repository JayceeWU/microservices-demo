package platform

import (
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5/pgxpool"
)

func MustEnv(key string) string {
	value := os.Getenv(key)
	if value == "" {
		panic(fmt.Sprintf("%s is required", key))
	}
	return value
}

func OpenDatabase() *pgxpool.Pool {
	ctx, cancel := ContextWithTimeout(10 * time.Second)
	defer cancel()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		slog.Error("database configuration failed", "error", "DATABASE_URL is required")
		os.Exit(1)
	}
	config, err := pgxpool.ParseConfig(dsn)
	if err == nil {
		config.ConnConfig.Tracer = otelpgx.NewTracer()
	}
	var pool *pgxpool.Pool
	if err == nil {
		pool, err = pgxpool.NewWithConfig(ctx, config)
	}
	if err != nil {
		slog.Error("database configuration failed", "error", err)
		os.Exit(1)
	}
	if err := pool.Ping(ctx); err != nil {
		slog.Error("database connection failed", "error", err)
		os.Exit(1)
	}
	return pool
}
