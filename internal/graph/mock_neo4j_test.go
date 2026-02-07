package graph

import (
	"context"
	"fmt"
	"net/url"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// mockRunCall records a single Run invocation.
type mockRunCall struct {
	cypher string
	params map[string]any
}

// mockSession implements sessionRunner for testing.
type mockSession struct {
	calls   []mockRunCall
	runFunc func(cypher string, params map[string]any) (resultIterator, error)
	closed  bool
}

func (m *mockSession) Run(_ context.Context, cypher string, params map[string]any) (resultIterator, error) {
	m.calls = append(m.calls, mockRunCall{cypher: cypher, params: params})
	if m.runFunc != nil {
		return m.runFunc(cypher, params)
	}
	return &mockResult{}, nil
}

func (m *mockSession) Close(_ context.Context) error {
	m.closed = true
	return nil
}

// mockResult implements resultIterator for testing.
type mockResult struct {
	records []*neo4j.Record
	index   int
	err     error
}

func (m *mockResult) Next(_ context.Context) bool {
	if m.index < len(m.records) {
		m.index++
		return true
	}
	return false
}

func (m *mockResult) Record() *neo4j.Record {
	if m.index > 0 && m.index <= len(m.records) {
		return m.records[m.index-1]
	}
	return nil
}

func (m *mockResult) Err() error {
	return m.err
}

// makeRecord creates a *neo4j.Record from key-value pairs.
func makeRecord(kv map[string]any) *neo4j.Record {
	keys := make([]string, 0, len(kv))
	values := make([]any, 0, len(kv))
	for k, v := range kv {
		keys = append(keys, k)
		values = append(values, v)
	}
	return &neo4j.Record{Keys: keys, Values: values}
}

// makeNodeRecord creates a record with standard node fields.
func makeNodeRecord(id, name, typ, source string) *neo4j.Record {
	return makeRecord(map[string]any{
		"id":          id,
		"name":        name,
		"type":        typ,
		"source":      source,
		"source_file": "",
		"provider":    "",
		"metadata":    "",
		"expires_at":  "",
		"last_seen":   "",
		"first_seen":  "",
	})
}

// mockSessionFactory returns a sessionFactory that always returns the given session.
func mockSessionFactory(session *mockSession) sessionFactory {
	return func(_ context.Context) sessionRunner {
		return session
	}
}

// failSessionFactory returns a sessionFactory whose Run always fails.
func failSessionFactory(err error) sessionFactory {
	return func(_ context.Context) sessionRunner {
		return &mockSession{
			runFunc: func(_ string, _ map[string]any) (resultIterator, error) {
				return nil, err
			},
		}
	}
}

// mockDriver implements neo4j.DriverWithContext for testing Close/Driver methods.
// DriverWithContext is an interface with no unexported methods, so it's mockable.
type mockDriver struct {
	closed   bool
	closeErr error
}

func (d *mockDriver) Close(_ context.Context) error {
	d.closed = true
	return d.closeErr
}

func (d *mockDriver) ExecuteQueryBookmarkManager() neo4j.BookmarkManager { return nil }
func (d *mockDriver) IsEncrypted() bool                                  { return false }
func (d *mockDriver) Target() url.URL                                    { return url.URL{} }
func (d *mockDriver) NewSession(_ context.Context, _ neo4j.SessionConfig) neo4j.SessionWithContext {
	return nil
}
func (d *mockDriver) VerifyAuthentication(_ context.Context, _ *neo4j.AuthToken) error { return nil }
func (d *mockDriver) VerifyConnectivity(_ context.Context) error                       { return nil }
func (d *mockDriver) GetServerInfo(_ context.Context) (neo4j.ServerInfo, error) {
	return nil, fmt.Errorf("not implemented")
}
