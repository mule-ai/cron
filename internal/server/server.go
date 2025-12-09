package server

import (
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"path"
	"strings"

	"cron-microservice/internal/config"
	"cron-microservice/internal/scheduler"
)

//go:embed web/static/* web/templates/*
var webFS embed.FS

type Server struct {
	config    *config.Config
	scheduler *scheduler.Scheduler
	templates *template.Template
}

func New(cfg *config.Config, sched *scheduler.Scheduler) *Server {
	tmpl := template.Must(template.ParseFS(webFS, "web/templates/*.html"))

	return &Server{
		config:    cfg,
		scheduler: sched,
		templates: tmpl,
	}
}

func (s *Server) Start(addr string) error {
	mux := http.NewServeMux()

	// API routes
	mux.HandleFunc("/api/jobs", s.handleJobs)
	mux.HandleFunc("/api/jobs/", s.handleJob)
	mux.HandleFunc("/api/jobs/test/", s.handleTestJob)
	mux.HandleFunc("/api/reminders/", s.handleReminder)

	// Static files - serve from web/static subdirectory
	staticFS, err := fs.Sub(webFS, "web/static")
	if err != nil {
		return fmt.Errorf("failed to create static filesystem: %w", err)
	}
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))

	// UI routes
	mux.HandleFunc("/", s.handleIndex)

	return http.ListenAndServe(addr, mux)
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	
	jobs := s.config.GetAllJobs()
	if err := s.templates.ExecuteTemplate(w, "index.html", jobs); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (s *Server) handleJobs(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		jobs := s.config.GetAllJobs()
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(jobs); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		
	case http.MethodPost:
		var job config.CronJob
		if err := json.NewDecoder(r.Body).Decode(&job); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		
		if err := s.config.AddJob(job); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		
		if err := s.config.Save(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		
		if err := s.scheduler.AddJob(job); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(job); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleJob(w http.ResponseWriter, r *http.Request) {
	jobID := path.Base(r.URL.Path)

	switch r.Method {
	case http.MethodGet:
		job, err := s.config.GetJob(jobID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(job); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		
	case http.MethodPut:
		var job config.CronJob
		if err := json.NewDecoder(r.Body).Decode(&job); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		
		if job.ID != jobID {
			http.Error(w, "Job ID mismatch", http.StatusBadRequest)
			return
		}
		
		if err := s.config.AddJob(job); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		
		if err := s.config.Save(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		
		if err := s.scheduler.AddJob(job); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(job); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

	case http.MethodDelete:
		if err := s.config.DeleteJob(jobID); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		
		if err := s.config.Save(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		
		if err := s.scheduler.RemoveJob(jobID); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusNoContent)
		
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleTestJob(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	jobID := path.Base(r.URL.Path)

	if err := s.scheduler.TestJob(jobID); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleReminder(w http.ResponseWriter, r *http.Request) {
	// Path format: /api/reminders/{jobID}/{reminderID}
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) != 4 {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	jobID := pathParts[2]
	reminderID := pathParts[3]

	switch r.Method {
	case http.MethodDelete:
		// Get the job
		job, err := s.config.GetJob(jobID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}

		// Find and remove the reminder
		reminderFound := false
		updatedReminders := []config.Reminder{}
		for _, reminder := range job.Reminders {
			if reminder.ID == reminderID {
				reminderFound = true
				continue // Skip this reminder (effectively removing it)
			}
			updatedReminders = append(updatedReminders, reminder)
		}

		if !reminderFound {
			http.Error(w, "Reminder not found", http.StatusNotFound)
			return
		}

		// Update the job with the new reminders list
		job.Reminders = updatedReminders

		// Save the updated job
		if err := s.config.AddJob(*job); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if err := s.config.Save(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Update the scheduler
		if err := s.scheduler.AddJob(*job); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusNoContent)

	case http.MethodPut:
		// Get the job
		job, err := s.config.GetJob(jobID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}

		// Parse the updated reminder from request body
		var updatedReminder config.Reminder
		if err := json.NewDecoder(r.Body).Decode(&updatedReminder); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Validate that the reminder ID matches
		if updatedReminder.ID != reminderID {
			http.Error(w, "Reminder ID mismatch", http.StatusBadRequest)
			return
		}

		// Find and update the reminder
		reminderFound := false
		for i, reminder := range job.Reminders {
			if reminder.ID == reminderID {
				job.Reminders[i] = updatedReminder
				reminderFound = true
				break
			}
		}

		if !reminderFound {
			http.Error(w, "Reminder not found", http.StatusNotFound)
			return
		}

		// Save the updated job
		if err := s.config.AddJob(*job); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if err := s.config.Save(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Update the scheduler
		if err := s.scheduler.AddJob(*job); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(updatedReminder); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}