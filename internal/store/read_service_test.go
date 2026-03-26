package store

import (
	"testing"
	"time"
)

func TestBuildDashboardTotals(t *testing.T) {
	totals := buildDashboardTotals(
		[]Session{
			{SessionID: "alpha", Status: SessionStatusConnected},
			{SessionID: "beta", Status: SessionStatusQRReady},
			{SessionID: "gamma", Status: SessionStatusError},
		},
		messageCounts{
			total:    18,
			inbound:  11,
			outbound: 7,
		},
		3,
	)

	if totals.TotalSessions != 3 {
		t.Fatalf("expected 3 total sessions, got %d", totals.TotalSessions)
	}
	if totals.ConnectedSessions != 1 || totals.PairingSessions != 1 || totals.DisconnectedSessions != 1 {
		t.Fatalf("unexpected session totals: %+v", totals)
	}
	if totals.Messages24h != 18 || totals.Inbound24h != 11 || totals.Outbound24h != 7 || totals.ActiveWebhooks != 3 {
		t.Fatalf("unexpected message totals: %+v", totals)
	}
}

func TestBuildDailySeries(t *testing.T) {
	start := time.Date(2026, 3, 19, 0, 0, 0, 0, time.UTC)
	series := buildDailySeries(start, 4, []dailyCountRow{
		{
			day:      time.Date(2026, 3, 19, 15, 0, 0, 0, time.UTC),
			total:    2,
			inbound:  1,
			outbound: 1,
		},
		{
			day:      time.Date(2026, 3, 21, 4, 0, 0, 0, time.UTC),
			total:    5,
			inbound:  3,
			outbound: 2,
		},
	})

	if len(series) != 4 {
		t.Fatalf("expected 4 series items, got %d", len(series))
	}
	if series[1].Date != "2026-03-20" || series[1].TotalMessages != 0 {
		t.Fatalf("expected empty 2026-03-20 bucket, got %+v", series[1])
	}
	if series[2].Date != "2026-03-21" || series[2].TotalMessages != 5 {
		t.Fatalf("unexpected 2026-03-21 bucket: %+v", series[2])
	}
}

func TestNormalizeAnalyticsRange(t *testing.T) {
	if normalizeAnalyticsRange("30d") != 30 {
		t.Fatalf("expected 30d to normalize to 30")
	}
	if normalizeAnalyticsRange("") != 7 {
		t.Fatalf("expected default range to normalize to 7")
	}
}
