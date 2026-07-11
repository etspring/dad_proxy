package protocol

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// Ошибки разбора игрового TCP-кадра.
var (
	ErrFrameTooShort = errors.New("game frame is too short")
	ErrFrameSize     = errors.New("invalid game frame size")
)

// ParsePacketID возвращает PacketCommand из заголовка кадра (u16 LE на offset 4).
func ParsePacketID(frame []byte) (uint16, error) {
	if len(frame) < 6 {
		return 0, ErrFrameTooShort
	}
	return binary.LittleEndian.Uint16(frame[4:6]), nil
}

// EncodeFrame собирает TCP-кадр для отправки клиенту.
//
// Формат (как у dndserver + u32-префикс dad_proxy):
//   [u32 total][u16 packet_id][u16 0][protobuf...]
//   protobuf начинается с offset 8
func EncodeFrame(packetID uint16, body []byte) []byte {
	return EncodeFrameWithHeader([8]byte{}, packetID, body, false)
}

// EncodeFrameWithHeader клонирует 8-байтный префикс с реального upstream-кадра.
//
// Если hasHeader=false, собирает префикс с нуля. Иначе берёт байты 4-7 из шаблона
// и подставляет packetID + обновляет u32 длину.
func EncodeFrameWithHeader(header [8]byte, packetID uint16, body []byte, hasHeader bool) []byte {
	frame := make([]byte, 8+len(body))
	if hasHeader {
		copy(frame[:8], header[:])
	} else {
		binary.LittleEndian.PutUint32(frame[0:4], 0)
	}
	binary.LittleEndian.PutUint16(frame[4:6], packetID)
	frame[6] = 0
	frame[7] = 0
	copy(frame[8:], body)
	binary.LittleEndian.PutUint32(frame[0:4], uint32(len(frame)))
	return frame
}

// FrameBody возвращает protobuf-тело из кадра без заголовка.
func FrameBody(frame []byte) ([]byte, error) {
	if len(frame) < 8 {
		return nil, ErrFrameTooShort
	}
	total := int(binary.LittleEndian.Uint32(frame[0:4]))
	if total != len(frame) {
		return nil, fmt.Errorf("%w: declared=%d actual=%d", ErrFrameSize, total, len(frame))
	}
	return frame[8:], nil
}

// HeaderHex возвращает hex первых 8 байт кадра для отладки.
func HeaderHex(frame []byte) string {
	if len(frame) < 8 {
		return ""
	}
	return fmt.Sprintf("%x", frame[:8])
}
