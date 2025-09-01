package conf

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/spf13/viper"
)
// TODO save logs and stats in same dir
// TODO: merge a same docker swarm service containers logs
type Config struct {
	HealthCheckPort int    `mapstructure:"health_port"`
	Timeout         string `mapstructure:"timeout"`
	TimeoutDuration time.Duration
	RunSchedule     struct {
		Start string `mapstructure:"start"`
		End   string `mapstructure:"end"`
	} `mapstructure:"run_schedule"`
	RunScheduleTime RunSchedule `mapstructure:"run_schedule"`
	Hosts           []Host      `mapstructure:"hosts"`
	StatInterval    int         `mapstructure:"stat_interval"`
}
type RunSchedule struct {
	Start time.Duration
	End   time.Duration
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
	v.SetDefault("hosts.stat_interval", 30)
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
	if cfg.Timeout != "" {
		// check if allowed spans like day (d) , hours (h) or minutes (m) exist in timeout
		if !strings.ContainsAny(cfg.Timeout, "dhms") {
			return nil, errors.New("invalid timeout")
		}
		dur, err := time.ParseDuration(cfg.Timeout)
		if err != nil {
			return nil, errors.New("invalid timeout")
		}
		cfg.TimeoutDuration = dur
	}

	if cfg.RunSchedule.Start != "" {
		startTime, err := time.Parse("2006/01/02 15:04", cfg.RunSchedule.Start)
		if err != nil {
			return nil, fmt.Errorf("invalid start time: %v", err)
		}

		cfg.RunScheduleTime.Start = time.Duration(startTime.Hour())*time.Hour +
			time.Duration(startTime.Minute())*time.Minute

		endTime, err := time.Parse("2006/01/02 15:04", cfg.RunSchedule.End)
		if err != nil {
			return nil, fmt.Errorf("invalid end time: %v", err)
		}

		cfg.RunScheduleTime.End = time.Duration(endTime.Hour())*time.Hour +
			time.Duration(endTime.Minute())*time.Minute
	}

	return &cfg, nil
}
