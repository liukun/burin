Snippets: type a "## " heading's keyword and it is replaced by the text under
it, with {placeholders} expanded. =b64 pipes the clipboard through
scripts/b64.py. Text before the first heading is a comment. See
docs/snippets.md.

## ;date
{datetime("%Y-%m-%d")}

## ;time
{datetime("%H:%M")}

## =b64
{clipboard|b64}
