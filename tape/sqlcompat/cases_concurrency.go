package sqlcompat

import (
	"fmt"
	"sync"
	"testing"

	"github.com/seanly/dmr-devkit/config"
	"github.com/seanly/dmr-devkit/tape"
)

// RunConcurrencyCases executes T13 for one driver.
func RunConcurrencyCases(t *testing.T, driver Driver, rep *Report) {
	t.Helper()
	err := caseT13ConcurrentAppend(t, driver)
	if err != nil {
		rep.Add("T13", driver, StatusFail, err.Error(), false)
		t.Logf("[%s/T13] %v", driver, err)
		return
	}
	rep.Add("T13", driver, StatusPass, "", false)
}

func caseT13ConcurrentAppend(t *testing.T, driver Driver) error {
	store, err := openTapeStoreOrSkip(t, driver, config.FTS5False)
	if err != nil {
		return err
	}
	const (
		workers = 10
		per     = 20
	)
	var wg sync.WaitGroup
	errCh := make(chan error, workers)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for i := 0; i < per; i++ {
				if err := store.Append("conc", tape.TapeEntry{
					Kind:    "message",
					Payload: map[string]any{"content": fmt.Sprintf("w%d-i%d", n, i)},
				}); err != nil {
					errCh <- err
					return
				}
			}
		}(w)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			return err
		}
	}
	entries, err := store.FetchAll("conc", nil)
	if err != nil {
		return err
	}
	want := workers * per
	if len(entries) != want {
		return fmt.Errorf("concurrent append: want %d rows, got %d", want, len(entries))
	}
	for i := 1; i < len(entries); i++ {
		if entries[i].ID <= entries[i-1].ID {
			return fmt.Errorf("IDs not monotonic at index %d: %d <= %d", i, entries[i].ID, entries[i-1].ID)
		}
	}
	return nil
}
