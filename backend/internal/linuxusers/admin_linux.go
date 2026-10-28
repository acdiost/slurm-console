//go:build linux

package linuxusers

import (
	"context"
	"encoding/json"
	"errors"
	"golang.org/x/sys/unix"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type diskStore struct{}

func rootDirectory(path string) error {
	if err := os.Mkdir(path, 0700); err != nil && !os.IsExist(err) {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0700 || info.Sys().(*syscall.Stat_t).Uid != 0 {
		return errors.New("unsafe directory")
	}
	return nil
}
func rootRead(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil, ErrAbsent
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Sys().(*syscall.Stat_t).Uid != 0 {
		return nil, errors.New("unsafe root file")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 16385))
	if len(data) > 16384 {
		return nil, errors.New("root record exceeds bound")
	}
	return data, err
}
func diskGet(path string, value any) error {
	raw, err := rootRead(path)
	if err != nil {
		return err
	}
	return StrictJSON(raw, value)
}
func diskPut(path string, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".record-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(raw)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
func (diskStore) Operation(id string) (v HostOperation, err error) {
	err = diskGet(filepath.Join(AdminStatePath, "operations", id+".json"), &v)
	return
}
func (diskStore) SaveOperation(id string, v HostOperation) error {
	return diskPut(filepath.Join(AdminStatePath, "operations", id+".json"), v)
}
func (diskStore) Lock(name string) (v LockState, err error) {
	err = diskGet(filepath.Join(AdminStatePath, "locks", name+".json"), &v)
	return
}
func (diskStore) SaveLock(name string, v LockState) error {
	return diskPut(filepath.Join(AdminStatePath, "locks", name+".json"), v)
}
func (diskStore) Target(name string) (v string, err error) {
	err = diskGet(filepath.Join(AdminStatePath, "targets", name+".json"), &v)
	return
}
func (diskStore) SaveTarget(name, id string) error {
	return diskPut(filepath.Join(AdminStatePath, "targets", name+".json"), id)
}

type localHost struct{}

func publicFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, MaxBytes+1))
	if len(b) > MaxBytes {
		return nil, errors.New("host metadata exceeds bound")
	}
	return b, err
}
func hostCommand(ctx context.Context, path string, args ...string) error {
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C", "LC_ALL=C"}
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	cmd.WaitDelay = time.Second
	configureProcess(cmd)
	return cmd.Run()
}
func getentAbsent(ctx context.Context, database, key string) bool {
	_, err := NewReader().Run(ctx, database, key)
	var exit *exec.ExitError
	return errors.As(err, &exit) && exit.ExitCode() == 2
}
func (localHost) CheckCreate(ctx context.Context, v CreateSpec) error {
	if !getentAbsent(ctx, "passwd", v.Name) || !getentAbsent(ctx, "passwd", strconv.FormatUint(uint64(v.UID), 10)) || !getentAbsent(ctx, "group", v.Name) {
		return errors.New("identity exists or NSS unavailable")
	}
	if _, err := os.Lstat("/home/" + v.Name); !os.IsNotExist(err) {
		return errors.New("home exists or unavailable")
	}
	parent, err := os.Lstat("/home")
	if err != nil || !parent.IsDir() || parent.Mode()&os.ModeSymlink != 0 || parent.Mode().Perm()&0022 != 0 || parent.Sys().(*syscall.Stat_t).Uid != 0 {
		return errors.New("unsafe home parent")
	}
	return nil
}
func (localHost) Create(ctx context.Context, v CreateSpec) error {
	return hostCommand(ctx, "/usr/sbin/useradd", "--uid", strconv.FormatUint(uint64(v.UID), 10), "--user-group", "--create-home", "--home-dir", "/home/"+v.Name, "--skel", AdminStatePath+"/skel", "--shell", v.Shell, "--comment", v.Comment, "--password", "!", "--expiredate", "1", "--no-log-init", "-K", "HOME_MODE=0700", "--", v.Name)
}
func (localHost) SetExpiry(ctx context.Context, name string, expiry int64) error {
	return hostCommand(ctx, "/usr/sbin/usermod", "--expiredate", strconv.FormatInt(expiry, 10), "--", name)
}
func (localHost) Inspect(ctx context.Context, name string) (AccountState, error) {
	snapshot, err := NewReader().Read(ctx, Request{Version: 1, Name: name})
	if err != nil || len(snapshot.Items) != 1 || snapshot.Items[0].Source != "local" {
		return AccountState{}, errors.New("not an exact local identity")
	}
	u := snapshot.Items[0]
	raw, err := publicFile("/etc/shadow")
	if err != nil {
		return AccountState{}, err
	}
	defer clear(raw)
	var shadow []string
	for _, line := range strings.Split(string(raw), "\n") {
		parts := strings.Split(line, ":")
		if len(parts) == 9 && parts[0] == name {
			if shadow != nil {
				return AccountState{}, errors.New("duplicate shadow identity")
			}
			shadow = parts
		}
	}
	if shadow == nil {
		return AccountState{}, errors.New("shadow identity missing")
	}
	expiry := int64(-1)
	if shadow[7] != "" {
		expiry, err = strconv.ParseInt(shadow[7], 10, 64)
		if err != nil || expiry < 0 || expiry > 3652058 {
			return AccountState{}, errors.New("invalid account expiry")
		}
	}
	privateGroup := false
	privileged := u.GID == 0
	groups, err := publicFile("/etc/group")
	if err != nil {
		return AccountState{}, err
	}
	for _, line := range strings.Split(string(groups), "\n") {
		v := strings.Split(line, ":")
		if len(v) != 4 {
			continue
		}
		if gid, e := strconv.ParseUint(v[2], 10, 32); e == nil && v[0] == name && uint32(gid) == u.GID {
			privateGroup = true
		}
		if v[0] == "root" || v[0] == "wheel" || v[0] == "sudo" || v[0] == "admin" {
			gid, e := strconv.ParseUint(v[2], 10, 32)
			if e == nil && uint32(gid) == u.GID {
				privileged = true
			}
			for _, member := range strings.Split(v[3], ",") {
				if member == name {
					privileged = true
				}
			}
		}
	}
	homeReady := false
	homeIdentity := ""
	if info, e := os.Lstat(u.Home); e == nil {
		st := info.Sys().(*syscall.Stat_t)
		homeReady = info.IsDir() && info.Mode()&os.ModeSymlink == 0 && info.Mode().Perm() == 0700 && st.Uid == u.UID
		homeIdentity = strconv.FormatUint(st.Dev, 10) + ":" + strconv.FormatUint(st.Ino, 10)
	}
	return AccountState{User: u, Expiry: expiry, Identity: Hash(struct {
		User             User
		CredentialPolicy []string
		HomeIdentity     string
	}{u, shadow[1:7], homeIdentity}), Privileged: privileged, PrivateGroup: privateGroup, HomeReady: homeReady}, nil
}
func ServeAdmin(input io.Reader, output io.Writer) int {
	if os.Geteuid() != 0 {
		return 2
	}
	raw, err := io.ReadAll(io.LimitReader(input, 8193))
	var request AdminRequest
	if err != nil || len(raw) > 8192 || StrictJSON(raw, &request) != nil {
		return 2
	}
	result := adminDenied("unavailable", "主机账户管理暂不可用。")
	var policy AdminPolicy
	if diskGet(AdminPolicyPath, &policy) != nil || policy.CallerUID == 0 || policy.MinUID < 1000 || policy.MaxUID > 60000 || policy.MinUID > policy.MaxUID {
		_ = json.NewEncoder(output).Encode(result)
		return 0
	}
	if uid := os.Getenv("SUDO_UID"); uid != "" && uid != strconv.FormatUint(uint64(policy.CallerUID), 10) {
		return 2
	}
	for _, path := range []string{AdminStatePath, AdminStatePath + "/operations", AdminStatePath + "/targets", AdminStatePath + "/locks", AdminStatePath + "/skel"} {
		if rootDirectory(path) != nil {
			_ = json.NewEncoder(output).Encode(result)
			return 0
		}
	}
	lock, err := os.OpenFile(AdminStatePath+"/host.lock", os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		_ = json.NewEncoder(output).Encode(result)
		return 0
	}
	defer lock.Close()
	if syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		_ = json.NewEncoder(output).Encode(adminDenied("busy", "主机账户管理繁忙。"))
		return 0
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if request.Mode != "status" && unix.Access("/etc", unix.W_OK) != nil {
		_ = json.NewEncoder(output).Encode(adminDenied("read_only", "主机账户数据库不允许写入，请由管理员检查执行环境。"))
		return 0
	}
	engine := Engine{Policy: policy, Host: localHost{}, Store: diskStore{}}
	switch request.Mode {
	case "capabilities", "prepare":
		result = engine.Prepare(ctx, request)
	case "execute":
		result = engine.Execute(ctx, request)
	case "status":
		result = engine.Status(ctx, request.OperationID)
	default:
		result = adminDenied("invalid_request", "主机请求模式不合法。")
	}
	if json.NewEncoder(output).Encode(result) != nil {
		return 1
	}
	return 0
}
