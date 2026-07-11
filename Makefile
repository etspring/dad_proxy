.PHONY: build run clean test deploy tail setup-go

APP_NAME=dad_proxy
BUILD_DIR=build
GO_LOCAL=$(HOME)/.local/go1.22.12/bin/go
GO_VERSION=1.22.12
GO=$(shell if [ -x "$(GO_LOCAL)" ]; then echo "$(GO_LOCAL)"; else command -v go; fi)

LDFLAGS=-s -w

check-go:
	@$(GO) version >/dev/null 2>&1 || (echo "error: go not found. Run: make setup-go" && exit 1)
	@$(GO) env GOVERSION | grep -qE 'go1\.(2[2-9]|[3-9][0-9])' || ( \
		echo "error: need Go >= 1.22 (module uses go 1.22.0). Current: $$($(GO) version)"; \
		echo "Run: make setup-go"; \
		exit 1)

# Устанавливает Go $(GO_VERSION) в ~/.local/go1.22.12 (не трогает системный пакет).
setup-go:
	@set -e; \
	V="$(GO_VERSION)"; DIR="$(GO_LOCAL)"; DIR="$${DIR%/bin/go}"; \
	if [ -x "$$DIR/bin/go" ]; then \
		echo "Go already installed: $$($$DIR/bin/go version)"; \
		exit 0; \
	fi; \
	echo "Installing Go $$V to $$DIR..."; \
	curl -fsSL "https://go.dev/dl/go$$V.linux-amd64.tar.gz" -o /tmp/go.tar.gz; \
	rm -rf "$$DIR"; mkdir -p "$$(dirname "$$DIR")"; \
	tar -C "$$(dirname "$$DIR")" -xzf /tmp/go.tar.gz; \
	mv "$$(dirname "$$DIR")/go" "$$DIR"; \
	rm -f /tmp/go.tar.gz; \
	"$$DIR/bin/go" version; \
	echo "Add to ~/.bashrc: export PATH=\"$$DIR/bin:\$$PATH\""

build: check-go
	mkdir -p $(BUILD_DIR)
	$(GO) mod download
	CGO_ENABLED=0 $(GO) build -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/$(APP_NAME) ./cmd/$(APP_NAME)

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
	$(GO) clean

test: check-go
	$(GO) test -v ./...

dev: check-go
	$(GO) run ./cmd/$(APP_NAME)/main.go
