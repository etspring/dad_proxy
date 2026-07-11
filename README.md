# dad_proxy

Прокси-сервер для игры Dark and Darker: подмена `helloWorld`, TCP/UDP-туннели к игровым серверам, мониторинг сессий и инъекция объявлений через protobuf.

## Установка (Linux)

```bash
curl -fsSL https://raw.githubusercontent.com/etspring/dad_proxy/main/install.sh | sh
```

Конкретная версия: `VERSION=v1.1.3 curl -fsSL ... | sh`. Удаление: `curl -fsSL ... | sh -s -- uninstall`.

После установки отредактируйте `/etc/dad_proxy/dad_proxy.env` (в первую очередь `DAD_PROXY_IP`).

Тестовый proxy: `144.124.242.135`

Для работы с ним измените файл hosts:

```
144.124.242.135 live-gateway.lunatichigh.net
```

Сборка и запуск: [BUILD.md](BUILD.md), [RUN.md](RUN.md).

## Эндпоинты

| Метод | Путь | Назначение |
|-------|------|------------|
| `GET` | `/` | Информация о сервисе и версии |
| `GET` | `DAD_PROXY_API_HELLO` (по умолчанию `/dc/helloWorld`) | Прокси к DaD API, поднятие/переиспользование туннеля |
| `GET` | `/api/tunnels` | Активные туннели, UDP-ноги, игроки в матче |
| `GET` | `/api/sessions` | Активные TCP-сессии лобби (ник, accountId, туннель) |
| `POST` | `/api/announce` | Рассылка `S2C_OPERATE_ANNOUNCE_NOT` клиентам в лобби/игре |

Заголовок ответа: `X-Proxy-Version: dad_proxy/<version>`.

### `GET /`

```json
{
  "app": "Progulka`s Dark and Darker game proxy",
  "version": "1.1.3",
  "details": "https://cadiastands.ru"
}
```

### `GET /api/sessions`

Список игроков на **TCP**-туннелях лобби (логин, выбор персонажа, матчмейкинг). Данные из protobuf-пакетов (`ACCOUNT_LOGIN`, `CHARACTER_LIST`, `LOBBY_ENTER`, `LOBBY_CHARACTER_INFO`).

```json
{
  "count": 1,
  "sessions": [
    {
      "peer": "95.24.181.120:5808",
      "tunnelPort": 20201,
      "accountId": "4048673",
      "characterId": "17959245",
      "nickName": "BogKuzya",
      "connectedAt": "2026-07-10T18:29:38Z",
      "updatedAt": "2026-07-10T18:34:05Z"
    }
  ]
}
```

### `POST /api/announce`

Тело запроса:

```json
{
  "message": "Текст объявления",
  "designDataId": "",
  "params": [],
  "tunnelPort": 0
}
```

- `tunnelPort: 0` - все TCP-туннели; иначе только указанный `localPort`.
- Если задан `DAD_PROXY_ANNOUNCE_TOKEN`, нужен заголовок `X-Announce-Token`.

Ответ: `{"sent": 3, "tunnelPort": 0}` - число клиентов, в очередь которых поставлен кадр.

## Общая схема работы

Пользователь через hosts подменяет IP для `live-gateway.lunatichigh.net` на IP прокси.

1. Клиент шлёт `GET http://live-gateway.lunatichigh.net/dc/helloWorld`.
2. Прокси копирует заголовки и определяет IP пользователя.
3. Прокси делает `GET` в `DAD_API_URL`.
4. Из ответа берутся `ipAddress` и `port`.
5. Прокси поднимает TCP-туннель на порту из `DAD_PROXY_PORTS_RANGE` (или переиспользует существующий).
   - При `underMaintenance != 0` туннель не создаётся.
6. Клиенту возвращается JSON: `ipAddress` -> `DAD_PROXY_IP`, `port` -> локальный порт туннеля, `remote` -> IP пользователя.
7. Трафик лобби идёт через TCP-туннель; при входе в матч - через UDP-ногу (split: клиент -> `udpClientPort`, upstream - ephemeral сокет на `DAD_PROXY_IP`).
8. При `DAD_PROXY_TCP_PAYLOAD_REWRITE=true` (по умолчанию) в TCP-потоке подменяются адреса игровых серверов и парсятся protobuf-кадры (ник, карта, announce).

## Формат `/api/tunnels`

`GET` возвращает JSON:

- `count` - число TCP-туннелей (`localPort > 0`);
- `tunnels` - метрики TCP и при наличии UDP-ноги поля `udpClientPort`, счётчики;
- `udpTunnelCount`, `udpTunnels` - сводка по UDP (включая чисто UDP-туннели к портам из `DAD_PROXY_UDP_PORTS_RANGE`);
- `totalUdpSessions` - суммарное число UDP-сессий.

В `udpTunnels[]` для игроков в матче:

- `createdAt`, `lastActivityAt` - время создания UDP-ноги и последней UDP-активности;
- `players` - ник и выбранная карта (`dungeonIdTag` из лобби до `ENTER_GAME`).

Пример:

```json
{
  "app": "Progulka`s Dark and Darker game proxy",
  "version": "1.1.3",
  "count": 1,
  "totalUdpSessions": 2,
  "tunnels": [
    {
      "remoteIp": "35.71.175.214",
      "remotePort": 20202,
      "localPort": 20200,
      "udpClientPort": 7701,
      "createdAt": "2026-07-10T18:29:00Z",
      "lastActivityAt": "2026-07-10T18:35:00Z",
      "activeTcpConnections": 1,
      "totalTcpConnections": 4,
      "bytesFromClientsToRemote": 1048576,
      "bytesFromRemoteToClients": 983040
    }
  ],
  "udpTunnelCount": 1,
  "udpTunnels": [
    {
      "remoteIp": "52.1.2.3",
      "remotePort": 7777,
      "localPort": 20200,
      "udpClientPort": 7701,
      "createdAt": "2026-07-10T18:35:00Z",
      "lastActivityAt": "2026-07-10T18:40:00Z",
      "upstreamAddr": "52.1.2.3:7777",
      "activeSessions": 1,
      "totalSessions": 1,
      "datagramsFromClients": 50,
      "datagramsToClients": 49,
      "players": [
        {
          "nickName": "BogKuzya",
          "currentMap": "IceCavern"
        }
      ]
    }
  ]
}
```

`currentMap` - сырой `dungeonIdTag` из лобби (например `GoblinCave`, `Ruins`, `Inferno`). Появляется после выбора карты и входа в матч; при random matchmaking может отсутствовать.

### UDP idle timeout

`DAD_PROXY_UDP_IDLE_TIMEOUT` (по умолчанию `10m`) закрывает UDP-ногу, если **нет UDP-датаграмм** дольше интервала. TCP-активность лобби таймер не продлевает. Значение `0` отключает закрытие.

## Protobuf

Схемы в `proto/` (из `DungeonCrawler.exe`), Go-код в `internal/pb/`. Генерация: `./scripts/genproto.sh` (см. [BUILD.md](BUILD.md)).

Используется для:

- парсинга identity на TCP (ник, accountId, выбор карты);
- привязки игрока к UDP-туннелю на `S2C_ENTER_GAME_SERVER_NOT`;
- инъекции `S2C_OPERATE_ANNOUNCE_NOT` в TCP-поток клиента.

Формат игрового TCP-кадра: `[u32 total_len][u16 packet_id][u16 0][protobuf...]`.

## Переменные среды

| Переменная | По умолчанию | Описание |
|------------|--------------|----------|
| `DAD_PROXY_API_PORT` | `80` | Порт HTTP API |
| `DAD_PROXY_API_HELLO` | `/dc/helloWorld` | Путь helloWorld на прокси |
| `DAD_PROXY_PORTS_RANGE` | `20200,20300` | Локальные TCP-порты туннелей |
| `DAD_API_URL` | `http://live-gateway.lunatichigh.net/dc/helloWorld` | Upstream helloWorld |
| `DAD_PROXY_IP` | `127.0.0.1` | IP, отдаваемый клиенту в `ipAddress` |
| `DAD_PROXY_SHARE` | `true` | Отправка конфига на share endpoint при старте |
| `DAD_PROXY_ENVIRONMENT` | `development` | Окружение логирования |
| `DAD_PROXY_UDP_PORTS_RANGE` | `7700,8000` | Диапазон UDP-портов игровых серверов |
| `DAD_PROXY_UDP_CLIENT_BIND_RANGE` | `7700,8000` | Локальные UDP-порты для клиентов |
| `DAD_PROXY_UDP_IDLE_TIMEOUT` | `10m` | Idle-закрытие UDP-ноги (`0` - выкл.) |
| `DAD_PROXY_TCP_PAYLOAD_REWRITE` | `true` | Подмена адресов и парсинг protobuf в TCP |
| `DAD_PROXY_ANNOUNCE_TOKEN` | *(пусто)* | Токен для `POST /api/announce` |

Пример env: `deploy/systemd/dad_proxy.env.example`.

## История версий

См. [CHANGELOG.md](CHANGELOG.md).
