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
	"github.com/viert/go-lame"
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
	done := make(chan struct{})
	go func() {
		defer close(done)
		client := &http.Client{}
		req, err := http.NewRequest("PUT", serverURL, reader)
		if err != nil {
			logger.Errorf("Failed to create HTTP request: %v", err)
			return
		}
		req.SetBasicAuth(username, password)
		req.Header.Set("Content-Type", "audio/mp3")

		logger.Info("Sending HTTP request to server")
		resp, err := client.Do(req)
		if err != nil {
			logger.Errorf("HTTP request failed: %v", err)
			return
		}
		defer resp.Body.Close()

		logger.Infof("Received server response: %s", resp.Status)
		if resp.StatusCode != http.StatusOK {
			bodyBytes, _ := io.ReadAll(resp.Body)
			logger.Errorf("Server error response body: %s", string(bodyBytes))
			return
		}

		// Wait for streaming to complete
		<-done
	}()

	// Initialize LAME encoder
	enc := lame.NewEncoder(writer)
	defer enc.Close()

	// Configure encoder
	enc.SetQuality(5)
	enc.SetInSamplerate(44100)
	enc.SetNumChannels(1)

	// Open input stream with PCM configuration
	inputChannels := 1
	sampleRate := 44100
	framesPerBuffer := make([]byte, 4096)

	stream, err := portaudio.OpenDefaultStream(
		inputChannels,
		0,
		float64(sampleRate),
		len(framesPerBuffer),
		&framesPerBuffer,
	)
	if err != nil {
		logger.Errorf("Failed to open stream: %v", err)
		return err
	}
	defer stream.Close()

	if err := stream.Start(); err != nil {
		logger.Errorf("Failed to start stream: %v", err)
		return err
	}

	// Handle interrupt signal
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	logger.Info("Starting audio capture. Press Ctrl+C to stop.")

	// Main capture loop
	for {
		select {
		case sig := <-sigs:
			logger.Infof("Received signal: %v, shutting down", sig)
			stream.Stop()
			enc.Close()
			writer.Close()
			return nil
		default:
			err := stream.Read()
			if err != nil {
				logger.Errorf("Error reading from stream: %v", err)
				continue
			}

			// Write PCM data to encoder
			n, err := enc.Write(framesPerBuffer)
			if err != nil {
				logger.Errorf("Error encoding audio: %v", err)
				continue
			}
			logger.Debugf("Encoded %d bytes", n)
		}
	}
}
