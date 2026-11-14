package protocol

import "testing"

func TestReviewedVersionRangeAndCanonicalNames(t *testing.T) {
	for _, version := range Versions() {
		for _, input := range []string{version, version[1:], " " + version + " "} {
			if got, ok := Normalize(input); !ok || got != version {
				t.Fatalf("normalize %q: %q %v", input, got, ok)
			}
		}
	}
	for _, version := range []string{"", "auto", "v0.0.39", "v0.0.45", "v0.0.400", "v0.0.40/jobs", "V0.0.40"} {
		if _, ok := Normalize(version); ok {
			t.Fatal("accepted unreviewed version", version)
		}
	}
	if ReservationWrites("v0.0.40") || ReservationWrites("v0.0.42") || !ReservationWrites("v0.0.43") || QOSGroupMinutes("v0.0.41") || !QOSGroupMinutes("v0.0.42") {
		t.Fatal("incorrect version capability boundaries")
	}
}
