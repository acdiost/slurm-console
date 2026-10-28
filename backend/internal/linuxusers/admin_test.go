package linuxusers

import (
	"context"
	"errors"
	"testing"
)

type memoryStore struct {
	ops     map[string]HostOperation
	locks   map[string]LockState
	targets map[string]string
	broken  bool
}

func newStore() *memoryStore {
	return &memoryStore{ops: map[string]HostOperation{}, locks: map[string]LockState{}, targets: map[string]string{}}
}
func (s *memoryStore) Operation(k string) (HostOperation, error) {
	v, ok := s.ops[k]
	if !ok {
		return v, ErrAbsent
	}
	return v, nil
}
func (s *memoryStore) SaveOperation(k string, v HostOperation) error {
	if s.broken {
		return errors.New("disk failed")
	}
	s.ops[k] = v
	return nil
}
func (s *memoryStore) Lock(k string) (LockState, error) {
	v, ok := s.locks[k]
	if !ok {
		return v, ErrAbsent
	}
	return v, nil
}
func (s *memoryStore) SaveLock(k string, v LockState) error { s.locks[k] = v; return nil }
func (s *memoryStore) Target(k string) (string, error) {
	v, ok := s.targets[k]
	if !ok {
		return v, ErrAbsent
	}
	return v, nil
}
func (s *memoryStore) SaveTarget(k, v string) error { s.targets[k] = v; return nil }

type fakeHost struct {
	users     map[string]AccountState
	writes    int
	uncertain bool
}

func (h *fakeHost) Inspect(_ context.Context, n string) (AccountState, error) {
	v, ok := h.users[n]
	if !ok {
		return v, ErrAbsent
	}
	return v, nil
}
func (h *fakeHost) CheckCreate(_ context.Context, v CreateSpec) error {
	for name, u := range h.users {
		if name == v.Name || u.User.UID == v.UID {
			return errors.New("exists")
		}
	}
	return nil
}
func (h *fakeHost) Create(_ context.Context, v CreateSpec) error {
	h.writes++
	h.users[v.Name] = AccountState{User: User{Name: v.Name, UID: v.UID, GID: v.UID, Comment: v.Comment, Home: "/home/" + v.Name, Shell: v.Shell, Source: "local", Kind: "regular"}, Expiry: 1, Identity: Hash(v), PrivateGroup: true, HomeReady: true}
	if h.uncertain {
		return errors.New("lost reply")
	}
	return nil
}
func (h *fakeHost) SetExpiry(_ context.Context, n string, x int64) error {
	h.writes++
	u := h.users[n]
	u.Expiry = x
	h.users[n] = u
	if h.uncertain {
		return errors.New("lost reply")
	}
	return nil
}
func fixtureEngine() (*Engine, *fakeHost, *memoryStore) {
	h := &fakeHost{users: map[string]AccountState{}}
	s := newStore()
	return &Engine{Policy: AdminPolicy{Enabled: true, CallerUID: 201, MinUID: 10000, MaxUID: 59999, ProtectedUsers: []string{"consoleadmin"}}, Host: h, Store: s}, h, s
}
func execution(t *testing.T, e *Engine, action string, spec CreateSpec, id string) AdminRequest {
	t.Helper()
	r := AdminRequest{Mode: "prepare", Action: action, Spec: spec}
	p := e.Prepare(context.Background(), r)
	if !p.Allowed {
		t.Fatal(p)
	}
	r.Mode = "execute"
	r.OperationID = id
	r.Expected = p.Fingerprint
	return r
}
func TestLocalAccountCreateUnlockLockRestoresExpiryAndNeverReplays(t *testing.T) {
	e, h, _ := fixtureEngine()
	ctx := context.Background()
	spec := CreateSpec{Name: "worker", UID: 10001, Comment: "Worker", Shell: "/bin/bash"}
	r := execution(t, e, "create", spec, "operation_create_123")
	v := e.Execute(ctx, r)
	if v.Status != "confirmed" || h.users["worker"].Expiry != 1 {
		t.Fatal(v)
	}
	if e.Execute(ctx, r).Status != "confirmed" || h.writes != 1 {
		t.Fatal("create replayed")
	}
	r = execution(t, e, "unlock", CreateSpec{Name: "worker"}, "operation_unlock_123")
	if e.Execute(ctx, r).Status != "confirmed" || h.users["worker"].Expiry != -1 {
		t.Fatal("unlock failed")
	}
	u := h.users["worker"]
	u.Expiry = 25000
	h.users["worker"] = u
	r = execution(t, e, "lock", CreateSpec{Name: "worker"}, "operation_lock_12345")
	if e.Execute(ctx, r).Status != "confirmed" {
		t.Fatal("lock failed")
	}
	r = execution(t, e, "unlock", CreateSpec{Name: "worker"}, "operation_restore_123")
	if e.Execute(ctx, r).Status != "confirmed" || h.users["worker"].Expiry != 25000 {
		t.Fatal("original future expiry lost")
	}
}
func TestLocalAccountUnknownReconcilesOnlyByReadingAndBlocksAnotherIntent(t *testing.T) {
	e, h, _ := fixtureEngine()
	h.uncertain = true
	ctx := context.Background()
	r := execution(t, e, "create", CreateSpec{Name: "worker", UID: 10001, Shell: "/sbin/nologin"}, "operation_unknown_123")
	if e.Execute(ctx, r).Status != "unknown" {
		t.Fatal("uncertainty lost")
	}
	if e.Prepare(ctx, AdminRequest{Mode: "prepare", Action: "unlock", Spec: CreateSpec{Name: "worker"}}).Allowed {
		t.Fatal("overlapping intent permitted")
	}
	if e.Status(ctx, r.OperationID).Status != "confirmed" || h.writes != 1 {
		t.Fatal("reconciliation resent mutation")
	}
	if e.Execute(ctx, r).Status != "confirmed" || h.writes != 1 {
		t.Fatal("replay resent mutation")
	}
	r.Spec.UID = 10002
	if e.Execute(ctx, r).Code != "conflict" {
		t.Fatal("operation reused with altered input")
	}
}
func TestLocalAccountPolicyStaleProofAndAuditDiskFailurePreventWrites(t *testing.T) {
	e, h, s := fixtureEngine()
	ctx := context.Background()
	spec := CreateSpec{Name: "worker", UID: 10001, Shell: "/bin/bash"}
	r := execution(t, e, "create", spec, "operation_disk_12345")
	s.broken = true
	if v := e.Execute(ctx, r); v.Status == "confirmed" || h.writes != 0 {
		t.Fatal(v)
	}
	s.broken = false
	r.Expected = "stale"
	if e.Execute(ctx, r).Code != "changed" || h.writes != 0 {
		t.Fatal("stale precondition allowed write")
	}
	for _, bad := range []CreateSpec{{Name: "root", UID: 0, Shell: "/bin/bash"}, {Name: "consoleadmin", UID: 10001, Shell: "/bin/bash"}, {Name: "../evil", UID: 10001, Shell: "/bin/bash"}, {Name: "worker", UID: 10001, Shell: "/bin/sh"}, {Name: "worker", UID: 10001, Comment: "evil\n", Shell: "/bin/bash"}} {
		if e.Prepare(ctx, AdminRequest{Mode: "prepare", Action: "create", Spec: bad}).Allowed {
			t.Fatal(bad)
		}
	}
	u := AccountState{User: User{Name: "worker", UID: 10001, GID: 10001, Source: "nss"}, Expiry: -1, Identity: "original"}
	h.users["worker"] = u
	if e.Prepare(ctx, AdminRequest{Mode: "prepare", Action: "lock", Spec: CreateSpec{Name: "worker"}}).Allowed {
		t.Fatal("directory user modified")
	}
	u.User.Source = "local"
	u.Privileged = true
	h.users["worker"] = u
	if e.Prepare(ctx, AdminRequest{Mode: "prepare", Action: "lock", Spec: CreateSpec{Name: "worker"}}).Allowed {
		t.Fatal("privileged user modified")
	}
	u.Privileged = false
	u.Expiry = 1
	h.users["worker"] = u
	if e.Prepare(ctx, AdminRequest{Mode: "prepare", Action: "unlock", Spec: CreateSpec{Name: "worker"}}).Allowed {
		t.Fatal("foreign expiry policy overridden")
	}
	s.locks["worker"] = LockState{Identity: "different", OriginalExpiry: -1}
	if e.Prepare(ctx, AdminRequest{Mode: "prepare", Action: "unlock", Spec: CreateSpec{Name: "worker"}}).Allowed {
		t.Fatal("replaced identity unlocked")
	}
}
