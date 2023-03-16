package main

import (
	"embed"
	"github.com/getsentry/sentry-go"
	"github.com/sensepost/gowitness/cmd"
	"log"
	"os"
)

//go:embed web/assets/* web/ui-templates/* web/static-templates/*
var assets embed.FS

func main() {

	err := sentry.Init(sentry.ClientOptions{
		Dsn: os.Getenv("SENTRY_DSN"),
		// Set TracesSampleRate to 1.0 to capture 100%
		// of transactions for performance monitoring.
		// We recommend adjusting this value in production,
		TracesSampleRate: 0,
	})
	if err != nil {
		log.Fatalf("sentry.Init: %s", err)
	}
	cmd.Embedded = assets
	cmd.Execute()
}
