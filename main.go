package main

import (
	"flag"
	"fmt"
	"os"

	config "github.com/ftarlao/duplito/config"
	workflow "github.com/ftarlao/duplito/workflow"
)

var (
	recurseFlag      bool
	updateFlag       bool
	ignoreErrorsFlag bool
	numThreads       int // New flag for number of threads
)

// customUsage defines the help text for the program.
func customUsage() {
	fmt.Fprintf(os.Stderr, "Usage: %s [-r] [-u] [-i] [-t num_threads] <folder-path>\n\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "Finds duplicate files in folder-path using MD5 hashes stored in ~/.duplito/filemap.gob.\n")
	fmt.Fprintf(os.Stderr, "Options:\n")
	fmt.Fprintf(os.Stderr, "  -r, --recurse         Recurse into subdirectories (automatic with -u)\n")
	fmt.Fprintf(os.Stderr, "  -u, --update          Update hash database (implies -r)\n")
	fmt.Fprintf(os.Stderr, "  -i, --ignore-errors   Ignore unreadable/inaccessible files\n")
	fmt.Fprintf(os.Stderr, "  -t, --threads         Number of concurrent hashing threads (default: 3)\n")
	fmt.Fprintf(os.Stderr, "Behavior:\n")
	fmt.Fprintf(os.Stderr, "  -u: Recursively compute and save file hashes.\n")
	fmt.Fprintf(os.Stderr, "  No -u: Load hash database and list files with duplicate status.\n")
	fmt.Fprintf(os.Stderr, "\nDeveloped by Fabiano Tarlao ftarlao@github.com\n\n")
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
}

func main() {

	flag.Parse()

	args := flag.Args()
	if len(args) != 1 {
		flag.Usage()
		os.Exit(1)
	}
	folderPath := args[0]

	if _, err := os.Stat(folderPath); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Error: folder %s does not exist\n", folderPath)
		os.Exit(1)
	}

	var filesHashMap = make(map[string][]string)

	if updateFlag {
		recurseFlag = true // -u implies -r
		var err error
		filesHashMap, err = workflow.CalculateFileHashes(folderPath, ignoreErrorsFlag, recurseFlag, numThreads)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error calculating hashes: %v\n", err)
			os.Exit(1)
		}
		if err = config.SaveMap(filesHashMap); err != nil {
			fmt.Fprintf(os.Stderr, "Error saving config: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Configuration updated successfully")
		fmt.Printf("Number of different files in database: %d\n", len(filesHashMap))
	} else {
		var err error
		filesHashMap, err = config.LoadMap()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Loaded configuration:")
		fmt.Printf("Number of different files in database: %d\n", len(filesHashMap))
		reversefilesHashMap := config.InvertMap(filesHashMap)
		if err = workflow.ListFiles(folderPath, recurseFlag, ignoreErrorsFlag, filesHashMap, reversefilesHashMap); err != nil {
			fmt.Fprintf(os.Stderr, "Error listing files: %v\n", err)
			os.Exit(1)
		}
	}
}
