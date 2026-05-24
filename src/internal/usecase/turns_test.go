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
	"github.com/stretchr/testify/mock"

	"memory-service/internal/adapters/llm"
	"memory-service/internal/adapters/store"
	"memory-service/internal/service/consolidation"
	"memory-service/internal/service/extraction"
)

// ---- pgx infrastructure stubs ----
// These implement external library interfaces and are kept as minimal hand-written stubs.

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

type emptyRows struct{}

func (r *emptyRows) Close()                                       {}
func (r *emptyRows) Err() error                                   { return nil }
func (r *emptyRows) Next() bool                                   { return false }
func (r *emptyRows) Scan(...any) error                            { return nil }
func (r *emptyRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (r *emptyRows) Values() ([]any, error)                       { return nil, nil }
func (r *emptyRows) RawValues() [][]byte                          { return nil }
func (r *emptyRows) Conn() *pgx.Conn                              { return nil }
func (r *emptyRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }

type mockTx struct {
	id        uuid.UUID
	commitErr error
}

func (m *mockTx) Commit(ctx context.Context) error    { return m.commitErr }
func (m *mockTx) Rollback(ctx context.Context) error  { return nil }
func (m *mockTx) Exec(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}
func (m *mockTx) Query(_ context.Context, _ string, _ ...any) (pgx.Rows, error) {
	return &emptyRows{}, nil
}
func (m *mockTx) QueryRow(_ context.Context, _ string, _ ...any) pgx.Row {
	return &mockPgxRow{id: m.id}
}
func (m *mockTx) Begin(_ context.Context) (pgx.Tx, error) { return nil, nil }
func (m *mockTx) CopyFrom(_ context.Context, _ pgx.Identifier, _ []string, _ pgx.CopyFromSource) (int64, error) {
	return 0, nil
}
func (m *mockTx) SendBatch(_ context.Context, _ *pgx.Batch) pgx.BatchResults { return nil }
func (m *mockTx) LargeObjects() pgx.LargeObjects                              { return pgx.LargeObjects{} }
func (m *mockTx) Prepare(_ context.Context, _, _ string) (*pgconn.StatementDescription, error) {
	return nil, nil
}
func (m *mockTx) Conn() *pgx.Conn { return nil }

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

// ---- testify/mock service mocks ----

// MockExtractionService mocks ExtractionService using testify/mock.
type MockExtractionService struct{ mock.Mock }

func (m *MockExtractionService) Extract(ctx context.Context, input extraction.ExtractionInput) ([]extraction.Candidate, []llm.Relationship, error) {
	args := m.Called(ctx, input)
	cands, _ := args.Get(0).([]extraction.Candidate)
	rels, _ := args.Get(1).([]llm.Relationship)
	return cands, rels, args.Error(2)
}

func (m *MockExtractionService) Embed(ctx context.Context, text string) ([]float32, error) {
	args := m.Called(ctx, text)
	emb, _ := args.Get(0).([]float32)
	return emb, args.Error(1)
}

// MockConsolidationService mocks ConsolidationService using testify/mock.
type MockConsolidationService struct{ mock.Mock }

func (m *MockConsolidationService) ConsolidateFact(
	ctx context.Context,
	q store.Querier,
	userID, memType string,
	key *string,
	value, evidence string,
	confidence float32,
	entities []string,
	embedding []float32,
	sourceSession *string,
	sourceTurn *uuid.UUID,
) (uuid.UUID, consolidation.Result, error) {
	args := m.Called(ctx, q, userID, memType, key, value, evidence, confidence, entities, embedding, sourceSession, sourceTurn)
	return args.Get(0).(uuid.UUID), args.Get(1).(consolidation.Result), args.Error(2)
}

// MockRelationshipsService mocks RelationshipsService using testify/mock.
type MockRelationshipsService struct{ mock.Mock }

func (m *MockRelationshipsService) ProcessRelationships(
	ctx context.Context,
	q store.Querier,
	userID string,
	rels []llm.Relationship,
	entityMemoryMap map[string]uuid.UUID,
	fallbackMemoryID uuid.UUID,
) error {
	args := m.Called(ctx, q, userID, rels, entityMemoryMap, fallbackMemoryID)
	return args.Error(0)
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
	ext := new(MockExtractionService)

	uc := NewIngestTurnUsecase(pool, ext, new(MockConsolidationService), new(MockRelationshipsService), nil)
	out, err := uc.Ingest(context.Background(), newTestTurn("s1", nil))

	assert.NoError(t, err)
	assert.Equal(t, turnID.String(), out.ID)
	ext.AssertNotCalled(t, "Extract")
}

func TestIngest_NilExtractor_SkipsExtraction(t *testing.T) {
	turnID := uuid.New()
	pool := &mockTxPool{txs: []*mockTx{{id: turnID}}}

	uc := NewIngestTurnUsecase(pool, nil, new(MockConsolidationService), new(MockRelationshipsService), nil)
	uid := "u1"
	out, err := uc.Ingest(context.Background(), newTestTurn("s1", &uid))

	assert.NoError(t, err)
	assert.Equal(t, turnID.String(), out.ID)
}

func TestIngest_BeginTxError_ReturnsError(t *testing.T) {
	pool := &mockTxPool{beginErr: errors.New("connection refused")}
	uc := NewIngestTurnUsecase(pool, nil, nil, nil, nil)

	_, err := uc.Ingest(context.Background(), newTestTurn("s1", nil))
	assert.ErrorContains(t, err, "begin transaction")
}

func TestIngest_CommitError_ReturnsError(t *testing.T) {
	tx := &mockTx{id: uuid.New(), commitErr: errors.New("commit failed")}
	pool := &mockTxPool{txs: []*mockTx{tx}}
	uc := NewIngestTurnUsecase(pool, nil, nil, nil, nil)

	_, err := uc.Ingest(context.Background(), newTestTurn("s1", nil))
	assert.ErrorContains(t, err, "commit transaction")
}

func TestIngest_WithUserID_ExtractorCalled(t *testing.T) {
	turnID := uuid.New()
	pool := &mockTxPool{txs: []*mockTx{{id: turnID}, {id: uuid.New()}}}

	cands := []extraction.Candidate{
		{Type: "fact", Key: strPtr("name"), Value: "Alice", Evidence: "explicit", Confidence: 0.95, Entities: []string{"alice"}},
	}

	ext := new(MockExtractionService)
	ext.On("Extract", mock.Anything, mock.Anything).Return(cands, []llm.Relationship{}, nil)
	ext.On("Embed", mock.Anything, mock.Anything).Return([]float32{0.1, 0.2}, nil)

	cons := new(MockConsolidationService)
	cons.On("ConsolidateFact",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything,
		mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything,
		mock.Anything, mock.Anything,
	).Return(uuid.New(), consolidation.ResultADD, nil)

	rel := new(MockRelationshipsService)

	uc := NewIngestTurnUsecase(pool, ext, cons, rel, nil)
	uid := "u1"
	out, err := uc.Ingest(context.Background(), newTestTurn("s1", &uid))

	assert.NoError(t, err)
	assert.Equal(t, turnID.String(), out.ID)
	ext.AssertExpectations(t)
	cons.AssertExpectations(t)
	rel.AssertNotCalled(t, "ProcessRelationships")
}

func TestIngest_ExtractionError_NonFatal(t *testing.T) {
	turnID := uuid.New()
	pool := &mockTxPool{txs: []*mockTx{{id: turnID}}}

	ext := new(MockExtractionService)
	ext.On("Extract", mock.Anything, mock.Anything).Return(nil, nil, errors.New("llm error"))

	cons := new(MockConsolidationService)

	uc := NewIngestTurnUsecase(pool, ext, cons, new(MockRelationshipsService), nil)
	uid := "u1"
	out, err := uc.Ingest(context.Background(), newTestTurn("s1", &uid))

	assert.NoError(t, err, "extraction error must be non-fatal")
	assert.Equal(t, turnID.String(), out.ID)
	ext.AssertExpectations(t)
	cons.AssertNotCalled(t, "ConsolidateFact")
}

func TestIngest_ZeroCandidates_SkipsConsolidation(t *testing.T) {
	turnID := uuid.New()
	pool := &mockTxPool{txs: []*mockTx{{id: turnID}}}

	ext := new(MockExtractionService)
	ext.On("Extract", mock.Anything, mock.Anything).Return([]extraction.Candidate{}, []llm.Relationship{}, nil)

	cons := new(MockConsolidationService)

	uc := NewIngestTurnUsecase(pool, ext, cons, new(MockRelationshipsService), nil)
	uid := "u1"
	out, err := uc.Ingest(context.Background(), newTestTurn("s1", &uid))

	assert.NoError(t, err)
	assert.Equal(t, turnID.String(), out.ID)
	ext.AssertExpectations(t)
	cons.AssertNotCalled(t, "ConsolidateFact")
}
