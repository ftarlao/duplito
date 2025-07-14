# Duplito 🔍 - File Lister and Duplicate Finder

Duplito is a lightweight, efficient **command-line tool** designed to help you identify duplicate files on your system. Whether you're cleaning up old 
downloads, organizing photos, or freeing up disk space, Duplito makes the process simple and straightforward.
Duplito lists the files in folders (like 'ls' command or like 'find') by highlighting what is duplicate (and where its duplicates are) and what is not.

## Features

* **Fast Scanning:** Utilizes efficient hashing algorithms (quick hash: MD5 of file parts and filesize) to compare file contents, not just names or sizes.
* **Flexible Paths:** Scan single directories, subdirectories, and even entire drives.
* **Detailed Output:** Clearly lists all identified duplicate groups, showing their paths and sizes.
* **Safe Operations:** Only lists files and highlight duplicates, no disk changes are made

**VERY IMPORTANT** duplito looks also at the file content, but with -u, for huge files it only looks at the hash of the first and last portion of the file, and the filesize.
Please consider with -u the **equality measure an ehuristic**. For the full hash use -U 

```
Usage: ./duplito [-rUu] [-i] [-t num_threads] [<path1> ...]

./duplito identifies potential duplicates using a **composite MD5 hash**
derived from each file's content and size. Hashing info is stored at 
`~/.duplito/filemap.gob`. The program lists all requested files OR files
in a `folder-path`, highlighting duplicates and their respective locations.

When listing files <path1> defaults to current folder "."Options:
  -r, --recurse         Recurse into subdirectories (auto with -u or -U).
  -u, --update          Update hash database using quick-partial hash (implies -r).
                        If no paths, defaults to user home (or / for root).
  -U, --UPDATE          Update hash database using full file hash (implies -r).
  -d, --duplicates      Only shows the duplicates in filelist (summary not affected).
                        If no paths, defaults to user home (or / for root).
  -m, --min-file-size   Only lists files with size greater or equal, than the provided filesize.
                        Directory and overall summaries are not affected.
  -i, --ignore-errors   Ignore unreadable/inaccessible files.
  -t, --threads         Number of concurrent hashing threads (default: 3).

  -s, --summary         Display only 'per' directory summaries and the final overall
                        summary, with statistics.
  -o, --overall         Display only the final overall summary with statistics.

  -p, --min-dir-perc         Visualizes summary and file list only for folders with a percentage
                        of duplicates greater than the specified value (default: 0%).
  -b, --min-dir-bytes        Visualizes summary and file list only for folders with a file size
                        of duplicates that exceeds the provided value (default: 0 byte).
Behavior:
  -u or -U: Recursively computes and saves file hashes. Paths are
            optional, defaulting to user home or /.
  No -u/-U: Loads hash database and lists files with duplicate status.
            Paths or filenames are required for this mode.
```

Developed by Fabiano Tarlao (2025)

## How to Compile from Sources

Install git and golang  

```
git clone https://github.com/ftarlao/duplito.git
cd duplito
go mod tidy         (don't now, perhaps not mandatory)
```

In order to create a bin for local usage with all debug symbols:

```go build -o duplito```

In order to create a release (statically linked bin with debug stuff stripped, and useless path info removed):

```CGO_ENABLED=0 go build -a -trimpath -ldflags '-extldflags "-static" -s -w' -o duplito```


## Usage Examples

### Updating the File Database

To **update or create the files database** for all files within the `/home/pippo/` folder and its subfolders, use the `-u` option. This operation is crucial before checking for duplicates, as it builds the necessary index.
Please note that **the previous files database is overwritten**.

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

You can also ask to check for duplicates by providing specific filenames or a list of paths:
```Bash
duplito -r -i /home/pippo/file1.txt /home/pippo/temp/file2.bin /home/pippo/testdir/
```

You can also use the **shell expansion** to check for files with specific name pattern:
```Bash
duplito -r -i /home/pippo/*.txt 
```

You can omit the path information, 'duplito' lists files in the current folder 
```Bash
duplito   
```

the same as ```duplito .```

You can also choose to visualize only the statistic summaries, for each folder and subfolder 
and the overall summary (of all listed files). With these options the  file list are not
visualized, only summaries!

```Bash
duplito -r -s -i /home/pippo
```
In details, for each folder he visualize the summary only, and recurse into subdirs, also 
ignores errors (e.g. i/o errors, and so on) and keeps working on the other files.
![image](https://github.com/user-attachments/assets/01d39320-f0f6-4bcb-803c-903ea0bf92ce)

```Bash
duplito -r -o -i /home/pippo
```
Only the final 'statistics' summary for ALL files included in provided paths and folder (-r implies 
subfolders)
![image](https://github.com/user-attachments/assets/c4632b5b-e676-4943-909d-89794cac83ff)

Typical file list example:

![image](https://github.com/user-attachments/assets/b551921f-f040-4599-a9b5-80b3fb2811e9)
