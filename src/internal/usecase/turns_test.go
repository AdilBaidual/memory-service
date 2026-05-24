// Package usecase implements the business-logic layer for the memory service.
package usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"

	"memory-service/internal/adapters/llm"
	"memory-service/internal/adapters/store"
	"memory-service/internal/service/consolidation"
	"memory-service/internal/service/extraction"
)

// ---- mock pgx.Row ----

type mockPgxRow struct {
	id  uuid.UUID
	err error
}

func (r *mockPgxRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) > 0 {
		if id, ok := dest[0].(*uuid.UUID); ok {
			*id = r.id
		}
	}
	return nil
}

// ---- mock pgx.Rows (always empty) ----

type emptyRows struct{}

func (r *emptyRows) Close()                                      {}
func (r *emptyRows) Err() error                                  { return nil }
func (r *emptyRows) Next() bool                                  { return false }
func (r *emptyRows) Scan(...any) error                           { return nil }
func (r *emptyRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (r *emptyRows) Values() ([]any, error)                      { return nil, nil }
func (r *emptyRows) RawValues() [][]byte                        { return nil }
func (r *emptyRows) Conn() *pgx.Conn                             { return nil }
func (r *emptyRows) CommandTag() pgconn.CommandTag               { return pgconn.CommandTag{} }

// ---- mock pgx.Tx ----

type mockTx struct {
	id        uuid.UUID
	commitErr error
}

func (m *mockTx) Commit(ctx context.Context) error   { return m.commitErr }
func (m *mockTx) Rollback(ctx context.Context) error { return nil }
func (m *mockTx) Exec(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}
func (m *mockTx) Query(_ context.Context, _ string, _ ...any) (pgx.Rows, error) {
	return &emptyRows{}, nil
}
func (m *mockTx) QueryRow(_ context.Context, _ string, _ ...any) pgx.Row {
	return &mockPgxRow{id: m.id}
}
func (m *mockTx) Begin(ctx context.Context) (pgx.Tx, error) {
	return nil, nil
}
func (m *mockTx) CopyFrom(_ context.Context, _ pgx.Identifier, _ []string, _ pgx.CopyFromSource) (int64, error) {
	return 0, nil
}
func (m *mockTx) SendBatch(_ context.Context, _ *pgx.Batch) pgx.BatchResults {
	return nil
}
func (m *mockTx) LargeObjects() pgx.LargeObjects {
	return pgx.LargeObjects{}
}
func (m *mockTx) Prepare(_ context.Context, _, _ string) (*pgconn.StatementDescription, error) {
	return nil, nil
}
func (m *mockTx) Conn() *pgx.Conn { return nil }

// ---- mock TxPool ----

type mockTxPool struct {
	txs      []*mockTx
	txIdx    int
	beginErr error
}

func (p *mockTxPool) Begin(_ context.Context) (pgx.Tx, error) {
	if p.beginErr != nil {
		return nil, p.beginErr
	}
	if p.txIdx < len(p.txs) {
		tx := p.txs[p.txIdx]
		p.txIdx++
		return tx, nil
	}
	return &mockTx{id: uuid.New()}, nil
}
func (p *mockTxPool) Exec(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}
func (p *mockTxPool) Query(_ context.Context, _ string, _ ...any) (pgx.Rows, error) {
	return &emptyRows{}, nil
}
func (p *mockTxPool) QueryRow(_ context.Context, _ string, _ ...any) pgx.Row {
	return &mockPgxRow{id: uuid.New()}
}

// ---- mock ExtractionService ----

type mockExtractionSvc struct {
	candidates []extraction.Candidate
	rels       []llm.Relationship
	err        error
	called     bool
	embedResp  []float32
	embedErr   error
}

func (m *mockExtractionSvc) Extract(_ context.Context, _ extraction.ExtractionInput) ([]extraction.Candidate, []llm.Relationship, error) {
	m.called = true
	return m.candidates, m.rels, m.err
}
func (m *mockExtractionSvc) Embed(_ context.Context, _ string) ([]float32, error) {
	return m.embedResp, m.embedErr
}

// ---- mock ConsolidationService ----

type mockConsSvc struct {
	err    error
	called int
	retID  uuid.UUID
}

func (m *mockConsSvc) ConsolidateFact(
	_ context.Context,
	_ store.Querier,
	_, _ string,
	_ *string,
	_, _ string,
	_ float32,
	_ []string,
	_ []float32,
	_ *string,
	_ *uuid.UUID,
) (uuid.UUID, consolidation.Result, error) {
	m.called++
	id := m.retID
	if id == uuid.Nil {
		id = uuid.New()
	}
	return id, consolidation.ResultADD, m.err
}

// ---- mock RelationshipsService ----

type mockRelSvc struct {
	err    error
	called bool
}

func (m *mockRelSvc) ProcessRelationships(
	_ context.Context,
	_ store.Querier,
	_ string,
	_ []llm.Relationship,
	_ map[string]uuid.UUID,
	_ uuid.UUID,
) error {
	m.called = true
	return m.err
}

// ---- helpers ----

func strPtr(s string) *string { return &s }

func newTestTurn(sessionID string, userID *string) TurnInput {
	return TurnInput{
		SessionID: sessionID,
		UserID:    userID,
		Messages:  []TurnMessage{{Role: "user", Content: "hello"}},
		Timestamp: time.Now(),
	}
}

// ---- tests ----

func TestIngest_NoUserID_SkipsExtraction(t *testing.T) {
	turnID := uuid.New()
	pool := &mockTxPool{txs: []*mockTx{{id: turnID}}}
	ext := &mockExtractionSvc{}

	uc := NewIngestTurnUsecase(pool, ext, &mockConsSvc{}, &mockRelSvc{})
	out, err := uc.Ingest(context.Background(), newTestTurn("s1", nil))

	assert.NoError(t, err)
	assert.Equal(t, turnID.String(), out.ID)
	assert.False(t, ext.called, "extraction must not be called without user_id")
}

func TestIngest_NilExtractor_SkipsExtraction(t *testing.T) {
	turnID := uuid.New()
	pool := &mockTxPool{txs: []*mockTx{{id: turnID}}}

	uc := NewIngestTurnUsecase(pool, nil, &mockConsSvc{}, &mockRelSvc{})
	uid := "u1"
	out, err := uc.Ingest(context.Background(), newTestTurn("s1", &uid))

	assert.NoError(t, err)
	assert.Equal(t, turnID.String(), out.ID)
}

func TestIngest_BeginTxError_ReturnsError(t *testing.T) {
	pool := &mockTxPool{beginErr: errors.New("connection refused")}
	uc := NewIngestTurnUsecase(pool, nil, nil, nil)

	_, err := uc.Ingest(context.Background(), newTestTurn("s1", nil))
	assert.ErrorContains(t, err, "begin transaction")
}

func TestIngest_CommitError_ReturnsError(t *testing.T) {
	tx := &mockTx{id: uuid.New(), commitErr: errors.New("commit failed")}
	pool := &mockTxPool{txs: []*mockTx{tx}}
	uc := NewIngestTurnUsecase(pool, nil, nil, nil)

	_, err := uc.Ingest(context.Background(), newTestTurn("s1", nil))
	assert.ErrorContains(t, err, "commit transaction")
}

func TestIngest_WithUserID_ExtractorCalled(t *testing.T) {
	turnID := uuid.New()
	tx1 := &mockTx{id: turnID}
	tx2 := &mockTx{id: uuid.New()}
	pool := &mockTxPool{txs: []*mockTx{tx1, tx2}}

	ext := &mockExtractionSvc{
		candidates: []extraction.Candidate{
			{Type: "fact", Key: strPtr("name"), Value: "Alice", Evidence: "explicit", Confidence: 0.95, Entities: []string{"alice"}},
		},
		rels:      []llm.Relationship{},
		embedResp: []float32{0.1, 0.2},
	}
	cons := &mockConsSvc{}
	rel := &mockRelSvc{}

	uc := NewIngestTurnUsecase(pool, ext, cons, rel)
	uid := "u1"
	out, err := uc.Ingest(context.Background(), newTestTurn("s1", &uid))

	assert.NoError(t, err)
	assert.Equal(t, turnID.String(), out.ID)
	assert.True(t, ext.called, "extraction must be called with user_id")
}

func TestIngest_ExtractionError_NonFatal(t *testing.T) {
	turnID := uuid.New()
	tx1 := &mockTx{id: turnID}
	tx2 := &mockTx{id: uuid.New()}
	pool := &mockTxPool{txs: []*mockTx{tx1, tx2}}

	ext := &mockExtractionSvc{err: errors.New("llm error")}
	cons := &mockConsSvc{}

	uc := NewIngestTurnUsecase(pool, ext, cons, &mockRelSvc{})
	uid := "u1"
	out, err := uc.Ingest(context.Background(), newTestTurn("s1", &uid))

	assert.NoError(t, err, "extraction error must be non-fatal")
	assert.Equal(t, turnID.String(), out.ID)
	assert.Zero(t, cons.called, "consolidation must not be called when extraction fails")
}

func TestIngest_ZeroCandidates_SkipsConsolidation(t *testing.T) {
	turnID := uuid.New()
	tx1 := &mockTx{id: turnID}
	tx2 := &mockTx{id: uuid.New()}
	pool := &mockTxPool{txs: []*mockTx{tx1, tx2}}

	ext := &mockExtractionSvc{candidates: []extraction.Candidate{}, rels: []llm.Relationship{}}
	cons := &mockConsSvc{}

	uc := NewIngestTurnUsecase(pool, ext, cons, &mockRelSvc{})
	uid := "u1"
	out, err := uc.Ingest(context.Background(), newTestTurn("s1", &uid))

	assert.NoError(t, err)
	assert.Equal(t, turnID.String(), out.ID)
	assert.Zero(t, cons.called)
}
