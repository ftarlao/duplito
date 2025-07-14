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

	cfg "github.com/ftarlao/duplito/config"
	counters "github.com/ftarlao/duplito/counters"
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
	RealHash bool
	IsUpdate bool
}

// fileResult represents the result of processing a file.
type fileResult struct {
	Path       string
	HashPairID utils.HashPair //contains also filesize
	Err        error
	IsUpdate   bool
}

// findFiles walks the directory and sends file tasks to a channel.
func findFiles(
	paths []string,
	tasks chan<- fileTask,
	wg *sync.WaitGroup,
	opt cfg.Options,
	ctx context.Context,
) {
	defer wg.Done()

	sizeToFileTask := make(map[int64]fileTask) //record the first filetask for a filesize value

	for _, pathname := range paths {
		err := utils.HybridWalk(pathname, func(path string, d os.DirEntry, err error) error {
			select {
			case <-ctx.Done():
				// The context has been cancelled. Time to stop.
				return errors.New("File search stops")
				// Exits the goroutine and file walking
			default:

			}

			absPath, filesize, checkErr := utils.CheckFile(path, d, err, opt.RecurseFlag, path)
			if checkErr != nil && checkErr != filepath.SkipDir {
				fmt.Fprintf(os.Stderr, "Error while accessing file %s details: %v\n", path, err)
				if opt.IgnoreErrorsFlag {
					checkErr = nil
				}
				return checkErr
			}
			if absPath == "" { // Skipped by checkFile (e.g., directory, symlink, non-regular, or ignored error)
				return nil
			}

			ft := fileTask{Path: path, AbsPath: absPath, Filesize: filesize, RealHash: false, IsUpdate: false}
			if oldTask, ok := sizeToFileTask[filesize]; ok {
				//Other file with same size
				ft.RealHash = true
				if !oldTask.RealHash {
					//we have not processed the first, let's do it, it's like a delayed processing
					oldTask.RealHash = true
					oldTask.IsUpdate = true
					sizeToFileTask[filesize] = oldTask //update the status for this size
					tasks <- oldTask                   //sends also the previous task (recalcluate hash)
				}
			} else {
				sizeToFileTask[filesize] = ft
			}
			tasks <- ft
			return nil
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error during directory walk %s: %v\n", pathname, err)
			if !opt.IgnoreErrorsFlag {
				break
			}
		}
	}
	//fmt.Printf("\nUnique sizes: %d\n", len(sizeToFileTask))  //INSPECTION CODE
	close(tasks) // Important: close the channel when all tasks are sent
}

// fileWorker processes file tasks from the input channel and sends results to the output channel.
func fileWorker(
	id int,
	tasks chan fileTask, //being able to close
	results chan fileResult,
	wg *sync.WaitGroup,
	opt cfg.Options,
	cancel context.CancelFunc,
) {
	defer wg.Done()
	for task := range tasks {
		file, err := os.Open(task.Path)
		if err != nil {
			//fmt.Fprintf(os.Stderr, "Worker %d: Error opening %s: %v\n", id, task.Path, err)
			results <- fileResult{Path: task.AbsPath, Err: fmt.Errorf("Worker %d, failed to open %s: %w", id, task.Path, err)}
			if opt.IgnoreErrorsFlag {
				continue
			} else {
				cancel() //Stops the file walking
				return
			}
		}
		var hashSum string

		switch {
		case !task.RealHash:
			{
				hashSum = ""
			}
		case !opt.UpdateFullFlag:
			{
				hashSum, err = utils.QuickHashGen(file, 2*1024*1024, task.Filesize)
			}
		default:
			{
				//remains only the full hash
				hashSum, err = utils.HashGen(file)
			}
		}

		hashPair := utils.HashPair{
			Filesize: task.Filesize,
			Hash:     hashSum,
		}
		file.Close() // Close the file immediately after hashing

		if err != nil {
			//fmt.Fprintf(os.Stderr, "Worker %d: Error hashing %s: %v\n", id, task.Path, err)
			results <- fileResult{Path: task.AbsPath, Err: fmt.Errorf("Worker %d, failed to hash %s: %w", id, task.Path, err), IsUpdate: task.IsUpdate}
			if opt.IgnoreErrorsFlag {
				continue
			} else {
				return
			}
		}
		results <- fileResult{Path: task.AbsPath, HashPairID: hashPair, Err: nil, IsUpdate: task.IsUpdate}
	}
}

// collectResults collects results from workers, updates the hash map, and manages progress display.
func collectResults(
	results <-chan fileResult,
	hashMap map[utils.HashPair][]string,
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
		hashMap[res.HashPairID] = append(hashMap[res.HashPairID], res.Path)
		if !res.IsUpdate {
			totalBytes += res.HashPairID.Filesize
			numFiles++
		} else {
			//remove the older fake HashID that has an empty Hash part "" ; this one was useful
			//ONLY when ONE file has this filesize (no computation performed)
			oldPair := utils.HashPair{
				Filesize: res.HashPairID.Filesize,
				Hash:     "",
			}
			delete(hashMap, oldPair)
		}

		// Update progress display
		duration := time.Since(startTime).Seconds()
		if duration > 0 && time.Since(lastUpdate) >= 2*time.Second {
			currentSpeed := int64(float64(totalBytes) / duration)
			fmt.Printf("\r[Processed_filesize/sec] Read speed: %-25s|\t\tnumber of files: %d", utils.RepresentBytes(currentSpeed)+"/s", numFiles)
			lastUpdate = time.Now()
		}
	}

	// Final summary after all results are processed
	duration := time.Since(startTime).Seconds()
	if duration > 0 {
		avgSpeed := int64(float64(totalBytes) / duration)
		fmt.Println() // Move to a new line after progress updates
		fmt.Printf("\r[Processed_filesize/sec] Read speed: %-25s|\t\tnumber of files: %d", utils.RepresentBytes(avgSpeed)+"/s", numFiles)
	}
}

// CalculateFileHashes calculates MD5 hashes for all files in a given directory and its subdirectories
// using a specified number of concurrent threads.
// If ignoreErrors is true, skips unreadable/inaccessible files, logs them to stderr, and continues.
// If ignoreErrors is false, returns an error on the first failure.
// Displays current read speed in-place and final average read speed.
func CalculateFileHashes(
	paths []string,
	opt cfg.Options) (map[utils.HashPair][]string, error) {
	// This gives us a 'ctx' to pass to goroutines and a 'cancel' function
	// to call when we want to stop them.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if opt.NumThreads <= 0 {
		return nil, fmt.Errorf("number of threads must be greater than 0")
	}

	hashMap := make(map[utils.HashPair][]string) // This map will be safely updated by the single collector goroutine

	// Channels for tasks and results
	tasks := make(chan fileTask, opt.NumThreads*2)     // Buffered channel for files to be processed
	results := make(chan fileResult, opt.NumThreads*2) // Buffered channel for processed file results

	var wgFindFiles sync.WaitGroup
	var wgWorkers sync.WaitGroup
	var wgCollector sync.WaitGroup

	// 1. Start the file finder goroutine
	wgFindFiles.Add(1)
	go findFiles(paths, tasks, &wgFindFiles, opt, ctx)

	// 2. Start worker goroutines
	for i := 0; i < opt.NumThreads; i++ {
		wgWorkers.Add(1)
		go fileWorker(i+1, tasks, results, &wgWorkers, opt, cancel)
	}

	// 3. Start results collector goroutine
	wgCollector.Add(1)
	go collectResults(results, hashMap, &wgCollector, opt.IgnoreErrorsFlag)

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
	//

	return hashMap, nil
}

const TERM_POS int = 100                   //limits the positioning of file status in output
const SEP_WIDTH int = 70                   //width of  ---  separator
var indent string = strings.Repeat(" ", 8) // one tabs (8 spaces) from filename column start

func processSingleFolder(
	filesList []string,
	dir string,
	sizeByFile map[string]int64,
	overallStats *counters.Stats,
	hashMap map[utils.HashPair][]string,
	reverseHashMap map[string]utils.HashPair,
	opt cfg.Options,
) {
	var sb strings.Builder
	var dirStats counters.Stats

	filenamespace := utils.Min(utils.MaxFilenameLength(filesList)+8, TERM_POS)
	sort.Strings(filesList)
	for _, path := range filesList {
		filename := filepath.Base(path)
		fmt.Fprintf(&sb, "  %-*s", filenamespace, filename)
		filesize := sizeByFile[path]

		if filesize == 0 {
			fmt.Fprintf(&sb, " %sZERO SIZE%s\n", ColorYellow, ColorReset)
			overallStats.AddIgnoredFile(0)
			dirStats.AddIgnoredFile(0)
			continue
		}

		hash, exists := reverseHashMap[path]
		if !exists {
			fmt.Fprintf(&sb, " %sFILE NOT IN DATABASE%s\n", ColorYellow, ColorReset)
			overallStats.AddIgnoredFile(filesize)
			dirStats.AddIgnoredFile(filesize)
			continue
		}

		if len(hashMap[hash]) == 1 {
			overallStats.AddUniqueFile(filesize)
			dirStats.AddUniqueFile(filesize)
			if !opt.DuplicatesOnlyFlag {
				fmt.Fprintf(&sb, " %sNOT DUPLICATE (%s)%s\n",
					ColorGreen,
					utils.RepresentBytes(filesize),
					ColorReset)
			}

		} else {
			overallStats.AddDupFile(filesize)
			dirStats.AddDupFile(filesize)

			fmt.Fprintf(&sb, " %sDUPLICATE OF: (%s)%s\n",
				ColorLightRed,
				utils.RepresentBytes(filesize),
				ColorReset)
			for _, dupPath := range hashMap[hash] {
				if dupPath != path {
					fmt.Fprintf(&sb, "%s- %s%s%s\n", indent, ColorCyan, dupPath, ColorReset)
				}
			}

		}
	}

	if opt.Minperc <= utils.Max(int(dirStats.DupPerc()), int(dirStats.DupSizePerc())) &&
		opt.Minbytes <= dirStats.SizeofDupFiles {
		//Output Directory header
		if opt.OutputType <= 1 {
			fmt.Print(ColorLightBlue)
			utils.PrintSeparator(SEP_WIDTH)
			fmt.Printf("FOLDER: %s\n", dir)
			fmt.Print(dirStats.StringSummary())
			utils.PrintSeparator(SEP_WIDTH)
			fmt.Print(ColorReset)
		}
		//Output Files info for this Directory
		if opt.OutputType == 0 {
			fmt.Println(sb.String())
		}
		if opt.OutputType <= 1 {
			fmt.Println()
		}
	}
}

func ListFiles(
	paths []string,
	opt cfg.Options,
	hashMap map[utils.HashPair][]string,
	reverseHashMap map[string]utils.HashPair,
) error {

	var overallStats counters.Stats
	var filesInDir []string
	var currPath string
	//filesByDir := make(map[string][]string)
	sizeByFile := make(map[string]int64)

	for _, pathname := range paths {
		currPath = ""
		err := utils.HybridWalk(pathname, func(path string, d os.DirEntry, err error) error {
			absPath, size, err := utils.CheckFile(path, d, err, opt.RecurseFlag, pathname)
			if err != nil && err != filepath.SkipDir {
				fmt.Fprintf(os.Stderr, "Error while accessing file %s details: %v\n", path, err)
				if opt.IgnoreErrorsFlag {
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

			dir := filepath.Dir(absPath)
			if currPath == "" {
				//first time, let's use the first folder
				currPath = dir
			}

			if currPath != dir {
				//we are in another folder let's process previous one

				processSingleFolder(
					filesInDir,
					currPath,
					sizeByFile,
					&overallStats,
					hashMap,
					reverseHashMap,
					opt,
				)

				filesInDir = nil
				sizeByFile = make(map[string]int64)
				currPath = dir
			}

			filesInDir = append(filesInDir, absPath)
			sizeByFile[absPath] = size

			return nil
		})
		if err != nil {
			errwalk := fmt.Errorf("failed to walk directory or access file %s: %w", pathname, err)
			fmt.Fprintf(os.Stderr, errwalk.Error())
			if !opt.IgnoreErrorsFlag {
				return err
			}
		}

		//The last folder ends without triggering the process, here we have to do:
		processSingleFolder(
			filesInDir,
			currPath,
			sizeByFile,
			&overallStats,
			hashMap,
			reverseHashMap,
			opt,
		)

	}

	//Write overall stats
	utils.PrintSeparator(SEP_WIDTH)
	fmt.Println("OVERALL STATS")
	fmt.Print(overallStats.StringSummary())
	utils.PrintSeparator(SEP_WIDTH)
	return nil
}
