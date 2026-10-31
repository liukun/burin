package workspace

import "testing"

var (
	slack  = App{PID: 10, Name: "Slack", BundleID: "com.tinyspeck.slackmacgap"}
	kitty  = App{PID: 20, Name: "kitty", BundleID: "net.kovidgoyal.kitty"}
	chrome = App{PID: 30, Name: "Google Chrome", BundleID: "com.google.Chrome"}
)

func active(a App) App { a.Active = true; return a }

func hidden(a App) App { a.Hidden = true; return a }

func TestDecide(t *testing.T) {
	secondChrome := chrome
	secondChrome.PID = 31
	tests := []struct {
		name    string
		apps    []App
		target  string
		want    Action
		wantPID int32
	}{
		{"not running launches", []App{kitty}, "Slack", LaunchApp, 0},
		{"running in background activates", []App{slack, active(kitty)}, "Slack", ActivateApp, 10},
		{"frontmost hides", []App{active(slack), kitty}, "Slack", HideApp, 10},
		{"of several instances the frontmost wins", []App{chrome, active(secondChrome)}, "Google Chrome", HideApp, 31},
		{"of several background instances the first wins", []App{chrome, secondChrome}, "Google Chrome", ActivateApp, 30},
		{"matches the bundle ID", []App{slack}, "com.tinyspeck.slackmacgap", ActivateApp, 10},
		{"hidden activates", []App{hidden(slack), active(kitty)}, "Slack", ActivateApp, 10},
		{"name match is exact", []App{slack}, "slack", LaunchApp, 0},
		{"no running apps launches", nil, "Slack", LaunchApp, 0},
		{"empty name matches no app without a bundle ID", []App{{PID: 40, Name: "helper"}}, "", LaunchApp, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, app := Decide(tt.apps, tt.target)
			if got != tt.want || app.PID != tt.wantPID {
				t.Errorf("Decide(%q) = %v, pid %d; want %v, pid %d", tt.target, got, app.PID, tt.want, tt.wantPID)
			}
		})
	}
}
