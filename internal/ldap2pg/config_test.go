package ldap2pg_test

import (
	"testing"

	"github.com/dalibo/ldap2pg/internal/ldap2pg"
	"github.com/stretchr/testify/suite"
	"go.uber.org/zap/zaptest"
)

type ConfigSuite struct {
	suite.Suite
}

func (suite *ConfigSuite) SetupSuite() {
	ldap2pg.Logger = zaptest.NewLogger(suite.T()).Sugar()
}

func (suite *ConfigSuite) TeardownSuite() {
	ldap2pg.Logger = nil
}

func (suite *ConfigSuite) TestLoadEnvDoesNotOverwriteConfigFile() {
	r := suite.Require()

	config := ldap2pg.Config{
		ConfigFile: "defined-ldap2pg.yaml",
	}
	values := ldap2pg.EnvValues{
		ConfigFile: "env-ldap2pg.yaml",
	}
	config.LoadEnv(values)

	r.Equal(config.ConfigFile, "defined-ldap2pg.yaml")
}

func TestConfig(t *testing.T) {
	suite.Run(t, new(ConfigSuite))
}
