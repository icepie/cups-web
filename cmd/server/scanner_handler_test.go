package main

import (
	"context"
	"reflect"
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
