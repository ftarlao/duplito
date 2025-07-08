package counters

import (
	"fmt"

	"github.com/ftarlao/duplito/utils"
)

type Stats struct {
	NumFiles         int64
	NumDupFiles      int64
	NumIgnoredFiles  int64
	SizeofFiles      int64
	SizeofDupFiles   int64
	SizeIgnoredFiles int64
}

func (s *Stats) Reset() {
	s.NumFiles = 0
	s.NumDupFiles = 0
	s.SizeofDupFiles = 0
	s.SizeofFiles = 0
}

func (s *Stats) AddDupFile(size int64) {
	s.AddUniqueFile(size)
	s.NumDupFiles++
	s.SizeofDupFiles += size
}

func (s *Stats) AddUniqueFile(size int64) {
	s.NumFiles++
	s.SizeofFiles += size
}

func (s *Stats) AddIgnoredFile(size int64) {
	s.AddUniqueFile(size)
	s.NumIgnoredFiles++
	s.SizeIgnoredFiles += size
}

//Percentage of Duplicates files
func (s *Stats) DupPerc() float32 {
	return 100.0 * float32(s.NumDupFiles) / float32(s.NumFiles)
}

//Percentage of Duplicates filesize
func (s *Stats) DupSizePerc() float32 {
	return 100.0 * float32(s.SizeofDupFiles) / float32(s.SizeofFiles)
}

//Percentage of Duplicates filesize
func (s *Stats) StringSummary() string {
	text := fmt.Sprintf("\tFILES: %d\t\t\tSIZE: %s\n\tDUPLICATES: %d  [%.1f%%]\tDUP_SIZE: %s [%.1f%%]\n\tIGNORED: %d\t\t\tIGN_SIZE %s\n",
		s.NumFiles, utils.RepresentBytes(s.SizeofFiles),
		s.NumDupFiles, s.DupPerc(), utils.RepresentBytes(s.SizeofDupFiles), s.DupSizePerc(),
		s.NumIgnoredFiles, utils.RepresentBytes(s.SizeIgnoredFiles))
	return text
}
