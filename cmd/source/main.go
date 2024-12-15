package main

import (
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/gordonklaus/portaudio"
	ffmpeg_go "github.com/u2takey/ffmpeg-go"
	"go.uber.org/zap"
)

func main() {
	zap, _ := zap.NewProduction()
	defer zap.Sync()
	logger := zap.Sugar().With("module", "source")

	inputFile := flag.String("file", "", "Path to the audio file to stream")
	serverURL := flag.String("url", "http://localhost:8000/source", "URL of the streaming server")
	username := flag.String("user", "sourceuser", "Username for authentication")
	password := flag.String("pass", "sourcepass", "Password for authentication")
	flag.Parse()

	logger.Info("Starting audio streamer...")
	logger.Infof("Server URL: %s", *serverURL)
	if *inputFile != "" {
		logger.Infof("Streaming from file: %s", *inputFile)
		// Stream from file
		err := streamFromFile(*inputFile, *serverURL, *username, *password, logger)
		if err != nil {
			logger.Fatalf("Error streaming from file: %v", err)
		}
	} else {
		logger.Info("Streaming from microphone")
		// Stream from microphone
		err := streamFromMic(*serverURL, *username, *password, logger)
		if err != nil {
			logger.Fatalf("Error streaming from microphone: %v", err)
		}
	}
}

func streamFromFile(filePath, serverURL, username, password string, logger *zap.SugaredLogger) error {
	// Create a pipe to connect ffmpeg output and HTTP request body
	reader, writer := io.Pipe()

	// Start the HTTP request in a goroutine
	go func() {
		client := &http.Client{}
		req, err := http.NewRequest("PUT", serverURL, reader)
		if err != nil {
			logger.Fatalf("Could not create request: %v", err)
		}
		req.SetBasicAuth(username, password)
		req.Header.Set("Content-Type", "audio/mpeg") // Set Content-Type to audio/mpeg

		// Send the request
		resp, err := client.Do(req)
		if err != nil {
			logger.Fatalf("Could not perform request: %v", err)
		}
		defer func() {
			resp.Body.Close()
			logger.Info("Closed server response")
		}()

		// Check response
		logger.Infof("Received server response: %s", resp.Status)
		if resp.StatusCode != http.StatusOK {
			bodyBytes, _ := io.ReadAll(resp.Body)
			logger.Fatalf("Server responded with status %s: %s", resp.Status, string(bodyBytes))
		}

		logger.Info("Streaming from file completed successfully")
	}()

	// Use ffmpeg-go to stream the audio file in real-time
	logger.Info("Starting ffmpeg to stream audio file in real-time")
	err := ffmpeg_go.Input(filePath, ffmpeg_go.KwArgs{"re": ""}).
		Output("pipe:", ffmpeg_go.KwArgs{
			"format": "mp3",
			"acodec": "copy",
		}).
		WithOutput(writer).
		Run()
	if err != nil {
		logger.Errorf("FFmpeg error: %v", err)
		return fmt.Errorf("could not stream audio file: %v", err)
	}

	// Close the writer to signal the end of data
	logger.Info("Closing writer pipe")
	writer.Close()

	return nil
}

func streamFromMic(serverURL, username, password string, logger *zap.SugaredLogger) error {
	logger.Info("Initializing PortAudio")
	// Initialize PortAudio
	err := portaudio.Initialize()
	if err != nil {
		logger.Errorf("Failed to initialize PortAudio: %v", err)
		return fmt.Errorf("could not initialize PortAudio: %v", err)
	}
	defer func() {
		portaudio.Terminate()
		logger.Info("Terminated PortAudio")
	}()

	// Create a pipe to connect the audio input and HTTP request body
	logger.Info("Creating pipe for audio data")
	reader, writer := io.Pipe()

	// Start the HTTP request in a goroutine
	logger.Info("Starting HTTP request goroutine")
	go func() {
		client := &http.Client{}
		req, err := http.NewRequest("PUT", serverURL, reader)
		if err != nil {
			logger.Errorf("Failed to create HTTP request: %v", err)
			logger.Fatalf("Could not create request: %v", err)
		}
		req.SetBasicAuth(username, password)
		req.Header.Set("Content-Type", "audio/pcm") // Using raw PCM data

		logger.Info("Sending HTTP request to server")
		resp, err := client.Do(req)
		if err != nil {
			logger.Errorf("HTTP request failed: %v", err)
			logger.Fatalf("Could not perform request: %v", err)
		}
		defer func() {
			resp.Body.Close()
			logger.Info("Closed server response")
		}()

		logger.Infof("Received server response: %s", resp.Status)
		if resp.StatusCode != http.StatusOK {
			bodyBytes, _ := io.ReadAll(resp.Body)
			logger.Errorf("Server error response body: %s", string(bodyBytes))
			logger.Fatalf("Server responded with status %s: %s", resp.Status, string(bodyBytes))
		}
	}()

	// List available devices
	devices, err := portaudio.Devices()
	if err != nil {
		logger.Errorf("Failed to get devices: %v", err)
		return fmt.Errorf("could not get devices: %v", err)
	}
	logger.Info("Available devices:")
	for i, device := range devices {
		logger.Infof("Device %d: %s", i, device.Name)
	}

	inputDevice, err := portaudio.DefaultInputDevice()
	if err != nil {
		logger.Errorf("Failed to get default input device: %v", err)
		return fmt.Errorf("could not get default input device: %v", err)
	}

	logger.Infof("Default input device: %+v", inputDevice)
	// Open default input stream
	logger.Info("Opening default input stream")

	stream, err := portaudio.OpenDefaultStream(1, 0, 44100, 1024, func(in []int16) {
		// Convert []int16 to []byte
		buf := make([]byte, len(in)*2)
		for i, v := range in {
			buf[i*2] = byte(v)
			buf[i*2+1] = byte(v >> 8)
		}
		// Write the captured audio data to the writer
		_, err := writer.Write(buf)
		if err != nil {
			logger.Errorf("Error writing to pipe: %v", err)
		} else {
			logger.Infof("Wrote %d bytes to pipe", len(buf))
		}
	})
	if err != nil {
		logger.Errorf("Failed to open default input stream: %v", err)
		return fmt.Errorf("could not open default input stream: %v", err)
	}
	defer func() {
		stream.Close()
		logger.Info("Closed default input stream")
	}()

	// Start the stream
	logger.Info("Starting input stream")
	err = stream.Start()
	if err != nil {
		logger.Errorf("Failed to start input stream: %v", err)
		return fmt.Errorf("could not start input stream: %v", err)
	}

	logger.Info("Streaming from microphone. Press Ctrl+C to stop.")

	// Handle interrupt signal for graceful shutdown
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigs
	logger.Infof("Received signal: %v, shutting down", sig)

	// Stop the stream
	logger.Info("Stopping input stream")
	err = stream.Stop()
	if err != nil {
		logger.Errorf("Failed to stop input stream: %v", err)
		return fmt.Errorf("could not stop input stream: %v", err)
	}

	// Close the writer to signal the end of data
	logger.Info("Closing writer pipe")
	writer.Close()

	logger.Info("Streaming stopped.")
	return nil
}
