package blueprints

import (
	"sync"

	"github.com/port-labs/port-github-migrator/internal/port"
)

const searchConcurrency = 5

// CountOldEntities searches old-installation entities per blueprint with bounded concurrency.
func CountOldEntities(client *port.Client, blueprintIDs []string, oldInstallID string) (map[string]int, map[string]error) {
	counts := make(map[string]int, len(blueprintIDs))
	errs := make(map[string]error)
	var (
		countsMu sync.Mutex
		errsMu   sync.Mutex
		wg       sync.WaitGroup
	)
	sem := make(chan struct{}, searchConcurrency)

	for _, bp := range blueprintIDs {
		bp := bp
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			entities, err := client.SearchOldEntitiesByBlueprint(bp, oldInstallID)
			if err != nil {
				errsMu.Lock()
				errs[bp] = err
				errsMu.Unlock()
				return
			}

			countsMu.Lock()
			counts[bp] = len(entities)
			countsMu.Unlock()
		}()
	}

	wg.Wait()
	return counts, errs
}
