package store_test

import "runtime"

type runtimeMem struct{ heapLive uint64 }

func (m *runtimeMem) read() {
	runtime.GC()
	runtime.GC()
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	m.heapLive = ms.HeapAlloc // live bytes after GC; HeapInuse adds span fragmentation noise
}

func runtimeKeepAlive(v any) { runtime.KeepAlive(v) }
