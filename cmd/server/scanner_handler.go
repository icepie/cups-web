package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	scannerDiscoveryTimeout = 15 * time.Second
	scannerScanTimeout      = 100 * time.Second
	scannerCacheTTL         = 5 * time.Minute
)

var scannerDiscoveryCache struct {
	sync.Mutex
	scanners  []scannerInfo
	fetchedAt time.Time
}

var scannerLinePattern = regexp.MustCompile("(?m)^device `([^']+)' is a (.+)$")

type scannerInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type scanRequest struct {
	Device     string `json:"device"`
	Mode       string `json:"mode"`
	Resolution int    `json:"resolution"`
	Output     string `json:"output"`
}

type scannedDocument struct {
	path        string
	filename    string
	contentType string
	cleanup     func()
}

func scannersHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), scannerDiscoveryTimeout)
	defer cancel()

	refresh := r.URL.Query().Get("refresh") == "true"
	scanners, err := cachedScanners(ctx, refresh)
	if err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	writeJSON(w, scanners)
}

func scanHandler(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	var req scanRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid scan request")
		return
	}
	if err := req.normalize(); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	discoveryCtx, discoveryCancel := context.WithTimeout(r.Context(), scannerDiscoveryTimeout)
	scanners, err := cachedScanners(discoveryCtx, false)
	discoveryCancel()
	if err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	if !scannerExists(scanners, req.Device) {
		writeJSONError(w, http.StatusBadRequest, "scanner is unavailable")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), scannerScanTimeout)
	defer cancel()
	document, err := scanToDocument(ctx, req)
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, err.Error())
		return
	}
	defer document.cleanup()

	f, err := os.Open(document.path)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to open scanned document")
		return
	}
	defer f.Close()

	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", document.contentType)
	w.Header().Set("Content-Disposition", `inline; filename="`+document.filename+`"`)
	http.ServeContent(w, r, document.filename, time.Time{}, f)
}

func cachedScanners(ctx context.Context, refresh bool) ([]scannerInfo, error) {
	scannerDiscoveryCache.Lock()
	defer scannerDiscoveryCache.Unlock()

	if !refresh && !scannerDiscoveryCache.fetchedAt.IsZero() && time.Since(scannerDiscoveryCache.fetchedAt) < scannerCacheTTL {
		return cloneScanners(scannerDiscoveryCache.scanners), nil
	}

	scanners, err := listScanners(ctx)
	if err != nil {
		return nil, err
	}
	scannerDiscoveryCache.scanners = cloneScanners(scanners)
	scannerDiscoveryCache.fetchedAt = time.Now()
	return cloneScanners(scanners), nil
}

func cloneScanners(scanners []scannerInfo) []scannerInfo {
	return append([]scannerInfo(nil), scanners...)
}

func listScanners(ctx context.Context) ([]scannerInfo, error) {
	if _, err := exec.LookPath("scanimage"); err != nil {
		return nil, errors.New("scanner support is not installed")
	}

	out, err := exec.CommandContext(ctx, "scanimage", "--list-devices").CombinedOutput()
	if ctx.Err() != nil {
		return nil, errors.New("scanner discovery timed out")
	}
	if err != nil {
		return nil, fmt.Errorf("scanner discovery failed: %s", commandErrorSummary(out))
	}
	return parseScanners(string(out)), nil
}
func parseScanners(out string) []scannerInfo {
	matches := scannerLinePattern.FindAllStringSubmatch(out, -1)
	scanners := make([]scannerInfo, 0, len(matches))
	for _, match := range matches {
		id := strings.TrimSpace(match[1])
		name := strings.TrimSpace(match[2])
		if id != "" && name != "" {
			scanners = append(scanners, scannerInfo{ID: id, Name: name})
		}
	}
	return scanners
}

func scannerExists(scanners []scannerInfo, id string) bool {
	for _, scanner := range scanners {
		if scanner.ID == id {
			return true
		}
	}
	return false
}

func (req *scanRequest) normalize() error {
	if strings.TrimSpace(req.Device) == "" {
		return errors.New("scanner is required")
	}
	if req.Mode == "" {
		req.Mode = "Color"
	}
	switch req.Mode {
	case "Color", "Gray", "Lineart":
	default:
		return errors.New("unsupported scan mode")
	}
	if req.Resolution == 0 {
		req.Resolution = 300
	}
	switch req.Resolution {
	case 75, 100, 150, 200, 300, 600:
	default:
		return errors.New("unsupported scan resolution")
	}
	if req.Output == "" {
		req.Output = "pdf"
	}
	switch req.Output {
	case "pdf", "png":
		return nil
	default:
		return errors.New("unsupported scan output")
	}
}

func scanToDocument(ctx context.Context, req scanRequest) (scannedDocument, error) {
	tmpDir, err := os.MkdirTemp("", "cups-web-scan-")
	if err != nil {
		return scannedDocument{}, fmt.Errorf("failed to create scan workspace: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(tmpDir) }

	imagePath := tmpDir + "/scan.png"
	args := []string{
		"--device-name", req.Device,
		"--format=png",
		"--mode", req.Mode,
		"--resolution", fmt.Sprint(req.Resolution),
	}
	imageFile, err := os.OpenFile(imagePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		cleanup()
		return scannedDocument{}, fmt.Errorf("failed to create scan output: %w", err)
	}
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "scanimage", args...)
	cmd.Stdout = imageFile
	cmd.Stderr = &stderr
	err = cmd.Run()
	closeErr := imageFile.Close()
	if ctx.Err() != nil {
		cleanup()
		return scannedDocument{}, errors.New("scan timed out")
	}
	if err != nil {
		cleanup()
		return scannedDocument{}, fmt.Errorf("scan failed: %s", commandErrorSummary(stderr.Bytes()))
	}
	if closeErr != nil {
		cleanup()
		return scannedDocument{}, fmt.Errorf("failed to save scan: %w", closeErr)
	}
	if req.Output == "png" {
		return scannedDocument{path: imagePath, filename: "scan.png", contentType: "image/png", cleanup: cleanup}, nil
	}

	pdfPath, pdfCleanup, err := convertImageToPDF(imagePath, "portrait", "A4")
	if err != nil {
		cleanup()
		return scannedDocument{}, fmt.Errorf("failed to create scanned PDF: %w", err)
	}
	return scannedDocument{
		path:        pdfPath,
		filename:    "scan.pdf",
		contentType: "application/pdf",
		cleanup: func() {
			pdfCleanup()
			cleanup()
		},
	}, nil
}

func commandErrorSummary(out []byte) string {
	message := strings.TrimSpace(string(out))
	if message == "" {
		return "command failed"
	}
	if newline := strings.IndexByte(message, '\n'); newline >= 0 {
		return message[:newline]
	}
	return message
}
