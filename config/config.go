package config

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"

	_ "modernc.org/sqlite" // Pure Go SQLite driver
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

const (
	dbDirName  = ".duplito"
	dbFileName = "duplito.db"
	hashLength = 36 // Max length for the hash column
	// mmap_size is the maximum number of bytes of the database file that SQLite will try to map into the process address space.
	// A common large value is 256MB (256 * 1024 * 1024 bytes).
	mmap_size = 268435456
)

// FileRecord represents a record retrieved from the database,
// including the full directory name.
type FileRecord struct {
	Directory string
	Filename  string
	Hash      string
	Filesize  int64
}

// DBManager manages the SQLite database operations.
type DBManager struct {
	db *sql.DB
	mu sync.Mutex // Mutex to protect database access, especially for setup/teardown

	// Prepared statements
	stmtSelectFolderPK              *sql.Stmt
	stmtInsertFolder                *sql.Stmt
	stmtInsertFile                  *sql.Stmt
	stmtDeleteFile                  *sql.Stmt
	stmtCountByFilesize             *sql.Stmt
	stmtGetRecordsByHashAndFilesize *sql.Stmt
	stmtGetRecordByDirFilename      *sql.Stmt
	stmtUpdateHash                  *sql.Stmt
	stmtDeleteFolders               *sql.Stmt
	stmtDeleteFiles                 *sql.Stmt
	stmtGetFilesCount               *sql.Stmt
}

// NewDBManager initializes and returns a new DBManager instance.
// It sets up the database directory and creates tables if they don't exist.
func NewDBManager() (*DBManager, error) {
	dm := &DBManager{}
	err := dm.initDB()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize database: %w", err)
	}
	return dm, nil
}

// initDB handles opening the database, setting pragmas, and creating tables.
func (dm *DBManager) initDB() error {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get user home directory: %w", err)
	}

	dbPath := filepath.Join(homeDir, dbDirName)
	if err := os.MkdirAll(dbPath, 0755); err != nil {
		return fmt.Errorf("failed to create database directory %s: %w", dbPath, err)
	}

	dbFile := filepath.Join(dbPath, dbFileName)

	// Open the database connection.
	// _journal=WAL: Enables Write-Ahead Logging for better concurrency (reads during writes).
	// _foreign_keys=ON: Ensures foreign key constraints are enforced.
	// _timeout=5000: Sets a 5-second timeout for busy connections.
	db, err := sql.Open("sqlite", fmt.Sprintf("file:%s?_journal=WAL&_foreign_keys=ON&_timeout=5000", dbFile))
	if err != nil {
		return fmt.Errorf("failed to open database file %s: %w", dbFile, err)
	}

	// Set connection pool limits
	db.SetMaxOpenConns(2) //One thread reads and one write
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(0) //common for embeddable

	dm.db = db

	// Enable memory-mapped I/O for the main database file
	// WAL and SHM files are typically memory-mapped by default when WAL mode is active.
	_, err = dm.db.Exec(fmt.Sprintf("PRAGMA mmap_size = %d;", mmap_size))
	if err != nil {
		dm.db.Close()
		return fmt.Errorf("failed to set mmap_size pragma: %w", err)
	}

	// Create tables if they don't exist
	if err := dm.createTables(); err != nil {
		dm.db.Close() // Close on error
		return fmt.Errorf("failed to create tables: %w", err)
	}

	// Prepare all statements once
	if err := dm.prepareStatements(); err != nil {
		dm.db.Close() // Close on error
		return fmt.Errorf("failed to prepare statements: %w", err)
	}

	log.Printf("Database initialized successfully at %s", dbFile)
	return nil
}

// Close closes the database connection and all prepared statements.
func (dm *DBManager) Close() error {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	if dm.db == nil {
		return nil // Already closed or not initialized
	}

	// Close all prepared statements
	if dm.stmtSelectFolderPK != nil {
		dm.stmtSelectFolderPK.Close()
	}
	if dm.stmtInsertFolder != nil {
		dm.stmtInsertFolder.Close()
	}
	if dm.stmtInsertFile != nil {
		dm.stmtInsertFile.Close()
	}
	if dm.stmtDeleteFile != nil {
		dm.stmtDeleteFile.Close()
	}
	if dm.stmtCountByFilesize != nil {
		dm.stmtCountByFilesize.Close()
	}
	if dm.stmtGetRecordsByHashAndFilesize != nil {
		dm.stmtGetRecordsByHashAndFilesize.Close()
	}
	if dm.stmtGetRecordByDirFilename != nil {
		dm.stmtGetRecordByDirFilename.Close()
	}
	if dm.stmtUpdateHash != nil {
		dm.stmtUpdateHash.Close()
	}
	if dm.stmtDeleteFolders != nil {
		dm.stmtDeleteFolders.Close()
	}
	if dm.stmtDeleteFiles != nil {
		dm.stmtDeleteFiles.Close()
	}
	if dm.stmtGetFilesCount != nil {
		dm.stmtGetFilesCount.Close()
	}

	log.Println("Closing database connection.")
	return dm.db.Close()
}

// createTables creates the 'folders' and 'files' tables if they don't exist.
func (dm *DBManager) createTables() error {
	// Table for directories
	createFoldersTableSQL := `
	CREATE TABLE IF NOT EXISTS folders (
		folder_pk INTEGER PRIMARY KEY AUTOINCREMENT,
		foldername TEXT NOT NULL UNIQUE
	);`

	// Table for files
	createFilesTableSQL := fmt.Sprintf(`
	CREATE TABLE IF NOT EXISTS files (
		folders_fk INTEGER NOT NULL,
		filename TEXT NOT NULL,
		hash TEXT NOT NULL CHECK(LENGTH(hash) <= %d), -- Enforce max length at DB level
		filesize INTEGER NOT NULL,
		PRIMARY KEY (folders_fk, filename),
		FOREIGN KEY (folders_fk) REFERENCES folders(folder_pk) ON DELETE CASCADE
	);
	CREATE INDEX IF NOT EXISTS idx_files_hash_filesize ON files (hash, filesize); -- Composite index
	CREATE INDEX IF NOT EXISTS idx_files_filesize ON files (filesize);
	`, hashLength) // Use hashLength constant here

	_, err := dm.db.Exec(createFoldersTableSQL)
	if err != nil {
		return fmt.Errorf("failed to create folders table: %w", err)
	}

	_, err = dm.db.Exec(createFilesTableSQL)
	if err != nil {
		return fmt.Errorf("failed to create files table: %w", err)
	}

	return nil
}

// prepareStatements prepares all SQL statements for later reuse.
func (dm *DBManager) prepareStatements() error {
	var err error

	// For getFolderPK
	dm.stmtSelectFolderPK, err = dm.db.Prepare("SELECT folder_pk FROM folders WHERE foldername = ?")
	if err != nil {
		return fmt.Errorf("failed to prepare stmtSelectFolderPK: %w", err)
	}
	dm.stmtInsertFolder, err = dm.db.Prepare("INSERT INTO folders (foldername) VALUES (?)")
	if err != nil {
		return fmt.Errorf("failed to prepare stmtInsertFolder: %w", err)
	}

	// For InsertRow
	insertFileSQL := `
	INSERT INTO files (folders_fk, filename, hash, filesize)
	VALUES (?, ?, ?, ?)
	ON CONFLICT(folders_fk, filename) DO UPDATE SET
		hash = EXCLUDED.hash,
		filesize = EXCLUDED.filesize;`
	dm.stmtInsertFile, err = dm.db.Prepare(insertFileSQL)
	if err != nil {
		return fmt.Errorf("failed to prepare stmtInsertFile: %w", err)
	}

	// For DeleteRow
	dm.stmtDeleteFile, err = dm.db.Prepare("DELETE FROM files WHERE folders_fk = ? AND filename = ?")
	if err != nil {
		return fmt.Errorf("failed to prepare stmtDeleteFile: %w", err)
	}

	// For GetCountByFilesize
	dm.stmtCountByFilesize, err = dm.db.Prepare("SELECT COUNT(*) FROM files WHERE filesize = ?")
	if err != nil {
		return fmt.Errorf("failed to prepare stmtCountByFilesize: %w", err)
	}

	// For GetRecordsByHashAndFilesize
	getRecordsByHashAndFilesizeSQL := `
	SELECT f.filename, f.hash, f.filesize, d.foldername
	FROM files f
	JOIN folders d ON f.folders_fk = d.folder_pk
	WHERE f.hash = ? AND f.filesize = ?;`
	dm.stmtGetRecordsByHashAndFilesize, err = dm.db.Prepare(getRecordsByHashAndFilesizeSQL)
	if err != nil {
		return fmt.Errorf("failed to prepare stmtGetRecordsByHashAndFilesize: %w", err)
	}

	// For GetRecordByDirFilename
	getRecordByDirFilenameSQL := `
	SELECT f.filename, f.hash, f.filesize, d.foldername
	FROM files f
	JOIN folders d ON f.folders_fk = d.folder_pk
	WHERE d.foldername = ? AND f.filename = ?;`
	dm.stmtGetRecordByDirFilename, err = dm.db.Prepare(getRecordByDirFilenameSQL)
	if err != nil {
		return fmt.Errorf("failed to prepare stmtGetRecordByDirFilename: %w", err)
	}

	// For UpdateHash
	dm.stmtUpdateHash, err = dm.db.Prepare("UPDATE files SET hash = ? WHERE folders_fk = ? AND filename = ?")
	if err != nil {
		return fmt.Errorf("failed to prepare stmtUpdateHash: %w", err)
	}

	// For ClearAllRows
	dm.stmtDeleteFolders, err = dm.db.Prepare("DELETE FROM folders")
	if err != nil {
		return fmt.Errorf("failed to prepare stmtDeleteFolders: %w", err)
	}
	dm.stmtDeleteFiles, err = dm.db.Prepare("DELETE FROM files")
	if err != nil {
		return fmt.Errorf("failed to prepare stmtDeleteFiles: %w", err)
	}

	// For GetFilesCount
	dm.stmtGetFilesCount, err = dm.db.Prepare("SELECT COUNT(*) FROM files")
	if err != nil {
		return fmt.Errorf("failed to prepare stmtGetFilesCount: %w", err)
	}

	return nil
}

// getFolderPK retrieves the primary key for a given foldername,
// inserting it if it doesn't exist.
// This method now correctly uses the prepared statements bound to the transaction.
func (dm *DBManager) getFolderPK(tx *sql.Tx, foldername string) (int64, error) {
	var folderPK int64

	// Use the prepared statement bound to the current transaction
	err := tx.Stmt(dm.stmtSelectFolderPK).QueryRow(foldername).Scan(&folderPK)

	if err == sql.ErrNoRows {
		// Folder not found, insert it using the prepared statement bound to the transaction
		res, err := tx.Stmt(dm.stmtInsertFolder).Exec(foldername)
		if err != nil {
			return 0, fmt.Errorf("failed to insert folder '%s': %w", foldername, err)
		}
		folderPK, err = res.LastInsertId()
		if err != nil {
			return 0, fmt.Errorf("failed to get last insert ID for folder '%s': %w", foldername, err)
		}
	} else if err != nil {
		return 0, fmt.Errorf("failed to query folder '%s': %w", foldername, err)
	}
	return folderPK, nil
}

// InsertRow inserts a new file record into the database.
// It handles the directory lookup/insertion in the 'folders' table.
func (dm *DBManager) InsertRow(directory string, filename string, hash string, filesize int64) error {
	if len(hash) > hashLength {
		return fmt.Errorf("hash value '%s' exceeds maximum length of %d characters", hash, hashLength)
	}

	tx, err := dm.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback() // Rollback on error or if not explicitly committed

	// Use the transaction for getFolderPK as well
	folderPK, err := dm.getFolderPK(tx, directory)
	if err != nil {
		return fmt.Errorf("failed to get/insert folder PK for '%s': %w", directory, err)
	}

	_, err = dm.stmtInsertFile.Exec(folderPK, filename, hash, filesize)
	if err != nil {
		return fmt.Errorf("failed to insert/update file '%s/%s': %w", directory, filename, err)
	}

	return tx.Commit()
}

// DeleteRow deletes a file record based on its directory and filename.
func (dm *DBManager) DeleteRow(directory string, filename string) error {
	tx, err := dm.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Use the transaction for getFolderPK as well
	folderPK, err := dm.getFolderPK(tx, directory)
	if err != nil {
		return fmt.Errorf("failed to get folder PK for '%s': %w", directory, err) // Error if folder not found
	}

	res, err := dm.stmtDeleteFile.Exec(folderPK, filename)
	if err != nil {
		return fmt.Errorf("failed to delete file '%s/%s': %w", directory, filename, err)
	}

	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected for delete: %w", err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("file '%s/%s' not found for deletion", directory, filename)
	}

	return tx.Commit()
}

// GetCountByFilesize returns the number of files with the same filesize.
func (dm *DBManager) GetCountByFilesize(filesize int64) (int, error) {
	var count int
	err := dm.stmtCountByFilesize.QueryRow(filesize).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to get count by filesize %d: %w", filesize, err)
	}
	return count, nil
}

// GetRecordsByHashAndFilesize returns all file records for a provided hash and filesize.
func (dm *DBManager) GetRecordsByHashAndFilesize(hash string, filesize int64) ([]FileRecord, error) {
	rows, err := dm.stmtGetRecordsByHashAndFilesize.Query(hash, filesize)
	if err != nil {
		return nil, fmt.Errorf("failed to query records by hash '%s' and filesize %d: %w", hash, filesize, err)
	}
	defer rows.Close()

	var records []FileRecord
	for rows.Next() {
		var fr FileRecord
		if err := rows.Scan(&fr.Filename, &fr.Hash, &fr.Filesize, &fr.Directory); err != nil {
			return nil, fmt.Errorf("failed to scan record for hash '%s' and filesize %d: %w", hash, filesize, err)
		}
		records = append(records, fr)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows for hash '%s' and filesize %d: %w", hash, filesize, err)
	}

	return records, nil
}

// GetRecordByDirFilename returns a single file record for a provided directory and filename,
// including the full directory name.
func (dm *DBManager) GetRecordByDirFilename(directory string, filename string) (*FileRecord, error) {
	var fr FileRecord
	err := dm.stmtGetRecordByDirFilename.QueryRow(directory, filename).Scan(&fr.Filename, &fr.Hash, &fr.Filesize, &fr.Directory)
	if err == sql.ErrNoRows {
		return nil, nil // Record not found
	} else if err != nil {
		return nil, fmt.Errorf("failed to get record for '%s/%s': %w", directory, filename, err)
	}

	return &fr, nil
}

// UpdateHash updates the hash value for a file identified by directory and filename.
func (dm *DBManager) UpdateHash(directory string, filename string, newHash string) error {
	if len(newHash) > hashLength {
		return fmt.Errorf("new hash value '%s' exceeds maximum length of %d characters", newHash, hashLength)
	}

	tx, err := dm.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Use the transaction for getFolderPK as well
	folderPK, err := dm.getFolderPK(tx, directory)
	if err != nil {
		return fmt.Errorf("failed to get folder PK for '%s': %w", directory, err) // Error if folder not found
	}

	res, err := dm.stmtUpdateHash.Exec(newHash, folderPK, filename)
	if err != nil {
		return fmt.Errorf("failed to update hash for '%s/%s': %w", directory, filename, err)
	}

	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected for update: %w", err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("file '%s/%s' not found for update", directory, filename)
	}

	return tx.Commit()
}

// ClearAllRows deletes all records from both 'files' and 'folders' tables.
func (dm *DBManager) ClearAllRows() error {
	tx, err := dm.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	_, err = dm.stmtDeleteFolders.Exec()
	if err != nil {
		return fmt.Errorf("failed to clear folders table: %w", err)
	}

	// For good measure, also clear files table directly (though CASCADE should handle it)
	_, err = dm.stmtDeleteFiles.Exec()
	if err != nil {
		return fmt.Errorf("failed to clear files table: %w", err)
	}

	return tx.Commit()
}

// GetFilesCount returns the total number of records in the 'files' table.
func (dm *DBManager) GetFilesCount() (int, error) {
	var count int
	err := dm.stmtGetFilesCount.QueryRow().Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to get files count: %w", err)
	}
	return count, nil
}
