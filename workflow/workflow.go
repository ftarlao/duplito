package workflow

import (
	"context"
	"crypto/md5"
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
	Filename   string
	PathFolder string
	Filesize   int64
	RealHash   bool
	//UpdatePrevious bool
	IsUpdate bool
}

// fileResult represents the result of processing a file.
type fileResult struct {
	Filename   string
	PathFolder string
	HashPairID utils.HashPair //contains hash and filesize both
	Err        error
	IsUpdate   bool
}

// findFiles walks the directory and sends file tasks to a channel.
func findFiles(
	paths []string,
	tasks chan<- fileTask,
	dbManager *cfg.DBManager,
	wg *sync.WaitGroup,
	opt cfg.Options,
	ctx context.Context,

) {
	defer wg.Done()
	var filesizeCount map[int64]int = make(map[int64]int)

	for _, pathname := range paths {
		err := utils.HybridWalk(pathname, func(path string, d os.DirEntry, err error) error {
			select {
			case <-ctx.Done():
				// The context has been cancelled. Time to stop.
				return errors.New("File search stops")
				// Exits the goroutine and file walking
			default:

			}

			fileName, absFolderPath, filesize, checkErr := utils.CheckFile(path, d, err, opt.RecurseFlag, path)
			if checkErr != nil && checkErr != filepath.SkipDir {
				fmt.Fprintf(os.Stderr, "Error while accessing file %s details: %v\n", path, err)
				if opt.IgnoreErrorsFlag {
					checkErr = nil
				}
				return checkErr
			}
			if fileName == "" { // Skipped by checkFile (e.g., directory, symlink, non-regular, or ignored error)
				return nil
			}

			num := filesizeCount[filesize]
			filesizeCount[filesize]++

			ft := fileTask{Filename: fileName,
				PathFolder: absFolderPath,
				Filesize:   filesize,
				RealHash:   false,
				IsUpdate:   false,
			}

			if num >= 1 {
				ft.RealHash = true
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
	myHashEngine := md5.New()
	for task := range tasks {
		fullAbsFilename := filepath.Join(task.PathFolder, task.Filename)
		file, err := os.Open(fullAbsFilename)
		if err != nil {
			//fmt.Fprintf(os.Stderr, "Worker %d: Error opening %s: %v\n", id, task.Path, err)
			results <- fileResult{Filename: task.Filename,
				PathFolder: task.PathFolder,
				Err: fmt.Errorf("Worker %d, failed to open %s: %w", id,
					fullAbsFilename,
					err)}
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
				hashSum, err = utils.QuickHashGen(myHashEngine, file, 2*1024*1024, task.Filesize)
			}
		default:
			{
				//remains only the full hash
				hashSum, err = utils.HashGen(myHashEngine, file)
			}
		}

		hashPair := utils.HashPair{
			Filesize: task.Filesize,
			Hash:     hashSum,
		}
		file.Close() // Close the file immediately after hashing

		if err != nil {
			//fmt.Fprintf(os.Stderr, "Worker %d: Error hashing %s: %v\n", id, task.Path, err)
			results <- fileResult{Filename: task.Filename,
				PathFolder: task.PathFolder,
				Err:        fmt.Errorf("Worker %d, failed to hash %s: %w", id, fullAbsFilename, err),
				IsUpdate:   task.IsUpdate}
			if opt.IgnoreErrorsFlag {
				continue
			} else {
				return
			}
		}
		results <- fileResult{Filename: task.Filename,
			PathFolder: task.PathFolder,
			HashPairID: hashPair,
			Err:        nil,
			IsUpdate:   task.IsUpdate}
	}
}

// collectResults collects results from workers, updates the hash map, and manages progress display.
func collectResults(
	results <-chan fileResult,
	task chan fileTask,
	dbManager *cfg.DBManager,
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

		err := dbManager.InsertRow(res.PathFolder, res.Filename,
			res.HashPairID.Hash, res.HashPairID.Filesize)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error, details: %v\n", res.Err)
			os.Exit(0)
		}

		if !res.IsUpdate {
			totalBytes += res.HashPairID.Filesize
			numFiles++
		}

		num, err := dbManager.GetCountByFilesize(res.HashPairID.Filesize)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error, details: %v\n", res.Err)
			os.Exit(0)
		}
		if num == 2 {
			//look for old fake hash, the first inserted filesize
			var records []cfg.FileRecord
			records, err = dbManager.GetRecordsByHashAndFilesize("", res.HashPairID.Filesize)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error, details: %v\n", res.Err)
				os.Exit(0)
			}
			faulty := records[0]
			var updateTask fileTask = fileTask{
				Filename:   faulty.Filename,
				PathFolder: faulty.Directory,
				Filesize:   faulty.Filesize,
				RealHash:   true,
				IsUpdate:   true,
			}
			task <- updateTask
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
	opt cfg.Options,
	dbManager *cfg.DBManager) error {
	// This gives us a 'ctx' to pass to goroutines and a 'cancel' function
	// to call when we want to stop them.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if opt.NumThreads <= 0 {
		return fmt.Errorf("number of threads must be greater than 0")
	}

	// Channels for tasks and results
	tasks := make(chan fileTask, opt.NumThreads*2)     // Buffered channel for files to be processed
	results := make(chan fileResult, opt.NumThreads*2) // Buffered channel for processed file results

	var wgFindFiles sync.WaitGroup
	var wgWorkers sync.WaitGroup
	var wgCollector sync.WaitGroup

	// 1. Start the file finder goroutine
	wgFindFiles.Add(1)
	go findFiles(paths, tasks, dbManager, &wgFindFiles, opt, ctx)

	// 2. Start worker goroutines
	for i := 0; i < opt.NumThreads; i++ {
		wgWorkers.Add(1)
		go fileWorker(i+1, tasks, results, &wgWorkers, opt, cancel)
	}

	// 3. Start results collector goroutine
	wgCollector.Add(1)
	go collectResults(results, tasks, dbManager, &wgCollector, opt.IgnoreErrorsFlag)

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

	return nil
}

const TERM_POS int = 100                   //limits the positioning of file status in output
const SEP_WIDTH int = 70                   //width of  ---  separator
var indent string = strings.Repeat(" ", 8) // one tabs (8 spaces) from filename column start

func processSingleFolder(
	filesList []cfg.FileRecord,
	dir string,
	dbManager *cfg.DBManager,
	overallStats *counters.Stats,
	opt cfg.Options,
) {
	var sb strings.Builder
	var dirStats counters.Stats

	filenamespace := utils.Min(utils.MaxFilenameLength(filesList)+8, TERM_POS)
	// Sort by Directory (ascending), then by Filename (ascending)
	sort.Slice(filesList, func(i, j int) bool {
		if filesList[i].Directory != filesList[j].Directory {
			return filesList[i].Directory < filesList[j].Directory
		}
		return filesList[i].Filename < filesList[j].Filename
	})

	for _, path := range filesList {

		oksize := path.Filesize >= opt.MinFileBytes

		if path.Filesize == 0 {
			utils.FprintfIf(!opt.DuplicatesOnlyFlag && oksize,
				&sb, "  %-*s", filenamespace, path.Filename)
			utils.FprintfIf(!opt.DuplicatesOnlyFlag && oksize,
				&sb, " %sZERO SIZE%s\n", ColorYellow, ColorReset)
			overallStats.AddIgnoredFile(0)
			dirStats.AddIgnoredFile(0)
			continue
		}

		fileRecord, _ := dbManager.GetRecordByDirFilename(path.Directory, path.Filename)

		if fileRecord == nil {
			utils.FprintfIf(!opt.DuplicatesOnlyFlag && oksize,
				&sb, "  %-*s", filenamespace, path.Filename)
			utils.FprintfIf(!opt.DuplicatesOnlyFlag && oksize,
				&sb, " %sFILE NOT IN DATABASE%s\n", ColorYellow, ColorReset)
			overallStats.AddIgnoredFile(path.Filesize)
			dirStats.AddIgnoredFile(path.Filesize)
			continue
		}

		withSameHash, err := dbManager.GetRecordsByHashAndFilesize(path.Hash, path.Filesize)
		if err != nil {
			fmt.Fprintf(os.Stderr, "File database access, details: %v\n", err)
			os.Exit(0)
		}

		if len(withSameHash) == 1 {
			overallStats.AddUniqueFile(path.Filesize)
			dirStats.AddUniqueFile(path.Filesize)
			utils.FprintfIf(!opt.DuplicatesOnlyFlag && oksize,
				&sb, "  %-*s", filenamespace, path.Filename)
			utils.FprintfIf(!opt.DuplicatesOnlyFlag && oksize,
				&sb, " %sNOT DUPLICATE (%s)%s\n",
				ColorGreen,
				utils.RepresentBytes(path.Filesize),
				ColorReset)

		} else {
			overallStats.AddDupFile(path.Filesize)
			dirStats.AddDupFile(path.Filesize)

			utils.FprintfIf(oksize, &sb, "  %-*s", filenamespace, path.Filesize)
			utils.FprintfIf(oksize, &sb, " %sDUPLICATE OF: (%s)%s\n",
				ColorLightRed,
				utils.RepresentBytes(path.Filesize),
				ColorReset)
			for _, dupPath := range withSameHash {
				if dupPath != path {
					utils.FprintfIf(oksize,
						&sb, "%s- %s%s%s\n", indent, ColorCyan, dupPath, ColorReset)
				}
			}

		}
	}

	if opt.MinDirPerc <= utils.Max(int(dirStats.DupPerc()), int(dirStats.DupSizePerc())) &&
		opt.MinDirBytes <= dirStats.SizeofDupFiles {
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
	dbManager *cfg.DBManager,
) error {

	var overallStats counters.Stats
	var filesInDir []cfg.FileRecord
	var currPath string

	for _, pathname := range paths {
		currPath = ""
		err := utils.HybridWalk(pathname, func(path string, d os.DirEntry, err error) error {
			var newFile cfg.FileRecord
			newFile.Filename, newFile.Directory, newFile.Filesize, err = utils.CheckFile(path, d, err, opt.RecurseFlag, pathname)
			newFile.Hash = ""

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
			if newFile.Filename == "" {
				return nil
			}

			if currPath == "" {
				//first time, let's use the first folder
				currPath = newFile.Directory
			}

			if currPath != newFile.Directory {
				//we are in another folder let's process previous one

				processSingleFolder(
					filesInDir,
					currPath,
					dbManager,
					&overallStats,
					opt,
				)

				filesInDir = nil
				currPath = newFile.Directory
			}

			filesInDir = append(filesInDir, newFile)

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
			dbManager,
			&overallStats,
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
