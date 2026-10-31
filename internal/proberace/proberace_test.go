// Package proberace is a temporary CI probe (unmerged): seeds a data race.
package proberace

import (
	"sync"
	"testing"
)

// TestDeliberateRace passes without -race and fails under -race.
func TestDeliberateRace(t *testing.T) {
	t.Parallel()

	counter := 0

	var wg sync.WaitGroup

	for range 4 {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for range 1000 {
				counter++
			}
		}()
	}

	wg.Wait()
	t.Log(counter)
}
