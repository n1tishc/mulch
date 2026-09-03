package event

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schema string

type Publisher interface{ Publish(Event) }

type writeKind uint8

const (
	writeCreateSession writeKind = iota
	writeAppendEvent
	writeEndSession
)

type writeRequest struct {
	kind      writeKind
	ctx       context.Context
	session   Session
	event     Event
	sessionID string
	status    Status
	reply     chan writeResult
}
type writeResult struct {
	event Event
	err   error
}

type SQLiteStore struct {
	db        *sql.DB
	writes    chan writeRequest
	done      chan struct{}
	cancel    context.CancelFunc
	closeOnce sync.Once
	publisher Publisher
}

func Open(ctx context.Context, path string, publisher Publisher) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if _, err = db.ExecContext(ctx, schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("initialize event store: %w", err)
	}
	writerCtx, cancel := context.WithCancel(ctx)
	s := &SQLiteStore{db: db, writes: make(chan writeRequest), done: make(chan struct{}), cancel: cancel, publisher: publisher}
	go s.writeLoop(writerCtx)
	return s, nil
}

func (s *SQLiteStore) CreateSession(ctx context.Context, session Session) error {
	if session.CreatedAt.IsZero() {
		session.CreatedAt = time.Now().UTC()
	}
	result, err := s.write(ctx, writeRequest{kind: writeCreateSession, session: session})
	if err != nil {
		return err
	}
	return result.err
}

func (s *SQLiteStore) Append(ctx context.Context, e Event) (Event, error) {
	result, err := s.write(ctx, writeRequest{kind: writeAppendEvent, event: e})
	if err != nil {
		return Event{}, err
	}
	return result.event, result.err
}

func (s *SQLiteStore) write(ctx context.Context, request writeRequest) (writeResult, error) {
	request.ctx = ctx
	request.reply = make(chan writeResult, 1)
	select {
	case s.writes <- request:
	case <-ctx.Done():
		return writeResult{}, ctx.Err()
	case <-s.done:
		return writeResult{}, errors.New("event store closed")
	}
	select {
	case result := <-request.reply:
		return result, nil
	case <-ctx.Done():
		return writeResult{}, ctx.Err()
	}
}

func (s *SQLiteStore) writeLoop(ctx context.Context) {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		close(s.done)
		return
	}
	defer conn.Close()
	defer close(s.done)
	for {
		select {
		case <-ctx.Done():
			return
		case req := <-s.writes:
			req.reply <- s.executeWrite(conn, req)
		}
	}
}

func (s *SQLiteStore) executeWrite(conn *sql.Conn, req writeRequest) writeResult {
	switch req.kind {
	case writeCreateSession:
		x := req.session
		_, err := conn.ExecContext(req.ctx, `INSERT INTO sessions(id,parent_id,fork_seq,label,task,model,context_window,workdir,created_at,status) VALUES(?,?,?,?,?,?,?,?,?,?)`, x.ID, nullableString(x.ParentID), x.ForkSeq, nullableString(x.Label), x.Task, x.Model, x.ContextWindow, x.Workdir, x.CreatedAt.Format(time.RFC3339Nano), StatusRunning)
		return writeResult{err: err}
	case writeAppendEvent:
		e := req.event
		if e.CreatedAt.IsZero() {
			e.CreatedAt = time.Now().UTC()
		}
		if e.Payload == nil {
			e.Payload = json.RawMessage(`{}`)
		}
		var id, seq int64
		err := conn.QueryRowContext(req.ctx, `INSERT INTO events(session_id,seq,turn,type,payload,tokens,visible,image_ref,created_at) VALUES(?,(SELECT COALESCE(MAX(seq),0)+1 FROM events WHERE session_id=?),?,?,?,?,?,?,?) RETURNING id,seq`, e.SessionID, e.SessionID, e.Turn, e.Type, string(e.Payload), e.Tokens, boolInt(e.Visible), nullableString(e.ImageRef), e.CreatedAt.Format(time.RFC3339Nano)).Scan(&id, &seq)
		if err == nil {
			e.ID, e.Seq = id, seq
			if s.publisher != nil {
				s.publisher.Publish(e)
			}
		}
		return writeResult{event: e, err: err}
	case writeEndSession:
		_, err := conn.ExecContext(req.ctx, `UPDATE sessions SET status=?,ended_at=? WHERE id=?`, req.status, time.Now().UTC().Format(time.RFC3339Nano), req.sessionID)
		return writeResult{err: err}
	default:
		return writeResult{err: errors.New("unknown event-store write")}
	}
}

func (s *SQLiteStore) List(ctx context.Context, sessionID string, from int64) ([]Event, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,session_id,seq,turn,type,payload,tokens,visible,image_ref,created_at FROM events WHERE session_id=? AND seq>=? ORDER BY seq`, sessionID, from)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []Event
	for rows.Next() {
		var e Event
		var payload, created string
		var tokens sql.NullInt64
		var imageRef sql.NullString
		var visible int
		if err := rows.Scan(&e.ID, &e.SessionID, &e.Seq, &e.Turn, &e.Type, &payload, &tokens, &visible, &imageRef, &created); err != nil {
			return nil, err
		}
		e.Payload = []byte(payload)
		if tokens.Valid {
			value := int(tokens.Int64)
			e.Tokens = &value
		}
		e.Visible = visible != 0
		e.ImageRef = imageRef.String
		e.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
		if err != nil {
			return nil, err
		}
		result = append(result, e)
	}
	return result, rows.Err()
}

func (s *SQLiteStore) Visible(ctx context.Context, sessionID string) ([]Event, error) {
	events, err := s.List(ctx, sessionID, 1)
	if err != nil {
		return nil, err
	}
	result := events[:0]
	for _, e := range events {
		if e.Visible {
			result = append(result, e)
		}
	}
	return result, nil
}

func (s *SQLiteStore) EndSession(ctx context.Context, id string, status Status) error {
	result, err := s.write(ctx, writeRequest{kind: writeEndSession, sessionID: id, status: status})
	if err != nil {
		return err
	}
	return result.err
}

func (s *SQLiteStore) Session(ctx context.Context, id string) (Session, error) {
	var x Session
	var created string
	var ended sql.NullString
	var parentID, label sql.NullString
	var forkSeq sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT id,parent_id,fork_seq,label,task,model,context_window,workdir,status,created_at,ended_at FROM sessions WHERE id=?`, id).Scan(&x.ID, &parentID, &forkSeq, &label, &x.Task, &x.Model, &x.ContextWindow, &x.Workdir, &x.Status, &created, &ended)
	if err != nil {
		return x, err
	}
	x.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
	x.ParentID, x.Label = parentID.String, label.String
	if forkSeq.Valid {
		value := forkSeq.Int64
		x.ForkSeq = &value
	}
	if ended.Valid {
		t, e := time.Parse(time.RFC3339Nano, ended.String)
		if e != nil {
			return x, e
		}
		x.EndedAt = &t
	}
	return x, err
}

func (s *SQLiteStore) Close() error {
	var err error
	s.closeOnce.Do(func() { s.cancel(); <-s.done; err = s.db.Close() })
	return err
}
func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
