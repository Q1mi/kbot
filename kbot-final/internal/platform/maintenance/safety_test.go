package maintenance

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestArchiveOldPartitionsRequiresObjectStorage(t *testing.T) {
	s := NewService(nil, nil, 13)
	n, err := s.ArchiveOldPartitions(context.Background(), time.Now())
	if n != 0 {
		t.Fatalf("archived=%d want=0", n)
	}
	if !errors.Is(err, ErrArchiveUnavailable) {
		t.Fatalf("error=%v want=%v", err, ErrArchiveUnavailable)
	}
}
