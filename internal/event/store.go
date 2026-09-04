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
	writeBranch
	writeSetLabel
	writeResumeSession
	writeSetVisible
)

type writeRequest struct {
	kind      writeKind
	ctx       context.Context
	session   Session
	event     Event
	sessionID string
	status    Status
	label     string
	atSeq     int64
	seqs      []int64
	visible   bool
	reason    string
	by        string
	reply     chan writeResult
}
type writeResult struct {
	event   Event
	session Session
	err     error
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
		tx, err := conn.BeginTx(req.ctx, nil)
		if err != nil {
			return writeResult{err: err}
		}
		defer tx.Rollback()
		var id, seq int64
		err = tx.QueryRowContext(req.ctx, `INSERT INTO events(session_id,seq,turn,type,payload,tokens,visible,image_ref,created_at) VALUES(?,(SELECT COALESCE(MAX(seq),0)+1 FROM events WHERE session_id=?),?,?,?,?,?,?,?) RETURNING id,seq`, e.SessionID, e.SessionID, e.Turn, e.Type, string(e.Payload), e.Tokens, boolInt(e.Visible), nullableString(e.ImageRef), e.CreatedAt.Format(time.RFC3339Nano)).Scan(&id, &seq)
		if err == nil {
			e.ID, e.Seq = id, seq
			err = tx.Commit()
		}
		if err == nil {
			if s.publisher != nil {
				s.publisher.Publish(e)
			}
		}
		return writeResult{event: e, err: err}
	case writeEndSession:
		_, err := conn.ExecContext(req.ctx, `UPDATE sessions SET status=?,ended_at=? WHERE id=?`, req.status, time.Now().UTC().Format(time.RFC3339Nano), req.sessionID)
		return writeResult{err: err}
	case writeSetLabel:
		result, err := conn.ExecContext(req.ctx, `UPDATE sessions SET label=? WHERE id=?`, nullableString(req.label), req.sessionID)
		if err == nil {
			var affected int64
			affected, err = result.RowsAffected()
			if err == nil && affected == 0 {
				err = sql.ErrNoRows
			}
		}
		return writeResult{err: err}
	case writeResumeSession:
		result, err := conn.ExecContext(req.ctx, `UPDATE sessions SET status=?,ended_at=NULL WHERE id=?`, StatusRunning, req.sessionID)
		if err == nil {
			var affected int64
			affected, err = result.RowsAffected()
			if err == nil && affected == 0 {
				err = sql.ErrNoRows
			}
		}
		return writeResult{err: err}
	case writeBranch:
		return s.executeBranch(conn, req)
	case writeSetVisible:
		tx, err := conn.BeginTx(req.ctx, nil)
		if err != nil {
			return writeResult{err: err}
		}
		defer tx.Rollback()
		changes := make([]VisibilityChange, 0, len(req.seqs))
		for _, seq := range req.seqs {
			var current int
			if err := tx.QueryRowContext(req.ctx, `SELECT visible FROM events WHERE session_id=? AND seq=?`, req.sessionID, seq).Scan(&current); err != nil {
				return writeResult{err: fmt.Errorf("event %s/%d: %w", req.sessionID, seq, err)}
			}
			result, updateErr := tx.ExecContext(req.ctx, `UPDATE events SET visible=? WHERE session_id=? AND seq=?`, boolInt(req.visible), req.sessionID, seq)
			if updateErr != nil {
				return writeResult{err: updateErr}
			}
			affected, updateErr := result.RowsAffected()
			if updateErr != nil {
				return writeResult{err: updateErr}
			}
			if affected == 0 {
				return writeResult{err: fmt.Errorf("event %s/%d: %w", req.sessionID, seq, sql.ErrNoRows)}
			}
			changes = append(changes, VisibilityChange{Seq: seq, From: current != 0, To: req.visible})
		}
		payload, err := json.Marshal(ContextVisibility{Changes: changes, Reason: req.reason, By: req.by})
		if err != nil {
			return writeResult{err: err}
		}
		var marker Event
		marker.SessionID, marker.Type, marker.Payload, marker.CreatedAt = req.sessionID, TypeContextVisibility, payload, time.Now().UTC()
		if err = tx.QueryRowContext(req.ctx, `INSERT INTO events(session_id,seq,turn,type,payload,visible,created_at) VALUES(?,(SELECT COALESCE(MAX(seq),0)+1 FROM events WHERE session_id=?),0,?,?,0,?) RETURNING id,seq`, req.sessionID, req.sessionID, marker.Type, string(marker.Payload), marker.CreatedAt.Format(time.RFC3339Nano)).Scan(&marker.ID, &marker.Seq); err != nil {
			return writeResult{err: err}
		}
		if err = tx.Commit(); err != nil {
			return writeResult{err: err}
		}
		if s.publisher != nil {
			s.publisher.Publish(marker)
		}
		return writeResult{}
	default:
		return writeResult{err: errors.New("unknown event-store write")}
	}
}

func (s *SQLiteStore) executeBranch(conn *sql.Conn, req writeRequest) writeResult {
	var parent Session
	var parentID, label, created string
	var forkSeq sql.NullInt64
	err := conn.QueryRowContext(req.ctx, `SELECT id,COALESCE(parent_id,''),fork_seq,COALESCE(label,''),task,model,context_window,workdir,status,created_at FROM sessions WHERE id=?`, req.sessionID).Scan(&parent.ID, &parentID, &forkSeq, &label, &parent.Task, &parent.Model, &parent.ContextWindow, &parent.Workdir, &parent.Status, &created)
	if err != nil {
		return writeResult{err: err}
	}
	var maxSeq int64
	if err = conn.QueryRowContext(req.ctx, `SELECT COALESCE(MAX(seq),0) FROM events WHERE session_id=?`, req.sessionID).Scan(&maxSeq); err != nil {
		return writeResult{err: err}
	}
	if req.atSeq < 0 || req.atSeq > maxSeq {
		return writeResult{err: fmt.Errorf("fork sequence %d is outside session history (0-%d)", req.atSeq, maxSeq)}
	}
	rows, err := conn.QueryContext(req.ctx, `SELECT id,session_id,seq,turn,type,payload,tokens,visible,image_ref,created_at FROM events WHERE session_id=? ORDER BY seq`, req.sessionID)
	if err != nil {
		return writeResult{err: err}
	}
	var history []Event
	for rows.Next() {
		candidate, scanErr := scanEvent(rows)
		if scanErr != nil {
			rows.Close()
			return writeResult{err: scanErr}
		}
		history = append(history, candidate)
	}
	if err = rows.Close(); err != nil {
		return writeResult{err: err}
	}
	visibleAtFork := make(map[int64]bool, len(history))
	for _, candidate := range history {
		visibleAtFork[candidate.Seq] = candidate.Visible
	}
	for i := len(history) - 1; i >= 0; i-- {
		candidate := history[i]
		if candidate.Seq <= req.atSeq || candidate.Type != TypeContextVisibility {
			continue
		}
		var marker ContextVisibility
		if err = candidate.Decode(&marker); err != nil {
			return writeResult{err: fmt.Errorf("decode visibility event %d: %w", candidate.Seq, err)}
		}
		for _, change := range marker.Changes {
			visibleAtFork[change.Seq] = change.From
		}
	}
	child := req.session
	child.ParentID = parent.ID
	child.ForkSeq = &req.atSeq
	child.Task, child.Model, child.ContextWindow, child.Workdir = parent.Task, parent.Model, parent.ContextWindow, parent.Workdir
	child.Status = StatusRunning
	child.CreatedAt = time.Now().UTC()
	tx, err := conn.BeginTx(req.ctx, nil)
	if err != nil {
		return writeResult{err: err}
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(req.ctx, `INSERT INTO sessions(id,parent_id,fork_seq,task,model,context_window,workdir,created_at,status) VALUES(?,?,?,?,?,?,?,?,?)`, child.ID, child.ParentID, req.atSeq, child.Task, child.Model, child.ContextWindow, child.Workdir, child.CreatedAt.Format(time.RFC3339Nano), StatusRunning); err != nil {
		return writeResult{err: err}
	}
	startPayload, err := json.Marshal(SessionStart{Task: child.Task, Model: child.Model, Workdir: child.Workdir, ParentID: child.ParentID, ForkSeq: child.ForkSeq})
	if err != nil {
		return writeResult{err: err}
	}
	if _, err = tx.ExecContext(req.ctx, `INSERT INTO events(session_id,seq,turn,type,payload,visible,created_at) VALUES(?,1,0,?,?,0,?)`, child.ID, TypeSessionStart, string(startPayload), child.CreatedAt.Format(time.RFC3339Nano)); err != nil {
		return writeResult{err: err}
	}
	childSeq := int64(2)
	for _, candidate := range history {
		if candidate.Seq > req.atSeq || !visibleAtFork[candidate.Seq] {
			continue
		}
		if _, err = tx.ExecContext(req.ctx, `INSERT INTO events(session_id,seq,turn,type,payload,tokens,visible,image_ref,created_at) VALUES(?,?,?,?,?,?,1,?,?)`, child.ID, childSeq, candidate.Turn, candidate.Type, string(candidate.Payload), candidate.Tokens, nullableString(candidate.ImageRef), candidate.CreatedAt.Format(time.RFC3339Nano)); err != nil {
			return writeResult{err: err}
		}
		childSeq++
	}
	if err = tx.Commit(); err != nil {
		return writeResult{err: err}
	}
	return writeResult{session: child}
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

func (s *SQLiteStore) ResumeSession(ctx context.Context, id string) error {
	result, err := s.write(ctx, writeRequest{kind: writeResumeSession, sessionID: id})
	if err != nil {
		return err
	}
	return result.err
}

func (s *SQLiteStore) SetLabel(ctx context.Context, id, label string) error {
	result, err := s.write(ctx, writeRequest{kind: writeSetLabel, sessionID: id, label: label})
	if err != nil {
		return err
	}
	return result.err
}

func (s *SQLiteStore) Branch(ctx context.Context, parentID string, atSeq int64) (Session, error) {
	id, err := NewSessionID()
	if err != nil {
		return Session{}, err
	}
	result, err := s.write(ctx, writeRequest{kind: writeBranch, sessionID: parentID, atSeq: atSeq, session: Session{ID: id}})
	if err != nil {
		return Session{}, err
	}
	return result.session, result.err
}

func (s *SQLiteStore) SetVisible(ctx context.Context, sessionID string, seqs []int64, visible bool) error {
	return s.SetVisibleBecause(ctx, sessionID, seqs, visible, "explicit visibility change", "event.Store.SetVisible")
}

func (s *SQLiteStore) SetVisibleBecause(ctx context.Context, sessionID string, seqs []int64, visible bool, reason, by string) error {
	if reason == "" || by == "" {
		return errors.New("visibility reason and actor are required")
	}
	result, err := s.write(ctx, writeRequest{kind: writeSetVisible, sessionID: sessionID, seqs: append([]int64(nil), seqs...), visible: visible, reason: reason, by: by})
	if err != nil {
		return err
	}
	return result.err
}

func (s *SQLiteStore) Sessions(ctx context.Context) ([]Session, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,parent_id,fork_seq,label,task,model,context_window,workdir,status,created_at,ended_at FROM sessions ORDER BY created_at,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var sessions []Session
	for rows.Next() {
		x, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, x)
	}
	return sessions, rows.Err()
}

func (s *SQLiteStore) Tree(ctx context.Context, rootID string) (Node, error) {
	sessions, err := s.Sessions(ctx)
	if err != nil {
		return Node{}, err
	}
	byParent := make(map[string][]Session)
	var root *Session
	for i := range sessions {
		if sessions[i].ID == rootID {
			root = &sessions[i]
		}
		byParent[sessions[i].ParentID] = append(byParent[sessions[i].ParentID], sessions[i])
	}
	if root == nil {
		return Node{}, sql.ErrNoRows
	}
	var build func(Session) Node
	build = func(x Session) Node {
		n := Node{Session: x}
		for _, child := range byParent[x.ID] {
			n.Children = append(n.Children, build(child))
		}
		return n
	}
	return build(*root), nil
}

func (s *SQLiteStore) Session(ctx context.Context, id string) (Session, error) {
	x, err := scanSession(s.db.QueryRowContext(ctx, `SELECT id,parent_id,fork_seq,label,task,model,context_window,workdir,status,created_at,ended_at FROM sessions WHERE id=?`, id))
	return x, err
}

type scanner interface{ Scan(...any) error }

func scanEvent(row scanner) (Event, error) {
	var candidate Event
	var payload, created string
	var tokens sql.NullInt64
	var imageRef sql.NullString
	var visible int
	if err := row.Scan(&candidate.ID, &candidate.SessionID, &candidate.Seq, &candidate.Turn, &candidate.Type, &payload, &tokens, &visible, &imageRef, &created); err != nil {
		return candidate, err
	}
	candidate.Payload = []byte(payload)
	if tokens.Valid {
		value := int(tokens.Int64)
		candidate.Tokens = &value
	}
	candidate.Visible, candidate.ImageRef = visible != 0, imageRef.String
	parsed, err := time.Parse(time.RFC3339Nano, created)
	if err != nil {
		return candidate, err
	}
	candidate.CreatedAt = parsed
	return candidate, nil
}

func scanSession(row scanner) (Session, error) {
	var x Session
	var created string
	var ended sql.NullString
	var parentID, label sql.NullString
	var forkSeq sql.NullInt64
	err := row.Scan(&x.ID, &parentID, &forkSeq, &label, &x.Task, &x.Model, &x.ContextWindow, &x.Workdir, &x.Status, &created, &ended)
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
