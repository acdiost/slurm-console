//go:build !linux && !darwin

package maintenance

import (
	"fmt"
	"os"
)

func Shared() (func(), error)    { return func() {}, nil }
func Exclusive() (func(), error) { return nil, fmt.Errorf("maintenance requires Linux") }

func OwnedBy(info os.FileInfo, uid int) bool { return false }
