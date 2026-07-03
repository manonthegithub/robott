package api

type ledRequest struct {
	On bool `json:"on"`
}

type stepperRequest struct {
	Steps int    `json:"steps"`
	Dir   string `json:"dir"`
}

type servoRequest struct {
	AngleDeg float64 `json:"angle_deg"`
}

type queuedResponse struct {
	Status string `json:"status"`
}

type errorResponse struct {
	Error string `json:"error"`
}
