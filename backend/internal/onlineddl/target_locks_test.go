package onlineddl

import (
	"context"
	"testing"
	"time"
)

func TestTargetLocksSerializeOneServerAndReleaseIdempotently(t *testing.T) {
	locks := NewTargetLocks()
	release, ok := locks.TryAcquire("Writer.EXAMPLE.com.", 3306)
	if !ok {
		t.Fatal("first process must acquire its target")
	}
	if _, ok := locks.TryAcquire("writer.example.com", 3306); ok {
		t.Fatal("same normalized server must not run two tools concurrently")
	}
	if _, ok := locks.TryAcquire("other.example.com", 3306); !ok {
		t.Fatal("different servers must remain independent")
	}
	release()
	release()
	if _, ok := locks.TryAcquire("writer.example.com", 3306); !ok {
		t.Fatal("completed process must release its target")
	}
}

func TestTargetLocksWaitForBusyServerAndHonorCancellation(t *testing.T) {
	locks := NewTargetLocks()
	release, _ := locks.TryAcquire("writer.example.com", 3306)
	acquired := make(chan func(), 1)
	go func() {
		nextRelease, err := locks.Acquire(context.Background(), "writer.example.com", 3306)
		if err == nil {
			acquired <- nextRelease
		}
	}()
	select {
	case <-acquired:
		t.Fatal("second run acquired a busy server")
	case <-time.After(20 * time.Millisecond):
	}
	release()
	select {
	case nextRelease := <-acquired:
		nextRelease()
	case <-time.After(time.Second):
		t.Fatal("waiting run did not acquire released server")
	}

	busy, _ := locks.TryAcquire("writer.example.com", 3306)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := locks.Acquire(ctx, "writer.example.com", 3306); err == nil {
		t.Fatal("cancelled run must not keep waiting")
	}
	busy()
}
