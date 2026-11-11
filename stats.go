package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"
)

const (
	statsToday = iota
	statsWeek
	statsMonth
)

const (
	entryRetentionDays = 180
	entryLimit         = 500
)

type entryRecord struct {
	Carno  string    `json:"carno"`
	Serial string    `json:"serial"`
	Entry  time.Time `json:"entry"`
	Park   string    `json:"park,omitempty"`
}

type entryStore struct {
	mu      sync.Mutex
	path    string
	entries []entryRecord
}

func entryStorePath() string {
	base, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(base, "ParkingMonitor", "entries.json")
}

func loadEntryStore() *entryStore {
	store := &entryStore{path: entryStorePath()}
	store.load()
	return store
}

func (s *entryStore) load() {
	if s.path == "" {
		return
	}
	raw, err := os.ReadFile(s.path)
	if err != nil {
		return
	}
	var file struct {
		Entries []entryRecord `json:"entries"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		return
	}
	seen := make(map[string]bool, len(file.Entries))
	entries := make([]entryRecord, 0, len(file.Entries))
	for _, e := range file.Entries {
		if e.Entry.IsZero() {
			continue
		}
		key := entryKey(e.Carno, e.Serial, e.Entry)
		if seen[key] {
			continue
		}
		seen[key] = true
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Entry.Before(entries[j].Entry) })
	s.entries = entries
	s.mu.Lock()
	s.pruneLocked(time.Now())
	s.mu.Unlock()
}

func entryKey(carno, serial string, entry time.Time) string {
	if serial != "" {
		return carno + "|" + serial
	}
	return carno + "|" + strconv.FormatInt(entry.Unix(), 10)
}

func (s *entryStore) add(info EntryInfo) bool {
	if s == nil || info.Entry.IsZero() {
		return false
	}
	key := entryKey(info.Carno, info.Serial, info.Entry)
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.entries {
		if entryKey(e.Carno, e.Serial, e.Entry) == key {
			return false
		}
	}
	s.entries = append(s.entries, entryRecord{
		Carno:  info.Carno,
		Serial: info.Serial,
		Entry:  info.Entry,
		Park:   info.ParkName,
	})
	sort.Slice(s.entries, func(i, j int) bool { return s.entries[i].Entry.Before(s.entries[j].Entry) })
	s.pruneLocked(time.Now())
	s.saveLocked()
	return true
}

func (s *entryStore) pruneLocked(now time.Time) {
	cutoff := now.AddDate(0, 0, -entryRetentionDays)
	kept := s.entries[:0]
	for _, e := range s.entries {
		if e.Entry.Before(cutoff) {
			continue
		}
		kept = append(kept, e)
	}
	if len(kept) > entryLimit {
		kept = kept[len(kept)-entryLimit:]
	}
	s.entries = kept
}

func (s *entryStore) saveLocked() {
	if s.path == "" {
		return
	}
	raw, err := json.MarshalIndent(struct {
		Entries []entryRecord `json:"entries"`
	}{Entries: s.entries}, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(s.path, raw, 0o600)
}

func (s *entryStore) snapshot() []entryRecord {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]entryRecord(nil), s.entries...)
}

type chartBucket struct {
	Label     string
	Count     int
	Current   bool
	ShowLabel bool
}

func buildChartBuckets(entries []entryRecord, carno string, mode int, now time.Time) []chartBucket {
	now = now.In(cstZone)
	times := make([]time.Time, 0, len(entries))
	for _, e := range entries {
		if carno == "" || e.Carno != carno {
			continue
		}
		if e.Entry.IsZero() {
			continue
		}
		times = append(times, e.Entry.In(cstZone))
	}

	var starts []time.Time
	var labels []string
	var show []bool
	switch mode {
	case statsWeek:
		day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, cstZone)
		offset := (int(now.Weekday()) + 6) % 7
		start := day.AddDate(0, 0, -offset)
		names := []string{"周一", "周二", "周三", "周四", "周五", "周六", "周日"}
		for i := 0; i < 7; i++ {
			starts = append(starts, start.AddDate(0, 0, i))
			labels = append(labels, names[i])
			show = append(show, true)
		}
	case statsMonth:
		start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, cstZone)
		days := int(start.AddDate(0, 1, 0).Sub(start).Hours() / 24)
		for i := 0; i < days; i++ {
			starts = append(starts, start.AddDate(0, 0, i))
			labels = append(labels, strconv.Itoa(i+1))
			show = append(show, i%5 == 0 || i == days-1)
		}
	default:
		start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, cstZone)
		for i := 0; i < 24; i++ {
			starts = append(starts, start.Add(time.Duration(i)*time.Hour))
			labels = append(labels, strconv.Itoa(i)+"时")
			show = append(show, i%6 == 0)
		}
	}

	buckets := make([]chartBucket, len(starts))
	for i := range starts {
		end := starts[i].AddDate(0, 0, 1)
		if mode == statsToday {
			end = starts[i].Add(time.Hour)
		}
		buckets[i] = chartBucket{
			Label:     labels[i],
			ShowLabel: show[i],
			Current:   !now.Before(starts[i]) && now.Before(end),
		}
		for _, t := range times {
			if !t.Before(starts[i]) && t.Before(end) {
				buckets[i].Count++
			}
		}
	}
	return buckets
}
