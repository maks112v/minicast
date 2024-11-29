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
)

func main() {
	// Define command-line flags
	serverURL := flag.String("url", "http://localhost:8000/source", "URL of the streaming server")
	username := flag.String("user", "sourceuser", "Username for authentication")
	password := flag.String("pass", "sourcepass", "Password for authentication")
	flag.Parse()

	err := streamFromMic(*serverURL, *username, *password)
	if err != nil {
		log.Fatalf("Error streaming from microphone: %v", err)
	}
}

func streamFromMic(serverURL, username, password string) error {
	// Initialize malgo context
	ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, func(message string) {
		log.Printf("malgo message: %s", message)
	})
	if err != nil {
		return fmt.Errorf("could not initialize context: %v", err)
	}
	defer func() {
		_ = ctx.Uninit()
		ctx.Free()
	}()

	// Capture device configuration
	deviceConfig := malgo.DefaultDeviceConfig(malgo.Capture)
	deviceConfig.Capture.Format = malgo.FormatS16
	deviceConfig.Capture.Channels = 1
	deviceConfig.SampleRate = 44100
	deviceConfig.Alsa.NoMMap = 1 // For Linux ALSA

	// Create a pipe to connect the audio input and HTTP request body
	reader, writer := io.Pipe()

	// Start the HTTP request in a goroutine
	go func() {
		client := &http.Client{}
		req, err := http.NewRequest("PUT", serverURL, reader)
		if err != nil {
			log.Fatalf("could not create request: %v", err)
		}
		req.SetBasicAuth(username, password)
		req.Header.Set("Content-Type", "audio/pcm") // Using raw PCM data

		resp, err := client.Do(req)
		if err != nil {
			log.Fatalf("could not perform request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			bodyBytes, _ := io.ReadAll(resp.Body)
			log.Fatalf("server responded with status %s: %s", resp.Status, string(bodyBytes))
		}
	}()

	// Data callback function
	onRecvFrames := func(outputSamples, inputSamples []byte, frameCount uint32) {
		// Write the captured audio data to the writer
		_, err := writer.Write(inputSamples)
		if err != nil {
			log.Printf("error writing to pipe: %v", err)
		}
	}

	deviceCallbacks := malgo.DeviceCallbacks{
		Data: onRecvFrames,
	}

	// Initialize the capture device
	device, err := malgo.InitDevice(ctx.Context, deviceConfig, deviceCallbacks)
	if err != nil {
		return fmt.Errorf("could not initialize capture device: %v", err)
	}
	defer device.Uninit()

	// Start the device
	err = device.Start()
	if err != nil {
		return fmt.Errorf("could not start capture device: %v", err)
	}

	log.Println("Streaming from microphone. Press Ctrl+C to stop.")

	// Handle interrupt signal for graceful shutdown
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	<-sigs

	// Stop the device
	err = device.Stop()
	if err != nil {
		return fmt.Errorf("could not stop capture device: %v", err)
	}

	// Close the writer to signal the end of data
	writer.Close()

	log.Println("Streaming stopped.")

	return nil
}
