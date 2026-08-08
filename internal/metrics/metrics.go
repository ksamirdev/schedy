// Package metrics exposes Schedy's operational counters in the Prometheus text
// exposition format.
//
// It is hand-rolled rather than built on client_golang: the format is stable and
// the surface here is a handful of counters and two histograms, which is not
// worth five dependencies in a binary whose pitch is that it has almost none.
// The trade is that Go runtime metrics (goroutines, GC, heap) are not exported.
// ponytail: swap in client_golang if runtime metrics or exemplars are ever
// wanted - the call sites are all Observe*/Write and would not change.
package metrics

import (
	"fmt"
	"io"
	"math"
	"sort"
	"sync/atomic"
	"time"
)

// Delivery outcomes. A delivery is one HTTP request fired at a task's target;
// a task with retries produces several.
var (
	deliveriesOK   atomic.Uint64
	deliveriesFail atomic.Uint64
)

// Terminal task outcomes. Counted once per task, when it stops retrying.
var (
	tasksSucceeded atomic.Uint64
	tasksFailed    atomic.Uint64
)

// tasksSkipped counts tasks retired without delivery for exceeding the staleness
// limit - the visible cost of an outage, separate from delivery failures.
var tasksSkipped atomic.Uint64

// tasksReplayed counts finished tasks manually re-armed via the API.
var tasksReplayed atomic.Uint64

// inflight is the number of deliveries currently executing. Read against the
// configured concurrency limit, it says whether the runner is saturated.
var inflight atomic.Int64

// deliveryDuration is the round-trip time of a delivery request.
var deliveryDuration = newHistogram([]float64{
	0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30,
})

// lateness is how far past its scheduled time a task actually fired. This is
// the service level indicator: a healthy Schedy fires within a tick, and a
// backlog shows up here long before it shows up as a failure.
var lateness = newHistogram([]float64{
	0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 300, 900, 3600,
})

// ObserveDelivery records one delivery attempt and its round-trip time.
func ObserveDelivery(d time.Duration, ok bool) {
	if ok {
		deliveriesOK.Add(1)
	} else {
		deliveriesFail.Add(1)
	}
	deliveryDuration.observe(d)
}

// ObserveTaskFinished records a task reaching a terminal delivery outcome.
// Cancellations are not deliveries and are not counted here.
func ObserveTaskFinished(ok bool) {
	if ok {
		tasksSucceeded.Add(1)
	} else {
		tasksFailed.Add(1)
	}
}

// ObserveSkipped records a task retired without delivery for being too stale.
func ObserveSkipped() {
	tasksSkipped.Add(1)
}

// ObserveReplay records a finished task re-armed through the API.
func ObserveReplay() {
	tasksReplayed.Add(1)
}

// InflightAdd moves the in-flight delivery gauge by delta.
func InflightAdd(delta int64) {
	inflight.Add(delta)
}

// ObserveLateness records how late a task fired relative to its scheduled time.
// Negative values (a timer firing a hair early) are clamped to zero.
func ObserveLateness(d time.Duration) {
	lateness.observe(max(d, 0))
}

// Snapshot is the store-derived state a scrape needs. It is read per scrape
// rather than tracked incrementally so the numbers can't drift from the store
// across restarts or history expiry.
type Snapshot struct {
	// ByStatus counts tasks per lifecycle status. Statuses absent from the map
	// are still exported, as zero.
	ByStatus map[string]int
	// Overdue counts pending tasks already past their execute_at.
	Overdue int
}

// Statuses fixes the label set and the output order of the task gauge. Exporting
// every status on every scrape - including the zeroes - means a dashboard's
// series don't blink out of existence when a status happens to be empty.
var Statuses = []string{"pending", "running", "succeeded", "failed", "cancelled"}

// Write renders the current metrics in the Prometheus text exposition format.
//
// Label values here are all fixed literals from Statuses, so no escaping is
// needed; that stops being true the moment a metric is labelled by anything
// user-supplied (a task url, say), which is also why none is.
func Write(w io.Writer, s Snapshot) error {
	b := &writer{w: w}

	b.header("schedy_tasks", "gauge", "Tasks currently in each lifecycle status.")
	for _, status := range Statuses {
		b.line("schedy_tasks", fmt.Sprintf(`status=%q`, status), float64(s.ByStatus[status]))
	}

	b.header("schedy_tasks_overdue", "gauge", "Pending tasks whose execute_at has already passed.")
	b.line("schedy_tasks_overdue", "", float64(s.Overdue))

	b.header("schedy_deliveries_total", "counter", "Delivery requests fired at task targets, by outcome. Retries count individually.")
	b.line("schedy_deliveries_total", `result="success"`, float64(deliveriesOK.Load()))
	b.line("schedy_deliveries_total", `result="failure"`, float64(deliveriesFail.Load()))

	b.header("schedy_tasks_finished_total", "counter", "Tasks that reached a terminal delivery outcome, counted once each.")
	b.line("schedy_tasks_finished_total", `status="succeeded"`, float64(tasksSucceeded.Load()))
	b.line("schedy_tasks_finished_total", `status="failed"`, float64(tasksFailed.Load()))

	b.header("schedy_tasks_skipped_total", "counter", "Tasks retired without delivery for exceeding SCHEDY_MAX_STALENESS.")
	b.line("schedy_tasks_skipped_total", `reason="stale"`, float64(tasksSkipped.Load()))

	b.header("schedy_tasks_replayed_total", "counter", "Finished tasks manually re-armed through the API.")
	b.line("schedy_tasks_replayed_total", "", float64(tasksReplayed.Load()))

	b.header("schedy_deliveries_inflight", "gauge", "Deliveries currently executing. Compare against SCHEDY_MAX_CONCURRENT_DELIVERIES to spot saturation.")
	b.line("schedy_deliveries_inflight", "", float64(inflight.Load()))

	b.histogram("schedy_delivery_duration_seconds", "Round-trip time of delivery requests.", deliveryDuration)
	b.histogram("schedy_task_lateness_seconds", "Delay between a task's execute_at and the moment it fired.", lateness)

	return b.err
}

// Reset zeroes every counter. For tests only.
func Reset() {
	deliveriesOK.Store(0)
	deliveriesFail.Store(0)
	tasksSucceeded.Store(0)
	tasksFailed.Store(0)
	tasksSkipped.Store(0)
	tasksReplayed.Store(0)
	inflight.Store(0)
	deliveryDuration.reset()
	lateness.reset()
}

// histogram is a fixed-bucket cumulative histogram.
//
// Buckets hold non-cumulative counts and are summed at render time; sum is kept
// in nanoseconds so it can be a plain atomic integer rather than a compare-and-
// swap loop over float bits.
type histogram struct {
	upper   []float64 // bucket upper bounds, ascending, implicit +Inf on the end
	buckets []atomic.Uint64
	sumNs   atomic.Uint64
	count   atomic.Uint64
}

func newHistogram(upper []float64) *histogram {
	sort.Float64s(upper)
	return &histogram{upper: upper, buckets: make([]atomic.Uint64, len(upper)+1)}
}

func (h *histogram) observe(d time.Duration) {
	// SearchFloat64s returns the first index whose bound is >= v, which is
	// exactly Prometheus's `le` bucket. len(upper) means it exceeded them all,
	// i.e. the implicit +Inf bucket.
	h.buckets[sort.SearchFloat64s(h.upper, d.Seconds())].Add(1)
	h.sumNs.Add(uint64(max(d, 0)))
	h.count.Add(1)
}

func (h *histogram) reset() {
	for i := range h.buckets {
		h.buckets[i].Store(0)
	}
	h.sumNs.Store(0)
	h.count.Store(0)
}

// writer accumulates exposition output, holding the first write error so the
// render path stays free of per-line error checks.
type writer struct {
	w   io.Writer
	err error
}

func (b *writer) printf(format string, args ...any) {
	if b.err != nil {
		return
	}
	_, b.err = fmt.Fprintf(b.w, format, args...)
}

func (b *writer) header(name, typ, help string) {
	b.printf("# HELP %s %s\n# TYPE %s %s\n", name, help, name, typ)
}

func (b *writer) line(name, labels string, v float64) {
	if labels != "" {
		labels = "{" + labels + "}"
	}
	b.printf("%s%s %s\n", name, labels, format(v))
}

func (b *writer) histogram(name, help string, h *histogram) {
	b.header(name, "histogram", help)

	var cumulative uint64
	for i, bound := range h.upper {
		cumulative += h.buckets[i].Load()
		b.line(name+"_bucket", fmt.Sprintf(`le="%s"`, format(bound)), float64(cumulative))
	}
	cumulative += h.buckets[len(h.upper)].Load()
	b.line(name+"_bucket", `le="+Inf"`, float64(cumulative))

	b.line(name+"_sum", "", float64(h.sumNs.Load())/float64(time.Second))
	b.line(name+"_count", "", float64(h.count.Load()))
}

// format renders a value the way Prometheus expects: integers plain, fractions
// without trailing zeroes, and no exponent notation for the magnitudes here.
func format(v float64) string {
	if v == math.Trunc(v) && math.Abs(v) < 1e15 {
		return fmt.Sprintf("%d", int64(v))
	}
	return fmt.Sprintf("%g", v)
}
