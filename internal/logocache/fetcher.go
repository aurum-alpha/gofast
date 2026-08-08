package logocache

import (
	"context"

	"github.com/j27-aurum/gofast/internal/model"
)

type fetchJob struct {
	ctx       context.Context
	provider  model.ProviderID
	file      string
	sourceURL string
	headers   map[string]string
	forceFull bool
	result    chan error
}

type fetchWait struct {
	done chan struct{}
	err  error
}

func (c *Cache) startWorkers(n int) {
	if n <= 0 {
		n = defaultWorkers
	}
	for i := 0; i < n; i++ {
		c.wg.Add(1)
		go func() {
			defer c.wg.Done()
			for {
				select {
				case <-c.stop:
					return
				case job, ok := <-c.jobs:
					if !ok {
						return
					}
					err := c.fetchToDisk(job.ctx, job.provider, job.file, job.sourceURL, job.headers, job.forceFull)
					job.result <- err
				}
			}
		}()
	}
}

// waitFetch runs a disk fetch through the worker pool, coalescing duplicate keys.
func (c *Cache) waitFetch(ctx context.Context, provider model.ProviderID, file, sourceURL string, headers map[string]string, forceFull bool) error {
	key := string(provider) + "/" + file + "\n" + sourceURL

	c.mu.Lock()
	if w, ok := c.inflight[key]; ok {
		c.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-w.done:
			return w.err
		}
	}
	w := &fetchWait{done: make(chan struct{})}
	c.inflight[key] = w
	c.mu.Unlock()

	job := &fetchJob{
		ctx:       ctx,
		provider:  provider,
		file:      file,
		sourceURL: sourceURL,
		headers:   headers,
		forceFull: forceFull,
		result:    make(chan error, 1),
	}
	select {
	case <-ctx.Done():
		c.finishInflight(key, w, ctx.Err())
		return ctx.Err()
	case <-c.stop:
		c.finishInflight(key, w, context.Canceled)
		return context.Canceled
	case c.jobs <- job:
	}

	var err error
	select {
	case <-ctx.Done():
		err = ctx.Err()
		select {
		case e := <-job.result:
			if err == nil {
				err = e
			}
		case <-c.stop:
		}
	case err = <-job.result:
	}
	c.finishInflight(key, w, err)
	return err
}

func (c *Cache) finishInflight(key string, w *fetchWait, err error) {
	c.mu.Lock()
	if cur := c.inflight[key]; cur == w {
		delete(c.inflight, key)
	}
	w.err = err
	close(w.done)
	c.mu.Unlock()
}
