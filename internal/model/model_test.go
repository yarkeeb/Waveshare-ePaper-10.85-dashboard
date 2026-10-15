package model

import (
	"sync"
	"testing"
)

func TestConcurrentSnapshot(t *testing.T) {
	s := NewStore()
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Go(func() {
			for j := 0; j < 100; j++ {
				s.Put("sample", j, nil)
				snap := s.Snapshot()
				delete(snap, "sample")
			}
		})
	}
	wg.Wait()
	if len(s.Snapshot()) != 1 {
		t.Fatal("snapshot aliases store")
	}
}
