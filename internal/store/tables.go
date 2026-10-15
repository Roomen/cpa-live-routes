package store

import "hash/maphash"

// slot is one dictionary entry. ver is the event seq at which the entry was
// created or last changed; refs counts the records and entries pointing at it.
type slot[K comparable, V any] struct {
	key  K
	val  V
	refs int32
	ver  uint64
}

// table interns values by key and hands out IDs starting at 1. Slots live in a
// slice that drops its dead prefix, so memory follows the live entries.
type table[K comparable, V any] struct {
	idx   map[uint64]int32
	slots []*slot[K, V]
	base  int32 // ID of slots[0]
	next  int32
}

func newTable[K comparable, V any]() table[K, V] {
	return table[K, V]{idx: map[uint64]int32{}, base: 1}
}

func (t *table[K, V]) find(k K) (int32, *slot[K, V]) {
	if id, ok := t.idx[maphash.Comparable(hashSeed, k)]; ok {
		if sl := t.get(id); sl != nil && sl.key == k {
			return id, sl
		}
	}
	return 0, nil
}

func (t *table[K, V]) add(k K, v V, ver uint64) int32 {
	t.next++
	t.idx[maphash.Comparable(hashSeed, k)] = t.next
	t.slots = append(t.slots, &slot[K, V]{key: k, val: v, ver: ver})
	return t.next
}

func (t *table[K, V]) get(id int32) *slot[K, V] {
	if i := int(id - t.base); i >= 0 && i < len(t.slots) {
		return t.slots[i]
	}
	return nil
}

func (t *table[K, V]) acquire(id int32) {
	if sl := t.get(id); sl != nil {
		sl.refs++
	}
}

// release drops one reference and returns the removed slot when it was the last.
func (t *table[K, V]) release(id int32) *slot[K, V] {
	sl := t.get(id)
	if sl == nil {
		return nil
	}
	if sl.refs--; sl.refs > 0 {
		return nil
	}
	t.slots[id-t.base] = nil
	if h := maphash.Comparable(hashSeed, sl.key); t.idx[h] == id {
		delete(t.idx, h)
	}
	n := 0
	for n < len(t.slots) && t.slots[n] == nil {
		n++
	}
	if n > 0 {
		if n == len(t.slots) {
			t.slots = t.slots[:0]
		} else if n >= 64 && n*2 >= len(t.slots) {
			t.slots = append([]*slot[K, V](nil), t.slots[n:]...)
		} else {
			return sl
		}
		t.base += int32(n)
	}
	return sl
}

func (t *table[K, V]) ids(keep func(*slot[K, V]) bool) []int32 {
	out := make([]int32, 0, len(t.slots))
	for i, sl := range t.slots {
		if sl != nil && (keep == nil || keep(sl)) {
			out = append(out, t.base+int32(i))
		}
	}
	return out
}

type clientKey struct {
	kind, label string
	key         int32
	ip          string
}

type clientVal struct {
	ua  string
	key int32
}

type keyVal struct {
	label, mask string
	sum         [32]byte
}

type sessionVal struct {
	short          string
	parent, client int32
}

type ruleVal struct {
	alias, model string
}

type exitVal struct {
	provider, label string
	tried           bool
}
