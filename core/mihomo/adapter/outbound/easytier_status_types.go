package outbound

type EasyTierConnectionStatus struct {
	ID             string   `json:"id"`
	Protocol       string   `json:"protocol"`
	LocalEndpoint  string   `json:"local-endpoint"`
	RemoteEndpoint string   `json:"remote-endpoint"`
	LatencyMS      int64    `json:"latency-ms"`
	RxBytes        *uint64  `json:"rx-bytes,omitempty"`
	TxBytes        *uint64  `json:"tx-bytes,omitempty"`
	RxPackets      *uint64  `json:"rx-packets,omitempty"`
	TxPackets      *uint64  `json:"tx-packets,omitempty"`
	LossRate       *float32 `json:"loss-rate,omitempty"`
}
type EasyTierNodeStatus struct {
	PathLatencyMS             *int32                     `json:"path-latency-ms,omitempty"`
	LatencyFirstPathLatencyMS *int32                     `json:"latency-first-path-latency-ms,omitempty"`
	PeerID                    uint32                     `json:"peer-id"`
	InstanceID                string                     `json:"instance-id"`
	Version                   string                     `json:"version"`
	NextHop                   uint32                     `json:"next-hop"`
	Cost                      int32                      `json:"cost"`
	ConnectionType            string                     `json:"connection-type"`
	FeatureFlags              map[string]bool            `json:"feature-flags,omitempty"`
	ProxyCIDRs                []string                   `json:"proxy-cidrs"`
	Listeners                 []string                   `json:"listeners"`
	Connections               []EasyTierConnectionStatus `json:"connections"`
	Hostname                  string                     `json:"hostname"`
	IPv4                      string                     `json:"ipv4"`
	LatencyMS                 int64                      `json:"latency-ms"`
}
type EasyTierNetworkDetails struct {
	InstanceID string               `json:"instance-id"`
	DNSZone    string               `json:"dns-zone"`
	Local      EasyTierNodeStatus   `json:"local"`
	Peers      []EasyTierNodeStatus `json:"peers"`
}
type EasyTierStatus struct {
	State   string
	Network string
	Error   string
	Details *EasyTierNetworkDetails
}
