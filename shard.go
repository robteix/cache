package cache

import (
	"container/list"
	"sync"
	"time"
)

// type baba struct{}

// func (b *baba) Lock() {
// 	fmt.Println("lock")
// }
// func (b *baba) Unlock() {
// 	fmt.Println("unlock")
// }

type shard struct {
	sync.Mutex
	l   *list.List                    // the element list
	idx map[interface{}]*list.Element // the list index
	c   *Cache                        // reference to the parent cache
}

func newShard(c *Cache) *shard {
	return &shard{
		c:   c,
		idx: make(map[interface{}]*list.Element),
		l:   list.New(),
	}
}

func (s *shard) get(key interface{}) (interface{}, bool) {
	s.Lock()
	defer s.Unlock()

	if el, found := s.idx[key]; found && !s.expired(el.Value.(*cacheEntry)) {
		s.l.MoveToFront(el)
		el.Value.(*cacheEntry).lu = time.Now()
		return el.Value.(*cacheEntry).val, true
	}

	return nil, false
}

// helper function to check if a cacheEntry is expired. Caller should hold the
// mutex for reading
func (s *shard) expired(ce *cacheEntry) bool {
	if s.c.ttu == time.Duration(0) {
		return false // no expiration
	}
	return ce.lu.Add(s.c.ttu).Before(time.Now())
}

// sets the value of a key. If the key was found, the element is returned.
func (s *shard) add(key, val interface{}) *list.Element {
	s.Lock()
	defer s.Unlock()

	// check if already in the cache?
	if el, ok := s.idx[key]; ok {
		s.l.MoveToFront(el)
		el.Value.(*cacheEntry).val = val
		el.Value.(*cacheEntry).lu = time.Now()
		return el
	}

	el := s.l.PushFront(&cacheEntry{key, val, time.Now()})
	s.idx[key] = el

	// see if we're over capacity
	if s.c.cap > 0 && s.l.Len() > s.c.cap {
		s.removeOldest()
	}
	return el
}

// removes entries that are expired
func (s *shard) purge() int {
	s.Lock()
	defer s.Unlock()

	if s.l.Len() == 0 {
		return 0
	}
	var expired int
	if s.c.ttu != time.Duration(0) {
		for {
			el := s.l.Back()
			if el == nil {
				break // no more items
			}
			ce := el.Value.(*cacheEntry)
			if !s.expired(ce) {
				break // no more expired items
			}
			s.removeElement(el)
			expired++
		}
	}
	return expired
}

func (s *shard) remove(key interface{}) interface{} {
	s.Lock()
	defer s.Unlock()

	if el, found := s.idx[key]; found {
		_, value := s.removeElement(el)
		return value
	}

	return nil
}

// removes the oldest element in the cache. Caller must hold the mutex for writing
func (s *shard) removeOldest() (key, value interface{}) {
	el := s.l.Back()
	if el == nil {
		return
	}

	return s.removeElement(el)
}

func (s *shard) removeElement(el *list.Element) (key, value interface{}) {
	s.l.Remove(el)
	e := el.Value.(*cacheEntry)
	delete(s.idx, e.key)
	return e.key, e.val
}
