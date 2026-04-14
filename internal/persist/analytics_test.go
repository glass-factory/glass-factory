package persist

import (
	"testing"
	"time"
)

func TestRecordEvent(t *testing.T) {
	s := tempDB(t)

	now := time.Now().UTC().Format(time.RFC3339)
	ev := &SiteEvent{
		Page:        "/",
		EventType:   "pageview",
		Referrer:    "https://google.com",
		UserAgent:   "Mozilla/5.0",
		Language:    "en-GB",
		ScreenWidth: 1920,
		Timestamp:   now,
	}

	if err := s.RecordEvent(ev); err != nil {
		t.Fatalf("RecordEvent: %v", err)
	}

	events, err := s.RecentSiteEvents("", 10)
	if err != nil {
		t.Fatalf("RecentSiteEvents: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events count = %d, want 1", len(events))
	}

	got := events[0]
	if got.Page != "/" {
		t.Errorf("Page = %q, want %q", got.Page, "/")
	}
	if got.EventType != "pageview" {
		t.Errorf("EventType = %q, want %q", got.EventType, "pageview")
	}
	if got.Referrer != "https://google.com" {
		t.Errorf("Referrer = %q, want %q", got.Referrer, "https://google.com")
	}
	if got.Language != "en-GB" {
		t.Errorf("Language = %q, want %q", got.Language, "en-GB")
	}
	if got.ScreenWidth != 1920 {
		t.Errorf("ScreenWidth = %d, want 1920", got.ScreenWidth)
	}
	if got.ID == 0 {
		t.Error("expected non-zero ID")
	}
}

func TestPageViews(t *testing.T) {
	s := tempDB(t)

	now := time.Now().UTC().Format(time.RFC3339)

	// 3 views for /build, 2 for /nodes, 1 for /stats
	pages := []string{"/build", "/build", "/build", "/nodes", "/nodes", "/stats"}
	for _, p := range pages {
		if err := s.RecordEvent(&SiteEvent{
			Page:      p,
			EventType: "pageview",
			Timestamp: now,
		}); err != nil {
			t.Fatalf("RecordEvent: %v", err)
		}
	}

	counts, err := s.PageViews("2000-01-01T00:00:00Z", 10)
	if err != nil {
		t.Fatalf("PageViews: %v", err)
	}
	if len(counts) != 3 {
		t.Fatalf("page count = %d, want 3", len(counts))
	}

	// Ordered by count desc
	if counts[0].Page != "/build" || counts[0].Count != 3 {
		t.Errorf("counts[0] = %+v, want /build:3", counts[0])
	}
	if counts[1].Page != "/nodes" || counts[1].Count != 2 {
		t.Errorf("counts[1] = %+v, want /nodes:2", counts[1])
	}
	if counts[2].Page != "/stats" || counts[2].Count != 1 {
		t.Errorf("counts[2] = %+v, want /stats:1", counts[2])
	}
}

func TestRecentEvents(t *testing.T) {
	s := tempDB(t)

	now := time.Now().UTC().Format(time.RFC3339)

	// Mix of event types
	events := []struct {
		page      string
		eventType string
	}{
		{"/", "pageview"},
		{"/build", "pageview"},
		{"/build", "error"},
		{"/nodes", "click"},
		{"/", "pageview"},
	}
	for _, e := range events {
		if err := s.RecordEvent(&SiteEvent{
			Page:      e.page,
			EventType: e.eventType,
			Timestamp: now,
		}); err != nil {
			t.Fatalf("RecordEvent: %v", err)
		}
	}

	// Filter by type
	errors, err := s.RecentSiteEvents("error", 10)
	if err != nil {
		t.Fatalf("RecentSiteEvents(error): %v", err)
	}
	if len(errors) != 1 {
		t.Fatalf("error events = %d, want 1", len(errors))
	}
	if errors[0].Page != "/build" {
		t.Errorf("error page = %q, want /build", errors[0].Page)
	}

	clicks, err := s.RecentSiteEvents("click", 10)
	if err != nil {
		t.Fatalf("RecentSiteEvents(click): %v", err)
	}
	if len(clicks) != 1 {
		t.Fatalf("click events = %d, want 1", len(clicks))
	}

	pageviews, err := s.RecentSiteEvents("pageview", 10)
	if err != nil {
		t.Fatalf("RecentSiteEvents(pageview): %v", err)
	}
	if len(pageviews) != 3 {
		t.Fatalf("pageview events = %d, want 3", len(pageviews))
	}
}

func TestRecentEvents_All(t *testing.T) {
	s := tempDB(t)

	now := time.Now().UTC().Format(time.RFC3339)

	for _, et := range []string{"pageview", "error", "click", "pageview"} {
		if err := s.RecordEvent(&SiteEvent{
			Page:      "/test",
			EventType: et,
			Timestamp: now,
		}); err != nil {
			t.Fatalf("RecordEvent: %v", err)
		}
	}

	all, err := s.RecentSiteEvents("", 10)
	if err != nil {
		t.Fatalf("RecentSiteEvents(all): %v", err)
	}
	if len(all) != 4 {
		t.Fatalf("all events = %d, want 4", len(all))
	}

	// Most recent first (highest ID first)
	if all[0].EventType != "pageview" {
		t.Errorf("all[0].EventType = %q, want pageview", all[0].EventType)
	}
	if all[0].ID < all[1].ID {
		t.Error("events not in descending ID order")
	}
}

func TestEventStats(t *testing.T) {
	s := tempDB(t)

	now := time.Now().UTC().Format(time.RFC3339)

	// Record events for different pages
	for _, p := range []string{"/", "/build", "/nodes", "/", "/build"} {
		if err := s.RecordEvent(&SiteEvent{
			Page:      p,
			EventType: "pageview",
			Timestamp: now,
		}); err != nil {
			t.Fatalf("RecordEvent: %v", err)
		}
	}

	total, today, uniquePages, err := s.EventStats()
	if err != nil {
		t.Fatalf("EventStats: %v", err)
	}
	if total != 5 {
		t.Errorf("total = %d, want 5", total)
	}
	if today != 5 {
		t.Errorf("today = %d, want 5", today)
	}
	if uniquePages != 3 {
		t.Errorf("uniquePages = %d, want 3", uniquePages)
	}
}

func TestEventsByHour(t *testing.T) {
	s := tempDB(t)

	// Events in two different hours
	hour1 := "2026-04-14T10:00:00Z"
	hour1b := "2026-04-14T10:30:00Z"
	hour2 := "2026-04-14T11:00:00Z"

	for _, ts := range []string{hour1, hour1b, hour2} {
		if err := s.RecordEvent(&SiteEvent{
			Page:      "/",
			EventType: "pageview",
			Timestamp: ts,
		}); err != nil {
			t.Fatalf("RecordEvent: %v", err)
		}
	}

	counts, err := s.EventsByHour("2026-04-14T00:00:00Z")
	if err != nil {
		t.Fatalf("EventsByHour: %v", err)
	}
	if len(counts) != 2 {
		t.Fatalf("hour buckets = %d, want 2", len(counts))
	}

	if counts[0].Hour != "2026-04-14 10" || counts[0].Count != 2 {
		t.Errorf("counts[0] = %+v, want {2026-04-14 10, 2}", counts[0])
	}
	if counts[1].Hour != "2026-04-14 11" || counts[1].Count != 1 {
		t.Errorf("counts[1] = %+v, want {2026-04-14 11, 1}", counts[1])
	}
}
