# Cron Microservice

A lightweight, statically compiled Go microservice for scheduling and managing cron jobs with webhook capabilities.

## Features

- **Cron Job Scheduling**: Schedule webhooks using standard cron expressions
- **Webhook Support**: Execute GET and POST requests to configured endpoints
- **Webhook Chaining**: Save output from primary webhook and forward to secondary webhook
- **Embedded Web UI**: Minimalist web interface using embedded filesystem and htmx
- **Light/Dark Mode**: UI supports both light and dark themes
- **YAML Configuration**: Simple YAML-based configuration management
- **Test Functionality**: "Test Now" button to immediately execute any cron job
- **Static Binary**: Single statically compiled binary with embedded frontend

## Project Structure

```
cron-microservice/
├── cmd/cron-service/          # Main application entry point
├── internal/
│   ├── config/               # YAML configuration management
│   ├── scheduler/            # Cron scheduler and webhook executor
│   └── server/               # HTTP server with embedded filesystem
├── web/
│   ├── static/              # Static assets (CSS, JS)
│   └── templates/           # HTML templates
├── go.mod                   # Go module definition
├── Makefile                # Build automation
└── README.md               # This file
```

## Installation

### Prerequisites

- Go 1.24 or later
- Make (optional, for using Makefile)

### Building from Source

```bash
# Clone the repository
git clone <repository-url>
cd cron-microservice

# Download dependencies
go mod download

# Build the binary
make build

# Or build manually
go build -o cmd/cron-service/bin/cron-service ./cmd/cron-service
```

### Running the Service

```bash
# Run with default settings (config.yaml, :8080)
./cmd/cron-service/bin/cron-service

# Specify custom configuration file and address
./cmd/cron-service/bin/cron-service -config /path/to/config.yaml -addr :9090
```

## Configuration

The service uses a YAML configuration file to store cron job definitions. On first run, it will create an empty configuration file if one doesn't exist.

### Configuration File Format

```yaml
jobs:
  - id: unique-job-id
    name: "Job Name"
    schedule: "* * * * *"  # Standard cron format
    enabled: true
    description: "Optional description"
    primary:
      url: "https://api.example.com/webhook"
      method: "POST"
      headers:
        Content-Type: "application/json"
        Authorization: "Bearer token"
      body: '{"key": "value"}'
    secondary:
      url: "https://api.example.com/secondary"
      method: "POST"
      headers:
        Content-Type: "application/json"
    save_output: true  # Save primary output and send to secondary
```

### Cron Schedule Format

The service uses standard cron format: `Minute Hour Day Month Weekday`

- `* * * * *` - Every minute
- `0 * * * *` - Every hour
- `0 0 * * *` - Daily at midnight
- `0 0 * * 0` - Weekly on Sunday
- `0 0 1 * *` - Monthly on the 1st

### Webhook Configuration

#### Primary Webhook
- **URL**: The endpoint to call
- **Method**: HTTP method (GET or POST)
- **Headers**: Optional HTTP headers as key-value pairs
- **Body**: Request body for POST requests

#### Secondary Webhook (Optional)
- **URL**: Second endpoint to call
- **Method**: HTTP method (GET or POST)
- **Headers**: Optional HTTP headers

#### Output Chaining
When `save_output: true` is set:
1. Primary webhook executes and response is saved
2. Secondary webhook receives the saved output as its body
3. Useful for processing or logging responses

## Web Interface

The service provides a web interface at `http://localhost:8080` (or your configured address).

### Features

- **Job Management**: Add, edit, and delete cron jobs
- **Real-time Status**: View job status and configuration
- **Test Execution**: "Test Now" button for immediate job execution
- **Theme Toggle**: Switch between light and dark modes
- **Responsive Design**: Works on desktop and mobile

### Using the Interface

1. **Add a Job**: Click "Add Job" and fill in the form
2. **Edit a Job**: Click "Edit" on any existing job
3. **Test a Job**: Click "Test Now" to execute immediately
4. **Delete a Job**: Click "Delete" and confirm

## API Endpoints

### Jobs Management

- `GET /api/jobs` - List all jobs
- `POST /api/jobs` - Create a new job
- `GET /api/jobs/{id}` - Get specific job
- `PUT /api/jobs/{id}` - Update a job
- `DELETE /api/jobs/{id}` - Delete a job
- `POST /api/jobs/test/{id}` - Test execute a job

### UI Routes

- `GET /` - Main UI page
- `GET /static/*` - Static assets

## Development

### Project Structure

- **Backend**: Pure Go with standard library
- **Frontend**: Vanilla JavaScript with htmx for dynamic updates
- **Styling**: Custom CSS with CSS variables for theming
- **Database**: File-based YAML configuration (no external DB required)

### Building for Production

```bash
# Build optimized binary
make build

# The binary is statically compiled and includes all web assets
# Copy to target system and run
./cron-service -config /path/to/config.yaml
```

### Makefile Commands

- `make build` - Build the binary
- `make run` - Build and run the service
- `make test` - Run tests
- `make clean` - Clean build artifacts
- `make fmt` - Format Go code
- `make lint` - Run linter (requires golangci-lint)
- `make deps` - Download and tidy dependencies
- `make install` - Install binary to /usr/local/bin

## Examples

### Example 1: Simple GET Request

```yaml
jobs:
  - id: health-check
    name: "Health Check"
    schedule: "*/5 * * * *"  # Every 5 minutes
    enabled: true
    primary:
      url: "https://api.example.com/health"
      method: "GET"
```

### Example 2: POST with JSON Body

```yaml
jobs:
  - id: daily-report
    name: "Daily Report"
    schedule: "0 9 * * *"  # Daily at 9 AM
    enabled: true
    primary:
      url: "https://api.example.com/reports/daily"
      method: "POST"
      headers:
        Content-Type: "application/json"
        Authorization: "Bearer your-token-here"
      body: '{"type": "daily", "format": "json"}'
```

### Example 3: Webhook Chaining

```yaml
jobs:
  - id: process-and-log
    name: "Process and Log"
    schedule: "0 */6 * * *"  # Every 6 hours
    enabled: true
    primary:
      url: "https://api.example.com/process-data"
      method: "POST"
      headers:
        Content-Type: "application/json"
      body: '{"action": "process"}'
    secondary:
      url: "https://api.example.com/log-result"
      method: "POST"
      headers:
        Content-Type: "application/json"
    save_output: true
```

## Troubleshooting

### Common Issues

1. **Jobs not executing**: Check that jobs are enabled and schedule is correct
2. **Webhook failures**: Verify URLs, methods, and authentication headers
3. **Configuration errors**: Ensure YAML syntax is valid
4. **Port conflicts**: Use `-addr` flag to specify different port

### Logs

The service logs to stdout. Check the console output for execution errors and webhook responses.

## License

This project is part of the Mule AI development tools suite.