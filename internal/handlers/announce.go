package handlers

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"dad_proxy/internal/protocol"
	"dad_proxy/internal/tunnels"
)

// AnnounceHandler обрабатывает инъекцию глобальных объявлений в TCP-туннели.
type AnnounceHandler struct {
	logger        *slog.Logger
	tunnelManager *tunnels.Manager
	announceToken string
}

func NewAnnounceHandler(logger *slog.Logger, tunnelManager *tunnels.Manager, announceToken string) *AnnounceHandler {
	return &AnnounceHandler{
		logger:        logger,
		tunnelManager: tunnelManager,
		announceToken: strings.TrimSpace(announceToken),
	}
}

type announceRequest struct {
	Message      string   `json:"message"`
	DesignDataID string   `json:"designDataId"`
	Params       []string `json:"params"`
	TunnelPort   int      `json:"tunnelPort"`
}

type announceResponse struct {
	Sent       int `json:"sent"`
	TunnelPort int `json:"tunnelPort"`
}

// HandleAnnounce принимает POST /api/announce и рассылает SS2C_OPERATE_ANNOUNCE_NOT.
func (h *AnnounceHandler) HandleAnnounce(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if h.announceToken != "" && r.Header.Get("X-Announce-Token") != h.announceToken {
		h.logger.Warn("announce rejected: invalid token",
			"remote", r.RemoteAddr,
		)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 64*1024))
	if err != nil {
		h.logger.Warn("announce rejected: read body failed",
			"remote", r.RemoteAddr,
			"error", err,
		)
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return
	}

	var req announceRequest
	if err := json.Unmarshal(body, &req); err != nil {
		h.logger.Warn("announce rejected: invalid json",
			"remote", r.RemoteAddr,
			"error", err,
		)
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	h.logger.Info("announce request received",
		"remote", r.RemoteAddr,
		"tunnel_port", req.TunnelPort,
		"message", req.Message,
		"design_data_id", req.DesignDataID,
		"params", req.Params,
	)

	protoBody, err := protocol.BuildOperateAnnounceBody(protocol.AnnounceRequest{
		Message:      req.Message,
		DesignDataID: req.DesignDataID,
		Params:       req.Params,
	})
	if err != nil {
		h.logger.Warn("announce rejected: build protobuf failed",
			"remote", r.RemoteAddr,
			"error", err,
		)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	stats := h.tunnelManager.BroadcastAnnounce(req.TunnelPort, protoBody)
	h.logger.Info("announce dispatch finished",
		"remote", r.RemoteAddr,
		"tunnel_port", req.TunnelPort,
		"message", req.Message,
		"design_data_id", req.DesignDataID,
		"proto_body_len", len(protoBody),
		"queued", stats.Queued,
		"queue_full", stats.QueueFull,
		"tcp_sessions", stats.TCPSessions,
		"tunnels", stats.Tunnels,
	)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(announceResponse{
		Sent:       stats.Queued,
		TunnelPort: req.TunnelPort,
	})
}
