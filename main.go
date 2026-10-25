package main

import (
	"log"
	"time"

	"github.com/egoist/mygo"

	"burin/internal/eventtap"
	"burin/internal/expander"
)

const needAccessibility = "Grant Accessibility and Input Monitoring, then relaunch"

func snippets() []expander.Snippet {
	return []expander.Snippet{
		{Keyword: "xdate", Content: func() string { return time.Now().Format("2006-01-02") }},
		{Keyword: "xname", Content: func() string { return "liukun" }},
	}
}

func main() {
	mygo.App.WhenReady(func() {
		mygo.App.SetActivationPolicy(mygo.ActivationPolicyAccessory)

		status := "Snippet expansion on"
		if !eventtap.AccessibilityTrusted(true) {
			status = needAccessibility
		}
		ex := expander.New(snippets(), expander.DefaultDelay, expander.Paste)
		tap, err := expander.Listen(ex)
		if err != nil {
			log.Print(err)
			status = needAccessibility
		}
		mygo.App.OnQuit(func() {
			ex.Stop()
			if tap != nil {
				tap.Stop()
			}
		})

		_, err = mygo.NewTray(mygo.TrayOptions{
			ID:      "burin",
			Title:   "Burin",
			ToolTip: "Burin",
			Menu: mygo.NewMenu([]*mygo.MenuItem{
				{Label: status, Disabled: true},
				mygo.Separator(),
				{Role: mygo.RoleQuit},
			}),
		})
		if err != nil {
			log.Fatal(err)
		}
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
