package helper

import (
	"fmt"
	"sync"
)

type BoolSlice struct {
	slice []bool
	mtx   sync.RWMutex
}

func NewBoolSlice(ln int) *BoolSlice {
	return &BoolSlice{
		slice: make([]bool, ln),
	}
}

func (bs *BoolSlice) Set(i int, b bool) {
	bs.mtx.Lock()
	defer bs.mtx.Unlock()
	bs.slice[i] = b
}

func (bs *BoolSlice) Get(i int) bool {
	bs.mtx.Lock()
	defer bs.mtx.Unlock()
	return bs.slice[i]
}

func (bs *BoolSlice) Iterate(f func(k int, v bool)) {
	bs.mtx.RUnlock()
	defer bs.mtx.RUnlock()
	for k, v := range bs.slice {
		f(k, v)
	}
}

func (bs *BoolSlice) CalculateNum(val bool) int {
	bs.mtx.RLock()
	defer bs.mtx.RUnlock()
	nm := 0
	for _, v := range bs.slice {
		if v == val {
			nm++
		}
	}
	return nm
}

func (bs *BoolSlice) String() string {
	bs.mtx.RLock()
	defer bs.mtx.RUnlock()
	return fmt.Sprintf("%v", bs.slice)
}
