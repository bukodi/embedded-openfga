package fgaclient

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/amikos-tech/embedded-openfga/embeddfga"
	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	parser "github.com/openfga/language/pkg/go/transformer"
	"github.com/openfga/openfga/pkg/server"
	"github.com/openfga/openfga/pkg/tuple"
)

type Conn struct {
	fgaServer            *server.Server
	storeName            string
	storeID              string
	authorizationModelID string
}

func NewEmbeddedSqlite(ctx context.Context, datastoreURI string, modelData []byte, storeName string) (*Conn, error) {
	if datastoreURI == "" {
		return nil, fmt.Errorf("datastoreURI cannot be empty")
	}

	conn := Conn{
		storeName: storeName,
	}

	// Create a new server
	fgaServer, err := embeddfga.NewSqliteServer(datastoreURI)
	if err != nil {
		return nil, err
	}
	defer func() {
		if fgaServer != nil {
			fgaServer.Close()
		}
	}()

	// Create or lookup the store
	stores, err := fgaServer.ListStores(ctx, &openfgav1.ListStoresRequest{Name: conn.storeName})
	if err != nil {
		return nil, fmt.Errorf("failed to list stores: %w", err)
	}
	if len(stores.Stores) == 0 {
		cs, err := fgaServer.CreateStore(ctx, &openfgav1.CreateStoreRequest{
			Name: storeName,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create store: %w", err)
		}
		conn.storeID = cs.GetId()
		slog.Debug("Store created", slog.String("storeName", conn.storeName), slog.String("storeId", conn.storeID))
	} else {
		conn.storeID = stores.Stores[0].GetId()
		slog.Debug("Store found", slog.String("storeName", conn.storeName), slog.String("storeId", conn.storeID))
	}

	model, err := parser.TransformDSLToProto(string(modelData))
	if err != nil {
		return nil, fmt.Errorf("failed to transform DSL to OpenFGA model: %w", err)
	}

	models, err := fgaServer.ReadAuthorizationModels(ctx, &openfgav1.ReadAuthorizationModelsRequest{
		StoreId: conn.storeID,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to read authorization models: %w", err)
	}

	if len(models.GetAuthorizationModels()) == 0 {
		r, err := fgaServer.WriteAuthorizationModel(ctx, &openfgav1.WriteAuthorizationModelRequest{
			StoreId:         conn.storeID,
			SchemaVersion:   model.GetSchemaVersion(),
			TypeDefinitions: model.GetTypeDefinitions(),
			Conditions:      model.GetConditions(), // in this demo we don't use conditions, but you can add them and use them in your model
		})
		if err != nil {
			return nil, fmt.Errorf("failed to write the authorization model: %w", err)
		}
		conn.authorizationModelID = r.GetAuthorizationModelId()
		slog.Debug("Authorization model created", slog.String("authModelId", conn.authorizationModelID))
	} else {
		conn.authorizationModelID = models.GetAuthorizationModels()[0].GetId()
		slog.Debug("Authorization model found", slog.String("authModelId", conn.authorizationModelID))
	}

	conn.fgaServer = fgaServer
	fgaServer = nil
	slog.Info("Connected to OpenFGA server",
		slog.String("authModelId", conn.authorizationModelID),
		slog.String("storeName", conn.storeName), slog.String("storeId", conn.storeID),
	)
	return &conn, nil
}

func (c *Conn) Close() {
	c.fgaServer.Close()
}

func (c *Conn) AddTuples(ctx context.Context, tuples []*tuple.Tuple) error {
	var tupleKeys []*openfgav1.TupleKey
	for _, tpl := range tuples {
		tupleKeys = append(tupleKeys, tuple.NewTupleKey(tpl.Object, tpl.Relation, tpl.User))
	}
	_, err := c.fgaServer.Write(ctx, &openfgav1.WriteRequest{
		StoreId:              c.storeID,
		AuthorizationModelId: c.authorizationModelID,
		Writes: &openfgav1.WriteRequestWrites{
			TupleKeys: tupleKeys,
		},
	})
	if err != nil {
		return fmt.Errorf("failed to write tuple to OpenFGA: %w", err)
	}
	return nil
}

func (c *Conn) Check(ctx context.Context, t *tuple.Tuple) (bool, error) {
	v, err := c.fgaServer.Check(ctx, &openfgav1.CheckRequest{
		StoreId:              c.storeID,
		AuthorizationModelId: c.authorizationModelID,
		TupleKey:             tuple.NewCheckRequestTupleKey(t.Object, t.Relation, t.User),
	})
	if err != nil {
		return false, fmt.Errorf("failed to check tuple in OpenFGA: %w", err)
	}
	return v.GetAllowed(), nil
}
