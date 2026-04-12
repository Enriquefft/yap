package audioprep

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// wavHeader holds the PCM format fields extracted from a RIFF WAV file.
// Only the fields needed for audio preprocessing are retained.
type wavHeader struct {
	SampleRate    uint32
	NumChannels   uint16
	BitsPerSample uint16
}

// parseWAV extracts the PCM format header and int16 samples from a RIFF
// WAV byte slice. It walks chunks to find "fmt " and "data", handling
// extra chunks (LIST, JUNK, etc.) between them.
//
// Only AudioFormat=1 (uncompressed PCM) is supported. The caller is
// responsible for ensuring 16-bit sample depth — the returned []int16
// interprets the data section as little-endian signed 16-bit samples
// regardless of BitsPerSample (but the header is preserved so the
// caller can validate).
func parseWAV(data []byte) (wavHeader, []int16, error) {
	if len(data) < 12 {
		return wavHeader{}, nil, errors.New("WAV too short for RIFF header")
	}
	if string(data[0:4]) != "RIFF" {
		return wavHeader{}, nil, errors.New("missing RIFF magic")
	}
	if string(data[8:12]) != "WAVE" {
		return wavHeader{}, nil, errors.New("missing WAVE format identifier")
	}

	var h wavHeader
	var foundFmt, foundData bool
	var samples []int16

	// Walk chunks starting after the 12-byte RIFF/WAVE header.
	pos := 12
	for pos+8 <= len(data) {
		chunkID := string(data[pos : pos+4])
		chunkSize := int(binary.LittleEndian.Uint32(data[pos+4 : pos+8]))
		chunkData := pos + 8

		if chunkData+chunkSize > len(data) {
			// Tolerate a truncated final data chunk — use what we have.
			if chunkID == "data" && foundFmt {
				chunkSize = len(data) - chunkData
			} else {
				return wavHeader{}, nil, fmt.Errorf("chunk %q at offset %d extends beyond file", chunkID, pos)
			}
		}

		switch chunkID {
		case "fmt ":
			if chunkSize < 16 {
				return wavHeader{}, nil, fmt.Errorf("fmt chunk too small: %d bytes", chunkSize)
			}
			audioFormat := binary.LittleEndian.Uint16(data[chunkData : chunkData+2])
			if audioFormat != 1 {
				return wavHeader{}, nil, fmt.Errorf("unsupported audio format %d (only PCM=1)", audioFormat)
			}
			h.NumChannels = binary.LittleEndian.Uint16(data[chunkData+2 : chunkData+4])
			h.SampleRate = binary.LittleEndian.Uint32(data[chunkData+4 : chunkData+8])
			h.BitsPerSample = binary.LittleEndian.Uint16(data[chunkData+14 : chunkData+16])
			if h.BitsPerSample != 16 {
				return wavHeader{}, nil, fmt.Errorf("unsupported bits per sample: %d (only 16-bit PCM)", h.BitsPerSample)
			}
			if h.NumChannels != 1 {
				return wavHeader{}, nil, fmt.Errorf("unsupported channel count: %d (only mono)", h.NumChannels)
			}
			foundFmt = true

		case "data":
			if !foundFmt {
				return wavHeader{}, nil, errors.New("data chunk before fmt chunk")
			}
			sampleCount := chunkSize / 2 // 16-bit samples = 2 bytes each
			samples = make([]int16, sampleCount)
			for i := range sampleCount {
				offset := chunkData + i*2
				samples[i] = int16(binary.LittleEndian.Uint16(data[offset : offset+2]))
			}
			foundData = true
		}

		if foundFmt && foundData {
			break
		}

		// Advance to next chunk. Chunks are word-aligned (padded to even size).
		advance := chunkSize
		if advance%2 != 0 {
			advance++
		}
		pos = chunkData + advance
	}

	if !foundFmt {
		return wavHeader{}, nil, errors.New("no fmt chunk found")
	}
	if !foundData {
		return wavHeader{}, nil, errors.New("no data chunk found")
	}

	return h, samples, nil
}

// buildWAV constructs a minimal RIFF WAV file from a header and int16
// PCM samples. The output is a clean 44-byte header followed by the
// raw sample data — no extra chunks. The header fields (byte rate,
// block align) are computed from the provided format parameters.
func buildWAV(h wavHeader, samples []int16) ([]byte, error) {
	const bytesPerSample = 2 // 16-bit PCM, validated by parseWAV
	dataSize := len(samples) * bytesPerSample
	totalSize := 44 + dataSize

	buf := make([]byte, totalSize)

	// RIFF header
	copy(buf[0:4], "RIFF")
	binary.LittleEndian.PutUint32(buf[4:8], uint32(totalSize-8))
	copy(buf[8:12], "WAVE")

	// fmt subchunk
	copy(buf[12:16], "fmt ")
	binary.LittleEndian.PutUint32(buf[16:20], 16) // PCM subchunk size
	binary.LittleEndian.PutUint16(buf[20:22], 1)  // AudioFormat = PCM
	binary.LittleEndian.PutUint16(buf[22:24], h.NumChannels)
	binary.LittleEndian.PutUint32(buf[24:28], h.SampleRate)
	byteRate := h.SampleRate * uint32(h.NumChannels) * uint32(h.BitsPerSample) / 8
	binary.LittleEndian.PutUint32(buf[28:32], byteRate)
	blockAlign := h.NumChannels * h.BitsPerSample / 8
	binary.LittleEndian.PutUint16(buf[32:34], blockAlign)
	binary.LittleEndian.PutUint16(buf[34:36], h.BitsPerSample)

	// data subchunk
	copy(buf[36:40], "data")
	binary.LittleEndian.PutUint32(buf[40:44], uint32(dataSize))
	for i, s := range samples {
		binary.LittleEndian.PutUint16(buf[44+i*2:], uint16(s))
	}

	return buf, nil
}
