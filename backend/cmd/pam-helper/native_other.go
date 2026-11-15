//go:build !linux || (!amd64 && !arm64)

package main

import "slurm-console/backend/internal/pamauth"

func run(pamauth.Request) pamauth.Result { return pamauth.Result{Code: "unavailable"} }
