package v2rayxhttp

import (
	"bytes"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
)

// uploadQueue bounds both pending POSTs and sequence lookahead. Reading is
// serialized by the transport handler; POSTs may arrive concurrently.
type uploadQueue struct {
	mu       sync.Mutex
	changed  *sync.Cond
	packets  map[uint64][]byte
	next     uint64
	limit    int
	current  *bytes.Reader
	stream   io.Reader
	closed   bool
	budget   *atomic.Int64
	buffered int64
}

const maxBufferedUploadBytes = 64 * 1024 * 1024

func newUploadQueue(limit int) *uploadQueue {
	q := &uploadQueue{packets: make(map[uint64][]byte), limit: limit}
	q.changed = sync.NewCond(&q.mu)
	return q
}

func (q *uploadQueue) push(seq uint64, payload []byte) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return io.ErrClosedPipe
	}
	if q.stream != nil || seq < q.next || seq-q.next >= uint64(q.limit) {
		return fmt.Errorf("invalid upload sequence")
	}
	if _, exists := q.packets[seq]; exists {
		return fmt.Errorf("duplicate upload sequence")
	}
	if q.budget != nil && q.budget.Add(int64(len(payload))) > maxBufferedUploadBytes {
		q.budget.Add(-int64(len(payload)))
		return fmt.Errorf("server upload buffer is full")
	}
	q.buffered += int64(len(payload))
	q.packets[seq] = payload
	q.changed.Broadcast()
	return nil
}

func (q *uploadQueue) setStream(reader io.Reader) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed || q.stream != nil || q.next != 0 || len(q.packets) != 0 {
		return fmt.Errorf("upload already started or closed")
	}
	q.stream = reader
	q.changed.Broadcast()
	return nil
}

func (q *uploadQueue) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	for {
		if q.closed {
			return 0, io.ErrClosedPipe
		}
		if q.stream != nil {
			reader := q.stream
			q.mu.Unlock()
			n, err := reader.Read(p)
			q.mu.Lock()
			return n, err
		}
		if q.current != nil && q.current.Len() > 0 {
			n, err := q.current.Read(p)
			if q.current.Len() == 0 {
				size := q.current.Size()
				q.buffered -= size
				if q.budget != nil {
					q.budget.Add(-size)
				}
				q.current = nil
			}
			return n, err
		}
		if payload, exists := q.packets[q.next]; exists {
			delete(q.packets, q.next)
			q.next++
			q.current = bytes.NewReader(payload)
			continue
		}
		q.changed.Wait()
	}
}

func (q *uploadQueue) Close() error {
	q.mu.Lock()
	q.closed = true
	q.packets = nil
	q.current = nil
	if q.budget != nil {
		q.budget.Add(-q.buffered)
	}
	q.buffered = 0
	q.changed.Broadcast()
	q.mu.Unlock()
	return nil
}
