//go:build darwin

package keys

import "testing"

func TestExpansionKeysPostWithExpansionMark(t *testing.T) {
	tests := []struct {
		name    string
		posts   [2]keyPost
		keycode uint16
		flags   uint64
	}{
		{"backspace", backspacePosts(), keycodeDelete, 0},
		{"cmd+v", cmdVPosts(), keycodeV, eventFlagMaskCommand},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for i, p := range tt.posts {
				if p.mark != ExpansionMark {
					t.Errorf("post %d mark %#x, want %#x", i, p.mark, ExpansionMark)
				}
				if p.keycode != tt.keycode || p.flags != tt.flags {
					t.Errorf("post %d keycode %d flags %#x, want %d, %#x", i, p.keycode, p.flags, tt.keycode, tt.flags)
				}
				if wantDown := i == 0; p.keyDown != wantDown {
					t.Errorf("post %d keyDown = %v, want %v", i, p.keyDown, wantDown)
				}
			}
		})
	}
}

func TestStrokePostsSyntheticMark(t *testing.T) {
	for i, p := range strokePosts(1, 2) {
		if p.mark != SyntheticMark || p.keycode != 1 || p.flags != 2 {
			t.Errorf("post %d = %+v", i, p)
		}
	}
}
