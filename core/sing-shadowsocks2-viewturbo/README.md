# sing-shadowsocks2-viewturbo

`github.com/metacubex/sing-shadowsocks2` v0.2.7 with the verified ViewTurbo client handshake patch used by the FlClash fork.

## Wire behavior

The patch is activated **only** when an AEAD Shadowsocks password ends with `#viewTurbo`. The suffix is removed before the normal legacy key derivation, so this configuration:

```yaml
- name: ViewTurbo
  type: ss
  server: 203.0.113.10
  port: 15889
  cipher: chacha20-ietf-poly1305
  password: "password#viewTurbo"
  udp: true
```

uses the real Shadowsocks password `password` and writes:

1. `randAlpha(20..60) + "\r\n\r\n"`
2. fixed protocol token + `:` + 27 random letters, with byte 1 inverted
3. standard Shadowsocks AEAD salt/chunks, with the transmitted salt byte 0 inverted (KDF still uses the original salt)

The reader discards the server's plaintext HTTP response header before reading the Shadowsocks response salt.

## Important mihomo fix

mihomo normally calls `DialConn` for a plain SS outbound. Eagerly flushing the salt there can coalesce it with the authentication packet and deadlock the ViewTurbo gateway. For patched connections, `DialConn` deliberately returns a lazy connection after the first two writes; the salt is emitted with the first real payload, matching the known-working sing-box behavior.

Normal Shadowsocks passwords are unchanged.

## FlClash linkage

FlClash checks this repository out as a Git submodule and its `core/go.mod` contains:

```go
replace github.com/metacubex/sing-shadowsocks2 => ./sing-shadowsocks2-viewturbo
```

This makes the patched module part of both the Android arm64 and Windows amd64 core builds.

## Verification

```bash
go test ./...
```

Regression tests lock the 60-byte authentication packet, prefix format, HTTP-header skip, and lazy `DialConn` behavior.

## Base and license

Base: metacubex/sing-shadowsocks2 tag `v0.2.7` (`7f844b0`). License: GPL-3.0, preserved in `LICENSE`.
