package main

import (
	"encoding/binary"
	"log"
	"os"
	"time"

	"github.com/gordonklaus/portaudio"
)

func main() {
	// Initialize PortAudio
	err := portaudio.Initialize()
	if err != nil {
		log.Fatalf("Error initializing PortAudio: %v", err)
	}
	defer portaudio.Terminate()

	// Create an audio input stream
	sampleRate := 44100 // CD-quality audio
	framesPerBuffer := 512
	channels := 1 // Mono audio
	duration := 5 * time.Second

	buffer := make([]int16, framesPerBuffer)
	var audioData []int16

	stream, err := portaudio.OpenDefaultStream(channels, 0, float64(sampleRate), len(buffer), func(in []int16) {
		audioData = append(audioData, in...)
	})
	if err != nil {
		log.Fatalf("Error opening audio stream: %v", err)
	}

	// Start the stream
	err = stream.Start()
	if err != nil {
		log.Fatalf("Error starting stream: %v", err)
	}

	log.Println("Recording for 5 seconds...")
	time.Sleep(duration)

	// Stop and close the stream
	err = stream.Stop()
	if err != nil {
		log.Fatalf("Error stopping stream: %v", err)
	}

	err = stream.Close()
	if err != nil {
		log.Fatalf("Error closing stream: %v", err)
	}

	// Save the recorded audio to a WAV file
	saveToWav("output.wav", audioData, sampleRate, channels)
	log.Println("Recording saved to output.wav")
}

func saveToWav(filename string, data []int16, sampleRate, channels int) {
	file, err := os.Create(filename)
	if err != nil {
		log.Fatalf("Error creating file: %v", err)
	}
	defer file.Close()

	// WAV file header
	file.WriteString("RIFF")
	binary.Write(file, binary.LittleEndian, int32(36+len(data)*2)) // File size
	file.WriteString("WAVE")
	file.WriteString("fmt ")
	binary.Write(file, binary.LittleEndian, int32(16))       // Subchunk1Size
	binary.Write(file, binary.LittleEndian, int16(1))        // AudioFormat
	binary.Write(file, binary.LittleEndian, int16(channels)) // NumChannels
	binary.Write(file, binary.LittleEndian, int32(sampleRate))
	binary.Write(file, binary.LittleEndian, int32(sampleRate*channels*2)) // ByteRate
	binary.Write(file, binary.LittleEndian, int16(channels*2))            // BlockAlign
	binary.Write(file, binary.LittleEndian, int16(16))                    // BitsPerSample
	file.WriteString("data")
	binary.Write(file, binary.LittleEndian, int32(len(data)*2)) // Subchunk2Size

	// Write audio data
	for _, sample := range data {
		binary.Write(file, binary.LittleEndian, sample)
	}
}
