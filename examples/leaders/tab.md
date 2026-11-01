A leader: hold Tab for 200ms to show these entries, then press a key to run
one. The file is named after its trigger key; each "## <key> <title>" heading
is an entry and the lines under it are what it runs. Text before the first
heading is a comment. See docs/leaders.md.

:hold: 200ms
:layout: rows
## 1 Google Chrome
{open(Google Chrome)}
## 2 Firefox
{open(Firefox)}
## 3 Safari
{open(Safari)}
## q Slack
:row: break
{open(Slack)}
## w Terminal
{open(Terminal)}
## e Code
{open(Visual Studio Code)}
