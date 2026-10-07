package statistic

import C "github.com/metacubex/mihomo/constant"

type NodeTraffic struct {
	Name     string `json:"name"`
	Provider string `json:"provider"`
	Up       int64  `json:"up"`
	Down     int64  `json:"down"`
}

func nodeTrafficForConnection(conn C.Connection) *C.TrafficCounter {
	name := conn.Chains().Last()
	if name == "" {
		return nil
	}
	return conn.TrafficCounter()
}
