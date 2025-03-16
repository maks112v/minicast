package main

import (
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

var (
	// WebSocket upgrader
	upgrader = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true // Allow all origins for development
		},
	}

	// Manage connected clients
	clientsMu sync.RWMutex
	clients   = make(map[*websocket.Conn]bool)

	// Manage audio source
	sourceMu     sync.RWMutex
	sourceConn   *websocket.Conn
	audioChannel = make(chan []byte, 1024)
)

func corsMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, PUT, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers",
			"Content-Type, Authorization, Accept, Origin, X-Requested-With")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	}
}

func wsHandler(w http.ResponseWriter, r *http.Request, logger *zap.SugaredLogger) {
	// Upgrade HTTP connection to WebSocket
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.Errorf("Failed to upgrade connection: %v", err)
		return
	}

	// Check if this is a source connection (streaming from microphone)
	isSource := r.URL.Query().Get("source") == "true"

	if isSource {
		handleSource(conn, logger)
	} else {
		handleListener(conn, logger)
	}
}

func handleSource(conn *websocket.Conn, logger *zap.SugaredLogger) {
	logger.Info("Audio source connected")

	sourceMu.Lock()
	if sourceConn != nil {
		sourceMu.Unlock()
		conn.WriteMessage(websocket.TextMessage, []byte("Another source is already connected"))
		conn.Close()
		return
	}
	sourceConn = conn
	sourceMu.Unlock()

	defer func() {
		sourceMu.Lock()
		if sourceConn == conn {
			sourceConn = nil
		}
		sourceMu.Unlock()
		conn.Close()
		logger.Info("Audio source disconnected")
	}()

	for {
		messageType, data, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				logger.Errorf("Source WebSocket error: %v", err)
			}
			break
		}

		if messageType == websocket.BinaryMessage {
			// Broadcast to all listeners
			clientsMu.RLock()
			for client := range clients {
				err := client.WriteMessage(websocket.BinaryMessage, data)
				if err != nil {
					logger.Debugf("Error sending to listener: %v", err)
					client.Close()
					delete(clients, client)
				}
			}
			clientsMu.RUnlock()
		}
	}
}

func handleListener(conn *websocket.Conn, logger *zap.SugaredLogger) {
	logger.Info("Listener connected")

	// Add to clients map
	clientsMu.Lock()
	clients[conn] = true
	clientsMu.Unlock()

	defer func() {
		clientsMu.Lock()
		delete(clients, conn)
		clientsMu.Unlock()
		conn.Close()
		logger.Info("Listener disconnected")
	}()

	// Keep the connection alive and handle any incoming messages
	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				logger.Debugf("Listener WebSocket error: %v", err)
			}
			break
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

        .controls {
            display: flex;
            flex-direction: column;
            gap: 12px;
        }

        .volume-control {
            width: 100%;
            margin-top: 8px;
        }

        .volume-control input {
            width: 100%;
        }

        .visualizer {
            width: 100%;
            height: 60px;
            background: var(--background-color);
            border-radius: 8px;
            margin-top: 16px;
        }

        .status {
            margin-top: 16px;
            padding: 12px;
            border-radius: 8px;
            background: var(--background-color);
            font-size: 14px;
            text-align: center;
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
        }

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
        }
    </style>
</head>
<body>
    <div class="container">
        <h1>MiniCast Player</h1>
        <div class="player-wrapper">
            <div class="controls">
                <div id="status" class="status">Connecting to stream...</div>
                <canvas id="visualizer" class="visualizer"></canvas>
                <div class="volume-control">
                    <input type="range" id="volume" min="0" max="100" value="100">
                </div>
            </div>
        </div>
        <div id="error" class="error">
            Connection lost. Attempting to reconnect...
        </div>
    </div>
    <script>
        let audioContext;
        let audioSource;
        let gainNode;
        let analyser;
        let ws;
        let reconnectAttempts = 0;
        const maxReconnectAttempts = 5;
        
        const visualizer = document.getElementById('visualizer');
        const ctx = visualizer.getContext('2d');
        const volumeControl = document.getElementById('volume');
        const statusDiv = document.getElementById('status');
        const errorDiv = document.getElementById('error');

        function showError(message) {
            errorDiv.textContent = message;
            errorDiv.style.display = 'block';
            statusDiv.style.display = 'none';
        }

        function showStatus(message) {
            statusDiv.textContent = message;
            statusDiv.style.display = 'block';
            errorDiv.style.display = 'none';
        }

        function setupAudioContext() {
            audioContext = new (window.AudioContext || window.webkitAudioContext)();
            gainNode = audioContext.createGain();
            analyser = audioContext.createAnalyser();
            analyser.fftSize = 256;

            gainNode.connect(audioContext.destination);
            gainNode.connect(analyser);

            volumeControl.addEventListener('input', (e) => {
                gainNode.gain.value = e.target.value / 100;
            });
        }

        function drawVisualizer() {
            const bufferLength = analyser.frequencyBinCount;
            const dataArray = new Uint8Array(bufferLength);
            const width = visualizer.width;
            const height = visualizer.height;
            const barWidth = width / bufferLength;

            function draw() {
                requestAnimationFrame(draw);

                analyser.getByteFrequencyData(dataArray);
                ctx.fillStyle = '#000';
                ctx.fillRect(0, 0, width, height);

                for (let i = 0; i < bufferLength; i++) {
                    const barHeight = (dataArray[i] / 255) * height;
                    ctx.fillStyle = 'hsl(' + (i * 360 / bufferLength) + ', 100%, 50%)';
                    ctx.fillRect(i * barWidth, height - barHeight, barWidth - 1, barHeight);
                }
            }

            draw();
        }

        function connectWebSocket() {
            if (ws) {
                ws.close();
            }

            ws = new WebSocket('ws://' + window.location.host + '/ws');
            
            ws.onopen = () => {
                showStatus('Connected to stream');
                reconnectAttempts = 0;
            };

            ws.onclose = () => {
                if (reconnectAttempts < maxReconnectAttempts) {
                    reconnectAttempts++;
                    showError('Connection lost. Reconnecting...');
                    setTimeout(connectWebSocket, 1000 * Math.min(reconnectAttempts, 3));
                } else {
                    showError('Connection lost. Please refresh the page.');
                }
            };

            ws.onmessage = async (event) => {
                try {
                    const arrayBuffer = await event.data.arrayBuffer();
                    const audioBuffer = await audioContext.decodeAudioData(arrayBuffer);
                    
                    const source = audioContext.createBufferSource();
                    source.buffer = audioBuffer;
                    source.connect(gainNode);
                    source.start(0);
                } catch (error) {
                    console.error('Error playing audio:', error);
                }
            };

            ws.onerror = (error) => {
                console.error('WebSocket error:', error);
                showError('Connection error');
            };
        }

        // Initialize audio context and visualizer
        function init() {
            setupAudioContext();
            
            // Set up visualizer canvas
            visualizer.width = visualizer.clientWidth;
            visualizer.height = visualizer.clientHeight;
            
            // Start WebSocket connection
            connectWebSocket();
            
            // Start visualization
            drawVisualizer();
        }

        // Handle window resize
        window.addEventListener('resize', () => {
            visualizer.width = visualizer.clientWidth;
            visualizer.height = visualizer.clientHeight;
        });

        // Start everything when the page loads
        window.addEventListener('load', init);

        // Handle page visibility changes
        document.addEventListener('visibilitychange', () => {
            if (document.visibilityState === 'visible') {
                if (ws.readyState !== WebSocket.OPEN) {
                    connectWebSocket();
                }
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

	// WebSocket endpoint
	http.HandleFunc("/ws", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		wsHandler(w, r, logger)
	}))

	// Serve the stream player page
	http.HandleFunc("/listen", corsMiddleware(serveStreamPage))

	logger.Info("Starting streaming server on http://localhost:8001/")
	logger.Info("Stream player available at http://localhost:8001/listen")
	logger.Fatal(http.ListenAndServe(":8001", nil))
}
