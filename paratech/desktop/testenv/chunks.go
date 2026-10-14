package testenv

import (
	"encoding/binary"
	"math"
)

// Chunk files of the fake recorder carry a header, like a real audio chunk (a WAV header states its data size): the
// magic chunkMagic, the payload length as 4 bytes big endian, then the payload. A file that is shorter or longer than
// its header says was not written to the end, as the last chunk of a capture killed by a crash (PRD section 6.3).
const chunkMagic = "FCH1"

const chunkHeaderLen = 8

// EncodeChunk returns the content of a complete chunk file holding payload, which must be shorter than 4 GiB.
func EncodeChunk(payload []byte) []byte {
	n := len(payload)
	if int64(n) > math.MaxUint32 {
		panic("testenv: chunk payload too large")
	}
	out := make([]byte, chunkHeaderLen, chunkHeaderLen+n)
	copy(out, chunkMagic)
	binary.BigEndian.PutUint32(out[len(chunkMagic):], uint32(n))
	return append(out, payload...)
}

// ChunkPayload returns the payload of a chunk file; ok is false if the file is not a complete chunk.
func ChunkPayload(content []byte) (payload []byte, ok bool) {
	if len(content) < chunkHeaderLen || string(content[:len(chunkMagic)]) != chunkMagic {
		return nil, false
	}
	n := binary.BigEndian.Uint32(content[len(chunkMagic):chunkHeaderLen])
	if int64(len(content)-chunkHeaderLen) != int64(n) {
		return nil, false
	}
	return content[chunkHeaderLen:], true
}
