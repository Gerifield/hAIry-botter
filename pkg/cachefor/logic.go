package cachefor

import (
	"bytes"
	"crypto/sha1"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

const cacheFilePattern = "%s.cache"

var ErrCacheMiss = fmt.Errorf("cache miss")

// Logic .
type Logic struct{}

// New .
func New() *Logic {
	return &Logic{}
}

// Execute .
func (l *Logic) Execute(command []string, cacheTime time.Duration) error {

	stdOut, stdErr, exitCode, err := l.returnCached(command, cacheTime)
	if err == nil {
		// No, error, return everything
		_, _ = fmt.Fprintf(os.Stdout, stdOut)
		_, _ = fmt.Fprintf(os.Stderr, stdErr)
		os.Exit(exitCode)
	}
	// Ignore any other error from the cache miss for now, we can possibly log it maybe later

	stdOut, stdErr, exitCode, err = l.runAndCache(command, cacheTime)
	if err == nil {
		_, _ = fmt.Fprintf(os.Stdout, stdOut)
		_, _ = fmt.Fprintf(os.Stderr, stdErr)
		os.Exit(exitCode)
	}

	return err
}

// returnCached checks and loads in a cached value if it exists or not expired
// returns: stdout, stderr, exitCode, err
func (l *Logic) returnCached(command []string, cacheTime time.Duration) (string, string, int, error) {
	cacheFile := fmt.Sprintf(cacheFilePattern, genHash(strings.Join(command, " ")))
	cacheHit := false

	// Hit detection

	if stat, err := os.Stat(cacheFile); err == nil {
		if time.Since(stat.ModTime()) <= cacheTime {
			cacheHit = true
		}
	}

	if cacheHit {
		//
		// TODO: Read, parse and return values
		//
	}

	return "", "", 0, ErrCacheMiss
}

// runAndCache a command
func (l *Logic) runAndCache(command []string, cacheTime time.Duration) (string, string, int, error) {
	var cmd *exec.Cmd
	if len(command) > 1 {
		cmd = exec.Command(command[0], command[1:]...)
	} else {
		cmd = exec.Command(command[0])
	}

	// TODO: Stream the output to stdout/stderr while buffering it.
	// For now we just wait for it to finish and then save and print.
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	// Run the command
	err := cmd.Run()

	exitCode := 0
	if err != nil {

		if exitError, ok := errors.AsType[*exec.ExitError](err); ok {
			exitCode = exitError.ExitCode()
		} else {
			exitCode = 1
		}
	}

	//
	// TODO: Add actual caching here
	//

	return outBuf.String(), errBuf.String(), exitCode, err
}

// Cleanup .
func (l *Logic) Cleanup() {}

// genHash .
func genHash(command string) string {
	h := sha1.New()
	h.Write([]byte(command))
	return fmt.Sprintf("%x", h.Sum(nil))
}
