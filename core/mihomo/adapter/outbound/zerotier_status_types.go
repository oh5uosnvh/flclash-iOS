package outbound

type ZeroTierPeerStatus struct {
	Address   string
	Role      string
	Version   string
	Direct    bool
	Endpoints []string
	LatencyMS int64
}

type ZeroTierStatus struct {
	NetworkID string
	Network   string
	Node      string
	Online    bool
	Status    string
	AuthURL   string
	Addresses []string
	Routes    []string
	DNS       []string
	MTU       uint32
	Peers     []ZeroTierPeerStatus
	Error     string
}
