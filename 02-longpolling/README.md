# 02-longpolling

クライアントが `GET /poll` を投げてサーバー側で「次のメッセージが発火するまで待機」してから返す方式。返ってきたら即座に次のリクエストを張り直し、コネクションを長く保持する。

## 仕様

| 項目 | 値 |
|---|---|
| プロトコル | HTTP/1.1 |
| エンドポイント | `GET /poll`（サーバーが broadcast or タイムアウトまで待機） |
| broadcast 間隔 | 5秒（サーバー内部の固定値） |
| サーバー側待機タイムアウト | 30秒（broadcast が来なければ 204 No Content） |
| クライアント HTTP タイムアウト | 35秒 |
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
cd 02-longpolling
go run ./server
```

- Listen: `:8080`（`ADDR` 環境変数で変更可）
- メトリクス出力先: `./results/server-metrics-longpolling-<日時>.csv`（`RESULTS_DIR` で変更可）
- 起動と同時に 5秒周期の broadcaster goroutine が走り、購読中の `/poll` リクエスト全員に同じ Message を配る

### クライアント

別ターミナルで：

```bash
cd 02-longpolling
go run ./client -server http://localhost:8080
```

- デフォルトはレスポンスを受けたら即座に次の `/poll` を張る（`-interval` で最小間隔を指定可能）
- 12回受信ごとに1行ログ出力（`-verbose` で毎回出力）
- `Ctrl+C` で終了 → サマリ表示

## 測定手順（1時間バッテリー測定）

1. **サーバー起動（Mac 上）**
   ```bash
   cd 02-longpolling
   go run ./server
   ```
2. **クライアントを Android 用にクロスコンパイル**
   ```bash
   GOOS=android GOARCH=arm64 go build -o longpolling-client ./client
   ```
3. **Termux に配置して実行**
   ```bash
   # Termux 上
   ./longpolling-client -server http://<MacのIP>:8080
   ```
4. Android を 100% 充電 → ケーブルを抜く → 画面 OFF → 1時間放置
5. 1時間後、`Ctrl+C` 等でクライアント終了 → サマリを記録
6. サーバー側は `Ctrl+C` で停止 → CSV を確認

## 観察ポイント

- **クライアント側**: 1時間で何 % 電池が減ったか / 受信メッセージ数（broadcast が 5秒間隔なので最大 720回想定）/ 受信バイト数 / プロセス CPU 時間
- **サーバー側**: 接続が常時保持されるので `active_conns` が常に N（接続クライアント数）に張り付くはず / goroutine 数の推移（リクエスト中はリクエストごとに goroutine 増） / メモリ
- **ネットワーク**: TCP 接続が長時間維持される。Polling と違って接続セットアップが broadcast 周期に依存しないので、再接続頻度はクライアント側のレスポンス受信頻度と一致

## この方式の特徴（事前予想）

- **電池への影響**: TCP 接続を張りっぱなしにするので、ハンドシェイク回数は polling より大幅に少ない（broadcast 周期 = 5秒 のたびに1回再接続）。一方で接続維持中は radio が完全には眠れず、Doze 移行も阻害されがち → polling より少しマシ〜同等程度の予想
- **サーバーへの影響**: クライアント数 × 1 の goroutine が常に待機。100バイト × 数百クライアントでもメモリは小さいが、`active_conns` が常時高止まりするのが polling との大きな違い
- **遅延**: broadcast 発火から最大数十ms（チャネル send + flush）で届く。polling のような「間隔ぶんの平均待ち」は発生しない → **即時性は高い**
- **再接続頻度**: broadcast を1回受けるごとに再接続。サーバー側 timeout（30s）まで何も起きなければ 204 で抜けて即再接続
