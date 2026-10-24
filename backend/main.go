package main

import (
	"log/slog"
	"os"

	"slurm-console/backend/internal/application"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	app, err := application.New()
	if err != nil {
		slog.Error("invalid authentication configuration", "error", err)
		os.Exit(1)
	}
	if err := app.Start(); err != nil {
		slog.Error("application stopped", "error", err)
		os.Exit(1)
	}
}
