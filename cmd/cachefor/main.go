package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"hairy-botter/pkg/cachefor"
)

func main() {
	// Parse cacheTime from environment variable if present
	envCacheTime := os.Getenv("CACHE_TIME")
	defaultCacheTime := 5 * time.Minute
	if envCacheTime != "" {
		if parsed, err := time.ParseDuration(envCacheTime); err == nil {
			defaultCacheTime = parsed
		}
	}

	// Setup flag
	cacheTimePtr := flag.Duration("cacheTime", defaultCacheTime, "Duration to cache the command output (e.g., 5m, 1h)")

	// Custom usage output
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [options] <command> [args...]\n", os.Args[0])
		flag.PrintDefaults()
	}

	flag.Parse()

	args := flag.Args()
	if len(args) == 0 {
		flag.Usage()
		os.Exit(1)
	}

	cacheTime := *cacheTimePtr

	l := cachefor.New()

	// Make sure we run cleanup before exiting, but don't let os.Exit(exitCode) kill it
	// Since it is a goroutine, we make a little race condition on the current cache file too, but we won't block for long the execution
	cleanupDone := make(chan struct{})
	go func() {
		l.Cleanup()
		close(cleanupDone)
	}()

	// Ignore all errors
	stdOut, stdErr, exitCode, err := l.Execute(args, cacheTime)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	_, _ = fmt.Fprint(os.Stdout, stdOut)
	_, _ = fmt.Fprint(os.Stderr, stdErr)

	// Wait for cleanup
	<-cleanupDone
	os.Exit(exitCode)
}
