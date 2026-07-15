package mongo_test

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"

	mongodriver "go.mongodb.org/mongo-driver/v2/mongo"

	forgemongo "github.com/dmitrymomot/forge/data/mongo"
)

func TestClose_NilLoggerTolerated(t *testing.T) {
	// Close must tolerate a nil *mongo.Database and a nil logger without panicking;
	// the log line is simply skipped. This is the pure, server-free contract.
	assert.NotPanics(t, func() {
		forgemongo.Close(nil, nil)
	})
	assert.NotPanics(t, func() {
		var db *mongodriver.Database
		forgemongo.Close(db, slog.New(slog.DiscardHandler))
	})
}
