package aiassist

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/pocketbase/pocketbase/core"
)

var ErrDisabled = errors.New("AI disabled")
var ErrConflict = errors.New("AI preview expired, consumed or changed")
var ErrBusy = errors.New("AI budget exceeded")

type Failure struct{ Sent bool }

func (e *Failure) Error() string { return "AI request did not produce a validated answer" }

type Scope struct {
	Actor, Cluster, Source, Target string
	Revision                       int
}
type Preview struct {
	ID          string          `json:"id"`
	Digest      string          `json:"digest"`
	ExpiresAt   string          `json:"expiresAt"`
	ObservedAt  string          `json:"observedAt"`
	Destination Destination     `json:"destination"`
	Request     json.RawMessage `json:"request"`
}
type pending struct {
	scope     Scope
	preview   Preview
	expires   time.Time
	configKey string
}
type Service struct {
	mu      sync.Mutex
	profile *Profile
	pending map[string]pending
	active  map[string]bool
	last    map[string]time.Time
	slots   chan struct{}
	ctx     context.Context
}

var serviceMu sync.Mutex

func Register(app core.App, p *Profile) *Service {
	ctx, cancel := context.WithCancel(context.Background())
	s := &Service{profile: p, pending: map[string]pending{}, active: map[string]bool{}, last: map[string]time.Time{}, slots: make(chan struct{}, 2), ctx: ctx}
	app.OnTerminate().BindFunc(func(e *core.TerminateEvent) error {
		cancel()
		if p != nil {
			p.client.CloseIdleConnections()
		}
		return e.Next()
	})
	app.Store().Set("platform.aiAssistance", s)
	return s
}
func For(app core.App) *Service {
	serviceMu.Lock()
	defer serviceMu.Unlock()
	if s, ok := app.Store().Get("platform.aiAssistance").(*Service); ok {
		return s
	}
	return Register(app, nil)
}
func (s *Service) Enabled(cid string) bool {
	return s.profile != nil && slices.Contains(s.profile.config.ClusterIDs, cid)
}
func (s *Service) Destination() Destination {
	if s.profile == nil {
		return Destination{}
	}
	return s.profile.destination()
}
func (s *Service) Prepare(scope Scope, packet Packet, observed string) (Preview, error) {
	if !s.Enabled(scope.Cluster) {
		return Preview{}, ErrDisabled
	}
	if packet.Version != 1 || (packet.Source != "controller" && packet.Source != "accounting") || len(packet.Facts) > 48 {
		return Preview{}, ErrConflict
	}
	data, e := json.Marshal(packet)
	if e != nil {
		return Preview{}, ErrConflict
	}
	body := map[string]any{"model": s.profile.config.Model, "messages": []map[string]string{{"role": "system", "content": Instruction}, {"role": "user", "content": string(data)}}, "stream": false, "store": false, "response_format": map[string]string{"type": "json_object"}, s.profile.config.TokenParameter: s.profile.config.MaxOutputTokens}
	raw, e := json.Marshal(body)
	if e != nil || len(raw) > 16384 {
		return Preview{}, ErrConflict
	}
	sum := sha256.Sum256(raw)
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for id, v := range s.pending {
		if now.After(v.expires) {
			delete(s.pending, id)
		} else if v.scope.Actor == scope.Actor {
			count++
		}
	}
	for actor, t := range s.last {
		if now.Sub(t) > time.Minute && !s.active[actor] {
			delete(s.last, actor)
		}
	}
	if len(s.pending) >= 64 || count >= 8 {
		return Preview{}, ErrBusy
	}
	expiry := now.Add(2 * time.Minute)
	v := Preview{rand.Text(), hex.EncodeToString(sum[:]), expiry.UTC().Format(time.RFC3339Nano), observed, s.profile.destination(), raw}
	s.pending[v.ID] = pending{scope, v, expiry, s.profile.key}
	return v, nil
}

type Invocation struct {
	Scope   Scope
	Preview Preview
	service *Service
	once    sync.Once
	called  atomic.Bool
}

func (s *Service) Take(actor, cid, id, digest string) (*Invocation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.pending[id]
	if !ok || v.scope.Actor != actor || v.scope.Cluster != cid || time.Now().After(v.expires) || !s.Enabled(cid) || v.configKey != s.profile.key || subtle.ConstantTimeCompare([]byte(v.preview.Digest), []byte(digest)) != 1 {
		return nil, ErrConflict
	}
	if s.active[actor] || time.Since(s.last[actor]) < 30*time.Second {
		return nil, ErrBusy
	}
	select {
	case s.slots <- struct{}{}:
	default:
		return nil, ErrBusy
	}
	delete(s.pending, id)
	s.active[actor] = true
	s.last[actor] = time.Now()
	return &Invocation{Scope: v.scope, Preview: v.preview, service: s}, nil
}
func (i *Invocation) Release() {
	i.once.Do(func() { s := i.service; s.mu.Lock(); delete(s.active, i.Scope.Actor); s.mu.Unlock(); <-s.slots })
}

type Observation struct {
	Text        string   `json:"text"`
	EvidenceIDs []string `json:"evidenceIds"`
}
type Check struct {
	Audience    string   `json:"audience"`
	Text        string   `json:"text"`
	EvidenceIDs []string `json:"evidenceIds"`
}
type Answer struct {
	Summary      string        `json:"summary"`
	Observations []Observation `json:"observations"`
	Checks       []Check       `json:"checks"`
	Limitations  []string      `json:"limitations"`
}

func prose(v string, max int) bool {
	return utf8.ValidString(v) && len(strings.TrimSpace(v)) > 0 && utf8.RuneCountInString(v) <= max && !strings.ContainsFunc(v, func(r rune) bool { return unicode.IsControl(r) && r != '\n' }) && !strings.ContainsAny(v, "`<>")
}
func validate(answer Answer, body []byte) bool {
	var request struct{ Messages []struct{ Content string } }
	if json.Unmarshal(body, &request) != nil || len(request.Messages) != 2 {
		return false
	}
	var p Packet
	if json.Unmarshal([]byte(request.Messages[1].Content), &p) != nil {
		return false
	}
	ids := map[string]bool{}
	for _, f := range p.Facts {
		ids[f.ID] = true
	}
	refs := func(values []string) bool {
		if len(values) < 1 || len(values) > 5 {
			return false
		}
		seen := map[string]bool{}
		for _, id := range values {
			if !ids[id] || seen[id] {
				return false
			}
			seen[id] = true
		}
		return true
	}
	if !prose(answer.Summary, 300) || answer.Observations == nil || len(answer.Observations) < 1 || len(answer.Observations) > 4 || answer.Checks == nil || len(answer.Checks) < 1 || len(answer.Checks) > 4 || answer.Limitations == nil || len(answer.Limitations) < 1 || len(answer.Limitations) > 4 {
		return false
	}
	for _, o := range answer.Observations {
		if !prose(o.Text, 300) || !refs(o.EvidenceIDs) {
			return false
		}
	}
	for _, c := range answer.Checks {
		if !slices.Contains([]string{"user", "admin"}, c.Audience) || !prose(c.Text, 300) || !refs(c.EvidenceIDs) {
			return false
		}
	}
	for _, v := range answer.Limitations {
		if !prose(v, 300) {
			return false
		}
	}
	return true
}
func (i *Invocation) Call(ctx context.Context) (Answer, error) {
	if !i.called.CompareAndSwap(false, true) {
		return Answer{}, &Failure{false}
	}
	var answer Answer
	p := i.service.profile
	key := ""
	if p.config.APIKeyFile != "" {
		v, e := apiKey(p.config.APIKeyFile)
		if e != nil {
			return answer, &Failure{false}
		}
		key = v
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(p.config.TimeoutSeconds)*time.Second)
	defer cancel()
	stop := context.AfterFunc(i.service.ctx, cancel)
	defer stop()
	req, e := http.NewRequestWithContext(ctx, http.MethodPost, p.config.Endpoint, bytes.NewReader(i.Preview.Request))
	if e != nil {
		return answer, &Failure{false}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	response, e := p.client.Do(req)
	if e != nil {
		return answer, &Failure{true}
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return answer, &Failure{true}
	}
	raw, e := io.ReadAll(io.LimitReader(response.Body, (128<<10)+1))
	if e != nil || len(raw) > 128<<10 {
		return answer, &Failure{true}
	}
	var envelope struct {
		Choices []struct {
			Index        int
			FinishReason string `json:"finish_reason"`
			Message      struct {
				Role         string
				Content      *string
				Refusal      *string
				ToolCalls    []json.RawMessage `json:"tool_calls"`
				FunctionCall json.RawMessage   `json:"function_call"`
			}
		}
	}
	if json.Unmarshal(raw, &envelope) != nil || len(envelope.Choices) != 1 {
		return answer, &Failure{true}
	}
	choice := envelope.Choices[0]
	m := choice.Message
	if choice.Index != 0 || choice.FinishReason != "stop" || m.Role != "assistant" || m.Content == nil || m.Refusal != nil && *m.Refusal != "" || len(m.ToolCalls) > 0 || len(m.FunctionCall) > 0 && string(m.FunctionCall) != "null" || len(*m.Content) > 24576 || key != "" && strings.Contains(*m.Content, key) {
		return answer, &Failure{true}
	}
	if decode([]byte(*m.Content), &answer) != nil || !validate(answer, i.Preview.Request) {
		return Answer{}, &Failure{true}
	}
	return answer, nil
}
