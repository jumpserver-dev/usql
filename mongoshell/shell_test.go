package mongoshell

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func TestBSONAndPersistentJavaScript(t *testing.T) {
	var output bytes.Buffer
	s, err := newShell(nil, "test", &output)
	if err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{
		`let id = ObjectId("507f1f77bcf86cd799439011"); let n = NumberLong("9223372036854775807")`,
		`assert.eq(id, EJSON.parse(EJSON.stringify(id))); assert.eq(n, EJSON.parse(EJSON.stringify(n)))`,
		`assert.eq({$numberInt:"7"}, EJSON.serialize(new NumberInt(7)))`,
		`assert.eq({$date:{$numberLong:"1577836800000"}}, EJSON.serialize(ISODate("2020-01-01T00:00:00Z")))`,
		`assert.eq(3, [1, 2].map(x => x + 1)[0] + 1)`,
		`use audit_test`,
		`print(db.getName(), id.toHexString(), n.toString())`,
	} {
		if err := s.Eval(context.Background(), command, true); err != nil {
			t.Fatalf("%s: %v", command, err)
		}
	}
	if !strings.Contains(output.String(), "audit_test 507f1f77bcf86cd799439011 9223372036854775807") {
		t.Fatal(output.String())
	}
	if s.Database() != "audit_test" {
		t.Fatal("use did not change the prompt database")
	}
}

func TestCommandPreservesBSONAndCursorPagination(t *testing.T) {
	var commands []bson.D
	var output bytes.Buffer
	s, err := newRuntime(func(ctx context.Context, database string, cmd bson.D) (bson.Raw, error) {
		if database != "test" {
			t.Fatalf("database = %q", database)
		}
		commands = append(commands, cmd)
		var response bson.D
		if len(commands) == 1 {
			if cmd[0].Key != "find" {
				t.Fatal(cmd)
			}
			response = bson.D{{Key: "ok", Value: 1}, {Key: "cursor", Value: bson.D{{Key: "id", Value: int64(9007199254740993)}, {Key: "ns", Value: "test.users"}, {Key: "firstBatch", Value: bson.A{bson.D{{Key: "n", Value: 1}}}}}}}
		} else {
			if cmd[0].Key != "getMore" || cmd[0].Value != int64(9007199254740993) {
				t.Fatalf("cursor id lost precision: %v", cmd)
			}
			response = bson.D{{Key: "ok", Value: 1}, {Key: "cursor", Value: bson.D{{Key: "id", Value: int64(0)}, {Key: "ns", Value: "test.users"}, {Key: "nextBatch", Value: bson.A{bson.D{{Key: "n", Value: 2}}}}}}}
		}
		return bson.Marshal(response)
	}, "test", &output)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Eval(context.Background(), `db.users.find({_id:ObjectId("507f1f77bcf86cd799439011"), n:NumberLong("9223372036854775807")}).sort({n:1}).limit(2).toArray()`, true); err != nil {
		t.Fatal(err)
	}
	if len(commands) != 2 {
		t.Fatalf("commands = %v", commands)
	}
	filter := commands[0][1].Value.(bson.D)
	if _, ok := filter[0].Value.(bson.ObjectID); !ok {
		t.Fatal("ObjectId was not sent as BSON ObjectID")
	}
	if filter[1].Value != int64(9223372036854775807) {
		t.Fatal("NumberLong lost precision")
	}
	if !strings.Contains(output.String(), "n: 1") || !strings.Contains(output.String(), "n: 2") {
		t.Fatal(output.String())
	}
}

func TestInterruptRecoversRuntime(t *testing.T) {
	s, err := newShell(nil, "test", &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	s.timeout = 20 * time.Millisecond
	if err := s.Eval(context.Background(), `while (true) {}`, false); err == nil {
		t.Fatal("infinite script was not interrupted")
	}
	if err := s.Eval(context.Background(), `assert.eq(2, 1+1)`, false); err != nil {
		t.Fatal(err)
	}
	if err := s.Eval(context.Background(), `quit()`, false); !errors.Is(err, errQuit) {
		t.Fatal(err)
	}
}

func TestMongoServerErrorFields(t *testing.T) {
	s, err := newRuntime(func(context.Context, string, bson.D) (bson.Raw, error) {
		return nil, mongo.CommandError{Code: 26, Name: "NamespaceNotFound", Message: "missing collection"}
	}, "test", &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Eval(context.Background(), `try { db.runCommand({collStats:"missing"}); throw new Error("command should fail"); } catch(e) { assert.eq(26,e.code); assert.eq("NamespaceNotFound",e.codeName); assert.eq("MongoServerError",e.name); }`, false); err != nil {
		t.Fatal(err)
	}
}

func TestLiveMongoShell(t *testing.T) {
	uri := os.Getenv("USQL_MONGO_TEST_URI")
	if uri == "" {
		t.Skip("set USQL_MONGO_TEST_URI to a disposable MongoDB instance")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client, err := mongo.Connect(options.Client().ApplyURI(uri).SetServerSelectionTimeout(5 * time.Second))
	if err != nil {
		t.Fatal(err)
	}
	database := fmt.Sprintf("usql_shell_test_%d", time.Now().UnixNano())
	t.Cleanup(func() {
		cleanCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = client.Database(database).Drop(cleanCtx)
		_ = client.Disconnect(cleanCtx)
	})
	s, err := newShell(client, database, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	commands := []string{
		`db.missing.drop(); assert.eq([], db.missing.getIndexes())`,
		`var docs = []; for (let i=0;i<25;i++) docs.push({_id:ObjectId(), n:i, date:ISODate("2020-01-01"), large:NumberLong("9223372036854775807"), price:NumberDecimal("1.25")}); assert.eq(25, Object.keys(db.items.insertMany(docs).insertedIds).length)`,
		`assert.eq(25, Number(db.items.countDocuments({}))); assert.eq(25, db.items.find().batchSize(2).toArray().length)`,
		`assert.eq([24,23,22], db.items.find({n:{$gte:20}}, {_id:0,n:1}).sort({n:-1}).limit(3).map(x=>x.n))`,
		`assert.eq("9223372036854775807", db.items.findOne({n:0}).large.toString()); assert(db.items.findOne({n:0})._id instanceof ObjectId)`,
		`assert.eq(1, db.items.updateOne({n:0}, {$set:{name:"zero"}}).modifiedCount); assert.eq("zero", db.items.findOne({n:0}).name)`,
		`assert.eq(5, db.items.updateMany({n:{$gte:20}}, {$set:{high:true}}).matchedCount)`,
		`assert.eq("changed", db.items.findOneAndUpdate({n:1}, {$set:{name:"changed"}}, {returnDocument:"after"}).name)`,
		`assert.eq(25, Number(db.items.aggregate([{$group:{_id:null,n:{$sum:1}}}]).next().n))`,
		`assert.eq("n_1", db.items.createIndex({n:1})); assert(db.items.getIndexes().length>=2)`,
		`assert(db.getCollectionNames().includes("items")); assert.commandWorked(db.runCommand({ping:1}))`,
		`assert.eq(1, db.items.deleteOne({n:0}).deletedCount); assert.eq(5, db.items.deleteMany({high:true}).deletedCount)`,
		`let bulk = db.items.bulkWrite([{insertOne:{document:{_id:"bulk",n:100}}},{updateOne:{filter:{n:2},update:{$set:{name:"bulk-updated"}}}},{deleteOne:{filter:{_id:"bulk"}}}]); assert.eq(1,bulk.insertedCount); assert.eq(1,bulk.modifiedCount); assert.eq(1,bulk.deletedCount)`,
		`let seen = 0; db.items.find().batchSize(2).forEach(doc=>{seen++}); assert.eq(19, seen)`,
		`show collections`,
	}
	for _, command := range commands {
		if err := s.Eval(ctx, command, false); err != nil {
			t.Fatalf("%s: %v", command, err)
		}
	}
}
