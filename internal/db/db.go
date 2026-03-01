package db

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

func Open(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path+"?_journal_mode=WAL&_foreign_keys=on")
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	db.SetMaxOpenConns(1)
	if err := migrate(db); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return db, nil
}

func migrate(db *sql.DB) error {
	_, err := db.Exec(schema)
	return err
}

const schema = `
CREATE TABLE IF NOT EXISTS screens (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    name            TEXT    NOT NULL,
    display_name    TEXT    NOT NULL,
    x               INTEGER NOT NULL DEFAULT 0,
    y               INTEGER NOT NULL DEFAULT 0,
    width           INTEGER NOT NULL DEFAULT 1920,
    height          INTEGER NOT NULL DEFAULT 1080,
    enabled         INTEGER NOT NULL DEFAULT 1,
    off_hours_mode  TEXT    NOT NULL DEFAULT 'black',
    off_hours_image_path TEXT,
    operating_hours TEXT    NOT NULL DEFAULT '{}',
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS screen_state (
    screen_id  INTEGER PRIMARY KEY REFERENCES screens(id) ON DELETE CASCADE,
    media_id   INTEGER REFERENCES media(id),
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS media (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    screen_id     INTEGER  NOT NULL REFERENCES screens(id) ON DELETE CASCADE,
    filename      TEXT     NOT NULL,
    original_name TEXT     NOT NULL,
    media_type    TEXT     NOT NULL,
    file_size     INTEGER  NOT NULL,
    uploaded_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    scrubbed_at   DATETIME
);

CREATE TABLE IF NOT EXISTS audit_log (
    id                 INTEGER PRIMARY KEY AUTOINCREMENT,
    screen_id          INTEGER NOT NULL REFERENCES screens(id) ON DELETE CASCADE,
    event_type         TEXT    NOT NULL,
    media_id           INTEGER REFERENCES media(id),
    previous_media_id  INTEGER REFERENCES media(id),
    note               TEXT,
    created_at         DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
`
