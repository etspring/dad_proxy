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

`GET` возвращает JSON со сводкой по активным туннелям:

- `count` - число записей в `tunnels` (только TCP: у туннеля есть `localPort`);
- `tunnels` - снимки TCP-туннелей (метрики TCP и, если есть UDP-нога, поля `udpClientPort`, `udpLocalListenAddr`, счётчики UDP/байтов);
- `udpTunnelCount` - число записей в `udpTunnels`;
- `udpTunnels` - отдельная сводка по UDP (включая чисто UDP-туннели к игровым портам из `DAD_PROXY_UDP_PORTS_RANGE`, которые в `tunnels` не попадают).

Пример ответа:

```json
{
  "app": "Progulka`s Dark and Darker game proxy",
  "version": "1.1.2",
  "count": 1,
  "tunnels": [
    {
      "remoteIp": "35.71.175.214",
      "remotePort": 20202,
      "localPort": 20200,
      "udpClientPort": 7701,
      "udpLocalListenAddr": "0.0.0.0:7701",
      "createdAt": "2026-05-08T08:53:06Z",
      "lastActivityAt": "2026-05-08T08:53:20Z",
      "activeTcpConnections": 1,
      "totalTcpConnections": 4,
      "activeUdpSessions": 2,
      "totalUdpSessions": 5,
      "udpDatagramsFromClients": 50,
      "udpDatagramsToClients": 49,
      "bytesFromClientsToRemote": 1048576,
      "bytesFromRemoteToClients": 983040
    }
  ],
  "udpTunnelCount": 2,
  "udpTunnels": [
    {
      "remoteIp": "35.71.175.214",
      "remotePort": 20202,
      "localPort": 20200,
      "udpClientPort": 7701,
      "localListenAddr": "0.0.0.0:7701",
      "upstreamAddr": "35.71.175.214:20202",
      "activeSessions": 2,
      "totalSessions": 5,
      "datagramsFromClients": 50,
      "datagramsToClients": 49
    },
    {
      "remoteIp": "10.0.0.5",
      "remotePort": 7777,
      "localPort": 0,
      "udpClientPort": 7702,
      "localListenAddr": "0.0.0.0:7702",
      "upstreamAddr": "10.0.0.5:7777",
      "activeSessions": 1,
      "totalSessions": 1,
      "datagramsFromClients": 12,
      "datagramsToClients": 11
    }
  ]
}
```

Поля `udpClientPort`, `udpLocalListenAddr` / `localListenAddr` и блок `udpTunnels` присутствуют только если у туннеля поднята UDP-нога. `version` совпадает с `internal/version` и заголовком `X-Proxy-Version`.

## Переменные среды

Настройки читаются при старте из окружения процесса (например `deploy/systemd/dad_proxy.env.example`). Диапазоны портов задаются как `start,end` (оба конца включительно, `1..65535`, `start <= end`).

**HTTP и helloWorld**

- `DAD_PROXY_API_PORT` - порт HTTP API прокси (`/`, `/dc/helloWorld`, `/api/tunnels`; по умолчанию `80`).
- `DAD_API_URL` - полный URL upstream для проксирования `helloWorld` (по умолчанию `http://live-gateway.lunatichigh.net/dc/helloWorld`).
- `DAD_PROXY_IP` - IP, который прокси подставляет клиенту в `ipAddress` и в игровые payload; также IPv4 для bind туннелей, если задан не `0.0.0.0` / не unspecified (по умолчанию `127.0.0.1`).

**TCP-туннели (Таверна и прочий TCP)**

- `DAD_PROXY_PORTS_RANGE` - диапазон локальных портов для входящих TCP-соединений клиента (по умолчанию `20200,20300`). Для upstream-порта из этого же диапазона UDP-релей на том же номере порта не поднимается (только TCP).
- `DAD_PROXY_PORTS_RANGE_START` / `DAD_PROXY_PORTS_RANGE_END` - альтернатива `DAD_PROXY_PORTS_RANGE`: задать границы двумя переменными (нужны обе; если задана любая из пары, `DAD_PROXY_PORTS_RANGE` не используется).

**UDP и изменение TCP-payload**

- `DAD_PROXY_UDP_PORTS_RANGE` - диапазон **upstream**-портов игровых серверов, которые считаются игровым UDP: для них создаются split UDP-туннели и переписываются адреса в TCP-трафике туннеля (по умолчанию `7700,8000`). Порты, попадающие и в этот диапазон, и в `DAD_PROXY_PORTS_RANGE`, UDP не получают.
- `DAD_PROXY_UDP_CLIENT_BIND_RANGE` - диапазон **локальных** UDP-портов на прокси, на которые клиент шлёт игровой UDP (`udpClientPort` в `/api/tunnels` и в payload; по умолчанию `7700,8000`).
- `DAD_PROXY_TCP_PAYLOAD_REWRITE` - включить перепись адресов внутри TCP-кадров туннеля (TLV/protobuf, URL; по умолчанию `true`). `false` отключает подмену, туннели остаются прозрачными по байтам.

**Логи и регистрация**

- `DAD_PROXY_ENVIRONMENT` - `production`: JSON-логи в stdout; иначе (в т.ч. `development` по умолчанию): текстовые логи и `AddSource` в записях.
- `DAD_PROXY_SHARE` - при `true` (по умолчанию) асинхронно отправить конфиг на `https://cadiastands.ru/dad_proxy/share` при старте; `false` отключает.