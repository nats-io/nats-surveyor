package models

import "fmt"

// ApiError is included in all responses if there was an error.
type ApiError struct {
	Code        int    `json:"code"`
	ErrCode     uint16 `json:"err_code,omitempty"`
	Description string `json:"description,omitempty"`
}

func (e *ApiError) Error() string {
	return fmt.Sprintf("%s (%d)", e.Description, e.ErrCode)
}
