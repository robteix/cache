package cache

import (
	"bytes"
	"encoding/binary"
	"encoding/gob"
	"fmt"
	"hash/fnv"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// Cache implements a simple LRU-cache with optional time-to-use. The empty
// value is a cache with no max number of entries and no TTU. It is safe
// for concurrent use
type Cache struct {
	cap int           // the capacity. If 0, there is no limit
	ttu time.Duration // time-to-use. If 0, no expiration time.

	nshards int32    // number of shards to use
	shards  []*shard // the shards

	mu sync.RWMutex // protects the following fields
}

// Byter is implemented by any valie that has a Bytes method, which should
// returns a byte representation of the value. The Bytes method is used by to
// identify the correct shard of a given key.
type Byter interface {
	Bytes() []byte
}

// cacheEntry keeps the keyval and the last used time
type cacheEntry struct {
	key, val interface{}
	lu       time.Time // last used time
}

// New creates a new cache with the provided max number of entries and ttl.
func New(opts ...Option) *Cache {
	c := &Cache{nshards: 1}

	for _, o := range opts {
		o.apply(c)
	}

	c.shards = make([]*shard, c.nshards)
	for i := range c.shards {
		c.shards[i] = newShard(c)
	}

	return c
}

// init ensures the object is initialized
func (c *Cache) init() {
	if atomic.LoadInt32(&c.nshards) != 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.nshards == 0 {
		c.shards = []*shard{newShard(c)}
		atomic.StoreInt32(&c.nshards, 1)
	}
}

// Len returns the number of entries currently held in the cache
func (c *Cache) Len() int {
	c.init()

	c.mu.RLock()
	defer c.mu.RUnlock()

	l := 0
	for i := range c.shards {
		l += c.shards[i].l.Len()
	}

	return l
}

// Cap returns the capacity of this cache
func (c *Cache) Cap() int { return c.cap }

// TTU returns the time-to-use of the cache
func (c *Cache) TTU() time.Duration { return c.ttu }

// Add adds the new keyval pair to the cache. If the key is already present, it
// is updated
func (c *Cache) Add(key, val interface{}) {
	c.init()
	c.shard(key).add(key, val)
}

// Remove removes an entry from the cache from its key. It returns the cached
// value or nil if not present.
func (c *Cache) Remove(key interface{}) interface{} {
	c.init()
	return c.shard(key).remove(key)
}

// Get retrieves an element from the cache. It also returns a second value
// indicating whether the key was found
func (c *Cache) Get(key interface{}) (value interface{}, ok bool) {
	c.init()
	return c.shard(key).get(key)
}

// Purge will remove entries that are expired
func (c *Cache) Purge() int {
	c.init()

	c.mu.Lock()
	defer c.mu.Unlock()
	expired := 0
	for _, s := range c.shards {
		expired += s.purge()
	}
	return expired
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
	c.init()
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

// length of int in bytes
var il = strconv.IntSize / 8

// stringer is implemented by any value that has a String method. The String
// method should return a string representation of the value. This internal
// interface is here only to avoid a dependency to fmt.Stringer
type stringer interface {
	String() string
}

func (c *Cache) shard(key interface{}) *shard {
	h := fnv.New32a() // used to hash a byte array

	// try to get a bytes representation of the key any way we can, in order
	// from fastest to slowest
	switch v := key.(type) {
	case []byte:
		h.Write(v)
	case Byter:
		h.Write(v.Bytes())
	case string:
		h.Write([]byte(v))
	case stringer:
		h.Write([]byte(v.String()))
	case int:
		h.Write(intBytes(v))
	case *int:
		h.Write(intBytes(*v))
	case *bool, bool, []bool, *int8, int8, []int8, *uint8,
		uint8, *int16, int16, []int16, *uint16,
		uint16, []uint16, *int32, int32, []int32, *uint32, uint32, []uint32,
		*int64, int64, []int64, *uint64, uint64, []uint64:
		h.Write(toBytes(v))
	default:
		// the user is using an unknown type as the key, so we're now grasping
		// at straws here. This will be at least an order of magnitude slower
		// then the options above.
		var buf bytes.Buffer
		enc := gob.NewEncoder(&buf)
		err := enc.Encode(v)
		if err != nil {
			panic(fmt.Sprintf("could not encode type %T as bytes", key))
		}
		h.Write(buf.Bytes())
	}

	return c.shards[h.Sum32()&uint32(c.nshards-1)]
}

func toBytes(v interface{}) []byte {
	var buf bytes.Buffer
	if err := binary.Write(&buf, binary.LittleEndian, v); err != nil {
		panic(fmt.Sprintf("could not encode %v as bytes: %v", v, err))
	}
	return buf.Bytes()
}

// helper function to quickly turn an int into a byte slice
func intBytes(i int) []byte {
	b := make([]byte, il)
	b[0] = byte(i)
	b[1] = byte(i >> 8)
	b[2] = byte(i >> 16)
	b[3] = byte(i >> 24)
	if il == 8 {
		b[4] = byte(i >> 32)
		b[5] = byte(i >> 40)
		b[6] = byte(i >> 48)
		b[7] = byte(i >> 56)
	}
	return b
}
