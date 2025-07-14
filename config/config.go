package config

import (
	"encoding/gob"
	"fmt"
	"os"
	"path/filepath"

	utils "github.com/ftarlao/duplito/utils"
)

type Options struct {
	RecurseFlag        bool
	UpdateFlag         bool
	UpdateFullFlag     bool
	IgnoreErrorsFlag   bool
	NumThreads         int // New flag for number of threads
	Warnings           bool
	Summary            bool
	Overall            bool
	MinDirPerc         int
	MinDirBytes        int64
	OutputType         int //0 ALL, 1 SUMMARY, 2 ONLY FINAL SUMMARY
	DuplicatesOnlyFlag bool
	MinFileBytes       int64
}

// loadMap
// reads the map from ~/.duplito/filemap.gob if it exists.
// Returns an empty map if the file or folder doesn't exist.
func LoadMap() (map[utils.HashPair][]string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}
	configPath := filepath.Join(homeDir, ".duplito", "filemap.gob")

	file, err := os.Open(configPath)
	if os.IsNotExist(err) {
		return map[utils.HashPair][]string{}, nil // Return empty map if file doesn't exist
	} else if err != nil {
		return nil, fmt.Errorf("failed to open %s: %w", filepath.Base(configPath), err)
	}
	defer file.Close()

	var filemap map[utils.HashPair][]string
	if err := gob.NewDecoder(file).Decode(&filemap); err != nil {
		return nil, fmt.Errorf("failed to decode %s: %w", filepath.Base(configPath), err)
	}
	return filemap, nil
}

//saveMap saves the map to ~/.duplito/filemap.gob, creating the folder if needed.
func SaveMap(filemap map[utils.HashPair][]string) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}
	configPath := filepath.Join(homeDir, ".duplito", "filemap.gob")

	if err := os.MkdirAll(filepath.Dir(configPath), 0700); err != nil {
		return fmt.Errorf("failed to create %s folder: %w", filepath.Base(filepath.Dir(configPath)), err)
	}

	file, err := os.Create(configPath)
	if err != nil {
		return fmt.Errorf("failed to create %s: %w", filepath.Base(configPath), err)
	}
	defer file.Close()

	if err := gob.NewEncoder(file).Encode(filemap); err != nil {
		return fmt.Errorf("failed to encode %s: %w", filepath.Base(configPath), err)
	}
	return nil
}

// invertMap inverts a hash-to-paths map into a path-to-hash map.
// Each path in the input hashMap's slice becomes a key in the output map,
// with its corresponding hash as the value.
// If a path appears multiple times (unexpected), the last hash is used.
func InvertMap(hashMap map[utils.HashPair][]string) map[string]utils.HashPair {
	inverted := make(map[string]utils.HashPair)
	for hashPair, paths := range hashMap {
		for _, path := range paths {
			inverted[path] = hashPair
		}
	}
	return inverted
}
