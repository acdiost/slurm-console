package bulkcontrol

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestPoolSharesThreeWorkersAcrossBatches(t *testing.T) {
	s := &Service{workers: make(chan struct{}, 3)}
	var running, peak, total atomic.Int32
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.pool(context.Background(), 10, func(int) {
				n := running.Add(1)
				for {
					old := peak.Load()
					if n <= old || peak.CompareAndSwap(old, n) {
						break
					}
				}
				time.Sleep(time.Millisecond)
				running.Add(-1)
				total.Add(1)
			})
		}()
	}
	wg.Wait()
	if peak.Load() > 3 || total.Load() != 20 {
		t.Fatal(peak.Load(), total.Load())
	}
}
func TestBulkInputBoundaries(t *testing.T) {
	if ValidTargets("jobs.cancel", []string{"1", "1"}) || ValidTargets("nodes.drain", []string{"node[1-2]"}) || ValidTargets("nodes.resume", []string{"ALL"}) || ValidTargets("jobs.cancel", make([]string, 11)) {
		t.Fatal("broad or duplicate selector accepted")
	}
	for _, reason := range []string{"x", "bad\nreason", "bad\x00reason", strings.Repeat("字", 301)} {
		if validInput("jobs.cancel", 2, Input{Confirmation: "CANCEL 2", Reason: reason}) {
			t.Fatal("bad reason accepted")
		}
	}
	if Confirmation("nodes.resume", 10) != "RESUME 10" {
		t.Fatal("wrong count confirmation")
	}
}

func TestCancelledPoolDoesNotStartQueuedTargets(t *testing.T) {
	s := &Service{workers: make(chan struct{}, 3)}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var calls atomic.Int32
	s.pool(ctx, 10, func(int) { calls.Add(1) })
	if calls.Load() != 0 {
		t.Fatal("started queued work after application cancellation")
	}
}

func TestExpiredContextReadReleasesCapacityWithoutAnotherPrepare(t *testing.T) {
	for _, g := range []*group{{expires: time.Now().Add(-time.Second)}, {started: true, status: "completed", retain: time.Now().Add(-time.Second)}} {
		g.size = 100
		s := &Service{groups: map[string]*group{"expired": g}, bytes: 100}
		if s.lookup("expired") != nil || s.bytes != 0 || len(s.groups) != 0 {
			t.Fatal("expired context remained readable or retained capacity")
		}
	}
	g := &group{started: true, status: "processing", size: 100}
	s := &Service{groups: map[string]*group{"active": g}, bytes: 100}
	if s.lookup("active") != g || s.bytes != 100 {
		t.Fatal("running batch evicted")
	}
}
