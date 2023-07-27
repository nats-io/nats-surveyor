package surveyor

import (
	"github.com/nats-io/nats-server/v2/server"
	"time"
)

// todo: once 2.10.0 is released, upgrade nats-server dependency to 2.10.0 and remove these

// VNextAccountStatz is the AccountStatz model from 2.10
type VNextAccountStatz struct {
	ID       string              `json:"server_id"`
	Now      time.Time           `json:"now"`
	Accounts []*VNextAccountStat `json:"account_statz"`
}

// VNextAccountStat is the AccountStat model from 2.10
type VNextAccountStat struct {
	Account       string           `json:"acc"`
	Conns         int              `json:"conns"`
	LeafNodes     int              `json:"leafnodes"`
	TotalConns    int              `json:"total_conns"`
	NumSubs       uint32           `json:"num_subscriptions"`
	Sent          server.DataStats `json:"sent"`
	Received      server.DataStats `json:"received"`
	SlowConsumers int64            `json:"slow_consumers"`
}
