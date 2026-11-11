package main

import (
	"path/filepath"
	"testing"
	"time"
)

func TestBuildChartBuckets(t *testing.T) {
	now := time.Date(2026, 11, 10, 15, 30, 0, 0, cstZone) // 周二
	entries := []entryRecord{
		{Carno: "A", Entry: time.Date(2026, 11, 10, 9, 0, 0, 0, cstZone)},
		{Carno: "A", Entry: time.Date(2026, 11, 10, 9, 30, 0, 0, cstZone)},
		{Carno: "A", Entry: time.Date(2026, 11, 9, 8, 0, 0, 0, cstZone)},
		{Carno: "B", Entry: time.Date(2026, 11, 10, 10, 0, 0, 0, cstZone)},
	}

	today := buildChartBuckets(entries, "A", statsToday, now)
	if len(today) != 24 || today[9].Count != 2 || !today[15].Current {
		t.Fatalf("当日统计异常: len=%d 9时=%d 15时current=%v", len(today), today[9].Count, today[15].Current)
	}

	week := buildChartBuckets(entries, "A", statsWeek, now)
	if len(week) != 7 || week[0].Count != 1 || week[1].Count != 2 || !week[1].Current {
		t.Fatalf("本周统计异常: %+v", week)
	}

	month := buildChartBuckets(entries, "A", statsMonth, now)
	if len(month) != 30 || month[8].Count != 1 || month[9].Count != 2 || !month[9].Current {
		t.Fatalf("本月统计异常: len=%d 9日=%d 10日=%d", len(month), month[8].Count, month[9].Count)
	}

	if all := buildChartBuckets(entries, "", statsToday, now); all[9].Count != 0 {
		t.Fatalf("未填写车牌不应统计入场: %d", all[9].Count)
	}
}

func TestEntryStoreDedupeAndReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "entries.json")
	store := &entryStore{path: path}
	info := EntryInfo{Carno: "A", Serial: "S1", Entry: time.Now(), ParkName: "测试停车场"}
	if !store.add(info) {
		t.Fatal("首次入场应写入统计")
	}
	if store.add(info) {
		t.Fatal("同一停车记录不应重复写入统计")
	}

	reloaded := &entryStore{path: path}
	reloaded.load()
	if got := len(reloaded.snapshot()); got != 1 {
		t.Fatalf("重新加载后应有 1 条记录，实际 %d", got)
	}
}
