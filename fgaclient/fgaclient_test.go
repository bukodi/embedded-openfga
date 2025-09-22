package fgaclient

import (
	"os"
	"testing"

	"github.com/openfga/openfga/pkg/tuple"
)

func TestFgaClient(t *testing.T) {
	modelData, err := os.ReadFile("../model.fga")
	if err != nil {
		t.Fatalf("failed to read the model file: %+v", err)
	}
	conn, err := NewEmbeddedSqlite(t.Context(), t.TempDir()+"/openfga.db", modelData, "TEST_STORE")
	if err != nil {
		t.Fatalf("failed to create embedded OpenFGA server: %+v", err)
	}
	defer conn.Close()

	conn.AddTuples(t.Context(), []*tuple.Tuple{
		{Object: "document:1", Relation: "editor", User: "user:test@example.com"},
		{Object: "document:2", Relation: "editor", User: "user:test@example.com"},
		{Object: "document:2", Relation: "viewer", User: "user:another@example.com"},
		{Object: "app:auth", Relation: "admin", User: "user:test@example.com"},
	})

	if v, err := conn.Check(t.Context(),
		&tuple.Tuple{
			Object:   "document:1",
			Relation: "editor",
			User:     "user:test@example.com",
		}); err != nil {
		t.Errorf("failed to check tuple: %+v", err)
	} else {
		t.Log("Allowed:", v)
	}
	if v, err := conn.Check(t.Context(),
		&tuple.Tuple{
			Object:   "document:1",
			Relation: "editor",
			User:     "user:anoter@example.com",
		}); err != nil {
		t.Errorf("failed to check tuple: %+v", err)
	} else {
		t.Log("Allowed:", v)
	}

}
