package conf

import (
	"fmt"

	"github.com/spf13/viper"
)

type Config struct {
	HealthCheckPort int `mapstructure:"health_port"`
}

func NewConfig() (*Config, error) {
	v := viper.New()
	v.SetConfigName("config")
	v.SetConfigType("yaml")
	v.AddConfigPath(".")
	v.SetDefault("health_port", 8080)
	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("failed to read config: %s", err.Error())
	}
	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %s", err.Error())
	}
	return &cfg, nil
}
