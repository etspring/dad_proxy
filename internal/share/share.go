package share

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"dad_proxy/internal/config"
	"dad_proxy/internal/version"
)

type ProxyShare struct {
	config *config.Config
	logger *slog.Logger
	client *http.Client
}

type ShareData struct {
	ProxyIP     string `json:"proxy_ip"`
	APIURL      string `json:"api_url"`
	ProxyPort   string `json:"proxy_port"`
	Environment string `json:"environment"`
	ProxyShare  bool   `json:"proxy_share"`
	Timestamp   int64  `json:"timestamp"`
}

func NewProxyShare(cfg *config.Config, logger *slog.Logger) *ProxyShare {
	return &ProxyShare{
		config: cfg,
		logger: logger,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (ps *ProxyShare) SendConfig() error {
	shareData := ShareData{
		ProxyIP:     ps.config.ProxyIP,
		APIURL:      ps.config.APIURL,
		ProxyPort:   ps.config.ProxyPort,
		Environment: ps.config.Environment,
		ProxyShare:  ps.config.ProxyShare,
		Timestamp:   time.Now().Unix(),
	}

	jsonData, err := json.Marshal(shareData)
	if err != nil {
		ps.logger.Error("Failed to marshal share data", "error", err)
		return fmt.Errorf("marshal error: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, "https://cadiastands.ru/dad_proxy/share", bytes.NewBuffer(jsonData))
	if err != nil {
		ps.logger.Error("Failed to create POST request", "error", err)
		return fmt.Errorf("request creation error: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "dad_proxy/"+version.AppVersion)

	resp, err := ps.client.Do(req)
	if err != nil {
		ps.logger.Error("Failed to send POST request", "error", err)
		return fmt.Errorf("request error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		ps.logger.Warn("Unexpected response status",
			"status_code", resp.StatusCode,
			"expected", "200/201",
		)
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	ps.logger.Info("Proxy successfully shared!")

	return nil
}

func (ps *ProxyShare) SendConfigAsync() {
	go func() {
		if err := ps.SendConfig(); err != nil {
			ps.logger.Error("Failed to send config asynchronously", "error", err)
		}
	}()
}
