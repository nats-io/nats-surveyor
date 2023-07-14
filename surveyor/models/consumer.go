package models

import "time"

type ConsumerInfo struct {
	Stream         string          `json:"stream_name"`
	Name           string          `json:"name"`
	Created        time.Time       `json:"created"`
	Config         *ConsumerConfig `json:"config,omitempty"`
	Delivered      SequenceInfo    `json:"delivered"`
	AckFloor       SequenceInfo    `json:"ack_floor"`
	NumAckPending  int             `json:"num_ack_pending"`
	NumRedelivered int             `json:"num_redelivered"`
	NumWaiting     int             `json:"num_waiting"`
	NumPending     uint64          `json:"num_pending"`
	Cluster        *ClusterInfo    `json:"cluster,omitempty"`
	PushBound      bool            `json:"push_bound,omitempty"`
	// TimeStamp indicates when the info was gathered
	TimeStamp time.Time `json:"ts"`
}

type ConsumerConfig struct {
	// Durable is deprecated. All consumers should have names, picked by clients.
	Durable         string          `json:"durable_name,omitempty"`
	Name            string          `json:"name,omitempty"`
	Description     string          `json:"description,omitempty"`
	DeliverPolicy   DeliverPolicy   `json:"deliver_policy"`
	OptStartSeq     uint64          `json:"opt_start_seq,omitempty"`
	OptStartTime    *time.Time      `json:"opt_start_time,omitempty"`
	AckPolicy       AckPolicy       `json:"ack_policy"`
	AckWait         time.Duration   `json:"ack_wait,omitempty"`
	MaxDeliver      int             `json:"max_deliver,omitempty"`
	BackOff         []time.Duration `json:"backoff,omitempty"`
	FilterSubject   string          `json:"filter_subject,omitempty"`
	FilterSubjects  []string        `json:"filter_subjects,omitempty"`
	ReplayPolicy    ReplayPolicy    `json:"replay_policy"`
	RateLimit       uint64          `json:"rate_limit_bps,omitempty"` // Bits per sec
	SampleFrequency string          `json:"sample_freq,omitempty"`
	MaxWaiting      int             `json:"max_waiting,omitempty"`
	MaxAckPending   int             `json:"max_ack_pending,omitempty"`
	Heartbeat       time.Duration   `json:"idle_heartbeat,omitempty"`
	FlowControl     bool            `json:"flow_control,omitempty"`
	HeadersOnly     bool            `json:"headers_only,omitempty"`

	// Pull based options.
	MaxRequestBatch    int           `json:"max_batch,omitempty"`
	MaxRequestExpires  time.Duration `json:"max_expires,omitempty"`
	MaxRequestMaxBytes int           `json:"max_bytes,omitempty"`

	// Push based consumers.
	DeliverSubject string `json:"deliver_subject,omitempty"`
	DeliverGroup   string `json:"deliver_group,omitempty"`

	// Ephemeral inactivity threshold.
	InactiveThreshold time.Duration `json:"inactive_threshold,omitempty"`

	// Generally inherited by parent stream and other markers, now can be configured directly.
	Replicas int `json:"num_replicas"`
	// Force memory storage.
	MemoryStorage bool `json:"mem_storage,omitempty"`

	// Don't add to general clients.
	Direct bool `json:"direct,omitempty"`

	// Metadata is additional metadata for the Consumer.
	Metadata map[string]string `json:"metadata,omitempty"`
}

// SequenceInfo has both the consumer and the stream sequence and last activity.
type SequenceInfo struct {
	Consumer uint64     `json:"consumer_seq"`
	Stream   uint64     `json:"stream_seq"`
	Last     *time.Time `json:"last_active,omitempty"`
}

type CreateConsumerRequest struct {
	Stream string         `json:"stream_name"`
	Config ConsumerConfig `json:"config"`
}

// ConsumerNakOptions is for optional NAK values, e.g. delay.
type ConsumerNakOptions struct {
	Delay time.Duration `json:"delay"`
}

// DeliverPolicy determines how the consumer should select the first message to deliver.
type DeliverPolicy int

const (
	// DeliverAll will be the default so can be omitted from the request.
	DeliverAll DeliverPolicy = iota
	// DeliverLast will start the consumer with the last sequence received.
	DeliverLast
	// DeliverNew will only deliver new messages that are sent after the consumer is created.
	DeliverNew
	// DeliverByStartSequence will look for a defined starting sequence to start.
	DeliverByStartSequence
	// DeliverByStartTime will select the first messsage with a timestamp >= to StartTime.
	DeliverByStartTime
	// DeliverLastPerSubject will start the consumer with the last message for all subjects received.
	DeliverLastPerSubject
)

func (dp DeliverPolicy) String() string {
	switch dp {
	case DeliverAll:
		return "all"
	case DeliverLast:
		return "last"
	case DeliverNew:
		return "new"
	case DeliverByStartSequence:
		return "by_start_sequence"
	case DeliverByStartTime:
		return "by_start_time"
	case DeliverLastPerSubject:
		return "last_per_subject"
	default:
		return "undefined"
	}
}

// AckPolicy determines how the consumer should acknowledge delivered messages.
type AckPolicy int

const (
	// AckNone requires no acks for delivered messages.
	AckNone AckPolicy = iota
	// AckAll when acking a sequence number, this implicitly acks all sequences below this one as well.
	AckAll
	// AckExplicit requires ack or nack for all messages.
	AckExplicit
)

func (a AckPolicy) String() string {
	switch a {
	case AckNone:
		return "none"
	case AckAll:
		return "all"
	default:
		return "explicit"
	}
}

// ReplayPolicy determines how the consumer should replay messages it already has queued in the stream.
type ReplayPolicy int

const (
	// ReplayInstant will replay messages as fast as possible.
	ReplayInstant ReplayPolicy = iota
	// ReplayOriginal will maintain the same timing as the messages were received.
	ReplayOriginal
)

func (r ReplayPolicy) String() string {
	switch r {
	case ReplayInstant:
		return "instant"
	default:
		return "original"
	}
}
