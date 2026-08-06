package stirr

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
)

// auditDeadIDs POSTs /playable + probes masters for each videoid. IDs that
// return Aniview CON (deleted SSAI config) are returned. Transient errors are
// ignored so a flaky CDN does not empty the lineup.
func auditDeadIDs(ctx context.Context, resolver *Resolver, ids []string) []string {
	if resolver == nil || len(ids) == 0 {
		return nil
	}
	var (
		mu   sync.Mutex
		dead []string
		wg   sync.WaitGroup
		sem  = make(chan struct{}, providerEPGWorkers)
	)
	for _, id := range ids {
		id := id
		if id == "" {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			_, err := resolver.Resolve(ctx, id)
			if errors.Is(err, ErrDeadSSAI) {
				mu.Lock()
				dead = append(dead, id)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return dead
}

func encodeDeadIDs(ids []string) []byte {
	if len(ids) == 0 {
		return []byte("[]")
	}
	b, err := json.Marshal(ids)
	if err != nil {
		return []byte("[]")
	}
	return b
}

func decodeDeadIDs(raw []byte) map[string]struct{} {
	out := map[string]struct{}{}
	if len(raw) == 0 {
		return out
	}
	var ids []string
	if err := json.Unmarshal(raw, &ids); err != nil {
		return out
	}
	for _, id := range ids {
		if id != "" {
			out[id] = struct{}{}
		}
	}
	return out
}
