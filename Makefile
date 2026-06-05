.PHONY: build run clean test deploy tail

APP_NAME=dad_proxy
BUILD_DIR=build

LDFLAGS=-s -w

build:
	go mod download
	CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/$(APP_NAME) ./cmd/$(APP_NAME)

deploy:
	ssh $(SERVER_ADDRESS) "rm -rf /root/dad_proxy"
	scp -r ./build/dad_proxy ${SERVER_ADDRESS}:/root/
	ssh $(SERVER_ADDRESS) "mv /root/dad_proxy /opt/dad_proxy/"
	ssh $(SERVER_ADDRESS) "systemctl stop dad_proxy"
	ssh $(SERVER_ADDRESS) "rm -rf /var/log/dad_proxy/*"
	ssh $(SERVER_ADDRESS) "systemctl start dad_proxy"

tail:
	ssh $(SERVER_ADDRESS) "tail -f /var/log/dad_proxy/service.log"

run: build
	./$(BUILD_DIR)/$(APP_NAME)

clean:
	rm -rf $(BUILD_DIR)
	go clean

test:
	go test -v ./...

dev:
	go run ./cmd/$(APP_NAME)/main.go