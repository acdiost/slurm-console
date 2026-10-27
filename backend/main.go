package main

import (
	"log/slog"
	"os"
	"runtime"

	"slurm-console/backend/internal/application"
	"slurm-console/backend/internal/backup"
	"slurm-console/backend/internal/linuxusers"
	"slurm-console/backend/internal/maintenance"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	// NSS reads are standalone and never bootstrap a database or authentication.
	if len(os.Args) > 1 && os.Args[1] == linuxusers.ReaderCommand {
		if len(os.Args) != 2 || runtime.GOOS != "linux" {
			os.Exit(2)
		}
		os.Exit(linuxusers.ServeReader(os.Stdin, os.Stdout))
	}
	if handled, err := backup.Dispatch(os.Args[1:]); handled {
		if err != nil {
			slog.Error("maintenance command failed", "error", err)
			os.Exit(1)
		}
		return
	}
	if backup.FirstCommand(os.Args[1:]) != "serve" {
		unlock, err := maintenance.Shared()
		if err != nil {
			slog.Error("administration unavailable", "error", err)
			os.Exit(1)
		}
		defer unlock()
	}
	app, err := application.New()
	if err != nil {
		slog.Error("invalid application configuration", "error", err)
		os.Exit(1)
	}
	if err := app.Start(); err != nil {
		slog.Error("application stopped", "error", err)
		os.Exit(1)
	}
}
