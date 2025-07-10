package main

import (
	"context"
	"github.com/go-playground/validator/v10"
	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	parser "github.com/openfga/language/pkg/go/transformer"
	"github.com/openfga/openfga/cmd/migrate"
	"github.com/openfga/openfga/pkg/logger"
	"github.com/openfga/openfga/pkg/server"
	"github.com/openfga/openfga/pkg/storage/postgres"
	"github.com/openfga/openfga/pkg/storage/sqlcommon"
	"github.com/openfga/openfga/pkg/tuple"
	"github.com/pkg/errors"
	"github.com/spf13/viper"
	"go.uber.org/zap"
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
	Server                 *server.Server // reference to the OpenFGA server instance
	StoreName              string         `validate:"required"` // Human-readable name of the store, used for identification. OpenFGA works with storeIDs but we use the name to look it up at startupl;
	StoreID                string         // StoreID is the unique identifier for the store in OpenFGA, it is used to reference the store in API calls
	AuthorizationModelID   string         // AuthorizationModelID is the unique identifier for the authorization model in OpenFGA, it is used to reference the model in API calls
	AuthorizationModelName string         `validate:"required"`            // AuthorizationModelName is the human-readable name of the authorization model, used for identification
	Logger                 *zap.Logger    `validate:"required"`            // Logger is the logger instance used for logging in the OpenFGA server, I like zap!
	InitialTuples          []Tuple        `validate:"min=1,dive,required"` // InitialTuples is a list of tuples to be written to OpenFGA at startup, this is used to initialize the store with some data
	ModelFile              string         `validate:"required,file"`       // ModelFile is the path to the OpenFGA model file, it is used to define the authorization model in OpenFGA
	dataStoreURI           string         `validate:"required,url"`        // dataStoreURI is the URI of the PostgreSQL datastore, it is used to connect to the database
	MaxEvaluationCost      int            `validate:"gte=0"`               // This is a global setting, use wisely
	CacheTTL               time.Duration  `validate:"required"`            // CacheTTL is the time-to-live for the cache, used to control how long cached data is valid (default is 10 minutes)
}

type OpenFGAOption func(*OpenFGAServer) error

func WithLogger(logger *zap.Logger) OpenFGAOption {
	return func(fga *OpenFGAServer) error {
		if logger == nil {
			return errors.New("logger cannot be nil")
		}
		fga.Logger = logger
		return nil
	}
}

func WithInitialTuples(tuples []Tuple) OpenFGAOption {
	return func(fga *OpenFGAServer) error {
		if len(tuples) == 0 {
			return errors.New("initial tuples cannot be empty")
		}
		fga.InitialTuples = tuples
		return nil
	}
}

func WithModelFile(modelFile string) OpenFGAOption {
	return func(fga *OpenFGAServer) error {
		if modelFile == "" {
			return errors.New("model file cannot be empty")
		}
		fga.ModelFile = modelFile
		return nil
	}
}

func WithAuthorizationModelName(name string) OpenFGAOption {
	return func(fga *OpenFGAServer) error {
		if name == "" {
			return errors.New("authorization model name cannot be empty")
		}
		fga.AuthorizationModelName = name
		return nil
	}
}

func WithStoreName(name string) OpenFGAOption {
	return func(fga *OpenFGAServer) error {
		if name == "" {
			return errors.New("store name cannot be empty")
		}
		fga.StoreName = name
		return nil
	}
}

func WithMaxEvaluationCost(cost int) OpenFGAOption {
	return func(fga *OpenFGAServer) error {
		if cost < 0 {
			return errors.New("max evaluation cost must be greater than or equal to 0")
		}
		fga.MaxEvaluationCost = cost
		return nil
	}
}

func WithCacheTTL(ttl time.Duration) OpenFGAOption {
	return func(fga *OpenFGAServer) error {
		if ttl < 0 {
			return errors.New("cache TTL must be greater than or equal to 0")
		}
		fga.CacheTTL = ttl
		return nil
	}
}

func WithCacheTTLString(ttl string) OpenFGAOption {
	return func(fga *OpenFGAServer) error {
		if ttl == "" {
			return nil // we ignore empty TTLs, use the default value
		}
		parsedTTL, err := time.ParseDuration(ttl)
		if err != nil {
			return errors.Wrap(err, "failed to parse cache TTL duration")
		}
		if parsedTTL < 0 {
			return errors.New("cache TTL must be greater than or equal to 0")
		}
		fga.CacheTTL = parsedTTL
		return nil
	}
}

func NewOpenFGA(dataStoreURI string, opts ...OpenFGAOption) (*OpenFGAServer, error) {
	fga := &OpenFGAServer{
		dataStoreURI:      dataStoreURI,
		MaxEvaluationCost: 100,              // OpenFGA default max evaluation cost
		CacheTTL:          10 * time.Minute, // Default cache TTL
	}
	for _, opt := range opts {
		if err := opt(fga); err != nil {
			return nil, errors.Wrap(err, "failed to apply OpenFGA option")
		}
	}
	// 1. Validate server options
	v := validator.New()
	err := v.Struct(fga)
	if err != nil {
		return nil, errors.Wrap(err, "OpenFGA server configuration validation failed")
	}

	// 2. Setup PostgreSQL datastore
	confg := sqlcommon.NewConfig()
	pgConfig, err := postgres.New(
		fga.dataStoreURI,
		confg,
	)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create PostgreSQL datastore")
	}

	timeout := time.After(30 * time.Second)
	for {
		r, err := pgConfig.IsReady(context.Background())
		if err != nil {
			return nil, errors.Wrap(err, "error waiting for PostgreSQL datastore to be ready")
		}
		if r.IsReady {
			fga.Logger.Debug("PostgreSQL datastore is ready")
			break
		}
		select {
		case <-time.After(1 * time.Second):
			fga.Logger.Debug("Waiting for PostgreSQL datastore to be ready...", zap.String("message", r.Message))
		case <-timeout:
			return nil, errors.New("timed out waiting for PostgreSQL datastore to be ready")
		}
	}

	// 3. Run migration
	err = Migrate()
	if err != nil {
		fga.Logger.Error("Failed to run migration", zap.Error(err))
		return nil, errors.Wrap(err, "failed to run migration")
	} else {
		fga.Logger.Info("Migration completed")
	}

	viper.Set("maxConditionEvaluationCost", fga.MaxEvaluationCost) // use this wisely, it is a global setting and can have performance implications for slower modelsl
	// 4. Initialize OpenFGA server
	fgaServer, err := server.NewServerWithOpts(
		server.WithDatastore(pgConfig),
		server.WithLogger(&logger.ZapLogger{Logger: fga.Logger.With(zap.String("service", "authz"))}),
		server.WithCacheControllerEnabled(true),
		server.WithCacheControllerTTL(fga.CacheTTL),
		server.WithCheckQueryCacheEnabled(true),
		server.WithCheckQueryCacheTTL(fga.CacheTTL),
		server.WithCheckIteratorCacheEnabled(true),
		server.WithMaxChecksPerBatchCheck(5000),
	)
	if err != nil {
		return nil, errors.Wrap(err, "failed to initialize OpenFGA server")
	}
	timeout = time.After(30 * time.Second)
	for {
		isReady, err := fgaServer.IsReady(context.Background())
		if err != nil {
			return nil, errors.Wrap(err, "error checking OpenFGA server readiness")
		}
		if isReady {
			fga.Logger.Debug("OpenFGA server is ready")
			break
		}
		select {
		case <-time.After(1 * time.Second):
			fga.Logger.Debug("Waiting for OpenFGA server to be ready...")
		case <-timeout:
			return nil, errors.New("timed out waiting for OpenFGA server to be ready")
		}
	}

	fga.Server = fgaServer

	// 5. Create or lookup the store

	stores, err := fga.Server.ListStores(context.Background(), &openfgav1.ListStoresRequest{Name: fga.StoreName})
	if err != nil {
		return nil, errors.Wrap(err, "failed to list stores")
	}
	if len(stores.Stores) == 0 {
		cs, err := fga.Server.CreateStore(context.Background(), &openfgav1.CreateStoreRequest{
			Name: fga.StoreName,
		})
		if err != nil {
			fga.Logger.Error("Failed to create store", zap.Error(err))
			return nil, errors.Wrap(err, "failed to create store")
		}
		fga.StoreID = cs.GetId()
		fga.Logger.Debug("Store created", zap.String("id", fga.StoreID))
	} else {
		fga.StoreID = stores.Stores[0].GetId()
		fga.Logger.Info("Store found", zap.String("id", fga.StoreID))
	}

	// 6. Create or lookup the authorization model
	modelData, err := os.ReadFile(fga.ModelFile)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read model file")
	}

	model, err := parser.TransformDSLToProto(string(modelData))
	if err != nil {
		return nil, errors.Wrap(err, "failed to transform DSL to OpenFGA model")
	}

	models, err := fga.Server.ReadAuthorizationModels(context.Background(), &openfgav1.ReadAuthorizationModelsRequest{
		StoreId: fga.StoreID,
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to read authorization models")
	}

	if len(models.GetAuthorizationModels()) == 0 {
		r, err := fga.Server.WriteAuthorizationModel(context.Background(), &openfgav1.WriteAuthorizationModelRequest{
			StoreId:         fga.StoreID,
			SchemaVersion:   model.GetSchemaVersion(),
			TypeDefinitions: model.GetTypeDefinitions(),
			Conditions:      model.GetConditions(), // in this demo we don't use conditions, but you can add them and use them in your model
		})
		if err != nil {
			fga.Logger.Error("Failed to write authorization model", zap.Error(err))
			return nil, errors.Wrap(err, "failed to write authorization model")
		}
		fga.AuthorizationModelID = r.GetAuthorizationModelId()
		fga.Logger.Debug("Authorization model created", zap.String("model_id", fga.AuthorizationModelID))
	} else {
		fga.AuthorizationModelID = models.GetAuthorizationModels()[0].GetId()
		fga.Logger.Debug("Authorization model found", zap.String("model_id", fga.AuthorizationModelID))
	}

	// 7. Import initial tuples to OpenFGA
	err = fga.Write(context.Background(), fga.InitialTuples, true) // we ignore existing tuples
	if err != nil {
		return nil, errors.Wrap(err, "failed to write tuples to OpenFGA")
	}

	return fga, nil

}

func (fga *OpenFGAServer) Check(ctx context.Context, t Tuple) (bool, error) {
	v, err1 := fga.Server.Check(ctx, &openfgav1.CheckRequest{
		StoreId:              fga.StoreID,
		AuthorizationModelId: fga.AuthorizationModelID,
		TupleKey:             tuple.NewCheckRequestTupleKey(t.Object, t.Relation, t.User),
	})
	if err1 != nil {
		return false, errors.Wrap(err1, "failed to check tuple in OpenFGA")
	}
	return v.GetAllowed(), nil
}

func (fga *OpenFGAServer) Write(ctx context.Context, t []Tuple, ignoreExisting bool) error {
	if len(t) == 0 {
		return errors.New("no tuples provided to write")
	}
	var tupleKeys []*openfgav1.TupleKey
	for _, tpl := range t {
		tupleKeys = append(tupleKeys, tuple.NewTupleKey(tpl.Object, tpl.Relation, tpl.User))
	}
	_, err := fga.Server.Write(ctx, &openfgav1.WriteRequest{
		StoreId:              fga.StoreID,
		AuthorizationModelId: fga.AuthorizationModelID,
		Writes: &openfgav1.WriteRequestWrites{
			TupleKeys: tupleKeys,
		},
	})
	if err != nil {
		if strings.Contains(err.Error(), "already exists") && ignoreExisting { // if a batch write fails due to one exising pair the others won't be written, use this carefully
			fga.Logger.Info("Tuple already exists, ignoring", zap.Error(err))
			return nil
		}
		return errors.Wrap(err, "failed to write tuple to OpenFGA")
	}
	return nil
}

func (fga *OpenFGAServer) Close() error {
	if fga.Server != nil {
		fga.Server.Close()
	}
	return nil
}
