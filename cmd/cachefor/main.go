package main

import (
	"bytes"
	"crypto/sha1"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

func cleanupOldCaches(cacheTime time.Duration) {
	threshold := 2 * cacheTime

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
		if strings.HasSuffix(name, ".cache") {
			if info, err := file.Info(); err == nil {
				if time.Since(info.ModTime()) > threshold {
					// We can just ignore errors here; best effort cleanup
					_ = os.Remove(name)
				}
			}
		}
	}
}

func main() {
	os.Exit(run())
}

func run() int {
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
		return 1
	}

	cacheTime := *cacheTimePtr

	// Make sure we run cleanup before exiting
	defer cleanupOldCaches(cacheTime)

	// Cache key generation
	cmdStr := strings.Join(args, " ")
	h := sha1.New()
	h.Write([]byte(cmdStr))
	hashStr := fmt.Sprintf("%x", h.Sum(nil))

	cacheFile := fmt.Sprintf("%s.cache", hashStr)
	errFile := fmt.Sprintf("%s.err.cache", hashStr)
	exitFile := fmt.Sprintf("%s.exit.cache", hashStr)

	cacheHit := false

	// Hit detection
	if stat, err := os.Stat(cacheFile); err == nil {
		if time.Since(stat.ModTime()) <= cacheTime {
			cacheHit = true
		}
	}

	if cacheHit {
		// Handle cache hit

		// Write stdout from cache
		if f, err := os.Open(cacheFile); err == nil {
			io.Copy(os.Stdout, f)
			f.Close()
		}

		// Write stderr from cache if it exists
		if f, err := os.Open(errFile); err == nil {
			io.Copy(os.Stderr, f)
			f.Close()
		}

		// Get exit code from cache if it exists
		exitCode := 0
		if exitData, err := os.ReadFile(exitFile); err == nil {
			if parsedCode, err := strconv.Atoi(strings.TrimSpace(string(exitData))); err == nil {
				exitCode = parsedCode
			}
		}

		return exitCode
	} else {
		// Handle cache miss / execute command

		// Setup the command
		var cmd *exec.Cmd
		if len(args) > 1 {
			cmd = exec.Command(args[0], args[1:]...)
		} else {
			cmd = exec.Command(args[0])
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
			if exitError, ok := err.(*exec.ExitError); ok {
				exitCode = exitError.ExitCode()
			} else {
				exitCode = 1
			}
		}

		// Write cache files
		os.WriteFile(cacheFile, outBuf.Bytes(), 0644)

		if errBuf.Len() > 0 {
			os.WriteFile(errFile, errBuf.Bytes(), 0644)
		} else {
			os.Remove(errFile) // Clean up old err cache if exists
		}

		if exitCode != 0 {
			os.WriteFile(exitFile, []byte(strconv.Itoa(exitCode)), 0644)
		} else {
			os.Remove(exitFile) // Clean up old exit cache if exists
		}

		// Print outputs
		os.Stdout.Write(outBuf.Bytes())
		os.Stderr.Write(errBuf.Bytes())

		return exitCode
	}
}
