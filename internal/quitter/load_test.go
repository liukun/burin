package quitter

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// exampleFile is the example of the plan, with comments around it.
const exampleFile = `Slack 不在前台半小时就隐藏
## Slack
:after: 30m
:action: hide

## Preview
:bundle: com.apple.Preview
:after: 10m
:action: quit

## Dev Chrome
说明：全角冒号是注释
:path: ~/Apps/Google Chrome Canary.app
:after: 2h
`

func TestParseExample(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	set := Parse(exampleFile)
	if len(set.Errors) != 0 {
		t.Fatalf("Errors = %+v, want none", set.Errors)
	}
	want := []Rule{
		{Name: "Slack", Match: ByName, Value: "Slack", After: 30 * time.Minute, Action: Hide},
		{Name: "Preview", Match: ByBundle, Value: "com.apple.Preview", After: 10 * time.Minute, Action: Quit},
		{Name: "Dev Chrome", Match: ByPath, Value: home + "/Apps/Google Chrome Canary.app", After: 2 * time.Hour, Action: Hide},
	}
	if got := set.Config.Rules; !reflect.DeepEqual(got, want) {
		t.Fatalf("Rules = %+v, want %+v", got, want)
	}
}

func TestParseCloseWindow(t *testing.T) {
	set := Parse("## Hammerspoon\n:bundle: org.hammerspoon.Hammerspoon\n:after: 10m\n:action: close_window\n")
	if len(set.Errors) != 0 {
		t.Fatalf("Errors = %+v, want none", set.Errors)
	}
	want := []Rule{{Name: "Hammerspoon", Match: ByBundle, Value: "org.hammerspoon.Hammerspoon", After: 10 * time.Minute, Action: CloseWindow}}
	if got := set.Config.Rules; !reflect.DeepEqual(got, want) {
		t.Fatalf("Rules = %+v, want %+v", got, want)
	}
	if got := want[0].String(); got != "close_window after 10m0s" {
		t.Fatalf("String = %q, want close_window after 10m0s", got)
	}
}

func TestParseSectionErrors(t *testing.T) {
	tests := []struct {
		name    string
		section string
		wantErr string
	}{
		{"缺 after", ":action: quit\n", ":after: is required"},
		{"旧写法 after: 不再是字段", "after: 30m\n", ":after: is required"},
		{"after 不是时长", ":after: 30\n", `"30" is not a duration`},
		{"after 为零", ":after: 0s\n", `"0s" is not positive`},
		{"action 拼错", ":after: 1m\n:action: close\n", `"close" is not hide, quit or close_window`},
		{"未知 key", ":after: 1m\n:bundel: com.foo\n", `unknown key "bundel"`},
		{"重复 key", ":after: 1m\n:after: 2m\n", "duplicate :after:"},
		{"bundle 和 path 同时", ":after: 1m\n:bundle: com.foo\n:path: /Applications/Foo.app\n", ":bundle: and :path: exclude each other"},
		{"bundle 空", ":after: 1m\n:bundle:\n", ":bundle: needs a bundle identifier"},
		{"path 空", ":after: 1m\n:path:\n", ":path: needs an app path"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// The bad section sits between two good ones, which still load.
			data := "## Good\n:after: 1m\n\n## Bad\n" + tt.section + "\n## Also good\n:after: 1m\n"
			set := Parse(data)
			if len(set.Errors) != 1 {
				t.Fatalf("Errors = %+v, want 1", set.Errors)
			}
			if got := set.Errors[0].Error(); !strings.HasPrefix(got, "## Bad: ") || !strings.Contains(got, tt.wantErr) {
				t.Fatalf("error = %q, want ## Bad: ... %q", got, tt.wantErr)
			}
			names := []string{}
			for _, r := range set.Config.Rules {
				names = append(names, r.Name)
			}
			want := []string{"Good", "Also good"}
			if !reflect.DeepEqual(names, want) {
				t.Fatalf("names = %+v, want %+v", names, want)
			}
		})
	}
}

func TestParsePathWithoutTilde(t *testing.T) {
	set := Parse("## Foo\n:path: /Applications/Foo.app\n:after: 1m\n")
	if len(set.Errors) != 0 {
		t.Fatalf("Errors = %+v, want none", set.Errors)
	}
	if got := set.Config.Rules[0].Value; got != "/Applications/Foo.app" {
		t.Fatalf("Value = %q, want the path unchanged", got)
	}
}

func TestParsePathTrailingSlash(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct{ path, want string }{
		{"/Applications/Foo.app/", "/Applications/Foo.app"},
		{"~/Apps/Foo.app/", home + "/Apps/Foo.app"},
	}
	for _, tt := range tests {
		set := Parse("## Foo\n:path: " + tt.path + "\n:after: 1m\n")
		if len(set.Errors) != 0 {
			t.Fatalf("path %q: Errors = %+v, want none", tt.path, set.Errors)
		}
		if got := set.Config.Rules[0].Value; got != tt.want {
			t.Fatalf("path %q: Value = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	missing := Load(filepath.Join(dir, FileName))
	if missing.Config.Len() != 0 || len(missing.Errors) != 0 {
		t.Fatalf("missing file = %+v, want empty", missing)
	}
	path := filepath.Join(dir, FileName)
	if err := os.WriteFile(path, []byte(exampleFile), 0o644); err != nil {
		t.Fatal(err)
	}
	set := Load(path)
	if set.Config.Len() != 3 || len(set.Errors) != 0 {
		t.Fatalf("Load = %d rules, %+v errors, want 3, none", set.Config.Len(), set.Errors)
	}
	unreadable := Load(dir)
	if len(unreadable.Errors) != 1 {
		t.Fatalf("Load(dir) errors = %+v, want 1", unreadable.Errors)
	}
}

func TestParseProseIsComment(t *testing.T) {
	set := Parse("## Slack\n注意: 不要这样写\nTODO: 以后再调\n:after: 30m\n")
	if len(set.Errors) != 0 {
		t.Fatalf("Errors = %+v, want none", set.Errors)
	}
	want := []Rule{{Name: "Slack", Match: ByName, Value: "Slack", After: 30 * time.Minute}}
	if !reflect.DeepEqual(set.Config.Rules, want) {
		t.Fatalf("Rules = %+v, want %+v", set.Config.Rules, want)
	}
}
