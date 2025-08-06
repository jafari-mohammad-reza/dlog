package conf

import (
	"fmt"

	"github.com/spf13/viper"
)

type Config struct {
	HealthCheckPort int    `mapstructure:"health_port"`
	Hosts           []Host `mapstructure:"hosts"`
}

type Host struct {
	Name string `mapstructure:"name"`
	Addr string `mapstructure:"address"`
}

func NewConfig() (*Config, error) {
	v := viper.New()
	v.SetConfigName("config")
	v.SetConfigType("yaml")
	v.AddConfigPath(".")
	v.SetDefault("health_port", 8080)
	v.SetDefault("hosts.name", "localhost")
	v.SetDefault("hosts.address", "unix:///var/run/docker.sock")
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
	return &cfg, nil
}
