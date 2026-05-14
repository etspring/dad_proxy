package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	ProxyPort               string
	APIURL                  string
	ProxyIP                 string
	Environment             string
	ProxyShare              bool
	PortsRangeStart         int
	PortsRangeEnd           int
	UDPPortsRangeStart      int
	UDPPortsRangeEnd        int
	UDPClientBindRangeStart int
	UDPClientBindRangeEnd   int
	TCPPayloadRewrite       bool
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

	rangeStart, rangeEnd, err := parsePortsRangeFromEnv()
	if err != nil {
		return nil, err
	}

	udpRangeStart, udpRangeEnd, err := parseUDPPortsRangeFromEnv()
	if err != nil {
		return nil, err
	}

	udpClientBindStart, udpClientBindEnd, err := parseUDPClientBindRangeFromEnv()
	if err != nil {
		return nil, err
	}

	tcpRewrite := true
	if v := os.Getenv("DAD_PROXY_TCP_PAYLOAD_REWRITE"); v != "" {
		if parsed, err := strconv.ParseBool(v); err == nil {
			tcpRewrite = parsed
		}
	}

	return &Config{
		ProxyPort:               port,
		APIURL:                  apiURL,
		ProxyIP:                 proxyIP,
		Environment:             env,
		ProxyShare:              proxyShare,
		PortsRangeStart:         rangeStart,
		PortsRangeEnd:           rangeEnd,
		UDPPortsRangeStart:      udpRangeStart,
		UDPPortsRangeEnd:        udpRangeEnd,
		UDPClientBindRangeStart: udpClientBindStart,
		UDPClientBindRangeEnd:   udpClientBindEnd,
		TCPPayloadRewrite:       tcpRewrite,
	}, nil
}

func parsePortsRangeFromEnv() (int, int, error) {
	startRaw := strings.TrimSpace(os.Getenv("DAD_PROXY_PORTS_RANGE_START"))
	endRaw := strings.TrimSpace(os.Getenv("DAD_PROXY_PORTS_RANGE_END"))
	if startRaw != "" || endRaw != "" {
		if startRaw == "" || endRaw == "" {
			return 0, 0, fmt.Errorf("set both DAD_PROXY_PORTS_RANGE_START and DAD_PROXY_PORTS_RANGE_END, or use DAD_PROXY_PORTS_RANGE only")
		}
		start, err := strconv.Atoi(startRaw)
		if err != nil {
			return 0, 0, fmt.Errorf("invalid DAD_PROXY_PORTS_RANGE_START: %w", err)
		}
		end, err := strconv.Atoi(endRaw)
		if err != nil {
			return 0, 0, fmt.Errorf("invalid DAD_PROXY_PORTS_RANGE_END: %w", err)
		}
		if start < 1 || start > 65535 || end < 1 || end > 65535 || start > end {
			return 0, 0, fmt.Errorf("DAD_PROXY_PORTS_RANGE_START/END must be within 1..65535 and start<=end")
		}
		return start, end, nil
	}

	return parsePortsRange(os.Getenv("DAD_PROXY_PORTS_RANGE"))
}

func parseUDPPortsRangeFromEnv() (int, int, error) {
	raw := strings.TrimSpace(os.Getenv("DAD_PROXY_UDP_PORTS_RANGE"))
	if raw == "" {
		return 7700, 8000, nil
	}
	return parsePortsRangeWithLabel(raw, "DAD_PROXY_UDP_PORTS_RANGE")
}

func parseUDPClientBindRangeFromEnv() (int, int, error) {
	raw := strings.TrimSpace(os.Getenv("DAD_PROXY_UDP_CLIENT_BIND_RANGE"))
	if raw == "" {
		return 7700, 8000, nil
	}
	return parsePortsRangeWithLabel(raw, "DAD_PROXY_UDP_CLIENT_BIND_RANGE")
}

func parsePortsRangeWithLabel(raw string, label string) (int, int, error) {
	parts := strings.Split(raw, ",")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("%s must be in format start,end", label)
	}

	start, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return 0, 0, fmt.Errorf("invalid %s start: %w", label, err)
	}

	end, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil {
		return 0, 0, fmt.Errorf("invalid %s end: %w", label, err)
	}

	if start < 1 || start > 65535 || end < 1 || end > 65535 || start > end {
		return 0, 0, fmt.Errorf("%s must be within 1..65535 and start<=end", label)
	}

	return start, end, nil
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
