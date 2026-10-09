# 构建管线文档（BUILD.md）

FlClash iOS MG —— 唯一仓库文档。内核构成、版本与完整构建管线都在本文。

## 0. 项目构成

- Flutter 多平台工程（iOS Network Extension / Android / 桌面），上游基线 v0.9.4
- 内核 vendor 在 `core/mihomo/`：mihomo FlClash 基座（293cd231，随 v0.9.4）+ 私有协议套件
  - `x365`（365VPN/efan，VLESS 衍生，xhttp stream-one + REALITY）
  - `xhttp`（黑石，含 2026-09 key1 轮换修复）
  - `anytls`（`#SL` 后缀闪连，Chrome 指纹派生）
  - `oppa`（Trojan 风格裸 TLS 隧道）
  - `trojan`(mpw)（FastUP，自动 h2mux SingMux）
  - ViewTurbo SS（`core/sing-shadowsocks2-viewturbo`，`#viewTurbo` 密码后缀）
- 平台标识统一为 `cc.flclash.mg`（iOS bundle id / Android applicationId / 桌面 id）
- iOS 内存调度（`with_low_memory` 构建）：GOGC=30 + 20MB 堆软限；串行规则加载 +
  每 provider 即时 GC；单规则集 6000 条 clamp；footprint 分级归还（10s 常规 /
  2s 应急 ≥34MB）；x365 transport 懒初始化 + 3 分钟闲置淘汰

---

本文档描述 FlClash-iOS-MG 的完整构建管线：本地构建、CI 构建、签名安装与产物校验。

---

## 1. 环境要求

| 组件 | 版本 | 说明 |
|---|---|---|
| Flutter | 3.47.6（master channel） | 与 CI 对齐；其他版本未验证 |
| Go | 1.26.x | 内核构建；低版本未验证 |
| Xcode | 26.2 / 26.3 | 仅 macOS 构建需要 |
| CocoaPods | 最新稳定版 | `pod install` 由 flutter build 自动触发 |
| Rust + Cargo | 最新稳定版 | `plugins/rust_api` 需要 |

> 内存调度优化（`with_low_memory` 构建标签）对 Go 1.21+ 均可用；但生产构建请用 Go 1.26.x，与 CI 一致。

---

## 2. 源码获取

```bash
git clone --recursive <本仓库地址>
cd FlClash-iOS-MG
```

内核已 vendor 进仓库（`core/mihomo/`、`core/sing-shadowsocks2-viewturbo/`），无外部 submodule 依赖，clone 后即可构建。

### 2.1 内核完整性自检（可选但推荐）

```bash
# 协议注册标记（4 个私有协议必须在 parser.go 中注册）
grep -E 'case "(x365|xhttp|oppa|anytls)":' core/mihomo/adapter/parser.go
# 期望输出 4 行

# x365 协议关键代码
grep -n 'magic = \[4\]byte' core/mihomo/transport/x365/conn.go
grep -n 'invalid response magic' core/mihomo/transport/x365/conn.go

# 黑石 key1 轮换修复标记
grep -F 'do not hack this protocol please' core/mihomo/transport/blackstonexhttp/conn.go

# iOS 内存调度标记
grep -F 'SetGCPercent(30)' core/lowmem.go
grep -F 'fast-emergency' core/lib.go
grep -F 'EvictIdleLazyClients' core/mihomo/adapter/outbound/lazy_registry.go
```

全部命中即源码完整。

---

## 3. CI 构建（推荐）

### 3.1 ios-build 工作流

文件：`.github/workflows/ios.yaml`，手动触发（`workflow_dispatch`）。

```
Actions 页 → ios-build → Run workflow
  └─ build_env: stable / pre / dev（默认 stable）
```

bundle id 固定为 `cc.flclash.mg`（工作流内写死，无输入参数），每次构建的包标识恒定，**可直接覆盖安装升级**。

命令行触发：

```bash
gh workflow run ios.yaml --ref main -f build_env=stable
```

### 3.2 工作流执行步骤（约 15~25 分钟）

1. **Checkout**：完整历史 + 递归检出
2. **Verify vendored private kernel markers**：校验 4 个私有协议在 parser.go 的注册 + 关键传输层文件存在（缺一即 fail，防止空内核出包）
3. **Setup Flutter / Go**：3.47.6 + 1.26.8，带缓存
4. **Go protocol tests**：跑 `core/mihomo` 的协议单测（x365/xhttp/anytls/oppa/trojan/viewturbo 全链路回归）
5. **Install dependencies**：`flutter pub get`
6. **Build unsigned IPA**：`dart setup.dart ios --env stable --no-codesign -v`
   - setup.dart 完成 pbxproj 注入（bundle id）、xcconfig 生成、pod install
7. **IPA 内 x365 标记校验**：解包 IPA 后 grep 二进制中的 `invalid x365 response header`（确保编进去的是魔改内核而非上游原版）
8. **Upload artifact**：`FlClash-ios-arm64-unsigned`，保留 3 天

### 3.3 下载产物

```bash
RUN_ID=$(gh run list -w ios.yaml --limit 1 --json databaseId -q '.[0].databaseId')
gh run download $RUN_ID -n FlClash-ios-arm64-unsigned -D dist_download
ls dist_download/
# FlClash-<version>-ios-arm64-unsigned.ipa
```

### 3.4 build 工作流（.github/workflows/build.yaml）

通用多平台构建（Android/桌面），与 iOS 无关，按需触发。

---

## 4. 本地构建（macOS）

```bash
# 依赖就位后
dart setup.dart ios --env stable --no-codesign -v
# 产物: dist/FlClash-*-ios-arm64-unsigned.ipa
```

参数说明：
- `--env stable|pre|dev`：应用环境（影响应用名后缀与 flavors）
- `--no-codesign`：跳过签名（产出 unsigned 包，适合 TrollStore/自签）
- `--ios-bundle-id <id>`：覆盖 bundle id；默认 `cc.flclash.mg`。为保证覆盖安装升级，建议保持默认值不变

### 4.1 内核单独编译验证（Linux/任意平台，不出包）

```bash
cd core/mihomo
go build -tags "with_low_memory with_gvisor" -ldflags "-s -w" -o /tmp/mihomo-check .
go test ./adapter/outbound/ ./rules/provider/
go vet ./...
```

---

## 5. 签名与安装

unsigned IPA 的三种安装路径：

### 5.1 TrollStore（iOS 14.0~16.6.1 / 17.0 部分版本）

直接用 TrollStore 打开 IPA 安装，无需签名。NE 权限完整保留（entitlements 在包内）。

### 5.2 SideStore / AltStore（7 天签）

1. AltServer/SideServer 连接 iPhone
2. 侧载 IPA（需要 Apple ID + App-specific password）
3. 首次安装后需在 设置 → 通用 → VPN与设备管理 信任开发者证书
4. **NE 完整权限注意**：免费 Apple ID 签的 Network Extension 需要 SideStore 开启"JIT/增强"或使用付费开发者账号，否则 NE 无法启动

### 5.3 开发者证书签（推荐，长期有效）

```bash
# 用 codesign 重签（需替换 provisioning profile 与证书名）
unzip FlClash-*-unsigned.ipa -d payload
codesign -f -s "Apple Development: <cert>" \
  --entitlements ios/Runner/Runner.entitlements \
  payload/Payload/Runner.app
codesign -f -s "Apple Development: <cert>" \
  --entitlements ios/NECore/NECore.entitlements \
  payload/Payload/Runner.app/PlugIns/NECore.appex
# 重打包
cd payload && zip -r ../FlClash-signed.ipa Payload
```

> 重签时 bundle id 必须与 provisioning profile 匹配，NE 的 app group 也要同步改。

---

## 6. 产物校验

### 6.1 基础校验

```bash
md5sum FlClash-*-unsigned.ipa   # 与发布方提供的 MD5 对比
unzip -l FlClash-*-unsigned.ipa | head -20   # 结构: Payload/Runner.app + PlugIns/NECore.appex + PlugIns/Widget.appex
```

### 6.2 协议标记校验（确认魔改内核在包内）

```bash
unzip -q FlClash-*-unsigned.ipa -d verify
strings verify/Payload/Runner.app/PlugIns/NECore.appex/NECore | grep -c "x365"
# ≥1 即魔改内核已编入（上游原版内核此计数为 0）

strings verify/Payload/Runner.app/PlugIns/NECore.appex/NECore | grep -F "invalid x365 response header"
# 有输出 = x365 协议代码完整

strings verify/Payload/Runner.app/PlugIns/NECore.appex/NECore | grep -F "build	-tags"
# 期望: build -tags=with_gvisor,with_low_memory
```

### 6.3 内存调度标记校验

```bash
strings verify/Payload/Runner.app/PlugIns/NECore.appex/NECore | grep -F "fast-emergency"
strings verify/Payload/Runner.app/PlugIns/NECore.appex/NECore | grep -F "EvictIdleLazyClients"
strings verify/Payload/Runner.app/PlugIns/NECore.appex/NECore | grep -F "Go memory soft limit"
```

三者都有输出 = 内存调度代码完整编入。

---

## 7. 实机验收清单

安装后按顺序验证（对应本文 0. 节内存调度要点）：

| # | 场景 | 期望 |
|---|---|---|
| 1 | 启动 VPN 后 1 分钟（加载 121 节点 + 20 规则集） | 不闪退；日志 `[MEM] footprint=` 稳定在 15~22MB |
| 2 | 应用内全组测速 | 全部完成；测后 footprint ≤ 25MB；无 `memory_pressure_critical` 连环出现 |
| 3 | 挂机 10 分钟 | 不断流；每 10s 一条 `reclaim (routine)` |
| 4 | 触发 footprint≥34MB（大量并发下载） | 出现 `reclaim (fast-emergency)` 后回落；连接不中断 |
| 5 | 各协议节点逐一切换 | x365 / 黑石 xhttp / anytls(#SL) / oppa / FastUP / viewTurbo 全部可连通 |

日志获取：应用内 日志页 → 按等级过滤 info，搜 `[MEM]` 与 `[NE]`。

---

## 8. 常见问题

**Q: CI 在 Verify markers 步骤失败？**
A: 内核文件缺失或协议未注册。检查 `core/mihomo/adapter/parser.go` 的 4 个 case 是否在。

**Q: Build unsigned IPA 步骤报 Go 编译错误？**
A: 大概率是 lib.go 的 cgo 分支（`(android||ios)&&cgo`）问题——本地 Linux 构建不会编译这个分支，只有 CI/macOS 会走到。修复后需在 CI 验证。

**Q: 本地构建 Rust 报错？**
A: `plugins/rust_api` 需要 Rust 工具链：`rustup update stable`。

**Q: 实机 NE 启动后立刻被杀？**
A: 看日志最后一条 `[MEM] footprint=`。若 >40MB，确认安装的是带 `with_low_memory` 标签的构建（6.3 节校验第 3 项有输出）。

**Q: 测速后 memory pressure 连环出现？**
A: 属正常防御路径：`memory_pressure_critical` → `memory_pressure_reclaimed` 成对出现且 footprint 回落即可；若只 critical 不 reclaimed 才是问题。


---

## 9. 发布

当前发布版本：**flclash-iOSMG 0.01**（tag `v0.01`）

- 产物：`FlClash-0.9.4-ios-arm64-unsigned.ipa`（bundle id `cc.flclash.mg`）
- 下载：仓库 Releases 页 `v0.01` 资产（免登录直链）
- 覆盖安装：bundle id 与工作流已固定，此后每次 iOS 构建出的 IPA 均可直接覆盖安装升级（TrollStore 直接打开 IPA 安装即可），无需卸载
- 发布流程：
  ```bash
  # 1. 触发构建并等待完成
  gh workflow run ios.yaml --ref main -f build_env=stable
  # 2. 从 run 下载产物 IPA
  gh run download <run-id> -n FlClash-ios-arm64-unsigned -D dist
  # 3. 创建 GitHub Release 并上传 IPA
  gh release create v0.0X dist/*.ipa --title "flclash-iOSMG 0.0X" --notes "版本说明"
  ```
