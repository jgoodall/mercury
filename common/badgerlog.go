package common

import (
	"strings"

	"github.com/rs/zerolog"
)

// BadgerLogger is a wrapper around zerolog for badger.
// Call it like this:
//   db, err := badger.Open(badger.DefaultOptions(dbPath).WithLogger(&common.BadgerLogger{Logger: log.Logger}))
type BadgerLogger struct {
	Logger zerolog.Logger
}

// Errorf will output if zerolog loglevel is set to Error.
func (b *BadgerLogger) Errorf(format string, a ...interface{}) {
	b.Logger.Error().Str("component", "badger").Msgf(strings.TrimSuffix(format, "\n"), a...)
}

// Warningf will output if zerolog loglevel is set to Warn.
func (b *BadgerLogger) Warningf(format string, a ...interface{}) {
	b.Logger.Warn().Str("component", "badger").Msgf(strings.TrimSuffix(format, "\n"), a...)
}

// Infof will only output if zerolog loglevel is set to Debug (badger
// is too chatty otherwise).
func (b *BadgerLogger) Infof(format string, a ...interface{}) {
	b.Logger.Debug().Str("component", "badger").Msgf(strings.TrimSuffix(format, "\n"), a...)
}

// Debugf will output if zerolog loglevel is set to Debug.
func (b *BadgerLogger) Debugf(format string, a ...interface{}) {
	b.Logger.Debug().Str("component", "badger").Msgf(strings.TrimSuffix(format, "\n"), a...)
}
