Input source mark: the current input source shows as a large, faint
character in the middle of the current window's display when the
frontmost app or the input source changes, and as a small one that
follows the pointer after a left click; a click that switches apps shows
only the small one. See docs/input_sources.md. A
line ":enabled: no" at the top turns it off.
Each "## " section is an input source ID: ":text:" is what shows,
":color:" its color as #rrggbb or #rrggbbaa (white by default), and
":show: no" never shows it. An input source without a section shows
the first letter of its name, and its ID is logged once to
~/Library/Logs/burin.log so you can add one.

:enabled: yes

## com.apple.keylayout.ABC
:text: A
:color: #ffffff

## com.tencent.inputmethod.wetype.pinyin
:text: 微
:color: #1ab31a
