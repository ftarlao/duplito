package utils

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	// Import the term package
)

func Int64ToBytes(n int64) []byte {
	bytes := make([]byte, 8)
	for i := 0; i < 8; i++ {
		bytes[7-i] = byte(n >> (i * 8))
	}
	return bytes
}

func RepresentBytes(numbytes int64) string {
	switch {
	case numbytes >= 1048576:
		return fmt.Sprintf("%d MB", numbytes/1048576)
	case numbytes >= 1024:
		return fmt.Sprintf("%d KB", numbytes/1024)
	default:
		return fmt.Sprintf("%d Byte", numbytes)
	}
}

// PrintSeparator prints a line of hyphens that tries to match the terminal width.
// If the width cannot be determined (e.g., not running in a TTY), it prints a default length.
func PrintSeparator(len int) {

	// Create a string of hyphens with the determined width
	separator := strings.Repeat("-", len)

	// Print the separator
	fmt.Println(separator)
}

func Min(a, b int) int {
	if a > b {
		return b
	}
	return a
}

func MD5hash(file io.Reader) (string, error) {
	if file == nil {
		return "", fmt.Errorf("nil reader")
	}
	hash := md5.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("failed to hash: %w", err)
	}
	hashSum := fmt.Sprintf("%x", hash.Sum(nil))
	return hashSum, nil
}

type HashPair struct {
	Filesize int64 //please note this come first, useful for equality check
	Hash     string
}

func MD5QuickHash(file io.Reader, areasize int64, fileSize int64) (string, error) {
	const BIG_MULTIPLIER int = 10
	var tinyfile bool = false
	if file == nil {
		return "", fmt.Errorf("nil reader")
	}
	if areasize <= 0 {
		return "", fmt.Errorf("invalid areasize: %d", areasize)
	}
	seeker, ok := file.(io.Seeker)
	if !ok {
		return "", fmt.Errorf("file does not support seeking")
	}
	var readsize int64
	if int64(BIG_MULTIPLIER)*areasize >= fileSize {
		//seek time has a cost, for these reason
		tinyfile = true
		readsize = fileSize
	} else {
		readsize = areasize / 2
	}

	hash := md5.New()
	//hashing
	if fileSize != 0 {
		if _, err := io.CopyN(hash, file, readsize); err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
			return "", fmt.Errorf("failed to hash: %w", err)
		}
		if !tinyfile {
			_, err := seeker.Seek(-readsize, io.SeekEnd)
			if err != nil {
				return "", fmt.Errorf("failed to seek to last %d bytes: %w", readsize, err)
			}
			if _, err := io.CopyN(hash, file, readsize); err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
				return "", fmt.Errorf("failed to hash: %w", err)
			}
		}
	}
	//add tilesize info to the hash generation
	hash.Write(Int64ToBytes(fileSize))
	hashSum := hex.EncodeToString(hash.Sum(nil))
	return hashSum, nil

}

// checkFile performs common file checks for WalkDir callbacks.
// Returns the absolute path and size for valid regular files, or empty string, zero size, and nil to skip,
// or an error if ignoreErrors is false and a failure occurs.
func CheckFile(path string, d os.DirEntry, err error, recurse bool, rootPath string) (string, int64, error) {
	if !recurse && d.IsDir() && path != rootPath {
		return "", 0, filepath.SkipDir
	}
	if d.IsDir() {
		return "", 0, nil
	}
	if err != nil {
		return "", 0, fmt.Errorf("failed to access %s: %v", path, err)
	}
	fileInfo, err := d.Info()
	if err != nil {

		return "", 0, fmt.Errorf("failed to get info for %s: %v", path, err)
	}
	if fileInfo.Mode()&os.ModeSymlink != 0 {
		fmt.Fprintf(os.Stderr, "\nSkipping symbolic link %s\n", path)
		return "", 0, nil
	}
	if !fileInfo.Mode().IsRegular() {

		return "", 0, fmt.Errorf("%s is not a regular file", path)
	}
	absPath, err := filepath.Abs(path)
	if err != nil {

		return "", 0, fmt.Errorf("failed to get absolute path for %s: %v", path, err)
	}
	return absPath, fileInfo.Size(), nil
}

// maxFilenameLength returns the length of the longest filename in the given paths.
// Returns a minimum of 10 to avoid cramped output.
func MaxFilenameLength(paths []string) int {
	maxLen := 0
	for _, path := range paths {
		if len(filepath.Base(path)) > maxLen {
			maxLen = len(filepath.Base(path))
		}
	}
	if maxLen < 10 {
		maxLen = 10
	}
	return maxLen
}
