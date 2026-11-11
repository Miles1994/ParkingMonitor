package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const logRetentionDays = 7

type logFile struct {
	mu   sync.Mutex
	dir  string
	date string
}

func newLogFile() *logFile {
	base, err := os.UserConfigDir()
	if err != nil {
		return &logFile{}
	}
	dir := filepath.Join(base, "ParkingMonitor", "logs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return &logFile{}
	}
	return &logFile{dir: dir}
}

func (l *logFile) Dir() string {
	if l == nil {
		return ""
	}
	return l.dir
}

func (l *logFile) write(now time.Time, msg string) {
	if l == nil || l.dir == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	date := now.Format("2006-01-02")
	if date != l.date {
		l.date = date
		l.cleanupLocked(now)
	}
	path := filepath.Join(l.dir, "monitor-"+date+".log")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	_, _ = f.WriteString(now.Format("2006-01-02 15:04:05") + "  " + msg + "\r\n")
	_ = f.Close()
}

func (l *logFile) cleanupLocked(now time.Time) {
	entries, err := os.ReadDir(l.dir)
	if err != nil {
		return
	}
	cutoff := now.AddDate(0, 0, -logRetentionDays)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, "monitor-") || !strings.HasSuffix(name, ".log") {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.ModTime().Before(cutoff) {
			continue
		}
		_ = os.Remove(filepath.Join(l.dir, name))
	}
}
