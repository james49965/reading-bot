// Command reading-bot is a Telegram bot that writes reading notes into the
// markdown repo behind a Quartz site.
package main

import (
	"log"
	"os"

	"github.com/james49965/reading-bot/internal/config"
)

func main() {
	log.SetFlags(log.LstdFlags | log.LUTC)

	cfg, err := config.Load()
	if err != nil {
		// Exit non-zero so systemd records a failed start rather than a
		// process that looks alive but can do nothing.
		log.Printf("%v", err)
		os.Exit(1)
	}

	log.Printf("config loaded: %s", cfg.Redacted())

	if cfg.DryRun {
		log.Print("DRY_RUN is on: no GitHub writes will be made")
	}

	// Next: preflight the GitHub token against ContentDir, then start the
	// getUpdates loop. Both belong here, behind the config that just loaded.
	log.Print("nothing else implemented yet — exiting")
}
