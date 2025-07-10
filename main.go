package main

import (
	"flag"
	"fmt"
	"os"

	config "github.com/ftarlao/duplito/config"
	utils "github.com/ftarlao/duplito/utils"
	workflow "github.com/ftarlao/duplito/workflow"
)

var (
	recurseFlag      bool
	updateFlag       bool
	updateFullFlag   bool
	ignoreErrorsFlag bool
	numThreads       int // New flag for number of threads
	warnings         bool
	summary          bool
	overall          bool
	minperc          int
	minbytes         int64
	outputType       int //0 ALL, 1 SUMMARY, 2 ONLY FINAL SUMMARY
)

// customUsage defines the help text for the program.
func customUsage() {
	appName := os.Args[0] // Get the program name

	// Usage Line (fits easily)
	fmt.Fprintf(os.Stderr, "Usage: %s [-rUu] [-i] [-t num_threads] [<path1> ...]\n\n", appName)

	// Description
	fmt.Fprintf(os.Stderr, "%s identifies potential duplicates using a **composite MD5 hash**\n", appName)
	fmt.Fprintf(os.Stderr, "derived from each file's content and size. Hashing info is stored at \n")
	fmt.Fprintf(os.Stderr, "`~/.duplito/filemap.gob`. The program lists all requested files OR files\n")
	fmt.Fprintf(os.Stderr, "in a `folder-path`, highlighting duplicates and their respective locations.\n\n")
	fmt.Fprintf(os.Stderr, "When listing files <path1> defaults to current folder \".\"")
	// Options
	fmt.Fprintf(os.Stderr, "Options:\n")
	fmt.Fprintf(os.Stderr, "  -r, --recurse         Recurse into subdirectories (auto with -u or -U).\n")
	fmt.Fprintf(os.Stderr, "  -u, --update          Update hash database using quick-partial hash (implies -r).\n")
	fmt.Fprintf(os.Stderr, "                        If no paths, defaults to user home (or / for root).\n")
	fmt.Fprintf(os.Stderr, "  -U, --UPDATE          Update hash database using full file hash (implies -r).\n")
	fmt.Fprintf(os.Stderr, "                        If no paths, defaults to user home (or / for root).\n")
	fmt.Fprintf(os.Stderr, "  -i, --ignore-errors   Ignore unreadable/inaccessible files.\n")
	fmt.Fprintf(os.Stderr, "  -t, --threads         Number of concurrent hashing threads (default: 3).\n\n")
	fmt.Fprintf(os.Stderr, "  -s, --summary         Display only 'per' directory summaries and the final overall\n")
	fmt.Fprintf(os.Stderr, "                        summary, with statistics.\n")
	fmt.Fprintf(os.Stderr, "  -o, --overall         Display only the final overall summary with statistics.\n\n")
	fmt.Fprintf(os.Stderr, "  -m, --minperc         Visualizes summary and file list for folders with a percentage\n")
	fmt.Fprintf(os.Stderr, "                        of duplicates greater than the specified value (default: 0%%).\n")
	fmt.Fprintf(os.Stderr, "  -b, --minbytes        Visualizes summary and file list for folders with a file size\n")
	fmt.Fprintf(os.Stderr, "                        of duplicates that exceeds the provided value (default: 0 byte).\n")

	// Behavior Notes
	fmt.Fprintf(os.Stderr, "Behavior:\n")
	fmt.Fprintf(os.Stderr, "  -u or -U: Recursively computes and saves file hashes. Paths are\n")
	fmt.Fprintf(os.Stderr, "            optional, defaulting to user home or /.\n")
	fmt.Fprintf(os.Stderr, "  No -u/-U: Loads hash database and lists files with duplicate status.\n")
	fmt.Fprintf(os.Stderr, "            Paths or filenames are required for this mode.\n\n")

	// Developer Credit
	fmt.Fprintf(os.Stderr, "Developed by Tarlao Fabiano.\n\n")
}

func init() {
	// Set custom usage function
	flag.Usage = customUsage
	// Define flags
	flag.BoolVar(&recurseFlag, "r", false, "")
	flag.BoolVar(&recurseFlag, "recurse", false, "")
	flag.BoolVar(&updateFlag, "u", false, "")
	flag.BoolVar(&updateFlag, "update", false, "")
	flag.BoolVar(&ignoreErrorsFlag, "i", false, "")
	flag.BoolVar(&ignoreErrorsFlag, "ignore-errors", false, "")
	flag.IntVar(&numThreads, "t", 3, "")       // Changed default to 3 threads
	flag.IntVar(&numThreads, "threads", 3, "") // Changed default to 3 threads
	flag.BoolVar(&updateFullFlag, "U", false, "")
	flag.BoolVar(&updateFullFlag, "UPDATE", false, "")
	flag.BoolVar(&summary, "s", false, "")
	flag.BoolVar(&summary, "summary", false, "") //only folder summary and final summary
	flag.BoolVar(&overall, "o", false, "")       //only final summary
	flag.BoolVar(&overall, "overall", false, "")
	flag.IntVar(&minperc, "m", 0, "")
	flag.IntVar(&minperc, "minperc", 0, "")
	flag.Int64Var(&minbytes, "b", 0, "")
	flag.Int64Var(&minbytes, "minbytes", 0, "")
}

func main() {

	flag.Parse()

	switch { // No expression here, defaults to 'switch true'
	case overall:
		outputType = 2
	case summary:
		outputType = 1
	default:
		outputType = 0
	}

	paths := flag.Args() // Collect all non-flag arguments as paths

	if len(paths) == 0 { // Ensure at least one path is provided
		if updateFlag || updateFullFlag { //manage the -u case that is permessive
			userPath, uerr := utils.UserPathInfo()
			if uerr != nil {
				fmt.Printf(uerr.Error())
				os.Exit(1)
			}
			paths = append(paths, userPath)
		} else {
			//file list mode, defaults to current folder
			paths = append(paths, ".")
		}
	}

	// Validate that all provided paths exist
	for _, path := range paths {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "Error: path '%s' does not exist\n", path)
			os.Exit(1)
		}
	}

	var filesHashMap = make(map[utils.HashPair][]string)

	if updateFlag || updateFullFlag {
		recurseFlag = true // -u implies -r
		var err error
		filesHashMap, err = workflow.CalculateFileHashes(
			paths,
			ignoreErrorsFlag,
			recurseFlag,
			numThreads,
			updateFullFlag,
		)

		if err != nil {
			fmt.Fprintf(os.Stderr, "\nError calculating hashes: %v\n", err)
			os.Exit(1)
		}
		if err = config.SaveMap(filesHashMap); err != nil {
			fmt.Fprintf(os.Stderr, "\nError saving config: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("\nFiles database updated successfully")
		fmt.Printf("Number of different files in database: %d\n", len(filesHashMap))
	} else {
		var err error
		filesHashMap, err = config.LoadMap()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
			os.Exit(1)
		}

		//COMMENTED OUT AN OLD STUDY OF MINE ABOUT UNIQUESS OF FILES
		//The files with unique filesize are 53710 and occupy: 185.1 GB on overall 243.5 GB
		// var uniqueSizesByte int64
		// var uniquecount int64
		// var overallSIZE int64
		// for hashID, path := range filesHashMap {
		// 	overallSIZE += hashID.Filesize * int64(len(path))
		// 	if hashID.Hash == "" {
		// 		uniqueSizesByte += hashID.Filesize
		// 		uniquecount++
		// 	}
		// }
		// fmt.Printf("\n\nThe files with unique filesize are %d and occupy: %s on overall %s \n\n", uniquecount, utils.RepresentBytes(uniqueSizesByte),
		// 	utils.RepresentBytes(overallSIZE))
		// os.Exit(1)

		fmt.Printf("File database loaded, Number of different files in database: %d\n", len(filesHashMap))
		reversefilesHashMap := config.InvertMap(filesHashMap)
		if err = workflow.ListFiles(
			paths,
			recurseFlag,
			ignoreErrorsFlag,
			filesHashMap,
			reversefilesHashMap,
			outputType,
			minperc,
			minbytes); err != nil {
			fmt.Fprintf(os.Stderr, "Error listing files: %v\n", err)
			os.Exit(1)
		}
	}
}
