package main

import (
	"bufio"
	"io"
	"net/http"
	"sync"

	"go.uber.org/zap"
)

var (
	clientsMu     sync.Mutex
	clients       = make(map[chan []byte]struct{})
	sourceMu      sync.Mutex
	sourceReady   = sync.NewCond(&sourceMu)
	sourceRunning bool
)

func corsMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Allow all origins
		w.Header().Set("Access-Control-Allow-Origin", "*")

		// Allow specific HTTP methods
		w.Header().Set("Access-Control-Allow-Methods", "GET, PUT, OPTIONS")

		// Allow specific headers
		w.Header().Set("Access-Control-Allow-Headers",
			"Content-Type, Authorization, Accept, Origin, X-Requested-With")

		// Handle preflight requests
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		// Call the next handler
		next.ServeHTTP(w, r)
	}
}

func streamHandler(w http.ResponseWriter, r *http.Request, logger *zap.SugaredLogger) {
	logger.Info("Client connected")

	// Set necessary headers for audio streaming
	// w.Header().Add("Content-Type", "audio/mpeg")
	w.Header().Add("Content-Type", "audio/webm")
	w.Header().Add("Transfer-Encoding", "chunked")
	w.Header().Add("Connection", "keep-alive")

	sourceMu.Lock()
	for !sourceRunning {
		sourceReady.Wait()
	}
	sourceMu.Unlock()

	// Create a channel for the client
	clientChan := make(chan []byte, 1024)

	// Register the client
	clientsMu.Lock()
	clients[clientChan] = struct{}{}
	clientsMu.Unlock()
	logger.Infof("Number of clients: %d", len(clients))

	// Unregister the client when done
	defer func() {
		clientsMu.Lock()
		delete(clients, clientChan)
		clientsMu.Unlock()
		close(clientChan)
	}()

	// Stream data to the client
	for data := range clientChan {
		_, err := w.Write(data)
		if err != nil {
			// Client disconnected
			return
		}
		// Flush the data to the client
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}
}

func sourceHandler(w http.ResponseWriter, r *http.Request, logger *zap.SugaredLogger) {
	if r.Method != "PUT" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	user, pass, ok := r.BasicAuth()
	if !ok {
		w.Header().Set("WWW-Authenticate", `Basic realm="Restricted"`)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	logger.Info("Source connected")
	logger.Infof("Source connected: user=%s, pass=%s", user, pass)

	sourceMu.Lock()
	if sourceRunning {
		sourceMu.Unlock()
		http.Error(w, "Source already connected", http.StatusForbidden)
		return
	}
	sourceRunning = true
	sourceReady.Broadcast()
	sourceMu.Unlock()

	// Unset sourceRunning when done
	defer func() {
		sourceMu.Lock()
		sourceRunning = false
		sourceMu.Unlock()
		logger.Info("Source disconnected")
	}()

	// Read data from the source and broadcast to clients
	reader := bufio.NewReader(r.Body)
	buf := make([]byte, 1024)
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			data := make([]byte, n)
			copy(data, buf[:n])

			// Send the data to all connected clients
			clientsMu.Lock()
			// logger.Infof("Sending %d bytes to %d clients", n, len(clients))
			for clientChan := range clients {
				select {
				case clientChan <- data:
				default:
					// If client is not ready to receive data, skip
				}
			}
			clientsMu.Unlock()
		}
		if err != nil {
			if err == io.EOF {
				// Source disconnected
				break
			}
			logger.Errorf("Error reading from source: %v", err)
			break
		}
	}
}

func main() {
	zap, _ := zap.NewProduction()
	defer zap.Sync()
	logger := zap.Sugar().With("module", "server")

	http.HandleFunc("/stream", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		streamHandler(w, r, logger)
	}))
	http.HandleFunc("/source", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		sourceHandler(w, r, logger)
	}))

	logger.Info("Starting streaming server on http://localhost:8001/")
	logger.Fatal(http.ListenAndServe(":8001", nil))
}
