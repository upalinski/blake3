//go:build !amd64
// +build !amd64

package blake3

import "encoding/binary"

func compressNode(n Node) (out [16]uint32) {
	compressNodeGeneric(&out, n)
	return
}

func compressBuffer(buf *[maxSIMD * ChunkSize]byte, buflen int, key *[8]uint32, counter uint64, flags uint32) Node {
	return compressBufferGeneric(buf, buflen, key, counter, flags)
}

func CompressChunk(chunk []byte, key *[8]uint32, counter uint64, flags uint32) Node {
	n := Node{
		cv:       *key,
		counter:  counter,
		blockLen: blockSize,
		flags:    flags | flagChunkStart,
	}
	var block [blockSize]byte
	for len(chunk) > blockSize {
		copy(block[:], chunk)
		chunk = chunk[blockSize:]
		bytesToWords(block, &n.block)
		n.cv = ChainingValue(n)
		n.flags &^= flagChunkStart
	}
	// pad last block with zeros
	block = [blockSize]byte{}
	n.blockLen = uint32(len(chunk))
	copy(block[:], chunk)
	bytesToWords(block, &n.block)
	n.flags |= flagChunkEnd
	return n
}

func hashBlock(out *[64]byte, buf []byte) {
	var block [64]byte
	var words [16]uint32
	copy(block[:], buf)
	bytesToWords(block, &words)
	compressNodeGeneric(&words, Node{
		cv:       Iv,
		block:    words,
		blockLen: uint32(len(buf)),
		flags:    flagChunkStart | flagChunkEnd | FlagRoot,
	})
	wordsToBytes(words, out)
}

func compressBlocks(out *[maxSIMD * blockSize]byte, n Node) {
	var outs [maxSIMD][64]byte
	compressBlocksGeneric(&outs, n)
	for i := range outs {
		copy(out[i*64:], outs[i][:])
	}
}

func mergeSubtrees(cvs *[maxSIMD][8]uint32, numCVs uint64, key *[8]uint32, flags uint32) Node {
	return mergeSubtreesGeneric(cvs, numCVs, key, flags)
}

func bytesToWords(bytes [64]byte, words *[16]uint32) {
	for i := range words {
		words[i] = binary.LittleEndian.Uint32(bytes[4*i:])
	}
}

func wordsToBytes(words [16]uint32, block *[64]byte) {
	for i, w := range words {
		binary.LittleEndian.PutUint32(block[4*i:], w)
	}
}

func BytesToCV(b []byte) [8]uint32 {
	var cv [8]uint32
	for i := range cv {
		cv[i] = binary.LittleEndian.Uint32(b[4*i:])
	}
	return cv
}

func CvToBytes(cv *[8]uint32) *[32]byte {
	var b [32]byte
	for i, w := range cv {
		binary.LittleEndian.PutUint32(b[4*i:], w)
	}
	return &b
}
