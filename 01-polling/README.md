# 01-polling

クライアントが一定間隔でサーバーに `GET /poll` を送り、毎回新しいメッセージを取得する方式。

## 仕様

| 項目 | 値 |
|---|---|
| プロトコル | HTTP/1.1 |
| エンドポイント | `GET /poll` |
| ポーリング間隔 | 5秒（デフォルト） |
| ペイロードサイズ | 100バイト（JSON 内 `payload` フィールド） |
| サーバー側集計間隔 | 10秒（CSVに追記） |

### メッセージ形式

```json
{
  "seq": 12345,
  "ts": "2026-05-02T21:04:00.123Z",
  "payload": "xxxx...(100 bytes)..."
}
```

## ローカル起動

### サーバー

リポジトリルートから：

```bash
cd 01-polling
go run ./server
```

- Listen: `:8080`（`ADDR` 環境変数で変更可）
- メトリクス出力先: `./results/server-metrics-polling-<日時>.csv`（`RESULTS_DIR` で変更可）

### クライアント

別ターミナルで：

```bash
cd 01-polling
go run ./client -server http://localhost:8080 -interval 5s
```

- 12回受信ごとに1行ログ出力（`-verbose` で毎回出力）
- `Ctrl+C` で終了 → サマリ表示

## 測定手順（1時間バッテリー測定）

1. **サーバー起動（Mac 上）**
   ```bash
   cd 01-polling
   go run ./server
   ```
2. **クライアントを Android 用にクロスコンパイル**
   ```bash
   GOOS=android GOARCH=arm64 go build -o polling-client ./client
   ```
3. **Termux に配置して実行**
   ```bash
   # Termux 上
   ./polling-client -server http://<MacのIP>:8080 -interval 5s
   ```
4. Android を 100% 充電 → ケーブルを抜く → 画面 OFF → 1時間放置
5. 1時間後、`Ctrl+C` 等でクライアント終了 → サマリを記録
6. サーバー側は `Ctrl+C` で停止 → CSV を確認

## 観察ポイント

- **クライアント側**: 1時間で何 % 電池が減ったか / 受信メッセージ数（720回想定）/ 受信バイト数 / プロセス CPU 時間
- **サーバー側**: 平均・ピーク CPU 使用率 / メモリ使用量 / goroutine 数の推移
- **ネットワーク**: 1リクエストごとに TCP 接続が立ち上がる（HTTP/1.1 の挙動）。Keep-Alive が効いているか要確認

## この方式の特徴（事前予想）

- **電池への影響**: 5秒ごとの TCP 接続確立で wakeup が頻発し、Doze に入りにくいはず → 電池消費は **多めの想定**
- **サーバーへの影響**: 接続が短命なので goroutine 数は安定、メモリは小、CPU は接続セットアップ分が支配的
- **遅延**: ポーリング間隔ぶんの最大遅延（最悪 5秒、平均 2.5秒）
