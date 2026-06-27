package mongo_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	mongodriver "go.mongodb.org/mongo-driver/v2/mongo"

	forgemongo "github.com/dmitrymomot/forge/mongo"
)

func TestIsDuplicateKey(t *testing.T) {
	// CommandError carries an int32 Code; WriteError/WriteException carry int Codes.
	// E11000 is the duplicate-key code in every form, including wrapped and inside a
	// BulkWriteException.
	cmdErr := mongodriver.CommandError{Code: 11000, Message: "E11000 duplicate key error"}
	writeExc := mongodriver.WriteException{
		WriteErrors: mongodriver.WriteErrors{{Code: 11000, Message: "E11000 duplicate key"}},
	}
	bulkExc := mongodriver.BulkWriteException{
		WriteErrors: []mongodriver.BulkWriteError{
			{WriteError: mongodriver.WriteError{Code: 11000, Message: "E11000 duplicate key"}},
		},
	}

	assert.True(t, forgemongo.IsDuplicateKey(cmdErr))
	assert.True(t, forgemongo.IsDuplicateKey(writeExc))
	assert.True(t, forgemongo.IsDuplicateKey(bulkExc))
	// Wrapped is still detected (errors.As traverses the chain).
	assert.True(t, forgemongo.IsDuplicateKey(fmt.Errorf("insert failed: %w", writeExc)))

	// Non-duplicate codes and unrelated errors are false.
	assert.False(t, forgemongo.IsDuplicateKey(mongodriver.CommandError{Code: 26}))
	assert.False(t, forgemongo.IsDuplicateKey(errors.New("nope")))
	assert.False(t, forgemongo.IsDuplicateKey(nil))
}

func TestIsNotFound(t *testing.T) {
	assert.True(t, forgemongo.IsNotFound(mongodriver.ErrNoDocuments))
	assert.True(t, forgemongo.IsNotFound(fmt.Errorf("decode: %w", mongodriver.ErrNoDocuments)))
	assert.False(t, forgemongo.IsNotFound(errors.New("other")))
	assert.False(t, forgemongo.IsNotFound(nil))
}
