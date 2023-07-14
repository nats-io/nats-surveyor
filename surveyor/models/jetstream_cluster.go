package models

// Used to guide placement of streams and meta controllers in clustered JetStream.
type Placement struct {
	Cluster string   `json:"cluster,omitempty"`
	Tags    []string `json:"tags,omitempty"`
}
