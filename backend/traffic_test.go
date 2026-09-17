package main

import (
	"testing"
	"time"
)

func TestTrafficSnapshotUsesSessionTotalsAndIntervalRates(t *testing.T) {
	snapshot := snapshotTraffic(100, 200, 130, 260, 230, 460, 2*time.Second)
	if snapshot.UploadBytes != 130 || snapshot.DownloadBytes != 260 {
		t.Fatalf("wrong session totals: %#v", snapshot)
	}
	if snapshot.UploadBPS != 50 || snapshot.DownloadBPS != 100 {
		t.Fatalf("wrong transfer rates: %#v", snapshot)
	}
}

func TestTrafficSnapshotClampsCounterReset(t *testing.T) {
	snapshot := snapshotTraffic(500, 600, 550, 650, 20, 30, time.Second)
	if snapshot.UploadBytes != 0 || snapshot.DownloadBytes != 0 || snapshot.UploadBPS != 0 || snapshot.DownloadBPS != 0 {
		t.Fatalf("counter reset produced negative traffic: %#v", snapshot)
	}
}
