package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/gen2brain/malgo"
	ffmpeg_go "github.com/u2takey/ffmpeg-go"
)

func main() {
	// Set up logging with timestamp and file info
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	// Define command-line flags
	inputFile := flag.String("file", "", "Path to the audio file to stream")
	serverURL := flag.String("url", "http://localhost:8000/source", "URL of the streaming server")
	username := flag.String("user", "sourceuser", "Username for authentication")
	password := flag.String("pass", "sourcepass", "Password for authentication")
	flag.Parse()

	log.Printf("Starting audio streamer...")
	log.Printf("Server URL: %s", *serverURL)
	if *inputFile != "" {
		log.Printf("Streaming from file: %s", *inputFile)
		// Stream from file
		err := streamFromFile(*inputFile, *serverURL, *username, *password)
		if err != nil {
			log.Fatalf("Error streaming from file: %v", err)
		}
	} else {
		log.Printf("Streaming from microphone")
		// Stream from microphone
		err := streamFromMic(*serverURL, *username, *password)
		if err != nil {
			log.Fatalf("Error streaming from microphone: %v", err)
		}
	}
}

func streamFromFile(filePath, serverURL, username, password string) error {
	// Create a pipe to connect ffmpeg output and HTTP request body
	reader, writer := io.Pipe()

	// Start the HTTP request in a goroutine
	go func() {
		client := &http.Client{}
		req, err := http.NewRequest("PUT", serverURL, reader)
		if err != nil {
			log.Fatalf("Could not create request: %v", err)
		}
		req.SetBasicAuth(username, password)
		req.Header.Set("Content-Type", "audio/mpeg") // Set Content-Type to audio/mpeg

		// Send the request
		resp, err := client.Do(req)
		if err != nil {
			log.Fatalf("Could not perform request: %v", err)
		}
		defer func() {
			resp.Body.Close()
			log.Printf("Closed server response")
		}()

		// Check response
		log.Printf("Received server response: %s", resp.Status)
		if resp.StatusCode != http.StatusOK {
			bodyBytes, _ := io.ReadAll(resp.Body)
			log.Fatalf("Server responded with status %s: %s", resp.Status, string(bodyBytes))
		}

		log.Printf("Streaming from file completed successfully")
	}()

	// Use ffmpeg-go to stream the audio file in real-time
	log.Printf("Starting ffmpeg to stream audio file in real-time")
	err := ffmpeg_go.Input(filePath, ffmpeg_go.KwArgs{"re": ""}).
		Output("pipe:", ffmpeg_go.KwArgs{
			"format": "mp3",
			"acodec": "copy",
		}).
		WithOutput(writer).
		Run()
	if err != nil {
		log.Printf("FFmpeg error: %v", err)
		return fmt.Errorf("could not stream audio file: %v", err)
	}

	// Close the writer to signal the end of data
	log.Printf("Closing writer pipe")
	writer.Close()

	return nil
}

func streamFromMic(serverURL, username, password string) error {
	log.Printf("Initializing malgo context")
	// Initialize malgo context
	ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, func(message string) {
		log.Printf("malgo message: %s", message)
	})
	if err != nil {
		log.Printf("Failed to initialize malgo context: %v", err)
		return fmt.Errorf("could not initialize context: %v", err)
	}
	defer func() {
		_ = ctx.Uninit()
		ctx.Free()
		log.Printf("Uninitialized malgo context")
	}()

	// Capture device configuration
	log.Printf("Configuring capture device")
	deviceConfig := malgo.DefaultDeviceConfig(malgo.Capture)
	deviceConfig.Capture.Format = malgo.FormatS16
	deviceConfig.Capture.Channels = 1
	deviceConfig.SampleRate = 44100
	deviceConfig.Alsa.NoMMap = 1 // For Linux ALSA

	// Create a pipe to connect the audio input and HTTP request body
	log.Printf("Creating pipe for audio data")
	reader, writer := io.Pipe()

	// Start the HTTP request in a goroutine
	log.Printf("Starting HTTP request goroutine")
	go func() {
		client := &http.Client{}
		req, err := http.NewRequest("PUT", serverURL, reader)
		if err != nil {
			log.Printf("Failed to create HTTP request: %v", err)
			log.Fatalf("Could not create request: %v", err)
		}
		req.SetBasicAuth(username, password)
		req.Header.Set("Content-Type", "audio/pcm") // Using raw PCM data

		log.Printf("Sending HTTP request to server")
		resp, err := client.Do(req)
		if err != nil {
			log.Printf("HTTP request failed: %v", err)
			log.Fatalf("Could not perform request: %v", err)
		}
		defer func() {
			resp.Body.Close()
			log.Printf("Closed server response")
		}()

		log.Printf("Received server response: %s", resp.Status)
		if resp.StatusCode != http.StatusOK {
			bodyBytes, _ := io.ReadAll(resp.Body)
			log.Printf("Server error response body: %s", string(bodyBytes))
			log.Fatalf("Server responded with status %s: %s", resp.Status, string(bodyBytes))
		}
	}()

	// Data callback function
	log.Printf("Setting up data callback function")
	onRecvFrames := func(outputSamples, inputSamples []byte, frameCount uint32) {
		// Write the captured audio data to the writer
		_, err := writer.Write(inputSamples)
		if err != nil {
			log.Printf("Error writing to pipe: %v", err)
		} else {
			log.Printf("Wrote %d bytes to pipe", len(inputSamples))
		}
	}

	deviceCallbacks := malgo.DeviceCallbacks{
		Data: onRecvFrames,
	}

	// Initialize the capture device
	log.Printf("Initializing capture device")
	device, err := malgo.InitDevice(ctx.Context, deviceConfig, deviceCallbacks)
	if err != nil {
		log.Printf("Failed to initialize capture device: %v", err)
		return fmt.Errorf("could not initialize capture device: %v", err)
	}
	defer func() {
		device.Uninit()
		log.Printf("Uninitialized capture device")
	}()

	// Start the device
	log.Printf("Starting capture device")
	err = device.Start()
	if err != nil {
		log.Printf("Failed to start capture device: %v", err)
		return fmt.Errorf("could not start capture device: %v", err)
	}

	log.Println("Streaming from microphone. Press Ctrl+C to stop.")

	// Handle interrupt signal for graceful shutdown
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigs
	log.Printf("Received signal: %v, shutting down", sig)

	// Stop the device
	log.Printf("Stopping capture device")
	err = device.Stop()
	if err != nil {
		log.Printf("Failed to stop capture device: %v", err)
		return fmt.Errorf("could not stop capture device: %v", err)
	}

	// Close the writer to signal the end of data
	log.Printf("Closing writer pipe")
	writer.Close()

	log.Println("Streaming stopped.")
	return nil
}
