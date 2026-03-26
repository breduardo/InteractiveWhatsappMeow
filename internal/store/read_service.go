package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

type ReadService struct {
	db       *sql.DB
	sessions *SessionRepository
}

func NewReadService(db *sql.DB, sessions *SessionRepository) *ReadService {
	return &ReadService{
		db:       db,
		sessions: sessions,
	}
}

func (s *ReadService) GetDashboardSummary(ctx context.Context) (*DashboardSummary, error) {
	sessions, err := s.sessions.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}

	counts, err := s.countRecentMessages(ctx, time.Now().UTC().Add(-24*time.Hour), nil)
	if err != nil {
		return nil, fmt.Errorf("count recent messages: %w", err)
	}

	activeWebhooks, err := s.countActiveWebhooks(ctx)
	if err != nil {
		return nil, fmt.Errorf("count active webhooks: %w", err)
	}

	activity, err := s.listRecentActivity(ctx, 12)
	if err != nil {
		return nil, fmt.Errorf("list recent activity: %w", err)
	}

	return &DashboardSummary{
		Totals:         buildDashboardTotals(sessions, counts, activeWebhooks),
		Sessions:       sessions,
		RecentActivity: activity,
	}, nil
}

func (s *ReadService) ListChats(ctx context.Context, sessionID string) ([]ChatSummary, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, fmt.Errorf("sessionId is required")
	}

	canonicalChatExpr := canonicalChatJIDExpr("chat_jid")

	rows, err := s.db.QueryContext(
		ctx,
		`WITH normalized AS (
			SELECT
				session_id,
				`+canonicalChatExpr+` AS canonical_chat_jid,
				external_message_id,
				COALESCE(NULLIF(text, ''), NULLIF(media_file_name, ''), '[' || message_type || ']') AS preview,
				message_type,
				direction,
				message_timestamp,
				id
			FROM messages
			WHERE session_id = $1
		)
		, ranked AS (
			SELECT
				session_id,
				canonical_chat_jid,
				external_message_id,
				preview,
				message_type,
				direction,
				message_timestamp,
				ROW_NUMBER() OVER (PARTITION BY session_id, canonical_chat_jid ORDER BY message_timestamp DESC, id DESC) AS row_num,
				COUNT(*) OVER (PARTITION BY session_id, canonical_chat_jid) AS message_count
			FROM normalized
		)
		SELECT session_id, canonical_chat_jid, external_message_id, preview, message_type, direction, message_timestamp, message_count
		FROM ranked
		WHERE row_num = 1
		ORDER BY message_timestamp DESC, canonical_chat_jid ASC`,
		sessionID,
	)
	if err != nil {
		return nil, fmt.Errorf("query chats: %w", err)
	}
	defer rows.Close()

	chats := make([]ChatSummary, 0)
	for rows.Next() {
		var chat ChatSummary
		if err := rows.Scan(
			&chat.SessionID,
			&chat.ChatJID,
			&chat.LastMessageID,
			&chat.LastMessageText,
			&chat.LastMessageType,
			&chat.LastDirection,
			&chat.LastMessageAt,
			&chat.MessageCount,
		); err != nil {
			return nil, fmt.Errorf("scan chat summary: %w", err)
		}
		chats = append(chats, chat)
	}

	return chats, rows.Err()
}

func (s *ReadService) GetAnalyticsSummary(ctx context.Context, input AnalyticsSummaryInput) (*AnalyticsSummary, error) {
	dayCount := normalizeAnalyticsRange(input.Range)
	now := time.Now().UTC()
	rangeStart := startOfUTCDay(now).AddDate(0, 0, -(dayCount - 1))

	totals, err := s.countRecentMessages(ctx, rangeStart, input.SessionID)
	if err != nil {
		return nil, fmt.Errorf("count analytics totals: %w", err)
	}

	dailyRows, err := s.listDailyCounts(ctx, rangeStart, input.SessionID)
	if err != nil {
		return nil, fmt.Errorf("list daily counts: %w", err)
	}

	topChats, err := s.listTopChats(ctx, rangeStart, input.SessionID, 8)
	if err != nil {
		return nil, fmt.Errorf("list top chats: %w", err)
	}

	sessionBreakdown, err := s.listSessionBreakdown(ctx, rangeStart, input.SessionID)
	if err != nil {
		return nil, fmt.Errorf("list session breakdown: %w", err)
	}

	return &AnalyticsSummary{
		Range:            fmt.Sprintf("%dd", dayCount),
		Totals:           buildAnalyticsTotals(totals),
		DailySeries:      buildDailySeries(rangeStart, dayCount, dailyRows),
		TopChats:         topChats,
		SessionBreakdown: sessionBreakdown,
	}, nil
}

type messageCounts struct {
	total          int
	inbound        int
	outbound       int
	activeChats    int
	activeSessions int
}

type dailyCountRow struct {
	day      time.Time
	total    int
	inbound  int
	outbound int
}

func (s *ReadService) countRecentMessages(ctx context.Context, since time.Time, sessionID *string) (messageCounts, error) {
	canonicalChatExpr := canonicalChatJIDExpr("chat_jid")
	var row *sql.Row
	if sessionID != nil && strings.TrimSpace(*sessionID) != "" {
		row = s.db.QueryRowContext(
			ctx,
			`SELECT
				COUNT(*)::int,
				COALESCE(SUM(CASE WHEN direction = 'inbound' THEN 1 ELSE 0 END), 0)::int,
				COALESCE(SUM(CASE WHEN direction = 'outbound' THEN 1 ELSE 0 END), 0)::int,
				COUNT(DISTINCT `+canonicalChatExpr+`)::int,
				COUNT(DISTINCT session_id)::int
			FROM messages
			WHERE message_timestamp >= $1 AND session_id = $2`,
			since,
			strings.TrimSpace(*sessionID),
		)
	} else {
		row = s.db.QueryRowContext(
			ctx,
			`SELECT
				COUNT(*)::int,
				COALESCE(SUM(CASE WHEN direction = 'inbound' THEN 1 ELSE 0 END), 0)::int,
				COALESCE(SUM(CASE WHEN direction = 'outbound' THEN 1 ELSE 0 END), 0)::int,
				COUNT(DISTINCT `+canonicalChatExpr+`)::int,
				COUNT(DISTINCT session_id)::int
			FROM messages
			WHERE message_timestamp >= $1`,
			since,
		)
	}

	var counts messageCounts
	if err := row.Scan(
		&counts.total,
		&counts.inbound,
		&counts.outbound,
		&counts.activeChats,
		&counts.activeSessions,
	); err != nil {
		return messageCounts{}, err
	}

	return counts, nil
}

func (s *ReadService) countActiveWebhooks(ctx context.Context) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*)::int FROM webhooks WHERE is_active = TRUE`).Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
}

func (s *ReadService) listRecentActivity(ctx context.Context, limit int) ([]DashboardActivity, error) {
	if limit <= 0 {
		limit = 12
	}

	rows, err := s.db.QueryContext(
		ctx,
		`SELECT
			session_id,
			external_message_id,
			chat_jid,
			direction,
			message_type,
			COALESCE(NULLIF(text, ''), NULLIF(media_file_name, ''), '[' || message_type || ']') AS preview,
			status,
			message_timestamp
		FROM messages
		ORDER BY message_timestamp DESC, id DESC
		LIMIT $1`,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query recent activity: %w", err)
	}
	defer rows.Close()

	activity := make([]DashboardActivity, 0)
	for rows.Next() {
		var item DashboardActivity
		if err := rows.Scan(
			&item.SessionID,
			&item.MessageID,
			&item.ChatJID,
			&item.Direction,
			&item.MessageType,
			&item.Text,
			&item.Status,
			&item.MessageTimestamp,
		); err != nil {
			return nil, fmt.Errorf("scan recent activity: %w", err)
		}
		activity = append(activity, item)
	}

	return activity, rows.Err()
}

func (s *ReadService) listDailyCounts(ctx context.Context, since time.Time, sessionID *string) ([]dailyCountRow, error) {
	baseQuery := `SELECT
		date_trunc('day', message_timestamp AT TIME ZONE 'UTC') AS day,
		COUNT(*)::int,
		COALESCE(SUM(CASE WHEN direction = 'inbound' THEN 1 ELSE 0 END), 0)::int,
		COALESCE(SUM(CASE WHEN direction = 'outbound' THEN 1 ELSE 0 END), 0)::int
	FROM messages
	WHERE message_timestamp >= $1`

	var (
		rows *sql.Rows
		err  error
	)

	if sessionID != nil && strings.TrimSpace(*sessionID) != "" {
		rows, err = s.db.QueryContext(
			ctx,
			baseQuery+` AND session_id = $2 GROUP BY 1 ORDER BY 1 ASC`,
			since,
			strings.TrimSpace(*sessionID),
		)
	} else {
		rows, err = s.db.QueryContext(ctx, baseQuery+` GROUP BY 1 ORDER BY 1 ASC`, since)
	}
	if err != nil {
		return nil, fmt.Errorf("query daily counts: %w", err)
	}
	defer rows.Close()

	days := make([]dailyCountRow, 0)
	for rows.Next() {
		var row dailyCountRow
		if err := rows.Scan(&row.day, &row.total, &row.inbound, &row.outbound); err != nil {
			return nil, fmt.Errorf("scan daily counts: %w", err)
		}
		days = append(days, row)
	}

	return days, rows.Err()
}

func (s *ReadService) listTopChats(ctx context.Context, since time.Time, sessionID *string, limit int) ([]AnalyticsChat, error) {
	if limit <= 0 {
		limit = 8
	}

	query := `WITH ranked AS (
		SELECT
			session_id,
			chat_jid,
			COALESCE(NULLIF(text, ''), NULLIF(media_file_name, ''), '[' || message_type || ']') AS preview,
			message_timestamp,
			ROW_NUMBER() OVER (PARTITION BY session_id, chat_jid ORDER BY message_timestamp DESC, id DESC) AS row_num,
			COUNT(*) OVER (PARTITION BY session_id, chat_jid) AS message_count
		FROM messages
		WHERE message_timestamp >= $1`

	args := []interface{}{since}
	if sessionID != nil && strings.TrimSpace(*sessionID) != "" {
		query += ` AND session_id = $2`
		args = append(args, strings.TrimSpace(*sessionID))
	}

	limitPlaceholder := fmt.Sprintf("$%d", len(args)+1)
	query += `
	)
	SELECT session_id, chat_jid, preview, message_timestamp, message_count
	FROM ranked
	WHERE row_num = 1
	ORDER BY message_count DESC, message_timestamp DESC
	LIMIT ` + limitPlaceholder
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query top chats: %w", err)
	}
	defer rows.Close()

	chats := make([]AnalyticsChat, 0)
	for rows.Next() {
		var chat AnalyticsChat
		if err := rows.Scan(
			&chat.SessionID,
			&chat.ChatJID,
			&chat.LastMessageText,
			&chat.LastMessageAt,
			&chat.MessageCount,
		); err != nil {
			return nil, fmt.Errorf("scan top chats: %w", err)
		}
		chats = append(chats, chat)
	}

	return chats, rows.Err()
}

func (s *ReadService) listSessionBreakdown(ctx context.Context, since time.Time, sessionID *string) ([]SessionBreakdown, error) {
	query := `SELECT
		m.session_id,
		COALESCE(ses.name, ''),
		COALESCE(ses.status, ''),
		COUNT(*)::int,
		COALESCE(SUM(CASE WHEN m.direction = 'inbound' THEN 1 ELSE 0 END), 0)::int,
		COALESCE(SUM(CASE WHEN m.direction = 'outbound' THEN 1 ELSE 0 END), 0)::int,
		MAX(m.message_timestamp)
	FROM messages m
	LEFT JOIN sessions ses ON ses.session_id = m.session_id
	WHERE m.message_timestamp >= $1`

	args := []interface{}{since}
	if sessionID != nil && strings.TrimSpace(*sessionID) != "" {
		query += ` AND m.session_id = $2`
		args = append(args, strings.TrimSpace(*sessionID))
	}

	query += ` GROUP BY m.session_id, ses.name, ses.status
	ORDER BY COUNT(*) DESC, m.session_id ASC`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query session breakdown: %w", err)
	}
	defer rows.Close()

	items := make([]SessionBreakdown, 0)
	for rows.Next() {
		var item SessionBreakdown
		var status string
		var lastMessageAt sql.NullTime
		if err := rows.Scan(
			&item.SessionID,
			&item.Name,
			&status,
			&item.TotalMessages,
			&item.InboundMessages,
			&item.OutboundMessages,
			&lastMessageAt,
		); err != nil {
			return nil, fmt.Errorf("scan session breakdown: %w", err)
		}
		item.Status = SessionStatus(status)
		if lastMessageAt.Valid {
			item.LastMessageAt = &lastMessageAt.Time
		}
		items = append(items, item)
	}

	return items, rows.Err()
}

func buildDashboardTotals(sessions []Session, counts messageCounts, activeWebhooks int) DashboardTotals {
	totals := DashboardTotals{
		TotalSessions:  len(sessions),
		Messages24h:    counts.total,
		Inbound24h:     counts.inbound,
		Outbound24h:    counts.outbound,
		ActiveWebhooks: activeWebhooks,
	}

	for _, current := range sessions {
		switch current.Status {
		case SessionStatusConnected:
			totals.ConnectedSessions++
		case SessionStatusInitializing, SessionStatusQRReady, SessionStatusPairing:
			totals.PairingSessions++
		case SessionStatusDisconnected, SessionStatusLoggedOut, SessionStatusError:
			totals.DisconnectedSessions++
		}
	}

	return totals
}

func buildAnalyticsTotals(counts messageCounts) AnalyticsTotals {
	return AnalyticsTotals{
		TotalMessages:    counts.total,
		InboundMessages:  counts.inbound,
		OutboundMessages: counts.outbound,
		ActiveChats:      counts.activeChats,
		ActiveSessions:   counts.activeSessions,
	}
}

func buildDailySeries(start time.Time, dayCount int, rows []dailyCountRow) []AnalyticsDaily {
	rowMap := make(map[string]dailyCountRow, len(rows))
	for _, row := range rows {
		key := startOfUTCDay(row.day).Format("2006-01-02")
		rowMap[key] = row
	}

	series := make([]AnalyticsDaily, 0, dayCount)
	for idx := 0; idx < dayCount; idx++ {
		currentDay := start.AddDate(0, 0, idx)
		key := currentDay.Format("2006-01-02")
		item := AnalyticsDaily{Date: key}
		if row, ok := rowMap[key]; ok {
			item.TotalMessages = row.total
			item.InboundMessages = row.inbound
			item.OutboundMessages = row.outbound
		}
		series = append(series, item)
	}

	return series
}

func normalizeAnalyticsRange(value string) int {
	switch strings.TrimSpace(value) {
	case "30d":
		return 30
	default:
		return 7
	}
}

func startOfUTCDay(value time.Time) time.Time {
	utc := value.UTC()
	return time.Date(utc.Year(), utc.Month(), utc.Day(), 0, 0, 0, 0, time.UTC)
}
