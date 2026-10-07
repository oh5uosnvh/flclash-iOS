import Darwin
import NetworkExtension
import os

final class PacketTunnelNetworkConfiguration {
  private let ipv4Address = "172.19.0.1"
  private let ipv4AddressPrefix = "172.19.0.1/30"
  private let ipv4SubnetMask = "255.255.255.252"
  private let ipv4DNS = "172.19.0.2"
  private let ipv6Address = "fdfe:dcba:9876::1"
  private let ipv6AddressPrefix = "fdfe:dcba:9876::1/126"
  private let ipv6DNS = "fdfe:dcba:9876::2"
  private let netAny = "0.0.0.0"
  private let logger = Logger(
    subsystem: PacketTunnelEnvironment.extensionBundleIdentifier,
    category: "PacketTunnelNetworkConfiguration"
  )

  /// Complement of the bypassPrivate route table (the reserved and private
  /// ranges the table deliberately omits), used when the default route is
  /// claimed instead of enumerating every global unicast prefix.
  private lazy var bypassPrivateExcludedRoutes: [NEIPv4Route] = [
    "0.0.0.0/8",
    "10.0.0.0/8",
    "127.0.0.0/8",
    "169.254.0.0/16",
    "172.16.0.0/12",
    "192.168.0.0/16",
    "224.0.0.0/4",
    "255.128.0.0/9",
  ].compactMap { entry -> NEIPv4Route? in
    guard let cidr = CIDR(entry),
      let subnetMask = ipv4SubnetMaskStatic(prefixLength: cidr.prefixLength)
    else {
      return nil
    }
    return NEIPv4Route(
      destinationAddress: cidr.address,
      subnetMask: subnetMask
    )
  }

  func makeSettings(
    for options: PacketTunnelVPNOptions
  ) -> NEPacketTunnelNetworkSettings {
    let settings = NEPacketTunnelNetworkSettings(
      tunnelRemoteAddress: "127.0.0.1"
    )
    settings.mtu = NSNumber(value: options.mtu)

    let ipv4Settings = NEIPv4Settings(
      addresses: [ipv4Address],
      subnetMasks: [ipv4SubnetMask]
    )
    let isBypassPrivateTable = options.routeAddress.contains("1.0.0.0/8")
      && options.routeAddress.contains("128.0.0.0/3")
    let ipv4Routes = options.routeAddress.compactMap {
      route -> NEIPv4Route? in
      guard route.contains("."),
        let cidr = CIDR(route),
        let subnetMask = ipv4SubnetMask(
          prefixLength: cidr.prefixLength
        )
      else {
        return nil
      }
      return NEIPv4Route(
        destinationAddress: cidr.address,
        subnetMask: subnetMask
      )
    }
    // The bypassPrivate route table enumerates the global unicast space as
    // ~255 explicit prefixes. With enforceRoutes off, iOS re-evaluates the
    // path shortly after the tunnel comes up and, finding no default route
    // claim, tears the partial routes back down — traffic then falls through
    // to the physical interface (seen as a sudden drop ~10s after connect).
    // Expressing the same intent as default-route-plus-exclusions gives the
    // tunnel ownership of the default route, which iOS honours.
    if isBypassPrivateTable {
      ipv4Settings.includedRoutes = [.default()]
      ipv4Settings.excludedRoutes = bypassPrivateExcludedRoutes
    } else {
      ipv4Settings.includedRoutes = ipv4Routes.isEmpty
        ? [.default()]
        : ipv4Routes
    }
    settings.ipv4Settings = ipv4Settings

    var ipv6RouteCount = 0
    if options.ipv6 {
      let ipv6Settings = NEIPv6Settings(
        addresses: [ipv6Address],
        networkPrefixLengths: [NSNumber(value: 126)]
      )
      let ipv6Routes = options.routeAddress.compactMap {
        route -> NEIPv6Route? in
        guard route.contains(":"),
          let cidr = CIDR(route)
        else {
          return nil
        }
        return NEIPv6Route(
          destinationAddress: cidr.address,
          networkPrefixLength: NSNumber(value: cidr.prefixLength)
        )
      }
      ipv6RouteCount = ipv6Routes.count
      if ipv6RouteCount >= 5 {
        // bypassPrivate v6 table: claim the default route with the link-local
        // and multicast space excluded, mirroring the IPv4 handling above.
        ipv6Settings.includedRoutes = [.default()]
        ipv6Settings.excludedRoutes = [
          NEIPv6Route(destinationAddress: "fe80::", networkPrefixLength: 10),
          NEIPv6Route(destinationAddress: "::1", networkPrefixLength: 128),
        ]
      } else {
        ipv6Settings.includedRoutes = ipv6Routes.isEmpty
          ? [.default()]
          : ipv6Routes
      }
      settings.ipv6Settings = ipv6Settings
    }

    let dnsServers = options.ipv6
      ? [ipv4DNS, ipv6DNS]
      : [ipv4DNS]
    let dnsSettings = NEDNSSettings(servers: dnsServers)
    if options.captureDns {
      dnsSettings.matchDomains = [""]
    }
    settings.dnsSettings = dnsSettings

    if options.systemProxy {
      let proxySettings = NEProxySettings()
      proxySettings.httpEnabled = true
      proxySettings.httpServer = NEProxyServer(
        address: "127.0.0.1",
        port: options.port
      )
      proxySettings.httpsEnabled = true
      proxySettings.httpsServer = NEProxyServer(
        address: "127.0.0.1",
        port: options.port
      )
      proxySettings.exceptionList = options.bypassDomain
      settings.proxySettings = proxySettings
    }

    logger.debug(
      "makeSettings ipv4Routes=\(ipv4Routes.count, privacy: .public) ipv6Routes=\(ipv6RouteCount, privacy: .public)"
    )
    return settings
  }

  func tunAddress(for options: PacketTunnelVPNOptions) -> String {
    options.ipv6
      ? "\(ipv4AddressPrefix),\(ipv6AddressPrefix)"
      : ipv4AddressPrefix
  }

  func tunDNS(for options: PacketTunnelVPNOptions) -> String {
    if options.captureDns {
      return netAny
    }
    return options.ipv6 ? "\(ipv4DNS),\(ipv6DNS)" : ipv4DNS
  }

  func tunnelFileDescriptor() -> Int32? {
    var controlInfo = ctl_info()
    withUnsafeMutablePointer(to: &controlInfo.ctl_name) {
      $0.withMemoryRebound(
        to: CChar.self,
        capacity: MemoryLayout.size(ofValue: $0.pointee)
      ) {
        _ = strcpy($0, "com.apple.net.utun_control")
      }
    }
    for fileDescriptor: Int32 in 0...1024 {
      var address = sockaddr_ctl()
      var result: Int32 = -1
      var length = socklen_t(MemoryLayout.size(ofValue: address))
      withUnsafeMutablePointer(to: &address) {
        $0.withMemoryRebound(to: sockaddr.self, capacity: 1) {
          result = getpeername(fileDescriptor, $0, &length)
        }
      }
      if result != 0 || address.sc_family != AF_SYSTEM {
        continue
      }
      if controlInfo.ctl_id == 0 {
        result = ioctl(fileDescriptor, CTLIOCGINFO, &controlInfo)
        if result != 0 {
          continue
        }
      }
      if address.sc_id == controlInfo.ctl_id {
        return fileDescriptor
      }
    }
    return nil
  }

  private func ipv4SubnetMask(prefixLength: Int) -> String? {
    Self.ipv4SubnetMaskStatic(prefixLength: prefixLength)
  }

  private static func ipv4SubnetMaskStatic(prefixLength: Int) -> String? {
    guard (0...32).contains(prefixLength) else {
      return nil
    }
    let mask = prefixLength == 0
      ? 0
      : UInt32.max << UInt32(32 - prefixLength)
    return [
      (mask >> 24) & 0xff,
      (mask >> 16) & 0xff,
      (mask >> 8) & 0xff,
      mask & 0xff,
    ].map { String($0) }.joined(separator: ".")
  }
}

private struct CIDR {
  let address: String
  let prefixLength: Int

  init?(_ value: String) {
    let parts = value.split(separator: "/", maxSplits: 1).map(String.init)
    guard parts.count == 2,
      let prefixLength = Int(parts[1])
    else {
      return nil
    }
    address = parts[0]
    self.prefixLength = prefixLength
  }
}
