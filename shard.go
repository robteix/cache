package cache

import (
	"sync"
	"time"
)

type shard struct {
	sync.RWMutex
	idx map[interface{}]*Element // the list index
}

func newShard() *shard {
	return &shard{
		idx: make(map[interface{}]*Element),
	}
}

func (s *shard) get(key interface{}) *Element {
	s.RLock()
	defer s.RUnlock()
	return s.idx[key]
}

// sets the value of a key. If the key was found, the element is returned in found.
func (s *shard) set(key, val interface{}) (el *Element, found *Element) {
	el = &Element{key: key, val: val, lu: time.Now()}
	s.Lock()
	defer s.Unlock()
	found = s.idx[key]
	s.idx[key] = el
	return el, found
}

func (s *shard) remove(key interface{}) *Element {
	s.Lock()
	defer s.Unlock()
	el := s.idx[key]
	delete(s.idx, key)
	return el
}
