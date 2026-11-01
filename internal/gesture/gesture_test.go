package gesture

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"burin/internal/workspace"
)

func TestClassify(t *testing.T) {
	tests := []struct {
		dx, dy float64
		dir    Direction
		ok     bool
	}{
		{-40, 0, Left, true},
		{40, 5, Right, true},
		{3, -50, Up, true},
		{-2, 31, Down, true},
		{30, 0, Right, true},
		{29, 0, "", false},  // too short
		{20, 20, "", false}, // too short
		{0, 0, "", false},
		{40, 40, "", false},   // diagonal
		{70, 31, "", false},   // main axis 69%
		{70, 30, Right, true}, // main axis 70%
		{-12, -60, Up, true},
	}
	for _, tt := range tests {
		dir, ok := Classify(tt.dx, tt.dy)
		if dir != tt.dir || ok != tt.ok {
			t.Errorf("Classify(%v, %v) = %q, %v; want %q, %v", tt.dx, tt.dy, dir, ok, tt.dir, tt.ok)
		}
	}
}

func TestKindOf(t *testing.T) {
	tests := map[uint32]Kind{
		EventRightMouseDown: Press, EventOtherMouseDown: Press,
		EventRightMouseDragged: Drag, EventOtherMouseDragged: Drag,
		EventRightMouseUp: Release, EventOtherMouseUp: Release,
	}
	for et, want := range tests {
		if got, ok := KindOf(et); !ok || got != want {
			t.Errorf("KindOf(%d) = %v, %v; want %v", et, got, ok, want)
		}
	}
	if _, ok := KindOf(10); ok {
		t.Error("keyDown has a kind")
	}
}

func TestRecognizerClick(t *testing.T) {
	var r Recognizer
	if res := r.Handle(Press, ButtonRight, 500, 300); !res.Consume || res.Ended {
		t.Fatalf("press = %+v", res)
	}
	if res := r.Handle(Drag, ButtonRight, 800, 300); !res.Consume {
		t.Fatalf("drag = %+v", res)
	}
	// Released where it was pressed, give or take, whatever the drags.
	res := r.Handle(Release, ButtonRight, 505, 303)
	want := Result{Consume: true, Ended: true}
	if res != want {
		t.Fatalf("release = %+v, want %+v", res, want)
	}
}

func TestRecognizerGesture(t *testing.T) {
	var r Recognizer
	r.Handle(Press, ButtonRight, 1307, 600)
	// The system may send no drags; those it sends do not count.
	r.Handle(Drag, ButtonRight, 1307, 900)
	res := r.Handle(Release, ButtonRight, 1104, 601.5)
	want := Result{Consume: true, Ended: true, Gesture: true, Direction: Left}
	if res != want {
		t.Fatalf("release = %+v, want %+v", res, want)
	}
	// The next press starts from its own location.
	r.Handle(Press, ButtonRight, 0, 0)
	if res := r.Handle(Release, ButtonRight, 10, 0); res.Gesture {
		t.Fatalf("second release = %+v", res)
	}
	r.Handle(Press, ButtonRight, 100, 100)
	res = r.Handle(Release, ButtonRight, 130, 100)
	want = Result{Consume: true, Ended: true, Gesture: true, Direction: Right}
	if res != want {
		t.Fatalf("30px release = %+v, want %+v", res, want)
	}
}

func TestRecognizerReleaseWithoutPress(t *testing.T) {
	var r Recognizer
	if res := r.Handle(Drag, ButtonRight, 50, 0); res != (Result{}) {
		t.Fatalf("drag = %+v", res)
	}
	if res := r.Handle(Release, ButtonRight, 0, 0); res != (Result{}) {
		t.Fatalf("release = %+v", res)
	}
	r.Handle(Press, ButtonRight, 0, 0)
	r.Reset()
	if res := r.Handle(Release, ButtonRight, 100, 0); res != (Result{}) {
		t.Fatalf("release after Reset = %+v", res)
	}
}

func TestRecognizerSideButtons(t *testing.T) {
	var r Recognizer
	r.Handle(Press, 3, 200, 200)
	// Another button's events leave the press alone.
	if res := r.Handle(Drag, 4, 300, 200); res.Consume {
		t.Fatalf("drag of button 4 = %+v", res)
	}
	if res := r.Handle(Release, 4, 300, 200); res.Consume {
		t.Fatalf("release of button 4 = %+v", res)
	}
	res := r.Handle(Release, 3, 200, 160)
	want := Result{Consume: true, Ended: true, Gesture: true, Direction: Up}
	if res != want {
		t.Fatalf("release = %+v, want %+v", res, want)
	}
}

func TestRecognizerLastPressWins(t *testing.T) {
	var r Recognizer
	r.Handle(Press, ButtonRight, 0, 0)
	r.Handle(Press, ButtonMiddle, 40, 0)
	if res := r.Handle(Release, ButtonRight, 40, 0); res.Consume {
		t.Fatalf("release of the first button = %+v", res)
	}
	if res := r.Handle(Release, ButtonMiddle, 40, 0); !res.Ended || res.Gesture {
		t.Fatalf("release of the second button = %+v", res)
	}
}

func TestParseButton(t *testing.T) {
	ok := map[string]Button{"right": 1, "middle": 2, "button1": 1, "button2": 2, "button3": 3, "button12": 12}
	for name, want := range ok {
		if got, err := ParseButton(name); err != nil || got != want {
			t.Errorf("ParseButton(%q) = %v, %v; want %v", name, got, err, want)
		}
	}
	for _, name := range []string{"left", "button0"} {
		_, err := ParseButton(name)
		if err == nil || !strings.Contains(err.Error(), "left button is not supported") {
			t.Errorf("ParseButton(%q) err = %v", name, err)
		}
	}
	for _, name := range []string{"button", "button-1", "button+3", "buttonx", "wheel", ""} {
		if _, err := ParseButton(name); err == nil {
			t.Errorf("ParseButton(%q): no error", name)
		}
	}
}

func TestParse(t *testing.T) {
	rules, err := Parse("comment\n## left Back\n:app: Google Chrome\n:app: Safari\n{key(cmd+[)}\n\n## UP Mission Control\n{shell(open -b com.apple.exposelauncher)|null}\n## down\n\n:app:com.apple.Safari\n\nx\n:app: y\n## right\n:app: Notes, Mail\n:app: Pages\n\n:app: Keynote\nz\n")
	if err != nil {
		t.Fatal(err)
	}
	want := []Rule{
		{Direction: Left, Title: "Back", Apps: []string{"Google Chrome", "Safari"}, Content: "{key(cmd+[)}"},
		{Direction: Up, Title: "Mission Control", Content: "{shell(open -b com.apple.exposelauncher)|null}"},
		{Direction: Down, Apps: []string{"com.apple.Safari"}, Content: "x\n:app: y"},
		{Direction: Right, Apps: []string{"Notes, Mail", "Pages"}, Content: ":app: Keynote\nz"},
	}
	if !reflect.DeepEqual(rules, want) {
		t.Fatalf("rules = %+v, want %+v", rules, want)
	}
}

func TestParseOldStyleApp(t *testing.T) {
	rules, err := Parse("## left\napp: Safari\na\n")
	if err != nil {
		t.Fatal(err)
	}
	want := []Rule{{Direction: Left, Content: "app: Safari\na"}}
	if !reflect.DeepEqual(rules, want) {
		t.Fatalf("rules = %+v, want %+v", rules, want)
	}
}

func TestParseErrors(t *testing.T) {
	tests := map[string]string{
		"unknown direction": "## north x\na\n",
		"empty app":         "## left\n:app:\na\n",
		"empty second app":  "## left\n:app: Safari\n:app:  \na\n",
		"empty heading":     "## \na\n",
	}
	for name, data := range tests {
		if _, err := Parse(data); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func writeFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestLoad(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"right.md":   "## left Back\n{key(cmd+[)}\n",
		"Button4.md": "## up\nx\n",
		"middle.md":  "## sideways\nx\n",
		"left.md":    "## up\nx\n",
		"button0.md": "## up\nx\n",
		"wheel.md":   "## up\nx\n",
		"button1.md": "## up\nx\n",
		"notes.txt":  "ignored",
	})
	if err := os.Mkdir(filepath.Join(dir, "button5.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	set := Load(dir)
	var buttons []Button
	for _, c := range set.Configs {
		buttons = append(buttons, c.Button)
	}
	// Button4.md, button1.md (taking button 1 before right.md), middle.md.
	if want := []Button{4, 1, 2}; !reflect.DeepEqual(buttons, want) {
		t.Fatalf("buttons = %v, want %v", buttons, want)
	}
	if set.Configs[0].Err != nil || set.Configs[1].Err != nil {
		t.Fatalf("errors = %v, %v", set.Configs[0].Err, set.Configs[1].Err)
	}
	if err := set.Configs[2].Err; err == nil || !strings.Contains(err.Error(), "middle.md") {
		t.Fatalf("middle.md err = %v", err)
	}
	// left.md, button0.md, wheel.md, right.md (duplicate of button1.md).
	if len(set.Errors) != 4 {
		t.Fatalf("Errors = %v, want 4", set.Errors)
	}
	joined := ""
	for _, err := range set.Errors {
		joined += err.Error() + "\n"
	}
	for _, want := range []string{"left button is not supported", "wheel", "already has button1.md"} {
		if !strings.Contains(joined, want) {
			t.Errorf("Errors lack %q:\n%s", want, joined)
		}
	}
}

func TestLoadMissingDir(t *testing.T) {
	set := Load(filepath.Join(t.TempDir(), "none"))
	if len(set.Configs) != 0 || len(set.Errors) != 0 {
		t.Fatalf("set = %+v", set)
	}
	if set.Mask() != 0 {
		t.Fatal("empty set has a mask")
	}
}

func TestMask(t *testing.T) {
	right := uint64(1<<EventRightMouseDown | 1<<EventRightMouseUp | 1<<EventRightMouseDragged)
	other := uint64(1<<EventOtherMouseDown | 1<<EventOtherMouseUp | 1<<EventOtherMouseDragged)
	tests := []struct {
		configs []Config
		want    uint64
	}{
		{nil, 0},
		{[]Config{{Button: ButtonRight}}, right},
		{[]Config{{Button: ButtonMiddle}}, other},
		{[]Config{{Button: 3}, {Button: 4}}, other},
		{[]Config{{Button: ButtonRight}, {Button: 3}}, right | other},
		// A file that failed has no rules to listen for.
		{[]Config{{Button: ButtonRight, Err: os.ErrInvalid}}, 0},
	}
	for _, tt := range tests {
		s := Set{Configs: tt.configs}
		if got := s.Mask(); got != tt.want {
			t.Errorf("Mask(%+v) = %#x, want %#x", tt.configs, got, tt.want)
		}
	}
	var none *Set
	if none.Mask() != 0 || none.Has(ButtonRight) {
		t.Error("nil set listens")
	}
}

func TestMatch(t *testing.T) {
	s := &Set{Configs: []Config{
		{Button: ButtonRight, Rules: []Rule{
			{Direction: Left, Content: "global left"},
			{Direction: Left, Apps: []string{"Safari", "com.google.Chrome"}, Content: "browser left"},
			{Direction: Left, Apps: []string{"Safari"}, Content: "second safari left"},
			{Direction: Left, Content: "second global left"},
			{Direction: Up, Apps: []string{"Finder"}, Content: "finder up"},
		}},
		{Button: 3, Err: os.ErrInvalid, Rules: []Rule{{Direction: Left, Content: "broken"}}},
	}}
	safari := workspace.App{Name: "Safari", BundleID: "com.apple.Safari"}
	chrome := workspace.App{Name: "Google Chrome", BundleID: "com.google.Chrome"}
	other := workspace.App{Name: "Notes", BundleID: "com.apple.Notes"}
	tests := []struct {
		b       Button
		d       Direction
		app     workspace.App
		content string
		ok      bool
	}{
		{ButtonRight, Left, safari, "browser left", true},
		{ButtonRight, Left, chrome, "browser left", true},
		{ButtonRight, Left, other, "global left", true},
		{ButtonRight, Up, other, "", false},
		{ButtonRight, Up, workspace.App{Name: "Finder"}, "finder up", true},
		{ButtonRight, Down, safari, "", false},
		{ButtonMiddle, Left, safari, "", false},
		{3, Left, safari, "", false},
	}
	for _, tt := range tests {
		r, ok := s.Match(tt.b, tt.d, tt.app)
		if ok != tt.ok || r.Content != tt.content {
			t.Errorf("Match(%v, %s, %s) = %q, %v; want %q, %v", tt.b, tt.d, tt.app.Name, r.Content, ok, tt.content, tt.ok)
		}
	}
	if !s.Has(ButtonRight) || s.Has(3) || s.Has(ButtonMiddle) {
		t.Error("Has is wrong")
	}
}

func TestAvailable(t *testing.T) {
	s := &Set{Configs: []Config{
		{Button: ButtonRight, Rules: []Rule{
			{Direction: Left, Title: "Back", Content: "global left"},
			{Direction: Up, Apps: []string{"Finder"}, Content: "finder up"},
			{Direction: Left, Apps: []string{"Safari"}, Content: "safari left"},
			{Direction: Down, Content: "global down"},
			{Direction: Down, Content: "second global down"},
		}},
		{Button: 3, Err: os.ErrInvalid, Rules: []Rule{{Direction: Left, Content: "broken"}}},
	}}
	contents := func(rules []Rule) []string {
		var out []string
		for _, r := range rules {
			out = append(out, r.Content)
		}
		return out
	}
	tests := []struct {
		b    Button
		app  workspace.App
		want []string
	}{
		{ButtonRight, workspace.App{Name: "Notes"}, []string{"global left", "global down"}},
		{ButtonRight, workspace.App{Name: "Safari"}, []string{"safari left", "global down"}},
		{ButtonRight, workspace.App{Name: "Finder"}, []string{"global left", "finder up", "global down"}},
		{ButtonMiddle, workspace.App{Name: "Notes"}, nil},
		{3, workspace.App{Name: "Notes"}, nil},
	}
	for _, tt := range tests {
		got := contents(s.Available(tt.b, tt.app))
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("Available(%v, %s) = %q, want %q", tt.b, tt.app.Name, got, tt.want)
		}
	}
}

func TestRuleName(t *testing.T) {
	if got := (Rule{Direction: Left, Title: "Back"}).Name(ButtonRight); got != "Back" {
		t.Errorf("Name = %q", got)
	}
	if got := (Rule{Direction: Up}).Name(4); got != "button4 up" {
		t.Errorf("Name = %q", got)
	}
}
