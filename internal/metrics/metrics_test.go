package metrics

import (
	"bytes"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// render writes the current metrics and returns them as a set of lines, so a
// test can assert on one series without pinning the whole output.
func render(t *testing.T, s Snapshot) map[string]string {
	t.Helper()

	var buf bytes.Buffer
	require.NoError(t, Write(&buf, s))

	lines := map[string]string{}
	for _, line := range strings.Split(buf.String(), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, value, ok := strings.Cut(line, " ")
		require.True(t, ok, "malformed exposition line %q", line)
		lines[name] = value
	}
	return lines
}

func TestExposition(t *testing.T) {
	Reset()
	t.Cleanup(Reset)

	out := render(t, Snapshot{ByStatus: map[string]int{"pending": 842, "running": 3}, Overdue: 117})

	assert.Equal(t, "842", out[`schedy_tasks{status="pending"}`])
	assert.Equal(t, "3", out[`schedy_tasks{status="running"}`])
	assert.Equal(t, "117", out["schedy_tasks_overdue"])

	// A status absent from the snapshot still gets a series, so a dashboard
	// doesn't lose the line the moment the queue drains.
	for _, status := range []string{"succeeded", "failed", "cancelled"} {
		assert.Contains(t, out, `schedy_tasks{status="`+status+`"}`, "missing zero series for %s", status)
	}
}

func TestCounters(t *testing.T) {
	Reset()
	t.Cleanup(Reset)

	ObserveDelivery(10*time.Millisecond, true)
	ObserveDelivery(20*time.Millisecond, true)
	ObserveDelivery(30*time.Millisecond, false)
	ObserveTaskFinished(true)
	ObserveTaskFinished(false)

	out := render(t, Snapshot{})

	assert.Equal(t, "2", out[`schedy_deliveries_total{result="success"}`])
	assert.Equal(t, "1", out[`schedy_deliveries_total{result="failure"}`])
	assert.Equal(t, "1", out[`schedy_tasks_finished_total{status="succeeded"}`])
	assert.Equal(t, "1", out[`schedy_tasks_finished_total{status="failed"}`])
	assert.Equal(t, "3", out["schedy_delivery_duration_seconds_count"])
	assert.Equal(t, "0.06", out["schedy_delivery_duration_seconds_sum"])
}

// Buckets are cumulative and the last one is +Inf, per the exposition format. A
// histogram that violates either is silently wrong on every dashboard built on
// it, so pin the exact boundaries.
func TestHistogramBuckets(t *testing.T) {
	Reset()
	t.Cleanup(Reset)

	ObserveDelivery(3*time.Millisecond, true)  // <= 0.005
	ObserveDelivery(10*time.Millisecond, true) // exactly 0.01, counts in that bucket
	ObserveDelivery(100*time.Second, false)    // beyond every bound: +Inf only
	out := render(t, Snapshot{})

	assert.Equal(t, "1", out[`schedy_delivery_duration_seconds_bucket{le="0.005"}`])
	assert.Equal(t, "2", out[`schedy_delivery_duration_seconds_bucket{le="0.01"}`], "an observation exactly on a bound belongs to it")
	assert.Equal(t, "2", out[`schedy_delivery_duration_seconds_bucket{le="30"}`], "the 100s observation must not land in a finite bucket")
	assert.Equal(t, "3", out[`schedy_delivery_duration_seconds_bucket{le="+Inf"}`])
	assert.Equal(t, "3", out["schedy_delivery_duration_seconds_count"])
}

// Lateness is the service level indicator, and a timer firing a hair early must
// not push a negative value into it.
func TestLatenessClampsNegative(t *testing.T) {
	Reset()
	t.Cleanup(Reset)

	ObserveLateness(-5 * time.Second)
	out := render(t, Snapshot{})

	assert.Equal(t, "0", out["schedy_task_lateness_seconds_sum"])
	assert.Equal(t, "1", out[`schedy_task_lateness_seconds_bucket{le="0.1"}`])
}

// The runner observes from one goroutine per task, so the counters are written
// concurrently by construction. Run with -race.
func TestConcurrentObserve(t *testing.T) {
	Reset()
	t.Cleanup(Reset)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ObserveDelivery(time.Millisecond, true)
			ObserveLateness(time.Second)
		}()
	}
	wg.Wait()

	out := render(t, Snapshot{})
	assert.Equal(t, "100", out[`schedy_deliveries_total{result="success"}`])
	assert.Equal(t, "100", out["schedy_task_lateness_seconds_count"])
}

func TestSkippedAndInflight(t *testing.T) {
	Reset()
	t.Cleanup(Reset)

	ObserveSkipped()
	ObserveSkipped()
	InflightAdd(3)
	InflightAdd(-1)

	out := render(t, Snapshot{})

	assert.Equal(t, "2", out[`schedy_tasks_skipped_total{reason="stale"}`])
	assert.Equal(t, "2", out["schedy_deliveries_inflight"], "the gauge tracks deltas both ways")
}
