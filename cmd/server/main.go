package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
)

var (
	// Mutex to protect the clients map
	clientsMu sync.Mutex
	// Map to keep track of connected clients
	clients = make(map[chan []byte]struct{})
	// Mutex and condition variable to handle source connection
	sourceMu      sync.Mutex
	sourceReady   = sync.NewCond(&sourceMu)
	sourceRunning bool
)

// Handler for incoming client connections
func streamHandler(w http.ResponseWriter, r *http.Request) {
	// Wait until the source is connected
	log.Printf("Client connected")
	sourceMu.Lock()
	for !sourceRunning {
		sourceReady.Wait()
	}
	sourceMu.Unlock()

	// Set necessary headers for audio streaming
	w.Header().Set("Content-Type", "audio/mpeg")
	w.Header().Set("Transfer-Encoding", "chunked")

	// Create a channel for the client
	clientChan := make(chan []byte, 1024)

	// Register the client
	clientsMu.Lock()
	clients[clientChan] = struct{}{}
	clientsMu.Unlock()
	log.Printf("Number of clients: %d", len(clients))

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

// Handler for the source (butt)
func sourceHandler(w http.ResponseWriter, r *http.Request) {
	// Check for HTTP PUT method
	if r.Method != "PUT" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Handle basic authentication
	// user, pass, ok := r.BasicAuth()
	// if !ok {
	// 	w.Header().Set("WWW-Authenticate", `Basic realm="Restricted"`)
	// 	http.Error(w, "Unauthorized", http.StatusUnauthorized)
	// 	return
	// }

	// For simplicity, accept any credentials
	log.Printf("Source connected")
	// log.Printf("Source connected: user=%s, pass=%s", user, pass)

	// Ensure only one source is connected at a time
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
		log.Println("Source disconnected")
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
			log.Printf("Error reading from source: %v", err)
			break
		}
	}
}

func main() {
	// Set up the HTTP handlers
	http.HandleFunc("/stream", streamHandler)
	http.HandleFunc("/source", sourceHandler)

	fmt.Println("Starting streaming server on http://localhost:8001/")
	// Start the HTTP server
	log.Fatal(http.ListenAndServe(":8001", nil))
}
