package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	// Interfaces (optional; scripts can still have defaults)
	WANIF string `yaml:"wan_if"`
	LANIF string `yaml:"lan_if"`

	// GatewayUpPath   string `yaml:"gateway_up_path"`
	GatewayDownPath string `yaml:"gateway_down_path"`
	// Router scripts (new split)
	GatewaySetupPath   string `yaml:"gateway_setup_path"`
	GatewayPFApplyPath string `yaml:"gateway_pf_apply_path"`

	HealthCheckURL string        `yaml:"health_check_url"`
	CheckInterval  time.Duration `yaml:"check_interval"`
	CommandTimeout time.Duration `yaml:"command_timeout"`

	// transport control
	TransportAdoptExternal *bool         `yaml:"transport_adopt_external"`
	TransportPath          string        `yaml:"transport_path"`
	TransportConfigPath    string        `yaml:"transport_config_path"`
	TransportAutoStart     bool          `yaml:"transport_auto_start"`
	TransportAutoStop      bool          `yaml:"transport_auto_stop"`
	TransportStartTimeout  time.Duration `yaml:"transport_start_timeout"`
	TransportStopTimeout   time.Duration `yaml:"transport_stop_timeout"`
	TransportPidFile       string        `yaml:"transport_pid_file"`
	TransportLogFile       string        `yaml:"transport_log_file"`

	// Watchdog
	FailureThreshold int           `yaml:"failure_threshold"`
	RecoverCooldown  time.Duration `yaml:"recover_cooldown"`
	MaxRecoveries    int           `yaml:"max_recoveries"`
	HealthTimeout    time.Duration `yaml:"health_timeout"`

	// Fail-closed allowlists (planned)
	TransportServerIPs []string `yaml:"transport_server_ips"`
	WANDNSIPs          []string `yaml:"wan_dns_ips"`   // optional
	AllowWANNTP        bool     `yaml:"allow_wan_ntp"` // optional
}

// Default config.yaml path: ~/xrouter/config/xrouter/config.yaml
func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home dir: %w", err)
	}
	return filepath.Join(home, "xrouter", "config", "xrouter", "config.yaml"), nil
}

func Load(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %q: %w", path, err)
	}

	var c Config
	if err := yaml.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("parse yaml %q: %w", path, err)
	}

	applyDefaults(&c)

	if err := validate(&c); err != nil {
		return nil, err
	}

	return &c, nil
}

func applyDefaults(c *Config) {
	if c.HealthCheckURL == "" {
		c.HealthCheckURL = "https://api.ipify.org?format=text"
	}
	if c.CheckInterval == 0 {
		c.CheckInterval = 10 * time.Second
	}
	if c.CommandTimeout == 0 {
		c.CommandTimeout = 20 * time.Second
	}

	// transport defaults
	if c.TransportPath == "" {
		// Default current TUN transport backend path.
		c.TransportPath = "/usr/local/bin/sing-box"
	}
	if c.TransportStartTimeout == 0 {
		c.TransportStartTimeout = 8 * time.Second
	}
	if c.TransportStopTimeout == 0 {
		c.TransportStopTimeout = 8 * time.Second
	}
	// /path/to/runtime/transport.pid
	// /path/to/runtime/transport.log
	if c.TransportPidFile == "" || c.TransportLogFile == "" {
		home, _ := os.UserHomeDir()
		if c.TransportPidFile == "" {
			c.TransportPidFile = filepath.Join(home, "xrouter", "config", "xrouter", "transport.pid")
		}
		if c.TransportLogFile == "" {
			c.TransportLogFile = filepath.Join(home, "xrouter", "config", "xrouter", "transport.log")
		}
	}
	if c.TransportAdoptExternal == nil {
		v := true
		c.TransportAdoptExternal = &v
	}

	// Watchdog
	if c.FailureThreshold == 0 {
		c.FailureThreshold = 3
	}
	if c.RecoverCooldown == 0 {
		c.RecoverCooldown = 5 * time.Second
	}
	if c.MaxRecoveries == 0 {
		c.MaxRecoveries = 5
	}
	if c.HealthTimeout == 0 {
		c.HealthTimeout = 5 * time.Second
	}

}

func (c *Config) AdoptExternal() bool {
	if c.TransportAdoptExternal == nil {
		return true
	}
	return *c.TransportAdoptExternal
}

func validate(c *Config) error {
	var problems []string

	if c.TransportAutoStart {
		if c.TransportPath == "" {
			problems = append(problems, "transport_path is required when transport_auto_start=true")
		}
		if c.TransportConfigPath == "" {
			problems = append(problems, "transport_config_path is required when transport_auto_start=true")
		}
		// Policy B: adoption may be used even when auto-start is disabled.
		if c.AdoptExternal() && strings.TrimSpace(c.TransportConfigPath) == "" {
			problems = append(problems, "transport_config_path is required when transport_adopt_external=true (needed to adopt external process)")
		}
		if c.TransportStartTimeout < 1*time.Second {
			problems = append(problems, "transport_start_timeout must be >= 1s")
		}
	}

	// if c.GatewayUpPath == "" {
	//	problems = append(problems, "gateway_up_path is required")
	// }
	// if c.GatewayDownPath == "" {
	//	problems = append(problems, "gateway_down_path is required")
	// }
	// Scripts: required + must exist + must be executable
	if strings.TrimSpace(c.GatewaySetupPath) == "" {
		problems = append(problems, "gateway_setup_path is required")
	} else if err := mustBeExecutableFile(c.GatewaySetupPath); err != nil {
		problems = append(problems, fmt.Sprintf("gateway_setup_path invalid: %v", err))
	}
	if strings.TrimSpace(c.GatewayPFApplyPath) == "" {
		problems = append(problems, "gateway_pf_apply_path is required")
	} else if err := mustBeExecutableFile(c.GatewayPFApplyPath); err != nil {
		problems = append(problems, fmt.Sprintf("gateway_pf_apply_path invalid: %v", err))
	}
	if c.CheckInterval < 1*time.Second {
		problems = append(problems, "check_interval must be >= 1s")
	}
	if c.CommandTimeout < 1*time.Second {
		problems = append(problems, "command_timeout must be >= 1s")
	}

	if len(problems) > 0 {
		return errors.New("config invalid: " + joinProblems(problems))
	}
	return nil
}

func mustBeExecutableFile(path string) error {
	fi, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("%q not accessible: %w", path, err)
	}
	if fi.IsDir() {
		return fmt.Errorf("%q is a directory (expected a file)", path)
	}
	// Require at least one execute bit (owner/group/other)
	if fi.Mode()&0o111 == 0 {
		return fmt.Errorf("%q is not executable (run: chmod +x %s)", path, path)
	}
	return nil
}

func joinProblems(p []string) string {
	if len(p) == 1 {
		return p[0]
	}
	out := ""
	for i, s := range p {
		if i > 0 {
			out += "; "
		}
		out += s
	}
	return out
}
