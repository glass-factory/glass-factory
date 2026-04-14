// analytics.go provides site analytics persistence for Glass Factory HQ.
//
// Tracks page views, errors, and user interaction events to understand
// how makers use the site — without third-party trackers.
//
// 站点分析 — 页面浏览、错误和用户交互事件的持久化存储，无需第三方追踪。
package persist

import (
	"database/sql"
	"fmt"
	"strings"
)

// SiteEvent represents a single analytics event from the site.
type SiteEvent struct {
	ID          int64  `json:"id"`
	Page        string `json:"page"`
	EventType   string `json:"event_type"`   // pageview, error, click
	Referrer    string `json:"referrer"`
	UserAgent   string `json:"user_agent"`
	Language    string `json:"language"`      // browser language
	ScreenWidth int    `json:"screen_width"`
	Country     string `json:"country"`       // empty for now
	Timestamp   string `json:"timestamp"`
	Detail      string `json:"detail"`        // error message, click target, etc
}

// PageViewCount holds a page path and its view count.
type PageViewCount struct {
	Page  string `json:"page"`
	Count int    `json:"count"`
}

// HourCount holds an hour bucket and its event count.
type HourCount struct {
	Hour  string `json:"hour"`
	Count int    `json:"count"`
}

// migrateAnalytics creates the site_events table and indexes.
func (s *Store) migrateAnalytics() error {
	schema := `
	CREATE TABLE IF NOT EXISTS site_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		page TEXT NOT NULL,
		event_type TEXT NOT NULL DEFAULT 'pageview',
		referrer TEXT NOT NULL DEFAULT '',
		user_agent TEXT NOT NULL DEFAULT '',
		language TEXT NOT NULL DEFAULT '',
		screen_width INTEGER NOT NULL DEFAULT 0,
		country TEXT NOT NULL DEFAULT '',
		timestamp TEXT NOT NULL,
		detail TEXT NOT NULL DEFAULT ''
	);
	CREATE INDEX IF NOT EXISTS idx_events_page ON site_events(page);
	CREATE INDEX IF NOT EXISTS idx_events_type ON site_events(event_type);
	CREATE INDEX IF NOT EXISTS idx_events_ts ON site_events(timestamp);
	`
	_, err := s.db.Exec(schema)
	return err
}

// RecordEvent inserts a site analytics event.
func (s *Store) RecordEvent(e *SiteEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`
		INSERT INTO site_events (page, event_type, referrer, user_agent, language, screen_width, country, timestamp, detail)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.Page, e.EventType, e.Referrer, e.UserAgent, e.Language,
		e.ScreenWidth, e.Country, e.Timestamp, e.Detail,
	)
	return err
}

// PageViews returns page view counts grouped by page since the given timestamp,
// ordered by count descending.
func (s *Store) PageViews(since string, limit int) ([]PageViewCount, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(`
		SELECT page, COUNT(*) as cnt
		FROM site_events
		WHERE event_type = 'pageview' AND timestamp >= ?
		GROUP BY page
		ORDER BY cnt DESC
		LIMIT ?`, since, limit)
	if err != nil {
		return nil, fmt.Errorf("persist: page views: %w", err)
	}
	defer rows.Close()

	var counts []PageViewCount
	for rows.Next() {
		var pv PageViewCount
		if err := rows.Scan(&pv.Page, &pv.Count); err != nil {
			return nil, fmt.Errorf("persist: scan page view: %w", err)
		}
		counts = append(counts, pv)
	}
	return counts, nil
}

// RecentSiteEvents returns recent site events, optionally filtered by event type.
// Pass an empty eventType to return all events.
func (s *Store) RecentSiteEvents(eventType string, limit int) ([]*SiteEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var rows *sql.Rows
	var err error
	if eventType != "" {
		rows, err = s.db.Query(`
			SELECT id, page, event_type, referrer, user_agent, language, screen_width, country, timestamp, detail
			FROM site_events
			WHERE event_type = ?
			ORDER BY id DESC
			LIMIT ?`, eventType, limit)
	} else {
		rows, err = s.db.Query(`
			SELECT id, page, event_type, referrer, user_agent, language, screen_width, country, timestamp, detail
			FROM site_events
			ORDER BY id DESC
			LIMIT ?`, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("persist: recent site events: %w", err)
	}
	defer rows.Close()

	var events []*SiteEvent
	for rows.Next() {
		e := &SiteEvent{}
		if err := rows.Scan(&e.ID, &e.Page, &e.EventType, &e.Referrer, &e.UserAgent,
			&e.Language, &e.ScreenWidth, &e.Country, &e.Timestamp, &e.Detail); err != nil {
			return nil, fmt.Errorf("persist: scan site event: %w", err)
		}
		events = append(events, e)
	}
	return events, nil
}

// EventStats returns summary statistics: total events, events today, and unique page count.
func (s *Store) EventStats() (total int, today int, uniquePages int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err = s.db.QueryRow(`SELECT COUNT(*) FROM site_events`).Scan(&total); err != nil {
		err = fmt.Errorf("persist: event stats total: %w", err)
		return
	}

	// Today = timestamps starting with today's date prefix (YYYY-MM-DD)
	if err = s.db.QueryRow(`
		SELECT COUNT(*) FROM site_events
		WHERE timestamp >= date('now')`,
	).Scan(&today); err != nil {
		err = fmt.Errorf("persist: event stats today: %w", err)
		return
	}

	if err = s.db.QueryRow(`SELECT COUNT(DISTINCT page) FROM site_events`).Scan(&uniquePages); err != nil {
		err = fmt.Errorf("persist: event stats unique pages: %w", err)
		return
	}

	return total, today, uniquePages, nil
}

// EventsByHour returns event counts grouped by hour since the given timestamp.
// Hours are formatted as "YYYY-MM-DD HH" for charting.
func (s *Store) EventsByHour(since string) ([]HourCount, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(`
		SELECT substr(timestamp, 1, 13) as hour, COUNT(*) as cnt
		FROM site_events
		WHERE timestamp >= ?
		GROUP BY hour
		ORDER BY hour ASC`, since)
	if err != nil {
		return nil, fmt.Errorf("persist: events by hour: %w", err)
	}
	defer rows.Close()

	var counts []HourCount
	for rows.Next() {
		var hc HourCount
		if err := rows.Scan(&hc.Hour, &hc.Count); err != nil {
			return nil, fmt.Errorf("persist: scan hour count: %w", err)
		}
		// Normalise the "T" separator — substr(RFC3339, 1, 13) gives "2026-04-14T12"
		hc.Hour = strings.Replace(hc.Hour, "T", " ", 1)
		counts = append(counts, hc)
	}
	return counts, nil
}
