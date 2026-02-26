import { VoiceClient } from "./VoiceSDK.js";

const client = new VoiceClient("ws://localhost:8080");

const asrOutput = document.getElementById("asr-output") as HTMLTextAreaElement;
const ttsInput = document.getElementById("tts-input") as HTMLInputElement;
const startBtn = document.getElementById("start-asr") as HTMLButtonElement;
const stopBtn = document.getElementById("stop-asr") as HTMLButtonElement;
const speakBtn = document.getElementById("speak") as HTMLButtonElement;

let completedLines: string[] = [];

function onASRMessage(text: string, isFinal: boolean) {
  if (isFinal) {
    completedLines.push(text);
    asrOutput.value = completedLines.join("\n") + "\n";
  } else {
    // Partial: replace last line (current utterance) with latest text
    asrOutput.value = completedLines.join("\n") + (completedLines.length ? "\n" : "") + text;
  }
  asrOutput.scrollTop = asrOutput.scrollHeight;
}

startBtn.addEventListener("click", async () => {
  completedLines = [];
  startBtn.disabled = true;
  stopBtn.disabled = false;
  try {
    await client.startASR(onASRMessage);
  } catch (e) {
    onASRMessage(`[Error] ${e}`, true);
    startBtn.disabled = false;
    stopBtn.disabled = true;
  }
});

stopBtn.addEventListener("click", () => {
  client.stopASR();
  startBtn.disabled = false;
  stopBtn.disabled = true;
});

speakBtn.addEventListener("click", () => {
  const text = ttsInput.value.trim();
  if (!text) return;
  speakBtn.disabled = true;
  client.speak(text, () => {
    // Optional: show "playing" state
  }).finally(() => {
    speakBtn.disabled = false;
  });
});
