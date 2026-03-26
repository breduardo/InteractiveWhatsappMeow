package whatsapp

import (
	"fmt"

	"github.com/rs/zerolog"
	waLog "go.mau.fi/whatsmeow/util/log"
)

type Logger struct {
	logger zerolog.Logger
}

func NewLogger(logger zerolog.Logger) waLog.Logger {
	return &Logger{logger: logger}
}

func (l *Logger) Warnf(msg string, args ...interface{}) {
	l.logger.Warn().Msgf(msg, args...)
}

func (l *Logger) Errorf(msg string, args ...interface{}) {
	l.logger.Error().Msgf(msg, args...)
}

func (l *Logger) Infof(msg string, args ...interface{}) {
	l.logger.Info().Msgf(msg, args...)
}

func (l *Logger) Debugf(msg string, args ...interface{}) {
	l.logger.Debug().Msgf(msg, args...)
}

func (l *Logger) Sub(module string) waLog.Logger {
	return &Logger{logger: l.logger.With().Str("module", module).Logger()}
}

var _ waLog.Logger = (*Logger)(nil)

func format(msg string, args ...interface{}) string {
	if len(args) == 0 {
		return msg
	}
	return fmt.Sprintf(msg, args...)
}
