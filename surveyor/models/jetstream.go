package models

// JetStreamConfig determines this server's configuration.
// MaxMemory and MaxStore are in bytes.
type JetStreamConfig struct {
	MaxMemory  int64  `json:"max_memory"`
	MaxStore   int64  `json:"max_storage"`
	StoreDir   string `json:"store_dir,omitempty"`
	Domain     string `json:"domain,omitempty"`
	CompressOK bool   `json:"compress_ok,omitempty"`
	UniqueTag  string `json:"unique_tag,omitempty"`
}

// Statistics about JetStream for this server.
type JetStreamStats struct {
	Memory         uint64            `json:"memory"`
	Store          uint64            `json:"storage"`
	ReservedMemory uint64            `json:"reserved_memory"`
	ReservedStore  uint64            `json:"reserved_storage"`
	Accounts       int               `json:"accounts"`
	HAAssets       int               `json:"ha_assets"`
	API            JetStreamAPIStats `json:"api"`
}

type JetStreamAPIStats struct {
	Total    uint64 `json:"total"`
	Errors   uint64 `json:"errors"`
	Inflight uint64 `json:"inflight,omitempty"`
}
