# MiniCast

A lightweight audio streaming server that enables real-time audio broadcasting over WebSocket connections. Built with Go and Web Audio API.

## Features

- Real-time audio streaming using WebSocket
- CD quality audio (44.1kHz, 16-bit, stereo)
- Web-based audio player with visualizer
- Volume control and connection status monitoring
- Mobile-friendly responsive design
- Dark mode support

## Prerequisites

- Go 1.21 or later
- [Bun](https://bun.sh) for development and running the server

## Installation

1. Clone the repository:
   ```bash
   git clone https://github.com/maks112v/minicast.git
   cd minicast
   ```

2. Install dependencies:
   ```bash
   bun install
   ```

3. Build and run the server:
   ```bash
   bun run build
   bun run start
   ```

The server will start at `http://localhost:8001`.

## Usage

### Listening to Audio Stream

1. Open `http://localhost:8001/listen` in your web browser
2. The player will automatically connect to the stream
3. Use the volume slider to adjust the audio level
4. The visualizer will show the audio frequency spectrum in real-time

### Broadcasting Audio

To broadcast audio, you need to connect to the WebSocket endpoint with the `source=true` query parameter:

```javascript
const ws = new WebSocket('ws://localhost:8001/ws?source=true');

// Send audio data as binary messages
ws.send(audioData);
```

## Project Structure

```
minicast/
├── cmd/
│   └── server/
│       └── main.go       # Server entry point
├── pkg/
│   ├── audio/
│   │   └── processor.go  # Audio processing
│   ├── server/
│   │   ├── server.go     # HTTP server
│   │   └── templates/    # HTML templates
│   └── websocket/
│       └── manager.go    # WebSocket management
└── README.md
```

## Contributing

1. Fork the repository
2. Create your feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add some amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

## License

This project is licensed under the MIT License - see the LICENSE file for details. 