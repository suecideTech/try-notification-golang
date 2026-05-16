# 06-grpcstream

サーバーが gRPC server-streaming RPC でクライアントへ一定間隔でメッセージを push する方式。

## 仕様

| 項目 | 値 |
|---|---|
| プロトコル | HTTP/2（gRPC, plaintext） |
| サービス / RPC | `notify.Notifier/Stream`（server-streaming） |
| Listen ポート | `:50051` |
| 送信間隔 | 5秒（サーバー側の `time.Ticker`） |
| ペイロードサイズ | 100バイト（`Message.payload`） |
| サーバー側集計間隔 | 10秒（CSVに追記） |

### proto 定義（`proto/notify.proto`）

```proto
syntax = "proto3";

package notify;

option go_package = "github.com/y-hashisaka/try-notification-golang/06-grpcstream/notifypb";

service Notifier {
  rpc Stream(StreamRequest) returns (stream Message);
}

message StreamRequest {}

message Message {
  int64 seq = 1;
  string ts = 2;
  string payload = 3;
}
```

## コード生成

`notifypb/*.pb.go` はリポジトリにコミットしているので、通常のビルドでは再生成は不要。proto を変更したときだけ以下を実行する。

### 必要なツール

```bash
# protoc 本体（macOS）
brew install protobuf

# Go プラグイン
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

# プラグインのインストール先（$GOPATH/bin）が PATH に入っていること
export PATH="$PATH:$(go env GOPATH)/bin"
```

### 生成コマンド

```bash
cd 06-grpcstream
protoc --go_out=. --go-grpc_out=. \
  --go_opt=module=github.com/y-hashisaka/try-notification-golang/06-grpcstream \
  --go-grpc_opt=module=github.com/y-hashisaka/try-notification-golang/06-grpcstream \
  proto/notify.proto
```

`module=` オプションを使うと `option go_package` のパスから module path 部分を取り除いた相対位置（= `notifypb/`）に出力される。

## ローカル起動

### サーバー

```bash
cd 06-grpcstream
go run ./server
```

- Listen: `:50051`（`ADDR` 環境変数で変更可）
- メトリクス出力先: `./results/server-metrics-grpcstream-<日時>.csv`（`RESULTS_DIR` で変更可）

### クライアント

別ターミナルで：

```bash
cd 06-grpcstream
go run ./client -server localhost:50051
```

- 12回受信ごとに1行ログ出力（`-verbose` で毎回出力）
- ストリームが切れたら 1秒待って再接続
- `Ctrl+C` で終了 → サマリ表示

### grpcurl での疎通確認

サーバーが server reflection を有効にしていないので、`grpcurl` には `proto/notify.proto` を直接渡す。

```bash
# インストール（未導入なら）
brew install grpcurl

# Stream を1回呼び出し（受信のたびに JSON が出力され、Ctrl+C で止める）
grpcurl -plaintext \
  -import-path proto -proto notify.proto \
  -d '{}' \
  localhost:50051 notify.Notifier/Stream
```

## 測定手順（1時間バッテリー測定）

1. **サーバー起動（Mac 上）**
   ```bash
   cd 06-grpcstream
   go run ./server
   ```
2. **クライアントを Android 用にクロスコンパイル**
   ```bash
   GOOS=android GOARCH=arm64 go build -o grpcstream-client ./client
   ```
3. **Termux に配置して実行**
   ```bash
   # Termux 上
   ./grpcstream-client -server <MacのIP>:50051
   ```
4. Android を 100% 充電 → ケーブルを抜く → 画面 OFF → 1時間放置
5. 1時間後、`Ctrl+C` 等でクライアント終了 → サマリを記録
6. サーバー側は `Ctrl+C` で停止 → CSV を確認

## 観察ポイント

- **クライアント側**: 1時間で何 % 電池が減ったか / 受信メッセージ数（720回想定）/ 受信バイト数（protobuf なので JSON 比でかなり小さいはず）/ プロセス CPU 時間
- **サーバー側**: HTTP/2 の長期接続なので `active_conns` は接続中ずっと 1 のまま / メモリと goroutine 数の推移
- **ネットワーク**: HTTP/2 上の単一 TCP 接続を使い回し、PING フレームでアイドル維持される（gRPC のデフォルト keepalive 設定に依存）

## この方式の特徴（事前予想）

- **電池への影響**: 接続は1本張りっぱなしで、データ受信時のみ wakeup する想定。WebSocket と並んで省電力寄りになるはず。ただし HTTP/2 の PING / gRPC の keepalive 周期次第では Doze に入ってもタイマーで起こされる可能性あり
- **HTTP/2 の多重化**: 今回は1ストリームしか張らないので多重化の旨味は無いが、本番では複数 RPC を1コネクションに相乗りできる
- **サーバーへの影響**: 接続継続中は1 goroutine が滞在し続ける（`Stream` ハンドラ内の `for-select`）。WebSocket 実装と同等のメモリ/CPU プロファイルになるはず。protobuf エンコードは JSON より軽い分、CPU は控えめになるかも
- **遅延**: サーバー push なので、送信間隔ぶんの遅延だけ（最悪 5秒、平均 2.5秒）
- **Doze の影響**: Android が Doze に入ると TCP は維持されてもアプリの wakeup が抑止される。WebSocket と同じく、Doze 中はメッセージが溜まり、Doze 解除時にまとめて受信する挙動になる可能性が高い
