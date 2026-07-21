package mongostore

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	mongodriver "go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"

	"github.com/dmitrymomot/forge/auth/session"
	"github.com/dmitrymomot/forge/core/id"
	"github.com/dmitrymomot/forge/crypto/digest"
	"github.com/dmitrymomot/forge/web/fingerprint"
)

var collectionNameRe = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// sessionDoc is the BSON shape of one session record. _id is the hex SHA-256
// of the bearer token (never the token itself), session_id the stable UUID as
// its canonical string — UUIDv7 hex sorts lexicographically in time order, so
// a descending sort on it is newest-first. The fingerprint digest is stored
// as its JSON encoding, matching pgstore, so the wire shape stays identical
// across drivers.
type sessionDoc struct {
	CreatedAt   time.Time `bson:"created_at"`
	ExpiresAt   time.Time `bson:"expires_at"`
	LastSeenAt  time.Time `bson:"last_seen_at"`
	ID          string    `bson:"_id"`
	SessionID   string    `bson:"session_id"`
	UserID      string    `bson:"user_id"`
	Scope       string    `bson:"scope"`
	IP          string    `bson:"ip,omitempty"`
	UserAgent   string    `bson:"user_agent,omitempty"`
	Data        []byte    `bson:"data,omitempty"`
	Fingerprint []byte    `bson:"fingerprint,omitempty"`
}

// Store is the MongoDB implementation of session.Store + session.UserIndex.
// Tokens are persisted only as SHA-256 digests, so a database leak exposes no
// usable credentials. The client's lifecycle is the caller's.
type Store struct {
	coll *mongodriver.Collection
}

var (
	_ session.Store     = (*Store)(nil)
	_ session.UserIndex = (*Store)(nil)
)

// config carries construction-time settings.
type config struct {
	collection string
}

// Option configures New.
type Option func(*config)

// WithCollection overrides the collection name (default "forge_sessions").
func WithCollection(name string) Option {
	return func(c *config) { c.collection = name }
}

// New builds a MongoDB session Store over db. It performs no I/O; call
// EnsureIndexes once at boot. The collection handle is pinned to primary
// reads regardless of the client's read preference: a session store must
// read its own acknowledged writes — a secondary-lagging read would miss a
// just-saved session or, worse, resurrect a just-revoked one.
func New(db *mongodriver.Database, opts ...Option) (*Store, error) {
	cfg := config{collection: "forge_sessions"}
	for _, opt := range opts {
		opt(&cfg)
	}
	if db == nil {
		return nil, errors.New("mongostore: nil database")
	}
	if !collectionNameRe.MatchString(cfg.collection) {
		return nil, fmt.Errorf("mongostore: invalid collection name %q", cfg.collection)
	}
	coll := db.Collection(cfg.collection, options.Collection().SetReadPreference(readpref.Primary()))
	return &Store{coll: coll}, nil
}

// EnsureIndexes idempotently creates the indexes the store relies on: the
// user-listing index over authenticated sessions and a TTL index on
// expires_at, so MongoDB's TTL monitor reaps expired sessions natively
// (roughly once a minute — the Manager refuses expired records regardless,
// so the lag is invisible to callers). Run once at boot; re-running is a
// server-side no-op.
func (s *Store) EnsureIndexes(ctx context.Context) error {
	models := []mongodriver.IndexModel{
		{
			Keys: bson.D{{Key: "scope", Value: 1}, {Key: "user_id", Value: 1}, {Key: "session_id", Value: -1}},
			Options: options.Index().SetName("session_user").
				SetPartialFilterExpression(bson.D{{Key: "user_id", Value: bson.D{{Key: "$gt", Value: ""}}}}),
		},
		{
			Keys:    bson.D{{Key: "expires_at", Value: 1}},
			Options: options.Index().SetName("session_ttl").SetExpireAfterSeconds(0),
		},
	}
	if _, err := s.coll.Indexes().CreateMany(ctx, models); err != nil {
		return fmt.Errorf("mongostore: ensure indexes: %w", err)
	}
	return nil
}

func tokenHash(token string) string {
	return hex.EncodeToString(digest.SHA256([]byte(token)))
}

// Save upserts rec under token's digest; the token is returned unchanged.
func (s *Store) Save(ctx context.Context, token string, rec session.Record) (string, error) {
	doc := sessionDoc{
		ID:         tokenHash(token),
		SessionID:  rec.ID.String(),
		UserID:     rec.UserID,
		Scope:      rec.Scope,
		IP:         rec.IP,
		UserAgent:  rec.UserAgent,
		Data:       rec.Data,
		CreatedAt:  rec.CreatedAt,
		ExpiresAt:  rec.ExpiresAt,
		LastSeenAt: rec.LastSeenAt,
	}
	if rec.Fingerprint.Hash != "" {
		b, err := json.Marshal(rec.Fingerprint)
		if err != nil {
			return "", err
		}
		doc.Fingerprint = b
	}
	_, err := s.coll.ReplaceOne(ctx, bson.D{{Key: "_id", Value: doc.ID}}, doc,
		options.Replace().SetUpsert(true))
	if err != nil {
		return "", err
	}
	return token, nil
}

// Load returns the record for token, or session.ErrNotFound.
func (s *Store) Load(ctx context.Context, token string) (session.Record, error) {
	var doc sessionDoc
	err := s.coll.FindOne(ctx, bson.D{{Key: "_id", Value: tokenHash(token)}}).Decode(&doc)
	if errors.Is(err, mongodriver.ErrNoDocuments) {
		return session.Record{}, session.ErrNotFound
	}
	if err != nil {
		return session.Record{}, err
	}
	return recordOf(doc)
}

// Delete removes the record for token; absent tokens are a no-op.
func (s *Store) Delete(ctx context.Context, token string) error {
	_, err := s.coll.DeleteOne(ctx, bson.D{{Key: "_id", Value: tokenHash(token)}})
	return err
}

// ListByUser returns the records bound to userID within scope, newest first.
func (s *Store) ListByUser(ctx context.Context, scope, userID string) ([]session.Record, error) {
	cur, err := s.coll.Find(ctx,
		bson.D{{Key: "scope", Value: scope}, {Key: "user_id", Value: userID}},
		options.Find().SetSort(bson.D{{Key: "session_id", Value: -1}}))
	if err != nil {
		return nil, err
	}
	defer func() { _ = cur.Close(ctx) }()
	out := []session.Record{}
	for cur.Next(ctx) {
		var doc sessionDoc
		if err := cur.Decode(&doc); err != nil {
			return nil, err
		}
		rec, err := recordOf(doc)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, cur.Err()
}

// DeleteByUser removes every record bound to userID within scope, except the
// ids in keep.
func (s *Store) DeleteByUser(ctx context.Context, scope, userID string, keep ...id.UUID) error {
	filter := bson.D{{Key: "scope", Value: scope}, {Key: "user_id", Value: userID}}
	if len(keep) > 0 {
		keepIDs := make([]string, len(keep))
		for i, k := range keep {
			keepIDs[i] = k.String()
		}
		filter = append(filter, bson.E{Key: "session_id", Value: bson.D{{Key: "$nin", Value: keepIDs}}})
	}
	_, err := s.coll.DeleteMany(ctx, filter)
	return err
}

// DeleteOne removes the record for sessionID when it is bound to userID
// within scope; anything else is a no-op.
func (s *Store) DeleteOne(ctx context.Context, scope, userID string, sessionID id.UUID) error {
	_, err := s.coll.DeleteOne(ctx, bson.D{
		{Key: "scope", Value: scope},
		{Key: "user_id", Value: userID},
		{Key: "session_id", Value: sessionID.String()},
	})
	return err
}

func recordOf(doc sessionDoc) (session.Record, error) {
	var sid id.UUID
	if err := sid.UnmarshalText([]byte(doc.SessionID)); err != nil {
		return session.Record{}, fmt.Errorf("mongostore: corrupt session_id %q: %w", doc.SessionID, err)
	}
	rec := session.Record{
		ID:         sid,
		UserID:     doc.UserID,
		Scope:      doc.Scope,
		IP:         doc.IP,
		UserAgent:  doc.UserAgent,
		Data:       doc.Data,
		CreatedAt:  doc.CreatedAt,
		ExpiresAt:  doc.ExpiresAt,
		LastSeenAt: doc.LastSeenAt,
	}
	if len(doc.Fingerprint) > 0 {
		var d fingerprint.Digest
		if err := json.Unmarshal(doc.Fingerprint, &d); err != nil {
			return session.Record{}, err
		}
		rec.Fingerprint = d
	}
	return rec, nil
}
