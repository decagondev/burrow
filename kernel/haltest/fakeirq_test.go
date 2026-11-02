package haltest

import (
	"errors"
	"testing"
)

func TestFakeIRQPendingCoalescesAndCloses(t *testing.T) {
	q := NewFakeIRQ()
	q.Fire()
	q.Fire() // still one pending interrupt
	if err := q.Wait(); err != nil {
		t.Fatal(err)
	}
	q.Ack()
	done := make(chan error)
	go func() { done <- q.Wait() }()
	q.Close()
	if err := <-done; !errors.Is(err, ErrClosed) {
		t.Fatalf("Wait after Close = %v, want ErrClosed", err)
	}
	if q.Acks() != 1 {
		t.Fatalf("Acks = %d, want 1", q.Acks())
	}
}
