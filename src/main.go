package main

import (
	"context"
	"errors"
	"log"
	"os"

	"github.com/apimgr/api/src/app"
	"github.com/apimgr/api/src/config"
)

func main() {
	cfg, err := config.Load(os.Args[1:])
	if err != nil {
		log.Printf("Failed to load configuration: %v", err)
		var argumentError *config.RuntimeArgumentError
		if errors.As(err, &argumentError) {
			os.Exit(exUsage)
		}
		os.Exit(exConfig)
	}
	cfg.Build = config.BuildInfo{
		Version:      Version,
		CommitID:     CommitID,
		BuildEpoch:   BuildEpoch,
		OfficialSite: OfficialSite,
		BuildDate:    BuildDate,
	}
	if err := app.Run(context.Background(), cfg); err != nil {
		log.Printf("Server stopped with an error: %v", err)
		os.Exit(app.ExitCode(err))
	}
}
