//go:build !linux || !cgo

package main

import "slurm-console/backend/internal/pamauth"

func run(pamauth.Request) pamauth.Result { return pamauth.Result{Code: "unavailable"} }
