# dad_proxy
  Сборка

## Требования

- **Go >= 1.22** (в `go.mod` указано `go 1.22.0`)
- В Ubuntu/Debian пакет `golang-go` часто даёт Go 1.18 - для `make build` этого мало

## Linux (WSL)

```bash
make setup-go    # один раз, если системный go < 1.22
make build
make test
./build/dad_proxy
```

`Makefile` подхватывает `~/.local/go1.22.12/bin/go`, если он установлен.

## Protobuf

Схемы в `proto/` (из `DungeonCrawler.exe`). Генерация Go-кода:

```bash
./scripts/genproto.sh
```

Подробнее: [docs/PROTOBUF.md](docs/PROTOBUF.md)

## Windows

Нужен Go 1.22+ с https://go.dev/dl/
