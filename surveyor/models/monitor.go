package models

import "time"

// JetStreamVarz contains basic runtime information about jetstream
type JetStreamVarz struct {
	Config *JetStreamConfig `json:"config,omitempty"`
	Stats  *JetStreamStats  `json:"stats,omitempty"`
	Meta   *MetaClusterInfo `json:"meta,omitempty"`
}

// MetaClusterInfo shows information about the meta group.
type MetaClusterInfo struct {
	Name     string      `json:"name,omitempty"`
	Leader   string      `json:"leader,omitempty"`
	Peer     string      `json:"peer,omitempty"`
	Replicas []*PeerInfo `json:"replicas,omitempty"`
	Size     int         `json:"cluster_size"`
}

type AccountDetail struct {
	Name string `json:"name"`
	Id   string `json:"id"`
	JetStreamStats
	Streams []StreamDetail `json:"stream_detail,omitempty"`
}

// StreamDetail shows information about the stream state and its consumers.
type StreamDetail struct {
	Name               string              `json:"name"`
	Created            time.Time           `json:"created"`
	Cluster            *ClusterInfo        `json:"cluster,omitempty"`
	Config             *StreamConfig       `json:"config,omitempty"`
	State              StreamState         `json:"state,omitempty"`
	Consumer           []*ConsumerInfo     `json:"consumer_detail,omitempty"`
	Mirror             *StreamSourceInfo   `json:"mirror,omitempty"`
	Sources            []*StreamSourceInfo `json:"sources,omitempty"`
	RaftGroup          string              `json:"stream_raft_group,omitempty"`
	ConsumerRaftGroups []*RaftGroupDetail  `json:"consumer_raft_groups,omitempty"`
}

// JSzOptions are options passed to Jsz
type JSzOptions struct {
	Account    string `json:"account,omitempty"`
	Accounts   bool   `json:"accounts,omitempty"`
	Streams    bool   `json:"streams,omitempty"`
	Consumer   bool   `json:"consumer,omitempty"`
	Config     bool   `json:"config,omitempty"`
	LeaderOnly bool   `json:"leader_only,omitempty"`
	Offset     int    `json:"offset,omitempty"`
	Limit      int    `json:"limit,omitempty"`
	RaftGroups bool   `json:"raft,omitempty"`
}

// JSInfo has detailed information on JetStream.
type JSInfo struct {
	ID       string          `json:"server_id"`
	Now      time.Time       `json:"now"`
	Disabled bool            `json:"disabled,omitempty"`
	Config   JetStreamConfig `json:"config,omitempty"`
	JetStreamStats
	Streams   int              `json:"streams"`
	Consumers int              `json:"consumers"`
	Messages  uint64           `json:"messages"`
	Bytes     uint64           `json:"bytes"`
	Meta      *MetaClusterInfo `json:"meta_cluster,omitempty"`

	// aggregate raft info
	AccountDetails []*AccountDetail `json:"account_details,omitempty"`
}

type AccountStatzOptions struct {
	Accounts      []string `json:"accounts"`
	IncludeUnused bool     `json:"include_unused"`
}

type AccountStatz struct {
	ID       string         `json:"server_id"`
	Now      time.Time      `json:"now"`
	Accounts []*AccountStat `json:"account_statz"`
}

// RaftGroupDetail shows information details about the Raft group.
type RaftGroupDetail struct {
	Name      string `json:"name"`
	RaftGroup string `json:"raft_group,omitempty"`
}
