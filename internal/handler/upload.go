package handler

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/JeffResc/DisplayLoop/internal/db"
)

// HandleUpload processes a new media file upload for a screen.
func (a *App) HandleUpload(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := screenIDFromURL(r)

	screen, err := db.GetScreen(ctx, a.DB, id)
	if err != nil {
		a.respondError(w, http.StatusNotFound, "screen not found")
		return
	}

	if err := r.ParseMultipartForm(500 << 20); err != nil { // 500 MB max
		a.respondError(w, http.StatusBadRequest, "failed to parse upload")
		return
	}

	file, header, err := r.FormFile("media")
	if err != nil {
		a.respondError(w, http.StatusBadRequest, "no file provided")
		return
	}
	defer file.Close()

	mediaType, err := detectMediaType(header.Filename)
	if err != nil {
		a.respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Build destination path.
	ext := strings.ToLower(filepath.Ext(header.Filename))
	storedFilename := uuid.New().String() + ext
	destDir := filepath.Join(a.UploadsDir, itoa(id), "content")
	if err := os.MkdirAll(destDir, 0755); err != nil {
		a.respondError(w, http.StatusInternalServerError, "failed to create upload directory")
		return
	}
	destPath := filepath.Join(destDir, storedFilename)

	out, err := os.Create(destPath)
	if err != nil {
		a.respondError(w, http.StatusInternalServerError, "failed to create file")
		return
	}
	written, err := io.Copy(out, file)
	out.Close()
	if err != nil {
		_ = os.Remove(destPath)
		a.respondError(w, http.StatusInternalServerError, "failed to write file")
		return
	}

	// Record in DB.
	mediaID, err := db.InsertMedia(ctx, a.DB, db.Media{
		ScreenID:     id,
		Filename:     storedFilename,
		OriginalName: header.Filename,
		MediaType:    mediaType,
		FileSize:     written,
	})
	if err != nil {
		_ = os.Remove(destPath)
		a.respondError(w, http.StatusInternalServerError, "failed to record media")
		return
	}

	// Update active content, recording previous for rollback.
	prevState, _ := db.GetScreenState(ctx, a.DB, id)
	mid := int(mediaID)
	if err := db.SetScreenMedia(ctx, a.DB, id, &mid); err != nil {
		a.respondError(w, http.StatusInternalServerError, "failed to update screen state")
		return
	}

	var prevMediaID *int
	if prevState != nil {
		prevMediaID = prevState.MediaID
	}
	_ = db.InsertAuditLog(ctx, a.DB, id, "content_uploaded", &mid, prevMediaID, header.Filename)

	// Trigger playback immediately.
	isImage := mediaType == "image"
	_ = a.Players.Play(*screen, destPath, isImage)

	a.redirect(w, r, "/screens/"+itoa(id))
}

// HandleMediaServe serves an uploaded media file (for image thumbnails etc.).
func (a *App) HandleMediaServe(w http.ResponseWriter, r *http.Request) {
	screenID := screenIDFromURL(r)
	filename := filepath.Base(chi.URLParam(r, "filename"))

	// Prevent path traversal.
	if strings.Contains(filename, "..") || strings.Contains(filename, "/") {
		http.NotFound(w, r)
		return
	}

	filePath := filepath.Join(a.UploadsDir, itoa(screenID), "content", filename)
	http.ServeFile(w, r, filePath)
}

// saveOffHoursImage saves an off-hours image upload and returns its path.
func saveOffHoursImage(uploadsDir string, screenID int, originalName string, src io.Reader) (string, error) {
	ext := strings.ToLower(filepath.Ext(originalName))
	storedFilename := uuid.New().String() + ext
	destDir := filepath.Join(uploadsDir, itoa(screenID), "offhours")
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return "", fmt.Errorf("mkdir: %w", err)
	}
	destPath := filepath.Join(destDir, storedFilename)
	out, err := os.Create(destPath)
	if err != nil {
		return "", fmt.Errorf("create: %w", err)
	}
	defer out.Close()
	if _, err := io.Copy(out, src); err != nil {
		_ = os.Remove(destPath)
		return "", fmt.Errorf("write: %w", err)
	}
	return destPath, nil
}

func detectMediaType(filename string) (string, error) {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".gif", ".bmp", ".webp":
		return "image", nil
	case ".mp4", ".mkv", ".avi", ".mov", ".wmv", ".flv", ".webm", ".m4v":
		return "video", nil
	}
	return "", fmt.Errorf("unsupported file type: %s", ext)
}
