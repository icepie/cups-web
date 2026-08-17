package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestParseScanners(t *testing.T) {
	out := "Some backend message\n" +
		"device `hpaio:/usb/DeskJet_2130_series?serial=CN9744828Q079N' is a Hewlett-Packard DeskJet_2130_series all-in-one\n" +
		"device `airscan:e0:Office Scanner' is a eSCL Office Scanner\n"

	got := parseScanners(out)
	want := []scannerInfo{
		{ID: "hpaio:/usb/DeskJet_2130_series?serial=CN9744828Q079N", Name: "Hewlett-Packard DeskJet_2130_series all-in-one"},
		{ID: "airscan:e0:Office Scanner", Name: "eSCL Office Scanner"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseScanners() = %#v, want %#v", got, want)
	}
}

func TestScanRequestNormalize(t *testing.T) {
	tests := []struct {
		name    string
		req     scanRequest
		want    scanRequest
		wantErr bool
	}{
		{
			name: "defaults",
			req:  scanRequest{Device: "hpaio:/usb/test"},
			want: scanRequest{Device: "hpaio:/usb/test", Mode: "Color", Resolution: 300, Output: "pdf"},
		},
		{
			name: "lineart at supported resolution",
			req:  scanRequest{Device: "hpaio:/usb/test", Mode: "Lineart", Resolution: 600},
			want: scanRequest{Device: "hpaio:/usb/test", Mode: "Lineart", Resolution: 600, Output: "pdf"},
		},
		{
			name: "png output",
			req:  scanRequest{Device: "hpaio:/usb/test", Output: "png"},
			want: scanRequest{Device: "hpaio:/usb/test", Mode: "Color", Resolution: 300, Output: "png"},
		},
		{name: "missing scanner", req: scanRequest{}, wantErr: true},
		{name: "unsupported mode", req: scanRequest{Device: "hpaio:/usb/test", Mode: "CMYK"}, wantErr: true},
		{name: "unsupported resolution", req: scanRequest{Device: "hpaio:/usb/test", Resolution: 1200}, wantErr: true},
		{name: "unsupported output", req: scanRequest{Device: "hpaio:/usb/test", Output: "jpeg"}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.req
			err := got.normalize()
			if tt.wantErr {
				if err == nil {
					t.Fatal("normalize() succeeded, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("normalize() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("normalize() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestCachedScannersReturnsCopyWithinTTL(t *testing.T) {
	scannerDiscoveryCache.Lock()
	previousScanners := cloneScanners(scannerDiscoveryCache.scanners)
	previousFetchedAt := scannerDiscoveryCache.fetchedAt
	scannerDiscoveryCache.scanners = []scannerInfo{{ID: "test:scanner", Name: "Test Scanner"}}
	scannerDiscoveryCache.fetchedAt = time.Now()
	scannerDiscoveryCache.Unlock()
	t.Cleanup(func() {
		scannerDiscoveryCache.Lock()
		scannerDiscoveryCache.scanners = previousScanners
		scannerDiscoveryCache.fetchedAt = previousFetchedAt
		scannerDiscoveryCache.Unlock()
	})

	got, err := cachedScanners(context.Background(), false)
	if err != nil {
		t.Fatalf("cachedScanners() error = %v", err)
	}
	got[0].Name = "Mutated"

	again, err := cachedScanners(context.Background(), false)
	if err != nil {
		t.Fatalf("cachedScanners() second call error = %v", err)
	}
	if again[0].Name != "Test Scanner" {
		t.Fatalf("cached scanners were mutated: %#v", again)
	}
}

func TestReadPNMHeader(t *testing.T) {
	tests := []struct {
		name string
		data string
		want pnmHeader
	}{
		{
			name: "color with comment",
			data: "P6\n# scanimage\n2 3\n255\n",
			want: pnmHeader{magic: "P6", width: 2, height: 3, maxValue: 255, rowBytes: 6},
		},
		{
			name: "lineart",
			data: "P4\n9 2\n",
			want: pnmHeader{magic: "P4", width: 9, height: 2, maxValue: 1, rowBytes: 2},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := readPNMHeader(bufio.NewReader(bytes.NewBufferString(tt.data)))
			if err != nil {
				t.Fatalf("readPNMHeader() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("readPNMHeader() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestStreamScanForwardsPNMRows(t *testing.T) {
	dir := t.TempDir()
	scanimage := filepath.Join(dir, "scanimage")
	if err := os.WriteFile(scanimage, []byte("#!/bin/sh\nprintf 'P6\\n2 1\\n255\\n\\001\\002\\003\\004\\005\\006'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	recorder := httptest.NewRecorder()
	if err := streamScan(context.Background(), recorder, scanRequest{Device: "test", Mode: "Color", Resolution: 75}); err != nil {
		t.Fatalf("streamScan() error = %v", err)
	}
	parts := bytes.SplitN(recorder.Body.Bytes(), []byte{'\n'}, 2)
	if len(parts) != 2 {
		t.Fatalf("stream body = %q, want metadata plus pixels", recorder.Body.Bytes())
	}
	var header scanStreamHeader
	if err := json.Unmarshal(parts[0], &header); err != nil {
		t.Fatalf("stream metadata = %q: %v", parts[0], err)
	}
	wantHeader := scanStreamHeader{Magic: "P6", Width: 2, Height: 1, MaxValue: 255, RowBytes: 6}
	if header != wantHeader {
		t.Fatalf("stream metadata = %#v, want %#v", header, wantHeader)
	}
	if want := []byte{1, 2, 3, 4, 5, 6}; !bytes.Equal(parts[1], want) {
		t.Fatalf("stream pixels = %v, want %v", parts[1], want)
	}
}

func TestScanStreamHandlerReturnsErrorBeforeStreamStarts(t *testing.T) {
	dir := t.TempDir()
	scanimage := filepath.Join(dir, "scanimage")
	if err := os.WriteFile(scanimage, []byte("#!/bin/sh\nprintf 'scanner busy' >&2\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	scannerDiscoveryCache.Lock()
	previousScanners := cloneScanners(scannerDiscoveryCache.scanners)
	previousFetchedAt := scannerDiscoveryCache.fetchedAt
	scannerDiscoveryCache.scanners = []scannerInfo{{ID: "test", Name: "Test Scanner"}}
	scannerDiscoveryCache.fetchedAt = time.Now()
	scannerDiscoveryCache.Unlock()
	t.Cleanup(func() {
		scannerDiscoveryCache.Lock()
		scannerDiscoveryCache.scanners = previousScanners
		scannerDiscoveryCache.fetchedAt = previousFetchedAt
		scannerDiscoveryCache.Unlock()
	})

	request := httptest.NewRequest(http.MethodPost, "/api/scan/stream", strings.NewReader(`{"device":"test","mode":"Color","resolution":75}`))
	recorder := httptest.NewRecorder()
	scanStreamHandler(recorder, request)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body = %q", recorder.Code, http.StatusInternalServerError, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "scanner busy") {
		t.Fatalf("error body = %q, want scanner detail", recorder.Body.String())
	}
}
