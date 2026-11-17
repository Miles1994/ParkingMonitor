package main

import (
	"bytes"
	"os"
	"testing"
)

// TestProgressReaderSteps checks that the bar moves in whole percents and ends
// at exactly 100.
func TestProgressReaderSteps(t *testing.T) {
	total := 100
	var got []int
	r := &progressReader{inner: bytes.NewReader(make([]byte, total)), total: int64(total), last: -1, report: func(p int) { got = append(got, p) }}
	buf := make([]byte, 1)
	for {
		if _, err := r.Read(buf); err != nil {
			break
		}
	}
	if len(got) != total || got[0] != 1 || got[total-1] != 100 {
		t.Fatalf("unexpected progress: %v", got)
	}
	for i, p := range got {
		if p != i+1 {
			t.Fatalf("progress out of order at %d: %v", i, got)
		}
	}
}

// TestDownloadUpdateLive fetches the published executable the way the update
// dialog does. It is opt-in because the file is ~20 MB and a slow link needs
// minutes; it guards the fix that stopped a total timeout from aborting a
// transfer that was still making progress.
func TestDownloadUpdateLive(t *testing.T) {
	if os.Getenv("PM_LIVE_DOWNLOAD") != "1" {
		t.Skip("set PM_LIVE_DOWNLOAD=1 to download the published release")
	}
	info, err := checkLatestRelease()
	if err != nil {
		t.Fatalf("checkLatestRelease: %v", err)
	}
	progress := make([]int, 0, 12)
	path, err := downloadUpdate(info, func(p int) { progress = append(progress, p) })
	if err != nil {
		t.Fatalf("downloadUpdate: %v", err)
	}
	defer os.Remove(path)
	stat, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("downloaded %.2f MB, progress=%v", float64(stat.Size())/(1<<20), progress)
	if stat.Size() < 1<<20 {
		t.Fatal("file too small")
	}
}
