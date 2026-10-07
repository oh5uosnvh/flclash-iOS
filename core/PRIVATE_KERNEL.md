# Private kernel provenance (flclash-MG)

本仓库 `core/mihomo/` **不是** submodule，而是 vendor 进来的魔改内核：
上游 chenx-dust/mihomo `FlClash` 基座（2026-10-06, 2edbe27b）+ `oh5uosnvh/clash-meta-mods@mods`
全部 8 个私有协议提交 cherry-pick（与原内核文件逐字节一致，已校验）。

## 注入的私有协议

| 协议 | type 字段 | 来源 | 关键文件 |
|---|---|---|---|
| x365 (365VPN/efan) | `x365` | clash-meta-mods de4f15c + 6f89252 | transport/x365, adapter/outbound/x365.go |
| 黑石 xhttp | `xhttp` | clash-meta-mods de4f15c + 2724223 (2026-09 key1 轮换修复) | transport/blackstonexhttp, adapter/outbound/xhttp_private.go |
| 闪连 AnyTLS | `anytls` | clash-meta-mods 2f46768 + 77cd109 + 7923667 | transport/anytls (mod 版), adapter/outbound/anytls.go |
| oppa (NekoBoxYF) | `oppa` | clash-meta-mods fd627ff | transport/oppa, adapter/outbound/oppa.go |
| FastUP trojan | `trojan`(mpw) | clash-meta-mods 2a2f7ab | adapter/outbound/trojan.go |

## ViewTurbo SS

`core/sing-shadowsocks2-viewturbo/` ← `oh5uosnvh/flclash-MG: flclash-viewTurbo/`
（sing-shadowsocks2 魔改版：`#viewTurbo` 密码后缀触发 token 握手 + HTTP 前缀混淆）。
`core/go.mod` 已 `replace github.com/metacubex/sing-shadowsocks2 => ./sing-shadowsocks2-viewturbo`。

## 节点写法示例

```yaml
# 黑石
- {name: bs, type: xhttp, server: 1.2.3.4, port: 40081, gateway: xxx.example.com:20244,
   cipher: aes-128-ctr, password: "KEY1:KEY2HEX", sess: 68hex, udp: true}
# viewTurbo SS（普通 ss 节点密码追加 #viewTurbo）
- {name: vt, type: ss, server: 5.6.7.8, port: 23456, cipher: aes-128-gcm,
   password: "xxxxxxxx#viewTurbo"}
```

其余协议（restls/tlsmirror/amnezia/mieru/easytier/zerotier/tailscale/openvpn 等）
由 chenx 基座自带，与 FlClash-Patched 上游 iOS 版一致。

## iOS 说明

iOS 构建入口：`.github/workflows/ios.yaml`（workflow_dispatch → macos-26 →
`dart setup.dart ios --no-codesign` → `dist/FlClash-*-ios-arm64-unsigned.ipa`）。
签名与 NE 完整权限需自签（TrollStore / SideStore / AltStore / 证书皆可）。
