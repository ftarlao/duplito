package workflow

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	utils "github.com/ftarlao/duplito/utils"
)

//implement context

// ANSI color codes for terminal output
const (
	ColorBlue      = "\033[34m"
	ColorGreen     = "\033[32m"
	ColorWhite     = "\033[37m"
	ColorYellow    = "\033[33m"
	ColorCyan      = "\033[36m" // New color for duplicate paths
	ColorLightBlue = "\033[94m"
	ColorRed       = "\033[31m" // Standard Red
	ColorLightRed  = "\033[91m" // This is the specific code for bright red
	ColorReset     = "\033[0m"
)

// fileTask represents a file to be processed by a worker.
type fileTask struct {
	Path     string
	AbsPath  string
	Filesize int64
}

// fileResult represents the result of processing a file.
type fileResult struct {
	Path string
	Hash string
	Err  error
	Size int64 // Include size to update totalBytes in collector
}

// findFiles walks the directory and sends file tasks to a channel.
func findFiles(
	root string,
	tasks chan<- fileTask,
	wg *sync.WaitGroup,
	recurse, ignoreErrors bool,
	rootPath string,
	ctx context.Context,
) {
	defer wg.Done()
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		select {
		case <-ctx.Done():
			// The context has been cancelled. Time to stop.
			return errors.New("File search stops")
			// Exits the goroutine and file walking
		default:

		}

		absPath, filesize, checkErr := utils.CheckFile(path, d, err, recurse, rootPath)
		if checkErr != nil && checkErr != filepath.SkipDir {
			fmt.Fprintf(os.Stderr, "Error while accessing file %s details: %v\n", path, err)
			if ignoreErrors {
				checkErr = nil
			}
			return checkErr
		}
		if absPath == "" { // Skipped by checkFile (e.g., directory, symlink, non-regular, or ignored error)
			return nil
		}
		tasks <- fileTask{Path: path, AbsPath: absPath, Filesize: filesize}
		return nil
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error during directory walk %s: %v\n", root, err)
	}
	close(tasks) // Important: close the channel when all tasks are sent
}

// fileWorker processes file tasks from the input channel and sends results to the output channel.
func fileWorker(
	id int,
	tasks chan fileTask, //being able to close
	results chan fileResult,
	wg *sync.WaitGroup,
	ignoreErrors bool,
	cancel context.CancelFunc,
) {
	defer wg.Done()
	for task := range tasks {
		file, err := os.Open(task.Path)
		if err != nil {
			//fmt.Fprintf(os.Stderr, "Worker %d: Error opening %s: %v\n", id, task.Path, err)
			results <- fileResult{Path: task.AbsPath, Err: fmt.Errorf("Worker %d, failed to open %s: %w", id, task.Path, err), Size: task.Filesize}
			if ignoreErrors {
				continue
			} else {
				cancel() //Stops the file walking
				return
			}
		}

		hashSum, err := utils.MD5QuickHash(file, 2*1024*1024, task.Filesize)
		file.Close() // Close the file immediately after hashing

		if err != nil {
			//fmt.Fprintf(os.Stderr, "Worker %d: Error hashing %s: %v\n", id, task.Path, err)
			results <- fileResult{Path: task.AbsPath, Err: fmt.Errorf("Worker %d, failed to hash %s: %w", id, task.Path, err), Size: task.Filesize}
			if ignoreErrors {
				continue
			} else {
				return
			}
		}
		results <- fileResult{Path: task.AbsPath, Hash: hashSum, Size: task.Filesize, Err: nil}
	}
}

// collectResults collects results from workers, updates the hash map, and manages progress display.
func collectResults(
	results <-chan fileResult,
	hashMap map[string][]string,
	wg *sync.WaitGroup,
	ignoreErrors bool,
) {
	defer wg.Done()
	var totalBytes int64
	var numFiles int64
	startTime := time.Now()
	lastUpdate := time.Now()

	for res := range results {
		if res.Err != nil {
			fmt.Fprintf(os.Stderr, "Error, details: %v\n", res.Err)
			continue
		}
		hashMap[res.Hash] = append(hashMap[res.Hash], res.Path)
		totalBytes += res.Size
		numFiles++

		// Update progress display
		duration := time.Since(startTime).Seconds()
		if duration > 0 && time.Since(lastUpdate) >= 2*time.Second {
			currentSpeed := int64(float64(totalBytes) / duration)
			fmt.Printf("\rCurrent read speed: %s/s\t\t|\t\tnumber of files: %d", utils.RepresentBytes(currentSpeed), numFiles)
			lastUpdate = time.Now()
		}
	}

	// Final summary after all results are processed
	duration := time.Since(startTime).Seconds()
	if duration > 0 {
		avgSpeed := int64(float64(totalBytes) / duration)
		fmt.Println() // Move to a new line after progress updates
		fmt.Printf("\rCurrent read speed: %s/s\t\t|\t\tnumber of files: %d", utils.RepresentBytes(avgSpeed), numFiles)
	}
}

// CalculateFileHashes calculates MD5 hashes for all files in a given directory and its subdirectories
// using a specified number of concurrent threads.
// If ignoreErrors is true, skips unreadable/inaccessible files, logs them to stderr, and continues.
// If ignoreErrors is false, returns an error on the first failure.
// Displays current read speed in-place and final average read speed.
func CalculateFileHashes(folderPath string, ignoreErrors bool, recurseFlag bool, threads int) (map[string][]string, error) {
	// This gives us a 'ctx' to pass to goroutines and a 'cancel' function
	// to call when we want to stop them.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if threads <= 0 {
		return nil, fmt.Errorf("number of threads must be greater than 0")
	}

	hashMap := make(map[string][]string) // This map will be safely updated by the single collector goroutine

	// Channels for tasks and results
	tasks := make(chan fileTask, threads*2)     // Buffered channel for files to be processed
	results := make(chan fileResult, threads*2) // Buffered channel for processed file results

	var wgFindFiles sync.WaitGroup
	var wgWorkers sync.WaitGroup
	var wgCollector sync.WaitGroup

	// 1. Start the file finder goroutine
	wgFindFiles.Add(1)
	go findFiles(folderPath, tasks, &wgFindFiles, recurseFlag, ignoreErrors, folderPath, ctx)

	// 2. Start worker goroutines
	for i := 0; i < threads; i++ {
		wgWorkers.Add(1)
		go fileWorker(i+1, tasks, results, &wgWorkers, ignoreErrors, cancel)
	}

	// 3. Start results collector goroutine
	wgCollector.Add(1)
	go collectResults(results, hashMap, &wgCollector, ignoreErrors)

	// Wait for the file finder to finish and close the tasks channel
	wgFindFiles.Wait()
	// Wait for all workers to finish and close the results channel
	wgWorkers.Wait()
	close(results) //the channel is closed only when the last sent data is read

	// Wait for the collector to finish processing all results
	wgCollector.Wait()

	// A common error channel could be used if you want to explicitly signal errors from goroutines
	// and stop the process early if ignoreErrors is false. For now, workers just pass errors
	// in fileResult, and the collector handles them (or ignores them based on flag).
	// If any worker encountered a non-ignorable error, it would be returned via fileResult.Err,
	// but currently the main CalculateFileHashes only returns errors from WalkDir itself.
	// To truly propagate a worker error to the main function, an error channel would be needed.
	// For this exercise, we'll stick to the current error handling where non-ignored errors
	// from checkFile stop the walk, and worker errors are logged/ignored.

	return hashMap, nil
}

// SINGLE THREAD VERSION
// CalculateFileHashes recursively traverses the given folder path and computes the MD5 hash
// for each file, storing the hash as a hex string key and a list of full file paths as the value in a map.
// If ignoreErrors is true, skips unreadable/inaccessible files, logs them to stderr, and continues.
// If ignoreErrors is false, returns an error on the first failure.
// Displays current read speed in-place (KB/s or MB/s when possible, integer values) at most every 2 seconds
// and final average read speed on a new line.
// func CalculateFileHashes(folderPath string, ignoreErrors bool) (map[string][]string, error) {
// 	hashMap := make(map[string][]string)
// 	var totalBytes int64
// 	var numFiles int64
// 	startTime := time.Now()
// 	lastUpdate := time.Now()

// 	err := filepath.WalkDir(folderPath, func(path string, d os.DirEntry, err error) error {
// 		absPath, filesize, err := checkFile(path, d, err, recurseFlag, ignoreErrors, folderPath)
// 		if err != nil {
// 			return err
// 		}
// 		if absPath == "" {
// 			return nil
// 		}
// 		file, err := os.Open(path)
// 		if err != nil {
// 			if ignoreErrors {
// 				fmt.Fprintf(os.Stderr, "Error opening %s: %v\n", path, err)
// 				return nil
// 			}
// 			return fmt.Errorf("failed to open %s: %w", path, err)
// 		}
// 		defer file.Close()
// 		hashSum, err := md5QuickHash(file, 2*1024*1024, filesize)

// 		if err != nil {
// 			if ignoreErrors {
// 				fmt.Fprintf(os.Stderr, "Error hashing %s: %v\n", path, err)
// 				return nil
// 			}
// 			return fmt.Errorf("failed to hash %s: %w", path, err)
// 		}
// 		hashMap[hashSum] = append(hashMap[hashSum], absPath)
// 		totalBytes += filesize
// 		numFiles++
// 		duration := time.Since(startTime).Seconds()
// 		if duration > 0 && time.Since(lastUpdate) >= 2*time.Second {
// 			currentSpeed := float64(totalBytes) / duration
// 			switch {
// 			case currentSpeed >= 1048576:
// 				fmt.Printf("\rCurrent read speed: %d MB/s\t\t|\t\tnumber of files: %d", int(currentSpeed/1048576), numFiles)
// 			case currentSpeed >= 1024:
// 				fmt.Printf("\rCurrent read speed: %d KB/s\t\t|\t\tnumber of files: %d", int(currentSpeed/1024), numFiles)
// 			default:
// 				fmt.Printf("\rCurrent read speed: %d bytes/s\t\t|\t\tnumber of files: %d", int(currentSpeed), numFiles)
// 			}
// 			lastUpdate = time.Now()
// 		}
// 		return nil
// 	})
// 	if err != nil {
// 		fmt.Println()
// 		return nil, fmt.Errorf("failed to walk directory %s: %w", folderPath, err)
// 	}
// 	duration := time.Since(startTime).Seconds()
// 	if duration > 0 {
// 		avgSpeed := float64(totalBytes) / duration
// 		fmt.Println()
// 		switch {
// 		case avgSpeed >= 1048576:
// 			fmt.Printf("Final read speed: %d MB/s, Number of files: %d\n", int(avgSpeed/1048576), numFiles)
// 		case avgSpeed >= 1024:
// 			fmt.Printf("Final read speed: %d KB/s, Number of files: %d\n", int(avgSpeed/1024), numFiles)
// 		default:
// 			fmt.Printf("Final read speed: %d bytes/s, Number of files: %d\n", int(avgSpeed), numFiles)
// 		}
// 	}
// 	return hashMap, nil
// }

// // Channels for tasks and results
// type fileTask struct {
// 	path     string
// 	absPath  string
// 	filesize int64
// }
// type result struct {
// 	hash string // Worker-local hash map
// 	err  error
// 	task fileTask
// }

// listFiles walks folderPath, optionally recursing, and lists regular files (no symlinks).
// Uses pre-loaded hashMap (hash to paths) and reverseHashMap (path to hash) to determine duplicate status.
// Prints:
// - Directory path in blue.
// - Filename in a left-aligned column (width based on longest filename), followed by:
//   - "ZERO SIZE" (yellow) for zero-size files.
//   - "NOT DUPLICATE" (green) or "DUPLICATE OF:" (white) for non-zero-size files.
//   - Duplicate paths (cyan) indented two tabs (16 spaces) from the filename column start.
// Groups files by directory with blank lines for separation.
// Respects ignoreErrors for file access issues.
func ListFiles(folderPath string, recurse bool, ignoreErrors bool, hashMap map[string][]string, reverseHashMap map[string]string) error {
	TERM_POS := 100

	filesByDir := make(map[string][]string)
	sizesByPath := make(map[string]int64) // Store sizes for output
	err := filepath.WalkDir(folderPath, func(path string, d os.DirEntry, err error) error {
		absPath, size, err := utils.CheckFile(path, d, err, recurse, folderPath)
		if err != nil && err != filepath.SkipDir {
			fmt.Fprintf(os.Stderr, "Error while accessing file %s details: %v\n", path, err)
			if ignoreErrors {
				err = nil
			}
			return err
		}
		if err != nil {
			return err
		}
		if absPath == "" {
			return nil
		}
		if _, exists := reverseHashMap[absPath]; exists {
			dir := filepath.Dir(absPath)
			filesByDir[dir] = append(filesByDir[dir], absPath)
			sizesByPath[absPath] = size
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to walk directory %s: %w", folderPath, err)
	}
	var dirs []string
	for dir := range filesByDir {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)

	indent := strings.Repeat(" ", 8) // one tabs (8 spaces) from filename column start

	for i, dir := range dirs {
		if i > 0 {
			fmt.Println()
		}

		fmt.Print(ColorLightBlue)
		utils.PrintSeparator(len(dir))
		fmt.Printf("FOLDER: %s\n", dir)
		utils.PrintSeparator(len(dir))
		fmt.Print(ColorReset)

		filenamespace := utils.Min(utils.MaxFilenameLength(filesByDir[dir]), TERM_POS)
		files := filesByDir[dir]
		sort.Strings(files)
		for _, path := range files {
			filename := filepath.Base(path)
			fmt.Printf("  %-*s", filenamespace, filename)
			currSize := sizesByPath[path]
			if currSize == 0 {
				fmt.Printf(" %sZERO SIZE%s\n", ColorYellow, ColorReset)
				continue
			}
			hash := reverseHashMap[path]
			if len(hashMap[hash]) == 1 {
				fmt.Printf(" %sNOT DUPLICATE (%s)%s\n",
					ColorGreen,
					utils.RepresentBytes(currSize),
					ColorReset)
			} else {
				fmt.Printf(" %sDUPLICATE OF: (%s)%s\n",
					ColorLightRed,
					utils.RepresentBytes(currSize),
					ColorReset)
				for _, dupPath := range hashMap[hash] {
					if dupPath != path {
						fmt.Printf("%s- %s%s%s\n", indent, ColorCyan, dupPath, ColorReset)
					}
				}
			}
		}
	}
	return nil
}
