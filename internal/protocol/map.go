package protocol

import "strings"

// FormatCurrentMap формирует строку карты для API из выбора в лобби.
//
// Назначение: отображает dungeonIdTag, выбранный до входа в матч
// Используется: при привязке игрока к UDP-туннелю на ENTER_GAME
// Аргументы:
//   - dungeonIdTag: тег подземелья из lobby/match пакетов
//   - gameType: тип режима (solo/duo/trio и т.д.), 0 если неизвестен
// Возвращает: dungeonIdTag или пустую строку, если карта не выбрана
func FormatCurrentMap(dungeonIdTag string, gameType uint32) string {
	tag := strings.TrimSpace(dungeonIdTag)
	if tag == "" {
		return ""
	}
	if gameType == 0 {
		return tag
	}
	return tag
}
