package conf

import (
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewConfig_Defaults(t *testing.T) {
	// Test with no config file present
	cfg, err := NewConfig()
	require.NoError(t, err)
	assert.NotNil(t, cfg)
	assert.Equal(t, 8080, cfg.HealthCheckPort)
}

func TestNewConfig_WithValidTimeout(t *testing.T) {
	// Create a temporary config file
	configContent := `
health_port: 9090
timeout: "5m"
hosts:
  - name: "test-host"
    address: "unix:///var/run/docker.sock"
    method: "socket"
`
	err := os.WriteFile("config.yaml", []byte(configContent), 0644)
	require.NoError(t, err)
	defer os.Remove("config.yaml")

	cfg, err := NewConfig()
	require.NoError(t, err)
	assert.Equal(t, 9090, cfg.HealthCheckPort)
	assert.Equal(t, "5m", cfg.Timeout)
	assert.Equal(t, 5*time.Minute, cfg.TimeoutDuration)
	assert.Len(t, cfg.Hosts, 1)
	assert.Equal(t, "test-host", cfg.Hosts[0].Name)
	assert.Equal(t, Socket, cfg.Hosts[0].Method)
}

func TestNewConfig_InvalidTimeout(t *testing.T) {
	configContent := `
timeout: "invalid"
`
	err := os.WriteFile("config.yaml", []byte(configContent), 0644)
	require.NoError(t, err)
	defer os.Remove("config.yaml")

	_, err = NewConfig()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid timeout")
}

func TestNewConfig_TimeoutWithoutValidSuffix(t *testing.T) {
	configContent := `
timeout: "5"
`
	err := os.WriteFile("config.yaml", []byte(configContent), 0644)
	require.NoError(t, err)
	defer os.Remove("config.yaml")

	_, err = NewConfig()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid timeout")
}

func TestNewConfig_WithRunSchedule(t *testing.T) {
	configContent := `
run_schedule:
  start: "2024/01/01 09:00"
  end: "2024/01/01 17:00"
`
	err := os.WriteFile("config.yaml", []byte(configContent), 0644)
	require.NoError(t, err)
	defer os.Remove("config.yaml")

	_, err = NewConfig()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to unmarshal config")
}

func TestNewConfig_InvalidStartTime(t *testing.T) {
	configContent := `
run_schedule:
  start: "invalid-time"
  end: "2024/01/01 17:00"
`
	err := os.WriteFile("config.yaml", []byte(configContent), 0644)
	require.NoError(t, err)
	defer os.Remove("config.yaml")

	_, err = NewConfig()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to unmarshal config")
}

func TestNewConfig_InvalidEndTime(t *testing.T) {
	configContent := `
run_schedule:
  start: "2024/01/01 09:00"
  end: "invalid-time"
`
	err := os.WriteFile("config.yaml", []byte(configContent), 0644)
	require.NoError(t, err)
	defer os.Remove("config.yaml")

	_, err = NewConfig()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to unmarshal config")
}

func TestNewConfig_FileMethodOnDarwin(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("This test only runs on Darwin")
	}

	configContent := `
hosts:
  - name: "test-host"
    address: "/var/log/containers"
    method: "file"
`
	err := os.WriteFile("config.yaml", []byte(configContent), 0644)
	require.NoError(t, err)
	defer os.Remove("config.yaml")

	_, err = NewConfig()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid method for logs in your os")
}

func TestNewConfig_FileMethodOnNonDarwin(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("This test only runs on non-Darwin systems")
	}

	configContent := `
hosts:
  - name: "test-host"
    address: "/var/log/containers"
    method: "file"
`
	err := os.WriteFile("config.yaml", []byte(configContent), 0644)
	require.NoError(t, err)
	defer os.Remove("config.yaml")

	cfg, err := NewConfig()
	require.NoError(t, err)
	assert.Len(t, cfg.Hosts, 1)
	assert.Equal(t, File, cfg.Hosts[0].Method)
}

func TestNewConfig_ValidTimeoutFormats(t *testing.T) {
	testCases := []struct {
		timeout  string
		expected time.Duration
	}{
		{"1h", time.Hour},
		{"30m", 30 * time.Minute},
		{"5s", 5 * time.Second},
		{"2h30m", 2*time.Hour + 30*time.Minute},
	}

	for _, tc := range testCases {
		t.Run(tc.timeout, func(t *testing.T) {
			configContent := `timeout: "` + tc.timeout + `"`
			err := os.WriteFile("config.yaml", []byte(configContent), 0644)
			require.NoError(t, err)
			defer os.Remove("config.yaml")

			cfg, err := NewConfig()
			require.NoError(t, err)
			assert.Equal(t, tc.expected, cfg.TimeoutDuration)
		})
	}
}

func TestNewConfig_MultipleHosts(t *testing.T) {
	configContent := `
hosts:
  - name: "host1"
    address: "unix:///var/run/docker.sock"
    method: "socket"
  - name: "host2"
    address: "tcp://localhost:2376"
    method: "socket"
`
	err := os.WriteFile("config.yaml", []byte(configContent), 0644)
	require.NoError(t, err)
	defer os.Remove("config.yaml")

	cfg, err := NewConfig()
	require.NoError(t, err)
	assert.Len(t, cfg.Hosts, 2)
	assert.Equal(t, "host1", cfg.Hosts[0].Name)
	assert.Equal(t, "host2", cfg.Hosts[1].Name)
}

func TestNewConfig_InvalidYAML(t *testing.T) {
	configContent := `
invalid: yaml: content:
  - malformed
`
	err := os.WriteFile("config.yaml", []byte(configContent), 0644)
	require.NoError(t, err)
	defer os.Remove("config.yaml")

	_, err = NewConfig()
	assert.Error(t, err)
}
