package testjd_test

import (
	"errors"
	"testing"

	"github.com/Buffer379x/jdweb/internal/jd"
	"github.com/Buffer379x/jdweb/internal/testjd"
)

func TestAccountsAreStateful(t *testing.T) {
	s, c := setup(t)
	if list, err := c.Accounts(ctx); err != nil || len(list) != 0 {
		t.Fatalf("an empty fake lists no accounts: %v %v", list, err)
	}
	id := s.AddAccount(testjd.Account{Hostname: "a.example", Username: "u", Enabled: true, Valid: true})
	if err := c.AddAccount(ctx, "b.example", "v", "pw"); err != nil {
		t.Fatal(err)
	}
	if err := c.DisableAccounts(ctx, []int64{id}); err != nil {
		t.Fatal(err)
	}
	list, _ := c.Accounts(ctx)
	if len(list) != 2 || list[0].Enabled || !list[1].Enabled {
		t.Fatalf("accounts = %+v", list)
	}
	if err := c.RemoveAccounts(ctx, []int64{id}); err != nil {
		t.Fatal(err)
	}
	if list, _ = c.Accounts(ctx); len(list) != 1 || list[0].Hostname != "b.example" {
		t.Fatalf("after remove: %+v", list)
	}
}

func TestSettingsWithMetaKeepTheirType(t *testing.T) {
	s, c := setup(t)
	const iface = "x.Config"
	s.SetConfig(iface, "Mode", "A")
	s.SetConfigMeta(iface, "Mode", testjd.ConfigMeta{AbstractType: "ENUM", EnumOptions: [][]string{{"A", ""}, {"B", "Bee"}}, Docs: "d"})
	s.SetConfig(iface, "Ports", []any{80.0})
	s.SetConfigMeta(iface, "Ports", testjd.ConfigMeta{AbstractType: "INT_LIST"})
	if err := c.ConfigSet(ctx, iface, "", "Mode", "B"); err != nil {
		t.Fatal(err)
	}
	if err := c.ConfigSet(ctx, iface, "", "Mode", "Z"); !errors.Is(err, jd.ErrRejected) {
		t.Fatalf("an enum value outside the options is refused: %v", err)
	}
	if err := c.ConfigSet(ctx, iface, "", "Ports", []int{1, 2}); err != nil {
		t.Fatal(err)
	}
	entries, err := c.ConfigQuery(ctx, jd.ConfigQuery{ConfigInterface: iface, Values: true, DefaultValues: true})
	if err != nil || len(entries) != 2 {
		t.Fatalf("entries = %v %v", entries, err)
	}
	for _, e := range entries {
		if e.Key == "Mode" && (e.AbstractType != "ENUM" || len(e.EnumOptions) != 2 || string(e.Value) != `"B"` || e.Docs != "d") {
			t.Fatalf("enum entry = %+v", e)
		}
	}
	if err := c.ConfigReset(ctx, iface, "", "Mode"); err != nil {
		t.Fatal(err)
	}
	if v := s.Config(iface, "Mode"); v != "A" {
		t.Fatalf("reset = %v", v)
	}
}

func TestPluginSettingsAreSeparate(t *testing.T) {
	s, c := setup(t)
	s.SetConfig("p.Config", "Flag", false)
	s.SetConfigMeta("p.Config", "Flag", testjd.ConfigMeta{AbstractType: "BOOLEAN", Plugin: "host.example"})
	g, _ := c.ConfigQuery(ctx, jd.ConfigQuery{Pattern: "Flag", Values: true})
	if len(g) != 0 {
		t.Fatalf("plugin settings are not in the general config: %v", g)
	}
	entries, err := c.PluginSettings(ctx, jd.ConfigQuery{Values: true})
	if err != nil || len(entries) != 1 || entries[0].Storage != "host.example" {
		t.Fatalf("plugin entries = %+v %v", entries, err)
	}
	if err := c.PluginSet(ctx, "p.Config", "host.example", "Flag", true); err != nil {
		t.Fatal(err)
	}
	if err := c.PluginSet(ctx, "p.Config", "other.example", "Flag", true); !errors.Is(err, jd.ErrRejected) {
		t.Fatalf("the wrong plugin is refused: %v", err)
	}
}

func TestStrictDialogOrder(t *testing.T) {
	s, c := setup(t)
	s.StrictDialogOrder()
	first := s.AddDialog(testjd.Dialog{Type: "a.A"})
	second := s.AddDialog(testjd.Dialog{Type: "b.B"})
	err := c.DialogAnswer(ctx, second, map[string]any{"closereason": "OK"})
	var e *jd.Error
	if !errors.As(err, &e) || e.Type == "" || e.Type[:13] != "Answer Dialog" {
		t.Fatalf("answering the second dialog first: %v", err)
	}
	if err := c.DialogAnswer(ctx, first, map[string]any{"closereason": "OK"}); err != nil {
		t.Fatal(err)
	}
	if err := c.DialogAnswer(ctx, second, map[string]any{"closereason": "OK"}); err != nil {
		t.Fatal(err)
	}
}

func TestExtractionQueueCancel(t *testing.T) {
	s, c := setup(t)
	s.AddQueueItem(testjd.QueueItem{ArchiveID: "a.rar", ArchiveName: "a.rar", ControllerID: 7, ControllerStatus: "RUNNING"})
	q, err := c.ExtractionQueue(ctx)
	if err != nil || len(q) != 1 {
		t.Fatalf("queue = %v %v", q, err)
	}
	if ok, _ := c.ExtractionCancel(ctx, 99); ok {
		t.Fatal("an unknown controller is not cancelled")
	}
	if ok, _ := c.ExtractionCancel(ctx, 7); !ok {
		t.Fatal("the running controller is cancelled")
	}
	if q, _ = c.ExtractionQueue(ctx); len(q) != 0 {
		t.Fatalf("queue after cancel = %v", q)
	}
}
