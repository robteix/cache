package cache_test

import (
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/rselbach/cache"
)

func TestEmptyValue(t *testing.T) {
	var c cache.Cache

	c.Add("hello", "world")
	if c.Len() != 1 {
		t.Errorf("got len() %d, want 1", c.Len())
	}

	if v, ok := c.Get("hello"); !ok {
		t.Error("could not read key hello")
	} else if v != "world" {
		t.Errorf("got val %v, want world", v)
	}

}

func TestCache_Add(t *testing.T) {
	c := cache.New()
	for i := 1; i < 1000; i++ {
		c.Add(i, i)
		if c.Len() != i {
			t.Errorf("got len() %d, want %d", c.Len(), i)
		}
	}

	tests := []struct{ entries, max, len int }{
		{0, 0, 0},
		{10, 2, 2},      // more entries than capacity
		{10, 11, 10},    // less entries than capacity
		{100, 100, 100}, // entries == max
	}

	for _, test := range tests {
		c = cache.New(cache.WithCapacity(test.max))
		for i := 0; i < test.entries; i++ {
			c.Add(i, i)
		}
		if c.Len() != test.len {
			t.Errorf("capacity: got len() %d, want %d", c.Len(), test.len)
		}
	}
}

func TestCache_Get(t *testing.T) {
	c := cache.New()
	c.Add(1, 2)
	i, ok := c.Get(1)
	if !ok {
		t.Error("could not retrieve value")
	}
	if i != 2 {
		t.Errorf("got %v, want 2", i)
	}
}

func TestCache_GetExpired(t *testing.T) {
	c := cache.New(cache.WithTTU(1 * time.Second))
	c.Add(1, 2)
	c.Purge() // too soon to expire
	if c.Len() != 1 {
		t.Errorf("got len() %d, want 1", c.Len())
	}

	time.Sleep(1 * time.Second)
	c.Purge() // should expire
	if c.Len() != 0 {
		t.Errorf("got len() %d, want 0", c.Len())
	}
}

func ExampleNew() {
	// create a new cache with a time-to-use of half a second
	c := cache.New(cache.WithTTU(500 * time.Millisecond))

	// add something to the cache
	c.Add("hello", "world")

	// tries to retrieve the value from the key
	v, ok := c.Get("hello")
	fmt.Println("v", v, "ok", ok)

	// sleep so the item expires
	time.Sleep(1 * time.Second)
	c.Purge() // purges cache of old items

	// tries to retrieve the value again
	v, ok = c.Get("hello")
	fmt.Println("v", v, "ok", ok)
	// Output: v world ok true
	// v <nil> ok false

}

func ExampleCache_StartPurger() {
	// create a cache with a half a 1-second TTU and maximum capacity of 100
	// entries
	c := cache.New(cache.WithTTU(1*time.Second), cache.WithCapacity(100))

	// start an aggressive purger that will run every second
	stop := c.StartPurger(1 * time.Second)
	defer stop()

	// add 200 several entries
	for i := 0; i < 200; i++ {
		c.Add(i, i)
	}

	// note that even though we added 200 entries, it will only hold the last
	// 100 due to the capacity limit
	fmt.Println("Len:", c.Len())

	// wait for the purger to remove expired items (all of them as the
	// TTU was 1s)
	time.Sleep(3 * time.Second)

	// now Len() is 0 as all entries have expired and were automatically purged
	fmt.Println("Len:", c.Len())
	// Output: Len: 100
	// Len: 0
}

// creates a slice of random ints
func randS(n int) []int {
	is := make([]int, n)
	for i := 0; i < n; i++ {
		is[i] = rand.Int()
	}

	return is
}

type test struct {
	name string
	opts []cache.Option
}

func mkTests() []test {
	tests := []test{}
	for shards := int32(1); shards <= 100; shards *= 10 {
		for cap := 1000; cap <= 100000; cap *= 10 {
			tests = append(tests, test{
				name: fmt.Sprintf("%d-cap-%d-shards", cap, shards),
				opts: []cache.Option{cache.WithCapacity(cap), cache.WithShards(shards)},
			})
		}
	}

	return tests
}

func BenchmarkAdd(b *testing.B) {
	tests := mkTests()

	for _, tst := range tests {
		b.Run(tst.name, func(b *testing.B) {
			opts := append([]cache.Option{cache.WithShards(1000)}, tst.opts...)
			c := cache.New(opts...)
			s := randS(b.N)

			b.ResetTimer()
			for n := 0; n < b.N; n++ {
				c.Add(s[n], s[n])
			}
		})
	}
}

// exists to prevent the compiler from optimizing c.Get calls away
var result int

type byterInt int64

func (bi byterInt) Bytes() []byte {
	return []byte{1, 2, 3, 4}
}

func BenchmarkGet(b *testing.B) {
	tests := mkTests()
	for _, tst := range tests {
		b.Run(tst.name, func(b *testing.B) {
			opts := append([]cache.Option{cache.WithShards(1000)}, tst.opts...)
			c := cache.New(opts...)
			s := randS(b.N)
			for n := 0; n < b.N; n++ {
				c.Add(s[n], s[n])
			}

			b.ResetTimer()

			var r int
			for n := 0; n < b.N; n++ {
				i, ok := c.Get(s[n])
				if ok {
					r = i.(int)
				}
			}
			result += r
		})
	}
}
