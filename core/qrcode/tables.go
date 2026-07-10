package qrcode

// blockSpec is the Reed-Solomon block structure for one version/level: the EC
// codewords per block, and the two possible data-block groups. Group 2 fields
// are zero when the version/level has a single block size.
type blockSpec struct {
	ecPerBlock   int
	group1Blocks int
	group1Words  int
	group2Blocks int
	group2Words  int
}

// ecBlocks[version][level] holds the EC-block structure from ISO/IEC 18004
// Table 9 (2015 edition). Index 0 is unused (versions are 1-based); the level
// order is L, M, Q, H, matching the Level iota. All 40 versions are present;
// the total codeword count per version is invariant across the four levels
// (asserted by TestTotalCodewordsConstantPerVersion).
var ecBlocks = [41][4]blockSpec{
	1: {
		LevelL: {ecPerBlock: 7, group1Blocks: 1, group1Words: 19},
		LevelM: {ecPerBlock: 10, group1Blocks: 1, group1Words: 16},
		LevelQ: {ecPerBlock: 13, group1Blocks: 1, group1Words: 13},
		LevelH: {ecPerBlock: 17, group1Blocks: 1, group1Words: 9},
	},
	2: {
		LevelL: {ecPerBlock: 10, group1Blocks: 1, group1Words: 34},
		LevelM: {ecPerBlock: 16, group1Blocks: 1, group1Words: 28},
		LevelQ: {ecPerBlock: 22, group1Blocks: 1, group1Words: 22},
		LevelH: {ecPerBlock: 28, group1Blocks: 1, group1Words: 16},
	},
	3: {
		LevelL: {ecPerBlock: 15, group1Blocks: 1, group1Words: 55},
		LevelM: {ecPerBlock: 26, group1Blocks: 1, group1Words: 44},
		LevelQ: {ecPerBlock: 18, group1Blocks: 2, group1Words: 17},
		LevelH: {ecPerBlock: 22, group1Blocks: 2, group1Words: 13},
	},
	4: {
		LevelL: {ecPerBlock: 20, group1Blocks: 1, group1Words: 80},
		LevelM: {ecPerBlock: 18, group1Blocks: 2, group1Words: 32},
		LevelQ: {ecPerBlock: 26, group1Blocks: 2, group1Words: 24},
		LevelH: {ecPerBlock: 16, group1Blocks: 4, group1Words: 9},
	},
	5: {
		LevelL: {ecPerBlock: 26, group1Blocks: 1, group1Words: 108},
		LevelM: {ecPerBlock: 24, group1Blocks: 2, group1Words: 43},
		LevelQ: {ecPerBlock: 18, group1Blocks: 2, group1Words: 15, group2Blocks: 2, group2Words: 16},
		LevelH: {ecPerBlock: 22, group1Blocks: 2, group1Words: 11, group2Blocks: 2, group2Words: 12},
	},
	6: {
		LevelL: {ecPerBlock: 18, group1Blocks: 2, group1Words: 68},
		LevelM: {ecPerBlock: 16, group1Blocks: 4, group1Words: 27},
		LevelQ: {ecPerBlock: 24, group1Blocks: 4, group1Words: 19},
		LevelH: {ecPerBlock: 28, group1Blocks: 4, group1Words: 15},
	},
	7: {
		LevelL: {ecPerBlock: 20, group1Blocks: 2, group1Words: 78},
		LevelM: {ecPerBlock: 18, group1Blocks: 4, group1Words: 31},
		LevelQ: {ecPerBlock: 18, group1Blocks: 2, group1Words: 14, group2Blocks: 4, group2Words: 15},
		LevelH: {ecPerBlock: 26, group1Blocks: 4, group1Words: 13, group2Blocks: 1, group2Words: 14},
	},
	8: {
		LevelL: {ecPerBlock: 24, group1Blocks: 2, group1Words: 97},
		LevelM: {ecPerBlock: 22, group1Blocks: 2, group1Words: 38, group2Blocks: 2, group2Words: 39},
		LevelQ: {ecPerBlock: 22, group1Blocks: 4, group1Words: 18, group2Blocks: 2, group2Words: 19},
		LevelH: {ecPerBlock: 26, group1Blocks: 4, group1Words: 14, group2Blocks: 2, group2Words: 15},
	},
	9: {
		LevelL: {ecPerBlock: 30, group1Blocks: 2, group1Words: 116},
		LevelM: {ecPerBlock: 22, group1Blocks: 3, group1Words: 36, group2Blocks: 2, group2Words: 37},
		LevelQ: {ecPerBlock: 20, group1Blocks: 4, group1Words: 16, group2Blocks: 4, group2Words: 17},
		LevelH: {ecPerBlock: 24, group1Blocks: 4, group1Words: 12, group2Blocks: 4, group2Words: 13},
	},
	10: {
		LevelL: {ecPerBlock: 18, group1Blocks: 2, group1Words: 68, group2Blocks: 2, group2Words: 69},
		LevelM: {ecPerBlock: 26, group1Blocks: 4, group1Words: 43, group2Blocks: 1, group2Words: 44},
		LevelQ: {ecPerBlock: 24, group1Blocks: 6, group1Words: 19, group2Blocks: 2, group2Words: 20},
		LevelH: {ecPerBlock: 28, group1Blocks: 6, group1Words: 15, group2Blocks: 2, group2Words: 16},
	},
	11: {
		LevelL: {ecPerBlock: 20, group1Blocks: 4, group1Words: 81},
		LevelM: {ecPerBlock: 30, group1Blocks: 1, group1Words: 50, group2Blocks: 4, group2Words: 51},
		LevelQ: {ecPerBlock: 28, group1Blocks: 4, group1Words: 22, group2Blocks: 4, group2Words: 23},
		LevelH: {ecPerBlock: 24, group1Blocks: 3, group1Words: 12, group2Blocks: 8, group2Words: 13},
	},
	12: {
		LevelL: {ecPerBlock: 24, group1Blocks: 2, group1Words: 92, group2Blocks: 2, group2Words: 93},
		LevelM: {ecPerBlock: 22, group1Blocks: 6, group1Words: 36, group2Blocks: 2, group2Words: 37},
		LevelQ: {ecPerBlock: 26, group1Blocks: 4, group1Words: 20, group2Blocks: 6, group2Words: 21},
		LevelH: {ecPerBlock: 28, group1Blocks: 7, group1Words: 14, group2Blocks: 4, group2Words: 15},
	},
	13: {
		LevelL: {ecPerBlock: 26, group1Blocks: 4, group1Words: 107},
		LevelM: {ecPerBlock: 22, group1Blocks: 8, group1Words: 37, group2Blocks: 1, group2Words: 38},
		LevelQ: {ecPerBlock: 24, group1Blocks: 8, group1Words: 20, group2Blocks: 4, group2Words: 21},
		LevelH: {ecPerBlock: 22, group1Blocks: 12, group1Words: 11, group2Blocks: 4, group2Words: 12},
	},
	14: {
		LevelL: {ecPerBlock: 30, group1Blocks: 3, group1Words: 115, group2Blocks: 1, group2Words: 116},
		LevelM: {ecPerBlock: 24, group1Blocks: 4, group1Words: 40, group2Blocks: 5, group2Words: 41},
		LevelQ: {ecPerBlock: 20, group1Blocks: 11, group1Words: 16, group2Blocks: 5, group2Words: 17},
		LevelH: {ecPerBlock: 24, group1Blocks: 11, group1Words: 12, group2Blocks: 5, group2Words: 13},
	},
	15: {
		LevelL: {ecPerBlock: 22, group1Blocks: 5, group1Words: 87, group2Blocks: 1, group2Words: 88},
		LevelM: {ecPerBlock: 24, group1Blocks: 5, group1Words: 41, group2Blocks: 5, group2Words: 42},
		LevelQ: {ecPerBlock: 30, group1Blocks: 5, group1Words: 24, group2Blocks: 7, group2Words: 25},
		LevelH: {ecPerBlock: 24, group1Blocks: 11, group1Words: 12, group2Blocks: 7, group2Words: 13},
	},
	16: {
		LevelL: {ecPerBlock: 24, group1Blocks: 5, group1Words: 98, group2Blocks: 1, group2Words: 99},
		LevelM: {ecPerBlock: 28, group1Blocks: 7, group1Words: 45, group2Blocks: 3, group2Words: 46},
		LevelQ: {ecPerBlock: 24, group1Blocks: 15, group1Words: 19, group2Blocks: 2, group2Words: 20},
		LevelH: {ecPerBlock: 30, group1Blocks: 3, group1Words: 15, group2Blocks: 13, group2Words: 16},
	},
	17: {
		LevelL: {ecPerBlock: 28, group1Blocks: 1, group1Words: 107, group2Blocks: 5, group2Words: 108},
		LevelM: {ecPerBlock: 28, group1Blocks: 10, group1Words: 46, group2Blocks: 1, group2Words: 47},
		LevelQ: {ecPerBlock: 28, group1Blocks: 1, group1Words: 22, group2Blocks: 15, group2Words: 23},
		LevelH: {ecPerBlock: 28, group1Blocks: 2, group1Words: 14, group2Blocks: 17, group2Words: 15},
	},
	18: {
		LevelL: {ecPerBlock: 30, group1Blocks: 5, group1Words: 120, group2Blocks: 1, group2Words: 121},
		LevelM: {ecPerBlock: 26, group1Blocks: 9, group1Words: 43, group2Blocks: 4, group2Words: 44},
		LevelQ: {ecPerBlock: 28, group1Blocks: 17, group1Words: 22, group2Blocks: 1, group2Words: 23},
		LevelH: {ecPerBlock: 28, group1Blocks: 2, group1Words: 14, group2Blocks: 19, group2Words: 15},
	},
	19: {
		LevelL: {ecPerBlock: 28, group1Blocks: 3, group1Words: 113, group2Blocks: 4, group2Words: 114},
		LevelM: {ecPerBlock: 26, group1Blocks: 3, group1Words: 44, group2Blocks: 11, group2Words: 45},
		LevelQ: {ecPerBlock: 26, group1Blocks: 17, group1Words: 21, group2Blocks: 4, group2Words: 22},
		LevelH: {ecPerBlock: 26, group1Blocks: 9, group1Words: 13, group2Blocks: 16, group2Words: 14},
	},
	20: {
		LevelL: {ecPerBlock: 28, group1Blocks: 3, group1Words: 107, group2Blocks: 5, group2Words: 108},
		LevelM: {ecPerBlock: 26, group1Blocks: 3, group1Words: 41, group2Blocks: 13, group2Words: 42},
		LevelQ: {ecPerBlock: 30, group1Blocks: 15, group1Words: 24, group2Blocks: 5, group2Words: 25},
		LevelH: {ecPerBlock: 28, group1Blocks: 15, group1Words: 15, group2Blocks: 10, group2Words: 16},
	},
	21: {
		LevelL: {ecPerBlock: 28, group1Blocks: 4, group1Words: 116, group2Blocks: 4, group2Words: 117},
		LevelM: {ecPerBlock: 26, group1Blocks: 17, group1Words: 42},
		LevelQ: {ecPerBlock: 28, group1Blocks: 17, group1Words: 22, group2Blocks: 6, group2Words: 23},
		LevelH: {ecPerBlock: 30, group1Blocks: 19, group1Words: 16, group2Blocks: 6, group2Words: 17},
	},
	22: {
		LevelL: {ecPerBlock: 28, group1Blocks: 2, group1Words: 111, group2Blocks: 7, group2Words: 112},
		LevelM: {ecPerBlock: 28, group1Blocks: 17, group1Words: 46},
		LevelQ: {ecPerBlock: 30, group1Blocks: 7, group1Words: 24, group2Blocks: 16, group2Words: 25},
		LevelH: {ecPerBlock: 24, group1Blocks: 34, group1Words: 13},
	},
	23: {
		LevelL: {ecPerBlock: 30, group1Blocks: 4, group1Words: 121, group2Blocks: 5, group2Words: 122},
		LevelM: {ecPerBlock: 28, group1Blocks: 4, group1Words: 47, group2Blocks: 14, group2Words: 48},
		LevelQ: {ecPerBlock: 30, group1Blocks: 11, group1Words: 24, group2Blocks: 14, group2Words: 25},
		LevelH: {ecPerBlock: 30, group1Blocks: 16, group1Words: 15, group2Blocks: 14, group2Words: 16},
	},
	24: {
		LevelL: {ecPerBlock: 30, group1Blocks: 6, group1Words: 117, group2Blocks: 4, group2Words: 118},
		LevelM: {ecPerBlock: 28, group1Blocks: 6, group1Words: 45, group2Blocks: 14, group2Words: 46},
		LevelQ: {ecPerBlock: 30, group1Blocks: 11, group1Words: 24, group2Blocks: 16, group2Words: 25},
		LevelH: {ecPerBlock: 30, group1Blocks: 30, group1Words: 16, group2Blocks: 2, group2Words: 17},
	},
	25: {
		LevelL: {ecPerBlock: 26, group1Blocks: 8, group1Words: 106, group2Blocks: 4, group2Words: 107},
		LevelM: {ecPerBlock: 28, group1Blocks: 8, group1Words: 47, group2Blocks: 13, group2Words: 48},
		LevelQ: {ecPerBlock: 30, group1Blocks: 7, group1Words: 24, group2Blocks: 22, group2Words: 25},
		LevelH: {ecPerBlock: 30, group1Blocks: 22, group1Words: 15, group2Blocks: 13, group2Words: 16},
	},
	26: {
		LevelL: {ecPerBlock: 28, group1Blocks: 10, group1Words: 114, group2Blocks: 2, group2Words: 115},
		LevelM: {ecPerBlock: 28, group1Blocks: 19, group1Words: 46, group2Blocks: 4, group2Words: 47},
		LevelQ: {ecPerBlock: 28, group1Blocks: 28, group1Words: 22, group2Blocks: 6, group2Words: 23},
		LevelH: {ecPerBlock: 30, group1Blocks: 33, group1Words: 16, group2Blocks: 4, group2Words: 17},
	},
	27: {
		LevelL: {ecPerBlock: 30, group1Blocks: 8, group1Words: 122, group2Blocks: 4, group2Words: 123},
		LevelM: {ecPerBlock: 28, group1Blocks: 22, group1Words: 45, group2Blocks: 3, group2Words: 46},
		LevelQ: {ecPerBlock: 30, group1Blocks: 8, group1Words: 23, group2Blocks: 26, group2Words: 24},
		LevelH: {ecPerBlock: 30, group1Blocks: 12, group1Words: 15, group2Blocks: 28, group2Words: 16},
	},
	28: {
		LevelL: {ecPerBlock: 30, group1Blocks: 3, group1Words: 117, group2Blocks: 10, group2Words: 118},
		LevelM: {ecPerBlock: 28, group1Blocks: 3, group1Words: 45, group2Blocks: 23, group2Words: 46},
		LevelQ: {ecPerBlock: 30, group1Blocks: 4, group1Words: 24, group2Blocks: 31, group2Words: 25},
		LevelH: {ecPerBlock: 30, group1Blocks: 11, group1Words: 15, group2Blocks: 31, group2Words: 16},
	},
	29: {
		LevelL: {ecPerBlock: 30, group1Blocks: 7, group1Words: 116, group2Blocks: 7, group2Words: 117},
		LevelM: {ecPerBlock: 28, group1Blocks: 21, group1Words: 45, group2Blocks: 7, group2Words: 46},
		LevelQ: {ecPerBlock: 30, group1Blocks: 1, group1Words: 23, group2Blocks: 37, group2Words: 24},
		LevelH: {ecPerBlock: 30, group1Blocks: 19, group1Words: 15, group2Blocks: 26, group2Words: 16},
	},
	30: {
		LevelL: {ecPerBlock: 30, group1Blocks: 5, group1Words: 115, group2Blocks: 10, group2Words: 116},
		LevelM: {ecPerBlock: 28, group1Blocks: 19, group1Words: 47, group2Blocks: 10, group2Words: 48},
		LevelQ: {ecPerBlock: 30, group1Blocks: 15, group1Words: 24, group2Blocks: 25, group2Words: 25},
		LevelH: {ecPerBlock: 30, group1Blocks: 23, group1Words: 15, group2Blocks: 25, group2Words: 16},
	},
	31: {
		LevelL: {ecPerBlock: 30, group1Blocks: 13, group1Words: 115, group2Blocks: 3, group2Words: 116},
		LevelM: {ecPerBlock: 28, group1Blocks: 2, group1Words: 46, group2Blocks: 29, group2Words: 47},
		LevelQ: {ecPerBlock: 30, group1Blocks: 42, group1Words: 24, group2Blocks: 1, group2Words: 25},
		LevelH: {ecPerBlock: 30, group1Blocks: 23, group1Words: 15, group2Blocks: 28, group2Words: 16},
	},
	32: {
		LevelL: {ecPerBlock: 30, group1Blocks: 17, group1Words: 115},
		LevelM: {ecPerBlock: 28, group1Blocks: 10, group1Words: 46, group2Blocks: 23, group2Words: 47},
		LevelQ: {ecPerBlock: 30, group1Blocks: 10, group1Words: 24, group2Blocks: 35, group2Words: 25},
		LevelH: {ecPerBlock: 30, group1Blocks: 19, group1Words: 15, group2Blocks: 35, group2Words: 16},
	},
	33: {
		LevelL: {ecPerBlock: 30, group1Blocks: 17, group1Words: 115, group2Blocks: 1, group2Words: 116},
		LevelM: {ecPerBlock: 28, group1Blocks: 14, group1Words: 46, group2Blocks: 21, group2Words: 47},
		LevelQ: {ecPerBlock: 30, group1Blocks: 29, group1Words: 24, group2Blocks: 19, group2Words: 25},
		LevelH: {ecPerBlock: 30, group1Blocks: 11, group1Words: 15, group2Blocks: 46, group2Words: 16},
	},
	34: {
		LevelL: {ecPerBlock: 30, group1Blocks: 13, group1Words: 115, group2Blocks: 6, group2Words: 116},
		LevelM: {ecPerBlock: 28, group1Blocks: 14, group1Words: 46, group2Blocks: 23, group2Words: 47},
		LevelQ: {ecPerBlock: 30, group1Blocks: 44, group1Words: 24, group2Blocks: 7, group2Words: 25},
		LevelH: {ecPerBlock: 30, group1Blocks: 59, group1Words: 16, group2Blocks: 1, group2Words: 17},
	},
	35: {
		LevelL: {ecPerBlock: 30, group1Blocks: 12, group1Words: 121, group2Blocks: 7, group2Words: 122},
		LevelM: {ecPerBlock: 28, group1Blocks: 12, group1Words: 47, group2Blocks: 26, group2Words: 48},
		LevelQ: {ecPerBlock: 30, group1Blocks: 39, group1Words: 24, group2Blocks: 14, group2Words: 25},
		LevelH: {ecPerBlock: 30, group1Blocks: 22, group1Words: 15, group2Blocks: 41, group2Words: 16},
	},
	36: {
		LevelL: {ecPerBlock: 30, group1Blocks: 6, group1Words: 121, group2Blocks: 14, group2Words: 122},
		LevelM: {ecPerBlock: 28, group1Blocks: 6, group1Words: 47, group2Blocks: 34, group2Words: 48},
		LevelQ: {ecPerBlock: 30, group1Blocks: 46, group1Words: 24, group2Blocks: 10, group2Words: 25},
		LevelH: {ecPerBlock: 30, group1Blocks: 2, group1Words: 15, group2Blocks: 64, group2Words: 16},
	},
	37: {
		LevelL: {ecPerBlock: 30, group1Blocks: 17, group1Words: 122, group2Blocks: 4, group2Words: 123},
		LevelM: {ecPerBlock: 28, group1Blocks: 29, group1Words: 46, group2Blocks: 14, group2Words: 47},
		LevelQ: {ecPerBlock: 30, group1Blocks: 49, group1Words: 24, group2Blocks: 10, group2Words: 25},
		LevelH: {ecPerBlock: 30, group1Blocks: 24, group1Words: 15, group2Blocks: 46, group2Words: 16},
	},
	38: {
		LevelL: {ecPerBlock: 30, group1Blocks: 4, group1Words: 122, group2Blocks: 18, group2Words: 123},
		LevelM: {ecPerBlock: 28, group1Blocks: 13, group1Words: 46, group2Blocks: 32, group2Words: 47},
		LevelQ: {ecPerBlock: 30, group1Blocks: 48, group1Words: 24, group2Blocks: 14, group2Words: 25},
		LevelH: {ecPerBlock: 30, group1Blocks: 42, group1Words: 15, group2Blocks: 32, group2Words: 16},
	},
	39: {
		LevelL: {ecPerBlock: 30, group1Blocks: 20, group1Words: 117, group2Blocks: 4, group2Words: 118},
		LevelM: {ecPerBlock: 28, group1Blocks: 40, group1Words: 47, group2Blocks: 7, group2Words: 48},
		LevelQ: {ecPerBlock: 30, group1Blocks: 43, group1Words: 24, group2Blocks: 22, group2Words: 25},
		LevelH: {ecPerBlock: 30, group1Blocks: 10, group1Words: 15, group2Blocks: 67, group2Words: 16},
	},
	40: {
		LevelL: {ecPerBlock: 30, group1Blocks: 19, group1Words: 118, group2Blocks: 6, group2Words: 119},
		LevelM: {ecPerBlock: 28, group1Blocks: 18, group1Words: 47, group2Blocks: 31, group2Words: 48},
		LevelQ: {ecPerBlock: 30, group1Blocks: 34, group1Words: 24, group2Blocks: 34, group2Words: 25},
		LevelH: {ecPerBlock: 30, group1Blocks: 20, group1Words: 15, group2Blocks: 61, group2Words: 16},
	},
}

// alignmentCentersByVersion[version] lists the alignment-pattern center
// coordinates (ISO/IEC 18004 Annex E). Index 0 is unused; v1 has no alignment
// patterns. The centers are shared row/column positions: every pair (r, c)
// except those overlapping the finder patterns carries an alignment pattern.
var alignmentCentersByVersion = [41][]int{
	1:  {},
	2:  {6, 18},
	3:  {6, 22},
	4:  {6, 26},
	5:  {6, 30},
	6:  {6, 34},
	7:  {6, 22, 38},
	8:  {6, 24, 42},
	9:  {6, 26, 46},
	10: {6, 28, 50},
	11: {6, 30, 54},
	12: {6, 32, 58},
	13: {6, 34, 62},
	14: {6, 26, 46, 66},
	15: {6, 26, 48, 70},
	16: {6, 26, 50, 74},
	17: {6, 30, 54, 78},
	18: {6, 30, 56, 82},
	19: {6, 30, 58, 86},
	20: {6, 34, 62, 90},
	21: {6, 28, 50, 72, 94},
	22: {6, 26, 50, 74, 98},
	23: {6, 30, 54, 78, 102},
	24: {6, 28, 54, 80, 106},
	25: {6, 32, 58, 84, 110},
	26: {6, 30, 58, 86, 114},
	27: {6, 34, 62, 90, 118},
	28: {6, 26, 50, 74, 98, 122},
	29: {6, 30, 54, 78, 102, 126},
	30: {6, 26, 52, 78, 104, 130},
	31: {6, 30, 56, 82, 108, 134},
	32: {6, 34, 60, 86, 112, 138},
	33: {6, 30, 58, 86, 114, 142},
	34: {6, 34, 62, 90, 118, 146},
	35: {6, 30, 54, 78, 102, 126, 150},
	36: {6, 24, 50, 76, 102, 128, 154},
	37: {6, 28, 54, 80, 106, 132, 158},
	38: {6, 32, 58, 84, 110, 136, 162},
	39: {6, 26, 54, 82, 110, 138, 166},
	40: {6, 30, 58, 86, 114, 142, 170},
}

// versionSpec returns the Reed-Solomon block structure (ISO/IEC 18004 Table 9)
// for a version/level.
func versionSpec(version int, level Level) blockSpec {
	return ecBlocks[version][level]
}

// alignmentCenters returns the alignment-pattern center coordinates for a
// version (empty for v1).
func alignmentCenters(version int) []int {
	return alignmentCentersByVersion[version]
}

// charCountBits returns the byte-mode character-count indicator width: 8 bits
// for versions 1-9, 16 bits for versions 10-40.
func charCountBits(version int) int {
	if version <= 9 {
		return 8
	}
	return 16
}

// dataCodewords returns the total number of data codewords for a version/level.
func dataCodewords(version int, level Level) int {
	s := ecBlocks[version][level]
	return s.group1Blocks*s.group1Words + s.group2Blocks*s.group2Words
}

// dataCapacityBytes returns the usable byte-mode payload capacity for a
// version/level as the total data codewords (before mode+count overhead is
// deducted at encode time). Used by pickVersion.
func dataCapacityBytes(version int, level Level) int {
	return dataCodewords(version, level)
}
