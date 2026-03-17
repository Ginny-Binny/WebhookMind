package store

import (
	"fmt"
	"time"

	"github.com/gauravfs-14/webhookmind/internal/models"
	"github.com/gocql/gocql"
)

type ScyllaStore struct {
	session *gocql.Session
}

func NewScyllaStore(hosts []string, keyspace string) (*ScyllaStore, error) {
	cluster := gocql.NewCluster(hosts...)
	cluster.Keyspace = keyspace
	cluster.Consistency = gocql.Quorum
	cluster.ConnectTimeout = 10 * time.Second
	cluster.Timeout = 5 * time.Second

	session, err := cluster.CreateSession()
	if err != nil {
		return nil, fmt.Errorf("create scylla session: %w", err)
	}

	return &ScyllaStore{session: session}, nil
}

func (s *ScyllaStore) InsertEvent(event *models.WebhookEvent) error {
	eventUUID, err := gocql.ParseUUID(event.ID)
	if err != nil {
		return fmt.Errorf("parse event uuid: %w", err)
	}

	err = s.session.Query(
		`INSERT INTO webhook_events (source_id, received_at, event_id, raw_body, headers) VALUES (?, ?, ?, ?, ?)`,
		event.SourceID,
		event.ReceivedAt,
		eventUUID,
		event.RawBody,
		event.Headers,
	).Exec()
	if err != nil {
		return fmt.Errorf("insert event to scylla: %w", err)
	}

	return nil
}

func (s *ScyllaStore) GetEvent(sourceID string, eventID string) (*models.WebhookEvent, error) {
	eventUUID, err := gocql.ParseUUID(eventID)
	if err != nil {
		return nil, fmt.Errorf("parse event uuid: %w", err)
	}

	var event models.WebhookEvent
	var receivedAt time.Time
	var rawBody []byte
	var headers map[string]string

	err = s.session.Query(
		`SELECT source_id, received_at, event_id, raw_body, headers FROM webhook_events WHERE source_id = ? AND event_id = ? ALLOW FILTERING`,
		sourceID, eventUUID,
	).Scan(&event.SourceID, &receivedAt, &eventUUID, &rawBody, &headers)
	if err != nil {
		return nil, fmt.Errorf("get event from scylla: %w", err)
	}

	event.ID = eventID
	event.ReceivedAt = receivedAt
	event.RawBody = rawBody
	event.Headers = headers

	return &event, nil
}

func (s *ScyllaStore) GetEventsBySourceSince(sourceID string, since time.Time) ([]*models.WebhookEvent, error) {
	iter := s.session.Query(
		`SELECT source_id, received_at, event_id, raw_body, headers FROM webhook_events
		 WHERE source_id = ? AND received_at >= ? ALLOW FILTERING`,
		sourceID, since,
	).PageSize(100).Iter()

	var events []*models.WebhookEvent
	var sid string
	var receivedAt time.Time
	var eventUUID gocql.UUID
	var rawBody []byte
	var headers map[string]string

	for iter.Scan(&sid, &receivedAt, &eventUUID, &rawBody, &headers) {
		events = append(events, &models.WebhookEvent{
			ID:         eventUUID.String(),
			SourceID:   sid,
			ReceivedAt: receivedAt,
			RawBody:    rawBody,
			Headers:    headers,
		})
	}

	if err := iter.Close(); err != nil {
		return nil, fmt.Errorf("iterate events from scylla: %w", err)
	}

	return events, nil
}

func (s *ScyllaStore) Close() {
	s.session.Close()
}
