package blake3

import (
	"encoding/binary"
	"encoding/hex"
	"io"
	"math/bits"
)

func CustomBaoEncodeFile(dst io.WriterAt, data io.Reader, dataLen int64, blockSize uint64, outboard bool) ([32]byte, error) {
	return CustomBaoEncode(dst, data, dataLen, blockSize, outboard, true, 0, 0)
}

func CustomBaoEncodePiece(dst io.WriterAt, data io.Reader, dataLen int64, blockSize uint64, outboard bool, pieceSize uint64, pieceIndex uint64) ([32]byte, error) {
	return CustomBaoEncode(dst, data, dataLen, blockSize, outboard, false, pieceSize, pieceIndex)
}

func CustomBaoEncode(dst io.WriterAt, data io.Reader, dataLen int64, blockSize uint64, outboard bool, isRoot bool, pieceSize uint64, pieceIndex uint64) ([32]byte, error) {
	// to preserve the original file position in independent raw pieces
	counter := pieceSize / blockSize * pieceIndex
	chunkBuf := make([]byte, blockSize)
	var err error
	read := func(p []byte) []byte {
		if err == nil {
			_, err = io.ReadFull(data, p)
		}
		if err != nil {
			panic(err)
		}
		return p
	}
	write := func(p []byte, off uint64) {
		if err == nil {
			_, err = dst.WriteAt(p, int64(off))
		}
		if err != nil {
			panic(err)
		}
	}

	// NOTE: unlike the reference implementation, we write directly in
	// pre-order, rather than writing in post-order and then flipping. This cuts
	// the I/O required in half, but also makes hashing multiple chunks in SIMD
	// a lot trickier. I'll save that optimization for a rainy day.
	var rec func(bufLen uint64, flags uint32, off uint64) (uint64, [8]uint32)
	rec = func(bufLen uint64, flags uint32, off uint64) (uint64, [8]uint32) {
		if err != nil {
			return 0, [8]uint32{}
		} else if bufLen <= blockSize {
			cv := groupChainingValue(blockSize, read(chunkBuf[:bufLen]), counter, flags)
			counter++
			if !outboard {
				write(chunkBuf[:bufLen], off)
			}
			return 0, cv
		}
		mid := uint64(1) << (bits.Len64(bufLen-1) - 1)
		lchildren, l := rec(mid, 0, off+64)
		llen := lchildren * 32
		if !outboard {
			llen += (mid / blockSize) * blockSize
		}
		rchildren, r := rec(bufLen-mid, 0, off+64+llen)
		write(cvToBytes(&l)[:], off)
		write(cvToBytes(&r)[:], off+32)

		return 2 + lchildren + rchildren, chainingValue(parentNode(l, r, iv, flags))
	}

	binary.LittleEndian.PutUint64(chunkBuf[:8], uint64(dataLen))
	write(chunkBuf[:8], 0)

	// isRoot flag allows to distinguish between first and second layer trees
	var flags uint32
	if isRoot {
		flags = flagRoot
	} else {
		flags = 0
	}

	_, root := rec(uint64(dataLen), flags, 8)
	return *cvToBytes(&root), err
}

func CustomBaoEncodeSecondLayerHashTree(hashes []string) ([32]byte, error) {

	hashQueue := make([][8]uint32, len(hashes))
	for i, hash := range hashes {
		h, err := hex.DecodeString(hash)
		if err != nil {
			panic(err)
		}
		hashQueue[i] = bytesToCV(h)
	}

	// first part of dirty hack to handle uneven number of leafs in the same way as bao
	if len(hashes)%2 == 1 {
		hashQueue = enqueue(hashQueue, hashQueue[len(hashes)-1])
	}

	var l [8]uint32
	var r [8]uint32
	for len(hashQueue) > 2 {
		l, hashQueue = dequeue(hashQueue)
		r, hashQueue = dequeue(hashQueue)

		// second part of dirty hack
		if hex.EncodeToString(cvToBytes(&l)[:]) == hex.EncodeToString(cvToBytes(&r)[:]) {
			hashQueue = enqueue(hashQueue, l)
		} else {
			parentHash := chainingValue(parentNode(l, r, iv, 0))
			hashQueue = enqueue(hashQueue, parentHash)
		}
	}

	rootNode := parentNode(hashQueue[0], hashQueue[1], iv, flagRoot)
	rootCv := chainingValue(rootNode)
	return *cvToBytes(&rootCv), nil
}

func enqueue(queue [][8]uint32, element [8]uint32) [][8]uint32 {
	queue = append(queue, element) // Simply append to enqueue.
	return queue
}

func dequeue(queue [][8]uint32) ([8]uint32, [][8]uint32) {
	return queue[0], queue[1:] // Slice off the element once it is dequeued.
}

// based on https://github.com/oconnor663/bao/commit/54b9aafdba08def1b702ceff1bc4e1250583a7d9
func groupChainingValue(blockSize uint64, groupBytes []byte, groupIndex uint64, flags uint32) [8]uint32 {
	startingChunkIndex := groupIndex * (blockSize / chunkSize)
	return subtreeChainingValue(groupBytes, startingChunkIndex, flags)
}

func subtreeChainingValue(subtreeBytes []byte, startingChunkIndex uint64, flags uint32) [8]uint32 {
	if len(subtreeBytes) <= chunkSize {
		return chainingValue(compressChunk(subtreeBytes, &iv, startingChunkIndex, flags))
	}
	mid := uint64(1) << (bits.Len64(uint64(len(subtreeBytes)-1)) - 1)

	chunkIndex := startingChunkIndex
	l := subtreeChainingValue(subtreeBytes[:mid], chunkIndex, 0)
	chunkIndex += mid / chunkSize
	r := subtreeChainingValue(subtreeBytes[mid:], chunkIndex, 0)
	return chainingValue(parentNode(l, r, iv, flags))
}
