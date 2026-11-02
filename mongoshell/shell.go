// Package mongoshell implements MongoDB's JavaScript shell inside usql.
package mongoshell

import (
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/dop251/goja"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

//go:embed shell.js
var bootstrap string

type commandRunner func(context.Context, string, bson.D) (bson.Raw, error)

type Shell struct {
	vm       *goja.Runtime
	run      commandRunner
	out      io.Writer
	ctx      context.Context
	jsonMode string
	exit     bool
	timeout  time.Duration
	cleanup  func()
}

func newShell(client *mongo.Client, database string, out io.Writer, settings ...*options.ClientOptions) (*Shell, error) {
	backend := &backend{client: client, cursors: make(map[int64]cursorState)}
	if len(settings) > 0 {
		backend.settings = settings[0]
	}
	s, err := newRuntime(backend.command, database, out)
	if err == nil {
		s.cleanup = backend.close
	}
	return s, err
}

func newRuntime(run commandRunner, database string, out io.Writer) (*Shell, error) {
	s := &Shell{vm: goja.New(), run: run, out: out, timeout: 2 * time.Minute}
	s.vm.SetMaxCallStackSize(1024)
	_ = s.vm.Set("__database", database)
	_ = s.vm.Set("__command", func(db, payload string) string {
		var command bson.D
		if err := bson.UnmarshalExtJSON([]byte(payload), false, &command); err != nil {
			panic(s.vm.NewGoError(err))
		}
		if len(command) == 0 {
			panic(s.vm.NewTypeError("MongoDB command must not be empty"))
		}
		raw, err := s.run(s.ctx, db, command)
		if err != nil {
			exception := s.vm.NewGoError(err)
			var commandError mongo.CommandError
			if errors.As(err, &commandError) {
				_ = exception.Set("name", "MongoServerError")
				_ = exception.Set("code", commandError.Code)
				_ = exception.Set("codeName", commandError.Name)
			}
			panic(exception)
		}
		encoded, err := bson.MarshalExtJSON(raw, true, false)
		if err != nil {
			panic(s.vm.NewGoError(err))
		}
		return string(encoded)
	})
	_ = s.vm.Set("__objectID", func() string { return bson.NewObjectID().Hex() })
	_ = s.vm.Set("__uuid", func(value string) string {
		data, err := hex.DecodeString(strings.ReplaceAll(value, "-", ""))
		if err != nil || len(data) != 16 {
			panic(s.vm.NewTypeError("UUID requires 32 hexadecimal digits"))
		}
		return base64.StdEncoding.EncodeToString(data)
	})
	_ = s.vm.Set("load", func(paths ...string) bool {
		for _, path := range paths {
			source, err := readScript(path)
			if err != nil {
				panic(s.vm.NewGoError(err))
			}
			if _, err := s.vm.RunScript(path, source); err != nil {
				panic(err)
			}
		}
		return true
	})
	_ = s.vm.Set("__print", func(text string) {
		if _, err := fmt.Fprintln(out, text); err != nil {
			panic(s.vm.NewGoError(err))
		}
	})
	_ = s.vm.Set("__quit", func(code int) {
		if code != 0 {
			panic(s.vm.NewGoError(fmt.Errorf("shell exited with status %d", code)))
		}
		s.exit = true
		panic(s.vm.NewGoError(errQuit))
	})
	if _, err := s.vm.RunString(bootstrap); err != nil {
		return nil, err
	}
	return s, nil
}

var errQuit = errors.New("shell exit")
var useCommand = regexp.MustCompile(`^use\s+([^\s;]+)\s*;?$`)
var showCommand = regexp.MustCompile(`^show\s+(dbs|databases|collections|tables|users|roles|profile|logs)\s*;?$`)

func (s *Shell) Database() string {
	if db, ok := s.vm.Get("db").(*goja.Object); ok {
		if name := db.Get("_name"); name != nil && !goja.IsUndefined(name) {
			return name.String()
		}
	}
	return "mongo"
}

func (s *Shell) Close() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = s.Eval(ctx, "__closeCursors()", false)
	if s.cleanup != nil {
		s.cleanup()
		s.cleanup = nil
	}
}

// Eval evaluates in a persistent runtime; interrupting a command leaves the shell usable.
func (s *Shell) Eval(ctx context.Context, source string, display bool) error {
	source = strings.TrimSpace(source)
	if source == "" {
		return nil
	}
	if m := useCommand.FindStringSubmatch(source); m != nil {
		name, _ := json.Marshal(m[1])
		source = "db = db.getSiblingDB(" + string(name) + "); undefined"
	} else if m := showCommand.FindStringSubmatch(source); m != nil {
		source = "__show(" + fmt.Sprintf("%q", m[1]) + ")"
	} else {
		switch strings.TrimSuffix(source, ";") {
		case "exit", "quit":
			source = "quit()"
		case "help":
			source = "help()"
		case "it":
			source = "__more()"
		}
	}
	// Database operations are synchronous; a leading mongosh await is redundant.
	source = strings.TrimPrefix(source, "await ")
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	s.ctx = ctx
	finished := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		select {
		case <-ctx.Done():
			s.vm.Interrupt(ctx.Err())
		case <-finished:
		}
	}()
	defer func() { close(finished); <-done; s.vm.ClearInterrupt() }()
	value, err := s.vm.RunString(source)
	if s.exit {
		return errQuit
	}
	if err != nil {
		return err
	}
	if display && !goja.IsUndefined(value) {
		if err = s.vm.Set("__result", value); err != nil {
			return err
		}
		_, err = s.vm.RunString("__display(__result, " + fmt.Sprintf("%q", s.jsonMode) + ")")
		_ = s.vm.Set("__result", goja.Undefined())
	}
	return err
}

// Complete returns lexical shell completions without making network calls.
func (s *Shell) Complete(line string) []string {
	words := []string{"db.", "show dbs", "show collections", "use ", "help", "exit", "it",
		"db.getCollection(", "db.getCollectionNames()", "db.runCommand(", "db.getSiblingDB(",
		"db.stats()", "db.serverStatus()", "rs.status()", "rs.conf()", "sh.status()"}
	var matches []string
	for _, word := range words {
		if strings.HasPrefix(word, line) {
			matches = append(matches, word)
		}
	}
	return matches
}
