package mongoshell

import (
	"context"
	"errors"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type cursorState struct {
	cursor    *mongo.Cursor
	namespace string
}
type backend struct {
	client   *mongo.Client
	settings *options.ClientOptions
	cursors  map[int64]cursorState
}

// Retain cancellation, but do not let RunCommand inject maxTimeMS into commands
// such as getMore and killCursors which do not accept it. The shell owns timeouts.
type commandContext struct{ context.Context }

func (commandContext) Deadline() (time.Time, bool) { return time.Time{}, false }

func (b *backend) command(ctx context.Context, database string, command bson.D) (bson.Raw, error) {
	if b.client == nil {
		return nil, errors.New("not connected to MongoDB")
	}
	ctx = commandContext{ctx}
	name := strings.ToLower(command[0].Key)
	if name == "getmore" {
		id, _ := command[0].Value.(int64)
		if state, ok := b.cursors[id]; ok {
			delete(b.cursors, id)
			batchSize := int32(100)
			for _, field := range command {
				if field.Key == "batchSize" {
					switch n := field.Value.(type) {
					case int32:
						batchSize = n
					case int64:
						batchSize = int32(n)
					}
				}
			}
			state.cursor.SetBatchSize(batchSize)
			return b.batch(ctx, state, false)
		}
		return nil, errors.New("unknown or closed Mongo Shell cursor")
	}
	if name == "killcursors" {
		var killed bson.A
		for _, field := range command {
			if field.Key != "cursors" {
				continue
			}
			ids, _ := field.Value.(bson.A)
			for _, value := range ids {
				id, _ := value.(int64)
				if state, ok := b.cursors[id]; ok {
					if err := state.cursor.Close(ctx); err != nil {
						return nil, err
					}
					delete(b.cursors, id)
					killed = append(killed, id)
				}
			}
		}
		return bson.Marshal(bson.D{{Key: "ok", Value: 1}, {Key: "cursorsKilled", Value: killed}})
	}
	runOptions := options.RunCmd()
	write := false
	switch name {
	case "insert", "update", "delete", "findandmodify", "create", "drop", "dropdatabase", "createindexes", "dropindexes":
		write = true
	}
	if b.settings != nil {
		if !write && b.settings.ReadPreference != nil {
			runOptions.SetReadPreference(b.settings.ReadPreference)
		}
		if write && b.settings.WriteConcern != nil && !hasField(command, "writeConcern") {
			wc := b.settings.WriteConcern
			value := bson.D{}
			if wc.W != nil {
				value = append(value, bson.E{Key: "w", Value: wc.W})
			}
			if wc.Journal != nil {
				value = append(value, bson.E{Key: "j", Value: *wc.Journal})
			}
			command = append(command, bson.E{Key: "writeConcern", Value: value})
		}
		if (name == "find" || name == "aggregate" || name == "count" || name == "distinct") && b.settings.ReadConcern != nil && b.settings.ReadConcern.Level != "" && !hasField(command, "readConcern") {
			command = append(command, bson.E{Key: "readConcern", Value: bson.D{{Key: "level", Value: b.settings.ReadConcern.Level}}})
		}
	}
	switch name {
	case "find", "aggregate", "listcollections", "listindexes":
		cursor, err := b.client.Database(database).RunCommandCursor(ctx, command, runOptions)
		if err != nil {
			return nil, err
		}
		collection, ok := command[0].Value.(string)
		if !ok {
			collection = "$cmd." + command[0].Key
		}
		return b.batch(ctx, cursorState{cursor, database + "." + collection}, true)
	default:
		return b.client.Database(database).RunCommand(ctx, command, runOptions).Raw()
	}
}

func hasField(command bson.D, key string) bool {
	for _, field := range command {
		if field.Key == key {
			return true
		}
	}
	return false
}

func (b *backend) batch(ctx context.Context, state cursorState, first bool) (bson.Raw, error) {
	batch := bson.A{}
	if !first && state.cursor.RemainingBatchLength() == 0 && state.cursor.Next(ctx) {
		batch = append(batch, append(bson.Raw(nil), state.cursor.Current...))
	}
	for state.cursor.RemainingBatchLength() > 0 && state.cursor.Next(ctx) {
		batch = append(batch, append(bson.Raw(nil), state.cursor.Current...))
	}
	if err := state.cursor.Err(); err != nil {
		_ = state.cursor.Close(ctx)
		return nil, err
	}
	id := state.cursor.ID()
	if id != 0 {
		b.cursors[id] = state
	} else {
		_ = state.cursor.Close(ctx)
	}
	key := "nextBatch"
	if first {
		key = "firstBatch"
	}
	return bson.Marshal(bson.D{{Key: "ok", Value: 1}, {Key: "cursor", Value: bson.D{{Key: "id", Value: id}, {Key: "ns", Value: state.namespace}, {Key: key, Value: batch}}}})
}

func (b *backend) close() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for id, state := range b.cursors {
		_ = state.cursor.Close(ctx)
		delete(b.cursors, id)
	}
}
