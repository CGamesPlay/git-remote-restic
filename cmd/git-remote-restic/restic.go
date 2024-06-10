package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/restic/restic/lib/backend"
	"github.com/restic/restic/lib/backend/azure"
	"github.com/restic/restic/lib/backend/b2"
	"github.com/restic/restic/lib/backend/gs"
	"github.com/restic/restic/lib/backend/limiter"
	"github.com/restic/restic/lib/backend/local"
	"github.com/restic/restic/lib/backend/location"
	"github.com/restic/restic/lib/backend/logger"
	"github.com/restic/restic/lib/backend/rclone"
	"github.com/restic/restic/lib/backend/rest"
	"github.com/restic/restic/lib/backend/s3"
	"github.com/restic/restic/lib/backend/sema"
	"github.com/restic/restic/lib/backend/sftp"
	"github.com/restic/restic/lib/backend/swift"
	"github.com/restic/restic/lib/debug"
	"github.com/restic/restic/lib/options"
	"github.com/restic/restic/lib/repository"
	"github.com/restic/restic/lib/restic"

	"github.com/restic/restic/lib/errors"
)

// TimeFormat is the format used for all timestamps printed by restic.
const TimeFormat = "2006-01-02 15:04:05"

type backendWrapper func(r restic.Backend) (restic.Backend, error)

// GlobalOptions hold all global options for restic.
type GlobalOptions struct {
	Repo            string
	RepositoryFile  string
	PasswordFile    string
	PasswordCommand string
	KeyHint         string
	Quiet           bool
	Verbose         int
	NoLock          bool
	RetryLock       time.Duration
	JSON            bool
	CacheDir        string
	NoCache         bool
	CleanupCache    bool
	Compression     repository.CompressionMode
	PackSize        uint
	NoExtraVerify   bool

	backend.TransportOptions
	limiter.Limits

	stdout io.Writer
	stderr io.Writer

	backends             *location.Registry
	backendInnerTestHook backendWrapper

	// verbosity is set as follows:
	//  0 means: don't print any messages except errors, this is used when --quiet is specified
	//  1 is the default: print essential messages
	//  2 means: print more messages, report minor things, this is used when --verbose is specified
	//  3 means: print very detailed debug messages, this is used when --verbose=2 is specified
	verbosity uint

	Options []string
}

var globalOptions = GlobalOptions{
	stdout: os.Stdout,
	stderr: os.Stderr,
	Limits: limiter.Limits{UploadKb: 0, DownloadKb: 0},
}

var isReadingPassword bool

func init() {
	backends := location.NewRegistry()
	backends.Register(azure.NewFactory())
	backends.Register(b2.NewFactory())
	backends.Register(gs.NewFactory())
	backends.Register(local.NewFactory())
	backends.Register(rclone.NewFactory())
	backends.Register(rest.NewFactory())
	backends.Register(s3.NewFactory())
	backends.Register(sftp.NewFactory())
	backends.Register(swift.NewFactory())
	globalOptions.backends = backends

	globalOptions.Repo = os.Getenv("RESTIC_REPOSITORY")
	globalOptions.RepositoryFile = os.Getenv("RESTIC_REPOSITORY_FILE")
	globalOptions.PasswordFile = os.Getenv("RESTIC_PASSWORD_FILE")
	globalOptions.KeyHint = os.Getenv("RESTIC_KEY_HINT")
	globalOptions.PasswordCommand = os.Getenv("RESTIC_PASSWORD_COMMAND")
	if os.Getenv("RESTIC_CACERT") != "" {
		globalOptions.RootCertFilenames = strings.Split(os.Getenv("RESTIC_CACERT"), ",")
	}
	globalOptions.TLSClientCertKeyFilename = os.Getenv("RESTIC_TLS_CLIENT_CERT")
	comp := os.Getenv("RESTIC_COMPRESSION")
	if comp != "" {
		// ignore error as there's no good way to handle it
		_ = globalOptions.Compression.Set(comp)
	}
	// parse target pack size from env, on error the default value will be used
	targetPackSize, _ := strconv.ParseUint(os.Getenv("RESTIC_PACK_SIZE"), 10, 32)
	globalOptions.PackSize = uint(targetPackSize)
}

// Printf writes the message to the configured stdout stream.
func Printf(format string, args ...interface{}) {
	_, err := fmt.Fprintf(globalOptions.stdout, format, args...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "unable to write to stdout: %v\n", err)
	}
}

// Print writes the message to the configured stdout stream.
func Print(args ...interface{}) {
	_, err := fmt.Fprint(globalOptions.stdout, args...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "unable to write to stdout: %v\n", err)
	}
}

// Println writes the message to the configured stdout stream.
func Println(args ...interface{}) {
	_, err := fmt.Fprintln(globalOptions.stdout, args...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "unable to write to stdout: %v\n", err)
	}
}

// Verbosef calls Printf to write the message when the verbose flag is set.
func Verbosef(format string, args ...interface{}) {
	if globalOptions.verbosity >= 1 {
		Printf(format, args...)
	}
}

// Verboseff calls Printf to write the message when the verbosity is >= 2
func Verboseff(format string, args ...interface{}) {
	if globalOptions.verbosity >= 2 {
		Printf(format, args...)
	}
}

// Warnf writes the message to the configured stderr stream.
func Warnf(format string, args ...interface{}) {
	_, err := fmt.Fprintf(globalOptions.stderr, format, args...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "unable to write to stderr: %v\n", err)
	}
	debug.Log(format, args...)
}

func parseConfig(loc location.Location, opts options.Options) (interface{}, error) {
	cfg := loc.Config
	if cfg, ok := cfg.(restic.ApplyEnvironmenter); ok {
		cfg.ApplyEnvironment("")
	}

	// only apply options for a particular backend here
	opts = opts.Extract(loc.Scheme)
	if err := opts.Apply(loc.Scheme, cfg); err != nil {
		return nil, err
	}

	debug.Log("opening %v repository at %#v", loc.Scheme, cfg)
	return cfg, nil
}

// Open the backend specified by a location config.
func open(ctx context.Context, s string, opts options.Options) (restic.Backend, error) {
	gopts := globalOptions
	debug.Log("parsing location %v", location.StripPassword(gopts.backends, s))
	loc, err := location.Parse(gopts.backends, s)
	if err != nil {
		return nil, errors.Fatalf("parsing repository location failed: %v", err)
	}

	var be restic.Backend

	cfg, err := parseConfig(loc, opts)
	if err != nil {
		return nil, err
	}

	rt, err := backend.Transport(gopts.TransportOptions)
	if err != nil {
		return nil, errors.Fatal(err.Error())
	}

	// wrap the transport so that the throughput via HTTP is limited
	lim := limiter.NewStaticLimiter(gopts.Limits)
	rt = lim.Transport(rt)

	factory := gopts.backends.Lookup(loc.Scheme)
	if factory == nil {
		return nil, errors.Fatalf("invalid backend: %q", loc.Scheme)
	}

	be, err = factory.Open(ctx, cfg, rt, lim)
	if err != nil {
		return nil, errors.Fatalf("unable to open repository at %v: %v", location.StripPassword(gopts.backends, s), err)
	}

	// wrap with debug logging and connection limiting
	be = logger.New(sema.NewBackend(be))

	// wrap backend if a test specified an inner hook
	if gopts.backendInnerTestHook != nil {
		be, err = gopts.backendInnerTestHook(be)
		if err != nil {
			return nil, err
		}
	}

	// check if config is there
	fi, err := be.Stat(ctx, restic.Handle{Type: restic.ConfigFile})
	if err != nil {
		return nil, errors.Fatalf("unable to open config file: %v\nIs there a repository at the following location?\n%v", err, location.StripPassword(gopts.backends, s))
	}

	if fi.Size == 0 {
		return nil, errors.New("config file has zero size, invalid repository?")
	}

	return be, nil
}
