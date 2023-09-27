package blake3

import "unsafe"

//go:generate go run avo/gen.go -out blake3_amd64.s

//go:noescape
func compressChunksAVX512(cvs *[16][8]uint32, buf *[16 * ChunkSize]byte, key *[8]uint32, counter uint64, flags uint32)

//go:noescape
func compressChunksAVX2(cvs *[8][8]uint32, buf *[8 * ChunkSize]byte, key *[8]uint32, counter uint64, flags uint32)

//go:noescape
func compressBlocksAVX512(out *[1024]byte, block *[16]uint32, cv *[8]uint32, counter uint64, blockLen uint32, flags uint32)

//go:noescape
func compressBlocksAVX2(out *[512]byte, msgs *[16]uint32, cv *[8]uint32, counter uint64, blockLen uint32, flags uint32)

//go:noescape
func compressParentsAVX2(parents *[8][8]uint32, cvs *[16][8]uint32, key *[8]uint32, flags uint32)

func compressNode(n Node) (out [16]uint32) {
	compressNodeGeneric(&out, n)
	return
}

func compressBufferAVX512(buf *[maxSIMD * ChunkSize]byte, buflen int, key *[8]uint32, counter uint64, flags uint32) Node {
	var cvs [maxSIMD][8]uint32
	compressChunksAVX512(&cvs, buf, key, counter, flags)
	numChunks := uint64(buflen / ChunkSize)
	if buflen%ChunkSize != 0 {
		// use non-asm for remainder
		partialChunk := buf[buflen-buflen%ChunkSize : buflen]
		cvs[numChunks] = ChainingValue(CompressChunk(partialChunk, key, counter+numChunks, flags))
		numChunks++
	}
	return mergeSubtrees(&cvs, numChunks, key, flags)
}

func compressBufferAVX2(buf *[maxSIMD * ChunkSize]byte, buflen int, key *[8]uint32, counter uint64, flags uint32) Node {
	var cvs [maxSIMD][8]uint32
	cvHalves := (*[2][8][8]uint32)(unsafe.Pointer(&cvs))
	bufHalves := (*[2][8 * ChunkSize]byte)(unsafe.Pointer(buf))
	compressChunksAVX2(&cvHalves[0], &bufHalves[0], key, counter, flags)
	numChunks := uint64(buflen / ChunkSize)
	if numChunks > 8 {
		compressChunksAVX2(&cvHalves[1], &bufHalves[1], key, counter+8, flags)
	}
	if buflen%ChunkSize != 0 {
		// use non-asm for remainder
		partialChunk := buf[buflen-buflen%ChunkSize : buflen]
		cvs[numChunks] = ChainingValue(CompressChunk(partialChunk, key, counter+numChunks, flags))
		numChunks++
	}
	return mergeSubtrees(&cvs, numChunks, key, flags)
}

func compressBuffer(buf *[maxSIMD * ChunkSize]byte, buflen int, key *[8]uint32, counter uint64, flags uint32) Node {
	switch {
	case haveAVX512 && buflen >= ChunkSize*2:
		return compressBufferAVX512(buf, buflen, key, counter, flags)
	case haveAVX2 && buflen >= ChunkSize*2:
		return compressBufferAVX2(buf, buflen, key, counter, flags)
	default:
		return compressBufferGeneric(buf, buflen, key, counter, flags)
	}
}

func CompressChunk(chunk []byte, key *[8]uint32, counter uint64, flags uint32) Node {
	n := Node{
		cv:       *key,
		counter:  counter,
		blockLen: blockSize,
		flags:    flags | flagChunkStart,
	}
	blockBytes := (*[64]byte)(unsafe.Pointer(&n.block))[:]
	for len(chunk) > blockSize {
		copy(blockBytes, chunk)
		chunk = chunk[blockSize:]
		n.cv = ChainingValue(n)
		n.flags &^= flagChunkStart
	}
	// pad last block with zeros
	n.block = [16]uint32{}
	copy(blockBytes, chunk)
	n.blockLen = uint32(len(chunk))
	n.flags |= flagChunkEnd
	return n
}

func hashBlock(out *[64]byte, buf []byte) {
	var block [16]uint32
	copy((*[64]byte)(unsafe.Pointer(&block))[:], buf)
	compressNodeGeneric((*[16]uint32)(unsafe.Pointer(out)), Node{
		cv:       Iv,
		block:    block,
		blockLen: uint32(len(buf)),
		flags:    flagChunkStart | flagChunkEnd | FlagRoot,
	})
}

func compressBlocks(out *[maxSIMD * blockSize]byte, n Node) {
	switch {
	case haveAVX512:
		compressBlocksAVX512(out, &n.block, &n.cv, n.counter, n.blockLen, n.flags)
	case haveAVX2:
		outs := (*[2][512]byte)(unsafe.Pointer(out))
		compressBlocksAVX2(&outs[0], &n.block, &n.cv, n.counter, n.blockLen, n.flags)
		compressBlocksAVX2(&outs[1], &n.block, &n.cv, n.counter+8, n.blockLen, n.flags)
	default:
		outs := (*[maxSIMD][64]byte)(unsafe.Pointer(out))
		compressBlocksGeneric(outs, n)
	}
}

func mergeSubtrees(cvs *[maxSIMD][8]uint32, numCVs uint64, key *[8]uint32, flags uint32) Node {
	if !haveAVX2 {
		return mergeSubtreesGeneric(cvs, numCVs, key, flags)
	}
	for numCVs > 2 {
		if numCVs%2 == 0 {
			compressParentsAVX2((*[8][8]uint32)(unsafe.Pointer(cvs)), cvs, key, flags)
		} else {
			keep := cvs[numCVs-1]
			compressParentsAVX2((*[8][8]uint32)(unsafe.Pointer(cvs)), cvs, key, flags)
			cvs[numCVs/2] = keep
			numCVs++
		}
		numCVs /= 2
	}
	return ParentNode(cvs[0], cvs[1], *key, flags)
}

func wordsToBytes(words [16]uint32, block *[64]byte) {
	*block = *(*[64]byte)(unsafe.Pointer(&words))
}

func bytesToCV(b []byte) [8]uint32 {
	return *(*[8]uint32)(unsafe.Pointer(&b[0]))
}

func CvToBytes(cv *[8]uint32) *[32]byte {
	return (*[32]byte)(unsafe.Pointer(cv))
}
