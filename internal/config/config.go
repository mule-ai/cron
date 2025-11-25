package config

import (
	"fmt"
	"os"
	"sync"

	"gopkg.in/yaml.v3"
)

type WebhookConfig struct {
	URL                  string            `yaml:"url" json:"url"`
	Method               string            `yaml:"method" json:"method"`
	Headers              map[string]string `yaml:"headers,omitempty" json:"headers,omitempty"`
	Body                 string            `yaml:"body,omitempty" json:"body,omitempty"`
	JQSelectors          map[string]string `yaml:"jq_selectors,omitempty" json:"jq_selectors,omitempty"`
	BodyTemplate         string            `yaml:"body_template,omitempty" json:"body_template,omitempty"`
	OnlyIfVarsNonEmpty   bool              `yaml:"only_if_vars_non_empty,omitempty" json:"only_if_vars_non_empty,omitempty"`
}

type CronJob struct {
	ID          string         `yaml:"id" json:"id"`
	Name        string         `yaml:"name" json:"name"`
	Schedule    string         `yaml:"schedule" json:"schedule"`
	Enabled     bool           `yaml:"enabled" json:"enabled"`
	Primary     WebhookConfig  `yaml:"primary" json:"primary"`
	Secondary   *WebhookConfig `yaml:"secondary,omitempty" json:"secondary,omitempty"`
	SaveOutput  bool           `yaml:"save_output,omitempty" json:"save_output,omitempty"`
	Description string         `yaml:"description,omitempty" json:"description,omitempty"`
}

type Config struct {
	mu       sync.RWMutex
	filename string
	Jobs     []CronJob `yaml:"jobs"`
}

func New(filename string) *Config {
	return &Config{
		filename: filename,
		Jobs:     []CronJob{},
	}
}

func (c *Config) Load() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	data, err := os.ReadFile(c.filename)
	if err != nil {
		if os.IsNotExist(err) {
			c.Jobs = []CronJob{}
			return nil
		}
		return fmt.Errorf("failed to read config file: %w", err)
	}

	if err := yaml.Unmarshal(data, c); err != nil {
		return fmt.Errorf("failed to parse config file: %w", err)
	}

	return nil
}

func (c *Config) Save() error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(c.filename, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

func (c *Config) AddJob(job CronJob) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	for i, existingJob := range c.Jobs {
		if existingJob.ID == job.ID {
			c.Jobs[i] = job
			return nil
		}
	}

	c.Jobs = append(c.Jobs, job)
	return nil
}

func (c *Config) DeleteJob(id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	for i, job := range c.Jobs {
		if job.ID == id {
			c.Jobs = append(c.Jobs[:i], c.Jobs[i+1:]...)
			return nil
		}
	}

	return fmt.Errorf("job with id %s not found", id)
}

func (c *Config) GetJob(id string) (*CronJob, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	for _, job := range c.Jobs {
		if job.ID == id {
			return &job, nil
		}
	}

	return nil, fmt.Errorf("job with id %s not found", id)
}

func (c *Config) GetAllJobs() []CronJob {
	c.mu.RLock()
	defer c.mu.RUnlock()

	jobs := make([]CronJob, len(c.Jobs))
	copy(jobs, c.Jobs)
	return jobs
}