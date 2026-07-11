package handlers

import (
	"encoding/json"
	"net/http"

	"dad_proxy/internal/tunnels"
	"dad_proxy/internal/version"
)

type sessionsResponse struct {
	App      string                      `json:"app"`
	Version  string                      `json:"version"`
	Count    int                         `json:"count"`
	Sessions []tunnels.TCPSessionIdentity `json:"sessions"`
}

// HandleSessions возвращает активные TCP-сессии с ником и привязкой к туннелю.
func (h *ProxyHandler) HandleSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sessions := h.tunnelManager.ListTCPSessions()
	resp := sessionsResponse{
		App:      "Progulka`s Dark and Darker game proxy",
		Version:  version.AppVersion,
		Count:    len(sessions),
		Sessions: sessions,
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Proxy-Version", "dad_proxy/"+version.AppVersion)
	w.WriteHeader(http.StatusOK)

	body, err := json.Marshal(resp)
	if err != nil {
		h.logger.Error("Failed to marshal sessions list", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	if _, err = w.Write(body); err != nil {
		h.logger.Error("Failed to write sessions list response", "error", err)
	}
}
