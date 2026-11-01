Quitter rules: one "## " section per app, with :key: value lines. This one
hides Preview once it has not been the frontmost app for two hours. Text
before the first heading, and any line that is not a :key: value line, is a
comment. See docs/quitter.md.

## Preview
:bundle: com.apple.Preview
:after: 2h
:action: hide
