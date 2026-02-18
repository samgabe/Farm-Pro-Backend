package database

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

func EnsureSchema(ctx context.Context, pool *pgxpool.Pool, schemaPath string) error {
	if strings.TrimSpace(schemaPath) == "" {
		schemaPath = "db/schema.sql"
	}

	data, err := os.ReadFile(filepath.Clean(schemaPath))
	if err != nil {
		return fmt.Errorf("read schema file failed (%s): %w", schemaPath, err)
	}

	statements := strings.Split(string(data), ";")
	for _, stmt := range statements {
		query := strings.TrimSpace(stmt)
		if query == "" {
			continue
		}
		if _, err := pool.Exec(ctx, query); err != nil {
			return fmt.Errorf("schema statement failed: %w", err)
		}
	}

	return nil
}
