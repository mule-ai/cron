package scheduler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/itchyny/gojq"
	"github.com/robfig/cron/v3"
	"cron-microservice/internal/config"
)

type Scheduler struct {
	cron       *cron.Cron
	jobs       map[string]cron.EntryID
	config     *config.Config
	httpClient *http.Client
	mu         sync.RWMutex
	outputs    map[string]string // Store outputs from webhook calls
	logger     *log.Logger
}

func New(cfg *config.Config) *Scheduler {
	return &Scheduler{
		cron: cron.New(),
		jobs: make(map[string]cron.EntryID),
		config: cfg,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		outputs: make(map[string]string),
		logger:  log.New(log.Writer(), "[SCHEDULER] ", log.LstdFlags),
	}
}

func (s *Scheduler) Start() {
	s.cron.Start()
}

func (s *Scheduler) Stop() {
	s.cron.Stop()
}

func (s *Scheduler) AddJob(job config.CronJob) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !job.Enabled {
		return nil
	}

	// Remove existing job if it exists
	if entryID, exists := s.jobs[job.ID]; exists {
		s.cron.Remove(entryID)
		delete(s.jobs, job.ID)
	}

	action := func() {
		s.executeJob(job)
	}

	entryID, err := s.cron.AddFunc(job.Schedule, action)
	if err != nil {
		return fmt.Errorf("failed to add cron job: %w", err)
	}

	s.jobs[job.ID] = entryID
	return nil
}

func (s *Scheduler) RemoveJob(jobID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if entryID, exists := s.jobs[jobID]; exists {
		s.cron.Remove(entryID)
		delete(s.jobs, jobID)
		delete(s.outputs, jobID)
	}

	return nil
}

func (s *Scheduler) executeJob(job config.CronJob) {
	ctx := context.Background()

	s.logger.Printf("[JOB_START] Executing job: %s (ID: %s)", job.Name, job.ID)

	// Execute primary webhook
	s.logger.Printf("[PRIMARY_WEBHOOK] Sending %s request to %s", job.Primary.Method, job.Primary.URL)
	if job.Primary.Body != "" {
		s.logger.Printf("[PRIMARY_WEBHOOK] Request body: %s", job.Primary.Body)
	}

	output, err := s.executeWebhook(ctx, job.Primary)
	if err != nil {
		s.logger.Printf("[PRIMARY_WEBHOOK_ERROR] Failed to execute primary webhook for job %s: %v", job.ID, err)
		return
	}

	s.logger.Printf("[PRIMARY_WEBHOOK_SUCCESS] Primary webhook executed successfully for job %s", job.ID)
	s.logger.Printf("[PRIMARY_WEBHOOK_RESPONSE] Response: %s", output)

	// Save output if configured
	if job.SaveOutput && output != "" {
		s.mu.Lock()
		s.outputs[job.ID] = output
		s.mu.Unlock()
		s.logger.Printf("[OUTPUT_SAVED] Saved output for job %s: %s", job.ID, output)
	} else if job.SaveOutput {
		s.logger.Printf("[OUTPUT_EMPTY] No output to save for job %s", job.ID)
	}

	// Execute secondary webhook if configured
	if job.Secondary != nil {
		s.logger.Printf("[SECONDARY_WEBHOOK] Preparing secondary webhook for job %s", job.ID)

		// If we have saved output, use it as data for secondary webhook
		if job.SaveOutput {
			s.mu.RLock()
			data := s.outputs[job.ID]
			s.mu.RUnlock()

			if data != "" {
				s.logger.Printf("[SECONDARY_WEBHOOK] Processing saved output: %s", data)

				// Extract variables using jq selectors if configured
				var variables map[string]interface{}
				if len(job.Secondary.JQSelectors) > 0 {
					s.logger.Printf("[JQ_EXTRACTION] Extracting variables using jq selectors")
					vars, err := s.extractVariables(data, job.Secondary.JQSelectors)
					if err != nil {
						s.logger.Printf("[JQ_ERROR] Failed to extract variables: %v", err)
					} else {
						variables = vars
						s.logger.Printf("[JQ_SUCCESS] Extracted %d variables", len(variables))
					}
				}

				// Create a copy of secondary config
				secondary := *job.Secondary

				// If template is provided, process it with extracted variables
				if secondary.BodyTemplate != "" {
					s.logger.Printf("[TEMPLATE_PROCESSING] Processing template: %s", secondary.BodyTemplate)
					processedBody, err := s.processTemplate(secondary.BodyTemplate, variables)
					if err != nil {
						s.logger.Printf("[TEMPLATE_ERROR] Failed to process template: %v", err)
						secondary.Body = data // Fallback to raw data
					} else {
						secondary.Body = processedBody
						s.logger.Printf("[TEMPLATE_SUCCESS] Processed template result: %s", processedBody)
					}
				} else {
					// No template, use raw data as before
					secondary.Body = data
					s.logger.Printf("[SECONDARY_WEBHOOK] Using raw saved output as body")
				}

				s.logger.Printf("[SECONDARY_WEBHOOK] Sending %s request to %s", secondary.Method, secondary.URL)
				if _, err := s.executeWebhook(ctx, secondary); err != nil {
					s.logger.Printf("[SECONDARY_WEBHOOK_ERROR] Failed to execute secondary webhook for job %s: %v", job.ID, err)
				} else {
					s.logger.Printf("[SECONDARY_WEBHOOK_SUCCESS] Secondary webhook executed successfully for job %s", job.ID)
				}
			} else {
				s.logger.Printf("[SECONDARY_WEBHOOK_SKIPPED] No saved output available for job %s", job.ID)
			}
		} else {
			// Execute secondary webhook without saved output
			s.logger.Printf("[SECONDARY_WEBHOOK] Sending %s request to %s", job.Secondary.Method, job.Secondary.URL)
			if _, err := s.executeWebhook(ctx, *job.Secondary); err != nil {
				s.logger.Printf("[SECONDARY_WEBHOOK_ERROR] Failed to execute secondary webhook for job %s: %v", job.ID, err)
			} else {
				s.logger.Printf("[SECONDARY_WEBHOOK_SUCCESS] Secondary webhook executed successfully for job %s", job.ID)
			}
		}
	} else {
		s.logger.Printf("[SECONDARY_WEBHOOK_NONE] No secondary webhook configured for job %s", job.ID)
	}

	s.logger.Printf("[JOB_COMPLETE] Finished executing job: %s (ID: %s)", job.Name, job.ID)
}

// extractVariables uses jq selectors to extract data from JSON response
func (s *Scheduler) extractVariables(jsonData string, selectors map[string]string) (map[string]interface{}, error) {
	if len(selectors) == 0 {
		return nil, nil
	}

	// Parse the JSON data
	var data interface{}
	if err := json.Unmarshal([]byte(jsonData), &data); err != nil {
		return nil, fmt.Errorf("failed to parse JSON response: %w", err)
	}

	variables := make(map[string]interface{})

	for varName, selector := range selectors {
		query, err := gojq.Parse(selector)
		if err != nil {
			s.logger.Printf("[JQ_ERROR] Failed to parse jq selector '%s' for variable '%s': %v", selector, varName, err)
			continue
		}

		iter := query.Run(data)
		for {
			v, ok := iter.Next()
			if !ok {
				break
			}
			if err, ok := v.(error); ok {
				s.logger.Printf("[JQ_ERROR] Failed to execute jq selector '%s' for variable '%s': %v", selector, varName, err)
				continue
			}

			variables[varName] = v
			s.logger.Printf("[JQ_EXTRACT] Extracted variable '%s' with value: %v", varName, v)
			break // Take the first result
		}
	}

	return variables, nil
}

// processTemplate processes a template string with variables
func (s *Scheduler) processTemplate(templateStr string, variables map[string]interface{}) (string, error) {
	if templateStr == "" || len(variables) == 0 {
		return templateStr, nil
	}

	result := templateStr
	for varName, value := range variables {
		placeholder := fmt.Sprintf("{{%s}}", varName)
		// For strings, escape newlines and special chars for JSON
		if str, ok := value.(string); ok {
			// Escape newlines and other special characters for JSON
			escapedStr := strings.ReplaceAll(str, "\n", "\\n")
			escapedStr = strings.ReplaceAll(escapedStr, "\r", "\\r")
			escapedStr = strings.ReplaceAll(escapedStr, "\t", "\\t")
			escapedStr = strings.ReplaceAll(escapedStr, "\"", "\\\"")
			result = strings.ReplaceAll(result, placeholder, escapedStr)
			s.logger.Printf("[TEMPLATE_REPLACE] Replaced '%s' with escaped string", placeholder)
		} else {
			// For non-string values, marshal to JSON
			valueBytes, err := json.Marshal(value)
			if err != nil {
				s.logger.Printf("[TEMPLATE_ERROR] Failed to marshal value for variable '%s': %v", varName, err)
				valueStr := fmt.Sprintf("%v", value)
				result = strings.ReplaceAll(result, placeholder, valueStr)
			} else {
				result = strings.ReplaceAll(result, placeholder, string(valueBytes))
			}
			s.logger.Printf("[TEMPLATE_REPLACE] Replaced '%s' with '%s'", placeholder, string(valueBytes))
		}
	}

	return result, nil
}

func (s *Scheduler) executeWebhook(ctx context.Context, webhook config.WebhookConfig) (string, error) {
	var body io.Reader
	if webhook.Body != "" {
		body = bytes.NewBufferString(webhook.Body)
		s.logger.Printf("[WEBHOOK_REQUEST] Body: %s", webhook.Body)
	}

	req, err := http.NewRequestWithContext(ctx, webhook.Method, webhook.URL, body)
	if err != nil {
		s.logger.Printf("[WEBHOOK_ERROR] Failed to create request: %v", err)
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	// Log headers
	if len(webhook.Headers) > 0 {
		s.logger.Printf("[WEBHOOK_HEADERS] %d headers set", len(webhook.Headers))
		for key, value := range webhook.Headers {
			// Don't log sensitive headers like Authorization
			if key != "Authorization" {
				s.logger.Printf("[WEBHOOK_HEADER] %s: %s", key, value)
			} else {
				s.logger.Printf("[WEBHOOK_HEADER] %s: ***", key)
			}
		}
	}

	// Set headers
	for key, value := range webhook.Headers {
		req.Header.Set(key, value)
	}

	// Set default content type if not specified
	if req.Header.Get("Content-Type") == "" && webhook.Body != "" {
		req.Header.Set("Content-Type", "application/json")
		s.logger.Printf("[WEBHOOK_HEADER] Set default Content-Type: application/json")
	}

	s.logger.Printf("[WEBHOOK_EXECUTING] %s %s", webhook.Method, webhook.URL)
	resp, err := s.httpClient.Do(req)
	if err != nil {
		s.logger.Printf("[WEBHOOK_ERROR] Failed to execute webhook: %v", err)
		return "", fmt.Errorf("failed to execute webhook: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			s.logger.Printf("[WEBHOOK_ERROR] Failed to close response body: %v", err)
		}
	}()

	s.logger.Printf("[WEBHOOK_RESPONSE] Status: %d %s", resp.StatusCode, resp.Status)

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		s.logger.Printf("[WEBHOOK_ERROR] Failed to read response body: %v", err)
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode >= 400 {
		s.logger.Printf("[WEBHOOK_ERROR] Webhook returned error status %d: %s", resp.StatusCode, string(responseBody))
		return "", fmt.Errorf("webhook returned error status %d: %s", resp.StatusCode, string(responseBody))
	}

	s.logger.Printf("[WEBHOOK_SUCCESS] Response body: %s", string(responseBody))
	return string(responseBody), nil
}

func (s *Scheduler) TestJob(jobID string) error {
	job, err := s.config.GetJob(jobID)
	if err != nil {
		return err
	}

	// Execute job immediately in a goroutine
	go s.executeJob(*job)
	return nil
}

func (s *Scheduler) LoadJobs() error {
	jobs := s.config.GetAllJobs()
	
	for _, job := range jobs {
		if err := s.AddJob(job); err != nil {
			fmt.Printf("Failed to load job %s: %v\n", job.ID, err)
		}
	}

	return nil
}