# 03-httpstream

クライアントが一度 `GET /stream` で接続するとサーバー側がその接続を維持し続け、
HTTP/1.1 chunked transfer encoding でメッセージを定期的に push する方式（NDJSON ストリーム）。

## 仕様

| 項目 | 値 |
|---|---|
| プロトコル | HTTP/1.1 chunked transfer encoding |
| エンドポイント | `GET /stream` |
| Content-Type | `application/x-ndjson` |
| 配信間隔 | 5秒（サーバー側 `-interval` で変更可） |
| ペイロードサイズ | 100バイト（JSON 内 `payload` フィールド） |
| サーバー側集計間隔 | 10秒（CSVに追記） |
| 再接続 | クライアント側で 1秒待って自動再接続 |

### メッセージ形式

1行 = 1 JSON メッセージ（NDJSON）。

```json
{"seq":12345,"ts":"2026-05-02T21:04:00.123Z","payload":"xxxx...(100 bytes)..."}
```

## ローカル起動

### サーバー

リポジトリルートから：

```bash
cd 03-httpstream
go run ./server
```

- Listen: `:8080`（`ADDR` 環境変数で変更可）
- メトリクス出力先: `./results/server-metrics-httpstream-<日時>.csv`（`RESULTS_DIR` で変更可）
- 配信間隔は `-interval 5s` で変更可

### クライアント

別ターミナルで：

```bash
cd 03-httpstream
go run ./client -server http://localhost:8080
```

- 12回受信ごとに1行ログ出力（`-verbose` で毎回出力）
- 接続が切れたら 1秒待って自動再接続（再接続回数はサマリに出る）
- `Ctrl+C` で終了 → サマリ表示

## 測定手順（1時間バッテリー測定）

1. **サーバー起動（Mac 上）**
   ```bash
   cd 03-httpstream
   go run ./server
   ```
2. **クライアントを Android 用にクロスコンパイル**
   ```bash
   GOOS=android GOARCH=arm64 go build -o httpstream-client ./client
   ```
3. **Termux に配置して実行**
   ```bash
   # Termux 上
   ./httpstream-client -server http://<MacのIP>:8080
   ```
4. Android を 100% 充電 → ケーブルを抜く → 画面 OFF → 1時間放置
5. 1時間後、`Ctrl+C` 等でクライアント終了 → サマリを記録
6. サーバー側は `Ctrl+C` で停止 → CSV を確認

## 観察ポイント

- **クライアント側**: 1時間で何 % 電池が減ったか / 受信メッセージ数（720回想定）/ 受信バイト数 / 再接続回数 / プロセス CPU 時間
- **サーバー側**: 接続中の goroutine 数（`active_conns` × 2 程度になるはず）/ 平均・ピーク CPU 使用率 / メモリ使用量
- **ネットワーク**: TCP 接続が長期間維持され、5秒ごとに小さな chunk が流れる挙動を確認。NAT/中継のアイドルタイムアウトで切られないか観察

## この方式の特徴（事前予想）

- **電池への影響**: TCP 接続を張りっぱなしにするため、接続確立コストはほぼゼロ。受信のみで wakeup できるので、ポーリングより電池に優しい想定。ただし NAT keepalive のための ACK が背景で流れるので、Doze に深く入れるかは環境依存
- **サーバーへの影響**: 接続ごとに長期 goroutine が常駐するため、`goroutines` と `active_conns` がほぼ同数で安定。クライアント数 N に対しメモリは O(N)、暇な時間の CPU はほぼゼロ
- **遅延**: サーバーが push した瞬間に届くので、配信間隔以下の遅延は発生しない（実質ほぼゼロ）
- **再接続**: NAT/プロキシ/サーバー再起動で切れる可能性があり、クライアント側の再接続ロジックが効いているか `reconnects` で確認できる
- **比較ポイント**: 02-longpolling と比べると「リクエスト→レスポンス→再リクエスト」の山が無く、純粋な受信のみ。Doze 中の挙動差が一番見えそう
