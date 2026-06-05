package share

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"dad_proxy/internal/config"
	"dad_proxy/internal/version"
)

type ProxyShare struct {
	config *config.Config
	logger *slog.Logger
	client *http.Client
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
	params := url.Values{}
	params.Set("proxy_ip", ps.config.ProxyIP)
	params.Set("api_url", ps.config.APIURL)
	params.Set("proxy_port", ps.config.ProxyPort)
	params.Set("environment", ps.config.Environment)
	params.Set("proxy_share", strconv.FormatBool(ps.config.ProxyShare))
	params.Set("timestamp", strconv.FormatInt(time.Now().Unix(), 10))

	shareURL := "https://cadiastands.ru/dad_proxy/share?" + params.Encode()

	req, err := http.NewRequest(http.MethodGet, shareURL, nil)
	if err != nil {
		ps.logger.Error("Failed to create GET request", "error", err)
		return fmt.Errorf("request creation error: %w", err)
	}

	req.Header.Set("User-Agent", "dad_proxy/"+version.AppVersion)

	resp, err := ps.client.Do(req)
	if err != nil {
		ps.logger.Error("Failed to send GET request", "error", err)
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
