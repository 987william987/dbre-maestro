package realtime

import (
	"encoding/json"
	"sync"
)

type Event struct {
	Type string
	Data json.RawMessage
}

type Broker struct {
	mu          sync.RWMutex
	nextSubID   uint64
	subscribers map[uint64]map[uint64]chan Event
}

func NewBroker() *Broker {
	return &Broker{
		subscribers: make(map[uint64]map[uint64]chan Event),
	}
}

func (b *Broker) Subscribe(userID uint64, buffer int) (<-chan Event, func()) {
	if buffer <= 0 {
		buffer = 1
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	b.nextSubID++
	subID := b.nextSubID
	ch := make(chan Event, buffer)
	if _, ok := b.subscribers[userID]; !ok {
		b.subscribers[userID] = make(map[uint64]chan Event)
	}
	b.subscribers[userID][subID] = ch

	cancel := func() {
		b.mu.Lock()
		defer b.mu.Unlock()

		userSubs, ok := b.subscribers[userID]
		if !ok {
			return
		}
		if _, exists := userSubs[subID]; exists {
			delete(userSubs, subID)
		}
		if len(userSubs) == 0 {
			delete(b.subscribers, userID)
		}
	}

	return ch, cancel
}

func (b *Broker) PublishToUsers(userIDs []uint64, eventType string, payload any) {
	if b == nil || len(userIDs) == 0 || eventType == "" {
		return
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return
	}

	event := Event{Type: eventType, Data: data}
	for _, userID := range dedupeUserIDs(userIDs) {
		if userID == 0 {
			continue
		}
		for _, subscriber := range b.snapshotSubscribers(userID) {
			select {
			case subscriber <- event:
			default:
				select {
				case <-subscriber:
				default:
				}
				select {
				case subscriber <- event:
				default:
				}
			}
		}
	}
}

func (b *Broker) snapshotSubscribers(userID uint64) []chan Event {
	b.mu.RLock()
	defer b.mu.RUnlock()

	userSubs, ok := b.subscribers[userID]
	if !ok {
		return nil
	}

	snapshot := make([]chan Event, 0, len(userSubs))
	for _, subscriber := range userSubs {
		snapshot = append(snapshot, subscriber)
	}
	return snapshot
}

func dedupeUserIDs(userIDs []uint64) []uint64 {
	seen := make(map[uint64]struct{}, len(userIDs))
	deduped := make([]uint64, 0, len(userIDs))
	for _, userID := range userIDs {
		if userID == 0 {
			continue
		}
		if _, ok := seen[userID]; ok {
			continue
		}
		seen[userID] = struct{}{}
		deduped = append(deduped, userID)
	}
	return deduped
}
