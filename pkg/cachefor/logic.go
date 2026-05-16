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

	"google.golang.org/protobuf/proto"
)

//go:generate protoc --go_out=paths=source_relative:. pkg/cachefor/cacheFile.proto

const cacheFilePattern = "%s.cache"

var ErrCacheMiss = fmt.Errorf("cache miss")

// Logic .
type Logic struct{}

// New .
func New() *Logic {
	return &Logic{}
}

// Execute .
func (l *Logic) Execute(command []string, cacheTime time.Duration) (string, string, int, error) {
	stdOut, stdErr, exitCode, err := l.returnCached(command, cacheTime)
	if err == nil {
		return stdOut, stdErr, exitCode, err
	}
	// Ignore any other error from the cache miss for now, we can possibly log it maybe later

	stdOut, stdErr, exitCode, err = l.runAndCache(command, cacheTime)
	if err == nil {
		return stdOut, stdErr, exitCode, err
	}

	return "", "", 0, err
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
		b, err := os.ReadFile(cacheFile)
		if err != nil {
			return "", "", 0, err
		}

		c := &CacheFile{}
		err = proto.Unmarshal(b, c)
		if err != nil {
			return "", "", 0, err
		}

		return c.StdOut, c.StdErr, int(c.ErrCode), nil
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

	// Convert and save cache file
	c := &CacheFile{
		Duration: cacheTime.Nanoseconds(),
		StdOut:   outBuf.String(),
		StdErr:   errBuf.String(),
		ErrCode:  int64(exitCode),
	}

	// Failed to save, it might be fine to skip this if failed
	b, err := proto.Marshal(c)
	if err != nil {
		return "", "", 0, err
	}

	cacheFile := fmt.Sprintf(cacheFilePattern, genHash(strings.Join(command, " ")))
	err = os.WriteFile(cacheFile, b, 0644)
	if err != nil {
		return "", "", 0, err
	}

	return outBuf.String(), errBuf.String(), exitCode, err
}

// Cleanup checks all .cache files and try to get the duration from them and cleanup
func (l *Logic) Cleanup() {
	// Read current directory
	files, err := os.ReadDir(".")
	if err != nil {
		return
	}

	for _, file := range files {
		if file.IsDir() {
			continue
		}

		name := file.Name()
		var dur time.Duration
		var durErr error
		if strings.HasSuffix(name, ".cache") {
			if info, err := file.Info(); err == nil {
				// Check duration, ignore the ones we can't read to be safe
				// We could log the error later
				dur, durErr = checkSavedDuration(name)
				if durErr == nil && time.Since(info.ModTime()) > dur {
					// We can just ignore errors here; best effort cleanup
					_ = os.Remove(name)
				}
			}
		}
	}
}

// checkSavedDuration from file
func checkSavedDuration(fileName string) (time.Duration, error) {
	b, err := os.ReadFile(fileName)
	if err != nil {
		return 0, err
	}

	c := &CacheFile{}
	err = proto.Unmarshal(b, c)
	if err != nil {
		return 0, err
	}

	return time.Duration(c.Duration), nil
}

// genHash .
func genHash(command string) string {
	h := sha1.New()
	h.Write([]byte(command))
	return fmt.Sprintf("%x", h.Sum(nil))
}
