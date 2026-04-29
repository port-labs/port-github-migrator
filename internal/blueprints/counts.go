package blueprints

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/briandowns/spinner"
	"github.com/port-labs/port-github-migrator/internal/port"
)

const fetchConcurrency = 8

// Count is a single blueprint's entity count (or the error encountered while fetching it).
type Count struct {
	Name  string
	Count int
	Err   error
}

// FetchCounts loads the blueprints reachable from the given installation and
// concurrently counts the entities ingested under each, with a live spinner
// rendered to spinnerOut (typically stderr).
func FetchCounts(client *port.Client, oldInstallID string, spinnerOut io.Writer) ([]Count, error) {
	blueprints, err := client.GetBlueprintsByDataSource(oldInstallID)
	if err != nil {
		return nil, fmt.Errorf("failed to get blueprints: %w", err)
	}

	sort.Strings(blueprints)

	results := make([]Count, len(blueprints))
	sem := make(chan struct{}, fetchConcurrency)
	var wg sync.WaitGroup

	searchOpts := &port.SearchOptions{
		IncludeProperties: false,
		IncludeRelations:  false,
		EnforceTotalLimit: false,
	}

	s := spinner.New(spinner.CharSets[14], 100*time.Millisecond)
	s.Writer = spinnerOut
	s.HideCursor = true

	var (
		progressMu sync.Mutex
		inFlight   = make(map[string]bool)
		completed  int
	)
	refreshSuffix := func() {
		names := make([]string, 0, len(inFlight))
		for n := range inFlight {
			names = append(names, n)
		}
		sort.Strings(names)
		const preview = 3
		var label string
		if len(names) <= preview {
			label = strings.Join(names, ", ")
		} else {
			label = strings.Join(names[:preview], ", ") + fmt.Sprintf(" (+%d more)", len(names)-preview)
		}
		s.Lock()
		s.Suffix = fmt.Sprintf(" Fetching entities (%d/%d) — %s", completed, len(blueprints), label)
		s.Unlock()
	}

	refreshSuffix()
	s.Start()

	for i, bp := range blueprints {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, bp string) {
			defer wg.Done()
			defer func() { <-sem }()

			progressMu.Lock()
			inFlight[bp] = true
			refreshSuffix()
			progressMu.Unlock()

			entities, err := client.SearchOldEntitiesByBlueprint(bp, oldInstallID, searchOpts)

			progressMu.Lock()
			delete(inFlight, bp)
			completed++
			refreshSuffix()
			progressMu.Unlock()

			if err != nil {
				results[i] = Count{Name: bp, Err: err}
				return
			}
			results[i] = Count{Name: bp, Count: len(entities)}
		}(i, bp)
	}
	wg.Wait()
	s.Stop()

	return results, nil
}

// PrintCounts renders the standard NAME / ENTITIES table. When includeEmpty is
// false, blueprints with 0 entities are omitted.
func PrintCounts(w io.Writer, counts []Count, includeEmpty bool) {
	fmt.Fprintln(w, "NAME                              ENTITIES")
	fmt.Fprintln(w, "──────────────────────────────────────────")
	for _, r := range counts {
		if r.Err != nil {
			fmt.Fprintf(w, "%-33s %s\n", r.Name, r.Err.Error())
			continue
		}
		if r.Count == 0 && !includeEmpty {
			continue
		}
		fmt.Fprintf(w, "%-33s %d\n", r.Name, r.Count)
	}
}
