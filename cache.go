package cache

import (
	"bytes"
	"container/list"
	"encoding/binary"
	"encoding/gob"
	"fmt"
	"hash/fnv"
	"sync"
	"sync/atomic"
	"time"
)

// Cache implements a simple LRU-cache with optional time-to-use. The empty
// value is a cache with no max number of entries and no TTU. It is safe
// for concurrent use
type Cache struct {
	cap     int           // the capacity. If 0, there is no limit
	ttu     time.Duration // time-to-use. If 0, no expiration time.
	coolOff time.Duration // time to wait before pushing a value to front

	initOnce sync.Once // ensure lazy init runs only once

	len int64 // current number of elements held

	l        *list.List
	deleteCh chan *Element
	frontCh  chan *Element
	done     chan struct{}

	shards    []*shard
	numShards int32
}

// New creates a new cache with the provided max number of entries and ttl.
func New(opts ...Option) *Cache {
	c := &Cache{}

	for _, o := range opts {
		o.apply(c)
	}

	return c
}

// lazyInit ensures the object is initialized
func (c *Cache) lazyInit() {
	c.initOnce.Do(func() {
		c.l = list.New()
		c.numShards = 100 // fixed for now
		c.shards = make([]*shard, c.numShards)
		c.frontCh = make(chan *Element, 10000)
		c.deleteCh = make(chan *Element, 10000)
		c.done = make(chan struct{})

		for i := 0; i < int(c.numShards); i++ {
			c.shards[i] = &shard{
				idx: make(map[interface{}]*Element),
			}
		}
		go c.listWorker()
	})
}

// Stop will stop the cache.
func (c *Cache) Stop() {
	close(c.done)
}

// the listWorker is responsable for manipulating the list containing the elements.
func (c *Cache) listWorker() {
	for {
		select {
		case el := <-c.deleteCh:
			if el.el != nil {
				c.l.Remove(el.el)
				c.len--
			}
		case el := <-c.frontCh:
			el.el = c.l.PushFront(el)
			c.len++
		case _, ok := <-c.done:
			if !ok {
				return
			}
		}
	}
}

// Len returns the number of items currently held in the cache. This is an
// approximation, as the exact number of items is counted asynchronosly when the
// worker processes items
func (c *Cache) Len() int {
	len := atomic.LoadInt64(&c.len)
	return int(len)
}

// Cap returns the capacity of this cache
func (c *Cache) Cap() int { return c.cap }

// TTU returns the time-to-use of the cache
func (c *Cache) TTU() time.Duration { return c.ttu }

// Add adds the new keyval pair to the cache. If the key is already present, it
// is updated
func (c *Cache) Add(key, val interface{}) {
	c.lazyInit()

	el, old := c.shard(key).set(key, val)
	if old != nil {
		c.deleteCh <- old
	}
	c.frontCh <- el

	// see if we're over capacity
	if c.cap > 0 && c.l.Len() > c.cap {
		c.removeOldest()
	}
}

// Remove removes an entry from the cache from its key. It returns the cached
// value or nil if not present.
func (c *Cache) Remove(key interface{}) interface{} {
	c.lazyInit()

	el := c.shard(key).remove(key)
	if el != nil {
		c.deleteCh <- el
	}
	return el
}

// Get retrieves an element from the cache. It also returns a second value
// indicating whether the key was found
func (c *Cache) Get(key interface{}) (value interface{}, ok bool) {
	c.lazyInit()

	el := c.shard(key).get(key)
	if el != nil && !c.expired(el) {
		// avoid moving to front too often if we have a coolOff
		if c.coolOff == 0 || time.Since(el.lu) > c.coolOff {
			c.frontCh <- el
		}
		return el.val, true
	}

	return nil, false
}

// shard returns a shard based on the key's hash
func (c *Cache) shard(key interface{}) *shard {
	h := fnv.New32a()

	// we need to turn the key into a byte array. The gob.Encoder is slow, so we
	// try to isolate some common types so we can do a faster conversion if we
	// can
	switch v := key.(type) {
	case string:
		h.Write([]byte(v))
	case []byte:
		h.Write(v)
	case *bool, bool, []bool, *int8, int8, []int8, *uint8,
		uint8, *int16, int16, []int16, *uint16,
		uint16, []uint16, *int32, int32, []int32, *uint32, uint32, []uint32,
		*int64, int64, []int64, *uint64, uint64, []uint64:
		var buf bytes.Buffer
		if err := binary.Write(&buf, binary.LittleEndian, v); err != nil {
			panic(fmt.Sprintf("could not encode %v as bytes", v))
		}
		h.Write(buf.Bytes())
	default:
		// this is slow
		var buf bytes.Buffer
		enc := gob.NewEncoder(&buf)
		err := enc.Encode(v)
		if err != nil {
			panic(fmt.Sprintf("could not encode type %T as bytes", key))
		}
		h.Write(buf.Bytes())
	}

	return c.shards[h.Sum32()&uint32(c.numShards-1)]
}

// Purge will remove entries that are expired
func (c *Cache) Purge() int {
	c.lazyInit()

	if c.l.Len() == 0 {
		return 0
	}
	var expired int
	if c.ttu != time.Duration(0) {
		for {
			b := c.l.Back()
			if b == nil {
				break // no more items
			}
			el := b.Value.(*Element)
			if !c.expired(el) {
				break // no more expired items
			}
			el = c.shard(el.key).remove(el.key)
			if el != nil {
				c.deleteCh <- el
			}

			expired++
		}
	}
	return expired
}

// removes the oldest element in the cache. Caller must hold the mutex for writing
func (c *Cache) removeOldest() (key, value interface{}) {
	b := c.l.Back()
	if b == nil {
		return
	}

	el := b.Value.(*Element)
	c.Remove(el.key)

	return el.key, el.val
}

// helper function to check if a cacheEntry is expired. Caller should hold the
// mutex for reading
func (c *Cache) expired(ce *Element) bool {
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
