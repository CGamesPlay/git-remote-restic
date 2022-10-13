package main

import (
	"context"
	"os"
	"time"

	"github.com/restic/restic/lib/backend"
	"github.com/restic/restic/lib/backend/azure"
	"github.com/restic/restic/lib/backend/b2"
	"github.com/restic/restic/lib/backend/gs"
	"github.com/restic/restic/lib/backend/limiter"
	"github.com/restic/restic/lib/backend/local"
	"github.com/restic/restic/lib/backend/location"
	"github.com/restic/restic/lib/backend/rclone"
	"github.com/restic/restic/lib/backend/rest"
	"github.com/restic/restic/lib/backend/s3"
	"github.com/restic/restic/lib/backend/sftp"
	"github.com/restic/restic/lib/backend/swift"
	"github.com/restic/restic/lib/debug"
	"github.com/restic/restic/lib/errors"
	"github.com/restic/restic/lib/options"
	"github.com/restic/restic/lib/restic"
)

func parseConfig(loc location.Location, opts options.Options) (interface{}, error) {
	// only apply options for a particular backend here
	opts = opts.Extract(loc.Scheme)

	switch loc.Scheme {
	case "local":
		cfg := loc.Config.(local.Config)
		if err := opts.Apply(loc.Scheme, &cfg); err != nil {
			return nil, err
		}

		debug.Log("opening local repository at %#v", cfg)
		return cfg, nil

	case "sftp":
		cfg := loc.Config.(sftp.Config)
		if err := opts.Apply(loc.Scheme, &cfg); err != nil {
			return nil, err
		}

		debug.Log("opening sftp repository at %#v", cfg)
		return cfg, nil

	case "s3":
		cfg := loc.Config.(s3.Config)
		if cfg.KeyID == "" {
			cfg.KeyID = os.Getenv("AWS_ACCESS_KEY_ID")
		}

		if cfg.Secret.String() == "" {
			cfg.Secret = options.NewSecretString(os.Getenv("AWS_SECRET_ACCESS_KEY"))
		}

		if cfg.KeyID == "" && cfg.Secret.String() != "" {
			return nil, errors.Fatalf("unable to open S3 backend: Key ID ($AWS_ACCESS_KEY_ID) is empty")
		} else if cfg.KeyID != "" && cfg.Secret.String() == "" {
			return nil, errors.Fatalf("unable to open S3 backend: Secret ($AWS_SECRET_ACCESS_KEY) is empty")
		}

		if cfg.Region == "" {
			cfg.Region = os.Getenv("AWS_DEFAULT_REGION")
		}

		if err := opts.Apply(loc.Scheme, &cfg); err != nil {
			return nil, err
		}

		debug.Log("opening s3 repository at %#v", cfg)
		return cfg, nil

	case "gs":
		cfg := loc.Config.(gs.Config)
		if cfg.ProjectID == "" {
			cfg.ProjectID = os.Getenv("GOOGLE_PROJECT_ID")
		}

		if err := opts.Apply(loc.Scheme, &cfg); err != nil {
			return nil, err
		}

		debug.Log("opening gs repository at %#v", cfg)
		return cfg, nil

	case "azure":
		cfg := loc.Config.(azure.Config)
		if cfg.AccountName == "" {
			cfg.AccountName = os.Getenv("AZURE_ACCOUNT_NAME")
		}

		if cfg.AccountKey.String() == "" {
			cfg.AccountKey = options.NewSecretString(os.Getenv("AZURE_ACCOUNT_KEY"))
		}

		if cfg.AccountSAS.String() == "" {
			cfg.AccountSAS = options.NewSecretString(os.Getenv("AZURE_ACCOUNT_SAS"))
		}

		if err := opts.Apply(loc.Scheme, &cfg); err != nil {
			return nil, err
		}

		debug.Log("opening gs repository at %#v", cfg)
		return cfg, nil

	case "swift":
		cfg := loc.Config.(swift.Config)

		if err := swift.ApplyEnvironment("", &cfg); err != nil {
			return nil, err
		}

		if err := opts.Apply(loc.Scheme, &cfg); err != nil {
			return nil, err
		}

		debug.Log("opening swift repository at %#v", cfg)
		return cfg, nil

	case "b2":
		cfg := loc.Config.(b2.Config)

		if cfg.AccountID == "" {
			cfg.AccountID = os.Getenv("B2_ACCOUNT_ID")
		}

		if cfg.AccountID == "" {
			return nil, errors.Fatalf("unable to open B2 backend: Account ID ($B2_ACCOUNT_ID) is empty")
		}

		if cfg.Key.String() == "" {
			cfg.Key = options.NewSecretString(os.Getenv("B2_ACCOUNT_KEY"))
		}

		if cfg.Key.String() == "" {
			return nil, errors.Fatalf("unable to open B2 backend: Key ($B2_ACCOUNT_KEY) is empty")
		}

		if err := opts.Apply(loc.Scheme, &cfg); err != nil {
			return nil, err
		}

		debug.Log("opening b2 repository at %#v", cfg)
		return cfg, nil
	case "rest":
		cfg := loc.Config.(rest.Config)
		if err := opts.Apply(loc.Scheme, &cfg); err != nil {
			return nil, err
		}

		debug.Log("opening rest repository at %#v", cfg)
		return cfg, nil
	case "rclone":
		cfg := loc.Config.(rclone.Config)
		if err := opts.Apply(loc.Scheme, &cfg); err != nil {
			return nil, err
		}

		debug.Log("opening rest repository at %#v", cfg)
		return cfg, nil
	}

	return nil, errors.Fatalf("invalid backend: %q", loc.Scheme)
}

// Open the backend specified by a location config.
func openResticBackend(ctx context.Context, s string, opts options.Options) (restic.Backend, error) {
	debug.Log("parsing location %v", location.StripPassword(s))
	loc, err := location.Parse(s)
	if err != nil {
		return nil, errors.Fatalf("parsing repository location failed: %v", err)
	}

	var be restic.Backend

	cfg, err := parseConfig(loc, opts)
	if err != nil {
		return nil, err
	}

	tropts := backend.TransportOptions{}
	rt, err := backend.Transport(tropts)
	if err != nil {
		return nil, err
	}

	// wrap the transport so that the throughput via HTTP is limited
	lim := limiter.NewStaticLimiter(limiter.Limits{0, 0})
	rt = lim.Transport(rt)

	switch loc.Scheme {
	case "local":
		be, err = local.Open(ctx, cfg.(local.Config))
	case "sftp":
		be, err = sftp.Open(ctx, cfg.(sftp.Config))
	case "s3":
		be, err = s3.Open(ctx, cfg.(s3.Config), rt)
	case "gs":
		be, err = gs.Open(cfg.(gs.Config), rt)
	case "azure":
		be, err = azure.Open(cfg.(azure.Config), rt)
	case "swift":
		be, err = swift.Open(ctx, cfg.(swift.Config), rt)
	case "b2":
		be, err = b2.Open(ctx, cfg.(b2.Config), rt)
	case "rest":
		be, err = rest.Open(cfg.(rest.Config), rt)
	case "rclone":
		be, err = rclone.Open(cfg.(rclone.Config), lim)

	default:
		return nil, errors.Fatalf("invalid backend: %q", loc.Scheme)
	}

	if err != nil {
		return nil, errors.Fatalf("unable to open repo at %v: %v", location.StripPassword(s), err)
	}

	// check if config is there
	fi, err := be.Stat(ctx, restic.Handle{Type: restic.ConfigFile})
	if err != nil {
		return nil, errors.Fatalf("unable to open config file: %v\nIs there a repository at the following location?\n%v", err, location.StripPassword(s))
	}

	if fi.Size == 0 {
		return nil, errors.New("config file has zero size, invalid repository?")
	}

	be = backend.NewRetryBackend(be, 10, func(msg string, err error, d time.Duration) {
		Warnf("%v returned error, retrying after %v: %v\n", msg, d, err)
	})

	return be, nil
}
