# 04-sse

サーバーが 1 本の HTTP コネクション上で `text/event-stream` を使って継続的にイベントを送り続ける方式（Server-Sent Events）。クライアントは `bufio.Scanner` で行単位に読み、`data:` 行をパースしてメッセージを取り出す。

## 仕様

| 項目 | 値 |
|---|---|
| プロトコル | HTTP/1.1（chunked transfer / event-stream） |
| エンドポイント | `GET /events` |
| 送信間隔 | 5秒（サーバー側 ticker） |
| ペイロードサイズ | 100バイト（JSON 内 `payload` フィールド） |
| サーバー側集計間隔 | 10秒（CSVに追記） |
| Content-Type | `text/event-stream` |

### メッセージ形式（SSE フレーム）

```
data: {"seq":12345,"ts":"2026-05-02T21:04:00.123Z","payload":"xxxx...(100 bytes)..."}
\n
```

`data: <JSON>\n\n` を 1 イベントとして繰り返し送信する。

## ローカル起動

### サーバー

リポジトリルートから：

```bash
cd 04-sse
go run ./server
```

- Listen: `:8080`（`ADDR` 環境変数で変更可）
- メトリクス出力先: `./results/server-metrics-sse-<日時>.csv`（`RESULTS_DIR` で変更可）

### クライアント

別ターミナルで：

```bash
cd 04-sse
go run ./client -server http://localhost:8080
```

- 12回受信ごとに1行ログ出力（`-verbose` で毎回出力）
- 接続が切れたら 1秒待って自動再接続
- `Ctrl+C` で終了 → サマリ表示

## 測定手順（1時間バッテリー測定）

1. **サーバー起動（Mac 上）**
   ```bash
   cd 04-sse
   go run ./server
   ```
2. **クライアントを Android 用にクロスコンパイル**
   ```bash
   GOOS=android GOARCH=arm64 go build -o sse-client ./client
   ```
3. **Termux に配置して実行**
   ```bash
   # Termux 上
   ./sse-client -server http://<MacのIP>:8080
   ```
4. Android を 100% 充電 → ケーブルを抜く → 画面 OFF → 1時間放置
5. 1時間後、`Ctrl+C` 等でクライアント終了 → サマリを記録
6. サーバー側は `Ctrl+C` で停止 → CSV を確認

## 観察ポイント

- **クライアント側**: 1時間で何 % 電池が減ったか / 受信メッセージ数（720回想定）/ 受信バイト数 / プロセス CPU 時間
- **サーバー側**: 平均・ピーク CPU 使用率 / メモリ使用量 / goroutine 数の推移（接続が長命なので 1 ハンドラ = 1 goroutine が滞留する）
- **ネットワーク**: TCP コネクションは張りっぱなし。1イベントあたりのバイト数（`data: ` プレフィクス + JSON + `\n\n`）

## この方式の特徴（事前予想）

- **HTTP Streaming（03）との違い**: 03 は生の chunked body をそのまま流すアドホック仕様。SSE は `text/event-stream` という MIME タイプ＋`data:` / `event:` / `id:` / `retry:` といったフィールド規約が決まっており、ブラウザの `EventSource` API がそのまま使える標準仕様。今回の Go クライアント実装上はパース処理（`data: ` プレフィクス + 空行区切り）が増える分だけ違うが、ワイヤ上の挙動は 03 と非常に近い
- **ブラウザ標準対応**: 追加ライブラリなしで Web フロントから `new EventSource('/events')` で受信できるのが最大の利点。一方で Go クライアントから見ると差は限定的
- **再接続セマンティクス**: SSE 仕様では切断時にブラウザが自動再接続し、`Last-Event-ID` ヘッダで再開できる規約がある。今回の Go クライアントは仕様準拠ではなく「切れたら 1 秒待って再接続」だけのシンプル実装
- **電池への影響**: TCP 接続は 1 本で張りっぱなし → ラジオの wakeup は受信時のみ。03-httpstream とほぼ同等で、01-polling より少ない想定
- **サーバーへの影響**: 接続中はクライアントごとに 1 goroutine が常駐 → 多数同時接続時はメモリ・goroutine 数が線形に増える（03 と同様）
- **遅延**: サーバー送信間隔（5秒）= 平均遅延 ≒ 即時。ポーリングのような最大遅延は発生しない
