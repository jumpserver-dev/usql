package mongoshell

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/chzyer/readline"
	"github.com/dop251/goja"
	"github.com/spf13/cobra"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/x/mongo/driver/connstring"
	"golang.org/x/term"
)

// IsInvocation routes MongoDB URLs and explicit --mongo / mongo mode before SQL parsing.
func IsInvocation(args []string) bool {
	for _, arg := range args {
		if arg == "--mongo" || strings.HasPrefix(arg, "mongodb://") || strings.HasPrefix(arg, "mongodb+srv://") {
			return true
		}
	}
	return len(args) > 0 && args[0] == "mongo"
}

type cliOptions struct {
	username, password, authDB, host, port                string
	tls, insecure, noDB, quiet, shell, noInit, noPassword bool
	caFile, certFile, jsonMode                            string
	evals, files                                          []string
}

// New creates the MongoDB command without changing usql's SQL flags.
func New(args []string, version string) *cobra.Command {
	o := &cliOptions{}
	c := &cobra.Command{
		Use:           "usql --mongo [mongodb://host/database] [flags] [scripts.js...]",
		Short:         "MongoDB JavaScript shell (Go driver and Goja)",
		Version:       version,
		SilenceErrors: true, SilenceUsage: true,
		RunE: func(cmd *cobra.Command, positional []string) error { return o.run(cmd.Context(), positional) },
	}
	if len(args) > 0 && args[0] == "mongo" {
		args = args[1:]
	}
	c.SetArgs(normalizePasswordArgs(args))
	f := c.Flags()
	f.Bool("mongo", false, "use Mongo Shell mode")
	f.StringVarP(&o.username, "username", "u", "", "MongoDB username")
	f.StringVarP(&o.password, "password", "p", "", "password; omit its value to prompt")
	f.Lookup("password").NoOptDefVal = "\x00prompt"
	f.StringVar(&o.authDB, "authenticationDatabase", "", "authentication database")
	f.StringVar(&o.host, "host", "", "MongoDB host (default localhost)")
	f.StringVar(&o.port, "port", "", "MongoDB port (default 27017)")
	f.BoolVar(&o.tls, "tls", false, "enable TLS")
	f.BoolVar(&o.insecure, "tlsAllowInvalidCertificates", false, "allow invalid TLS certificates")
	f.BoolVar(&o.insecure, "tlsInsecure", false, "disable TLS certificate validation")
	f.StringVar(&o.caFile, "tlsCAFile", "", "TLS CA certificate file")
	f.StringVar(&o.certFile, "tlsCertificateKeyFile", "", "TLS client certificate and key PEM file")
	f.BoolVar(&o.noDB, "nodb", false, "start without connecting to MongoDB")
	f.BoolVarP(&o.quiet, "quiet", "q", false, "suppress startup messages")
	f.BoolVar(&o.shell, "shell", false, "remain interactive after scripts or --eval")
	f.BoolVar(&o.noInit, "norc", false, "skip ~/.mongoshrc.js")
	f.BoolVarP(&o.noInit, "no-init", "X", false, "skip ~/.mongoshrc.js")
	f.BoolVarP(&o.noPassword, "no-password", "w", false, "never prompt for a password")
	f.StringArrayVar(&o.evals, "eval", nil, "evaluate JavaScript and exit (repeatable)")
	f.StringArrayVarP(&o.evals, "command", "c", nil, "evaluate JavaScript and exit (repeatable)")
	f.StringArrayVarP(&o.files, "file", "f", nil, "execute a JavaScript file (repeatable)")
	f.StringVar(&o.jsonMode, "json", "", "output Extended JSON: canonical or relaxed")
	f.Lookup("json").NoOptDefVal = "relaxed"
	return c
}

func (o *cliOptions) connection(positional []string) (string, []string, error) {
	var files []string
	uri := ""
	for _, arg := range positional {
		if strings.HasSuffix(arg, ".js") || strings.HasSuffix(arg, ".mongodb") {
			files = append(files, arg)
			continue
		}
		if uri != "" {
			return "", nil, errors.New("only one MongoDB connection string is allowed")
		}
		uri = arg
	}
	if uri == "" {
		host := o.host
		if host == "" {
			host = "127.0.0.1"
		}
		if o.port != "" {
			host = net.JoinHostPort(strings.Trim(host, "[]"), o.port)
		}
		uri = "mongodb://" + host + "/test"
	} else if !strings.HasPrefix(uri, "mongodb://") && !strings.HasPrefix(uri, "mongodb+srv://") {
		if !strings.ContainsAny(uri, "/:.") {
			uri = "mongodb://127.0.0.1/" + uri
		} else {
			uri = "mongodb://" + uri
		}
	}
	u, err := url.Parse(uri)
	if err != nil {
		return "", nil, errors.New("invalid MongoDB connection string")
	}
	q := u.Query()
	if o.authDB != "" {
		q.Set("authSource", o.authDB)
	}
	if o.tls {
		q.Set("tls", "true")
	}
	if o.insecure {
		q.Set("tlsInsecure", "true")
	}
	if o.caFile != "" {
		q.Set("tlsCAFile", o.caFile)
	}
	if o.certFile != "" {
		q.Set("tlsCertificateKeyFile", o.certFile)
	}
	if q.Has("data-masking-rules") {
		return "", nil, errors.New("data-masking-rules are not supported by Mongo Shell mode")
	}
	u.RawQuery = q.Encode()
	if o.username != "" {
		u.User = url.User(o.username)
	}
	passwordPresent := false
	if u.User != nil {
		_, passwordPresent = u.User.Password()
	}
	needPassword := !o.noDB && (o.password == "\x00prompt" || (u.User != nil && u.User.Username() != "" && !passwordPresent && o.password == "" && !o.noPassword && !strings.EqualFold(q.Get("authMechanism"), "MONGODB-X509")))
	password := o.password
	if needPassword {
		if !term.IsTerminal(int(os.Stdin.Fd())) {
			return "", nil, errors.New("password prompt requires a terminal; supply credentials in the connection string or use --no-password")
		}
		fmt.Fprint(os.Stdout, "Enter password:")
		secret, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stdout)
		if err != nil {
			return "", nil, err
		}
		password = string(secret)
	}
	if (needPassword || o.password != "") && u.User != nil {
		u.User = url.UserPassword(u.User.Username(), password)
	}
	return u.String(), files, nil
}

func (o *cliOptions) run(ctx context.Context, positional []string) error {
	if o.jsonMode != "" && o.jsonMode != "canonical" && o.jsonMode != "relaxed" {
		return errors.New("--json must be canonical or relaxed")
	}
	uri, files, err := o.connection(positional)
	if err != nil {
		return err
	}
	files = append(o.files, files...)
	cs, err := connstring.ParseAndValidate(uri)
	if err != nil {
		return errors.New("invalid MongoDB connection options")
	}
	database := cs.Database
	if database == "" {
		database = "test"
	}
	var client *mongo.Client
	var clientOptions *options.ClientOptions
	if !o.noDB {
		clientOptions = options.Client().ApplyURI(uri).SetMaxPoolSize(4).SetServerSelectionTimeout(10 * time.Second).SetTimeout(0)
		if proxyURL := mongoProxyURL(); proxyURL != "" {
			dialer, err := newProxyDialer(proxyURL)
			if err != nil {
				return err
			}
			clientOptions.SetDialer(dialer)
		}
		client, err = mongo.Connect(clientOptions)
		if err != nil {
			return err
		}
		defer func() {
			closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = client.Disconnect(closeCtx)
		}()
		pingCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		err = client.Ping(pingCtx, nil)
		cancel()
		if err != nil {
			return err
		}
	}
	s, err := newShell(client, database, os.Stdout, clientOptions)
	if err != nil {
		return err
	}
	s.jsonMode = o.jsonMode
	defer s.Close()
	if !o.noInit {
		if home, err := os.UserHomeDir(); err == nil {
			if source, err := readScript(filepath.Join(home, ".mongoshrc.js")); err == nil {
				if err := s.Eval(ctx, source, false); err != nil {
					return err
				}
			} else if !os.IsNotExist(err) {
				return err
			}
		}
	}
	for _, file := range files {
		source, err := readScript(file)
		if err != nil {
			return err
		}
		if err = s.evaluateInterruptible(ctx, source, false); errors.Is(err, errQuit) {
			return nil
		} else if err != nil {
			return err
		}
	}
	for i, source := range o.evals {
		if err = s.evaluateInterruptible(ctx, source, i == len(o.evals)-1); errors.Is(err, errQuit) {
			return nil
		} else if err != nil {
			return err
		}
	}
	if len(files)+len(o.evals) > 0 && !o.shell {
		return nil
	}
	interactive := term.IsTerminal(int(os.Stdin.Fd()))
	if interactive && !o.quiet {
		fmt.Fprintln(os.Stdout, "usql Mongo Shell — type help for commands, exit to quit")
	}
	return s.repl(ctx, interactive)
}

func readScript(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, 1024*1024+1))
	if len(b) > 1024*1024 {
		return "", errors.New("MongoDB script exceeds 1 MiB")
	}
	return string(b), err
}

func (s *Shell) evaluateInterruptible(ctx context.Context, source string, display bool) error {
	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt)
	defer cancel()
	return s.Eval(ctx, source, display)
}

func (s *Shell) repl(ctx context.Context, interactive bool) error {
	var next func(string) (string, error)
	if interactive {
		history := ""
		if cache, err := os.UserCacheDir(); err == nil {
			dir := filepath.Join(cache, "usql")
			if os.MkdirAll(dir, 0700) == nil {
				history = filepath.Join(dir, "mongo_history")
			}
		}
		line, err := readline.NewEx(&readline.Config{Prompt: s.Database() + "> ", HistoryFile: history,
			HistoryLimit: 1000, InterruptPrompt: "^C", EOFPrompt: "exit", AutoComplete: completer{s}})
		if err != nil {
			return err
		}
		defer line.Close()
		next = func(prompt string) (string, error) { line.SetPrompt(prompt); return line.Readline() }
	} else {
		scanner := bufio.NewScanner(os.Stdin)
		scanner.Buffer(make([]byte, 4096), 1024*1024)
		next = func(string) (string, error) {
			if scanner.Scan() {
				return scanner.Text(), nil
			}
			if scanner.Err() != nil {
				return "", scanner.Err()
			}
			return "", io.EOF
		}
	}
	pending := ""
	for {
		prompt := s.Database() + "> "
		if pending != "" {
			prompt = "... "
		}
		line, err := next(prompt)
		if errors.Is(err, readline.ErrInterrupt) {
			pending = ""
			continue
		}
		if errors.Is(err, io.EOF) {
			if pending != "" {
				return s.evaluateInterruptible(ctx, pending, true)
			}
			return nil
		}
		if err != nil {
			return err
		}
		source := pending + line
		if incomplete(source) {
			pending = source + "\n"
			if len(pending) > 1024*1024 {
				return errors.New("MongoDB input exceeds 1 MiB")
			}
			continue
		}
		pending = ""
		err = s.evaluateInterruptible(ctx, source, true)
		if errors.Is(err, errQuit) {
			return nil
		}
		if err != nil {
			if !interactive {
				return err
			}
			fmt.Fprintln(os.Stderr, err)
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
}

func incomplete(source string) bool {
	_, err := goja.Compile("", source, false)
	return err != nil && strings.Contains(err.Error(), "Unexpected end of input")
}

// pflag accepts optional string values with '='; mongosh also accepts '-p value'.
func normalizePasswordArgs(args []string) []string {
	result := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if (arg == "-p" || arg == "--password") && i+1 < len(args) {
			next := args[i+1]
			if !strings.HasPrefix(next, "-") && !strings.HasPrefix(next, "mongodb://") && !strings.HasPrefix(next, "mongodb+srv://") && !strings.HasSuffix(next, ".js") {
				arg += "=" + next
				i++
			}
		}
		result = append(result, arg)
	}
	return result
}

type completer struct{ shell *Shell }

func (c completer) Do(line []rune, pos int) ([][]rune, int) {
	prefix := string(line[:pos])
	var result [][]rune
	for _, match := range c.shell.Complete(prefix) {
		result = append(result, []rune(strings.TrimPrefix(match, prefix)))
	}
	return result, 0
}
