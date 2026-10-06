package binlogprobe

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync/atomic"
	"time"

	gomysql "github.com/go-mysql-org/go-mysql/mysql"
	"github.com/go-mysql-org/go-mysql/replication"
)

type Request struct {
	Host     string
	Port     uint16
	Username string
	Password string
	File     string
	Timeout  time.Duration
}

type FileTimestamp struct {
	File  string    `json:"file"`
	Start time.Time `json:"start_time"`
}

var serverID atomic.Uint32

func init() { serverID.Store(uint32(time.Now().UnixNano())) }

func ProbeStart(ctx context.Context, req Request) (FileTimestamp, error) {
	if req.Timeout <= 0 {
		req.Timeout = 10 * time.Second
	}
	probeCtx, cancel := context.WithTimeout(ctx, req.Timeout)
	defer cancel()
	syncer := replication.NewBinlogSyncer(replication.BinlogSyncerConfig{
		ServerID: nextServerID(), Flavor: "mysql", Host: req.Host, Port: req.Port,
		User: req.Username, Password: req.Password, ReadTimeout: req.Timeout,
		DisableRetrySync: true, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	defer syncer.Close()
	streamer, err := syncer.StartSync(gomysql.Position{Name: req.File, Pos: 4})
	if err != nil {
		return FileTimestamp{}, fmt.Errorf("start binlog timestamp probe failed: %w", err)
	}
	start, err := firstEventTime(probeCtx, streamer.GetEvent)
	if err != nil {
		return FileTimestamp{}, fmt.Errorf("read binlog timestamp failed: %w", err)
	}
	return FileTimestamp{File: req.File, Start: start}, nil
}

func firstEventTime(ctx context.Context, next func(context.Context) (*replication.BinlogEvent, error)) (time.Time, error) {
	for {
		event, err := next(ctx)
		if err != nil {
			return time.Time{}, err
		}
		if event != nil && event.Header != nil && event.Header.Timestamp > 0 {
			return time.Unix(int64(event.Header.Timestamp), 0).UTC(), nil
		}
	}
}

func nextServerID() uint32 {
	id := serverID.Add(1)
	if id == 0 {
		id = serverID.Add(1)
	}
	return id
}
