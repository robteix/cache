package cache

import (
	"container/list"
	"time"
)

// Element keeps the keyval and the last used time
type Element struct {
	// if this Element is inserted in a list, then this will point to its
	// corresponding list.Element.
	el       *list.Element
	key, val interface{}
	lu       time.Time // last use
}

// Value returns the value of the Element
func (e Element) Value() interface{} {
	return e.val
}
