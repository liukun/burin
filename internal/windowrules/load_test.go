package windowrules

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// exampleFile is the configuration of the plan, config.lua in md form.
const exampleFile = `Slack 重启后记不住位置，钉在 BenQ 左上角
## Slack
screen_w: 1920
screen_h: 1280
anchor: topLeft
mainTitle: \[Main\]
width_min: 0.7
width_max: 0.9
height_min: 0.5
height_max: 0.8

设置窗口 General 没全屏按钮，用 fullScreenable 排除
## Yab
screen_w: 1920
screen_h: 1280
anchor: bottomLeft
fullScreenable: true
width_min: 0.7
width_max: 0.9
height_min: 0.5
height_max: 0.8

## Trello
screen_w: 1080
screen_h: 1920
anchor: bottomLeft
width_min: 0.95
width_max: 1
height_min: 0.3
height_max: 0.4

BenQ 上其他窗口左边留 66px 露出 Slack 侧栏
## left margin
screen_w: 1920
screen_h: 1280
margin: 66
except: Finder
except: Foo
`

// comparableRule makes rules comparable: compiled regexps hold internal
// state reflect.DeepEqual should not look at.
type comparableRule struct {
	Rule
	Title, MainTitle string
}

func comparableRules(rules []Rule) []comparableRule {
	out := []comparableRule{}
	for _, r := range rules {
		c := comparableRule{Rule: r}
		if r.Title != nil {
			c.Title = r.Title.String()
		}
		if r.MainTitle != nil {
			c.MainTitle = r.MainTitle.String()
		}
		c.Rule.Title, c.Rule.MainTitle = nil, nil
		out = append(out, c)
	}
	return out
}

func TestParseExample(t *testing.T) {
	set := Parse(exampleFile)
	if len(set.Errors) != 0 {
		t.Fatalf("Errors = %+v, want none", set.Errors)
	}
	benqSize, portraitSize := Size{1920, 1280}, Size{1080, 1920}
	wantPins := []comparableRule{
		{Rule: Rule{App: "Slack", Screen: &benqSize, Anchor: TopLeft, Width: Range{0.7, 0.9}, Height: Range{0.5, 0.8}},
			MainTitle: `\[Main\]`},
		{Rule: Rule{App: "Yab", Screen: &benqSize, Anchor: BottomLeft, FullScreenable: true,
			Width: Range{0.7, 0.9}, Height: Range{0.5, 0.8}}},
		{Rule: Rule{App: "Trello", Screen: &portraitSize, Anchor: BottomLeft, Width: Range{0.95, 1}, Height: Range{0.3, 0.4}}},
	}
	if got := comparableRules(set.Config.Pins); !reflect.DeepEqual(got, wantPins) {
		t.Fatalf("Pins = %+v, want %+v", got, wantPins)
	}
	wantMargins := []MarginRule{{Screen: benqSize, Margin: 66, Except: []string{"Finder", "Foo"}}}
	if got := set.Config.Margins; !reflect.DeepEqual(got, wantMargins) {
		t.Fatalf("Margins = %+v, want %+v", got, wantMargins)
	}
	if got := set.Config.Len(); got != 4 {
		t.Fatalf("Len = %+v, want %+v", got, 4)
	}
	if !set.Config.Pins[0].MainTitle.MatchString("general - Slack [Main] 🏠") {
		t.Fatal("mainTitle does not match a [Main] title")
	}
}

const validPin = `anchor: topLeft
width_min: 0.5
width_max: 0.9
height_min: 0.5
height_max: 0.9
`

func TestParseSectionErrors(t *testing.T) {
	tests := []struct {
		name    string
		section string
		wantErr string
	}{
		{"缺 anchor", strings.Replace(validPin, "anchor: topLeft\n", "", 1), "anchor: is required"},
		{"anchor 拼错", strings.Replace(validPin, "topLeft", "topRight", 1), `"topRight" is not topLeft or bottomLeft`},
		{"缺 width_max", strings.Replace(validPin, "width_max: 0.9\n", "", 1), "width_max: is required"},
		{"min 大于 max", strings.Replace(validPin, "width_min: 0.5", "width_min: 0.95", 1), "need 0 <= min <= max <= 1"},
		{"max 大于 1", strings.Replace(validPin, "height_max: 0.9", "height_max: 1.5", 1), "need 0 <= min <= max <= 1"},
		{"不是数字", strings.Replace(validPin, "height_min: 0.5", "height_min: half", 1), `"half" is not a number`},
		{"screen 不成对", validPin + "screen_w: 1920\n", "go together"},
		{"未知 key", validPin + "widht_min: 0.5\n", `unknown key "widht_min"`},
		{"重复 key", validPin + "anchor: bottomLeft\n", "duplicate anchor:"},
		{"坏正则", validPin + "title: [\n", "title:"},
		{"fullScreenable 非布尔", validPin + "fullScreenable: maybe\n", `"maybe" is not true or false`},
		{"钉位规则不收 except", validPin + "except: Finder\n", "except: is not a key of a pin rule"},
		{"边距缺 screen", "margin: 66\n", "screen_w: and screen_h: are required"},
		{"边距负数", "screen_w: 1920\nscreen_h: 1280\nmargin: -1\n", "is negative"},
		{"边距不收 anchor", "screen_w: 1920\nscreen_h: 1280\nmargin: 66\nanchor: topLeft\n", "anchor: is not a key of a margin rule"},
		{"except 空", "screen_w: 1920\nscreen_h: 1280\nmargin: 66\nexcept:\n", "except: needs an app name"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// The bad section sits between two good ones, which still load.
			data := "## Good\n" + validPin + "\n## Bad\n" + tt.section + "\n## Also good\n" + validPin
			set := Parse(data)
			if len(set.Errors) != 1 {
				t.Fatalf("Errors = %+v, want 1", set.Errors)
			}
			if got := set.Errors[0].Error(); !strings.HasPrefix(got, "## Bad: ") || !strings.Contains(got, tt.wantErr) {
				t.Fatalf("error = %q, want ## Bad: ... %q", got, tt.wantErr)
			}
			apps := []string{}
			for _, r := range set.Config.Pins {
				apps = append(apps, r.App)
			}
			want := []string{"Good", "Also good"}
			if !reflect.DeepEqual(apps, want) {
				t.Fatalf("apps = %+v, want %+v", apps, want)
			}
		})
	}
}

func TestParseCommentsAndOptionalScreen(t *testing.T) {
	data := "前言\n## kitty\n随便写点注释\n" + validPin + "说明：全角冒号是注释\n"
	set := Parse(data)
	if len(set.Errors) != 0 {
		t.Fatalf("Errors = %+v, want none", set.Errors)
	}
	if len(set.Config.Pins) != 1 || set.Config.Pins[0].Screen != nil {
		t.Fatalf("Pins = %+v, want one without screen", set.Config.Pins)
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
	if set.Config.Len() != 4 || len(set.Errors) != 0 {
		t.Fatalf("Load = %d rules, %+v errors, want 4, none", set.Config.Len(), set.Errors)
	}
	unreadable := Load(dir)
	if len(unreadable.Errors) != 1 {
		t.Fatalf("Load(dir) errors = %+v, want 1", unreadable.Errors)
	}
}
