package api

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestFleetCollectionBoundsConcurrencyAndPreservesCompletedResultsOnBudget(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	items := make([]fleetItem, 24)
	for i := range items {
		items[i].State = "pending"
	}
	var active, peak, started atomic.Int32
	collectFleet(ctx, items, func(i int) fleetItem {
		started.Add(1)
		n := active.Add(1)
		defer active.Add(-1)
		for {
			old := peak.Load()
			if n <= old || peak.CompareAndSwap(old, n) {
				break
			}
		}
		if i == 4 {
			cancel()
		}
		if i != 0 {
			<-ctx.Done()
		}
		return fleetItem{State: "connected"}
	})
	if peak.Load() > 4 || started.Load() > 5 {
		t.Fatal("fleet issued unbounded work")
	}
	if items[0].State != "connected" || items[23].State != "pending" {
		t.Fatal("completed or deferred snapshots misrepresented")
	}
}
