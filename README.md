# dad_proxy

Прокси-сервер для игры Dark and Darker.

## Эндпоинты

- `GET /`
  - Информация о сервисе:
  - `{"app":"Progulka`s Dark and Darker game proxy","version":"1.1.0","details":"https://cadiastands.ru"}`
- `GET /dc/helloWorld`
  - Единственный endpoint, который обращается к внешнему DaD API и поднимает/переиспользует туннель.
- `GET /api/tunnels`
  - Возвращает список активных туннелей и метрики.

## Общая схема работы

Пользователь через файл hosts подменяет ip для live-gateway.lunatichigh.net, указывая IP proxy-сервера.

1. Клиент игры отправляет запрос на `http://live-gateway.lunatichigh.net/dc/helloWorld`.
2. Proxy копирует заголовки запроса и определяет реальный IP пользователя.
3. Proxy делает `GET` в `DAD_API_URL`.
4. Из ответа DaD API берутся `ipAddress` и `port`.
5. Proxy поднимает TCP-туннель на локальном порту из `DAD_PROXY_PORTS_RANGE`:
   - если туннель к `ipAddress:port` уже существует, новый не создается;
   - если нет, создается новый.
   - если `underMaintenance != 0`, туннель не поднимается и не переиспользуется.
6. Proxy возвращает клиенту модифицированный JSON:
   - `ipAddress` -> `DAD_PROXY_IP`;
   - `port` -> локальный порт поднятого туннеля;
   - `remote` -> IP пользователя.
7. Трафик от пользователя до игровых серверов ( Таверны ) идет через поднятый туннель.
8. При запуске игры из Таверны происходит перехват трафика и подмена адресов игровых серверов
9. Начинается обмен данными клиент <- UDP -> Прокси <- UDP -> сервер игры

Примечание: при `underMaintenance != 0` поле `port` остается из ответа DaD API, так как туннель в этом режиме отключен
  ибо корейцы катят обновку.


## Формат `/api/tunnels`

Пример ответа:

```json
{
  "app": "Progulka`s Dark and Darker game proxy",
  "version": "1.0.0",
  "count": 1,
  "tunnels": [
    {
      "remoteIp": "35.71.175.214",
      "remotePort": 20202,
      "localPort": 20200,
      "createdAt": "2026-05-08T08:53:06Z",
      "lastActivityAt": "2026-05-08T08:53:20Z",
      "activeTcpConnections": 1,
      "totalTcpConnections": 4,
      "activeUdpSessions": 0,
      "totalUdpSessions": 2,
      "udpDatagramsFromClients": 50,
      "udpDatagramsToClients": 49,
      "bytesFromClientsToRemote": 1048576,
      "bytesFromRemoteToClients": 983040
    }
  ]
}
```

## Переменные среды

- `DAD_PROXY_API_PORT` - порт HTTP API прокси (по умолчанию `80`).
- `DAD_PROXY_PORTS_RANGE` - диапазон локальных портов для TCP-туннелей в формате `start,end` (по умолчанию `20200,20300`).
- `DAD_API_URL` - upstream URL `helloWorld` (по умолчанию `http://live-gateway.lunatichigh.net/dc/helloWorld`).
- `DAD_PROXY_IP` - публичный IP прокси, который отдается клиенту в `ipAddress` (по умолчанию `127.0.0.1`).
- `DAD_PROXY_SHARE` - отправлять ли информацию о прокси во внешний share endpoint при старте (`true` по умолчанию).
- `DAD_PROXY_ENVIRONMENT` - окружение логирования (`development` по умолчанию).
- `DAD_PROXY_UDP_PORTS_RANGE` - диапазон UDP-портов игровых верверов в формате `start,end` (по умолчанию `7700,8000`).
- `DAD_PROXY_UDP_CLIENT_BIND_RANGE` - диапазон локальных портов для UDP-туннелей в формате `start,end` (по умолчанию `7700,8000`).

В этом варианте шаг с созданием пользователя `dad_proxy` можно пропустить.