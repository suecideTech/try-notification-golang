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

## 測定条件

- **クライアント**: Pixel5a + Termux で Go バイナリ実行
- **サーバー**: GCE (Tokyo) / Go 1.25.4
- **ネットワーク**: キャリア通信のみ（Wi-Fi OFF / USB 物理切断）
- **負荷**: 5 秒に 1 回 / 100 バイト
- **時間**: 画面 OFF + wakelock で 1 時間放置
- **電池指標**: `dumpsys battery` の Charge counter（μAh）

## 結果サマリ

| 順位 | 方式 | 電池減少 | クライアント CPU |
|---|---|---|---|
| 🥇 | longpolling | -114,006 μAh | 2.45 sec |
| 🥈 | SSE | -117,528 μAh | 0.44 sec |
| 🥈 | WebSocket | -117,528 μAh | 0.50 sec |
| 4 | grpcstream | -121,304 μAh | 1.14 sec |
| 5 | httpstream | -122,957 μAh | 0.72 sec |
| 6 | polling | -123,428 μAh | 4.15 sec |

最少と最多の差は **約 8.3%**。

## 主な発見

- **CPU 軽い ≠ 電池に優しい**：SSE は CPU 最軽量だが、longpolling より電池を食う
- **TCP 接続維持が効く**：polling のように毎回新規 TCP を張ると、モバイル NW では RRC promotion で radio が起き続ける
- 結論：「TCP 接続維持は電池消費を抑える最強の手段」

## 使い方

各ディレクトリで `go run ./server` / `go run ./client` で動く。詳細は各方式の `README.md` を参照。
