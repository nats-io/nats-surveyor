package models

import (
	"encoding/json"
	"fmt"
)

type StoreCompression uint8

const (
	NoCompression StoreCompression = iota
	S2Compression
)

func (alg StoreCompression) String() string {
	switch alg {
	case NoCompression:
		return "None"
	case S2Compression:
		return "S2"
	default:
		return "Unknown StoreCompression"
	}
}

func (alg StoreCompression) MarshalJSON() ([]byte, error) {
	var str string
	switch alg {
	case S2Compression:
		str = "s2"
	case NoCompression:
		str = "none"
	default:
		return nil, fmt.Errorf("unknown compression algorithm")
	}
	return json.Marshal(str)
}

func (alg *StoreCompression) UnmarshalJSON(b []byte) error {
	var str string
	if err := json.Unmarshal(b, &str); err != nil {
		return err
	}
	switch str {
	case "s2":
		*alg = S2Compression
	case "none":
		*alg = NoCompression
	default:
		return fmt.Errorf("unknown compression algorithm")
	}
	return nil
}
