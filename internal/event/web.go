package event

import "context"

type RequestReceipt struct {
	Fresh           bool
	Response, Error string
}

func (s *SQLiteStore) ReserveRequest(ctx context.Context, id, fingerprint string) (RequestReceipt, error) {
	r, err := s.write(ctx, writeRequest{kind: writeWebRequest, model: id, label: fingerprint})
	if err != nil {
		return RequestReceipt{}, err
	}
	return r.receipt, r.err
}
func (s *SQLiteStore) FinishRequest(ctx context.Context, id, response, failure string) error {
	r, err := s.write(ctx, writeRequest{kind: writeWebRequest, by: "finish", model: id, reason: response, label: failure})
	if err != nil {
		return err
	}
	return r.err
}

// AcquireExecution prevents two processes from executing the same session.
// Leases are not expired automatically: a crashed owner requires explicit recovery.
func (s *SQLiteStore) AcquireExecution(ctx context.Context, id string) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	owner, err := NewSessionID()
	if err != nil {
		return nil, err
	}
	// Once submitted, await the acknowledgement even if the task is cancelled.
	// Otherwise the INSERT can commit while the caller loses its release handle.
	r, err := s.write(context.WithoutCancel(ctx), writeRequest{kind: writeLease, sessionID: id, label: owner})
	if err != nil {
		return nil, err
	}
	if r.err != nil {
		return nil, r.err
	}
	return func() {
		_, _ = s.write(context.WithoutCancel(ctx), writeRequest{kind: writeLease, by: "release", sessionID: id, label: owner})
	}, nil
}
