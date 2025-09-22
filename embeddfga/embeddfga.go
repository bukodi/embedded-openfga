package embeddfga

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/openfga/openfga/pkg/server"
	"github.com/openfga/openfga/pkg/storage"
	"github.com/openfga/openfga/pkg/storage/migrate"
	"github.com/openfga/openfga/pkg/storage/sqlcommon"
	"github.com/openfga/openfga/pkg/storage/sqlite"
)

func NewSqliteServer(
	datastoreURI string,
) (*server.Server, error) {
	ds, err := newSqliteStore(context.Background(), datastoreURI, 6)
	if err != nil {
		return nil, fmt.Errorf("failed to create datastore: %w", err)
	}
	l := zap2Slog{
		slog: slog.Default().Handler().WithAttrs([]slog.Attr{slog.String("component", "embeddedfga")})}
	cacheTTL := time.Minute * 5
	fgaServer, err := server.NewServerWithOpts(
		server.WithDatastore(ds),
		server.WithLogger(l),
		server.WithCacheControllerEnabled(true),
		server.WithCacheControllerTTL(cacheTTL),
		server.WithCheckQueryCacheEnabled(true),
		server.WithCheckQueryCacheTTL(cacheTTL),
		server.WithCheckIteratorCacheEnabled(true),
		server.WithMaxChecksPerBatchCheck(5000),
		server.WithContextPropagationToDatastore(true),
		server.WithMaxChecksPerBatchCheck(5000),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize OpenFGA server: %w", err)
	}
	return fgaServer, nil
}

func newSqliteStore(
	ctx context.Context,
	datastoreURI string,
	schemaVersion uint,
) (storage.OpenFGADatastore, error) {
	l := zap2Slog{
		slog: slog.Default().Handler().WithAttrs([]slog.Attr{slog.String("component", "datastore")}),
	}

	confg := sqlcommon.NewConfig()
	confg.MaxOpenConns = 10
	confg.Logger = l
	ds, err := sqlite.New(
		datastoreURI,
		confg,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create datastore: %w", err)
	}
	r, err := ds.IsReady(ctx)
	if err == nil && r.IsReady {
		slog.Info("datastore ready")
		return ds, nil
	} else if err != nil {
		return nil, fmt.Errorf("error waiting for datastore to be ready: %w", err)
	} else if !r.IsReady && strings.Contains(r.Message, "datastore requires migrations") {
		// 3. Run migration
		// 3. Run migration
		slog.Warn("datastore requires migrations, running them now...")
		err := migrate.RunMigrations(migrate.MigrationConfig{
			Engine:        "sqlite",
			URI:           datastoreURI,
			Verbose:       true,
			TargetVersion: schemaVersion,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to run migrations: %w", err)
		}
		slog.Info("datastore migrations completed")
		r, err = ds.IsReady(ctx)
		if err != nil {
			return nil, fmt.Errorf("error waiting for datastore to be ready: %w", err)
		}
		if !r.IsReady {
			return nil, fmt.Errorf("datastore is not ready: %+v", r)
		}
		slog.Info("datastore ready")
		return ds, nil
	} else {
		return nil, fmt.Errorf("datastore is not ready: %+v", r)
	}
}
