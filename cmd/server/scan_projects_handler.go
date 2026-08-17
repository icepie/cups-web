package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"cups-web/internal/auth"
	"cups-web/internal/store"

	"github.com/gorilla/mux"
)

const maxScanPageUploadBytes int64 = 50 << 20

type scanProjectRequest struct {
	Name string `json:"name"`
}

type scanProjectResponse struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	PageCount int    `json:"pageCount"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

type scanPageResponse struct {
	ID          int64  `json:"id"`
	Position    int    `json:"position"`
	FileURL     string `json:"fileUrl"`
	OriginalURL string `json:"originalUrl"`
	CreatedAt   string `json:"createdAt"`
}

type scanProjectDetailResponse struct {
	Project scanProjectResponse `json:"project"`
	Pages   []scanPageResponse  `json:"pages"`
}

type scanPageOrderRequest struct {
	PageIDs []int64 `json:"pageIds"`
}

func scanProjectsHandler(w http.ResponseWriter, r *http.Request) {
	sess, err := auth.GetSession(r)
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var projects []store.ScanProject
	if err := appStore.WithTx(r.Context(), true, func(tx *sql.Tx) error {
		var err error
		projects, err = store.ListScanProjects(r.Context(), tx, sess.UserID)
		return err
	}); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to load scan projects")
		return
	}
	response := make([]scanProjectResponse, 0, len(projects))
	for _, project := range projects {
		response = append(response, mapScanProject(project))
	}
	writeJSON(w, response)
}

func createScanProjectHandler(w http.ResponseWriter, r *http.Request) {
	sess, err := auth.GetSession(r)
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var request scanProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	name := strings.TrimSpace(request.Name)
	if name == "" || len([]rune(name)) > 100 {
		writeJSONError(w, http.StatusBadRequest, "project name must be 1 to 100 characters")
		return
	}
	var project store.ScanProject
	if err := appStore.WithTx(r.Context(), false, func(tx *sql.Tx) error {
		var err error
		project, err = store.CreateScanProject(r.Context(), tx, sess.UserID, name)
		return err
	}); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to create scan project")
		return
	}
	writeJSONStatus(w, http.StatusCreated, mapScanProject(project))
}

func scanProjectHandler(w http.ResponseWriter, r *http.Request) {
	sess, projectID, ok := scanProjectSessionAndID(w, r)
	if !ok {
		return
	}
	var project store.ScanProject
	var pages []store.ScanPage
	if err := appStore.WithTx(r.Context(), true, func(tx *sql.Tx) error {
		var err error
		project, err = store.GetScanProject(r.Context(), tx, sess.UserID, projectID)
		if err != nil {
			return err
		}
		pages, err = store.ListScanPages(r.Context(), tx, sess.UserID, projectID)
		return err
	}); err != nil {
		writeScanProjectError(w, err, "failed to load scan project")
		return
	}
	response := scanProjectDetailResponse{
		Project: mapScanProject(project),
		Pages:   make([]scanPageResponse, 0, len(pages)),
	}
	for _, page := range pages {
		response.Pages = append(response.Pages, mapScanPage(projectID, page))
	}
	writeJSON(w, response)
}

func deleteScanProjectHandler(w http.ResponseWriter, r *http.Request) {
	sess, projectID, ok := scanProjectSessionAndID(w, r)
	if !ok {
		return
	}
	var paths []string
	if err := appStore.WithTx(r.Context(), false, func(tx *sql.Tx) error {
		var err error
		paths, err = store.DeleteScanProject(r.Context(), tx, sess.UserID, projectID)
		return err
	}); err != nil {
		writeScanProjectError(w, err, "failed to delete scan project")
		return
	}
	removeScanFiles(paths)
	w.WriteHeader(http.StatusNoContent)
}

func createScanPageHandler(w http.ResponseWriter, r *http.Request) {
	sess, projectID, ok := scanProjectSessionAndID(w, r)
	if !ok {
		return
	}
	relPath, absPath, err := saveScanPageUpload(w, r)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	var page store.ScanPage
	err = appStore.WithTx(r.Context(), false, func(tx *sql.Tx) error {
		var err error
		page, err = store.CreateScanPage(r.Context(), tx, sess.UserID, projectID, relPath)
		return err
	})
	if err != nil {
		_ = os.Remove(absPath)
		writeScanProjectError(w, err, "failed to save scan page")
		return
	}
	writeJSONStatus(w, http.StatusCreated, mapScanPage(projectID, page))
}

func updateScanPageHandler(w http.ResponseWriter, r *http.Request) {
	sess, projectID, pageID, ok := scanPageSessionAndIDs(w, r)
	if !ok {
		return
	}
	relPath, absPath, err := saveScanPageUpload(w, r)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	var oldPath string
	err = appStore.WithTx(r.Context(), false, func(tx *sql.Tx) error {
		var err error
		oldPath, err = store.UpdateScanPage(r.Context(), tx, sess.UserID, projectID, pageID, relPath)
		return err
	})
	if err != nil {
		_ = os.Remove(absPath)
		writeScanProjectError(w, err, "failed to update scan page")
		return
	}
	if oldPath != "" && oldPath != relPath {
		removeScanFiles([]string{oldPath})
	}
	writeJSON(w, map[string]string{"fileUrl": scanPageFileURL(projectID, pageID, false)})
}

func resetScanPageHandler(w http.ResponseWriter, r *http.Request) {
	sess, projectID, pageID, ok := scanPageSessionAndIDs(w, r)
	if !ok {
		return
	}
	var oldPath string
	if err := appStore.WithTx(r.Context(), false, func(tx *sql.Tx) error {
		var err error
		oldPath, err = store.ResetScanPage(r.Context(), tx, sess.UserID, projectID, pageID)
		return err
	}); err != nil {
		writeScanProjectError(w, err, "failed to reset scan page")
		return
	}
	if oldPath != "" {
		removeScanFiles([]string{oldPath})
	}
	writeJSON(w, map[string]string{"fileUrl": scanPageFileURL(projectID, pageID, false)})
}

func deleteScanPageHandler(w http.ResponseWriter, r *http.Request) {
	sess, projectID, pageID, ok := scanPageSessionAndIDs(w, r)
	if !ok {
		return
	}
	var paths []string
	if err := appStore.WithTx(r.Context(), false, func(tx *sql.Tx) error {
		var err error
		paths, err = store.DeleteScanPage(r.Context(), tx, sess.UserID, projectID, pageID)
		return err
	}); err != nil {
		writeScanProjectError(w, err, "failed to delete scan page")
		return
	}
	removeScanFiles(paths)
	w.WriteHeader(http.StatusNoContent)
}

func reorderScanPagesHandler(w http.ResponseWriter, r *http.Request) {
	sess, projectID, ok := scanProjectSessionAndID(w, r)
	if !ok {
		return
	}
	var request scanPageOrderRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := appStore.WithTx(r.Context(), false, func(tx *sql.Tx) error {
		return store.ReorderScanPages(r.Context(), tx, sess.UserID, projectID, request.PageIDs)
	}); err != nil {
		writeScanProjectError(w, err, "failed to reorder scan pages")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func scanPageFileHandler(w http.ResponseWriter, r *http.Request) {
	sess, projectID, pageID, ok := scanPageSessionAndIDs(w, r)
	if !ok {
		return
	}
	var page store.ScanPage
	if err := appStore.WithTx(r.Context(), true, func(tx *sql.Tx) error {
		var err error
		page, err = store.GetScanPage(r.Context(), tx, sess.UserID, projectID, pageID)
		return err
	}); err != nil {
		writeScanProjectError(w, err, "failed to load scan page")
		return
	}
	path := page.EditedPath
	if r.URL.Query().Get("original") == "1" || path == "" {
		path = page.OriginalPath
	}
	file, err := os.OpenInRoot(scanStorageDir(), filepath.FromSlash(path))
	if err != nil {
		writeJSONError(w, http.StatusNotFound, "scan page file not found")
		return
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to stat scan page file")
		return
	}
	w.Header().Set("Content-Type", "image/png")
	http.ServeContent(w, r, "scan.png", stat.ModTime(), file)
}

func scanProjectSessionAndID(w http.ResponseWriter, r *http.Request) (auth.Session, int64, bool) {
	sess, err := auth.GetSession(r)
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return auth.Session{}, 0, false
	}
	projectID, err := strconv.ParseInt(mux.Vars(r)["projectId"], 10, 64)
	if err != nil || projectID < 1 {
		writeJSONError(w, http.StatusBadRequest, "invalid scan project id")
		return auth.Session{}, 0, false
	}
	return sess, projectID, true
}

func scanPageSessionAndIDs(w http.ResponseWriter, r *http.Request) (auth.Session, int64, int64, bool) {
	sess, projectID, ok := scanProjectSessionAndID(w, r)
	if !ok {
		return auth.Session{}, 0, 0, false
	}
	pageID, err := strconv.ParseInt(mux.Vars(r)["pageId"], 10, 64)
	if err != nil || pageID < 1 {
		writeJSONError(w, http.StatusBadRequest, "invalid scan page id")
		return auth.Session{}, 0, 0, false
	}
	return sess, projectID, pageID, true
}

func saveScanPageUpload(w http.ResponseWriter, r *http.Request) (string, string, error) {
	r.Body = http.MaxBytesReader(w, r.Body, maxScanPageUploadBytes)
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		return "", "", fmt.Errorf("scan page upload is too large or invalid")
	}
	file, _, err := r.FormFile("image")
	if err != nil {
		return "", "", fmt.Errorf("missing scan page image")
	}
	defer file.Close()
	relPath, absPath, err := saveUploadedFile(io.LimitReader(file, maxScanPageUploadBytes), "scan.png", scanStorageDir())
	if err != nil {
		return "", "", fmt.Errorf("failed to store scan page")
	}
	if err := validateScanPNG(absPath); err != nil {
		_ = os.Remove(absPath)
		return "", "", err
	}
	return relPath, absPath, nil
}

func validateScanPNG(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("failed to validate scan page")
	}
	defer file.Close()
	config, format, err := image.DecodeConfig(file)
	if err != nil || format != "png" || config.Width < 1 || config.Height < 1 || config.Width > 6000 || config.Height > 6000 {
		return fmt.Errorf("scan page must be a PNG image up to 6000 × 6000 pixels")
	}
	return nil
}

func scanStorageDir() string {
	return filepath.Join(uploadDir, "scans")
}

func removeScanFiles(paths []string) {
	root, err := os.OpenRoot(scanStorageDir())
	if err != nil {
		return
	}
	defer root.Close()
	for _, path := range paths {
		if path != "" {
			_ = root.Remove(filepath.FromSlash(path))
		}
	}
}

func mapScanProject(project store.ScanProject) scanProjectResponse {
	return scanProjectResponse{
		ID:        project.ID,
		Name:      project.Name,
		PageCount: project.PageCount,
		CreatedAt: project.CreatedAt,
		UpdatedAt: project.UpdatedAt,
	}
}

func mapScanPage(projectID int64, page store.ScanPage) scanPageResponse {
	return scanPageResponse{
		ID:          page.ID,
		Position:    page.Position,
		FileURL:     scanPageFileURL(projectID, page.ID, false),
		OriginalURL: scanPageFileURL(projectID, page.ID, true),
		CreatedAt:   page.CreatedAt,
	}
}

func scanPageFileURL(projectID, pageID int64, original bool) string {
	url := fmt.Sprintf("/api/scan-projects/%d/pages/%d/file", projectID, pageID)
	if original {
		return url + "?original=1"
	}
	return url
}

func writeScanProjectError(w http.ResponseWriter, err error, fallback string) {
	if errors.Is(err, sql.ErrNoRows) {
		writeJSONError(w, http.StatusNotFound, "scan project or page not found")
		return
	}
	writeJSONError(w, http.StatusInternalServerError, fallback)
}
