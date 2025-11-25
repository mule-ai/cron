.PHONY: build run clean test fmt lint

# Binary name
BINARY_NAME=cron-service
BINARY_PATH=cmd/cron-service/bin/$(BINARY_NAME)

# Go commands
GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
GOMOD=$(GOCMD) mod

# Build flags
BUILD_FLAGS=-ldflags="-s -w" -trimpath

build:
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p cmd/cron-service/bin
	$(GOBUILD) $(BUILD_FLAGS) -o $(BINARY_PATH) ./cmd/cron-service

run: build
	@echo "Running $(BINARY_NAME)..."
	./$(BINARY_PATH)

clean:
	@echo "Cleaning..."
	$(GOCLEAN)
	@rm -rf cmd/cron-service/bin/

test:
	@echo "Running tests..."
	$(GOTEST) -v ./...

fmt:
	@echo "Formatting code..."
	$(GOCMD) fmt ./...

lint:
	@echo "Running linter..."
	@golangci-lint run ./... || echo "golangci-lint not found, skipping..."

deps:
	@echo "Downloading dependencies..."
	$(GOMOD) download
	$(GOMOD) tidy

install: build
	@echo "Installing $(BINARY_NAME)..."
	@cp $(BINARY_PATH) /usr/local/bin/$(BINARY_NAME) || echo "Installation failed (requires sudo)"

.DEFAULT_GOAL: build