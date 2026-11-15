package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"slurm-console/backend/internal/audit"
	"slurm-console/backend/internal/pamauth"
	_ "slurm-console/backend/migrations"
)

type setupPAMProvider struct {
	probes      int
	unavailable bool
}

func (p *setupPAMProvider) Run(_ context.Context, request pamauth.Request) (pamauth.Result, error) {
	if request.Mode == "probe" {
		p.probes++
		if p.unavailable {
			return pamauth.Result{}, pamauth.ErrUnavailable
		}
	}
	return pamauth.Result{OK: true, Username: request.Username}, nil
}

func setupPAMApp(t *testing.T) core.App {
	t.Helper()
	app := core.NewBaseApp(core.BaseAppConfig{DataDir: t.TempDir()})
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.ResetBootstrapState() })
	if err := app.RunAllMigrations(); err != nil {
		t.Fatal(err)
	}
	return app
}

func setupPAMReadiness(app core.App, provider *setupPAMProvider) {
	pamState(app).inspect = func(ctx context.Context) (PAMStatus, pamauth.Provider) {
		status := PAMStatus{Supported: true, HelperInstalled: true, PolicyInstalled: true}
		result, err := provider.Run(ctx, pamauth.Request{Mode: "probe"})
		if err != nil || !result.OK {
			status.Message = "请安装应用 PAM helper 和策略后重新检测。"
			return status, nil
		}
		status.Available = true
		return status, provider
	}
}

func setupPAMActor() audit.Actor {
	return audit.Actor{ID: "fixture", Label: "Fixture", Source: "local", RequestID: "fixture-request"}
}

func setupPAMAuthorize(core.App) error { return nil }

func TestPAMSetupPersistsAndReloadsAcrossStartup(t *testing.T) {
	t.Setenv("SLURM_CONSOLE_PAM_ENABLED", "false")
	app := setupPAMApp(t)
	provider := &setupPAMProvider{}
	setupPAMReadiness(app, provider)
	status, err := SavePAMSetup(context.Background(), app, true, 1, setupPAMActor(), setupPAMAuthorize)
	if err != nil || !status.Configured || status.Revision != 2 || provider.probes != 1 {
		t.Fatalf("enable: %+v, probes=%d, error=%v", status, provider.probes, err)
	}
	if active := app.Store().Get(pamProviderKey); active != provider {
		t.Fatal("provider not applied immediately")
	}
	dataDir := app.DataDir()
	if err := app.ResetBootstrapState(); err != nil {
		t.Fatal(err)
	}

	// A fresh app and lifecycle registration reproduce CLI startup, without an
	// HTTP server or the previous process's provider/store.
	restarted := core.NewBaseApp(core.BaseAppConfig{DataDir: dataDir})
	if err := ConfigurePAM(restarted); err != nil {
		t.Fatal(err)
	}
	restoredProvider := &setupPAMProvider{}
	setupPAMReadiness(restarted, restoredProvider)
	if err := restarted.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restarted.ResetBootstrapState() })
	if restarted.Store().Get(pamProviderKey) != restoredProvider || restoredProvider.probes != 1 {
		t.Fatal("saved choice not restored during bootstrap")
	}
	status, err = PAMSetupStatus(restarted)
	if err != nil || !status.Configured || !status.Available || status.Revision != 2 {
		t.Fatalf("restored status: %+v, %v", status, err)
	}
	if _, err := SavePAMSetup(context.Background(), restarted, false, 2, setupPAMActor(), setupPAMAuthorize); err != nil {
		t.Fatal(err)
	}
	// Explicit disable takes precedence even over a broken manual environment.
	t.Setenv("SLURM_CONSOLE_PAM_ENABLED", "not-a-bool")
	probes := restoredProvider.probes
	if err := ApplyPAMSetup(restarted); err != nil || restarted.Store().Get(pamProviderKey) != nil || restoredProvider.probes != probes {
		t.Fatal("saved disable did not override environment", err)
	}
}

func TestPAMSetupPreservesEnvironmentWithoutSavedOverride(t *testing.T) {
	app := setupPAMApp(t)
	provider := &setupPAMProvider{}
	setupPAMReadiness(app, provider)
	t.Setenv("SLURM_CONSOLE_PAM_ENABLED", "true")
	if err := ApplyPAMSetup(app); err != nil || app.Store().Get(pamProviderKey) != provider {
		t.Fatal("manual environment no longer works", err)
	}
	provider.unavailable = true
	if err := ApplyPAMSetup(app); !errors.Is(err, ErrPAMUnavailable) || app.Store().Get(pamProviderKey) != nil {
		t.Fatal("unavailable environment provider did not fail closed", err)
	}
	record, err := app.FindRecordById(pamSettingsCollection, PAMSettingsID)
	if err != nil || record.GetBool("has_override") || record.GetInt("revision") != 1 {
		t.Fatal("environment fallback wrote an override", err)
	}
}

func TestPAMSetupFailuresPreserveRuntimeAndSavedState(t *testing.T) {
	for _, mode := range []string{"probe", "unsupported", "revision", "permission", "audit"} {
		t.Run(mode, func(t *testing.T) {
			app := setupPAMApp(t)
			previous := &setupPAMProvider{}
			RegisterPAM(app, previous)
			candidate := &setupPAMProvider{}
			setupPAMReadiness(app, candidate)
			revision := 1
			authorize := setupPAMAuthorize
			denied := errors.New("management revoked")
			calls := 0
			switch mode {
			case "probe":
				candidate.unavailable = true
			case "unsupported":
				pamState(app).inspect = func(context.Context) (PAMStatus, pamauth.Provider) {
					return PAMStatus{Available: true}, candidate
				}
			case "revision":
				revision = 2
			case "permission":
				authorize = func(core.App) error {
					calls++
					if calls == 2 {
						return denied
					}
					return nil
				}
			case "audit":
				app.OnRecordCreate("operation_audits").BindFunc(func(*core.RecordEvent) error {
					return errors.New("synthetic audit failure")
				})
			}
			_, err := SavePAMSetup(context.Background(), app, true, revision, setupPAMActor(), authorize)
			if err == nil {
				t.Fatal("failed operation reported success")
			}
			if mode == "permission" && !errors.Is(err, denied) {
				t.Fatal("permission check not refreshed before commit", err)
			}
			if mode == "revision" && (!errors.Is(err, ErrPAMConflict) || candidate.probes != 0) {
				t.Fatal("stale revision probed or was not rejected", err)
			}
			record, readErr := app.FindRecordById(pamSettingsCollection, PAMSettingsID)
			if readErr != nil || record.GetInt("revision") != 1 || record.GetBool("has_override") || app.Store().Get(pamProviderKey) != previous {
				t.Fatal("failed operation changed saved or active provider", readErr)
			}
			if count, _ := app.CountRecords("operation_audits"); count != 0 {
				t.Fatal("rolled-back change retained an audit success")
			}
		})
	}
}

func TestPAMSetupUnavailableBootstrapAllowsLocalRepair(t *testing.T) {
	app := setupPAMApp(t)
	record, err := app.FindRecordById(pamSettingsCollection, PAMSettingsID)
	if err != nil {
		t.Fatal(err)
	}
	record.Set("has_override", true)
	record.Set("enabled", true)
	if err := app.Save(record); err != nil {
		t.Fatal(err)
	}
	dataDir := app.DataDir()
	if err := app.ResetBootstrapState(); err != nil {
		t.Fatal(err)
	}
	restarted := core.NewBaseApp(core.BaseAppConfig{DataDir: dataDir})
	if err := ConfigurePAM(restarted); err != nil {
		t.Fatal(err)
	}
	setupPAMReadiness(restarted, &setupPAMProvider{unavailable: true})
	if err := restarted.Bootstrap(); err != nil {
		t.Fatal("unavailable PAM prevented local administrator repair", err)
	}
	t.Cleanup(func() { _ = restarted.ResetBootstrapState() })
	status, err := PAMSetupStatus(restarted)
	if err != nil || status.Configured || status.Available || status.Message == "" || status.Revision != 1 {
		t.Fatalf("unavailable status lost actionable state: %+v, %v", status, err)
	}
}
