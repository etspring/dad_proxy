.PHONY: build run clean test

APP_NAME=dad_proxy
BUILD_DIR=build

build:
	go mod download
	go build -o $(BUILD_DIR)/$(APP_NAME) ./cmd/$(APP_NAME)

run: build
	./$(BUILD_DIR)/$(APP_NAME)

clean:
	rm -rf $(BUILD_DIR)
	go clean

test:
	go test -v ./...

dev:
	go run ./cmd/$(APP_NAME)/main.go