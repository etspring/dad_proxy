package protocol

import (
	"fmt"

	"dad_proxy/internal/pb"
	"google.golang.org/protobuf/proto"
)

// AnnounceRequest описывает одно объявление для клиентов.
type AnnounceRequest struct {
	Message      string
	DesignDataID string
	Params       []string
}

// BuildOperateAnnounceBody сериализует тело SS2C_OPERATE_ANNOUNCE_NOT.
func BuildOperateAnnounceBody(req AnnounceRequest) ([]byte, error) {
	if req.Message == "" && req.DesignDataID == "" {
		return nil, fmt.Errorf("announce message or designDataId is required")
	}

	item := &pb.SANNOUNCE_MESSAGE{}
	if req.DesignDataID != "" {
		item.DesignDataId = proto.String(req.DesignDataID)
	}
	if req.Message != "" {
		item.AnnounceMessage = proto.String(req.Message)
	}
	if len(req.Params) > 0 {
		item.Params = append(item.Params, req.Params...)
	}

	return proto.Marshal(&pb.SS2C_OPERATE_ANNOUNCE_NOT{
		AnnounceList: []*pb.SANNOUNCE_MESSAGE{item},
	})
}

// BuildOperateAnnounceFrame формирует полный TCP-кадр S2C_OPERATE_ANNOUNCE_NOT.
func BuildOperateAnnounceFrame(req AnnounceRequest) ([]byte, error) {
	body, err := BuildOperateAnnounceBody(req)
	if err != nil {
		return nil, err
	}
	return EncodeFrame(uint16(pb.PacketCommand_S2C_OPERATE_ANNOUNCE_NOT), body), nil
}
