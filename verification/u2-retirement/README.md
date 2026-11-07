# U2-2 Engine Session Lifetime Fence

本階段只處理 Engine Session 撤權與 Native Commit。Lease 的 OwnerKind 明確為
`engine-session-lifecycle; main-principal-unverified`，不是逐來源 Main Principal 授權。
普通 Release、lookup expiry、periodic sweep 在同一 Manager 刪除鎖內撤 Lease／Ticket；
既有單一 Release Hook 繼續在鎖外呼叫，沒有被覆蓋。

Native 入口短暫取得 Lease mutex，確認有效並記錄不可撤銷 entry，隨即釋鎖才進 C。
Native 已進入不表示 C 已返回，也不表示 PCM 成功。Cleanup 以 Lease pointer 與
當前 Player Lock 中的 physical generation 條件化處理，不能只用 Asset 或 Session。
待關閉 Reader 的資源計數與 Native 清理完成後才可標示此 Lease 的資源完成；這不
涵蓋 cache 擁有的共享下載者或既有 vseg cancel 回呼之背景 drain。

## 離線重跑

用 NEW 路徑保存結果，別覆寫既有 Lab。`env.sh` 只指向本輪 U2-2 cache / HOME，
Runtime、headers、合成 fixtures 與原 Validator 皆唯讀，不會安裝應用或初始化帳號。

```bash
source verification/u2-retirement/env.sh
RUN=$(mktemp -d "$U2_LAB/repeat-XXXXXXXX")
python3 verification/u2-retirement/extract-handlers.py "$RUN/handler"
cd "$RUN/handler"
"$U2_GO" test -race -tags vlc_ordering_test -count=1 -timeout 30s -v
cd "$U2_ROOT/engine"
"$U2_GO" test -race -tags vlc_ordering_test -count=1 -timeout 60s -v \
 ./cmd -run '^(TestAuthority|TestVLC|TestALAC|TestInvalidateALAC)'
"$U2_GO" test -race -tags vlc_ordering_test -count=1 -timeout 60s -v \
 ./core/vlc -run '^(TestNative|TestCallbackMediaTellsDataEndFromReadFailure|TestLeaseWatcher)'
"$U2_GO" test -race -count=1 -timeout 60s -v \
 ./core/playback ./internal/playbackcmd ./core/diskcache
```

原 U2-1 Handler 13 個 assertions 保留，harness 的 Session boundary 改成真正
Manager 的有限合成 fixture。額外 authority gap 使用真正 `Manager.Release()`。
Native 另由實際 Handler／Manager／StreamingReader／cgo／libVLC 驗證，沒有以
錄製 Player 取代 Native。`vlc_ordering_test` 的 fixture / sink / observer 不進普通 build。

所有 Principal、正常 Main UI、HTTP fallback 授權、Credential、DRM 與真人帳號仍須
獨立階段；本階段不修改 Renderer、EOF 策略、Duration 或 PR#3 raw Reader Close。
