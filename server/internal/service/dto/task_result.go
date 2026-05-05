package dto

type TaskResult struct {
	Status string  `json:"status"`
	Result *string `json:"result,omitempty"`
	Reason *string `json:"reason,omitempty"`
	Notes  *string `json:"notes,omitempty"`
}
