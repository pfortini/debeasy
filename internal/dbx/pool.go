package dbx

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/pfortini/debeasy/internal/store"
)

// Pool keeps one Driver per saved-connection id.
// Lazy-opened on first use; auto-closed after IdleTimeout of inactivity.
type Pool struct {
	mu       sync.Mutex
	drivers  map[int64]*entry
	idleTTL  time.Duration
	maxOpen  int
	store    *store.Connections
	stopChan chan struct{}
}

type entry struct {
	driver   Driver
	connID   int64
	lastUsed time.Time
}

func NewPool(connStore *store.Connections) *Pool {
	p := &Pool{
		drivers:  make(map[int64]*entry),
		idleTTL:  5 * time.Minute,
		maxOpen:  10,
		store:    connStore,
		stopChan: make(chan struct{}),
	}
	go p.gc()
	return p
}

func (p *Pool) Stop() {
	close(p.stopChan)
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, e := range p.drivers {
		_ = e.driver.Close()
	}
	p.drivers = nil
}

func (p *Pool) Get(ctx context.Context, connID int64) (Driver, error) {
	p.mu.Lock()
	if e, ok := p.drivers[connID]; ok {
		e.lastUsed = time.Now()
		p.mu.Unlock()
		return e.driver, nil
	}
	p.mu.Unlock()

	c, err := p.store.Get(ctx, connID)
	if err != nil {
		return nil, err
	}
	d, err := openDriver(c, p.maxOpen)
	if err != nil {
		return nil, err
	}
	if err := d.Ping(ctx); err != nil {
		_ = d.Close()
		return nil, fmt.Errorf("ping %s: %w", c.Name, err)
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if existing, ok := p.drivers[connID]; ok {
		_ = d.Close() // someone raced
		existing.lastUsed = time.Now()
		return existing.driver, nil
	}
	p.drivers[connID] = &entry{driver: d, connID: connID, lastUsed: time.Now()}
	return d, nil
}

// Evict closes and removes an entry — call when a connection is updated/deleted.
func (p *Pool) Evict(connID int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if e, ok := p.drivers[connID]; ok {
		_ = e.driver.Close()
		delete(p.drivers, connID)
	}
}

// Test opens a one-shot driver from a Connection (not stored in pool) to validate creds.
func (p *Pool) Test(ctx context.Context, c *store.Connection) error {
	d, err := openDriver(c, 2)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Ping(ctx)
}

// evictIdle closes and removes any driver whose lastUsed is older than idleTTL.
// Exposed as a method (not inline in gc) so tests can drive it deterministically
// without waiting on a ticker.
func (p *Pool) evictIdle() int {
	cutoff := time.Now().Add(-p.idleTTL)
	p.mu.Lock()
	defer p.mu.Unlock()
	evicted := 0
	for id, e := range p.drivers {
		if e.lastUsed.Before(cutoff) {
			_ = e.driver.Close()
			delete(p.drivers, id)
			evicted++
		}
	}
	return evicted
}

func (p *Pool) gc() {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-p.stopChan:
			return
		case <-t.C:
			_ = p.evictIdle()
		}
	}
}

func openDriver(c *store.Connection, maxOpen int) (Driver, error) {
	k, err := ParseKind(c.Kind)
	if err != nil {
		return nil, err
	}
	switch k {
	case KindPostgres:
		return openPostgres(c, maxOpen)
	case KindMySQL:
		return openMySQL(c, maxOpen)
	case KindSQLite:
		return openSQLite(c, maxOpen)
	}
	return nil, fmt.Errorf("unsupported kind %s", c.Kind)
}
