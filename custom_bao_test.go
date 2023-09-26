package blake3

import (
	"bytes"
	"encoding/hex"
	"io"
	"math"
	"os"
	"testing"

	"github.com/stretchr/testify/suite"
)

// TODO play with parameters. what if unevenly dividable by chunk size? not multiple of 2?
const sliceSize = chunkSize * 16        // 16 KB
const pieceSize = chunkSize * 1024 * 64 // 64 MB

// hash returned by official b3sum utility https://github.com/BLAKE3-team/BLAKE3#the-b3sum-utility
const fileRootHash = "55c6dac98fbc9a388f619f5f4ffc4c9fdd3eb37eab48afd68b65da90ef3070b1"

// parent node hashes that are 64 MB in length and belongs to the file root hash above
var pieceRootHashes = []string{
	"e7f42ec0b869d53c809a48717fefcc1666c9606130f8d4432f014a2931b89a5e",
	"ac2634b48e5ecdfea2d9ba02d8e55590afc0645aeb3e4656d45a3b1b912f901a",
	"331bfc34e4187d00c0ada7536c24562c758ecec630edb48e36900aeb8fcfda94",
	"e00e9d954cec8a5f640b0bcc43d5d3321bafe980770971ba12292924446a553e",
	"87403b7895e3af55afdd30f842da6b9109a0d44c102e0615249b59d3b77c988b",
	"9a76ac37c0c9e29a4f613e94449a3c8048c6ee7acf26254337c1de0d9e21ee7e",
	"d603d6b13beb499f103fa2b739ef0839fab06c6d6d5cee1f7879b166ef58066d",
	"0efc66b7b1034d0ef89971b018f522988ed5371304584d6aef665f0d35f26ba7",
	"2f820b83c6f8a04d9af49ec4f048c22c536574f53f8f096996f114fe2ca1ddd9",
	"c9f8544e9ff5333781ce660c58bb317f7ebed84c7afcc43bd7bc02ff78f0f87b",
	"2cdadb53165c9d13671a302fc0a08b1f421432e37bd95dd9b2c041843187fa4e",
	"7ce83475f4c328c7e6e6adc37d8bdb037be1da319206b545e4da0f23c9352084",
	"1776728ccbbd909885339fb29232179f8707e8eb36027b56722bc3b97668b794",
	"bb63308ae385464b45eedf7f8d2a4310f33d7d20259c00e0f6652965dd7d821a",
	"03362f9fcbd991b6d9a5fc513c0e1f204090193e307bd1a43067e5369ce004c7",
}

type HashTreeSuite struct {
	suite.Suite
	data *bytes.Reader
	//testSubject    *Storage
}

func TestHashTreeSuite(t *testing.T) {
	suite.Run(t, &HashTreeSuite{})
}

func (s *HashTreeSuite) SetupSuite() {
	dataLen := 1000000000

	// Write 1 GB
	data := make([]byte, dataLen)
	for i := 0; i < len(data); i++ {
		data[i] = 0
	}

	s.data = bytes.NewReader(data)
}

func (s *HashTreeSuite) BeforeTest(string, string) {
	// read data from the start
	_, err := s.data.Seek(0, 0)
	s.NoError(err)
}

func (s *HashTreeSuite) TestFileRootHashIsEqualToPureBlake3() {
	blake3Hasher := New(32, nil)
	copyBuf := make([]byte, 1024*1024)

	if _, err := io.CopyBuffer(blake3Hasher, s.data, copyBuf); err != nil {
		panic(err)
	}
	hash := blake3Hasher.Sum(nil)

	s.Equal(fileRootHash, hex.EncodeToString(hash[:]))
}

func (s *HashTreeSuite) TestHashTreeOriginalBaoSize() {
	// original bao
	outboard := NewOutboard("/tmp/data.outboard")
	defer outboard.CloseOrPanic()
	hash, err := BaoEncode(outboard, s.data, int64(s.data.Len()), true)
	s.NoError(err)
	s.Equal(fileRootHash, hex.EncodeToString(hash[:]))

	// ~ 60 MB
	s.Equal(int64(62499976), outboard.Size())
}

func (s *HashTreeSuite) TestHashTreeCompression16KbBlock() {
	// block size 16 KB
	outboard := NewOutboard("/tmp/data.outboard")
	defer outboard.CloseOrPanic()
	hash, err := CustomBaoEncodeFile(outboard, s.data, int64(s.data.Len()), chunkSize*16, true)
	s.NoError(err)
	s.Equal(fileRootHash, hex.EncodeToString(hash[:]))

	// ~3.7 MB
	s.Equal(int64(3906248), outboard.Size())
}

func (s *HashTreeSuite) TestHashTreeCompression128KbBlock() {
	// block size 128 KB
	outboard := NewOutboard("/tmp/data.outboard")
	defer outboard.CloseOrPanic()

	hash, err := CustomBaoEncodeFile(outboard, s.data, int64(s.data.Len()), chunkSize*128, true)
	s.NoError(err)
	s.Equal(fileRootHash, hex.EncodeToString(hash[:]))

	// ~476 KB
	s.Equal(int64(488264), outboard.Size())
}

func (s *HashTreeSuite) TestPieceHashes() {
	nPieces := (s.data.Len() + pieceSize - 1) / pieceSize

	for i := 0; i < nPieces; i++ {
		pieceOutboard := NewOutboard("/tmp/piece.outboard")
		pieceDataLen := math.Min(float64(s.data.Len()-pieceSize*i), pieceSize)
		pieceData := make([]byte, int(pieceDataLen))
		_, err := s.data.ReadAt(pieceData, int64(pieceSize*i))
		s.NoError(err)

		pieceHash, err := CustomBaoEncodePiece(pieceOutboard, bytes.NewReader(pieceData), int64(len(pieceData)), sliceSize, true, pieceSize, uint64(i))
		s.NoError(err)

		s.Equal(pieceRootHashes[i], hex.EncodeToString(pieceHash[:]))

		pieceOutboard.CloseOrPanic()
	}
}

func (s *HashTreeSuite) TestSecondLayerTree() {
	hash, err := CustomBaoEncodeSecondLayerHashTree(pieceRootHashes)
	s.NoError(err)

	s.Equal(fileRootHash, hex.EncodeToString(hash[:]))
}

type Outboard struct {
	*os.File
}

func NewOutboard(name string) *Outboard {
	file, err := os.Create(name)
	if err != nil {
		panic(err)
	}

	return &Outboard{file}
}

func (o *Outboard) Size() int64 {
	fileInfo, err := o.Stat()
	if err != nil {
		panic(err)
	}

	return fileInfo.Size()
}

func (o *Outboard) CloseOrPanic() {
	err := o.Close()
	if err != nil {
		panic(err)
	}
}

// TODO file counters. use partNumber and pass it as a parameter.
