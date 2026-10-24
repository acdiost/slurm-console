package pamauth

import "errors"

var ErrDenied = errors.New("PAM authentication denied")
var ErrUnavailable = errors.New("PAM service unavailable")

type Request struct {
	Mode       string `json:"mode"`
	Username   string `json:"username,omitempty"`
	Password   string `json:"password,omitempty"`
	RemoteHost string `json:"remoteHost,omitempty"`
}
type Result struct {
	OK       bool   `json:"ok"`
	Code     string `json:"code,omitempty"`
	Username string `json:"username,omitempty"`
}
