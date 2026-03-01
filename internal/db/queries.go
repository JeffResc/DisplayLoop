package db

import (
	"database/sql"
	"time"
)

// Screen represents a configured display output.
type Screen struct {
	ID                int
	Name              string
	DisplayName       string
	X, Y              int
	Width, Height     int
	Enabled           bool
	OffHoursMode      string // "black" | "image"
	OffHoursImagePath string
	OperatingHours    string // JSON
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// Media represents an uploaded file.
type Media struct {
	ID           int
	ScreenID     int
	Filename     string
	OriginalName string
	MediaType    string // "image" | "video"
	FileSize     int64
	UploadedAt   time.Time
	Scrubbed     bool
	ScrubbedAt   *time.Time
}

// AuditEntry is a row from audit_log with joined media info.
type AuditEntry struct {
	ID               int
	ScreenID         int
	ScreenName       string
	EventType        string
	MediaID          *int
	MediaOrigName    string
	MediaScrubbed    bool
	PrevMediaID      *int
	PrevMediaOrigName string
	PrevMediaScrubbed bool
	Note             string
	CreatedAt        time.Time
}

// ScreenState holds the current active media for a screen.
type ScreenState struct {
	ScreenID  int
	MediaID   *int
	UpdatedAt time.Time
}

// --- Screens ---

func InsertScreen(db *sql.DB, s Screen) (int64, error) {
	res, err := db.Exec(`
		INSERT INTO screens (name, display_name, x, y, width, height, enabled, off_hours_mode, operating_hours)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		s.Name, s.DisplayName, s.X, s.Y, s.Width, s.Height, boolInt(s.Enabled), s.OffHoursMode, s.OperatingHours,
	)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	// create initial screen_state row
	_, err = db.Exec(`INSERT INTO screen_state (screen_id) VALUES (?)`, id)
	return id, err
}

func GetScreen(db *sql.DB, id int) (*Screen, error) {
	row := db.QueryRow(`SELECT id, name, display_name, x, y, width, height, enabled,
		off_hours_mode, COALESCE(off_hours_image_path,''), operating_hours, created_at, updated_at
		FROM screens WHERE id = ?`, id)
	return scanScreen(row)
}

func ListScreens(db *sql.DB) ([]Screen, error) {
	rows, err := db.Query(`SELECT id, name, display_name, x, y, width, height, enabled,
		off_hours_mode, COALESCE(off_hours_image_path,''), operating_hours, created_at, updated_at
		FROM screens ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var screens []Screen
	for rows.Next() {
		s, err := scanScreen(rows)
		if err != nil {
			return nil, err
		}
		screens = append(screens, *s)
	}
	return screens, rows.Err()
}

func UpdateScreenName(db *sql.DB, id int, name string) error {
	_, err := db.Exec(`UPDATE screens SET name=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`, name, id)
	return err
}

func UpdateScreenEnabled(db *sql.DB, id int, enabled bool) error {
	_, err := db.Exec(`UPDATE screens SET enabled=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`, boolInt(enabled), id)
	return err
}

func UpdateScreenHours(db *sql.DB, id int, hours string) error {
	_, err := db.Exec(`UPDATE screens SET operating_hours=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`, hours, id)
	return err
}

func UpdateScreenOffHours(db *sql.DB, id int, mode, imagePath string) error {
	_, err := db.Exec(`UPDATE screens SET off_hours_mode=?, off_hours_image_path=NULLIF(?,?), updated_at=CURRENT_TIMESTAMP WHERE id=?`,
		mode, imagePath, "", id)
	return err
}

func DeleteScreen(db *sql.DB, id int) error {
	_, err := db.Exec(`DELETE FROM screens WHERE id=?`, id)
	return err
}

// --- Screen State ---

func GetScreenState(db *sql.DB, screenID int) (*ScreenState, error) {
	row := db.QueryRow(`SELECT screen_id, media_id, updated_at FROM screen_state WHERE screen_id=?`, screenID)
	var st ScreenState
	var mid sql.NullInt64
	if err := row.Scan(&st.ScreenID, &mid, &st.UpdatedAt); err != nil {
		return nil, err
	}
	if mid.Valid {
		v := int(mid.Int64)
		st.MediaID = &v
	}
	return &st, nil
}

func SetScreenMedia(db *sql.DB, screenID int, mediaID *int) error {
	if mediaID == nil {
		_, err := db.Exec(`UPDATE screen_state SET media_id=NULL, updated_at=CURRENT_TIMESTAMP WHERE screen_id=?`, screenID)
		return err
	}
	_, err := db.Exec(`UPDATE screen_state SET media_id=?, updated_at=CURRENT_TIMESTAMP WHERE screen_id=?`, *mediaID, screenID)
	return err
}

// --- Media ---

func InsertMedia(db *sql.DB, m Media) (int64, error) {
	res, err := db.Exec(`
		INSERT INTO media (screen_id, filename, original_name, media_type, file_size)
		VALUES (?, ?, ?, ?, ?)`,
		m.ScreenID, m.Filename, m.OriginalName, m.MediaType, m.FileSize,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func GetMedia(db *sql.DB, id int) (*Media, error) {
	row := db.QueryRow(`SELECT id, screen_id, filename, original_name, media_type, file_size, uploaded_at, scrubbed_at
		FROM media WHERE id=?`, id)
	return scanMedia(row)
}

func GetCurrentMedia(db *sql.DB, screenID int) (*Media, error) {
	row := db.QueryRow(`
		SELECT m.id, m.screen_id, m.filename, m.original_name, m.media_type, m.file_size, m.uploaded_at, m.scrubbed_at
		FROM media m
		JOIN screen_state ss ON ss.media_id = m.id
		WHERE ss.screen_id = ?`, screenID)
	m, err := scanMedia(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return m, err
}

// ScrubableMedia returns media that can be deleted: not current, older than cutoff, not yet scrubbed.
func ScrubableMedia(db *sql.DB, cutoff time.Time) ([]Media, error) {
	rows, err := db.Query(`
		SELECT m.id, m.screen_id, m.filename, m.original_name, m.media_type, m.file_size, m.uploaded_at, m.scrubbed_at
		FROM media m
		WHERE m.scrubbed_at IS NULL
		  AND m.uploaded_at < ?
		  AND m.id NOT IN (SELECT media_id FROM screen_state WHERE media_id IS NOT NULL)`,
		cutoff,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []Media
	for rows.Next() {
		m, err := scanMedia(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, *m)
	}
	return result, rows.Err()
}

func MarkMediaScrubbed(db *sql.DB, id int) error {
	_, err := db.Exec(`UPDATE media SET scrubbed_at=CURRENT_TIMESTAMP WHERE id=?`, id)
	return err
}

// --- Audit Log ---

func InsertAuditLog(db *sql.DB, screenID int, eventType string, mediaID, prevMediaID *int, note string) error {
	_, err := db.Exec(`
		INSERT INTO audit_log (screen_id, event_type, media_id, previous_media_id, note)
		VALUES (?, ?, ?, ?, ?)`,
		screenID, eventType, intPtrToNull(mediaID), intPtrToNull(prevMediaID), nullableString(note),
	)
	return err
}

func ListAuditLog(db *sql.DB, screenID int, limit int) ([]AuditEntry, error) {
	query := `
		SELECT a.id, a.screen_id, s.name,
		       a.event_type,
		       a.media_id, COALESCE(m.original_name,''), COALESCE(m.scrubbed_at IS NOT NULL, 0),
		       a.previous_media_id, COALESCE(pm.original_name,''), COALESCE(pm.scrubbed_at IS NOT NULL, 0),
		       COALESCE(a.note,''), a.created_at
		FROM audit_log a
		JOIN screens s ON s.id = a.screen_id
		LEFT JOIN media m  ON m.id  = a.media_id
		LEFT JOIN media pm ON pm.id = a.previous_media_id`
	var args []any
	if screenID > 0 {
		query += " WHERE a.screen_id = ?"
		args = append(args, screenID)
	}
	query += " ORDER BY a.created_at DESC"
	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []AuditEntry
	for rows.Next() {
		var e AuditEntry
		var mid, pmid sql.NullInt64
		var mScrubbed, pmScrubbed sql.NullBool
		if err := rows.Scan(
			&e.ID, &e.ScreenID, &e.ScreenName,
			&e.EventType,
			&mid, &e.MediaOrigName, &mScrubbed,
			&pmid, &e.PrevMediaOrigName, &pmScrubbed,
			&e.Note, &e.CreatedAt,
		); err != nil {
			return nil, err
		}
		if mid.Valid {
			v := int(mid.Int64)
			e.MediaID = &v
		}
		if pmid.Valid {
			v := int(pmid.Int64)
			e.PrevMediaID = &v
		}
		e.MediaScrubbed = mScrubbed.Bool
		e.PrevMediaScrubbed = pmScrubbed.Bool
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

func DeleteOldAuditLog(db *sql.DB, cutoff time.Time) (int64, error) {
	res, err := db.Exec(`DELETE FROM audit_log WHERE created_at < ?`, cutoff)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// --- helpers ---

type scanner interface {
	Scan(dest ...any) error
}

func scanScreen(row scanner) (*Screen, error) {
	var s Screen
	var enabled int
	if err := row.Scan(&s.ID, &s.Name, &s.DisplayName, &s.X, &s.Y, &s.Width, &s.Height,
		&enabled, &s.OffHoursMode, &s.OffHoursImagePath, &s.OperatingHours,
		&s.CreatedAt, &s.UpdatedAt); err != nil {
		return nil, err
	}
	s.Enabled = enabled != 0
	return &s, nil
}

func scanMedia(row scanner) (*Media, error) {
	var m Media
	var scrubbedAt sql.NullTime
	if err := row.Scan(&m.ID, &m.ScreenID, &m.Filename, &m.OriginalName,
		&m.MediaType, &m.FileSize, &m.UploadedAt, &scrubbedAt); err != nil {
		return nil, err
	}
	if scrubbedAt.Valid {
		m.Scrubbed = true
		m.ScrubbedAt = &scrubbedAt.Time
	}
	return &m, nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func intPtrToNull(v *int) any {
	if v == nil {
		return nil
	}
	return *v
}

func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
