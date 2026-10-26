package nodecontrol

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"github.com/pocketbase/pocketbase/core"
	"slurm-console/backend/internal/slurm/client"
	"slurm-console/backend/internal/slurm/models"
	_ "slurm-console/backend/migrations"
	"testing"
	"time"
)

func TestNodeConfirmationExpiryBindingAndProtectedStates(t *testing.T) {
	app := core.NewBaseApp(core.BaseAppConfig{DataDir: t.TempDir()})
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	defer app.ResetBootstrapState()
	if err := app.RunAllMigrations(); err != nil {
		t.Fatal(err)
	}
	service := For(app)
	p := proof{Actor: "actor", Cluster: "cluster", Node: "node1", Action: "drain", Operation: "expired", Expires: time.Now().Add(-time.Second).Unix()}
	data, _ := json.Marshal(p)
	encoded := base64.RawURLEncoding.EncodeToString(data)
	mac := hmac.New(sha256.New, service.key[:])
	_, _ = mac.Write([]byte(encoded))
	token := encoded + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if _, err := service.Execute("actor", "cluster", "node1", "drain", "request", Input{Proof: token, Confirmation: "node1", Reason: "maintenance"}); client.Code(err) != "confirmation_expired" {
		t.Fatal(err)
	}
	for _, values := range [][4]string{{"other", "cluster", "node1", "drain"}, {"actor", "other", "node1", "drain"}, {"actor", "cluster", "node2", "drain"}, {"actor", "cluster", "node1", "resume"}} {
		if _, err := service.parse(token, values[0], values[1], values[2], values[3]); client.Code(err) != "confirmation_invalid" {
			t.Fatal("unbound token")
		}
	}
	if _, err := service.parse(token+"x", "actor", "cluster", "node1", "drain"); err == nil {
		t.Fatal("signature ignored")
	}
	for _, states := range [][]string{{"DOWN", "POWERED_DOWN"}, {"DOWN", "MAINTENANCE"}, {"DOWN", "REBOOT_REQUESTED"}, {"UNKNOWN"}, {"FUTURE"}} {
		if statePolicy(models.Node{States: states}, "resume") == "" {
			t.Fatal("unsafe Resume accepted", states)
		}
	}
	if reached(models.Node{States: []string{"IDLE", "NOT_RESPONDING"}}, "resume", "") {
		t.Fatal("unregistered node reported as ready")
	}
	if !reached(models.Node{States: []string{"ALLOCATED", "DRAIN"}, Reason: "maintenance"}, "drain", "maintenance") {
		t.Fatal("Drain must allow existing jobs to continue")
	}
}
