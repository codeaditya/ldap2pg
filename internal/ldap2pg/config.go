package ldap2pg

import (
	"math"

	"github.com/kelseyhightower/envconfig"
	flag "github.com/spf13/pflag"
	"go.uber.org/zap/zapcore"
)

type Config struct {
	Action   CommandAction
	LogLevel zapcore.Level
	Ldap     struct {
		Uri      string
		BindDn   string
		Password string
	}
}

func LoadConfig() (self Config, err error) {
	self = Config{
		Action: RunAction,
		// Default to current LogLevel.
		LogLevel: LogLevel.Level(),
	}

	Logger.Debug("Loading Flag values.")
	flagValues := loadFlags()
	if flagValues.ShowHelp {
		self.Action = ShowHelpAction
		return
	}
	if flagValues.ShowVersion {
		self.Action = ShowVersionAction
		return
	}
	self.LoadFlags(flagValues)

	Logger.Debug("Loading Environment values.")
	var envValues EnvValues
	envconfig.MustProcess("", &envValues)
	self.LoadEnv(envValues)

	return self, nil
}

var levels []zapcore.Level = []zapcore.Level{
	zapcore.DebugLevel,
	zapcore.InfoLevel,
	zapcore.WarnLevel,
	zapcore.ErrorLevel,
	zapcore.FatalLevel,
}

func (self *Config) LoadFlags(values FlagValues) {
	change := 0 - values.Verbose + values.Quiet
	if change != 0 {
		var levelIndex int
		for i, level := range levels {
			if level == self.LogLevel {
				levelIndex = i
				break
			}
		}

		levelIndex = levelIndex + change
		levelIndex = int(math.Max(0, float64(levelIndex)))
		levelIndex = int(math.Min(float64(levelIndex), float64(len(levels)-1)))
		self.LogLevel = levels[levelIndex]
		Logger.Debugw("Setting log level.", "source", "flags", "level", self.LogLevel)
	}
}

func (self *Config) LoadEnv(values EnvValues) {
	Logger.Debugw("Setting LDAPURI.", "source", "env", "value", values.LdapUri)
	self.Ldap.Uri = values.LdapUri
	Logger.Debugw("Setting LDAPBINDDN.", "source", "env", "value", values.LdapBindDn)
	self.Ldap.BindDn = values.LdapBindDn
	Logger.Debugw("Setting LDAPPASSWORD.", "source", "env")
	self.Ldap.Password = values.LdapPassword
}

type EnvValues struct {
	LdapURI        string `envconfig:"LDAPURI"`
	LdapBindDn     string `envconfig:"LDAPBINDDN"`
	LdapPassword   string `envconfig:"LDAPPASSWORD"`
	LdapTLSReqcert string `envconfig:"LDAPTLS_REQCERT"`
	Dry            bool   `envconfig:"DRY" default:"true"`
}

type CommandAction int

const (
	RunAction CommandAction = iota
	ShowHelpAction
	ShowVersionAction
)

type FlagValues struct {
	Verbose     int
	Quiet       int
	Dry         bool
	ShowHelp    bool
	ShowVersion bool
	ConfigFile  string
}

func loadFlags() FlagValues {
	values := FlagValues{}
	flag.StringVarP(&values.ConfigFile, "config", "c", "", "Path to YAML configuration file.")
	flag.BoolVarP(&values.ShowHelp, "help", "?", false, "Show this help message and exit.")
	flag.BoolVarP(&values.ShowVersion, "version", "V", false, "Show version and exit.")
	flag.CountVarP(&values.Verbose, "verbose", "v", "Increase log verbosity.")
	flag.CountVarP(&values.Quiet, "quiet", "q", "Increase log verbosity.")
	flag.BoolVarP(&values.Dry, "dry", "n", true, "Don't touch Postgres, just print what to do.")
	flag.BoolVarP(&values.Dry, "real", "N", false, "Real mode, apply changes to Postgres instance.")
	flag.Parse()
	return values
}

func ShowHelp() {
	flag.Usage()
}
