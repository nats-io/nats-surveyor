package models

import (
	"time"

	"github.com/nats-io/jwt/v2"
)

// ServerStatsMsg is sent periodically with stats updates.
type ServerStatsMsg struct {
	Server ServerInfo  `json:"server"`
	Stats  ServerStats `json:"statsz"`
}

// ServerInfo identifies remote servers.
type ServerInfo struct {
	Name    string   `json:"name"`
	Host    string   `json:"host"`
	ID      string   `json:"id"`
	Cluster string   `json:"cluster,omitempty"`
	Domain  string   `json:"domain,omitempty"`
	Version string   `json:"ver"`
	Tags    []string `json:"tags,omitempty"`
	// Whether JetStream is enabled (deprecated in favor of the `ServerCapability`).
	JetStream bool `json:"jetstream"`
	// Generic capability flags
	Flags ServerCapability
	// Sequence and Time from the remote server for this message.
	Seq  uint64    `json:"seq"`
	Time time.Time `json:"time"`
}

// Type for our server capabilities.
type ServerCapability uint64

// ServerStats hold various statistics that we will periodically send out.
type ServerStats struct {
	Start            time.Time      `json:"start"`
	Mem              int64          `json:"mem"`
	Cores            int            `json:"cores"`
	CPU              float64        `json:"cpu"`
	Connections      int            `json:"connections"`
	TotalConnections uint64         `json:"total_connections"`
	ActiveAccounts   int            `json:"active_accounts"`
	NumSubs          uint32         `json:"subscriptions"`
	Sent             DataStats      `json:"sent"`
	Received         DataStats      `json:"received"`
	SlowConsumers    int64          `json:"slow_consumers"`
	Routes           []*RouteStat   `json:"routes,omitempty"`
	Gateways         []*GatewayStat `json:"gateways,omitempty"`
	ActiveServers    int            `json:"active_servers,omitempty"`
	JetStream        *JetStreamVarz `json:"jetstream,omitempty"`
}

// RouteStat holds route statistics.
type RouteStat struct {
	ID       uint64    `json:"rid"`
	Name     string    `json:"name,omitempty"`
	Sent     DataStats `json:"sent"`
	Received DataStats `json:"received"`
	Pending  int       `json:"pending"`
}

// GatewayStat holds gateway statistics.
type GatewayStat struct {
	ID         uint64    `json:"gwid"`
	Name       string    `json:"name"`
	Sent       DataStats `json:"sent"`
	Received   DataStats `json:"received"`
	NumInbound int       `json:"inbound_connections"`
}

// DataStats reports how may msg and bytes. Applicable for both sent and received.
type DataStats struct {
	Msgs  int64 `json:"msgs"`
	Bytes int64 `json:"bytes"`
}

// AccountStat contains the data common between AccountNumConns and AccountStatz
type AccountStat struct {
	Account       string    `json:"acc"`
	Conns         int       `json:"conns"`
	LeafNodes     int       `json:"leafnodes"`
	TotalConns    int       `json:"total_conns"`
	NumSubs       uint32    `json:"num_subscriptions"`
	Sent          DataStats `json:"sent"`
	Received      DataStats `json:"received"`
	SlowConsumers int64     `json:"slow_consumers"`
}

// ServerAPIResponse is the response type for the server API like varz, connz etc.
type ServerAPIResponse struct {
	Server *ServerInfo `json:"server"`
	Data   interface{} `json:"data,omitempty"`
	Error  *ApiError   `json:"error,omitempty"`
}

// TypedEvent is a event or advisory sent by the server that has nats type hints
// typically used for events that might be consumed by 3rd party event systems
type TypedEvent struct {
	Type string    `json:"type"`
	ID   string    `json:"id"`
	Time time.Time `json:"timestamp"`
}

// ClientInfo is detailed information about the client forming a connection.
type ClientInfo struct {
	Start      *time.Time    `json:"start,omitempty"`
	Host       string        `json:"host,omitempty"`
	ID         uint64        `json:"id,omitempty"`
	Account    string        `json:"acc,omitempty"`
	Service    string        `json:"svc,omitempty"`
	User       string        `json:"user,omitempty"`
	Name       string        `json:"name,omitempty"`
	Lang       string        `json:"lang,omitempty"`
	Version    string        `json:"ver,omitempty"`
	RTT        time.Duration `json:"rtt,omitempty"`
	Server     string        `json:"server,omitempty"`
	Cluster    string        `json:"cluster,omitempty"`
	Alternates []string      `json:"alts,omitempty"`
	Stop       *time.Time    `json:"stop,omitempty"`
	Jwt        string        `json:"jwt,omitempty"`
	IssuerKey  string        `json:"issuer_key,omitempty"`
	NameTag    string        `json:"name_tag,omitempty"`
	Tags       jwt.TagList   `json:"tags,omitempty"`
	Kind       string        `json:"kind,omitempty"`
	ClientType string        `json:"client_type,omitempty"`
	MQTTClient string        `json:"client_id,omitempty"` // This is the MQTT client ID
	Nonce      string        `json:"nonce,omitempty"`
}
