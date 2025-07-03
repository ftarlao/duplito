# Duplito 🔍 - Your Little Duplicate Finder

Duplito is a lightweight, efficient GO command-line tool designed to help you quickly identify duplicate files on your system. Whether you're cleaning up old 
downloads, organizing photos, or freeing up disk space, Duplito makes the process simple and straightforward.

## ✨ Features

* **Fast Scanning:** Utilizes efficient hashing algorithms (MD5 of file parts and size, yes an ehuristic) to compare file contents, not just names or sizes.
* **Flexible Paths:** Scan single directories, multiple directories, or even entire drives.
* **Detailed Output:** Clearly lists all identified duplicate groups, showing their paths and sizes.
* **Safe Operations:** Only list files and highlight duplicates, no disk changes are made

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
Output example:
![image](https://github.com/user-attachments/assets/59874128-68b1-48b5-be3d-ea5c2d2c99d6)

