package mongo

import (
	"errors"

	mongodriver "go.mongodb.org/mongo-driver/v2/mongo"
)

// IsDuplicateKey reports whether err is a MongoDB duplicate-key error (E11000 and
// its relatives), including when it is wrapped or nested inside a WriteException or
// BulkWriteException. It delegates to the driver's own IsDuplicateKeyError, which
// inspects the error chain via the ServerError interface, so app code can ask "was
// this a duplicate key?" without unwrapping driver error types by hand.
func IsDuplicateKey(err error) bool {
	return mongodriver.IsDuplicateKeyError(err)
}

// IsNotFound reports whether err is the driver's "no documents in result"
// sentinel, returned by SingleResult.Decode / FindOne when nothing matched. It
// traverses the error chain, so a wrapped ErrNoDocuments still matches.
func IsNotFound(err error) bool {
	return errors.Is(err, mongodriver.ErrNoDocuments)
}
