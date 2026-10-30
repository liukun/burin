package quitter

import (
	"reflect"
	"testing"
	"time"
)

var (
	t0 = time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC)

	slackRule   = Rule{Name: "Slack", Match: ByName, Value: "Slack", After: 30 * time.Minute, Action: Hide}
	previewRule = Rule{Name: "Preview", Match: ByBundle, Value: "com.apple.Preview", After: 10 * time.Minute, Action: Quit}
	canaryRule  = Rule{Name: "Dev Chrome", Match: ByPath, Value: "/Apps/Canary.app", After: time.Hour, Action: Quit}
	cfg         = Config{Rules: []Rule{slackRule, previewRule, canaryRule}}

	slack   = App{PID: 10, Name: "Slack", BundleID: "com.tinyspeck.slackmacgap", Path: "/Applications/Slack.app"}
	preview = App{PID: 20, Name: "预览", BundleID: "com.apple.Preview", Path: "/System/Applications/Preview.app"}
	canary  = App{PID: 30, Name: "Google Chrome Canary", BundleID: "com.google.Chrome.canary", Path: "/Apps/Canary.app"}
	kitty   = App{PID: 40, Name: "kitty", BundleID: "net.kovidgoyal.kitty", Path: "/Applications/kitty.app"}
)

func active(a App) App { a.Active = true; return a }
func hidden(a App) App { a.Hidden = true; return a }

// pids lists the pids of steps, in order.
func pids(steps []Step) []int32 {
	out := []int32{}
	for _, s := range steps {
		out = append(out, s.App.PID)
	}
	return out
}

func TestPlanFirstSeenIsActiveNow(t *testing.T) {
	steps, next := Plan(cfg, []App{slack, preview, kitty}, nil, t0)
	if len(steps) != 0 {
		t.Fatalf("steps = %+v, want none for apps seen the first time", steps)
	}
	want := State{10: t0, 20: t0, 40: t0}
	if !reflect.DeepEqual(next, want) {
		t.Fatalf("next = %+v, want %+v", next, want)
	}
}

func TestPlanFrontmostResetsTimer(t *testing.T) {
	prev := State{10: t0}
	now := t0.Add(time.Hour)
	steps, next := Plan(cfg, []App{active(slack)}, prev, now)
	if len(steps) != 0 {
		t.Fatalf("steps = %+v, want none for the frontmost app", steps)
	}
	if !next[10].Equal(now) {
		t.Fatalf("next[10] = %v, want %v", next[10], now)
	}
}

func TestPlanWaitsForThePeriod(t *testing.T) {
	prev := State{10: t0}
	// Exactly the period is not yet over it.
	steps, next := Plan(cfg, []App{slack}, prev, t0.Add(30*time.Minute))
	if len(steps) != 0 {
		t.Fatalf("steps = %+v, want none at exactly the period", steps)
	}
	if !next[10].Equal(t0) {
		t.Fatalf("next[10] = %v, want unchanged %v", next[10], t0)
	}
}

func TestPlanHide(t *testing.T) {
	prev := State{10: t0}
	now := t0.Add(31 * time.Minute)
	steps, next := Plan(cfg, []App{slack}, prev, now)
	want := []Step{{App: slack, Rule: slackRule, Idle: 31 * time.Minute}}
	if !reflect.DeepEqual(steps, want) {
		t.Fatalf("steps = %+v, want %+v", steps, want)
	}
	if !next[10].Equal(t0) {
		t.Fatalf("next[10] = %v, want unchanged %v after a hide", next[10], t0)
	}
	steps, _ = Plan(cfg, []App{hidden(slack)}, next, now.Add(time.Minute))
	if len(steps) != 0 {
		t.Fatalf("steps = %+v, want none for a hidden app", steps)
	}
}

func TestPlanQuitWaitsAnotherPeriod(t *testing.T) {
	prev := State{20: t0, 30: t0}
	now := t0.Add(2 * time.Hour)
	steps, next := Plan(cfg, []App{preview, canary}, prev, now)
	if got, want := pids(steps), []int32{20, 30}; !reflect.DeepEqual(got, want) {
		t.Fatalf("steps = %v, want %v", got, want)
	}
	if steps[0].Rule.Name != "Preview" || steps[1].Rule.Name != "Dev Chrome" {
		t.Fatalf("rules = %q, %q, want Preview, Dev Chrome", steps[0].Rule.Name, steps[1].Rule.Name)
	}
	if !next[20].Equal(now) || !next[30].Equal(now) {
		t.Fatalf("next = %+v, want quit apps active at %v", next, now)
	}
	// Still running, e.g. asking about unsaved changes: asked again only
	// after another period.
	steps, _ = Plan(cfg, []App{preview}, next, now.Add(5*time.Minute))
	if len(steps) != 0 {
		t.Fatalf("steps = %+v, want none within the next period", steps)
	}
	steps, _ = Plan(cfg, []App{preview}, next, now.Add(11*time.Minute))
	if got, want := pids(steps), []int32{20}; !reflect.DeepEqual(got, want) {
		t.Fatalf("steps = %v, want %v after another period", got, want)
	}
}

func TestPlanQuitsHiddenApp(t *testing.T) {
	steps, _ := Plan(cfg, []App{hidden(preview)}, State{20: t0}, t0.Add(time.Hour))
	if got, want := pids(steps), []int32{20}; !reflect.DeepEqual(got, want) {
		t.Fatalf("steps = %v, want %v", got, want)
	}
}

func TestPlanUnmatchedAndGoneApps(t *testing.T) {
	prev := State{40: t0, 99: t0}
	steps, next := Plan(cfg, []App{kitty}, prev, t0.Add(24*time.Hour))
	if len(steps) != 0 {
		t.Fatalf("steps = %+v, want none for an app without a rule", steps)
	}
	want := State{40: t0}
	if !reflect.DeepEqual(next, want) {
		t.Fatalf("next = %+v, want %+v without the gone pid", next, want)
	}
}

func TestPlanHeadingOnlyLabelsBundleAndPathRules(t *testing.T) {
	// An app named like a bundle rule's heading is not matched by it.
	named := App{PID: 50, Name: "Preview", BundleID: "com.example.preview"}
	steps, _ := Plan(cfg, []App{named}, State{50: t0}, t0.Add(time.Hour))
	if len(steps) != 0 {
		t.Fatalf("steps = %+v, want none", steps)
	}
}

func TestPlanFirstMatchingRuleWins(t *testing.T) {
	quitSlack := Rule{Name: "Slack bundle", Match: ByBundle, Value: slack.BundleID, After: time.Minute, Action: Quit}
	c := Config{Rules: []Rule{slackRule, quitSlack}}
	steps, _ := Plan(c, []App{slack}, State{10: t0}, t0.Add(5*time.Minute))
	if len(steps) != 0 {
		t.Fatalf("steps = %+v, want none: the 30m hide rule comes first", steps)
	}
}

func TestWithoutPID(t *testing.T) {
	got := withoutPID([]App{slack, kitty, preview}, kitty.PID)
	if want := []App{slack, preview}; !reflect.DeepEqual(got, want) {
		t.Fatalf("withoutPID = %+v, want %+v", got, want)
	}
}

func TestMarkActiveResetsTimer(t *testing.T) {
	// Frontmost for a moment between rounds: the next round sees it in
	// the background, but counts from the sample, not from t0.
	state := MarkActive(State{10: t0}, slack.PID, t0.Add(time.Hour))
	steps, next := Plan(cfg, []App{slack}, state, t0.Add(time.Hour+30*time.Second))
	if len(steps) != 0 {
		t.Fatalf("steps = %+v, want none after a short switch", steps)
	}
	if want := t0.Add(time.Hour); !next[10].Equal(want) {
		t.Fatalf("next[10] = %v, want %v", next[10], want)
	}
}

func TestMarkActiveUnknownPID(t *testing.T) {
	got := MarkActive(State{10: t0}, kitty.PID, t0.Add(time.Minute))
	want := State{10: t0, 40: t0.Add(time.Minute)}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("MarkActive = %+v, want %+v", got, want)
	}
}

func TestMarkActiveNilState(t *testing.T) {
	got := MarkActive(nil, slack.PID, t0)
	if want := (State{10: t0}); !reflect.DeepEqual(got, want) {
		t.Fatalf("MarkActive = %+v, want %+v", got, want)
	}
}

func TestMarkActiveNoFrontmost(t *testing.T) {
	if got := MarkActive(nil, -1, t0); got != nil {
		t.Fatalf("MarkActive(nil, -1) = %+v, want nil", got)
	}
	got := MarkActive(State{10: t0}, 0, t0.Add(time.Minute))
	if want := (State{10: t0}); !reflect.DeepEqual(got, want) {
		t.Fatalf("MarkActive(pid 0) = %+v, want %+v", got, want)
	}
}

func TestMarkActiveSelfPIDIsDropped(t *testing.T) {
	const self int32 = 77
	state := MarkActive(State{10: t0}, self, t0.Add(time.Minute))
	_, next := Plan(cfg, withoutPID([]App{slack, {PID: self, Name: "burin"}}, self), state, t0.Add(2*time.Minute))
	if _, ok := next[self]; ok {
		t.Fatalf("next = %+v, want the self pid dropped", next)
	}
}
