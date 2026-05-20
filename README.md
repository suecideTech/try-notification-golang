# try-notification-golang

Go の `GOOS=android GOARCH=arm64` でクロスコンパイルしたバイナリを Termux で動かして、サーバー Push 6 方式の Android 実機電池消費を測定した検証プロジェクト。

## 比較対象

| # | 方式 | ディレクトリ |
|---|---|---|
| 1 | Polling | [01-polling/](01-polling/) |
| 2 | Long Polling | [02-longpolling/](02-longpolling/) |
| 3 | HTTP Streaming | [03-httpstream/](03-httpstream/) |
| 4 | SSE | [04-sse/](04-sse/) |
| 5 | WebSocket | [05-websocket/](05-websocket/) |
| 6 | gRPC server streaming | [06-grpcstream/](06-grpcstream/) |

## 使い方

各ディレクトリで `go run ./server` / `go run ./client` で動く。詳細は各方式の `README.md` を参照。
