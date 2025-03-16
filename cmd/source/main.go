package main

import (
	"flag"
	"net/url"
	"os"
	"os/signal"
	"time"

	"github.com/gordonklaus/portaudio"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

const (
	sampleRate  = 44100
	numChannels = 2
	bufferSize  = 4096
)

func main() {
	// Parse command line flags
	addr := flag.String("addr", "localhost:8001", "server address")
	flag.Parse()

	// Initialize logger
	logger, _ := zap.NewProduction()
	defer logger.Sync()
	sugar := logger.Sugar()

	// Initialize PortAudio
	err := portaudio.Initialize()
	if err != nil {
		sugar.Fatalf("Failed to initialize PortAudio: %v", err)
	}
	defer portaudio.Terminate()

	// Open default input stream
	inputStream, err := portaudio.OpenDefaultStream(
		numChannels, // input channels
		0,           // output channels
		float64(sampleRate),
		bufferSize, // frames per buffer
		make([]float32, bufferSize*numChannels),
	)
	if err != nil {
		sugar.Fatalf("Failed to open input stream: %v", err)
	}
	defer inputStream.Close()

	err = inputStream.Start()
	if err != nil {
		sugar.Fatalf("Failed to start input stream: %v", err)
	}

	// Connect to WebSocket server
	u := url.URL{Scheme: "ws", Host: *addr, Path: "/ws", RawQuery: "source=true"}
	sugar.Infof("Connecting to %s", u.String())

	c, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		sugar.Fatalf("Failed to connect to WebSocket server: %v", err)
	}
	defer c.Close()

	// Handle interrupt signal
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)

	// Start streaming
	sugar.Info("Started streaming. Press Ctrl+C to stop.")

	audioBuffer := make([]float32, bufferSize*numChannels)
	done := make(chan struct{})

	go func() {
		defer close(done)
		for {
			err := inputStream.Read()
			if err != nil {
				sugar.Errorf("Failed to read from input stream: %v", err)
				return
			}

			// Convert float32 samples to bytes (16-bit PCM)
			pcmData := make([]byte, len(audioBuffer)*2)
			for i, sample := range audioBuffer {
				// Convert float32 [-1,1] to int16 and then to bytes
				pcmSample := int16(sample * 32767)
				pcmData[i*2] = byte(pcmSample)
				pcmData[i*2+1] = byte(pcmSample >> 8)
			}

			err = c.WriteMessage(websocket.BinaryMessage, pcmData)
			if err != nil {
				sugar.Errorf("Failed to write to WebSocket: %v", err)
				return
			}

			// Sleep for approximately the buffer duration (93ms for 4096 samples at 44.1kHz)
			time.Sleep(93 * time.Millisecond)
		}
	}()

	for {
		select {
		case <-done:
			return
		case <-interrupt:
			sugar.Info("Interrupt received, stopping...")
			err := c.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			if err != nil {
				sugar.Errorf("Failed to write close message: %v", err)
			}
			select {
			case <-done:
			case <-time.After(time.Second):
			}
			return
		}
	}
}
