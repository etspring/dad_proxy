package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	ProxyPort       string
	APIURL          string
	ProxyIP         string
	Environment     string
	ProxyShare      bool
	PortsRangeStart int
	PortsRangeEnd   int
}

func Load() (*Config, error) {
	port := os.Getenv("DAD_PROXY_API_PORT")
	if port == "" {
		port = "80"
	}

	apiURL := os.Getenv("DAD_API_URL")
	if apiURL == "" {
		apiURL = "http://live-gateway.lunatichigh.net/dc/helloWorld"
	}

	proxyIP := os.Getenv("DAD_PROXY_IP")
	if proxyIP == "" {
		proxyIP = "127.0.0.1"
	}

	proxyShareFlag := os.Getenv("DAD_PROXY_SHARE")
	proxyShare := true

	if proxyShareFlag != "" {
		if parsed, err := strconv.ParseBool(proxyShareFlag); err == nil {
			proxyShare = parsed
		}
	}

	env := os.Getenv("DAD_PROXY_ENVIRONMENT")
	if env == "" {
		env = "development"
	}

	rangeStart, rangeEnd, err := parsePortsRange(os.Getenv("DAD_PROXY_PORTS_RANGE"))
	if err != nil {
		return nil, err
	}

	return &Config{
		ProxyPort:       port,
		APIURL:          apiURL,
		ProxyIP:         proxyIP,
		Environment:     env,
		ProxyShare:      proxyShare,
		PortsRangeStart: rangeStart,
		PortsRangeEnd:   rangeEnd,
	}, nil
}

func ValidatePort(port string) error {
	_, err := strconv.Atoi(port)
	return err
}

func parsePortsRange(raw string) (int, int, error) {
	if raw == "" {
		raw = "20200,20300"
	}

	parts := strings.Split(raw, ",")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("DAD_PROXY_PORTS_RANGE must be in format start,end")
	}

	start, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return 0, 0, fmt.Errorf("invalid DAD_PROXY_PORTS_RANGE start: %w", err)
	}

	end, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil {
		return 0, 0, fmt.Errorf("invalid DAD_PROXY_PORTS_RANGE end: %w", err)
	}

	if start < 1 || start > 65535 || end < 1 || end > 65535 || start > end {
		return 0, 0, fmt.Errorf("DAD_PROXY_PORTS_RANGE must be within 1..65535 and start<=end")
	}

	return start, end, nil
}
