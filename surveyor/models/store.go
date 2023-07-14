package models

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// StreamState is information about the given stream.
type StreamState struct {
	Msgs        uint64            `json:"messages"`
	Bytes       uint64            `json:"bytes"`
	FirstSeq    uint64            `json:"first_seq"`
	FirstTime   time.Time         `json:"first_ts"`
	LastSeq     uint64            `json:"last_seq"`
	LastTime    time.Time         `json:"last_ts"`
	NumSubjects int               `json:"num_subjects,omitempty"`
	Subjects    map[string]uint64 `json:"subjects,omitempty"`
	NumDeleted  int               `json:"num_deleted,omitempty"`
	Deleted     []uint64          `json:"deleted,omitempty"`
	Lost        *LostStreamData   `json:"lost,omitempty"`
	Consumers   int               `json:"consumer_count"`
}

// LostStreamData indicates msgs that have been lost.
type LostStreamData struct {
	Msgs  []uint64 `json:"msgs"`
	Bytes uint64   `json:"bytes"`
}

// RetentionPolicy determines how messages in a set are retained.
type RetentionPolicy int

const (
	// LimitsPolicy (default) means that messages are retained until any given limit is reached.
	// This could be one of MaxMsgs, MaxBytes, or MaxAge.
	LimitsPolicy RetentionPolicy = iota
	// InterestPolicy specifies that when all known consumers have acknowledged a message it can be removed.
	InterestPolicy
	// WorkQueuePolicy specifies that when the first worker or subscriber acknowledges the message it can be removed.
	WorkQueuePolicy
)

// Discard Policy determines how we proceed when limits of messages or bytes are hit. The default, DicscardOld will
// remove older messages. DiscardNew will fail to store the new message.
type DiscardPolicy int

const (
	// DiscardOld will remove older messages to return to the limits.
	DiscardOld = iota
	// DiscardNew will error on a StoreMsg call
	DiscardNew
)

// StorageType determines how messages are stored for retention.
type StorageType int

const (
	// File specifies on disk, designated by the JetStream config StoreDir.
	FileStorage = StorageType(22)
	// MemoryStorage specifies in memory only.
	MemoryStorage = StorageType(33)
	// Any is for internals.
	AnyStorage = StorageType(44)
)

const (
	limitsPolicyString    = "limits"
	interestPolicyString  = "interest"
	workQueuePolicyString = "workqueue"
)

func (rp RetentionPolicy) String() string {
	switch rp {
	case LimitsPolicy:
		return "Limits"
	case InterestPolicy:
		return "Interest"
	case WorkQueuePolicy:
		return "WorkQueue"
	default:
		return "Unknown Retention Policy"
	}
}

func jsonString(s string) string {
	return "\"" + s + "\""
}

func (rp RetentionPolicy) MarshalJSON() ([]byte, error) {
	switch rp {
	case LimitsPolicy:
		return json.Marshal(limitsPolicyString)
	case InterestPolicy:
		return json.Marshal(interestPolicyString)
	case WorkQueuePolicy:
		return json.Marshal(workQueuePolicyString)
	default:
		return nil, fmt.Errorf("can not marshal %v", rp)
	}
}

func (rp *RetentionPolicy) UnmarshalJSON(data []byte) error {
	switch string(data) {
	case jsonString(limitsPolicyString):
		*rp = LimitsPolicy
	case jsonString(interestPolicyString):
		*rp = InterestPolicy
	case jsonString(workQueuePolicyString):
		*rp = WorkQueuePolicy
	default:
		return fmt.Errorf("can not unmarshal %q", data)
	}
	return nil
}

func (dp DiscardPolicy) String() string {
	switch dp {
	case DiscardOld:
		return "DiscardOld"
	case DiscardNew:
		return "DiscardNew"
	default:
		return "Unknown Discard Policy"
	}
}

func (dp DiscardPolicy) MarshalJSON() ([]byte, error) {
	switch dp {
	case DiscardOld:
		return json.Marshal("old")
	case DiscardNew:
		return json.Marshal("new")
	default:
		return nil, fmt.Errorf("can not marshal %v", dp)
	}
}

func (dp *DiscardPolicy) UnmarshalJSON(data []byte) error {
	switch strings.ToLower(string(data)) {
	case jsonString("old"):
		*dp = DiscardOld
	case jsonString("new"):
		*dp = DiscardNew
	default:
		return fmt.Errorf("can not unmarshal %q", data)
	}
	return nil
}

const (
	memoryStorageString = "memory"
	fileStorageString   = "file"
	anyStorageString    = "any"
)

func (st StorageType) String() string {
	switch st {
	case MemoryStorage:
		return "Memory"
	case FileStorage:
		return "File"
	case AnyStorage:
		return "Any"
	default:
		return "Unknown Storage Type"
	}
}

func (st StorageType) MarshalJSON() ([]byte, error) {
	switch st {
	case MemoryStorage:
		return json.Marshal(memoryStorageString)
	case FileStorage:
		return json.Marshal(fileStorageString)
	case AnyStorage:
		return json.Marshal(anyStorageString)
	default:
		return nil, fmt.Errorf("can not marshal %v", st)
	}
}

func (st *StorageType) UnmarshalJSON(data []byte) error {
	switch string(data) {
	case jsonString(memoryStorageString):
		*st = MemoryStorage
	case jsonString(fileStorageString):
		*st = FileStorage
	case jsonString(anyStorageString):
		*st = AnyStorage
	default:
		return fmt.Errorf("can not unmarshal %q", data)
	}
	return nil
}

const (
	ackNonePolicyString     = "none"
	ackAllPolicyString      = "all"
	ackExplicitPolicyString = "explicit"
)

func (ap AckPolicy) MarshalJSON() ([]byte, error) {
	switch ap {
	case AckNone:
		return json.Marshal(ackNonePolicyString)
	case AckAll:
		return json.Marshal(ackAllPolicyString)
	case AckExplicit:
		return json.Marshal(ackExplicitPolicyString)
	default:
		return nil, fmt.Errorf("can not marshal %v", ap)
	}
}

func (ap *AckPolicy) UnmarshalJSON(data []byte) error {
	switch string(data) {
	case jsonString(ackNonePolicyString):
		*ap = AckNone
	case jsonString(ackAllPolicyString):
		*ap = AckAll
	case jsonString(ackExplicitPolicyString):
		*ap = AckExplicit
	default:
		return fmt.Errorf("can not unmarshal %q", data)
	}
	return nil
}

const (
	replayInstantPolicyString  = "instant"
	replayOriginalPolicyString = "original"
)

func (rp ReplayPolicy) MarshalJSON() ([]byte, error) {
	switch rp {
	case ReplayInstant:
		return json.Marshal(replayInstantPolicyString)
	case ReplayOriginal:
		return json.Marshal(replayOriginalPolicyString)
	default:
		return nil, fmt.Errorf("can not marshal %v", rp)
	}
}

func (rp *ReplayPolicy) UnmarshalJSON(data []byte) error {
	switch string(data) {
	case jsonString(replayInstantPolicyString):
		*rp = ReplayInstant
	case jsonString(replayOriginalPolicyString):
		*rp = ReplayOriginal
	default:
		return fmt.Errorf("can not unmarshal %q", data)
	}
	return nil
}

const (
	deliverAllPolicyString       = "all"
	deliverLastPolicyString      = "last"
	deliverNewPolicyString       = "new"
	deliverByStartSequenceString = "by_start_sequence"
	deliverByStartTimeString     = "by_start_time"
	deliverLastPerPolicyString   = "last_per_subject"
	deliverUndefinedString       = "undefined"
)

func (p *DeliverPolicy) UnmarshalJSON(data []byte) error {
	switch string(data) {
	case jsonString(deliverAllPolicyString), jsonString(deliverUndefinedString):
		*p = DeliverAll
	case jsonString(deliverLastPolicyString):
		*p = DeliverLast
	case jsonString(deliverLastPerPolicyString):
		*p = DeliverLastPerSubject
	case jsonString(deliverNewPolicyString):
		*p = DeliverNew
	case jsonString(deliverByStartSequenceString):
		*p = DeliverByStartSequence
	case jsonString(deliverByStartTimeString):
		*p = DeliverByStartTime
	default:
		return fmt.Errorf("can not unmarshal %q", data)
	}

	return nil
}

func (p DeliverPolicy) MarshalJSON() ([]byte, error) {
	switch p {
	case DeliverAll:
		return json.Marshal(deliverAllPolicyString)
	case DeliverLast:
		return json.Marshal(deliverLastPolicyString)
	case DeliverLastPerSubject:
		return json.Marshal(deliverLastPerPolicyString)
	case DeliverNew:
		return json.Marshal(deliverNewPolicyString)
	case DeliverByStartSequence:
		return json.Marshal(deliverByStartSequenceString)
	case DeliverByStartTime:
		return json.Marshal(deliverByStartTimeString)
	default:
		return json.Marshal(deliverUndefinedString)
	}
}
