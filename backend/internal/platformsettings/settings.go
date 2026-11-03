// Package platformsettings stores reviewed, non-secret platform preferences.
package platformsettings

import (
	"encoding/json"
	"errors"
	"os"
	"runtime"
	"runtime/debug"
	"sync"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"slurm-console/backend/internal/aiassist"
)

const ID = "platformglobal1"

var ErrConflict = errors.New("settings revision conflict")

type Identity struct {
	Name           string `json:"name"`
	Description    string `json:"description"`
	LoginNotice    string `json:"loginNotice"`
	SupportContact string `json:"supportContact"`
}
type View struct {
	Identity
	Revision  int    `json:"revision"`
	UpdatedAt string `json:"updatedAt"`
}

func Read(app core.App) (View, error) {
	r, e := app.FindRecordById("platform_settings", ID)
	if e != nil {
		return View{}, e
	}
	return View{Identity{r.GetString("name"), r.GetString("description"), r.GetString("login_notice"), r.GetString("support_contact")}, r.GetInt("revision"), r.GetString("updated")}, nil
}
func Set(r *core.Record, v Identity) {
	r.Set("name", v.Name)
	r.Set("description", v.Description)
	r.Set("login_notice", v.LoginNotice)
	r.Set("support_contact", v.SupportContact)
}

type AI struct {
	Enabled         bool     `json:"enabled"`
	Label           string   `json:"label"`
	Endpoint        string   `json:"endpoint"`
	Model           string   `json:"model"`
	ClusterIDs      []string `json:"clusterIds"`
	TimeoutSeconds  int      `json:"timeoutSeconds"`
	MaxOutputTokens int      `json:"maxOutputTokens"`
	TokenParameter  string   `json:"tokenParameter"`
}
type AIView struct {
	AI
	Revision      int    `json:"revision"`
	Source        string `json:"source"`
	KeyConfigured bool   `json:"keyConfigured"`
	CAConfigured  bool   `json:"caConfigured"`
}

func merge(v AI, base aiassist.Config) aiassist.Config {
	return aiassist.Config{Enabled: v.Enabled, Label: v.Label, Endpoint: v.Endpoint, Model: v.Model, ClusterIDs: v.ClusterIDs, TimeoutSeconds: v.TimeoutSeconds, MaxOutputTokens: v.MaxOutputTokens, TokenParameter: v.TokenParameter, APIKeyFile: base.APIKeyFile, CAFile: base.CAFile}
}
func AIConfig(app core.App) (AIView, aiassist.Config, error) {
	base, _ := app.Store().Get("platform.aiBase").(aiassist.Config)
	v := AI{base.Enabled, base.Label, base.Endpoint, base.Model, append([]string{}, base.ClusterIDs...), base.TimeoutSeconds, base.MaxOutputTokens, base.TokenParameter}
	if v.Label == "" {
		v.Label = "AI 服务"
	}
	if v.TimeoutSeconds == 0 {
		v.TimeoutSeconds = 30
	}
	if v.MaxOutputTokens == 0 {
		v.MaxOutputTokens = 2048
	}
	if v.TokenParameter == "" {
		v.TokenParameter = "max_completion_tokens"
	}
	r, e := app.FindRecordById("platform_settings", ID)
	if e != nil {
		return AIView{}, base, e
	}
	source := "server"
	raw := r.GetString("ai_override")
	if raw != "" && raw != "null" {
		if e = json.Unmarshal([]byte(raw), &v); e != nil {
			return AIView{}, base, e
		}
		source = "platform"
	}
	if v.Endpoint != "" {
		if err := aiassist.ValidateEndpoint(v.Endpoint, base.APIKeyFile != "" || base.CAFile != ""); err != nil {
			return AIView{}, base, err
		}
	}
	return AIView{v, r.GetInt("revision"), source, base.APIKeyFile != "", base.CAFile != ""}, merge(v, base), nil
}
func Profile(app core.App, v AI) (*aiassist.Profile, error) {
	base, _ := app.Store().Get("platform.aiBase").(aiassist.Config)
	if v.Endpoint != "" {
		if err := aiassist.ValidateEndpoint(v.Endpoint, base.APIKeyFile != "" || base.CAFile != ""); err != nil {
			return nil, err
		}
	}
	return aiassist.NewProfile(merge(v, base))
}
func ApplyAI(app core.App) error {
	_, cfg, e := AIConfig(app)
	if e != nil {
		return e
	}
	p, e := aiassist.NewProfile(cfg)
	if e != nil {
		return e
	}
	aiassist.Register(app, p)
	return nil
}

var initMu sync.Mutex

func WriteMutex(app core.App) *sync.Mutex {
	initMu.Lock()
	defer initMu.Unlock()
	if m, ok := app.Store().Get("platform.settingsMutex").(*sync.Mutex); ok {
		return m
	}
	m := new(sync.Mutex)
	app.Store().Set("platform.settingsMutex", m)
	return m
}

var started = time.Now().UTC().Format(time.RFC3339Nano)

func Runtime() map[string]any {
	commit := "development"
	dirty := false
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, s := range info.Settings {
			switch s.Key {
			case "vcs.revision":
				commit = s.Value
			case "vcs.modified":
				dirty = s.Value == "true"
			}
		}
	}
	return map[string]any{"startedAt": started, "goVersion": runtime.Version(), "os": runtime.GOOS, "arch": runtime.GOARCH, "revision": commit, "modified": dirty, "runAsRoot": os.Geteuid() == 0, "database": "SQLite"}
}
