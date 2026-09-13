package disk

import (
	"context"
	"testing"
	"time"
)

func TestGrowthThresholdAndIncomplete(t *testing.T) {
	a := Snapshot{Root: "/logs", Complete: true, At: time.Now(), Nodes: []Node{{".", 100}}}
	b := a
	b.At = a.At.Add(time.Minute)
	b.Nodes = []Node{{".", 200}}
	if len(GrowthAlerts(a, b, 100)) != 1 {
		t.Fatal("threshold boundary not detected")
	}
	if len(GrowthAlerts(a, b, 101)) != 0 {
		t.Fatal("below threshold alerted")
	}
	b.Complete = false
	if len(GrowthAlerts(a, b, 100)) != 0 {
		t.Fatal("incomplete scan alerted")
	}
	b.Complete = true
	b.At = a.At
	if len(GrowthAlerts(a, b, 100)) != 0 {
		t.Fatal("invalid time interval alerted")
	}
}
func TestScheduleCancellationAndSpacing(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	var completed time.Time
	Schedule(ctx, 10*time.Millisecond, func(context.Context) {
		if calls > 0 && time.Since(completed) < 10*time.Millisecond {
			t.Error("job scheduled before interval")
		}
		calls++
		time.Sleep(5 * time.Millisecond)
		completed = time.Now()
		if calls == 2 {
			cancel()
		}
	})
	if calls != 2 {
		t.Fatalf("unexpected job count %d", calls)
	}
}
