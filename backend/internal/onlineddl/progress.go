package onlineddl

import (
	"context"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

type ProgressWriter func(context.Context, uint64, ProgressSnapshot, bool) (bool, error)
type ProgressSampler struct {
	mu                      sync.Mutex
	lastLatest, lastHistory map[uint64]time.Time
	now                     func() time.Time
	write                   ProgressWriter
}

func NewProgressSampler(write ProgressWriter) *ProgressSampler {
	return &ProgressSampler{lastLatest: map[uint64]time.Time{}, lastHistory: map[uint64]time.Time{}, now: time.Now, write: write}
}
func (s *ProgressSampler) Sample(ctx context.Context, id uint64, mode, output string) (bool, error) {
	return s.sample(ctx, id, mode, output, false)
}
func (s *ProgressSampler) SampleFinal(ctx context.Context, id uint64, mode, output string) (bool, error) {
	return s.sample(ctx, id, mode, output, true)
}
func (s *ProgressSampler) sample(ctx context.Context, id uint64, mode, output string, force bool) (bool, error) {
	progress, ok := ParseProgress(mode, output)
	if !ok {
		return false, nil
	}
	s.mu.Lock()
	now := s.now()
	if !force && now.Sub(s.lastLatest[id]) < 2*time.Second {
		s.mu.Unlock()
		return false, nil
	}
	history := now.Sub(s.lastHistory[id]) >= 10*time.Second
	s.lastLatest[id] = now
	if history {
		s.lastHistory[id] = now
	}
	s.mu.Unlock()
	if s.write == nil {
		return false, nil
	}
	return s.write(ctx, id, progress, history)
}
func (s *ProgressSampler) Forget(id uint64) {
	s.mu.Lock()
	delete(s.lastLatest, id)
	delete(s.lastHistory, id)
	s.mu.Unlock()
}

type ProgressSnapshot struct {
	Phase            string
	ProgressPercent  *float64
	CopiedRows       *uint64
	ETASeconds       *uint64
	ReplicationLagMs *uint64
	ThreadsRunning   *uint
	ThrottleReason   string
}

var (
	ghostProgress     = regexp.MustCompile(`(?i)Copy:\s*([0-9]+)\/([0-9]+)\s+([0-9]+(?:\.[0-9]+)?)%`)
	etaProgress       = regexp.MustCompile(`(?i)ETA:\s*([^;\s]+)`)
	ptoscProgress     = regexp.MustCompile(`(?i)([0-9]+)\s+rows copied;\s*([0-9]+(?:\.[0-9]+)?)%.*?([0-9]+):([0-9]{2}):([0-9]{2})\s+remain`)
	ptoscCopyProgress = regexp.MustCompile(`(?i)Copying\s+.+:\s*([0-9]+(?:\.[0-9]+)?)%\s+([0-9]+):([0-9]{2})\s+remain`)
	lagProgress       = regexp.MustCompile(`(?i)(?:HeartbeatLag|LAG):\s*([0-9]+(?:\.[0-9]+)?)s`)
	threadsProgress   = regexp.MustCompile(`(?i)MySQL load:[^\n]*Threads_running\s*[=:]\s*([0-9]+)`)
	stateProgress     = regexp.MustCompile(`(?i)State:\s*([a-z_-]+)`)
	throttleProgress  = regexp.MustCompile(`(?i)(throttl(?:ed|ing)[^;\n]*)`)
)

func ParseProgress(mode, output string) (ProgressSnapshot, bool) {
	lines := strings.Split(output, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if mode == ModeGhost {
			if match := ghostProgress.FindStringSubmatch(line); match != nil {
				copied, _ := strconv.ParseUint(match[1], 10, 64)
				percent, _ := strconv.ParseFloat(match[3], 64)
				progress := ProgressSnapshot{Phase: "copy", CopiedRows: &copied, ProgressPercent: &percent}
				if etaMatch := etaProgress.FindStringSubmatch(line); etaMatch != nil {
					if duration, err := time.ParseDuration(etaMatch[1]); err == nil {
						eta := uint64(duration / time.Second)
						progress.ETASeconds = &eta
					}
				}
				return enrichProgress(progress, output), true
			}
		} else if mode == ModePTOSC {
			if match := ptoscProgress.FindStringSubmatch(line); match != nil {
				copied, _ := strconv.ParseUint(match[1], 10, 64)
				percent, _ := strconv.ParseFloat(match[2], 64)
				hours, _ := strconv.ParseUint(match[3], 10, 64)
				minutes, _ := strconv.ParseUint(match[4], 10, 64)
				seconds, _ := strconv.ParseUint(match[5], 10, 64)
				eta := hours*3600 + minutes*60 + seconds
				return enrichProgress(ProgressSnapshot{Phase: "copy", CopiedRows: &copied, ProgressPercent: &percent, ETASeconds: &eta}, output), true
			}
			if match := ptoscCopyProgress.FindStringSubmatch(line); match != nil {
				percent, _ := strconv.ParseFloat(match[1], 64)
				minutes, _ := strconv.ParseUint(match[2], 10, 64)
				seconds, _ := strconv.ParseUint(match[3], 10, 64)
				eta := minutes*60 + seconds
				return enrichProgress(ProgressSnapshot{Phase: "copy", ProgressPercent: &percent, ETASeconds: &eta}, output), true
			}
		}
	}
	return ProgressSnapshot{}, false
}

func enrichProgress(progress ProgressSnapshot, output string) ProgressSnapshot {
	if match := lagProgress.FindStringSubmatch(output); match != nil {
		seconds, _ := strconv.ParseFloat(match[1], 64)
		value := uint64(seconds * 1000)
		progress.ReplicationLagMs = &value
	}
	if match := threadsProgress.FindStringSubmatch(output); match != nil {
		value, _ := strconv.ParseUint(match[1], 10, 64)
		converted := uint(value)
		progress.ThreadsRunning = &converted
	}
	if match := stateProgress.FindStringSubmatch(output); match != nil {
		progress.Phase = strings.ToLower(match[1])
	}
	if match := throttleProgress.FindStringSubmatch(output); match != nil {
		progress.ThrottleReason = strings.TrimSpace(match[1])
	}
	return progress
}
