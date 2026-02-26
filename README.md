## demo (ASR + TTS)

1. Start sherpa-onnx WebSocket ASR server (for real ASR):
```bash
./sherpa-onnx-online-websocket-server \
  --port=6006 \
  --tokens=./sherpa-onnx-streaming-zipformer-bilingual-zh-en-2023-02-20/tokens.txt \
  --encoder=./sherpa-onnx-streaming-zipformer-bilingual-zh-en-2023-02-20/encoder-epoch-99-avg-1.onnx \
  --decoder=./sherpa-onnx-streaming-zipformer-bilingual-zh-en-2023-02-20/decoder-epoch-99-avg-1.onnx \
  --joiner=./sherpa-onnx-streaming-zipformer-bilingual-zh-en-2023-02-20/joiner-epoch-99-avg-1.onnx \
  --num-threads=4
```

2. Build frontend and start Go server:
```bash
bun run build && go run main.go
```

3. Open http://localhost:8080 — ASR and TTS both use sherpa-onnx (no mocks).

---

## microphone realtime ASR
./sherpa-onnx-microphone \
  --tokens=./sherpa-onnx-streaming-zipformer-bilingual-zh-en-2023-02-20/tokens.txt \
  --encoder=./sherpa-onnx-streaming-zipformer-bilingual-zh-en-2023-02-20/encoder-epoch-99-avg-1.onnx \
  --decoder=./sherpa-onnx-streaming-zipformer-bilingual-zh-en-2023-02-20/decoder-epoch-99-avg-1.onnx \
  --joiner=./sherpa-onnx-streaming-zipformer-bilingual-zh-en-2023-02-20/joiner-epoch-99-avg-1.onnx

## websocket realtime ASR

./sherpa-onnx-online-websocket-server \
  --port=6006 \
  --tokens=./sherpa-onnx-streaming-zipformer-bilingual-zh-en-2023-02-20/tokens.txt \
  --encoder=./sherpa-onnx-streaming-zipformer-bilingual-zh-en-2023-02-20/encoder-epoch-99-avg-1.onnx \
  --decoder=./sherpa-onnx-streaming-zipformer-bilingual-zh-en-2023-02-20/decoder-epoch-99-avg-1.onnx \
  --joiner=./sherpa-onnx-streaming-zipformer-bilingual-zh-en-2023-02-20/joiner-epoch-99-avg-1.onnx \
  --num-threads=4

## offline tts
./sherpa-onnx-offline-tts \
  --matcha-acoustic-model=./matcha-icefall-zh-en/model-steps-3.onnx \
  --matcha-vocoder=./matcha-icefall-zh-en/vocos-16khz-univ.onnx \
  --matcha-lexicon=./matcha-icefall-zh-en/lexicon.txt \
  --matcha-tokens=./matcha-icefall-zh-en/tokens.txt \
  --matcha-data-dir=./matcha-icefall-zh-en/espeak-ng-data \
  --num-threads=4 \
  --output-filename=test.wav \
  "这是语音合成示例，语音助手已就绪。"