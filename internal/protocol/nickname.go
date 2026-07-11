package protocol

import "dad_proxy/internal/pb"

// DisplayNickName возвращает отображаемый ник из SACCOUNT_NICKNAME.
//
// Назначение: единая логика выбора ника (оригинал или streaming-mode)
// Используется: при разборе TCP-пакетов логина/лобби для привязки к туннелю
func DisplayNickName(n *pb.SACCOUNT_NICKNAME) string {
	if n == nil {
		return ""
	}
	if nick := n.GetOriginalNickName(); nick != "" {
		return nick
	}
	return n.GetStreamingModeNickName()
}
