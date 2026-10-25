package jobcontrol

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"slurm-console/backend/internal/slurm/client"
	"slurm-console/backend/internal/slurm/models"
	_ "slurm-console/backend/migrations"
)

func TestConfirmationBindingExpiryAndRootArrayGuards(t *testing.T) {
	app := core.NewBaseApp(core.BaseAppConfig{DataDir: t.TempDir()})
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	defer app.ResetBootstrapState()
	if err := app.RunAllMigrations(); err != nil {
		t.Fatal(err)
	}
	service := For(app)
	p := proof{Actor: "actor", Cluster: "cluster", Job: "42", Operation: "expired-operation", Expires: time.Now().Add(-time.Second).Unix()}
	data, _ := json.Marshal(p)
	encoded := base64.RawURLEncoding.EncodeToString(data)
	mac := hmac.New(sha256.New, service.key[:])
	_, _ = mac.Write([]byte(encoded))
	token := encoded + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if _, err := service.Cancel("actor", "cluster", "42", "request", Input{Proof: token, Confirmation: "42", Reason: "expiry test"}); client.Code(err) != "confirmation_expired" {
		t.Fatalf("expired confirmation accepted: %v", err)
	}
	if _, err := service.parse(token, "different-actor", "cluster", "42"); client.Code(err) != "confirmation_invalid" {
		t.Fatal("actor binding ignored")
	}
	if _, err := service.parse(token+"x", "actor", "cluster", "42"); client.Code(err) != "confirmation_invalid" {
		t.Fatal("tampered signature accepted")
	}
	zero, uid, submitted := int64(0), int64(1001), int64(1700000000)
	job := models.Job{ID: "42", User: "alice", UserID: &uid, SubmitTime: models.Number{Value: &submitted}, RestartCount: &zero, ArrayJobID: models.Number{Value: &zero}, HetJobID: models.Number{Value: &zero}, HetJobOffset: models.Number{Value: &zero}}
	if !ordinary(job) {
		t.Fatal("ordinary job rejected")
	}
	job.ArrayTaskID.Value = &zero
	if ordinary(job) {
		t.Fatal("array element accepted")
	}
	job.ArrayTaskID.Value = nil
	job.ArrayTaskID.Infinite = true
	if ordinary(job) {
		t.Fatal("unknown array task marker accepted")
	}
	job.ArrayTaskID.Infinite = false
	job.UserID = &zero
	if ordinary(job) {
		t.Fatal("root job accepted")
	}
}
