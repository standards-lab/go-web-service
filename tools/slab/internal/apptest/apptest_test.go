package apptest_test

import (
	"slices"
	"strconv"
	"sync"
	"testing"

	"github.com/standards-lab/go-web-service/tools/slab/internal/apptest"
)

func TestRecorder_LogsInOrder(t *testing.T) {
	var r apptest.Recorder
	if log := r.Log(); len(log) != 0 {
		t.Errorf("zero Recorder Log() = %q, want empty", log)
	}

	r.Record("a")
	r.Record("b")
	r.Record("a")

	if want := []string{"a", "b", "a"}; !slices.Equal(r.Log(), want) {
		t.Errorf("Log() = %q, want %q", r.Log(), want)
	}
}

func TestRecorder_LogIsACopy(t *testing.T) {
	var r apptest.Recorder
	r.Record("a")

	log := r.Log()
	log[0] = "changed"
	r.Record("b")

	if want := []string{"a", "b"}; !slices.Equal(r.Log(), want) {
		t.Errorf("Log() = %q, want %q", r.Log(), want)
	}
	if log[0] != "changed" || len(log) != 1 {
		t.Errorf("earlier Log() = %q, want it unchanged by later Records", log)
	}
}

// Run under -race, concurrent Records prove the Recorder's lock.
func TestRecorder_ConcurrentRecords(t *testing.T) {
	var r apptest.Recorder
	var wg sync.WaitGroup
	const n = 50
	for i := range n {
		wg.Go(func() { r.Record(strconv.Itoa(i)) })
	}
	wg.Wait()

	if got := len(r.Log()); got != n {
		t.Errorf("len(Log()) = %d, want %d", got, n)
	}
}
