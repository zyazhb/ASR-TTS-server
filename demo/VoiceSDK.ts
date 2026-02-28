/**
 * VoiceSDK - Dead-simple WebSocket client for ASR and TTS.
 * No external dependencies.
 */

const ASR_SAMPLE_RATE = 16000;
const TTS_SAMPLE_RATE = 16000;

export class VoiceClient {
	private baseUrl: string;
	private asrWs: WebSocket | null = null;
	private asrStream: MediaStream | null = null;
	private asrContext: AudioContext | null = null;
	private asrSource: MediaStreamAudioSourceNode | null = null;
	private asrProcessor: ScriptProcessorNode | null = null;

	constructor(baseUrl: string = "") {
		const proto = window.location.protocol === "https:" ? "wss:" : "ws:";
		this.baseUrl = baseUrl || `${proto}//${window.location.host}`;
	}

	/**
	 * Starts ASR: captures mic at 16kHz mono, streams PCM to /asr.
	 * onMessage(text, isFinal): partial results replace in-place, final appends.
	 */
	async startASR(
		onMessage: (text: string, isFinal: boolean) => void,
	): Promise<void> {
		const stream = await navigator.mediaDevices.getUserMedia({ audio: true });
		this.asrStream = stream;

		const ctx = new AudioContext();
		this.asrContext = ctx;

		const src = ctx.createMediaStreamSource(stream);
		this.asrSource = src;

		const ws = new WebSocket(`${this.baseUrl}/asr`);
		this.asrWs = ws;

		const nativeRate = ctx.sampleRate;
		// Downsample factor: native (e.g. 48k) -> 16k
		const downsampleFactor = Math.max(
			1,
			Math.round(nativeRate / ASR_SAMPLE_RATE),
		);

		ws.onopen = () => {
			const bufferSize = 4096;
			const processor = ctx.createScriptProcessor(bufferSize, 1, 1);
			this.asrProcessor = processor;

			processor.onaudioprocess = (e) => {
				if (ws.readyState !== WebSocket.OPEN) return;

				const input = e.inputBuffer.getChannelData(0);
				// Downsample to 16kHz; sherpa expects float32 in [-1,1]
				const outLen = Math.floor(input.length / downsampleFactor);
				const float32 = new Float32Array(outLen);
				for (let i = 0; i < outLen; i++) {
					float32[i] = Math.max(-1, Math.min(1, input[i * downsampleFactor]));
				}
				ws.send(float32.buffer);
			};

			src.connect(processor);
			// Connect to silent gain (avoids feedback; node must be connected to run)
			const silence = ctx.createGain();
			silence.gain.value = 0;
			processor.connect(silence);
			silence.connect(ctx.destination);
		};

		ws.onmessage = (ev) => {
			if (typeof ev.data === "string") {
				try {
					const obj = JSON.parse(ev.data);
					if (obj.error) {
						onMessage(`[Error] ${obj.error}`, true);
						return;
					}
					const text = obj.text ?? obj.result;
					const isFinal = !!obj.is_final;
					if (text !== undefined) onMessage(text, isFinal);
				} catch (_) {}
			}
		};

		ws.onerror = (e) => console.error("[VoiceSDK] ASR WebSocket error:", e);
	}

	/**
	 * Stops ASR: closes WebSocket and releases mic/media resources.
	 */
	stopASR(): void {
		if (this.asrProcessor && this.asrSource) {
			this.asrProcessor.disconnect();
			this.asrSource.disconnect();
			this.asrProcessor = null;
			this.asrSource = null;
		}
		if (this.asrContext) {
			this.asrContext.close();
			this.asrContext = null;
		}
		if (this.asrStream) {
			this.asrStream.getTracks().forEach((t) => {
				t.stop();
			});
			this.asrStream = null;
		}
		if (this.asrWs) {
			this.asrWs.close();
			this.asrWs = null;
		}
	}

	/**
	 * Connects to /tts, sends text, streams PCM and plays chunks as they arrive (pseudo real-time).
	 */
	async speak(text: string, onPlayStart?: () => void): Promise<void> {
		const ws = new WebSocket(`${this.baseUrl}/tts`);
		ws.binaryType = "arraybuffer";

		const ctx = new AudioContext({ sampleRate: TTS_SAMPLE_RATE });
		let nextStart = ctx.currentTime;
		let started = false;

		const schedule = (buf: ArrayBuffer) => {
			const pcm = new Int16Array(buf);
			const float32 = new Float32Array(pcm.length);
			for (let i = 0; i < pcm.length; i++) {
				float32[i] = pcm[i] / (pcm[i] < 0 ? 0x8000 : 0x7fff);
			}
			const audioBuffer = ctx.createBuffer(1, float32.length, TTS_SAMPLE_RATE);
			audioBuffer.getChannelData(0).set(float32);

			nextStart = Math.max(nextStart, ctx.currentTime);
			const source = ctx.createBufferSource();
			source.buffer = audioBuffer;
			source.connect(ctx.destination);
			source.start(nextStart);
			nextStart += audioBuffer.duration;

			if (!started) {
				started = true;
				onPlayStart?.();
			}
		};

		ws.onopen = () => {
			ws.send(JSON.stringify({ text }));
		};

		ws.onmessage = (ev) => {
			if (typeof ev.data === "string") {
				try {
					const obj = JSON.parse(ev.data);
					if (obj.error) {
						console.error("[VoiceSDK] TTS error:", obj.error);
						ws.close();
						return;
					}
					if (obj.status === "done") ws.close();
				} catch (_) {}
			} else if (ev.data instanceof ArrayBuffer) {
				ctx.resume().then(() => schedule(ev.data as ArrayBuffer));
			}
		};

		ws.onerror = (e) => console.error("[VoiceSDK] TTS WebSocket error:", e);
	}
}
