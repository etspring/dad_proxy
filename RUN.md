# dod_proxy
Запуск на Linux. Винды серверной увы ( или к счастью ) нет - проверить негде.

## Запуск через systemd

Готовый юнит: `deploy/systemd/dad_proxy.service`.

### Подготовка бинаря и каталогов

```bash
sudo mkdir -p /opt/dad_proxy /etc/dad_proxy
sudo cp build/dad_proxy /opt/dad_proxy/dad_proxy
sudo chmod +x /opt/dad_proxy/dad_proxy
```

### Добавление пользователя

```bash
sudo useradd --system --no-create-home --shell /usr/sbin/nologin dad_proxy
```

### ENV

```bash
sudo cp deploy/systemd/dad_proxy.env.example /etc/dad_proxy/dad_proxy.env
```

Отредактировать переменные среды в  `/etc/dad_proxy/dad_proxy.env`.

### Установка unit

Перед установкой создать /var/log/dad_proxy/ 

```bash
mkdir -p /var/log/dad_proxy/
```

или отредактировать StandardOutput/StandardError

```bash
sudo cp deploy/systemd/dad_proxy.service /etc/systemd/system/dad_proxy.service
sudo systemctl daemon-reload
sudo systemctl enable --now dad_proxy
```

### Логи

Пишутся в `/var/log/dad_proxy`:

- `/var/log/dad_proxy/service.log` - stdout
- `/var/log/dad_proxy/error.log` - stderr
