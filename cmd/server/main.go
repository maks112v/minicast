package main

import (
	"bytes"
	"net/http"
	"strings"
	"sync"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

var (
	clientsMu     sync.Mutex
	clients       = make(map[chan []byte]struct{})
	sourceMu      sync.Mutex
	sourceReady   = sync.NewCond(&sourceMu)
	sourceRunning bool
	upgrader      = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true // Allow all origins for development
		},
	}
	// Buffer for audio data
	audioBuffer = &bytes.Buffer{}
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
	logger.Info("Client connected to stream")

	// Set headers for audio streaming
	w.Header().Set("Content-Type", "audio/webm;codecs=opus")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Transfer-Encoding", "chunked")
	w.Header().Set("X-Content-Type-Options", "nosniff")

	// Create a channel for this client
	clientChan := make(chan []byte, 1024)
	ctx := r.Context()

	// Register client
	clientsMu.Lock()
	clients[clientChan] = struct{}{}
	clientsMu.Unlock()
	logger.Infof("Number of clients: %d", len(clients))

	// Clean up when done
	defer func() {
		clientsMu.Lock()
		delete(clients, clientChan)
		clientsMu.Unlock()
		close(clientChan)
		logger.Info("Client disconnected from stream")
	}()

	// Send initial audio data if available
	audioBufferCopy := make([]byte, audioBuffer.Len())
	copy(audioBufferCopy, audioBuffer.Bytes())
	if len(audioBufferCopy) > 0 {
		if err := writeData(w, audioBufferCopy); err != nil {
			logger.Debugf("Error writing initial data: %v", err)
			return
		}
	}

	// Stream new data as it comes in
	for {
		select {
		case <-ctx.Done():
			logger.Info("Client connection closed by context")
			return
		case data, ok := <-clientChan:
			if !ok {
				return
			}
			if err := writeData(w, data); err != nil {
				if isConnectionClosed(err) {
					logger.Debug("Client connection closed")
				} else {
					logger.Debugf("Error writing data: %v", err)
				}
				return
			}
		}
	}
}

// writeData writes data to the response writer and flushes it
func writeData(w http.ResponseWriter, data []byte) error {
	if _, err := w.Write(data); err != nil {
		return err
	}
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	return nil
}

// isConnectionClosed checks if the error is due to a closed connection
func isConnectionClosed(err error) bool {
	if err == nil {
		return false
	}
	str := err.Error()
	return strings.Contains(str, "broken pipe") ||
		strings.Contains(str, "connection reset by peer") ||
		strings.Contains(str, "client disconnected") ||
		strings.Contains(str, "i/o timeout")
}

func wsHandler(w http.ResponseWriter, r *http.Request, logger *zap.SugaredLogger) {
	logger.Info("WebSocket client attempting to connect")

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.Errorf("Failed to upgrade connection: %v", err)
		return
	}
	defer conn.Close()

	logger.Info("WebSocket client connected")

	sourceMu.Lock()
	sourceRunning = true
	sourceReady.Broadcast()
	sourceMu.Unlock()

	defer func() {
		sourceMu.Lock()
		sourceRunning = false
		sourceMu.Unlock()
		logger.Info("WebSocket client disconnected")
	}()

	for {
		messageType, data, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				logger.Errorf("WebSocket error: %v", err)
			}
			break
		}

		if messageType == websocket.BinaryMessage {
			// Store audio data in buffer
			audioBuffer.Write(data)
			if audioBuffer.Len() > 1024*1024 { // Keep last 1MB of audio
				audioBuffer.Reset()
			}

			// Broadcast to clients
			clientsMu.Lock()
			for clientChan := range clients {
				select {
				case clientChan <- data:
				default:
					// Skip if client buffer is full
				}
			}
			clientsMu.Unlock()
		}
	}
}

func serveStreamPage(w http.ResponseWriter, r *http.Request) {
	html := `<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0, maximum-scale=1.0, user-scalable=no">
    <title>MiniCast Player</title>
    <style>
        :root {
            --primary-color: #007bff;
            --background-color: #f8f9fa;
            --text-color: #333;
            --error-color: #dc3545;
            --success-color: #28a745;
        }

        * {
            margin: 0;
            padding: 0;
            box-sizing: border-box;
        }

        body {
            font-family: -apple-system, system-ui, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif;
            line-height: 1.6;
            color: var(--text-color);
            background-color: var(--background-color);
            -webkit-font-smoothing: antialiased;
            -moz-osx-font-smoothing: grayscale;
            touch-action: manipulation;
            padding: 16px;
            min-height: 100vh;
            display: flex;
            flex-direction: column;
        }

        .container {
            max-width: 600px;
            margin: 0 auto;
            width: 100%;
            background: white;
            border-radius: 12px;
            padding: 24px;
            box-shadow: 0 2px 8px rgba(0,0,0,0.1);
        }

        h1 {
            font-size: 24px;
            font-weight: 600;
            margin-bottom: 16px;
            text-align: center;
        }

        .player-wrapper {
            background: var(--background-color);
            border-radius: 8px;
            padding: 16px;
            margin: 16px 0;
        }

        audio {
            width: 100%;
            margin: 0;
            border-radius: 8px;
            height: 40px;
        }

        /* Improve audio controls on iOS */
        audio::-webkit-media-controls-panel {
            background-color: var(--background-color);
        }

        audio::-webkit-media-controls-play-button {
            background-color: var(--primary-color);
            border-radius: 50%;
        }

        audio::-webkit-media-controls-timeline {
            border-radius: 4px;
        }

        .status {
            margin-top: 16px;
            padding: 12px;
            border-radius: 8px;
            background: var(--background-color);
            font-size: 14px;
            text-align: center;
        }

        .status code {
            background: rgba(0,0,0,0.05);
            padding: 2px 6px;
            border-radius: 4px;
            font-family: monospace;
        }

        .error {
            background-color: #fff3f3;
            color: var(--error-color);
            display: none;
            padding: 12px;
            border-radius: 8px;
            margin-top: 16px;
            text-align: center;
        }

        .reconnecting {
            display: none;
            text-align: center;
            margin-top: 16px;
            color: var(--primary-color);
        }

        @media (max-width: 480px) {
            body {
                padding: 12px;
            }

            .container {
                padding: 16px;
            }

            h1 {
                font-size: 20px;
            }

            .player-wrapper {
                padding: 12px;
            }

            audio {
                height: 36px;
            }
        }

        /* Dark mode support */
        @media (prefers-color-scheme: dark) {
            :root {
                --background-color: #1a1a1a;
                --text-color: #fff;
            }

            body {
                background-color: #000;
            }

            .container {
                background: #2d2d2d;
            }

            .status code {
                background: rgba(255,255,255,0.1);
            }
        }
    </style>
</head>
<body>
    <div class="container">
        <h1>MiniCast Player</h1>
        <div class="player-wrapper">
            <audio id="audioPlayer" controls autoplay playsinline>
                <source src="/stream" type="audio/webm;codecs=opus">
                Your browser does not support the audio element.
            </audio>
        </div>
        <div class="status">
            Stream URL: <code>/stream</code>
        </div>
        <div id="error" class="error">
            Connection lost. Attempting to reconnect...
        </div>
        <div id="reconnecting" class="reconnecting">
            Reconnecting...
        </div>
    </div>
    <script>
        const audio = document.getElementById('audioPlayer');
        const errorDiv = document.getElementById('error');
        const reconnectingDiv = document.getElementById('reconnecting');
        let reconnectAttempts = 0;
        const maxReconnectAttempts = 5;
        
        function showError() {
            errorDiv.style.display = 'block';
            reconnectingDiv.style.display = 'none';
        }

        function showReconnecting() {
            errorDiv.style.display = 'none';
            reconnectingDiv.style.display = 'block';
        }

        function hideMessages() {
            errorDiv.style.display = 'none';
            reconnectingDiv.style.display = 'none';
        }

        function reconnect() {
            if (reconnectAttempts >= maxReconnectAttempts) {
                showError();
                return;
            }

            showReconnecting();
            reconnectAttempts++;
            
            setTimeout(() => {
                audio.src = '/stream';
                audio.load();
                audio.play().catch(console.error);
            }, 1000 * Math.min(reconnectAttempts, 3));
        }

        audio.addEventListener('error', (e) => {
            console.error('Audio error:', e);
            reconnect();
        });

        audio.addEventListener('playing', () => {
            hideMessages();
            reconnectAttempts = 0;
        });

        // Handle page visibility changes
        document.addEventListener('visibilitychange', () => {
            if (document.visibilityState === 'visible' && audio.paused) {
                audio.src = '/stream';
                audio.load();
                audio.play().catch(console.error);
            }
        });

        // Prevent device sleep if possible
        async function preventSleep() {
            try {
                if (navigator.wakeLock) {
                    await navigator.wakeLock.request('screen');
                }
            } catch (err) {
                console.log('Wake Lock not supported:', err);
            }
        }
        
        preventSleep();

        // Handle iOS audio session
        document.addEventListener('touchstart', () => {
            if (audio.paused) {
                audio.play().catch(console.error);
            }
        }, { once: true });
    </script>
</body>
</html>`
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(html))
}

func main() {
	zap, _ := zap.NewProduction()
	defer zap.Sync()
	logger := zap.Sugar().With("module", "server")

	// Serve static files from the current directory
	fs := http.FileServer(http.Dir("."))
	http.Handle("/", fs)

	// WebSocket endpoint for browser-based streaming
	http.HandleFunc("/ws", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		wsHandler(w, r, logger)
	}))

	// HTTP endpoints for compatibility with existing clients
	http.HandleFunc("/stream", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		streamHandler(w, r, logger)
	}))

	// Serve the stream player page
	http.HandleFunc("/listen", corsMiddleware(serveStreamPage))

	logger.Info("Starting streaming server on http://localhost:8001/")
	logger.Info("Stream player available at http://localhost:8001/listen")
	logger.Fatal(http.ListenAndServe(":8001", nil))
}
