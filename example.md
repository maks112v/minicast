# Basic Example

1. Start golang server `go run cmd/server/main.go`

2. Start stream using ffmpeg `go run cmd/source/main.go -file music.mp3 -url http://localhost:8001/source -user sourceuser -pass sourcepass`


```sh
export CGO_CFLAGS="-I/opt/homebrew/include"
export CGO_LDFLAGS="-L/opt/homebrew/lib"
```

3. Start stream from microphone `go run cmd/source/main.go -url http://localhost:8001/source -user sourceuser -pass sourcepass`
  - Install `brew install pkg-config portaudio lame`

4. Open browser and go to `http://localhost:8001/stream` to listen to the stream  
