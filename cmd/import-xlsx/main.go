package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/hctty27/x-type-center/internal/config"
	"github.com/hctty27/x-type-center/internal/importer"
	"github.com/hctty27/x-type-center/internal/store"
)

func main() {
	var filePath string
	var mappingPath string
	flag.StringVar(&filePath, "file", "", "xlsx file to import")
	flag.StringVar(&mappingPath, "mapping", "config/import-mapping.json", "import mapping JSON")
	flag.Parse()
	if filePath == "" {
		fmt.Fprintln(os.Stderr, "-file is required")
		os.Exit(2)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	cfg, err := config.Load()
	if err != nil {
		logger.Error("load config", "error", err)
		os.Exit(1)
	}
	ctx := context.Background()
	db, err := store.Open(ctx, cfg.DSN, cfg.DBMaxOpenConns, cfg.DBMaxIdleConns, cfg.DBConnMaxLifetime)
	if err != nil {
		logger.Error("open database", "error", err)
		os.Exit(1)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		logger.Error("migrate database", "error", err)
		os.Exit(1)
	}

	mapping, err := importer.LoadMapping(mappingPath)
	if err != nil {
		logger.Error("load mapping", "error", err)
		os.Exit(1)
	}
	report, err := importer.New(db).Import(ctx, filePath, mapping)
	if err != nil {
		logger.Error("import xlsx", "error", err)
		os.Exit(1)
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	_ = encoder.Encode(report)
}
