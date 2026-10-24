package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"time"

	"slurm-console/backend/internal/pamauth"
)

func main() {
	// A separate process bounds uninterruptible PAM modules, including under sudo.
	timer := time.AfterFunc(8*time.Second, func() { os.Exit(1) })
	defer timer.Stop()
	result := pamauth.Result{Code: "unavailable"}
	if len(os.Args) == 1 {
		input, err := io.ReadAll(io.LimitReader(os.Stdin, 16385))
		defer clear(input)
		if err == nil && len(input) <= 16384 {
			var request pamauth.Request
			decoder := json.NewDecoder(bytes.NewReader(input))
			decoder.DisallowUnknownFields()
			if decoder.Decode(&request) == nil {
				var extra any
				if decoder.Decode(&extra) == io.EOF {
					result = run(request)
				}
			}
		}
	}
	_ = json.NewEncoder(os.Stdout).Encode(result)
}
