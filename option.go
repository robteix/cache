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

// WithCapacity configures the max capacity of the cache. If cap is 0, then
// there is no set capacity and the cache will grow indefinely
func WithCapacity(cap int) Option {
	return optionFunc(func(c *Cache) {
		c.cap = cap
	})
}

// WithTTU configures the cache to expire elements older than the provided
// time-to-use (TTU)
func WithTTU(ttu time.Duration) Option {
	return optionFunc(func(c *Cache) {
		c.ttu = ttu
	})
}
