package config

import (
	"fmt"
	"net/url"
	"os"
	"time"
	_ "time/tzdata" // embedded time zone database (the runtime image has none)
)

type Config struct {
	GRPCPort    string
	DatabaseURL string
	Location    *time.Location
}

func Load() (Config, error) {
	loc, err := time.LoadLocation(getenv("APP_TIMEZONE", "Asia/Almaty"))
	if err != nil {
		return Config{}, fmt.Errorf("invalid APP_TIMEZONE: %w", err)
	}

	dsn := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(getenv("POSTGRES_USER", "hr"), os.Getenv("POSTGRES_PASSWORD")),
		Host:   getenv("DB_HOST", "localhost") + ":" + getenv("DB_PORT", "5432"),
		Path:   getenv("POSTGRES_DB", "hr"),
	}
	dsn.RawQuery = "sslmode=disable&timezone=UTC"

	return Config{
		GRPCPort:    getenv("GRPC_PORT", "9090"),
		DatabaseURL: dsn.String(),
		Location:    loc,
	}, nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
