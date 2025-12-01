VERSION := 1.0.0
BINARY_NAME := port-github-migrator
REPO := github.com/omby8888/port-github-migrator

# Build variables
LDFLAGS := -ldflags="-X main.Version=$(VERSION) -s -w"
BUILD_DIR := bin

.PHONY: build
build:
	@echo "🔨 Building for current platform..."
	go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd

.PHONY: build-release
build-release:
	@echo "🔨 Building for all platforms..."
	@mkdir -p $(BUILD_DIR)
	
	# Linux x64
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-x64 ./cmd
	
	# macOS x64
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-macos-x64 ./cmd
	
	# macOS arm64
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-macos-arm64 ./cmd
	
	# Windows x64
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-win-x64.exe ./cmd
	
	@ls -lh $(BUILD_DIR)/$(BINARY_NAME)*

.PHONY: run
run: build
	./$(BUILD_DIR)/$(BINARY_NAME) --help

.PHONY: clean
clean:
	@rm -rf $(BUILD_DIR)

.PHONY: test
test:
	go test -v ./...

.PHONY: fmt
fmt:
	go fmt ./...

.PHONY: vet
vet:
	go vet ./...

