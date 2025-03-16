package audio

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

// Processor handles audio data processing
type Processor struct {
	sampleRate  int
	numChannels int
	bitDepth    int
}

// NewProcessor creates a new audio processor
func NewProcessor(sampleRate, numChannels, bitDepth int) *Processor {
	return &Processor{
		sampleRate:  sampleRate,
		numChannels: numChannels,
		bitDepth:    bitDepth,
	}
}

// ProcessRawPCM processes raw PCM audio data and returns it in a format suitable for web audio
func (p *Processor) ProcessRawPCM(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty audio data")
	}

	// Create WAV header
	header := new(bytes.Buffer)

	// RIFF header
	header.WriteString("RIFF")
	binary.Write(header, binary.LittleEndian, uint32(len(data)+36)) // File size - 8
	header.WriteString("WAVE")

	// Format chunk
	header.WriteString("fmt ")
	binary.Write(header, binary.LittleEndian, uint32(16)) // Chunk size
	binary.Write(header, binary.LittleEndian, uint16(1))  // Audio format (PCM)
	binary.Write(header, binary.LittleEndian, uint16(p.numChannels))
	binary.Write(header, binary.LittleEndian, uint32(p.sampleRate))
	binary.Write(header, binary.LittleEndian, uint32(p.sampleRate*p.numChannels*p.bitDepth/8)) // Byte rate
	binary.Write(header, binary.LittleEndian, uint16(p.numChannels*p.bitDepth/8))              // Block align
	binary.Write(header, binary.LittleEndian, uint16(p.bitDepth))                              // Bits per sample

	// Data chunk
	header.WriteString("data")
	binary.Write(header, binary.LittleEndian, uint32(len(data)))

	// Combine header and data
	output := make([]byte, 0, header.Len()+len(data))
	output = append(output, header.Bytes()...)
	output = append(output, data...)

	return output, nil
}

// GetSampleRate returns the sample rate
func (p *Processor) GetSampleRate() int {
	return p.sampleRate
}

// GetNumChannels returns the number of channels
func (p *Processor) GetNumChannels() int {
	return p.numChannels
}

// GetBitDepth returns the bit depth
func (p *Processor) GetBitDepth() int {
	return p.bitDepth
}
