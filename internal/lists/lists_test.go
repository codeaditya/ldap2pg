package lists_test

import (
	"log/slog"
	"testing"

	"github.com/dalibo/ldap2pg/v6/internal"

	"github.com/stretchr/testify/suite"
)

// Global test suite for lists package.
type Suite struct {
	suite.Suite
}

func Test(t *testing.T) {
	if testing.Verbose() {
		internal.SetLoggingHandler(slog.LevelDebug, false)
	} else {
		internal.SetLoggingHandler(slog.LevelWarn, false)
	}
	suite.Run(t, new(Suite))
}
