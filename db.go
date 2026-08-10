package main

import (
	"database/sql"
	_ "embed"
	"fmt"
	"log"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaSQL string

func initDB(dbPath string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Enable WAL mode and foreign keys for high performance & integrity
	pragmas := []string{
		"PRAGMA journal_mode=WAL;",
		"PRAGMA foreign_keys=ON;",
		"PRAGMA busy_timeout=5000;",
	}
	for _, pragma := range pragmas {
		if _, err := db.Exec(pragma); err != nil {
			return nil, fmt.Errorf("failed to execute pragma (%s): %w", pragma, err)
		}
	}

	// Execute schema migrations
	if _, err := db.Exec(schemaSQL); err != nil {
		return nil, fmt.Errorf("failed to apply schema: %w", err)
	}

	if err := ensureColumn(db, "playlists", "spotify_id", "TEXT"); err != nil {
		return nil, fmt.Errorf("failed to migrate playlists: %w", err)
	}
	if err := ensureColumn(db, "albums", "in_collection", "INTEGER DEFAULT 0"); err != nil {
		return nil, fmt.Errorf("failed to migrate albums in_collection: %w", err)
	}
	if err := ensureColumn(db, "albums", "collection_added_at", "DATETIME"); err != nil {
		return nil, fmt.Errorf("failed to migrate albums collection_added_at: %w", err)
	}
	if _, err := db.Exec("CREATE UNIQUE INDEX IF NOT EXISTS idx_playlists_spotify_id ON playlists(spotify_id) WHERE spotify_id IS NOT NULL"); err != nil {
		return nil, fmt.Errorf("failed to create Spotify playlist index: %w", err)
	}

	// has_vinyl means "owned as physical vinyl", but earlier syncs also set it for
	// vinyl pressings that only existed on the wantlist. Recompute strictly from
	// collection-sourced release_versions so wantlist-only albums show correctly.
	if _, err := db.Exec(`
		UPDATE albums SET has_vinyl = (
			SELECT COUNT(*) FROM release_versions rv
			WHERE rv.album_id = albums.id AND rv.source = 'collection' AND rv.has_vinyl = 1
		) > 0`); err != nil {
		return nil, fmt.Errorf("failed to recompute albums has_vinyl: %w", err)
	}

	log.Printf("Database initialized at %s (WAL mode enabled)", dbPath)
	return db, nil
}

func ensureColumn(db *sql.DB, table, column, definition string) error {
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name, columnType string
		var notNull, primaryKey int
		var defaultValue interface{}
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return err
		}
		if name == column {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	_, err = db.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, definition))
	return err
}
