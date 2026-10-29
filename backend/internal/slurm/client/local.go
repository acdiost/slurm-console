package client

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"
)

// LocalCommand performs one request as the approved Unix socket peer. It never
// opens the platform database and never inherits root credentials or environment.
const LocalCommand = "slurm-unix-request"

type localRequest struct {
	Endpoint, Version, Method, Path string
	Timeout                         time.Duration
	Body                            []byte
	Write                           bool
}
type localResponse struct {
	Envelope      Envelope
	Sent          bool
	Code, Message string
}

type localOutput struct {
	bytes.Buffer
	exceeded bool
}

func (b *localOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 16<<20 {
		b.exceeded = true
		return len(p), nil
	}
	return b.Buffer.Write(p)
}

func (c *Client) localRequest(ctx context.Context, method, path string, body []byte, sent *bool) (Envelope, error) {
	fail := func() (Envelope, error) {
		return Envelope{}, Failure("unreachable", "Slurm 本地连接进程不可用，请检查连接身份与 socket 权限。")
	}
	ctx, cancel := context.WithTimeout(ctx, c.config.Timeout)
	defer cancel()
	executable, err := os.Executable()
	if err != nil {
		return fail()
	}
	cmd := exec.CommandContext(ctx, executable, LocalCommand)
	if configureLocalIdentity(cmd, c.config.LocalUsername) != nil {
		return fail()
	}
	raw, _ := json.Marshal(localRequest{c.config.Endpoint, c.config.Version, method, path, c.config.Timeout, body, sent != nil})
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C", "LC_ALL=C"}
	// The working directory can be a root-private database directory.
	cmd.Dir = "/"
	cmd.Stdin = bytes.NewReader(raw)
	cmd.Stderr = io.Discard
	cmd.WaitDelay = time.Second
	out := new(localOutput)
	cmd.Stdout = out
	if cmd.Start() != nil {
		return fail()
	}
	// Losing a child reply after a write starts is an unknown outcome, never a
	// confirmed rejection. Existing operation reconciliation handles this case.
	if sent != nil {
		*sent = true
	}
	err = cmd.Wait()
	if ctx.Err() != nil {
		if ctx.Err() == context.Canceled {
			return Envelope{}, context.Canceled
		}
		return Envelope{}, Failure("timeout", "Slurm 请求超时。")
	}
	if err != nil || out.exceeded {
		return fail()
	}
	var result localResponse
	if json.Unmarshal(out.Bytes(), &result) != nil {
		return fail()
	}
	if sent != nil {
		*sent = result.Sent
	}
	if result.Code != "" {
		return Envelope{}, Failure(result.Code, result.Message)
	}
	return result.Envelope, nil
}

// ServeLocal accepts only bounded Unix requests. Callers use fixed paths from
// Read, Cancel and ControlNode, and perform all platform authorization first.
func ServeLocal(in io.Reader, out io.Writer) int {
	raw, err := io.ReadAll(io.LimitReader(in, 16385))
	if err != nil || len(raw) > 16384 {
		return 2
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var r localRequest
	if decoder.Decode(&r) != nil || decoder.Decode(new(any)) != io.EOF {
		return 2
	}
	c, err := New(Config{Endpoint: r.Endpoint, AuthType: "local", Version: r.Version, Timeout: r.Timeout})
	if err != nil || len(r.Endpoint) < 5 || r.Endpoint[:5] != "unix:" || !validLocalRequest(r) {
		return 2
	}
	var sent *bool
	result := localResponse{}
	if r.Write {
		sent = &result.Sent
	}
	result.Envelope, err = c.request(context.Background(), r.Method, r.Path, r.Body, sent)
	if err != nil {
		result.Code = Code(err)
		result.Message = err.Error()
	}
	if json.NewEncoder(out).Encode(result) != nil {
		return 1
	}
	return 0
}

func validLocalRequest(r localRequest) bool {
	dbPrefix := "/slurmdb/" + r.Version + "/"
	if strings.HasPrefix(r.Path, dbPrefix) {
		if r.Method == "GET" && !r.Write && len(r.Body) == 0 {
			if r.Path == dbPrefix+"accounts/" || r.Path == dbPrefix+"users/?with_assocs=true" || r.Path == dbPrefix+"qos/" || r.Path == dbPrefix+"tres/" {
				return true
			}
			u, err := url.Parse(r.Path)
			if err != nil || u.Path != dbPrefix+"associations/" {
				return false
			}
			q, err := url.ParseQuery(u.RawQuery)
			return err == nil && len(q) == 1 && len(q["cluster"]) == 1 && ValidAccountingName(q.Get("cluster")) && r.Path == dbPrefix+"associations/?cluster="+url.QueryEscape(q.Get("cluster"))
		}
		// Root-generated writes must be the exact serialization of an approved
		// mutation; arbitrary JSON, CSV selectors and global deletions are rejected.
		return validAccountingWire(r) || validQOSWire(r) || validTRESWire(r)
	}
	prefix := "/slurm/" + r.Version + "/"
	if r.Method == "GET" && !r.Write && len(r.Body) == 0 {
		if r.Path == prefix+"shares" {
			return true
		}
		for _, resource := range []Resource{Ping, Jobs, Nodes, Partitions, Reservations} {
			if r.Path == prefix+string(resource)+"/" {
				return true
			}
		}
		if len(r.Path) > len(prefix+"job/") && r.Path[:len(prefix+"job/")] == prefix+"job/" {
			return ValidJobID(r.Path[len(prefix+"job/"):])
		}
		if len(r.Path) > len(prefix+"node/") && r.Path[:len(prefix+"node/")] == prefix+"node/" {
			return ValidNodeName(r.Path[len(prefix+"node/"):])
		}
	}
	if validReservationWire(r) {
		return true
	}
	if r.Method == "DELETE" && r.Write && len(r.Body) == 0 {
		start, end := prefix+"job/", "?signal=KILL"
		if len(r.Path) > len(start)+len(end) && r.Path[:len(start)] == start && r.Path[len(r.Path)-len(end):] == end {
			return ValidJobID(r.Path[len(start) : len(r.Path)-len(end)])
		}
	}
	if r.Method == "POST" && r.Write {
		start := prefix + "node/"
		if len(r.Path) <= len(start) || r.Path[:len(start)] != start || !ValidNodeTarget(r.Path[len(start):]) {
			return false
		}
		var body struct {
			State  []string `json:"state"`
			Reason string   `json:"reason"`
		}
		d := json.NewDecoder(bytes.NewReader(r.Body))
		d.DisallowUnknownFields()
		if d.Decode(&body) != nil || d.Decode(new(any)) != io.EOF || len(body.State) != 1 {
			return false
		}
		return body.State[0] == "RESUME" && body.Reason == "" || body.State[0] == "DRAIN" && len(body.Reason) >= 2 && len(body.Reason) <= 1200
	}
	return false
}
