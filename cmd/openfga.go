package main

import (
	"context"
	"encoding/json"
	"fmt"
	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	parser "github.com/openfga/language/pkg/go/transformer"
	"github.com/openfga/openfga/cmd/migrate"
	"github.com/openfga/openfga/pkg/logger"
	"github.com/openfga/openfga/pkg/server"
	"github.com/openfga/openfga/pkg/storage/postgres"
	"github.com/openfga/openfga/pkg/storage/sqlcommon"
	"github.com/openfga/openfga/pkg/tuple"
	"github.com/pkg/errors"
	"os"
	"strings"
	"time"
)

func Migrate() error {
	migrateCommand := migrate.NewMigrateCommand()
	migrateCommand.SetArgs([]string{"--datastore-engine", "postgres", "--datastore-uri", os.Getenv("DATASTORE_URI")})
	err := migrateCommand.Execute()
	return err
}

type Tuple struct {
	Object   string `json:"object"`
	Relation string `json:"relation"`
	User     string `json:"user"`
}

type OpenFGAServer struct {
	Server               *server.Server
	StoreID              string
	AuthorizationModelId string
}

func InitOpenFGA() (*OpenFGAServer, error) {
	if os.Getenv("INITIAL_TUPLES") == "" {
		return nil, errors.New("INITIAL_TUPLES environment variable is not set")
	}
	var tuples []Tuple
	if err := json.Unmarshal([]byte(os.Getenv("INITIAL_TUPLES")), &tuples); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal INITIAL_TUPLES environment variable")
	}
	// Configure PostgreSQL datastore
	confg := sqlcommon.NewConfig()
	pgConfig, err := postgres.New(
		os.Getenv("DATASTORE_URI"),
		confg,
	)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create PostgreSQL datastore")
	}

	maxWait := 5
	waits := 0
	for {
		r, e := pgConfig.IsReady(context.Background())
		if e != nil {
			return nil, errors.Wrap(e, "failed to check if PostgreSQL datastore is ready")
		}
		if r.IsReady {
			break
		} else {
			fmt.Println("Waiting for PostgreSQL to be ready...", r.Message)
			if strings.Contains(r.Message, "migrate") {
				fmt.Println("Running migration...")
				err := Migrate()
				if err != nil {
					return nil, errors.Wrap(err, "failed to run migration")
				} else {
					fmt.Println("Migration completed successfully")
				}
			}
			time.Sleep(1 * time.Second)
			waits++
			if waits > maxWait {
				return nil, errors.New("PostgreSQL is not ready after 5 seconds")
			}
		}
	}
	l, err := logger.NewLogger(logger.WithLevel("debug"))
	if err != nil {
		return nil, errors.Wrap(err, "failed to initialize logger")
	}
	// Initialize embedded OpenFGA
	fgaServer, err := server.NewServerWithOpts(
		server.WithDatastore(pgConfig),
		server.WithLogger(l),
		server.WithCacheControllerEnabled(true),
		server.WithCacheControllerTTL(time.Minute*5),
		server.WithCheckQueryCacheEnabled(true),
		server.WithCheckQueryCacheTTL(time.Minute*5),
	)
	if err != nil {
		return nil, errors.Wrap(err, "failed to initialize OpenFGA server")
	}
	timeout := time.After(30 * time.Second)
	for {
		isReady, err := fgaServer.IsReady(context.Background())
		if err != nil {
			return nil, errors.Wrap(err, "error checking OpenFGA server readiness")
		}
		if isReady {
			fmt.Println("OpenFGA server is ready")
			break
		}
		select {
		case <-time.After(1 * time.Second):
			fmt.Println("OpenFGA server is not ready yet, retrying...")
		case <-timeout:
			return nil, errors.New("timed out waiting for OpenFGA server to be ready")
		}
	}

	cs, err := fgaServer.CreateStore(context.Background(), &openfgav1.CreateStoreRequest{
		Name: "openfga-tes1t",
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to create OpenFGA store")
	}
	m := `
model
  schema 1.1

type user
type document
   relations
		define viewer: [user] or editor
		define editor: [user]
type app
   relations
		define admin: [user]
`
	model := parser.MustTransformDSLToProto(m)
	r, err := fgaServer.WriteAuthorizationModel(context.Background(), &openfgav1.WriteAuthorizationModelRequest{
		StoreId:         cs.GetId(),
		SchemaVersion:   model.GetSchemaVersion(),
		TypeDefinitions: model.GetTypeDefinitions(),
		Conditions:      model.GetConditions(),
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to write authorization model to OpenFGA")
	}
	var tupleKeys []*openfgav1.TupleKey
	for _, t := range tuples {
		tupleKeys = append(tupleKeys, tuple.NewTupleKey(t.Object, t.Relation, t.User))
	}

	_, err = fgaServer.Write(context.Background(), &openfgav1.WriteRequest{
		StoreId:              cs.GetId(),
		AuthorizationModelId: r.GetAuthorizationModelId(),
		Writes: &openfgav1.WriteRequestWrites{
			TupleKeys: tupleKeys,
		},
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to write tuples to OpenFGA")
	}

	return &OpenFGAServer{
		Server:               fgaServer,
		StoreID:              cs.GetId(),
		AuthorizationModelId: r.GetAuthorizationModelId(),
	}, nil

}
