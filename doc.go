// Package cache offers a time aware least-recently-used cache implementation
//
// Cache implements an least recently used cache with optional capacity and
// time-to-use. The empty value is usable as a cache that never expires and has
// no limit on the number of entries.
//
// Initialization
//
// For example, the following code will work:
//
//    c := &cache.Cache{}
//    c.Add("hello", "world")
//    if v, ok := c.Get("hello"); ok {
//       log.Println(v)
//    }
//
// However, a more useful cache would have a set capacity and/or a TTU so that
// it won't grow forever. The initialization function can be used along with
// functional parameters to configure a more useful Cache:
//
//    c := cache.New(cache.WithTTU(30 * time.Second), cache.WithCapacity(100))
//
// The above will limit the cache to a maximum number of 100 entries and the
// entries are considered expired if not access within 30s.
//
// Also note that since TTU is optional, one can create an LRU cache (not time
// aware) by omitting the time-to-use:
//
//    c := cache.New(cache.WithCapacity(1000))
//
// Purging
//
// When using a TTU, the cache needs to be purged periodically of expired
// entries. The Purge() method will remove any expired entries before returning.
// It is recommended that you run Purge() periodically to avoid accummulating
// expired entries. You can do that in a goroutine like the naive example below:
//
//    c := cache.New(cache.WithTTU(30 * time.Minute))
//    ctx, cancel = context.WithCancel(ctx)
//    defer cancel()
//    go func() {
//        for {
//           select {
//              case <-time.After(1 * time.Minute):
//                 c.Purge()
//              case <-ctx.Done():
//                 return
//           }
//        }
//    }()
//
// There is also a helper function that uses a time.Tick to run it at a given frequency:
//
//   c :=  cache.New(cache.WithTTU(30 * time.Minute))
//   stop := c.StartPurger(1 * time.Minute)
//   defer stop()
package cache
