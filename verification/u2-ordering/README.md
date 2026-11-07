# U2-1 Native Ordering 驗證

本目錄僅驗證 Engine / Player 命令順序，不啟動 Adam、DRM、帳號或正式 Profile。
正常 Renderer 未移植 Ordered Transport；U1 七項 Controller RED 必須保留。

Production 變更只在 Engine：獨立 admission / native commit、Player load/seek
世代、prepared reader / priority lease、同 ticket terminal reload。既有 callbacks EOF、
Duration、Session Touch、Renderer 分類器不變。測試 sink / instance 需顯式
`vlc_ordering_test` build tag；普通 build 排除這些入口。

## 協定

正常 Legacy 預設不需 ADAM_VLC_ORDERED，保持既有請求格式；Engine 以接收順序
配置內部 ticket。這不能識別「網路上晚到但使用者意圖更舊」的 Legacy request。

私人 Ordered Client 必須同時設定 `ADAM_VLC_ORDERED=1` 與新的私人
`ADAM_API_TOKEN`；Header 只用 `X-Api-Token`，不得交給遠端 Renderer。
缺 token 時 Start 拒絕啟動，不回退成未認證 Ordered 模式。

1. GET `/api/v1/vlc/control` 取得 Engine Boot / 當前 Epoch。
2. POST 同路由，送 `{boot, epoch, sessionId}`；Engine 驗證存在的 Session、CAS
   當前 Epoch，再產生新隨機 Epoch。Client 不能指定新 Epoch。
3. load/seek/pause/resume/stop 的 JSON 可附 `command`：
   `{boot,epoch,sessionId,loadGeneration,sequence,type,requestId}`。
   新 load 的 generation 必須增加，其餘命令必須吻合；sequence 單調且 <=2^53-1。
   Boot、Epoch、Session、type、requestId 格式都由 Engine 驗證；重複 / 逆序拒絕409。
4. POST `/api/v1/vlc/intent` 只接 type=intent 的版本化命令，先作廢舊工作，
   不冒充 Native seek 成功。供日後 Main/Renderer debounce 遷移使用。
5. Claim 後所有未版本化 load/seek/pause/resume/stop 拒絕409，不能 Legacy 繞過。
   Volume 是獨立 mixer 功能，未加入本階段五項 transport 排序協定。

此私人 API credential 是 Engine-wide authority，不是 U2-2 的逐來源 capability。
目前尚無 Main / Source Registry 授權遷移，不可開放給正常遠端 UI 或真人來源。

ACK `media-committed` 只代表媒體已交付與 play 已接受，start seek 可能尚待派送；
`seek-dispatched` 只代表 C set_time 已派送；Legacy 為 `seek-queued`。
Timeout / abort / 409 的 `mayHaveApplied` 不能當 rollback 或真正 PCM 成功。
Terminal reload 保留 Seek 的同一 ticket，logical load 不變，physical load 增加。

## 本機回歸

參照全新 Lab 的 REPRODUCE.md。`env.sh` 只設定執行子程序的環境與 Lab cache，
Runtime / headers / fixtures 均為唯讀外部依賴。不可重跑歷史 one-time port 工具覆寫來源。

```bash
source verification/u2-ordering/env.sh
python3 verification/u2-ordering/extract-handlers.py "$U2_LAB/handler-NEW"
cd "$U2_LAB/handler-NEW"
"$U2_GO" test -race -count=1 -timeout 30s -v
cd "$U2_ROOT/engine"
"$U2_GO" test -race -tags vlc_ordering_test -count=1 -timeout 60s -v \
  ./core/vlc -run '^(TestNative|TestCallbackMediaTellsDataEndFromReadFailure)'
"$U2_GO" test -race -count=1 -timeout 60s ./internal/playbackcmd ./core/diskcache
```

原始 RED、U1 七個 RED 與 U2-2 authority gap 都保留在本輪 Lab。
最後一項是已確認的未整合邊界：Session 在最後 existence check 後撤銷，尚不能
原子地作廢 Ticket。因此本輪整體維持 `U2-1 BLOCKED`；不能以 reference 安全
或測試 ACK 代替完整 Source Authority。後續最小接口設計見 U2-2-PREREQUISITES.md。
