# dad_proxy

## 1.1.0
  - Подмена адресов серверов в tcp-трафике.
  - Проксирование udp-трафика.

## 1.0.0
  - HTTP-проксирование запросов от пользователя ( клиента DaD ) в сторону http://live-gateway.lunatichigh.net/dc/helloWorld
  - Подмена ipAddress и port в ответе оригинального API для отдачи пользователю ( клиента DaD ).
  - Поднятие туннеля между proxy и ipAddress:port, из ответа API.
  - Шаринг proxy ( пока только отправка данных на cadiastands.ru/dad_proxy/share).
    ```
      type ShareData struct {
        ProxyIP     string `json:"proxy_ip"`
        APIURL      string `json:"api_url"`
        ProxyPort   string `json:"proxy_port"`
        Environment string `json:"environment"`
        ProxyShare  bool   `json:"proxy_share"`
        Timestamp   int64  `json:"timestamp"`
      }
    ```
  - Статочка на /api/tunnels ( может использоваться как healthcheck ).
  - Юниты для systemd.
