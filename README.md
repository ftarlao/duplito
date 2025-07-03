# Duplito 🔍 - File Lister and Duplicate Finder

Duplito is a lightweight, efficient **command-line tool** designed to help you identify duplicate files on your system. Whether you're cleaning up old 
downloads, organizing photos, or freeing up disk space, Duplito makes the process simple and straightforward.
Duplito lists the files in folder (like 'ls' command or like 'find') by highlighting what is duplicate (and where its duplicates are) and what is not.

## Features

* **Fast Scanning:** Utilizes efficient hashing algorithms (quick hash: MD5 of file parts and filesize) to compare file contents, not just names or sizes.
* **Flexible Paths:** Scan single directories, subdirectories, and even entire drives.
* **Detailed Output:** Clearly lists all identified duplicate groups, showing their paths and sizes.
* **Safe Operations:** Only lists files and highlight duplicates, no disk changes are made

**VERY IMPORTANT** duplito looks also at the file content, but for huge files it only looks at the hash of the first and last portion of the file, and the filesize.
Please consider the **equality measure an ehuristic**. I'll add the full-hash feature in the future. 

```
Usage: ./duplito [-r] [-u] [-i] [-t num_threads] <folder-path>

`duplito` identifies potential duplicates using a **composite MD5 hash** derived from a portion of each file's content and its size. This hashing information is stored in a database located at `~/.duplito/filemap.gob`. The program lists all files **in a requested `folder-path`**, explicitly highlighting duplicates and indicating their respective locations.
Options:
  -r, --recurse         Recurse into subdirectories (automatic with -u)
  -u, --update          Update hash database (implies -r)
  -i, --ignore-errors   Ignore unreadable/inaccessible files
  -t, --threads         Number of concurrent hashing threads (default: 3)
Behavior:
  -u: Recursively compute and save file hashes.
  No -u: Load hash database and list files with duplicate status.
```
Developed by Fabiano Tarlao (2025)

## Usage Examples

### Updating the File Database

To **update or create the files database** for all files within the `/home/pippo/` folder and its subfolders, use the `-u` option. This operation is crucial before checking for duplicates, as it builds the necessary index.

```bash
duplito -u -i /home/pippo/
```

After running this, you'll be ready to identify duplicates across all files in '/home/pippo/' and its subfolders.

### Checking for Duplicates in a Specific Directory

To **identify duplicate and unique files** specifically within the' /home/pippo/testdir/' directory, use the '-r' option.
```Bash
duplito -r -i /home/pippo/testdir/
```
Files with zero byte filesize are not checked to be duplicates, are flagged ZERO SIZE.  
Typical file list example:

![duplito_example](https://github.com/user-attachments/assets/2f750281-6aff-49b9-a5b3-051b70f9af97)
