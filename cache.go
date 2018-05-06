package cache

import (
	"container/list"
	"sync"
	"time"
)

// Cache implements a simple LRU-cache with optional time-to-use. The empty
// value is a cache with no max number of entries and no TTU. It is safe
// for concurrent use
type Cache struct {
	cap int           // the capacity. If 0, there is no limit
	ttu time.Duration // time-to-use. If 0, no expiration time.

	mu  sync.RWMutex // protects the following fields
	l   *list.List
	idx map[interface{}]*list.Element // the list index
}

// cacheEntry keeps the keyval and the last used time
type cacheEntry struct {
	key, val interface{}
	lu       time.Time // last used time
}

// New creates a new cache with the provided max number of entries and ttl.
func New(opts ...Option) *Cache {
	c := &Cache{
		l:   list.New(),
		idx: make(map[interface{}]*list.Element),
	}

	for _, o := range opts {
		o.apply(c)
	}

	return c
}

// init ensures the object is initialized
func (c *Cache) init() {
	if c.l == nil {
		c.l = list.New()
		c.idx = make(map[interface{}]*list.Element)
	}
}

// Len returns the number of entries currently held in the cache
func (c *Cache) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	c.init()
	return c.l.Len()
}

// Cap returns the capacity of this cache
func (c *Cache) Cap() int { return c.cap }

// TTU returns the time-to-use of the cache
func (c *Cache) TTU() time.Duration { return c.ttu }

// Add adds the new keyval pair to the cache. If the key is already present, it
// is updated
func (c *Cache) Add(key, val interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.init()

	// check if already in the cache?
	if el, ok := c.idx[key]; ok {
		c.l.MoveToFront(el)
		el.Value.(*cacheEntry).val = val
		el.Value.(*cacheEntry).lu = time.Now()
		return
	}

	el := c.l.PushFront(&cacheEntry{key, val, time.Now()})
	c.idx[key] = el

	// see if we're over capacity
	if c.cap > 0 && c.l.Len() > c.cap {
		c.removeOldest()
	}
}

// Remove removes an entry from the cache from its key. It returns the cached
// value or nil if not present.
func (c *Cache) Remove(key interface{}) interface{} {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.init()
	if el, found := c.idx[key]; found {
		_, value := c.remove(el)
		return value
	}
	return nil
}

// Get retrieves an element from the cache. It also returns a second value
// indicating whether the key was found
func (c *Cache) Get(key interface{}) (value interface{}, ok bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	c.init()

	if el, found := c.idx[key]; found && !c.expired(el.Value.(*cacheEntry)) {
		c.l.MoveToFront(el)
		el.Value.(*cacheEntry).lu = time.Now()
		return el.Value.(*cacheEntry).val, true
	}

	return nil, false
}

// Purge will remove entries that are expired
func (c *Cache) Purge() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.init()

	if c.l.Len() == 0 {
		return 0
	}
	var expired int
	if c.ttu != time.Duration(0) {
		for {
			el := c.l.Back()
			if el == nil {
				break // no more items
			}
			ce := el.Value.(*cacheEntry)
			if !c.expired(ce) {
				break // no more expired items
			}
			c.remove(el)
			expired++
		}
	}
	return expired
}

// removes the oldest element in the cache. Caller must hold the mutex for writing
func (c *Cache) removeOldest() (key, value interface{}) {
	el := c.l.Back()
	if el == nil {
		return
	}

	return c.remove(el)
}

func (c *Cache) remove(el *list.Element) (key, value interface{}) {
	c.l.Remove(el)
	e := el.Value.(*cacheEntry)
	delete(c.idx, e.key)
	return e.key, e.val
}

// helper function to check if a cacheEntry is expired. Caller should hold the
// mutex for reading
func (c *Cache) expired(ce *cacheEntry) bool {
	if c.ttu == time.Duration(0) {
		return false // no expiration
	}
	return ce.lu.Add(c.ttu).Before(time.Now())
}

// StartPurger is a helper function that starts a goroutine to periodically call
// Purge() at the provided freq. The returned stop function must be called to
// stop the purger, otherwise the garbage collector will not be able to free it
// and it will "leak".
//
// Also, the freq can have a detrimental effect on performance as the purger
// must lock the entire cache while it purges the cache. Since the Cache will
// ignore expired items, the need for frequent purges is greatly reduced.
func (c *Cache) StartPurger(freq time.Duration) (stop func()) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.ttu == time.Duration(0) {
		return func() {} // we don't need a purger if we don't have expiration
	}

	ticker := time.NewTicker(freq)
	done := make(chan bool)
	go func() {
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				c.Purge()
			}
		}
	}()

	stopFn := func() {
		ticker.Stop()
		done <- true
	}

	return stopFn
}
