package session

import (
	"context"

	waSQLStore "go.mau.fi/whatsmeow/store/sqlstore"
	waLog "go.mau.fi/whatsmeow/util/log"
)

func NewWAContainer(ctx context.Context, databaseURL string, logger waLog.Logger) (*waSQLStore.Container, error) {
	return waSQLStore.New(ctx, "postgres", databaseURL, logger)
}
