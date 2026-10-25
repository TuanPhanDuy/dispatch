package tui

import (
	"testing"
	"time"

	"github.com/adrg/xdg"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/tuanbin007/dispatch/internal/client"
	"github.com/tuanbin007/dispatch/internal/client/inprocess"
	"github.com/tuanbin007/dispatch/internal/config"
)

// newTestConfig loads a real config.Config backed by a temp XDG_CONFIG_HOME
// so Save() (called by several AppModel actions) writes somewhere safe
// instead of into whatever the test's CWD happens to be.
func newTestConfig(t *testing.T) *config.Config {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	xdg.Reload()
	t.Cleanup(xdg.Reload)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	cfg.Profiles = []config.AgentProfile{{Name: "cat", Command: "cat"}}
	return cfg
}

func newTestAppModel(t *testing.T) *AppModel {
	t.Helper()
	mgr := inprocess.New(100, 10)
	t.Cleanup(func() { _ = mgr.Close() })

	app := NewAppModel(mgr, newTestConfig(t))
	app.Init()
	return app
}

func TestAppModelNewSessionFlowAttachesOnCreate(t *testing.T) {
	app := newTestAppModel(t)

	model, _ := app.Update(newSessionRequestMsg{})
	app = model.(*AppModel)
	if app.mode != viewNewSession || app.wizard == nil {
		t.Fatalf("expected viewNewSession with a wizard, got mode=%v wizard=%v", app.mode, app.wizard)
	}

	model, _ = app.Update(createSessionMsg{
		profile: config.AgentProfile{Name: "cat", Command: "cat"},
		name:    "s1",
		cwd:     ".",
	})
	app = model.(*AppModel)
	if app.mode != viewAttached || app.attach == nil {
		t.Fatalf("expected viewAttached after create, got mode=%v attach=%v", app.mode, app.attach)
	}
}

func TestAppModelBackToListDetaches(t *testing.T) {
	app := newTestAppModel(t)

	model, _ := app.Update(createSessionMsg{
		profile: config.AgentProfile{Name: "cat", Command: "cat"},
		name:    "s1",
		cwd:     ".",
	})
	app = model.(*AppModel)
	if app.mode != viewAttached {
		t.Fatalf("expected viewAttached, got %v", app.mode)
	}

	model, _ = app.Update(backToListMsg{})
	app = model.(*AppModel)
	if app.mode != viewList || app.attach != nil {
		t.Fatalf("expected viewList with no attach, got mode=%v attach=%v", app.mode, app.attach)
	}
}

func TestAppModelCancelWizardReturnsToList(t *testing.T) {
	app := newTestAppModel(t)

	model, _ := app.Update(newSessionRequestMsg{})
	app = model.(*AppModel)

	model, _ = app.Update(cancelWizardMsg{})
	app = model.(*AppModel)
	if app.mode != viewList || app.wizard != nil {
		t.Fatalf("expected viewList with no wizard, got mode=%v wizard=%v", app.mode, app.wizard)
	}
}

func TestAppModelQuitAppReturnsQuitCmd(t *testing.T) {
	app := newTestAppModel(t)

	_, cmd := app.Update(quitAppMsg{})
	if cmd == nil {
		t.Fatal("expected a command")
	}
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Fatalf("expected tea.QuitMsg, got %#v", msg)
	}
}

func TestAppModelSessionListUpdatesListModel(t *testing.T) {
	app := newTestAppModel(t)

	summary := sessionListMsg(nil)
	model, _ := app.Update(summary)
	app = model.(*AppModel)
	if len(app.list.rows) != 0 {
		t.Fatalf("expected empty row list, got %+v", app.list.rows)
	}
}

func TestAppModelCreatePersistsDefinition(t *testing.T) {
	app := newTestAppModel(t)

	_, _ = app.Update(createSessionMsg{
		profile: config.AgentProfile{Name: "cat", Command: "cat"},
		name:    "s1",
		cwd:     ".",
	})

	if len(app.cfg.Sessions) != 1 || app.cfg.Sessions[0].Name != "s1" || app.cfg.Sessions[0].Profile != "cat" {
		t.Fatalf("expected persisted definition for s1, got %+v", app.cfg.Sessions)
	}
}

func TestAppModelStoppedDefinitionAppearsInMergedRows(t *testing.T) {
	app := newTestAppModel(t)
	app.cfg.UpsertSession(config.SessionDef{Name: "ghost", Profile: "cat", Cwd: "/tmp"})

	model, _ := app.Update(sessionListMsg(nil))
	app = model.(*AppModel)

	if len(app.list.rows) != 1 || app.list.rows[0].Live {
		t.Fatalf("expected one stopped row for ghost, got %+v", app.list.rows)
	}
	if app.list.rows[0].Name() != "ghost" {
		t.Fatalf("expected row name ghost, got %+v", app.list.rows[0])
	}
}

func TestAppModelRelaunchDefAttachesWithSameProfile(t *testing.T) {
	app := newTestAppModel(t)

	model, _ := app.Update(relaunchDefMsg{def: config.SessionDef{Name: "ghost", Profile: "cat", Cwd: "."}})
	app = model.(*AppModel)

	if app.mode != viewAttached || app.attach == nil {
		t.Fatalf("expected viewAttached after relaunch, got mode=%v attach=%v", app.mode, app.attach)
	}
}

func TestAppModelRelaunchDefUnknownProfileShowsToast(t *testing.T) {
	app := newTestAppModel(t)

	model, _ := app.Update(relaunchDefMsg{def: config.SessionDef{Name: "ghost", Profile: "does-not-exist", Cwd: "."}})
	app = model.(*AppModel)

	if app.mode == viewAttached {
		t.Fatal("expected relaunch with unknown profile to NOT attach")
	}
	if app.toast == "" {
		t.Fatal("expected a toast explaining the missing profile")
	}
}

func TestAppModelRemoveDefDeletesFromConfig(t *testing.T) {
	app := newTestAppModel(t)
	app.cfg.UpsertSession(config.SessionDef{Name: "ghost", Profile: "cat", Cwd: "."})

	_, _ = app.Update(removeDefMsg{name: "ghost"})

	if len(app.cfg.Sessions) != 0 {
		t.Fatalf("expected definition removed, got %+v", app.cfg.Sessions)
	}
}

func TestAppModelRemoveDefWithIDRemovesLiveExitedSession(t *testing.T) {
	app := newTestAppModel(t)

	model, _ := app.Update(createSessionMsg{
		profile: config.AgentProfile{Name: "cat", Command: "cat"},
		name:    "s1",
		cwd:     ".",
	})
	app = model.(*AppModel)
	id := clientSummaryFor(app, "s1").ID

	if err := app.manager.Kill(id); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	waitUntilClient(t, func() bool {
		return clientSummaryFor(app, "s1").Status == client.StatusExited
	})

	_, _ = app.Update(removeDefMsg{id: id, name: "s1"})

	if len(app.cfg.Sessions) != 0 {
		t.Fatalf("expected definition removed from config, got %+v", app.cfg.Sessions)
	}
	sessions, _ := app.manager.List()
	for _, s := range sessions {
		if s.ID == id {
			t.Fatalf("expected session %s removed from manager, still present: %+v", id, s)
		}
	}
}

func waitUntilClient(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met before timeout")
}

func TestAppModelRenameStoppedDefinitionUpdatesConfig(t *testing.T) {
	app := newTestAppModel(t)
	app.cfg.UpsertSession(config.SessionDef{Name: "old-name", Profile: "cat", Cwd: "/tmp"})

	row := Row{Live: false, Def: config.SessionDef{Name: "old-name", Profile: "cat", Cwd: "/tmp"}}
	_, _ = app.Update(renameSubmitMsg{row: row, name: "new-name"})

	if len(app.cfg.Sessions) != 1 || app.cfg.Sessions[0].Name != "new-name" || app.cfg.Sessions[0].Cwd != "/tmp" {
		t.Fatalf("expected renamed definition, got %+v", app.cfg.Sessions)
	}
}

func TestAppModelRenameLiveSessionUpdatesConfigDef(t *testing.T) {
	app := newTestAppModel(t)

	model, _ := app.Update(createSessionMsg{
		profile: config.AgentProfile{Name: "cat", Command: "cat"},
		name:    "old-name",
		cwd:     ".",
	})
	app = model.(*AppModel)

	row := Row{Live: true, Summary: clientSummaryFor(app, "old-name")}
	_, _ = app.Update(renameSubmitMsg{row: row, name: "new-name"})

	if len(app.cfg.Sessions) != 1 || app.cfg.Sessions[0].Name != "new-name" || app.cfg.Sessions[0].Profile != "cat" {
		t.Fatalf("expected renamed definition with profile preserved, got %+v", app.cfg.Sessions)
	}
}

func clientSummaryFor(app *AppModel, name string) client.SessionSummary {
	sessions, _ := app.manager.List()
	for _, s := range sessions {
		if s.Name == name {
			return s
		}
	}
	return client.SessionSummary{}
}
