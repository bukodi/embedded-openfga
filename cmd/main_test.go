package main

import "testing"

func TestCmgLine(t *testing.T) {
	t.Setenv("DATASTORE_URI", "postgres://postgres:pass123@localhost:5432/postgres")
	t.Setenv("INITIAL_TUPLES", `
	[
	{"object": "document:1", "relation": "editor", "user": "user:test@example.com"},
	{"object": "document:2", "relation": "editor", "user": "user:test@example.com"},
	{"object": "document:2", "relation": "viewer", "user": "user:another@example.com"},
	{"object": "app:auth", "relation": "admin", "user": "user:test@example.com"}
	]`)
	t.Setenv("MODEL_FILE", "../model.fga")
	t.Setenv("STORE_NAME", "embedded_fga")
	t.Setenv("AUTHORIZATION_MODEL_NAME", "default")
	t.Setenv("CACHE_TTL", "5m")

	main()
}
