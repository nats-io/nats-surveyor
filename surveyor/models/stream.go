package models

import "time"

// StreamConfig will determine the name, subjects and retention policy
// for a given stream. If subjects is empty the name will be used.
type StreamConfig struct {
	Name         string           `json:"name"`
	Description  string           `json:"description,omitempty"`
	Subjects     []string         `json:"subjects,omitempty"`
	Retention    RetentionPolicy  `json:"retention"`
	MaxConsumers int              `json:"max_consumers"`
	MaxMsgs      int64            `json:"max_msgs"`
	MaxBytes     int64            `json:"max_bytes"`
	MaxAge       time.Duration    `json:"max_age"`
	MaxMsgsPer   int64            `json:"max_msgs_per_subject"`
	MaxMsgSize   int32            `json:"max_msg_size,omitempty"`
	Discard      DiscardPolicy    `json:"discard"`
	Storage      StorageType      `json:"storage"`
	Replicas     int              `json:"num_replicas"`
	NoAck        bool             `json:"no_ack,omitempty"`
	Template     string           `json:"template_owner,omitempty"`
	Duplicates   time.Duration    `json:"duplicate_window,omitempty"`
	Placement    *Placement       `json:"placement,omitempty"`
	Mirror       *StreamSource    `json:"mirror,omitempty"`
	Sources      []*StreamSource  `json:"sources,omitempty"`
	Compression  StoreCompression `json:"compression"`

	// Allow applying a subject transform to incoming messages before doing anything else
	SubjectTransform *SubjectTransformConfig `json:"subject_transform,omitempty"`

	// Allow republish of the message after being sequenced and stored.
	RePublish *RePublish `json:"republish,omitempty"`

	// Allow higher performance, direct access to get individual messages. E.g. KeyValue
	AllowDirect bool `json:"allow_direct"`
	// Allow higher performance and unified direct access for mirrors as well.
	MirrorDirect bool `json:"mirror_direct"`

	// Allow KV like semantics to also discard new on a per subject basis
	DiscardNewPer bool `json:"discard_new_per_subject,omitempty"`

	// Optional qualifiers. These can not be modified after set to true.

	// Sealed will seal a stream so no messages can get out or in.
	Sealed bool `json:"sealed"`
	// DenyDelete will restrict the ability to delete messages.
	DenyDelete bool `json:"deny_delete"`
	// DenyPurge will restrict the ability to purge messages.
	DenyPurge bool `json:"deny_purge"`
	// AllowRollup allows messages to be placed into the system and purge
	// all older messages using a special msg header.
	AllowRollup bool `json:"allow_rollup_hdrs"`

	// Metadata is additional metadata for the Stream.
	Metadata map[string]string `json:"metadata,omitempty"`
}

// SubjectTransformConfig is for applying a subject transform (to matching messages) before doing anything else when a new message is received
type SubjectTransformConfig struct {
	Source      string `json:"src,omitempty"`
	Destination string `json:"dest"`
}

// RePublish is for republishing messages once committed to a stream.
type RePublish struct {
	Source      string `json:"src,omitempty"`
	Destination string `json:"dest"`
	HeadersOnly bool   `json:"headers_only,omitempty"`
}

// JSPubAckResponse is a formal response to a publish operation.
type JSPubAckResponse struct {
	Error *ApiError `json:"error,omitempty"`
	*PubAck
}

// PubAck is the detail you get back from a publish to a stream that was successful.
// e.g. +OK {"stream": "Orders", "seq": 22}
type PubAck struct {
	Stream    string `json:"stream"`
	Sequence  uint64 `json:"seq"`
	Domain    string `json:"domain,omitempty"`
	Duplicate bool   `json:"duplicate,omitempty"`
}

// StreamInfo shows config and current state for this stream.
type StreamInfo struct {
	Config     StreamConfig        `json:"config"`
	Created    time.Time           `json:"created"`
	State      StreamState         `json:"state"`
	Domain     string              `json:"domain,omitempty"`
	Cluster    *ClusterInfo        `json:"cluster,omitempty"`
	Mirror     *StreamSourceInfo   `json:"mirror,omitempty"`
	Sources    []*StreamSourceInfo `json:"sources,omitempty"`
	Alternates []StreamAlternate   `json:"alternates,omitempty"`
	// TimeStamp indicates when the info was gathered
	TimeStamp time.Time `json:"ts"`
}

type StreamAlternate struct {
	Name    string `json:"name"`
	Domain  string `json:"domain,omitempty"`
	Cluster string `json:"cluster"`
}

// ClusterInfo shows information about the underlying set of servers
// that make up the stream or consumer.
type ClusterInfo struct {
	Name     string      `json:"name,omitempty"`
	Leader   string      `json:"leader,omitempty"`
	Replicas []*PeerInfo `json:"replicas,omitempty"`
}

// PeerInfo shows information about all the peers in the cluster that
// are supporting the stream or consumer.
type PeerInfo struct {
	Name    string        `json:"name"`
	Current bool          `json:"current"`
	Offline bool          `json:"offline,omitempty"`
	Active  time.Duration `json:"active"`
	Lag     uint64        `json:"lag,omitempty"`
	Peer    string        `json:"peer"`
	// For migrations.
	cluster string
}

// StreamSourceInfo shows information about an upstream stream source.
type StreamSourceInfo struct {
	Name                 string          `json:"name"`
	External             *ExternalStream `json:"external,omitempty"`
	Lag                  uint64          `json:"lag"`
	Active               time.Duration   `json:"active"`
	Error                *ApiError       `json:"error,omitempty"`
	FilterSubject        string          `json:"filter_subject,omitempty"`
	SubjectTransformDest string          `json:"subject_transform_dest,omitempty"`
}

// StreamSource dictates how streams can source from other streams.
type StreamSource struct {
	Name                 string          `json:"name"`
	OptStartSeq          uint64          `json:"opt_start_seq,omitempty"`
	OptStartTime         *time.Time      `json:"opt_start_time,omitempty"`
	FilterSubject        string          `json:"filter_subject,omitempty"`
	SubjectTransformDest string          `json:"subject_transform_dest,omitempty"`
	External             *ExternalStream `json:"external,omitempty"`

	// Internal
	iname string // For indexing when stream names are the same for multiple sources.
}

// ExternalStream allows you to qualify access to a stream source in another account.
type ExternalStream struct {
	ApiPrefix     string `json:"api"`
	DeliverPrefix string `json:"deliver"`
}
