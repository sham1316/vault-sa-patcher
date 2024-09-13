package config

import (
	"encoding/json"
	"flag"
	configParser "github.com/sham1316/configparser"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"k8s.io/apimachinery/pkg/util/yaml"
	"os"
	"sync"
)

var config *Config
var once sync.Once
var configPath *string

type Password string

func (p Password) MarshalJSON() ([]byte, error) {
	if 0 == len(p) {
		return []byte(`""`), nil
	} else {
		return []byte(`"XXX"`), nil
	}
}

const (
	Annotation = "vault-sa-patcher"
)

type certificate string

func (c certificate) MarshalJSON() ([]byte, error) {
	if 0 == len(c) {
		return []byte(`""`), nil
	} else {
		return []byte(`"X509 cert"`), nil
	}

}

type Config struct {
	LogLevel         string   `default:"debug" env:"LOG_LEVEL"`
	InCluster        bool     `default:"true" env:"IN_CLUSTER"`
	TokenPath        string   `default:"/var/run/secrets/kubernetes.io/serviceaccount/token" env:"TOKEN_PATH"`
	Token            Password `default:"" env:"TOKEN"`
	Kubeconfig       string   `default:"" env:"KUBECONFIG"`
	Interval         int      `default:"300" env:"INTERVAL"`
	SecretNamePrefix string   `default:"image-poll-secret-from-vault-" env:"SECRET_NAME_PREFIX"`
	Vault            struct {
		VaultSchema string `default:"https" env:"VAULT_SCHEMA"`
		VaultPort   int    `default:"8200" env:"VAULT_PORT"`
		VaultRole   string `default:"image_pool_secret" env:"VAULT_ROLE"`
		VaultServer string `default:"vault-active" env:"VAULT_ACTIVE_SERVICE"`
		MountPath   string `default:"projects" env:"VAULT_MOUNT_PATH"`
		SecretPath  string `default:"share/docker/registries" env:"VAULT_SECRET_PATH"`
	}
	HTTP struct {
		ADDR        string `default:":8080" env:"HTTP_ADDR"`
		RoutePrefix string `default:"" env:"HTTP_ROUTE_PREFIX"`
	}
}

func GetCfg() *Config {
	once.Do(func() {
		configPath = flag.String("config", "config.yaml", "Configuration file path")
		flag.Parse()
		config = loadConfig(configPath)
		initZap(config)
		b, _ := json.Marshal(config) //nolint:errcheck
		zap.S().Debug(string(b))
	})
	return config
}

func initZap(config *Config) *zap.Logger {
	zapCfg := zap.NewProductionConfig()
	zapCfg.DisableStacktrace = true
	zapCfg.Encoding = "console"
	zapCfg.EncoderConfig = zap.NewDevelopmentEncoderConfig()
	logLevel, _ := zapcore.ParseLevel(config.LogLevel) //nolint:errcheck
	zapCfg.Level = zap.NewAtomicLevelAt(logLevel)
	zapLogger, _ := zapCfg.Build() //nolint:errcheck
	zap.ReplaceGlobals(zapLogger)
	return zapLogger
}
func loadConfig(configFile *string) *Config {
	config := Config{}
	_ = configParser.SetValue(&config, "default") //nolint:errcheck
	configYamlFile, _ := os.ReadFile(*configFile) //nolint:errcheck
	_ = yaml.Unmarshal(configYamlFile, &config)   //nolint:errcheck
	_ = configParser.SetValue(&config, "env")     //nolint:errcheck
	return &config
}
