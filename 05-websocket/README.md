# 05-websocket

クライアントが WebSocket でサーバーに接続し、サーバーが 5 秒ごとにメッセージをプッシュする方式。
TCP 上に張った 1 本の長期コネクションを双方向に使えるが、本実験ではサーバー → クライアントの一方向プッシュとして利用する。

## 仕様

| 項目 | 値 |
|---|---|
| プロトコル | HTTP/1.1 で Upgrade → WebSocket（RFC 6455） |
| エンドポイント | `GET /ws`（Upgrade） |
| 送信間隔 | 5秒（サーバー側 ticker） |
| ペイロードサイズ | 100バイト（JSON 内 `payload` フィールド） |
| ライブラリ | `github.com/coder/websocket`（旧 nhooyr.io/websocket） |
| サーバー側集計間隔 | 10秒（CSVに追記） |

### メッセージ形式

```json
{
  "seq": 12345,
  "ts": "2026-05-02T21:04:00.123Z",
  "payload": "xxxx...(100 bytes)..."
}
```

各メッセージは WebSocket の **テキストフレーム** として送られる（`wsjson.Write` の挙動）。

## ローカル起動

### サーバー

```bash
cd 05-websocket
go run ./server
```

- Listen: `:8080`（`ADDR` 環境変数で変更可）
- メトリクス出力先: `./results/server-metrics-websocket-<日時>.csv`（`RESULTS_DIR` で変更可）

### クライアント

別ターミナルで：

```bash
cd 05-websocket
go run ./client -server ws://localhost:8080/ws
```

- `-server` は `ws://`/`wss://` でも `http://`/`https://` でも受け付ける（`http://` は `ws://` に自動置換）
- パスが省略 or `/` の場合は `/ws` を補完する
- 12回受信ごとに1行ログ出力（`-verbose` で毎回出力）
- 切断・読み取りエラー時は **1秒待って再接続**
- `Ctrl+C` で終了 → サマリ表示

## 測定手順（1時間バッテリー測定）

1. **サーバー起動（Mac 上）**
   ```bash
   cd 05-websocket
   go run ./server
   ```
2. **クライアントを Android 用にクロスコンパイル**
   ```bash
   GOOS=android GOARCH=arm64 go build -o websocket-client ./client
   ```
3. **Termux に配置して実行**
   ```bash
   # Termux 上
   ./websocket-client -server ws://<MacのIP>:8080/ws
   ```
4. Android を 100% 充電 → ケーブルを抜く → 画面 OFF → 1時間放置
5. 1時間後、`Ctrl+C` でクライアント終了 → サマリを記録
6. サーバー側は `Ctrl+C` で停止 → CSV を確認

## 観察ポイント

- **クライアント側**: 1時間で何 % 電池が減ったか / 受信メッセージ数（720回想定）/ 受信バイト数 / プロセス CPU 時間
- **サーバー側**: `active_conns` が常に 1 に張り付くこと / goroutine 数の推移 / メモリ使用量
- **ネットワーク**: TCP 接続は 1 本のみ。接続確立コストは初回だけで、以降は WebSocket フレーム（数バイトのヘッダ）で送受信される

## この方式の特徴（事前予想）

- **電池への影響**: TCP 接続を 1 本張りっぱなしにするのでハンドシェイクは 1 回だけ。フレームヘッダが極小（〜10B 程度）なので 1 メッセージあたりの送受信バイト数は他方式より小さい。**理屈の上では 03-httpstream / 04-sse と並んでバッテリー効率が良いはず**
- **Doze の影響**: Android が Doze に入るとアプリプロセスが停止し、TCP 接続は維持されるが pong 応答が止まるため、サーバーの read deadline 次第で切断される可能性がある。本実装ではアプリ側でハートビートを明示しておらず、`coder/websocket` のデフォルト挙動（自動 ping/pong は無し）に任せている。Doze 中に切断された場合はクライアント側で 1 秒後に再接続するため、**Doze 解除時にまとめて recv が走る**挙動になる見込み
- **サーバーへの影響**: 接続ごとに 1 goroutine が常駐。1 接続あたりのメモリは小さいが、`active_conns` × メモリで効くので接続数が増えると linear に増える
- **遅延**: サーバーがプッシュした瞬間に届く（理論上は片道 RTT 程度）。ポーリング系より大幅に低遅延
- **双方向性**: 本実験では使っていないが、クライアント → サーバーの送信も同じ 1 本のコネクションで行える点が他方式との大きな違い
