// Package protocol describes the reviewed, versioned slurmrestd contracts.
package protocol

import "strings"

const DefaultVersion = "v0.0.43"

// Versions returns newest first. It is independent of the Slurm release number.
func Versions() []string { return []string{"v0.0.44", "v0.0.43", "v0.0.42", "v0.0.41", "v0.0.40"} }

func Normalize(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "v") {
		value = "v" + value
	}
	for _, version := range Versions() {
		if value == version {
			return value, true
		}
	}
	return "", false
}

func ReservationWrites(version string) bool { return version == "v0.0.43" || version == "v0.0.44" }
func QOSGroupMinutes(version string) bool   { return version == "v0.0.42" || ReservationWrites(version) }
