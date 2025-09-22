package embeddfga

import (
	"testing"
)

func TestNewSqliteServer(t *testing.T) {
	dbFile := t.TempDir() + "/openfga.db"
	fga1, err := NewSqliteServer(dbFile)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Created new OpenFGA server at %s", dbFile)
	fga1.Close()
	t.Logf("OpenFGA server closed")

	fga2, err := NewSqliteServer(dbFile)
	t.Logf("OpenFGA server reopened at %s", dbFile)
	if err != nil {
		t.Fatal(err)
	}
	fga2.Close()
	t.Logf("OpenFGA server closed")

}

func TestNewHttpService(t *testing.T) {
	dbFile := t.TempDir() + "/openfga.db"
	fga1, err := NewSqliteServer(dbFile)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Created new OpenFGA server at %s", dbFile)
	defer fga1.Close()
}
