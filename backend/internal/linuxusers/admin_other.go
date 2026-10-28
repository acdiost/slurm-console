//go:build !linux

package linuxusers

import "io"

func ServeAdmin(io.Reader, io.Writer) int { return 2 }
