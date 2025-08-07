package conf

import (
	"errors"
	"fmt"
	"runtime"
	"strings"

	"github.com/go-playground/validator/v10"
	"github.com/spf13/viper"
)

type Config struct {
	HealthCheckPort int    `mapstructure:"health_port"`
	Hosts           []Host `mapstructure:"hosts"`
}

type Host struct {
	Name   string    `mapstructure:"name"`
	Addr   string    `mapstructure:"address"`
	Method LogMethod `mapstructure:"method"`
}

func NewConfig() (*Config, error) {
	v := viper.New()
	v.SetConfigName("config")
	v.SetConfigType("yaml")
	v.AddConfigPath(".")
	v.SetDefault("health_port", 8080)
	v.SetDefault("hosts.name", "localhost")
	v.SetDefault("hosts.address", "unix:///var/run/docker.sock")
	v.SetDefault("hosts.method", "socket")
	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			fmt.Println("Config file not found, using defaults or environment variables.")
		} else {
			return nil, err
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %s", err.Error())
	}
	validate := validator.New()
	if err := validate.Struct(cfg); err != nil {
		var sb strings.Builder
		for _, err := range err.(validator.ValidationErrors) {
			sb.WriteString(fmt.Sprintf("Field '%s' failed on '%s'\n", err.Field(), err.Tag()))
		}
		return nil, errors.New(sb.String())
	}
	for _, h := range cfg.Hosts {
		if h.Method == File && runtime.GOOS == "darwin" {
			return nil, errors.New("invalid method for logs in your os")
		}
	}

	return &cfg, nil
}
