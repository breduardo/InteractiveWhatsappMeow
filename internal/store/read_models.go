package store

import "time"

type DashboardSummary struct {
	Totals         DashboardTotals     `json:"totals"`
	Sessions       []Session           `json:"sessions"`
	RecentActivity []DashboardActivity `json:"recentActivity"`
}

type DashboardTotals struct {
	TotalSessions        int `json:"totalSessions"`
	ConnectedSessions    int `json:"connectedSessions"`
	PairingSessions      int `json:"pairingSessions"`
	DisconnectedSessions int `json:"disconnectedSessions"`
	Messages24h          int `json:"messages24h"`
	Inbound24h           int `json:"inbound24h"`
	Outbound24h          int `json:"outbound24h"`
	ActiveWebhooks       int `json:"activeWebhooks"`
}

type DashboardActivity struct {
	SessionID        string           `json:"sessionId"`
	MessageID        string           `json:"messageId"`
	ChatJID          string           `json:"chatJid"`
	Direction        MessageDirection `json:"direction"`
	MessageType      string           `json:"messageType"`
	Text             string           `json:"text"`
	Status           MessageStatus    `json:"status"`
	MessageTimestamp time.Time        `json:"messageTimestamp"`
}

type ChatSummary struct {
	SessionID       string           `json:"sessionId"`
	ChatJID         string           `json:"chatJid"`
	LastMessageID   string           `json:"lastMessageId"`
	LastMessageText string           `json:"lastMessageText"`
	LastMessageType string           `json:"lastMessageType"`
	LastMessageAt   time.Time        `json:"lastMessageAt"`
	LastDirection   MessageDirection `json:"lastDirection"`
	MessageCount    int              `json:"messageCount"`
}

type AnalyticsSummaryInput struct {
	SessionID *string
	Range     string
}

type AnalyticsSummary struct {
	Range            string             `json:"range"`
	Totals           AnalyticsTotals    `json:"totals"`
	DailySeries      []AnalyticsDaily   `json:"dailySeries"`
	TopChats         []AnalyticsChat    `json:"topChats"`
	SessionBreakdown []SessionBreakdown `json:"sessionBreakdown"`
}

type AnalyticsTotals struct {
	TotalMessages    int `json:"totalMessages"`
	InboundMessages  int `json:"inboundMessages"`
	OutboundMessages int `json:"outboundMessages"`
	ActiveChats      int `json:"activeChats"`
	ActiveSessions   int `json:"activeSessions"`
}

type AnalyticsDaily struct {
	Date             string `json:"date"`
	TotalMessages    int    `json:"totalMessages"`
	InboundMessages  int    `json:"inboundMessages"`
	OutboundMessages int    `json:"outboundMessages"`
}

type AnalyticsChat struct {
	SessionID       string    `json:"sessionId"`
	ChatJID         string    `json:"chatJid"`
	LastMessageText string    `json:"lastMessageText"`
	LastMessageAt   time.Time `json:"lastMessageAt"`
	MessageCount    int       `json:"messageCount"`
}

type SessionBreakdown struct {
	SessionID        string        `json:"sessionId"`
	Name             string        `json:"name"`
	Status           SessionStatus `json:"status"`
	TotalMessages    int           `json:"totalMessages"`
	InboundMessages  int           `json:"inboundMessages"`
	OutboundMessages int           `json:"outboundMessages"`
	LastMessageAt    *time.Time    `json:"lastMessageAt,omitempty"`
}
