package binlogprobe

import (
	"context"
	"errors"
	"testing"

	"github.com/go-mysql-org/go-mysql/replication"
)

func TestFirstEventTimeSkipsMetadataWithoutTimestamp(t *testing.T) {
	events := []*replication.BinlogEvent{{Header: &replication.EventHeader{}}, {Header: &replication.EventHeader{Timestamp: 1_759_324_830}}}
	index := 0
	got, err := firstEventTime(context.Background(), func(context.Context) (*replication.BinlogEvent, error) {
		event := events[index]
		index++
		return event, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Unix() != 1_759_324_830 || index != 2 {
		t.Fatalf("got %v after %d events", got, index)
	}
}

func TestFirstEventTimeReturnsStreamError(t *testing.T) {
	want := errors.New("stream closed")
	_, err := firstEventTime(context.Background(), func(context.Context) (*replication.BinlogEvent, error) { return nil, want })
	if !errors.Is(err, want) {
		t.Fatalf("error = %v", err)
	}
}

func TestNextServerIDIsNonZeroAndUnique(t *testing.T) {
	first, second := nextServerID(), nextServerID()
	if first == 0 || second == 0 || first == second {
		t.Fatalf("server IDs = %d, %d", first, second)
	}
}
