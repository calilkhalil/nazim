// Package service defines the service data structure and validation.
package service

import (
	"fmt"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Duration is a custom type for serializing time.Duration in YAML.
type Duration struct {
	time.Duration
}

// MarshalYAML serializes Duration to YAML.
func (d Duration) MarshalYAML() (interface{}, error) {
	if d.Duration == 0 {
		return nil, nil
	}
	return formatDuration(d.Duration), nil
}

// UnmarshalYAML deserializes Duration from YAML.
func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	if value == nil || value.Value == "" {
		d.Duration = 0
		return nil
	}
	dur, err := parseDuration(value.Value)
	if err != nil {
		return err
	}
	d.Duration = dur
	return nil
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

func parseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	
	var multiplier time.Duration
	switch {
	case strings.HasSuffix(s, "s"):
		multiplier = time.Second
		s = strings.TrimSuffix(s, "s")
	case strings.HasSuffix(s, "m"):
		multiplier = time.Minute
		s = strings.TrimSuffix(s, "m")
	case strings.HasSuffix(s, "h"):
		multiplier = time.Hour
		s = strings.TrimSuffix(s, "h")
	case strings.HasSuffix(s, "d"):
		multiplier = 24 * time.Hour
		s = strings.TrimSuffix(s, "d")
	default:
		return 0, fmt.Errorf("invalid duration suffix, use s, m, h, or d")
	}

	var value int
	if _, err := fmt.Sscanf(s, "%d", &value); err != nil {
		return 0, fmt.Errorf("invalid duration value: %w", err)
	}

	return time.Duration(value) * multiplier, nil
}

// Service represents a service managed by Nazim.
type Service struct {
	Name      string   `yaml:"name"`
	Command   string   `yaml:"command"`
	Args      []string `yaml:"args,omitempty"`
	WorkDir   string   `yaml:"workdir,omitempty"`
	OnStartup bool     `yaml:"on_startup,omitempty"`
	Interval  Duration `yaml:"interval,omitempty"`
	Enabled   bool     `yaml:"enabled"`
	Platform  string   `yaml:"platform,omitempty"` // windows, linux, darwin
}

// Validate validates if the service is configured correctly.
func (s *Service) Validate() error {
	if s.Name == "" {
		return fmt.Errorf("service name is required")
	}
	if s.Command == "" {
		return fmt.Errorf("service command is required")
	}
	if !s.OnStartup && s.Interval.Duration == 0 {
		return fmt.Errorf("service must have either on_startup=true or an interval")
	}
	return nil
}

// RequiresScheduling returns true if the service needs scheduling.
func (s *Service) RequiresScheduling() bool {
	return s.Interval.Duration > 0
}

// GetInterval returns the interval as time.Duration.
func (s *Service) GetInterval() time.Duration {
	return s.Interval.Duration
}

// RequiresStartup returns true if the service should run on startup.
func (s *Service) RequiresStartup() bool {
	return s.OnStartup
}

