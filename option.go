package cache

import (
	"time"
)

// Option configures the Cache
type Option interface {
	apply(*Cache)
}

// helper Option implementation to quickly define new options
type optionFunc func(*Cache)

func (f optionFunc) apply(c *Cache) {
	f(c)
}

// WithCapacity configures the max capacity of each shard. If cap is 0, then
// there is no set capacity and the cache will grow indefinely
func WithCapacity(cap int) Option {
	return optionFunc(func(c *Cache) {
		c.cap = cap
	})
}

// WithShards configures the number of shards to split the cache. This number
// must be larger than 0. By default, the cache uses a single shard.
func WithShards(n int32) Option {
	return optionFunc(func(c *Cache) {
		if n < 1 {
			panic("the number of shards must be larger than 0")
		}
		c.nshards = n
	})
}

// WithTTU configures the cache to expire elements older than the provided
// time-to-use (TTU)
func WithTTU(ttu time.Duration) Option {
	return optionFunc(func(c *Cache) {
		c.ttu = ttu
	})
}
