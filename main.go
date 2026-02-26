package main

import (
	"context"
	_ "embed"
	"encoding/binary"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
)

//go:embed bin/sherpa-onnx-offline-tts
var embeddedTTS []byte

//go:embed bin/sherpa-onnx-online-websocket-server
var embeddedASR []byte

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for demo
	},
}

const (
	matchaDir   = "./matcha-icefall-zh-en"
	asrModelDir = "./sherpa-onnx-streaming-zipformer-bilingual-zh-en-2023-02-20"
)

var (
	extractedTTS string
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Extract and start sherpa-onnx-online-websocket-server
	asrPath, err := extractBinary("sherpa-asr-", embeddedASR)
	if err != nil {
		log.Fatal("extract ASR binary:", err)
	}
	defer os.Remove(asrPath)

	asrCmd := exec.CommandContext(ctx, asrPath,
		"--port=6006",
		"--tokens="+filepath.Join(asrModelDir, "tokens.txt"),
		"--encoder="+filepath.Join(asrModelDir, "encoder-epoch-99-avg-1.onnx"),
		"--decoder="+filepath.Join(asrModelDir, "decoder-epoch-99-avg-1.onnx"),
		"--joiner="+filepath.Join(asrModelDir, "joiner-epoch-99-avg-1.onnx"),
		"--num-threads=4",
	)
	asrCmd.Dir = "."
	asrCmd.Stdout = os.Stdout
	asrCmd.Stderr = os.Stderr
	if err := asrCmd.Start(); err != nil {
		log.Fatal("start ASR server:", err)
	}
	defer asrCmd.Process.Kill()

	// Wait for ASR server to listen
	time.Sleep(1 * time.Second)

	// Extract TTS binary for later use
	ttsPath, err := extractBinary("sherpa-tts-", embeddedTTS)
	if err != nil {
		log.Fatal("extract TTS binary:", err)
	}
	extractedTTS = ttsPath
	defer os.Remove(extractedTTS)

	http.HandleFunc("/asr", handleASR)
	http.HandleFunc("/tts", handleTTS)
	http.Handle("/", http.FileServer(http.Dir("./demo")))

	log.Println("Server listening on :8080")
	server := &http.Server{Addr: ":8080"}
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	<-ctx.Done()
	server.Shutdown(context.Background())
}

func extractBinary(prefix string, data []byte) (string, error) {
	tmp, err := os.CreateTemp("", prefix+"*.bin")
	if err != nil {
		return "", err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return "", err
	}
	if err := tmp.Chmod(0755); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return "", err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return "", err
	}
	return tmp.Name(), nil
}

const sherpaASRAddr = "127.0.0.1:6006"

// handleASR proxies raw PCM to sherpa-onnx-online-websocket-server. Fails if sherpa not running.
func handleASR(w http.ResponseWriter, r *http.Request) {
	clientConn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("ASR upgrade:", err)
		return
	}
	defer clientConn.Close()

	sherpaConn, _, err := websocket.DefaultDialer.Dial("ws://"+sherpaASRAddr, nil)
	if err != nil {
		log.Println("ASR: sherpa not available at", sherpaASRAddr, err)
		clientConn.WriteJSON(map[string]string{"error": "ASR server not available. Start sherpa-onnx-online-websocket-server on :6006"})
		return
	}
	defer sherpaConn.Close()

	go func() {
		for {
			mt, msg, err := sherpaConn.ReadMessage()
			if err != nil {
				return
			}
			if err := clientConn.WriteMessage(mt, msg); err != nil {
				return
			}
		}
	}()

	for {
		mt, msg, err := clientConn.ReadMessage()
		if err != nil {
			return
		}
		if err := sherpaConn.WriteMessage(mt, msg); err != nil {
			return
		}
	}
}

// handleTTS synthesizes full text in one sherpa call, streams PCM to client (single synthesis = natural prosody).
func handleTTS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("TTS upgrade:", err)
		return
	}
	defer conn.Close()

	conn.SetReadDeadline(time.Now().Add(120 * time.Second))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(120 * time.Second))
		return nil
	})

	_, msg, err := conn.ReadMessage()
	if err != nil {
		log.Println("TTS read:", err)
		return
	}

	var payload struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(msg, &payload); err != nil {
		conn.WriteJSON(map[string]string{"error": "invalid payload"})
		return
	}

	pcm, err := runSherpaTTS(strings.TrimSpace(payload.Text))
	if err != nil {
		log.Println("TTS:", err)
		conn.WriteJSON(map[string]string{"error": err.Error()})
		return
	}

	// Stream PCM in ~4KB chunks (network streaming only, synthesis is one-piece)
	const chunkSize = 4096
	for i := 0; i < len(pcm); i += chunkSize {
		end := i + chunkSize
		if end > len(pcm) {
			end = len(pcm)
		}
		if err := conn.WriteMessage(websocket.BinaryMessage, pcm[i:end]); err != nil {
			return
		}
	}

	conn.WriteJSON(map[string]string{"status": "done"})
}

// runSherpaTTS runs sherpa-onnx-offline-tts and returns raw 16-bit PCM (16kHz mono).
func runSherpaTTS(text string) ([]byte, error) {
	tmp, err := os.CreateTemp("", "tts-*.wav")
	if err != nil {
		return nil, err
	}
	tmpPath := tmp.Name()
	tmp.Close()
	defer os.Remove(tmpPath)

	cmd := exec.Command(extractedTTS,
		"--matcha-acoustic-model="+filepath.Join(matchaDir, "model-steps-3.onnx"),
		"--matcha-vocoder="+filepath.Join(matchaDir, "vocos-16khz-univ.onnx"),
		"--matcha-lexicon="+filepath.Join(matchaDir, "lexicon.txt"),
		"--matcha-tokens="+filepath.Join(matchaDir, "tokens.txt"),
		"--matcha-data-dir="+filepath.Join(matchaDir, "espeak-ng-data"),
		"--num-threads=4",
		"--tts-rule-fsts="+filepath.Join(matchaDir, "number-zh.fst")+","+filepath.Join(matchaDir, "phone-zh.fst")+","+filepath.Join(matchaDir, "date-zh.fst"),
		"--output-filename="+tmpPath,
		text,
	)
	cmd.Dir = "."
	if _, err := cmd.CombinedOutput(); err != nil {
		return nil, err
	}

	data, err := os.ReadFile(tmpPath)
	if err != nil {
		return nil, err
	}
	return extractWAVPCM(data)
}

// extractWAVPCM returns raw 16-bit PCM from WAV data chunk.
func extractWAVPCM(wav []byte) ([]byte, error) {
	if len(wav) < 12 {
		return nil, nil
	}
	if string(wav[0:4]) != "RIFF" || string(wav[8:12]) != "WAVE" {
		return nil, nil
	}
	i := 12
	for i+8 <= len(wav) {
		chunkID := string(wav[i : i+4])
		chunkLen := int(binary.LittleEndian.Uint32(wav[i+4 : i+8]))
		if chunkID == "data" {
			start := i + 8
			end := start + chunkLen
			if end > len(wav) {
				end = len(wav)
			}
			return wav[start:end], nil
		}
		// WAV chunks are 2-byte aligned
		i += 8 + ((chunkLen + 1) &^ 1)
	}
	return nil, nil
}
