// main.go
package main

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

type LogEntry struct {
	Filename  string `json:"filename"`
	Status    string `json:"status"`
	MD5       string `json:"md5,omitempty"`
	Error     string `json:"error,omitempty"`
	Timestamp string `json:"timestamp"`
}

func main() {
	inputDir := flag.String("input", "./input", "input directory containing r_* folders")
	concurrency := flag.Int("concurrency", runtime.NumCPU(), "max concurrent file processors per folder")
	verbose := flag.Bool("v", false, "verbose logging")
	flag.Parse()

	if *concurrency <= 0 {
		*concurrency = 1
	}

	if *verbose {
		fmt.Printf("Scanning input dir: %s (concurrency per folder: %d)\n", *inputDir, *concurrency)
	}

	err := processAll(*inputDir, *concurrency, *verbose)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ERROR:", err)
		os.Exit(1)
	}
}

func processAll(inputDir string, concurrency int, verbose bool) error {
	// Ensure input directory exists
	info, err := os.Stat(inputDir)
	if err != nil {
		return fmt.Errorf("input dir check: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("input path is not a directory: %s", inputDir)
	}

	// Find directories starting with r_
	entries, err := os.ReadDir(inputDir)
	if err != nil {
		return fmt.Errorf("read input dir: %w", err)
	}

	var targets []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, "r_") {
			targets = append(targets, filepath.Join(inputDir, name))
		}
	}

	if verbose {
		fmt.Printf("Found %d 'r_' folders to process\n", len(targets))
	}

	for _, folder := range targets {
		if verbose {
			fmt.Printf("Processing folder: %s\n", folder)
		}
		if err := processFolder(folder, concurrency, verbose); err != nil {
			// Log error but continue with other folders
			fmt.Fprintf(os.Stderr, "folder %s: processing error: %v\n", folder, err)
		}
	}

	return nil
}

// processFolder processes files inside a single folder concurrently, writes log, then renames folder.
func processFolder(folder string, concurrency int, verbose bool) error {
	dirEntries, err := os.ReadDir(folder)
	if err != nil {
		return fmt.Errorf("read folder: %w", err)
	}

	// Collect file entries (skip nested directories)
	var files []os.DirEntry
	for _, de := range dirEntries {
		if de.IsDir() {
			continue
		}
		files = append(files, de)
	}

	if verbose {
		fmt.Printf("  found %d files in %s\n", len(files), folder)
	}

	// Result channel
	resultsCh := make(chan LogEntry, len(files))

	// Semaphore to limit concurrency
	sem := make(chan struct{}, concurrency)

	var wg sync.WaitGroup
	for _, f := range files {
		wg.Add(1)
		sem <- struct{}{} // acquire
		// capture variables for goroutine
		filename := f.Name()
		go func(fname string) {
			defer wg.Done()
			defer func() { <-sem }() // release

			entry := processFile(filepath.Join(folder, fname), fname)
			resultsCh <- entry
			if verbose {
				fmt.Printf("    processed %s -> %s\n", fname, entry.Status)
			}
		}(filename)
	}

	// Wait for all and close resultsCh
	go func() {
		wg.Wait()
		close(resultsCh)
	}()

	// Collect results
	var results []LogEntry
	for r := range resultsCh {
		results = append(results, r)
	}

	// Sort results by filename for deterministic output
	sort.Slice(results, func(i, j int) bool { return results[i].Filename < results[j].Filename })

	// Determine overall success
	allSuccess := true
	for _, r := range results {
		if r.Status != "success" {
			allSuccess = false
			break
		}
	}

	// Write atomic log.tmp -> log.json
	if err := writeLogAtomic(folder, results); err != nil {
		// even if logging fails, try to rename to failure to avoid reprocessing in loop
		fmt.Fprintf(os.Stderr, "failed to write log for %s: %v\n", folder, err)
		// rename folder to failure
		if renameErr := renameFolderSafe(folder, false); renameErr != nil {
			return fmt.Errorf("logging error: %v; folder rename error: %v", err, renameErr)
		}
		return fmt.Errorf("failed to write log: %w", err)
	}

	// Finally rename directory based on success / failure
	if err := renameFolderSafe(folder, allSuccess); err != nil {
		return fmt.Errorf("rename folder: %w", err)
	}

	return nil
}

// processFile reads a file, calculates md5, timestamps results, handles errors gracefully.
func processFile(fullpath, basename string) LogEntry {
	now := time.Now().UTC().Format(time.RFC3339)
	f, err := os.Open(fullpath)
	if err != nil {
		return LogEntry{
			Filename:  basename,
			Status:    "error",
			Error:     fmt.Sprintf("failed to open file: %v", err),
			Timestamp: now,
		}
	}
	defer f.Close()

	hash := md5.New()
	// Copy file into hash efficiently (streams the file, avoids reading whole file into memory)
	if _, err := io.Copy(hash, f); err != nil {
		return LogEntry{
			Filename:  basename,
			Status:    "error",
			Error:     fmt.Sprintf("failed to read file: %v", err),
			Timestamp: now,
		}
	}

	sum := hash.Sum(nil)
	return LogEntry{
		Filename:  basename,
		Status:    "success",
		MD5:       hex.EncodeToString(sum),
		Timestamp: now,
	}
}

// writeLogAtomic writes results to a temporary file in the folder and renames it to log.json.
func writeLogAtomic(folder string, results []LogEntry) error {
	data, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal results: %w", err)
	}

	tmpPath := filepath.Join(folder, "log.tmp")
	finalPath := filepath.Join(folder, "log.json")

	// Use CreateTemp in same folder to ensure rename stays within same filesystem
	tmpFile, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("create tmp log: %w", err)
	}

	// Write and ensure data flushed to disk
	if _, err := tmpFile.Write(data); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write tmp log: %w", err)
	}
	if err := tmpFile.Sync(); err != nil {
		// best-effort sync; still attempt rename
		fmt.Fprintf(os.Stderr, "warning: fsync failed for %s: %v\n", tmpPath, err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close tmp log: %w", err)
	}

	// Rename tmp to final (atomic on POSIX when on same filesystem)
	if err := os.Rename(tmpPath, finalPath); err != nil {
		// cleanup tmp if rename fails
		os.Remove(tmpPath)
		return fmt.Errorf("rename tmp->final: %w", err)
	}

	return nil
}

// renameFolderSafe renames folder r_<id> -> d_<id> (allSuccess=true) or f_<id> (false).
// If the destination already exists, it adds a timestamp suffix to avoid conflicts.
func renameFolderSafe(src string, allSuccess bool) error {
	base := filepath.Base(src)
	parent := filepath.Dir(src)

	// Expect src base like r_001
	if !strings.HasPrefix(base, "r_") {
		// if it's already renamed or unexpected, still attempt rename to proper status prefix
		// but fabricate id as everything after first underscore if present
		parts := strings.SplitN(base, "_", 2)
		if len(parts) >= 2 {
			base = parts[1]
		} else {
			base = base
		}
	} else {
		base = strings.TrimPrefix(base, "r_")
	}

	var targetPrefix string
	if allSuccess {
		targetPrefix = "d_"
	} else {
		targetPrefix = "f_"
	}

	target := filepath.Join(parent, targetPrefix+base)

	// If target exists, add timestamp suffix to keep rename atomic and unique
	if _, err := os.Stat(target); err == nil {
		suffix := time.Now().UTC().Format("20060102T150405Z")
		target = filepath.Join(parent, fmt.Sprintf("%s%s_%s", targetPrefix, base, suffix))
	}

	// Use os.Rename which is atomic if within same filesystem
	if err := os.Rename(src, target); err != nil {
		return fmt.Errorf("os.Rename %s -> %s: %w", src, target, err)
	}
	return nil
}
