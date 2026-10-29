package adapter

import "testing"

func TestReservationHostlistBounded(t *testing.T) {
	for input, want := range map[string]int{"node[01-03],gpu2": 4, "rack[1-2]n[01-02]": 4, "localhost": 1} {
		v, ok := ExpandReservationNodes(input)
		if !ok || len(v) != want {
			t.Fatal(input, v, ok)
		}
	}
	for _, input := range []string{"ALL", "n[1-9999999]", "n[2-1]", "n1,n1", "n[01", "n[1,[2]]"} {
		if _, ok := ExpandReservationNodes(input); ok {
			t.Fatal(input)
		}
	}
}
