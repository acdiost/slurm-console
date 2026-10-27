package backup

import (
	"github.com/spf13/cobra"
	"strings"
)

// FirstCommand recognizes PocketBase global flags only to route maintenance
// outside PocketBase's eager Bootstrap and early signal shutdown path.
func FirstCommand(args []string) string {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if strings.HasPrefix(arg, "-") {
			if arg == "--dir" || arg == "--encryptionEnv" || arg == "--queryTimeout" {
				i++
			}
			continue
		}
		return arg
	}
	return ""
}
func Dispatch(args []string) (bool, error) {
	first := FirstCommand(args)
	if first != "backup" && first != "healthcheck" {
		return false, nil
	}
	root := &cobra.Command{Use: "slurm-console", SilenceErrors: true, SilenceUsage: true}
	root.AddCommand(Commands(), HealthCommand())
	root.SetArgs(args)
	return true, root.Execute()
}
