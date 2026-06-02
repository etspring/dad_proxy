package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"dad_proxy/internal/config"
	"dad_proxy/internal/models"
	"dad_proxy/internal/tunnels"
	"dad_proxy/internal/version"
)

type ProxyHandler struct {
	config        *config.Config
	logger        *slog.Logger
	client        *http.Client
	tunnelManager *tunnels.Manager
}

type rootResponse struct {
	App     string `json:"app"`
	Version string `json:"version"`
	Details string `json:"details"`
}

type tunnelsResponse struct {
	App              string                   `json:"app"`
	Version          string                   `json:"version"`
	Count            int                      `json:"count"`
	Tunnels          []tunnelPublicInfo       `json:"tunnels"`
	UDPTunnelCount   int                      `json:"udpTunnelCount"`
	TotalUDPSessions int64                    `json:"totalUdpSessions"`
	UDPTunnels       []tunnels.UDPTunnelStats `json:"udpTunnels"`
}

type tunnelPublicInfo struct {
	RemoteIP                 string    `json:"remoteIp"`
	RemotePort               int       `json:"remotePort"`
	LocalPort                int       `json:"localPort"`
	UDPClientPort            int       `json:"udpClientPort,omitempty"`
	CreatedAt                time.Time `json:"createdAt"`
	LastActivityAt           time.Time `json:"lastActivityAt"`
	ActiveTCPConnections     int64     `json:"activeTcpConnections"`
	TotalTCPConnections      int64     `json:"totalTcpConnections"`
	BytesFromClientsToRemote int64     `json:"bytesFromClientsToRemote"`
	BytesFromRemoteToClients int64     `json:"bytesFromRemoteToClients"`
	UDPLocalListenAddr       string    `json:"udpLocalListenAddr,omitempty"`
}

func toTunnelPublicInfo(ti tunnels.TunnelInfo) tunnelPublicInfo {
	return tunnelPublicInfo{
		RemoteIP:                 ti.RemoteIP,
		RemotePort:               ti.RemotePort,
		LocalPort:                ti.LocalPort,
		UDPClientPort:            ti.UDPClientPort,
		CreatedAt:                ti.CreatedAt,
		LastActivityAt:           ti.LastActivityAt,
		ActiveTCPConnections:     ti.ActiveTCPConnections,
		TotalTCPConnections:      ti.TotalTCPConnections,
		BytesFromClientsToRemote: ti.BytesFromClientsToRemote,
		BytesFromRemoteToClients: ti.BytesFromRemoteToClients,
		UDPLocalListenAddr:       ti.UDPLocalListenAddr,
	}
}

func NewProxyHandler(cfg *config.Config, logger *slog.Logger) *ProxyHandler {
	return &ProxyHandler{
		config:        cfg,
		logger:        logger,
		tunnelManager: tunnels.NewManager(cfg, logger),
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (h *ProxyHandler) HandleProxy(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/" {
		h.handleRoot(w, r)
		return
	}

	if r.URL.Path != h.config.APIHelloPath {
		http.NotFound(w, r)
		return
	}

	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	startTime := time.Now()

	userIP := h.getClientIP(r)

	headers := make(http.Header)
	for name, values := range r.Header {
		// Skip Host header
		if strings.EqualFold(name, "Host") {
			continue
		}
		for _, value := range values {
			headers.Add(name, value)
		}
	}

	h.logger.Info("Received user request",
		"method", r.Method,
		"path", r.URL.Path,
		"user_ip", userIP,
		"headers", h.formatHeaders(headers),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.config.APIURL, nil)
	if err != nil {
		h.logger.Error("Failed to create request to API", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Copy headers
	req.Header = headers.Clone()

	// Call external API
	resp, err := h.client.Do(req)
	if err != nil {
		h.logger.Error("Failed to call API",
			"error", err,
			"url", h.config.APIURL,
			"user_ip", userIP,
		)
		http.Error(w, "Failed to fetch data from API", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		h.logger.Error("Failed to read API response body", "error", err)
		http.Error(w, "Failed to read API response", http.StatusInternalServerError)
		return
	}

	// Log external API response
	h.logger.Info("API response received",
		"status_code", resp.StatusCode,
		"headers", h.formatHeaders(resp.Header),
		"body", string(body),
		"content_length", len(body),
	)

	// HTTP CODE == 200 ?
	if resp.StatusCode != http.StatusOK {
		h.logger.Warn("API returned non-OK status",
			"status_code", resp.StatusCode,
			"body", string(body),
			"user_ip", userIP,
		)
		http.Error(w, fmt.Sprintf("API returned status %d", resp.StatusCode), resp.StatusCode)
		return
	}

	var apiResp models.APIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		h.logger.Error("Failed to parse JSON response",
			"error", err,
			"body", string(body),
			"user_ip", userIP,
		)
		http.Error(w, "Invalid JSON response from API", http.StatusInternalServerError)
		return
	}

	originalIP := apiResp.IPAddress
	originalPort := apiResp.Port

	if apiResp.UnderMaintenance != 0 {
		apiResp.IPAddress = h.config.ProxyIP
		apiResp.Remote = userIP

		h.logger.Info("Upstream in maintenance mode, tunnel skipped",
			"under_maintenance", apiResp.UnderMaintenance,
			"original_ipAddress", originalIP,
			"original_port", originalPort,
		)

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Proxy-Version", "dad_proxy/"+version.AppVersion)
		w.WriteHeader(http.StatusOK)

		modifiedBody, marshalErr := json.Marshal(apiResp)
		if marshalErr != nil {
			h.logger.Error("Failed to marshal maintenance response", "error", marshalErr)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		if _, writeErr := w.Write(modifiedBody); writeErr != nil {
			h.logger.Error("Failed to write maintenance response", "error", writeErr)
		}

		elapsed := time.Since(startTime)
		h.logger.Info("Request completed in maintenance mode",
			"user_ip", userIP,
			"duration_ms", elapsed.Milliseconds(),
			"response_size", len(modifiedBody),
		)
		return
	}

	if originalIP == "" || originalPort <= 0 || originalPort > 65535 {
		h.logger.Error("Invalid upstream address in API response",
			"ipAddress", originalIP,
			"port", originalPort,
			"user_ip", userIP,
		)
		http.Error(w, "Invalid upstream endpoint from API", http.StatusBadGateway)
		return
	}

	tunnelInfo, err := h.tunnelManager.EnsureTunnel(originalIP, originalPort)
	if err != nil {
		h.logger.Error("Failed to create or reuse tunnel",
			"ipAddress", originalIP,
			"port", originalPort,
			"error", err,
			"user_ip", userIP,
		)
		http.Error(w, "Failed to establish tunnel", http.StatusBadGateway)
		return
	}

	apiResp.IPAddress = h.config.ProxyIP
	if tunnelInfo.UDPClientPort > 0 {
		apiResp.Port = tunnelInfo.UDPClientPort
	} else {
		apiResp.Port = tunnelInfo.LocalPort
	}
	apiResp.Remote = userIP

	h.logger.Info("Modified API response",
		"original_ipAddress", originalIP,
		"original_port", originalPort,
		"new ipAddress", apiResp.IPAddress,
		"new_port", apiResp.Port,
	)

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Proxy-Version", "dad_proxy/"+version.AppVersion)
	w.WriteHeader(http.StatusOK)

	modifiedBody, err := json.Marshal(apiResp)
	if err != nil {
		h.logger.Error("Failed to marshal modified response", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	if _, err := w.Write(modifiedBody); err != nil {
		h.logger.Error("Failed to write response to client", "error", err)
	}

	elapsed := time.Since(startTime)
	h.logger.Info("Request completed successfully",
		"user_ip", userIP,
		"duration_ms", elapsed.Milliseconds(),
		"response_size", len(modifiedBody),
	)
}

func (h *ProxyHandler) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	resp := rootResponse{
		App:     "Progulka`s Dark and Darker game proxy",
		Version: version.AppVersion,
		Details: "https://cadiastands.ru",
	}

	body, err := json.Marshal(resp)
	if err != nil {
		h.logger.Error("Failed to marshal root response", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Proxy-Version", "dad_proxy/"+version.AppVersion)
	w.WriteHeader(http.StatusOK)
	if _, err = w.Write(body); err != nil {
		h.logger.Error("Failed to write root response", "error", err)
	}
}

func (h *ProxyHandler) HandleTunnels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	infos := h.tunnelManager.ListTunnels()

	tcpTunnels := make([]tunnelPublicInfo, 0, len(infos))
	udpInfos := make([]tunnels.UDPTunnelStats, 0, len(infos))
	var totalUDPSessions int64
	for _, ti := range infos {
		totalUDPSessions += ti.TotalUDPSessions
		if ti.LocalPort > 0 {
			tcpTunnels = append(tcpTunnels, toTunnelPublicInfo(ti))
		}
		if ti.UDPClientPort > 0 {
			udpInfos = append(udpInfos, tunnels.UDPTunnelStatsFromInfo(ti))
		}
	}

	resp := tunnelsResponse{
		App:              "Progulka`s Dark and Darker game proxy",
		Version:          version.AppVersion,
		Count:            len(tcpTunnels),
		Tunnels:          tcpTunnels,
		UDPTunnelCount:   len(udpInfos),
		TotalUDPSessions: totalUDPSessions,
		UDPTunnels:       udpInfos,
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Proxy-Version", "dad_proxy/"+version.AppVersion)
	w.WriteHeader(http.StatusOK)

	responseBody, err := json.Marshal(resp)
	if err != nil {
		h.logger.Error("Failed to marshal tunnels list", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	if _, err = w.Write(responseBody); err != nil {
		h.logger.Error("Failed to write tunnels list response", "error", err)
	}
}

func (h *ProxyHandler) Close() {
	h.tunnelManager.Close()
}

func (h *ProxyHandler) getClientIP(r *http.Request) string {
	// Список заголовков в порядке приоритета
	headers := []string{
		"X-Forwarded-For",
		"X-Real-IP",
		"CF-Connecting-IP", // CF
		"True-Client-IP",   // CF
		"X-Forwarded",
		"Forwarded-For",
		"Forwarded",
	}

	for _, header := range headers {
		if value := r.Header.Get(header); value != "" {
			if header == "X-Forwarded-For" {
				ips := strings.Split(value, ",")
				if len(ips) > 0 {
					return strings.TrimSpace(ips[0])
				}
			}
			return strings.TrimSpace(value)
		}
	}

	ip := r.RemoteAddr

	if idx := strings.LastIndex(ip, ":"); idx != -1 {
		ip = ip[:idx]
	}

	ip = strings.Trim(ip, "[]")

	return ip
}

func (h *ProxyHandler) formatHeaders(headers http.Header) string {
	if len(headers) == 0 {
		return "{}"
	}

	var sb strings.Builder
	sb.WriteString("{")
	i := 0
	for name, values := range headers {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(fmt.Sprintf("%s: %s", name, strings.Join(values, ", ")))
		i++
	}
	sb.WriteString("}")
	return sb.String()
}
