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
	reminders  map[string]*time.Timer // Store timers for reminders
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
		reminders: make(map[string]*time.Timer),
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

	// Remove existing job if it exists
	if entryID, exists := s.jobs[job.ID]; exists {
		s.cron.Remove(entryID)
		delete(s.jobs, job.ID)
	}

	// Remove existing reminders for this job
	s.removeJobReminders(job.ID)

	// If job is disabled, don't schedule it (just remove if it existed)
	if !job.Enabled {
		return nil
	}

	action := func() {
		s.executeJob(job)
	}

	entryID, err := s.cron.AddFunc(job.Schedule, action)
	if err != nil {
		return fmt.Errorf("failed to add cron job: %w", err)
	}

	s.jobs[job.ID] = entryID

	// Schedule reminders for this job
	for _, reminder := range job.Reminders {
		if err := s.scheduleReminder(job, reminder); err != nil {
			s.logger.Printf("[REMINDER_ERROR] Failed to schedule reminder %s for job %s: %v", reminder.ID, job.ID, err)
		}
	}

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

	// Remove reminders for this job
	s.removeJobReminders(jobID)

	return nil
}

// removeJobReminders removes all reminders for a job
func (s *Scheduler) removeJobReminders(jobID string) {
	// Remove all reminders that start with this job ID
	for reminderID, timer := range s.reminders {
		if strings.HasPrefix(reminderID, jobID+"_") {
			timer.Stop()
			delete(s.reminders, reminderID)
		}
	}
}

// removeReminder removes a specific reminder
func (s *Scheduler) removeReminder(jobID, reminderID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	reminderKey := jobID + "_" + reminderID
	if timer, exists := s.reminders[reminderKey]; exists {
		timer.Stop()
		delete(s.reminders, reminderKey)
	}
}

// scheduleReminder schedules a reminder to be executed at its specified time
func (s *Scheduler) scheduleReminder(job config.CronJob, reminder config.Reminder) error {
	now := time.Now()
	if reminder.Datetime.Before(now) {
		// Reminder is in the past, don't schedule it
		s.logger.Printf("[REMINDER_SKIPPED] Reminder %s is in the past, skipping", reminder.ID)
		return nil
	}

	duration := reminder.Datetime.Sub(now)

	action := func() {
		s.executeReminder(job, reminder)
	}

	timer := time.AfterFunc(duration, action)
	s.reminders[job.ID+"_"+reminder.ID] = timer

	s.logger.Printf("[REMINDER_SCHEDULED] Scheduled reminder %s for job %s in %v", reminder.ID, job.ID, duration)
	return nil
}

// executeReminder executes a reminder by sending a webhook
func (s *Scheduler) executeReminder(job config.CronJob, reminder config.Reminder) {
	s.logger.Printf("[REMINDER_START] Executing reminder: %s for job: %s", reminder.Text, job.Name)

	// Create a temporary webhook config for the reminder based on the primary webhook
	reminderWebhook := job.Primary

	// Process the body template with the REMINDER variable
	if reminderWebhook.Body != "" {
		variables := map[string]interface{}{
			"REMINDER": reminder.Text,
		}

		processedBody, err := s.processTemplate(reminderWebhook.Body, variables)
		if err != nil {
			s.logger.Printf("[REMINDER_ERROR] Failed to process template for reminder %s: %v", reminder.ID, err)
			// Fall back to original body
		} else {
			reminderWebhook.Body = processedBody
			s.logger.Printf("[REMINDER_TEMPLATE] Processed template: %s", processedBody)
		}
	}

	// Execute the primary webhook for the reminder and capture response
	ctx := context.Background()
	primaryResponse, err := s.executeWebhook(ctx, reminderWebhook)
	if err != nil {
		s.logger.Printf("[REMINDER_ERROR] Failed to execute primary webhook for reminder %s: %v", reminder.ID, err)
	} else {
		s.logger.Printf("[REMINDER_PRIMARY_SUCCESS] Primary webhook for reminder %s executed successfully", reminder.ID)
		s.logger.Printf("[REMINDER_PRIMARY_RESPONSE] Response: %s", primaryResponse)
	}

	// Execute secondary webhook if configured and enabled
	if job.Secondary != nil && job.Secondary.Enabled {
		s.logger.Printf("[REMINDER_SECONDARY] Preparing secondary webhook for reminder %s", reminder.ID)

		// Create a copy of secondary config
		secondaryWebhook := *job.Secondary

		// For reminders, we want to process the secondary webhook similar to regular jobs
		// We'll use the primary response as data for the secondary webhook
		if primaryResponse != "" {
			s.logger.Printf("[REMINDER_SECONDARY] Processing primary response: %s", primaryResponse)

			// Log the JQ selectors configuration
			s.logger.Printf("[REMINDER_DEBUG] Job secondary JQ selectors: %+v", job.Secondary.JQSelectors)
			s.logger.Printf("[REMINDER_DEBUG] Job secondary JQ selectors length: %d", len(job.Secondary.JQSelectors))

			// Extract variables using jq selectors if configured
			var variables map[string]interface{}
			if len(job.Secondary.JQSelectors) > 0 {
				s.logger.Printf("[REMINDER_JQ_EXTRACTION] Extracting variables using jq selectors")
				vars, err := s.extractVariables(primaryResponse, job.Secondary.JQSelectors)
				if err != nil {
					s.logger.Printf("[REMINDER_JQ_ERROR] Failed to extract variables: %v", err)
				} else {
					variables = vars
					s.logger.Printf("[REMINDER_JQ_SUCCESS] Extracted %d variables", len(variables))
					// Log extracted variables
					for k, v := range variables {
						s.logger.Printf("[REMINDER_JQ_VARIABLE] %s = %v", k, v)
					}
				}
			} else {
				s.logger.Printf("[REMINDER_JQ_SKIP] No JQ selectors configured for secondary webhook")
			}

			// Add the reminder text as a special variable
			if variables == nil {
				variables = make(map[string]interface{})
			}
			variables["REMINDER"] = reminder.Text

			// Only add message variable with the full primary response if it wasn't already extracted by JQ
			if _, exists := variables["message"]; !exists {
				variables["message"] = primaryResponse
				s.logger.Printf("[REMINDER_MESSAGE_VAR] Setting message variable to primary response as fallback")
			} else {
				s.logger.Printf("[REMINDER_MESSAGE_VAR] Keeping JQ-extracted message variable")
			}

			// If template is provided, process it with extracted variables
			if secondaryWebhook.BodyTemplate != "" {
				s.logger.Printf("[REMINDER_SECONDARY_TEMPLATE] Processing template: %s", secondaryWebhook.BodyTemplate)
				processedBody, err := s.processTemplate(secondaryWebhook.BodyTemplate, variables)
				if err != nil {
					s.logger.Printf("[REMINDER_SECONDARY_TEMPLATE_ERROR] Failed to process template for reminder %s: %v", reminder.ID, err)
					// Fall back to using primary response directly in body
					secondaryWebhook.Body = primaryResponse
				} else {
					secondaryWebhook.Body = processedBody
					s.logger.Printf("[REMINDER_SECONDARY_TEMPLATE_SUCCESS] Processed template result: %s", processedBody)
				}
			} else if secondaryWebhook.Body != "" {
				// If there's a body but no template, process it with variables
				processedBody, err := s.processTemplate(secondaryWebhook.Body, variables)
				if err != nil {
					s.logger.Printf("[REMINDER_SECONDARY_BODY_ERROR] Failed to process body for reminder %s: %v", reminder.ID, err)
				} else {
					secondaryWebhook.Body = processedBody
					s.logger.Printf("[REMINDER_SECONDARY_BODY_SUCCESS] Processed body: %s", processedBody)
				}
			} else {
				// Default to using the primary response as body
				secondaryWebhook.Body = primaryResponse
			}
		} else {
			// No primary response, use reminder text as fallback
			variables := map[string]interface{}{
				"REMINDER": reminder.Text,
				"message":  reminder.Text,
			}

			// Process template or body with reminder text
			if secondaryWebhook.BodyTemplate != "" {
				s.logger.Printf("[REMINDER_SECONDARY_TEMPLATE] Processing template with reminder text: %s", secondaryWebhook.BodyTemplate)
				processedBody, err := s.processTemplate(secondaryWebhook.BodyTemplate, variables)
				if err != nil {
					s.logger.Printf("[REMINDER_SECONDARY_TEMPLATE_ERROR] Failed to process template for reminder %s: %v", reminder.ID, err)
					// Fall back to using reminder text directly in body
					secondaryWebhook.Body = fmt.Sprintf("{\"reminder\": \"%s\", \"message\": \"%s\"}", reminder.Text, reminder.Text)
				} else {
					secondaryWebhook.Body = processedBody
					s.logger.Printf("[REMINDER_SECONDARY_TEMPLATE_SUCCESS] Processed template result: %s", processedBody)
				}
			} else if secondaryWebhook.Body != "" {
				// If there's a body but no template, process it with reminder text
				processedBody, err := s.processTemplate(secondaryWebhook.Body, variables)
				if err != nil {
					s.logger.Printf("[REMINDER_SECONDARY_BODY_ERROR] Failed to process body for reminder %s: %v", reminder.ID, err)
				} else {
					secondaryWebhook.Body = processedBody
					s.logger.Printf("[REMINDER_SECONDARY_BODY_SUCCESS] Processed body: %s", processedBody)
				}
			} else {
				// Default body with just the reminder text
				secondaryWebhook.Body = fmt.Sprintf("{\"reminder\": \"%s\", \"message\": \"%s\"}", reminder.Text, reminder.Text)
			}
		}

		// Execute the secondary webhook
		if _, err := s.executeWebhook(ctx, secondaryWebhook); err != nil {
			s.logger.Printf("[REMINDER_SECONDARY_ERROR] Failed to execute secondary webhook for reminder %s: %v", reminder.ID, err)
		} else {
			s.logger.Printf("[REMINDER_SECONDARY_SUCCESS] Secondary webhook for reminder %s executed successfully", reminder.ID)
		}
	} else if job.Secondary != nil {
		s.logger.Printf("[REMINDER_SECONDARY_DISABLED] Secondary webhook is disabled for reminder %s", reminder.ID)
	} else {
		s.logger.Printf("[REMINDER_NO_SECONDARY] No secondary webhook configured for reminder %s", reminder.ID)
	}

	// Clean up the timer
	s.mu.Lock()
	delete(s.reminders, job.ID+"_"+reminder.ID)
	s.mu.Unlock()

	// Delete the reminder from the job configuration
	if err := s.config.DeleteReminder(job.ID, reminder.ID); err != nil {
		s.logger.Printf("[REMINDER_CLEANUP_ERROR] Failed to delete reminder %s from job %s: %v", reminder.ID, job.ID, err)
	} else {
		s.logger.Printf("[REMINDER_DELETED] Successfully deleted reminder %s from job %s", reminder.ID, job.ID)

		// Save the updated configuration
		if err := s.config.Save(); err != nil {
			s.logger.Printf("[REMINDER_SAVE_ERROR] Failed to save config after deleting reminder %s: %v", reminder.ID, err)
		} else {
			s.logger.Printf("[REMINDER_CONFIG_SAVED] Configuration saved after deleting reminder %s", reminder.ID)
		}
	}
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

	// Execute secondary webhook if configured and enabled
	if job.Secondary != nil {
		if !job.Secondary.Enabled {
			s.logger.Printf("[SECONDARY_WEBHOOK_DISABLED] Secondary webhook is disabled for job %s", job.ID)
			return
		}

		s.logger.Printf("[SECONDARY_WEBHOOK] Preparing secondary webhook for job %s", job.ID)
		s.logger.Printf("[SECONDARY_WEBHOOK_DETAILS] URL: %s, Method: %s", job.Secondary.URL, job.Secondary.Method)

		// Log headers if present
		if len(job.Secondary.Headers) > 0 {
			s.logger.Printf("[SECONDARY_WEBHOOK_HEADERS] Headers: %+v", job.Secondary.Headers)
		}

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
						// Log extracted variables
						for k, v := range variables {
							s.logger.Printf("[JQ_VARIABLE] %s = %v", k, v)
						}
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

				// Log the body that will be sent
				if secondary.Body != "" {
					s.logger.Printf("[SECONDARY_WEBHOOK_BODY] Sending body: %s", secondary.Body)
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

			// Log the body that will be sent
			if job.Secondary.Body != "" {
				s.logger.Printf("[SECONDARY_WEBHOOK_BODY] Sending body: %s", job.Secondary.Body)
			}

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
	s.logger.Printf("[EXTRACT_VARIABLES_DEBUG] Called with jsonData length: %d", len(jsonData))
	s.logger.Printf("[EXTRACT_VARIABLES_DEBUG] Selectors: %+v", selectors)
	s.logger.Printf("[EXTRACT_VARIABLES_DEBUG] Number of selectors: %d", len(selectors))

	if len(selectors) == 0 {
		s.logger.Printf("[EXTRACT_VARIABLES_DEBUG] No selectors provided, returning nil")
		return nil, nil
	}

	// Parse the JSON data
	var data interface{}
	if err := json.Unmarshal([]byte(jsonData), &data); err != nil {
		s.logger.Printf("[EXTRACT_VARIABLES_ERROR] Failed to parse JSON response: %v", err)
		s.logger.Printf("[EXTRACT_VARIABLES_ERROR] JSON data: %s", jsonData)
		return nil, fmt.Errorf("failed to parse JSON response: %w", err)
	}

	variables := make(map[string]interface{})

	for varName, selector := range selectors {
		s.logger.Printf("[EXTRACT_VARIABLES_DEBUG] Processing selector: %s -> %s", varName, selector)
		query, err := gojq.Parse(selector)
		if err != nil {
			s.logger.Printf("[JQ_ERROR] Failed to parse jq selector '%s' for variable '%s': %v", selector, varName, err)
			continue
		}

		iter := query.Run(data)
		for {
			v, ok := iter.Next()
			if !ok {
				s.logger.Printf("[JQ_DEBUG] No more results for selector '%s' -> '%s'", varName, selector)
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

	s.logger.Printf("[EXTRACT_VARIABLES_DEBUG] Returning %d variables", len(variables))
	return variables, nil
}

// processTemplate processes a template string with variables
func (s *Scheduler) processTemplate(templateStr string, variables map[string]interface{}) (string, error) {
	if templateStr == "" {
		return templateStr, nil
	}

	result := templateStr

	// Handle REMINDER variable specially if not in variables map
	reminderPlaceholder := "{{REMINDER}}"
	if strings.Contains(result, reminderPlaceholder) {
		if reminderText, ok := variables["REMINDER"]; ok {
			// For strings, escape newlines and special chars for JSON
			if str, ok := reminderText.(string); ok {
				// Escape newlines and other special characters for JSON
				escapedStr := strings.ReplaceAll(str, "\n", "\\n")
				escapedStr = strings.ReplaceAll(escapedStr, "\r", "\\r")
				escapedStr = strings.ReplaceAll(escapedStr, "\t", "\\t")
				escapedStr = strings.ReplaceAll(escapedStr, "\"", "\\\"")
				result = strings.ReplaceAll(result, reminderPlaceholder, escapedStr)
				s.logger.Printf("[TEMPLATE_REPLACE] Replaced '{{REMINDER}}' with escaped string")
			} else {
				// For non-string values, marshal to JSON
				valueBytes, err := json.Marshal(reminderText)
				if err != nil {
					s.logger.Printf("[TEMPLATE_ERROR] Failed to marshal REMINDER value: %v", err)
					valueStr := fmt.Sprintf("%v", reminderText)
					result = strings.ReplaceAll(result, reminderPlaceholder, valueStr)
				} else {
					result = strings.ReplaceAll(result, reminderPlaceholder, string(valueBytes))
				}
				s.logger.Printf("[TEMPLATE_REPLACE] Replaced '{{REMINDER}}' with '%s'", string(valueBytes))
			}
		} else {
			// If REMINDER variable is not provided, replace with empty string
			result = strings.ReplaceAll(result, reminderPlaceholder, "")
			s.logger.Printf("[TEMPLATE_REPLACE] Replaced '{{REMINDER}}' with empty string (no reminder provided)")
		}
	}

	// Handle other variables
	for varName, value := range variables {
		// Skip REMINDER as it's already handled
		if varName == "REMINDER" {
			continue
		}

		placeholder := fmt.Sprintf("{{%s}}", varName)
		if !strings.Contains(result, placeholder) {
			continue
		}

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

	// Create a context with timeout if specified
	requestCtx := ctx
	if webhook.Timeout > 0 {
		var cancel context.CancelFunc
		requestCtx, cancel = context.WithTimeout(ctx, time.Duration(webhook.Timeout)*time.Second)
		defer cancel()
		s.logger.Printf("[WEBHOOK_TIMEOUT] Using custom timeout: %d seconds", webhook.Timeout)
	} else {
		s.logger.Printf("[WEBHOOK_TIMEOUT] Using default timeout")
	}

	req, err := http.NewRequestWithContext(requestCtx, webhook.Method, webhook.URL, body)
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