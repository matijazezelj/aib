package graph

import (
	"context"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// resultIterator abstracts the subset of neo4j.ResultWithContext we use.
type resultIterator interface {
	Next(ctx context.Context) bool
	Record() *neo4j.Record
	Err() error
}

// sessionRunner abstracts the subset of neo4j.SessionWithContext we use.
type sessionRunner interface {
	Run(ctx context.Context, cypher string, params map[string]any) (resultIterator, error)
	Close(ctx context.Context) error
}

// sessionFactory creates a new sessionRunner for a given context.
type sessionFactory func(ctx context.Context) sessionRunner

// neo4jSessionAdapter wraps a real neo4j.SessionWithContext to implement sessionRunner.
type neo4jSessionAdapter struct {
	session neo4j.SessionWithContext
}

func (a *neo4jSessionAdapter) Run(ctx context.Context, cypher string, params map[string]any) (resultIterator, error) {
	return a.session.Run(ctx, cypher, params)
}

func (a *neo4jSessionAdapter) Close(ctx context.Context) error {
	return a.session.Close(ctx)
}

// newNeo4jSessionFactory returns a sessionFactory backed by a real neo4j driver.
func newNeo4jSessionFactory(driver neo4j.DriverWithContext) sessionFactory {
	return func(ctx context.Context) sessionRunner {
		return &neo4jSessionAdapter{session: driver.NewSession(ctx, neo4j.SessionConfig{})}
	}
}
