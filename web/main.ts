/*
MIT License

Copyright (c) 2026 GoAkt Team

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
*/

// Skybound Runner client — renders server-authoritative co-op state on canvas.
//
// Snapshots arrive over WebSocket at ~60 Hz. Rendering runs on its own
// requestAnimationFrame loop and interpolates entity positions between the
// two most recent snapshots, so display stays smooth under network jitter.
// The tile grid is a delta: the server only includes it when it changes, and
// the client caches the last full copy.

interface MarioState {
  x: number;
  y: number;
  vx: number;
  vy: number;
  onGround: boolean;
  facing: number;
  dead: boolean;
  shield: boolean;
  invuln: number;
  crouch: boolean;
}

interface PlayerView {
  name: string;
  mario: MarioState;
}

interface EnemyState {
  id: number;
  kind: number;
  x: number;
  y: number;
  vy: number;
  dir: number;
  alive: boolean;
  dying: number;
  baseX: number;
  baseY: number;
}

interface ItemState {
  id: number;
  kind: number;
  x: number;
  y: number;
  vy: number;
  dir: number;
  alive: boolean;
}

interface Snapshot {
  tick: number;
  t: number;
  tiles?: number[][];
  players: PlayerView[];
  you: number;
  enemies: EnemyState[];
  items: ItemState[];
  cameraX: number;
  stage: number;
  totalStages: number;
  world: number;
  stageInWorld: number;
  stageName: string;
  theme: number;
  stageClear: boolean;
  score: number;
  coins: number;
  lives: number;
  timeLeft: number;
  gameOver: boolean;
  won: boolean;
  paused: boolean;
  checkpointX: number;
  checkpointActive: boolean;
}

const MSG_TYPE_INPUT = "input";

const ACTION = {
  LEFT: "left",
  LEFT_END: "left-end",
  RIGHT: "right",
  RIGHT_END: "right-end",
  JUMP: "jump",
  JUMP_END: "jump-end",
  DOWN: "down",
  DOWN_END: "down-end",
  PAUSE: "pause",
  RESTART: "restart",
} as const;

type Action = typeof ACTION[keyof typeof ACTION];

// Tile kinds — mirror types.go
const TILE = {
  AIR: 0,
  GROUND: 1,
  BRICK: 2,
  QUESTION: 3,
  PIPE: 4,
  FLAG: 5,
  COIN: 6,
  CHECKPOINT: 7,
} as const;

// Enemy kinds — mirror types.go
const ENEMY = {
  CRAWLER: 0,
  FLYER: 1,
  SPIKY: 2,
} as const;

const TILE_PX = 32;
const LEVEL_H = 15;
const VIEW_W = 20; // the server computes its camera for this view width
const BEST_KEY = "skybound-best-score";

interface WorldTheme {
  skyTop: string;
  skyBottom: string;
  horizon: string;
  sun: string;
  distant: string;
  midground: string;
  ground: string;
  grass: string;
  grassDark: string;
  brick: string;
  pipe: string;
  question: string;
  coin: string;
  accent: string;
}

const THEMES: readonly WorldTheme[] = [
  {
    skyTop: "#3f8fdd", skyBottom: "#a8ddf7", horizon: "#ffd9a0", sun: "#fff0c2",
    distant: "#7fa7c9", midground: "#4f8a5d", ground: "#8a5a34", grass: "#57b046",
    grassDark: "#3a8a35", brick: "#bb7744", pipe: "#4b9b69", question: "#e8a83a",
    coin: "#ffe278", accent: "#f7f4cf",
  },
  {
    skyTop: "#183c53", skyBottom: "#397080", horizon: "#6fa8a2", sun: "#e2f4ef",
    distant: "#2c5e6e", midground: "#1a4553", ground: "#4f6770", grass: "#3f9078",
    grassDark: "#2c6f5c", brick: "#8c6c56", pipe: "#2f9a83", question: "#e2a34b",
    coin: "#f7d16e", accent: "#c7edf1",
  },
  {
    skyTop: "#31264f", skyBottom: "#b04a46", horizon: "#e0703c", sun: "#ffb45e",
    distant: "#55334f", midground: "#2e2639", ground: "#45404a", grass: "#9a7a44",
    grassDark: "#715a2e", brick: "#76606a", pipe: "#80525e", question: "#d58046",
    coin: "#ffd36a", accent: "#ff9560",
  },
];

function rgba(hex: string, alpha: number): string {
  const n = parseInt(hex.slice(1), 16);
  return `rgba(${(n >> 16) & 255}, ${(n >> 8) & 255}, ${n & 255}, ${alpha})`;
}

function mod(value: number, m: number): number {
  return ((value % m) + m) % m;
}

function clamp(v: number, lo: number, hi: number): number {
  return Math.max(lo, Math.min(hi, v));
}

// Deterministic per-tile hash so terrain texture is stable frame to frame.
function tileHash(x: number, y: number): number {
  let h = (x * 374761393 + y * 668265263) | 0;
  h = ((h ^ (h >>> 13)) * 1274126177) | 0;
  return (h ^ (h >>> 16)) >>> 0;
}

interface Particle {
  x: number;
  y: number;
  vx: number;
  vy: number;
  life: number;
  color: string;
  size: number;
}

function el<T extends HTMLElement>(id: string): T {
  const found = document.getElementById(id);
  if (!found) throw new Error(`missing #${id}`);
  return found as T;
}

function canvasCtx(c: HTMLCanvasElement): CanvasRenderingContext2D {
  const ctx = c.getContext("2d");
  if (!ctx) throw new Error("2D context unavailable");
  return ctx;
}

const board = el<HTMLCanvasElement>("board");
const ctx = canvasCtx(board);
const scoreVal = el("scoreVal");
const bestVal = el("bestVal");
const coinsVal = el("coinsVal");
const timeVal = el("timeVal");
const livesVal = el("livesVal");
const worldVal = el("worldVal");
const playersVal = el("playersVal");
const stageNameEl = el("stageName");
const statusEl = el("status");
const fullscreenBtn = el<HTMLButtonElement>("fullscreenBtn");
const soundBtn = el<HTMLButtonElement>("soundBtn");
const gameOverOverlay = el("gameOverOverlay");
const winOverlay = el("winOverlay");
const pauseOverlay = el("pauseOverlay");
const stageClearOverlay = el("stageClearOverlay");
const stageClearName = el("stageClearName");

// --- Snapshot buffering for interpolation ---
let curr: Snapshot | null = null;
let prev: Snapshot | null = null;
let currAt = 0;
let snapInterval = 16.7; // rolling estimate of the snapshot cadence, ms
let tilesCache: number[][] = [];
let ws: WebSocket | null = null;

let displayCameraX = 0;
let particles: Particle[] = [];
let shakeUntil = 0; // performance.now() timestamp when camera shake ends
let displayScore = 0;
let displayCoins = 0;
let bestScore = Number(localStorage.getItem(BEST_KEY) ?? "0") || 0;

// The canvas backing store always shows LEVEL_H tiles vertically and matches
// the window's aspect ratio horizontally, so the game fills the whole screen
// without stretching — wider windows simply see more of the level.
function resizeBoard(): void {
  const aspect = Math.max(0.5, Math.min(3, window.innerWidth / Math.max(1, window.innerHeight)));
  board.height = LEVEL_H * TILE_PX;
  board.width = Math.round(board.height * aspect);
}
addEventListener("resize", resizeBoard);
addEventListener("orientationchange", resizeBoard);
resizeBoard();

// Older Safari only ships a webkit-prefixed AudioContext.
const AudioContextCtor: typeof AudioContext | undefined =
  window.AudioContext ??
  (window as Window & { webkitAudioContext?: typeof AudioContext }).webkitAudioContext;

class AudioManager {
  private context: AudioContext | null = null;
  private master: GainNode | null = null;
  private musicBus: GainNode | null = null;
  private musicTimer: number | null = null;
  private musicTheme = 0;
  private musicStarted = false;
  muted = false;

  unlock(): void {
    if (!this.context) {
      if (!AudioContextCtor) return;
      this.context = new AudioContextCtor();
      this.master = this.context.createGain();
      this.master.gain.value = 0.55;
      this.master.connect(this.context.destination);
      this.musicBus = this.context.createGain();
      this.musicBus.gain.value = 0.09;
      this.musicBus.connect(this.master);
    }
    if (this.context.state === "suspended") void this.context.resume();
    this.startMusic();
  }

  toggleMute(): void {
    this.muted = !this.muted;
    if (this.muted) {
      this.stopMusic();
    } else {
      this.startMusic();
    }
  }

  setTheme(theme: number): void {
    if (this.musicTheme === theme) return;
    this.musicTheme = theme;
    if (this.musicStarted) {
      this.stopMusic();
      this.startMusic();
    }
  }

  play(kind: "jump" | "point" | "stomp" | "hurt" | "clear" | "power"): void {
    if (this.muted) return;
    this.unlock();
    if (!this.context || !this.master) return;

    const settings = {
      jump: [440, 640, 0.10, "square"],
      point: [660, 990, 0.14, "sine"],
      stomp: [170, 95, 0.10, "square"],
      hurt: [220, 90, 0.22, "sawtooth"],
      clear: [523, 1046, 0.35, "triangle"],
      power: [392, 1175, 0.28, "triangle"],
    } as const;
    const [start, end, duration, waveform] = settings[kind];
    const oscillator = this.context.createOscillator();
    const gain = this.context.createGain();
    oscillator.type = waveform;
    oscillator.frequency.setValueAtTime(start, this.context.currentTime);
    oscillator.frequency.exponentialRampToValueAtTime(end, this.context.currentTime + duration);
    gain.gain.setValueAtTime(0.06, this.context.currentTime);
    gain.gain.exponentialRampToValueAtTime(0.001, this.context.currentTime + duration);
    oscillator.connect(gain).connect(this.master);
    oscillator.start();
    oscillator.stop(this.context.currentTime + duration);
  }

  private startMusic(): void {
    if (this.muted || this.musicStarted || !this.context || !this.musicBus) return;
    this.musicStarted = true;
    this.scheduleMusicLoop();
  }

  private stopMusic(): void {
    if (this.musicTimer !== null) {
      window.clearTimeout(this.musicTimer);
      this.musicTimer = null;
    }
    this.musicStarted = false;
  }

  private scheduleMusicLoop(): void {
    if (!this.context || !this.musicBus || !this.musicStarted || this.muted) return;

    const step = 0.17;
    const start = this.context.currentTime + 0.06;
    const motif = MUSIC_MOTIFS[this.musicTheme] ?? MUSIC_MOTIFS[0]!;
    for (let index = 0; index < motif.lead.length; index++) {
      const at = start + index * step;
      const lead = motif.lead[index];
      const bass = motif.bass[index];
      if (lead !== null) this.note(lead, at, step * 0.82, "square", 0.38);
      if (bass !== null) this.note(bass, at, step * 0.92, "triangle", 0.44);
      if (index % 4 === 0) this.drum(at, 0.055, 85);
      if (index % 4 === 2) this.drum(at, 0.025, 260);
    }
    this.musicTimer = window.setTimeout(() => this.scheduleMusicLoop(), motif.lead.length * step * 1000);
  }

  private note(midi: number, at: number, duration: number, type: OscillatorType, volume: number): void {
    if (!this.context || !this.musicBus) return;
    const oscillator = this.context.createOscillator();
    const gain = this.context.createGain();
    oscillator.type = type;
    oscillator.frequency.value = 440 * 2 ** ((midi - 69) / 12);
    gain.gain.setValueAtTime(volume, at);
    gain.gain.exponentialRampToValueAtTime(0.001, at + duration);
    oscillator.connect(gain).connect(this.musicBus);
    oscillator.start(at);
    oscillator.stop(at + duration);
  }

  private drum(at: number, duration: number, frequency: number): void {
    if (!this.context || !this.musicBus) return;
    const oscillator = this.context.createOscillator();
    const gain = this.context.createGain();
    oscillator.type = "square";
    oscillator.frequency.setValueAtTime(frequency, at);
    oscillator.frequency.exponentialRampToValueAtTime(36, at + duration);
    gain.gain.setValueAtTime(0.28, at);
    gain.gain.exponentialRampToValueAtTime(0.001, at + duration);
    oscillator.connect(gain).connect(this.musicBus);
    oscillator.start(at);
    oscillator.stop(at + duration);
  }
}

interface MusicMotif {
  lead: readonly (number | null)[];
  bass: readonly (number | null)[];
}

// Original 16-step themes: bright fields, mechanical works, and ember ruins.
const MUSIC_MOTIFS: readonly MusicMotif[] = [
  {
    lead: [72, 76, 79, 76, 74, 72, 69, null, 72, 74, 76, 79, 81, 79, 76, 74],
    bass: [48, null, 48, null, 50, null, 50, null, 45, null, 45, null, 43, null, 43, null],
  },
  {
    lead: [64, null, 67, 69, 64, null, 67, 71, 62, null, 65, 67, 62, null, 65, 69],
    bass: [40, null, 40, null, 43, null, 43, null, 38, null, 38, null, 41, null, 41, null],
  },
  {
    lead: [69, 68, 65, null, 69, 72, 71, null, 67, 65, 62, null, 65, 67, 69, 74],
    bass: [38, null, 38, null, 41, null, 41, null, 36, null, 36, null, 34, null, 34, null],
  },
];

const audio = new AudioManager();

function connect(): void {
  const proto = location.protocol === "https:" ? "wss:" : "ws:";
  ws = new WebSocket(`${proto}//${location.host}/ws`);
  ws.onopen = () => { statusEl.textContent = "connected"; };
  ws.onclose = () => {
    ws = null;
    statusEl.textContent = "disconnected — retrying";
    setTimeout(connect, 1000);
  };
  ws.onerror = () => ws?.close();
  ws.onmessage = (e: MessageEvent<string>) => {
    const snap = JSON.parse(e.data) as Snapshot;
    const now = performance.now();
    if (curr) {
      snapInterval = clamp(0.9 * snapInterval + 0.1 * (now - currAt), 10, 100);
    }
    prev = curr;
    curr = snap;
    currAt = now;
    if (snap.tiles) tilesCache = snap.tiles;
    handleSnapshotChange(prev, snap);
    // requestAnimationFrame stalls in background tabs (and headless
    // captures); keep the picture moving from the message stream instead.
    if (now - lastFrameAt > 100) render(now);
  };
}
connect();

// localPlayer returns this session's player view within a snapshot.
function localPlayer(s: Snapshot): PlayerView | undefined {
  return s.players[s.you] ?? s.players[0];
}

// playerIn finds the same player (by name) in another snapshot.
function playerIn(s: Snapshot | null, name: string): PlayerView | undefined {
  return s?.players.find((p) => p.name === name);
}

function persistBest(score: number): void {
  if (score > bestScore) {
    bestScore = score;
    localStorage.setItem(BEST_KEY, String(bestScore));
  }
}

function handleSnapshotChange(previous: Snapshot | null, current: Snapshot): void {
  audio.setTheme(current.theme);
  const me = localPlayer(current);
  if (!previous || !me) {
    displayCameraX = current.cameraX;
    return;
  }
  const prevMe = playerIn(previous, me.name);

  const stompedEnemy = current.enemies.find((enemy) => {
    const before = previous.enemies.find((e) => e.id === enemy.id);
    return before !== undefined && before.alive && !enemy.alive && enemy.dying > 0;
  });
  const earnedPoint = current.score > previous.score && !current.stageClear;

  if (stompedEnemy) {
    audio.play("stomp");
    emitParticles(stompedEnemy.x + 0.4, stompedEnemy.y + 0.6, 6, "#f7f4cf");
  } else if (earnedPoint) {
    audio.play("point");
    emitParticles(me.mario.x, me.mario.y, 8, themeFor(current).coin);
  }

  if (prevMe && !prevMe.mario.shield && me.mario.shield) {
    audio.play("power");
    emitParticles(me.mario.x, me.mario.y, 14, "#5abeff");
  }

  if (prevMe && !prevMe.mario.dead && me.mario.dead) {
    audio.play("hurt");
    emitParticles(me.mario.x, me.mario.y, 12, "#ff795e");
    shakeUntil = performance.now() + 450;
  }

  // A shield break also deserves feedback: invuln jumps while staying alive.
  if (prevMe && prevMe.mario.shield && !me.mario.shield && !me.mario.dead) {
    audio.play("hurt");
    shakeUntil = performance.now() + 250;
  }

  if (current.stageClear && !previous.stageClear) {
    audio.play("clear");
    persistBest(current.score);
  }
  if ((current.gameOver && !previous.gameOver) || (current.won && !previous.won)) {
    persistBest(current.score);
  }
}

function emitParticles(x: number, y: number, count: number, color: string): void {
  for (let i = 0; i < count; i++) {
    particles.push({
      x: x * TILE_PX + TILE_PX / 2,
      y: y * TILE_PX + TILE_PX / 2,
      vx: (Math.random() - 0.5) * 3,
      vy: -Math.random() * 2 - 0.5,
      life: 24 + Math.floor(Math.random() * 18),
      color,
      size: 2 + Math.floor(Math.random() * 3),
    });
  }
}

function send(action: Action): void {
  if (ws && ws.readyState === WebSocket.OPEN) {
    ws.send(JSON.stringify({ type: MSG_TYPE_INPUT, action }));
  }
}

// Safari (iPad, and macOS before 16.4) only exposes the webkit-prefixed
// Fullscreen API; iPhone Safari has no element fullscreen at all.
interface WebkitDocument extends Document {
  webkitFullscreenElement?: Element | null;
  webkitExitFullscreen?: () => Promise<void> | void;
}
interface WebkitElement extends HTMLElement {
  webkitRequestFullscreen?: () => Promise<void> | void;
}

function currentFullscreenElement(): Element | null {
  const doc = document as WebkitDocument;
  return doc.fullscreenElement ?? doc.webkitFullscreenElement ?? null;
}

async function toggleFullscreen(): Promise<void> {
  const doc = document as WebkitDocument;
  const root = document.documentElement as WebkitElement;
  try {
    if (currentFullscreenElement()) {
      if (doc.exitFullscreen) await doc.exitFullscreen();
      else if (doc.webkitExitFullscreen) await doc.webkitExitFullscreen();
    } else {
      if (root.requestFullscreen) await root.requestFullscreen();
      else if (root.webkitRequestFullscreen) await root.webkitRequestFullscreen();
    }
  } catch {
    // Fullscreen can be denied by browser or embed permissions. The game
    // still fills the available viewport via CSS in that case.
  }
}

fullscreenBtn.addEventListener("click", () => {
  void toggleFullscreen();
  fullscreenBtn.blur();
});

function syncFullscreenLabel(): void {
  fullscreenBtn.textContent = currentFullscreenElement() ? "EXIT" : "FULL";
}
document.addEventListener("fullscreenchange", syncFullscreenLabel);
document.addEventListener("webkitfullscreenchange", syncFullscreenLabel);

soundBtn.addEventListener("click", () => {
  audio.unlock();
  audio.toggleMute();
  soundBtn.textContent = audio.muted ? "MUTED" : "SOUND";
  soundBtn.blur();
});

const held = new Set<string>();

addEventListener("keydown", (e: KeyboardEvent) => {
  if (e.repeat) return;
  audio.unlock();
  switch (e.code) {
    case "ArrowLeft":  case "KeyA": send(ACTION.LEFT); held.add("left"); break;
    case "ArrowRight": case "KeyD": send(ACTION.RIGHT); held.add("right"); break;
    case "ArrowUp":    case "KeyW":
    case "Space": send(ACTION.JUMP); held.add("jump"); break;
    case "ArrowDown":  case "KeyS": send(ACTION.DOWN); held.add("down"); break;
    case "KeyP": send(ACTION.PAUSE); break;
    case "KeyR": send(ACTION.RESTART); break;
    case "KeyF": void toggleFullscreen(); break;
    case "KeyM":
      audio.toggleMute();
      soundBtn.textContent = audio.muted ? "MUTED" : "SOUND";
      break;
    default: return;
  }
  e.preventDefault();
});

addEventListener("keyup", (e: KeyboardEvent) => {
  switch (e.code) {
    case "ArrowLeft":  case "KeyA":
      if (held.delete("left")) send(ACTION.LEFT_END);
      break;
    case "ArrowRight": case "KeyD":
      if (held.delete("right")) send(ACTION.RIGHT_END);
      break;
    case "ArrowUp":    case "KeyW":
    case "Space":
      if (held.delete("jump")) send(ACTION.JUMP_END);
      break;
    case "ArrowDown":  case "KeyS":
      if (held.delete("down")) send(ACTION.DOWN_END);
      break;
  }
});

// Safari drops keyup events around fullscreen transitions, Cmd-Tab, and
// while the Meta key is held, leaving the runner moving on its own. Release
// everything whenever the page loses focus or visibility.
function releaseHeldKeys(): void {
  if (held.delete("left")) send(ACTION.LEFT_END);
  if (held.delete("right")) send(ACTION.RIGHT_END);
  if (held.delete("jump")) send(ACTION.JUMP_END);
  if (held.delete("down")) send(ACTION.DOWN_END);
}
addEventListener("blur", releaseHeldKeys);
addEventListener("pagehide", releaseHeldKeys);
document.addEventListener("visibilitychange", () => {
  if (document.hidden) releaseHeldKeys();
});

// Touch controls for iPhone/iPad, where there is no keyboard. The buttons
// are only visible on coarse-pointer devices (see index.html CSS).
function bindHoldButton(id: string, key: string, start: Action, end: Action): void {
  const btn = document.getElementById(id);
  if (!btn) return;
  const press = (e: PointerEvent): void => {
    e.preventDefault();
    audio.unlock();
    if (btn.setPointerCapture) btn.setPointerCapture(e.pointerId);
    if (!held.has(key)) {
      held.add(key);
      send(start);
    }
  };
  const release = (e: PointerEvent): void => {
    e.preventDefault();
    if (held.delete(key)) send(end);
  };
  btn.addEventListener("pointerdown", press);
  btn.addEventListener("pointerup", release);
  btn.addEventListener("pointercancel", release);
  btn.addEventListener("contextmenu", (e) => e.preventDefault());
}

bindHoldButton("touchLeft", "left", ACTION.LEFT, ACTION.LEFT_END);
bindHoldButton("touchRight", "right", ACTION.RIGHT, ACTION.RIGHT_END);
bindHoldButton("touchDown", "down", ACTION.DOWN, ACTION.DOWN_END);
bindHoldButton("touchJump", "jump", ACTION.JUMP, ACTION.JUMP_END);

// With no keyboard, tapping a game-over/win overlay restarts the campaign.
for (const overlay of [gameOverOverlay, winOverlay]) {
  overlay.addEventListener("click", () => send(ACTION.RESTART));
}

// --- Interpolation helpers ---

function lerp(a: number, b: number, t: number): number {
  return a + (b - a) * t;
}

// lerpMario interpolates a player's position between snapshots; teleports
// (respawns, stage loads) fall back to the current position.
function lerpMario(before: MarioState | undefined, now: MarioState, t: number): MarioState {
  if (!before || Math.abs(before.x - now.x) > 3 || Math.abs(before.y - now.y) > 3) return now;
  return { ...now, x: lerp(before.x, now.x, t), y: lerp(before.y, now.y, t) };
}

// --- Render loop ---

let lastFrameAt = 0;

function frame(): void {
  render(performance.now());
  requestAnimationFrame(frame);
}
requestAnimationFrame(frame);

function render(now: number): void {
  if (!curr || tilesCache.length === 0) return;
  lastFrameAt = now;
  const s = curr;
  const alpha = prev ? clamp((now - currAt) / snapInterval, 0, 1) : 1;
  const theme = themeFor(s);

  // Camera: interpolate the server value, then smooth, then recenter for the
  // actual canvas width and clamp to the level bounds.
  const serverCam = prev ? lerp(prev.cameraX, s.cameraX, alpha) : s.cameraX;
  displayCameraX += (serverCam - displayCameraX) * 0.18;
  const viewTiles = board.width / TILE_PX;
  const levelW = tilesCache[0]?.length ?? 0;
  const maxCam = Math.max(0, levelW - viewTiles);
  let camX = clamp(displayCameraX - (viewTiles - VIEW_W) / 2, 0, maxCam);

  // Camera shake after a hit, decaying over its duration.
  let shakeY = 0;
  if (now < shakeUntil) {
    const falloff = (shakeUntil - now) / 450;
    camX += Math.sin(now * 0.09) * 0.09 * falloff;
    shakeY = Math.cos(now * 0.13) * 5 * falloff;
  }

  ctx.save();
  ctx.translate(0, shakeY);

  drawBackground(ctx, theme, camX, s.tick);

  // Tiles
  const startCol = Math.max(0, Math.floor(camX));
  const endCol = Math.min(startCol + Math.ceil(viewTiles) + 2, levelW);

  for (let y = 0; y < tilesCache.length; y++) {
    const row = tilesCache[y]!;
    for (let x = startCol; x < endCol; x++) {
      const tile = row[x]!;
      if (tile === TILE.AIR) continue;
      const px = (x - camX) * TILE_PX;
      const py = y * TILE_PX;
      if (px + TILE_PX < 0 || px > board.width) continue;
      const exposed = y === 0 || tilesCache[y - 1]?.[x] === TILE.AIR;
      drawTile(ctx, tile, px, py, theme, s.tick, x, y, exposed, s.checkpointActive);
    }
  }

  // Items
  for (const it of s.items ?? []) {
    if (!it.alive) continue;
    const before = prev?.items?.find((p) => p.id === it.id);
    const ix = before ? lerp(before.x, it.x, alpha) : it.x;
    const iy = before ? lerp(before.y, it.y, alpha) : it.y;
    const px = (ix - camX) * TILE_PX;
    if (px + TILE_PX < -32 || px > board.width + 32) continue;
    drawShieldOrb(ctx, px, iy * TILE_PX, s.tick);
  }

  // Enemies
  for (const e of s.enemies ?? []) {
    const squashing = !e.alive && e.dying > 0;
    if (!e.alive && !squashing) continue;
    const before = prev?.enemies?.find((p) => p.id === e.id);
    const ex = before ? lerp(before.x, e.x, alpha) : e.x;
    const ey = before ? lerp(before.y, e.y, alpha) : e.y;
    const px = (ex - camX) * TILE_PX;
    const py = ey * TILE_PX;
    if (px + TILE_PX < -32 || px > board.width + 32) continue;
    if (squashing) {
      drawSquashed(ctx, px, py, e, theme);
    } else if (e.kind === ENEMY.FLYER) {
      drawFlyer(ctx, px, py, s.tick, e.dir, theme);
    } else if (e.kind === ENEMY.SPIKY) {
      drawSpiky(ctx, px, py, s.tick, e.dir, theme);
    } else {
      drawCrawler(ctx, px, py, s.tick, e.dir, theme);
    }
  }

  // Players — draw teammates first, the local player on top.
  const me = localPlayer(s);
  for (let i = s.players.length - 1; i >= 0; i--) {
    const view = s.players[i]!;
    const before = playerIn(prev, view.name);
    const mario = lerpMario(before?.mario, view.mario, alpha);
    const mx = (mario.x - camX) * TILE_PX;
    const my = mario.y * TILE_PX;
    if (mx + TILE_PX < -48 || mx > board.width + 48) continue;
    drawRunner(ctx, mx, my, mario, s.tick, i, view === me);
  }

  drawParticles(ctx, camX);
  ctx.restore();

  // HUD updates — score/coins tick up toward the real values.
  displayScore = tickToward(displayScore, s.score);
  displayCoins = tickToward(displayCoins, s.coins);
  if (s.score > bestScore) bestScore = s.score;
  scoreVal.textContent = String(Math.round(displayScore)).padStart(6, "0");
  bestVal.textContent = String(bestScore).padStart(6, "0");
  coinsVal.textContent = String(Math.round(displayCoins)).padStart(2, "0");
  timeVal.textContent = String(s.timeLeft).padStart(3, "0");
  livesVal.textContent = String(s.lives);
  worldVal.textContent = `${s.world}-${s.stageInWorld}`;
  playersVal.textContent = s.players.length > 1 ? `${s.players.length}P CO-OP` : "";
  stageNameEl.textContent = s.stageName.toUpperCase();

  gameOverOverlay.classList.toggle("show", s.gameOver);
  winOverlay.classList.toggle("show", s.won);
  pauseOverlay.classList.toggle("show", s.paused && !s.gameOver && !s.won);
  stageClearOverlay.classList.toggle("show", s.stageClear);
  stageClearName.textContent = `${s.world}-${s.stageInWorld}  ${s.stageName.toUpperCase()}`;
}

// tickToward animates a HUD counter toward its target value.
function tickToward(display: number, target: number): number {
  if (display === target) return target;
  if (display > target) return target; // score reset (restart): snap down
  const step = Math.max(1, Math.ceil((target - display) * 0.12));
  return Math.min(target, display + step);
}

function themeFor(s: Snapshot): WorldTheme {
  return THEMES[s.theme] ?? THEMES[0]!;
}

// One ridge line of a mountain range, layered sines for a natural profile.
function drawRange(
  c: CanvasRenderingContext2D,
  fill: string,
  base: number,
  amp: number,
  speed: number,
  camX: number,
  seed: number,
): void {
  c.fillStyle = fill;
  c.beginPath();
  c.moveTo(0, board.height);
  for (let x = 0; x <= board.width; x += 8) {
    const wx = (x + camX * TILE_PX * speed) * 0.012;
    const ridge =
      Math.sin(wx + seed) * 0.5 +
      Math.sin(wx * 2.17 + seed * 1.9) * 0.3 +
      Math.sin(wx * 5.1 + seed * 0.7) * 0.2;
    c.lineTo(x, base - (ridge + 0.6) * amp);
  }
  c.lineTo(board.width, board.height);
  c.closePath();
  c.fill();
}

function drawBackground(c: CanvasRenderingContext2D, theme: WorldTheme, camX: number, tick: number): void {
  const W = board.width;
  const H = board.height;

  // Sky with a warm glow near the horizon, like the logo's sunset.
  const sky = c.createLinearGradient(0, 0, 0, H);
  sky.addColorStop(0, theme.skyTop);
  sky.addColorStop(0.55, theme.skyBottom);
  sky.addColorStop(1, theme.horizon);
  c.fillStyle = sky;
  c.fillRect(0, -20, W, H + 40);

  // Sun (low ember sun in the ruins theme, pale high sun underground).
  const sunX = W * 0.78;
  const sunY = H * (theme === THEMES[2] ? 0.52 : 0.3);
  const glow = c.createRadialGradient(sunX, sunY, 4, sunX, sunY, 130);
  glow.addColorStop(0, rgba(theme.sun, 0.8));
  glow.addColorStop(0.3, rgba(theme.sun, 0.3));
  glow.addColorStop(1, rgba(theme.sun, 0));
  c.fillStyle = glow;
  c.fillRect(0, 0, W, H);
  c.fillStyle = theme.sun;
  c.beginPath();
  c.arc(sunX, sunY, 20, 0, Math.PI * 2);
  c.fill();

  // Two mountain ranges at different parallax depths, far one hazier.
  drawRange(c, rgba(theme.distant, 0.6), 355, 65, 0.1, camX, 3.7);
  drawRange(c, theme.midground, 425, 75, 0.26, camX, 9.2);

  // Atmospheric haze sitting on the horizon.
  const haze = c.createLinearGradient(0, 290, 0, 430);
  haze.addColorStop(0, rgba(theme.horizon, 0));
  haze.addColorStop(1, rgba(theme.horizon, 0.35));
  c.fillStyle = haze;
  c.fillRect(0, 290, W, 140);

  // Drifting clouds.
  for (let i = 0; i < 4; i++) {
    const cx = mod(i * 230 + 60 - camX * TILE_PX * 0.08 - tick * 0.12, W + 280) - 140;
    const cy = 55 + (i % 3) * 40 + Math.sin(i * 3.1) * 10;
    drawCloud(c, cx, cy, 0.75 + (i % 3) * 0.25, theme);
  }

  if (theme === THEMES[0]) {
    // Small floating islands, a nod to the logo art.
    for (let i = 0; i < 2; i++) {
      const ix = mod(i * 470 + 220 - camX * TILE_PX * 0.16, W + 320) - 160;
      const iy = 105 + i * 55;
      c.save();
      c.globalAlpha = 0.85;
      c.fillStyle = "#6f5136";
      c.beginPath();
      c.moveTo(ix - 36, iy);
      c.lineTo(ix + 36, iy);
      c.lineTo(ix + 20, iy + 16);
      c.lineTo(ix + 4, iy + 30);
      c.lineTo(ix - 16, iy + 18);
      c.closePath();
      c.fill();
      c.fillStyle = theme.grass;
      c.fillRect(ix - 40, iy - 7, 80, 8);
      c.fillStyle = theme.grassDark;
      c.fillRect(ix - 40, iy - 1, 80, 3);
      c.restore();
    }

    // Birds gliding across the sky.
    c.strokeStyle = "rgba(25, 35, 60, 0.6)";
    c.lineWidth = 1.5;
    for (let i = 0; i < 3; i++) {
      const bx = mod(i * 300 + 900 - tick * (0.9 + i * 0.2), W + 100) - 50;
      const by = 70 + i * 30 + Math.sin(tick / 12 + i * 2) * 5;
      const flap = 3 + Math.sin(tick / 5 + i) * 3;
      c.beginPath();
      c.moveTo(bx - 6, by);
      c.quadraticCurveTo(bx - 3, by - flap, bx, by);
      c.quadraticCurveTo(bx + 3, by - flap, bx + 6, by);
      c.stroke();
    }
  } else if (theme === THEMES[1]) {
    c.fillStyle = "rgba(215, 250, 245, 0.2)";
    for (let x = 40; x < W; x += 130) c.fillRect(x, 100 + ((tick / 8) % 30), 4, 90);
  } else {
    c.fillStyle = "rgba(255, 125, 68, 0.22)";
    for (let x = 0; x < W; x += 90) {
      c.beginPath();
      c.arc(x + 45, 390, 25 + Math.sin((tick + x) / 10) * 6, Math.PI, 0);
      c.fill();
    }
  }
}

function drawTile(
  c: CanvasRenderingContext2D,
  tile: number,
  px: number,
  py: number,
  theme: WorldTheme,
  tick: number,
  tileX: number,
  tileY: number,
  exposed: boolean,
  checkpointActive: boolean,
): void {
  const size = TILE_PX;
  const h = tileHash(tileX, tileY);

  if (tile === TILE.COIN) {
    const bob = Math.sin(tick / 7) * 2;
    c.fillStyle = theme.coin;
    c.beginPath();
    c.ellipse(px + size / 2, py + size / 2 + bob, size / 5, size / 4, 0, 0, Math.PI * 2);
    c.fill();
    c.strokeStyle = "rgba(85, 52, 22, 0.7)";
    c.lineWidth = 2;
    c.stroke();
    c.fillStyle = "rgba(255,255,255,0.85)";
    c.fillRect(px + size / 2 - 4, py + size / 2 + bob - 6, 2, 3);
    return;
  }

  if (tile === TILE.FLAG) {
    c.fillStyle = "#d8e7e3";
    c.fillRect(px + size / 2 - 2, py, 4, size);
    c.fillStyle = theme.accent;
    c.beginPath();
    c.moveTo(px + size / 2 + 2, py + 4);
    c.lineTo(px + size / 2 + 18, py + 12);
    c.lineTo(px + size / 2 + 2, py + 20);
    c.fill();
    return;
  }

  if (tile === TILE.CHECKPOINT) {
    // Mid-stage checkpoint: a small pennant pole, gold once activated.
    const poleX = px + size / 2 - 1;
    c.fillStyle = "#7a7f88";
    c.fillRect(poleX, py - size, 3, size * 2 - 2);
    c.fillStyle = "rgba(0,0,0,0.3)";
    c.fillRect(px + size / 2 - 5, py + size - 4, 11, 4);
    c.fillStyle = checkpointActive ? "#f2c14e" : "#9aa4b2";
    c.beginPath();
    c.moveTo(poleX + 3, py - size + 2);
    c.lineTo(poleX + 17, py - size + 8);
    c.lineTo(poleX + 3, py - size + 14);
    c.closePath();
    c.fill();
    if (checkpointActive) {
      c.fillStyle = rgba("#f2c14e", 0.35 + Math.sin(tick / 6) * 0.15);
      c.beginPath();
      c.arc(poleX + 1, py - size + 8, 12, 0, Math.PI * 2);
      c.fill();
    }
    return;
  }

  if (tile === TILE.PIPE) {
    c.fillStyle = theme.pipe;
    c.fillRect(px + 2, py, size - 4, size);
    if (exposed) {
      // Rim only on the top segment of the pipe.
      c.fillStyle = theme.accent;
      c.fillRect(px, py, size, 8);
    }
    c.fillStyle = "rgba(0, 0, 0, 0.32)";
    c.fillRect(px + 4, py + (exposed ? 8 : 0), 4, size - (exposed ? 8 : 0));
    c.fillStyle = "rgba(255, 255, 255, 0.18)";
    c.fillRect(px + size - 8, py + (exposed ? 8 : 0), 3, size - (exposed ? 8 : 0));
    return;
  }

  if (tile === TILE.QUESTION) {
    c.fillStyle = theme.question;
    c.fillRect(px + 1, py + 1, size - 2, size - 2);
    c.fillStyle = "rgba(76, 45, 25, 0.42)";
    c.fillRect(px + 1, py + 1, size - 2, 4);
    c.fillStyle = "#000";
    c.font = "bold 18px 'Press Start 2P', monospace";
    c.textAlign = "center";
    c.fillText(tick % 90 < 72 ? "?" : "!", px + size / 2, py + size / 2 + 6);
    return;
  }

  if (tile === TILE.BRICK) {
    c.fillStyle = theme.brick;
    c.fillRect(px, py, size, size);
    // Per-tile tint variation so walls don't look like wallpaper.
    c.fillStyle = rgba("#000000", (h & 3) * 0.035);
    c.fillRect(px, py, size, size);
    c.fillStyle = "rgba(255,255,255,0.12)";
    c.fillRect(px, py, size, 2);
    // Running-bond mortar lines.
    c.strokeStyle = "rgba(40, 20, 10, 0.45)";
    c.lineWidth = 1.5;
    c.beginPath();
    c.moveTo(px, py + size / 2);
    c.lineTo(px + size, py + size / 2);
    c.moveTo(px + size / 2, py);
    c.lineTo(px + size / 2, py + size / 2);
    c.moveTo(px + size / 4, py + size / 2);
    c.lineTo(px + size / 4, py + size);
    c.moveTo(px + (size * 3) / 4, py + size / 2);
    c.lineTo(px + (size * 3) / 4, py + size);
    c.stroke();
    return;
  }

  // Ground: textured soil, capped with grass wherever it faces the sky.
  c.fillStyle = theme.ground;
  c.fillRect(px, py, size, size);

  // Soil speckles and the odd embedded stone, stable per tile.
  c.fillStyle = "rgba(0,0,0,0.22)";
  for (let i = 0; i < 4; i++) {
    const sx = px + 2 + ((h >>> (i * 4)) & 15) * 1.8;
    const sy = py + 10 + ((h >>> (i * 4 + 2)) & 15) * 1.3;
    c.fillRect(sx, sy, 3, 2);
  }
  c.fillStyle = "rgba(255,255,255,0.14)";
  for (let i = 0; i < 3; i++) {
    const sx = px + 3 + ((h >>> (i * 5 + 3)) & 15) * 1.7;
    const sy = py + 12 + ((h >>> (i * 5 + 6)) & 15) * 1.2;
    c.fillRect(sx, sy, 2, 2);
  }
  if ((h & 7) === 0) {
    const sx = px + 6 + (h & 15);
    const sy = py + 16 + ((h >>> 6) & 7);
    c.fillStyle = "#a8886a";
    c.fillRect(sx, sy, 6, 4);
    c.fillStyle = "rgba(0,0,0,0.25)";
    c.fillRect(sx, sy + 3, 6, 1);
  }
  c.fillStyle = "rgba(0,0,0,0.22)";
  c.fillRect(px, py + size - 4, size, 4);

  if (exposed) {
    // Grass cap with an uneven soil line.
    c.fillStyle = theme.grass;
    c.fillRect(px, py, size, 7);
    c.fillStyle = theme.grassDark;
    for (let i = 0; i < 8; i++) {
      c.fillRect(px + i * 4, py + 5 + ((h >>> i) & 1) * 2, 4, 3);
    }
    // Blades poking above the edge.
    c.fillStyle = theme.grass;
    for (let i = 0; i < 4; i++) {
      const bx = px + 2 + ((h >>> (i * 3)) & 7) * 3.5;
      c.fillRect(bx, py - 3 - ((h >>> (i + 8)) & 1), 2, 4);
    }
    // Occasional tiny wildflower, like the logo's grass blocks.
    if (h % 11 === 0) {
      const fx = px + 8 + ((h >>> 9) & 15);
      c.fillStyle = theme.grassDark;
      c.fillRect(fx + 1, py - 4, 1, 4);
      c.fillStyle = "#f6f2ec";
      c.fillRect(fx - 1, py - 7, 2, 2);
      c.fillRect(fx + 3, py - 7, 2, 2);
      c.fillRect(fx + 1, py - 9, 2, 2);
      c.fillRect(fx + 1, py - 5, 2, 2);
      c.fillStyle = "#f2c14e";
      c.fillRect(fx + 1, py - 7, 2, 2);
    }
  } else {
    c.fillStyle = "rgba(0,0,0,0.15)";
    c.fillRect(px, py, size, 2);
  }
}

// Hero palettes: player one matches the logo; co-op partners get their own
// suit colors so everyone can find themselves on screen.
interface HeroPalette {
  suit: string;
  suitDark: string;
  trim: string;
  hair: string;
  hairLight: string;
}

const HERO_PALETTES: readonly HeroPalette[] = [
  { suit: "#2b59c3", suitDark: "#1d3f8f", trim: "#d9342b", hair: "#5b3a21", hairLight: "#7c512c" },
  { suit: "#c23b2b", suitDark: "#8f2a1d", trim: "#2b59c3", hair: "#2d2d2d", hairLight: "#4c4c4c" },
  { suit: "#1f8a4c", suitDark: "#14603a", trim: "#e8a83a", hair: "#5b3a21", hairLight: "#7c512c" },
  { suit: "#7d3fbf", suitDark: "#5a2a8f", trim: "#f2c14e", hair: "#20140a", hairLight: "#43301c" },
];

const HERO_COMMON = {
  skin: "#f2b482",
  glove: "#1c1c22",
  white: "#f4f7ff",
  belt: "#26262e",
  buckle: "#f2c14e",
  flame: "#5abeff",
  flameCore: "#eaf8ff",
} as const;

function drawRunner(
  c: CanvasRenderingContext2D,
  px: number,
  py: number,
  mario: MarioState,
  tick: number,
  playerIndex: number,
  isLocal: boolean,
): void {
  const pal = HERO_PALETTES[playerIndex % HERO_PALETTES.length]!;
  const w = TILE_PX * 0.85;
  const h = TILE_PX;
  const dir = mario.facing > 0 ? 1 : -1;

  c.save();

  if (mario.dead) {
    // Death tumble: spin and fade around the sprite center.
    c.globalAlpha = 0.85;
    c.translate(px + w / 2, py + h / 2);
    c.rotate(tick * 0.25 * dir);
    c.translate(-(px + w / 2), -(py + h / 2));
  } else if (mario.invuln > 0 && Math.floor(tick / 3) % 2 === 0) {
    // Post-hit invulnerability blink.
    c.globalAlpha = 0.35;
  }

  if (mario.crouch && !mario.dead) {
    // The server's Y is the top of the shrunken crouch box; draw the whole
    // sprite squashed toward the feet.
    const footY = py + crouchPx();
    c.translate(px + w / 2, footY);
    c.scale(1.06, crouchPx() / h);
    c.translate(-(px + w / 2), -footY);
    py = footY - h;
  }

  // Shield aura.
  if (mario.shield && !mario.dead) {
    const pulse = 0.22 + Math.sin(tick / 5) * 0.08;
    c.fillStyle = rgba(HERO_COMMON.flame, pulse);
    c.beginPath();
    c.ellipse(px + w / 2, py + h / 2, w * 0.85, h * 0.72, 0, 0, Math.PI * 2);
    c.fill();
    c.strokeStyle = rgba(HERO_COMMON.flame, pulse + 0.25);
    c.lineWidth = 2;
    c.stroke();
  }

  // Ground shadow when airborne — gives depth and weight
  if (!mario.onGround && !mario.dead) {
    const shadowW = w * 0.7;
    const shadowY = py + h - 2;
    c.fillStyle = "rgba(0,0,0,0.25)";
    c.beginPath();
    c.ellipse(px + w / 2, shadowY, shadowW / 2, 4, 0, 0, Math.PI * 2);
    c.fill();
  }

  const walking = mario.onGround && Math.abs(mario.vx) > 0.02 && !mario.dead;
  const walkFrame = walking ? Math.floor(tick / 6) % 2 : 0;
  const jumping = !mario.onGround && mario.vy < -0.05 && !mario.dead;
  const falling = !mario.onGround && mario.vy >= -0.05 && !mario.dead;

  const stretchY = jumping ? -1 : falling ? 1 : 0;
  const bodyX = px;
  const bodyW = w;
  const bodyY = py - stretchY;
  const centerX = bodyX + bodyW / 2;

  // --- Jet pack and boost trail (behind the body, on the trailing side) ---
  const packX = dir > 0 ? bodyX - 2 : bodyX + bodyW - 4;
  if (!mario.onGround && !mario.dead) {
    c.fillStyle = "#9aa4b2";
    c.fillRect(packX, bodyY + 15, 6, 9);
    c.fillStyle = "#6d7684";
    c.fillRect(packX, bodyY + 22, 6, 2);
    const flick = (Math.floor(tick / 2) % 2) * 3;
    const flameLen = (jumping ? 14 : 8) + flick;
    c.fillStyle = rgba(HERO_COMMON.flame, 0.85);
    c.beginPath();
    c.moveTo(packX, bodyY + 24);
    c.lineTo(packX + 6, bodyY + 24);
    c.lineTo(packX + 3 - dir * 3, bodyY + 24 + flameLen);
    c.closePath();
    c.fill();
    c.fillStyle = HERO_COMMON.flameCore;
    c.fillRect(packX + 2, bodyY + 24, 2, flameLen - 5);
  }

  // --- Legs: trim-colored suit legs, suit-colored boots with pale soles ---
  const legY = bodyY + h - 10;
  const stride = walking ? (walkFrame === 0 ? 3 : -3) : 0;

  c.fillStyle = pal.trim;
  if (walking) {
    c.fillRect(bodyX + 6, legY + stride, 9, 8 - Math.abs(stride));
    c.fillRect(bodyX + bodyW - 15, legY - stride, 9, 8 - Math.abs(stride));
  } else if (jumping) {
    c.fillRect(bodyX + 8, legY + 2, 8, 6);
    c.fillRect(bodyX + bodyW - 16, legY + 2, 8, 6);
  } else if (falling) {
    c.fillRect(bodyX + 4, legY, 9, 8);
    c.fillRect(bodyX + bodyW - 13, legY, 9, 8);
  } else {
    c.fillRect(bodyX + 7, legY, 8, 8);
    c.fillRect(bodyX + bodyW - 15, legY, 8, 8);
  }

  c.fillStyle = pal.suit;
  c.fillRect(bodyX + 5, legY + 6 + stride, 11, 4);
  c.fillRect(bodyX + bodyW - 16, legY + 6 - stride, 11, 4);
  c.fillStyle = HERO_COMMON.white;
  c.fillRect(bodyX + 5, legY + 9 + stride, 11, 2);
  c.fillRect(bodyX + bodyW - 16, legY + 9 - stride, 11, 2);

  // --- Torso: suit chest, trim shoulder armor, white emblem, belt ---
  c.fillStyle = pal.suit;
  c.fillRect(bodyX + 5, bodyY + 15, bodyW - 10, 8);
  c.fillStyle = pal.suitDark;
  c.fillRect(bodyX + 5, bodyY + 21, bodyW - 10, 2);
  c.fillStyle = pal.trim;
  c.fillRect(bodyX + 5, bodyY + 14, 4, 4);
  c.fillRect(bodyX + bodyW - 9, bodyY + 14, 4, 4);
  // White wing emblem.
  c.fillStyle = HERO_COMMON.white;
  c.fillRect(centerX - 4, bodyY + 17, 3, 2);
  c.fillRect(centerX + 1, bodyY + 17, 3, 2);
  c.fillRect(centerX - 1, bodyY + 19, 2, 2);
  // Belt with buckle.
  c.fillStyle = HERO_COMMON.belt;
  c.fillRect(bodyX + 5, bodyY + 22, bodyW - 10, 3);
  c.fillStyle = HERO_COMMON.buckle;
  c.fillRect(centerX - 2, bodyY + 22, 4, 3);

  // --- Arms: trim sleeves ending in black fingerless gloves ---
  const shoulderY = bodyY + 15;
  if (jumping || falling) {
    // Leading fist punches forward, like the logo pose.
    c.fillStyle = pal.trim;
    c.fillRect(dir > 0 ? bodyX + bodyW - 7 : bodyX - 3, shoulderY + 1, 10, 4);
    c.fillStyle = HERO_COMMON.glove;
    c.fillRect(dir > 0 ? bodyX + bodyW + 2 : bodyX - 7, shoulderY, 5, 6);
    // Trailing arm swept back.
    c.fillStyle = pal.trim;
    c.fillRect(dir > 0 ? bodyX - 3 : bodyX + bodyW - 7, shoulderY + 4, 8, 4);
    c.fillStyle = HERO_COMMON.glove;
    c.fillRect(dir > 0 ? bodyX - 6 : bodyX + bodyW + 2, shoulderY + 5, 4, 5);
  } else if (walking) {
    const swing = walkFrame === 0 ? 2 : -2;
    c.fillStyle = pal.trim;
    c.fillRect(bodyX + (dir > 0 ? -1 : bodyW - 4), shoulderY + 2 + swing, 5, 6);
    c.fillRect(bodyX + (dir > 0 ? bodyW - 4 : -1), shoulderY + 2 - swing, 5, 6);
    c.fillStyle = HERO_COMMON.glove;
    c.fillRect(bodyX + (dir > 0 ? -1 : bodyW - 4), shoulderY + 7 + swing, 5, 3);
    c.fillRect(bodyX + (dir > 0 ? bodyW - 4 : -1), shoulderY + 7 - swing, 5, 3);
  } else {
    c.fillStyle = pal.trim;
    c.fillRect(bodyX + 2, shoulderY + 2, 5, 6);
    c.fillRect(bodyX + bodyW - 7, shoulderY + 2, 5, 6);
    c.fillStyle = HERO_COMMON.glove;
    c.fillRect(bodyX + 2, shoulderY + 7, 5, 3);
    c.fillRect(bodyX + bodyW - 7, shoulderY + 7, 5, 3);
  }

  // --- Head: tan skin, spiky hair swept back ---
  c.fillStyle = HERO_COMMON.skin;
  c.fillRect(bodyX + 7, bodyY + 6, bodyW - 14, 10);
  // Ear on the trailing side.
  c.fillRect(dir > 0 ? bodyX + 5 : bodyX + bodyW - 9, bodyY + 10, 3, 4);

  // Hair mass and a short fringe over the forehead.
  c.fillStyle = pal.hair;
  c.fillRect(bodyX + 6, bodyY + 2, bodyW - 12, 5);
  c.fillRect(dir > 0 ? bodyX + bodyW - 11 : bodyX + 6, bodyY + 6, 5, 2);
  // Spikes leaning away from the direction of travel.
  for (let i = 0; i < 3; i++) {
    const sx = bodyX + 8 + i * ((bodyW - 16) / 2);
    c.beginPath();
    c.moveTo(sx - 2, bodyY + 3);
    c.lineTo(sx - dir * 3, bodyY - 1 - (i % 2) * 2);
    c.lineTo(sx + 2, bodyY + 3);
    c.closePath();
    c.fill();
  }
  c.fillStyle = pal.hairLight;
  c.fillRect(bodyX + 8, bodyY + 3, bodyW - 16, 1);

  // Eye, brow, and a hint of a grin on the leading side.
  c.fillStyle = "#1b1b1b";
  const eyeX = dir > 0 ? bodyX + bodyW - 12 : bodyX + 9;
  c.fillRect(eyeX, bodyY + 10, 3, 3);
  c.fillRect(eyeX - 1, bodyY + 8, 4, 1);
  c.fillRect(dir > 0 ? bodyX + bodyW - 12 : bodyX + 9, bodyY + 14, 4, 1);

  c.restore();

  // Teammate marker so co-op partners can spot each other.
  if (!isLocal && !mario.dead) {
    c.fillStyle = rgba(pal.suit, 0.9);
    c.beginPath();
    c.moveTo(px + w / 2 - 5, py - 12);
    c.lineTo(px + w / 2 + 5, py - 12);
    c.lineTo(px + w / 2, py - 5);
    c.closePath();
    c.fill();
  }
}

// crouchPx is the crouched sprite height in pixels (mirrors server crouchH).
function crouchPx(): number {
  return TILE_PX * 0.6;
}

function drawCrawler(
  c: CanvasRenderingContext2D,
  px: number,
  py: number,
  tick: number,
  dir: number,
  theme: WorldTheme,
): void {
  const w = TILE_PX * 0.8;
  const h = TILE_PX * 0.8;
  const step = Math.floor(tick / 8) % 2; // waddle animation
  const look = dir < 0 ? -1 : 1;

  // Original round crawler body.
  c.fillStyle = theme.brick;
  c.beginPath();
  c.ellipse(px + w / 2, py + h / 2 + 3, w / 2, h / 2 - 3, 0, 0, Math.PI * 2);
  c.fill();

  // Lighter belly
  c.fillStyle = theme.accent;
  c.beginPath();
  c.ellipse(px + w / 2, py + h / 2 + 6, w / 2 - 4, h / 2 - 7, 0, 0, Math.PI * 2);
  c.fill();

  // Feet — alternate for a waddle
  c.fillStyle = "#3a2410";
  if (step === 0) {
    c.fillRect(px + 3, py + h - 4, 9, 5);
    c.fillRect(px + w - 12, py + h - 2, 9, 5);
  } else {
    c.fillRect(px + 3, py + h - 2, 9, 5);
    c.fillRect(px + w - 12, py + h - 4, 9, 5);
  }

  // Eyes look toward travel direction
  const eyeShift = look * 2;
  c.fillStyle = "#fff";
  c.fillRect(px + 6, py + 7, 7, 9);
  c.fillRect(px + w - 13, py + 7, 7, 9);
  c.fillStyle = "#000";
  c.fillRect(px + 8 + eyeShift, py + 10, 3, 4);
  c.fillRect(px + w - 11 + eyeShift, py + 10, 3, 4);

  // Angry eyebrows
  c.fillStyle = "#000";
  c.fillRect(px + 5, py + 5, 9, 3);
  c.fillRect(px + w - 14, py + 5, 9, 3);
}

// drawSpiky is a crawler that grew a mohawk of spikes — stomping it is fatal
// for the player, and the silhouette warns about that from a distance.
function drawSpiky(
  c: CanvasRenderingContext2D,
  px: number,
  py: number,
  tick: number,
  dir: number,
  theme: WorldTheme,
): void {
  const w = TILE_PX * 0.8;
  const h = TILE_PX * 0.8;
  const step = Math.floor(tick / 8) % 2;
  const look = dir < 0 ? -1 : 1;

  // Spikes first, poking above the shell.
  c.fillStyle = "#d8d2c8";
  for (let i = 0; i < 5; i++) {
    const sx = px + 3 + i * ((w - 6) / 4);
    c.beginPath();
    c.moveTo(sx - 3, py + 8);
    c.lineTo(sx, py - 4 - (i % 2) * 2);
    c.lineTo(sx + 3, py + 8);
    c.closePath();
    c.fill();
  }

  // Dark shell body.
  c.fillStyle = "#5c3140";
  c.beginPath();
  c.ellipse(px + w / 2, py + h / 2 + 3, w / 2, h / 2 - 3, 0, 0, Math.PI * 2);
  c.fill();
  c.fillStyle = rgba(theme.accent, 0.35);
  c.beginPath();
  c.ellipse(px + w / 2, py + h / 2 + 7, w / 2 - 5, h / 2 - 8, 0, 0, Math.PI * 2);
  c.fill();

  // Feet
  c.fillStyle = "#2a1810";
  if (step === 0) {
    c.fillRect(px + 3, py + h - 4, 9, 5);
    c.fillRect(px + w - 12, py + h - 2, 9, 5);
  } else {
    c.fillRect(px + 3, py + h - 2, 9, 5);
    c.fillRect(px + w - 12, py + h - 4, 9, 5);
  }

  // Menacing red eyes.
  const eyeShift = look * 2;
  c.fillStyle = "#ffdd57";
  c.fillRect(px + 6, py + 9, 6, 7);
  c.fillRect(px + w - 12, py + 9, 6, 7);
  c.fillStyle = "#c62828";
  c.fillRect(px + 7 + eyeShift, py + 11, 3, 4);
  c.fillRect(px + w - 11 + eyeShift, py + 11, 3, 4);
}

// drawFlyer hovers with flapping wings; its bob comes from the server.
function drawFlyer(
  c: CanvasRenderingContext2D,
  px: number,
  py: number,
  tick: number,
  dir: number,
  theme: WorldTheme,
): void {
  const w = TILE_PX * 0.8;
  const h = TILE_PX * 0.8;
  const flap = Math.sin(tick / 3) * 6;
  const look = dir < 0 ? -1 : 1;

  // Wings behind the body.
  c.fillStyle = "rgba(240, 248, 255, 0.85)";
  c.beginPath();
  c.ellipse(px + 4, py + h / 2 - 2 + flap * 0.4, 9, 4, -0.5 - flap * 0.06, 0, Math.PI * 2);
  c.fill();
  c.beginPath();
  c.ellipse(px + w - 4, py + h / 2 - 2 + flap * 0.4, 9, 4, 0.5 + flap * 0.06, 0, Math.PI * 2);
  c.fill();

  // Round body.
  c.fillStyle = theme.pipe;
  c.beginPath();
  c.ellipse(px + w / 2, py + h / 2, w / 2 - 2, h / 2 - 4, 0, 0, Math.PI * 2);
  c.fill();
  c.fillStyle = rgba("#ffffff", 0.25);
  c.beginPath();
  c.ellipse(px + w / 2, py + h / 2 - 4, w / 2 - 7, h / 2 - 9, 0, 0, Math.PI * 2);
  c.fill();

  // Single big eye looking in the travel direction.
  c.fillStyle = "#fff";
  c.beginPath();
  c.arc(px + w / 2 + look * 3, py + h / 2 - 1, 6, 0, Math.PI * 2);
  c.fill();
  c.fillStyle = "#000";
  c.beginPath();
  c.arc(px + w / 2 + look * 5, py + h / 2 - 1, 3, 0, Math.PI * 2);
  c.fill();

  // Tail feather.
  c.fillStyle = rgba(theme.accent, 0.8);
  c.beginPath();
  c.moveTo(px + w / 2 - look * (w / 2 - 3), py + h / 2);
  c.lineTo(px + w / 2 - look * (w / 2 + 6), py + h / 2 - 3 + flap * 0.3);
  c.lineTo(px + w / 2 - look * (w / 2 + 4), py + h / 2 + 4);
  c.closePath();
  c.fill();
}

// drawSquashed renders a freshly stomped enemy: flattened and fading out.
function drawSquashed(
  c: CanvasRenderingContext2D,
  px: number,
  py: number,
  e: EnemyState,
  theme: WorldTheme,
): void {
  const w = TILE_PX * 0.8;
  const h = TILE_PX * 0.8;
  const t = e.dying / 36; // 1 → fresh stomp, 0 → gone
  c.save();
  c.globalAlpha = Math.min(1, t + 0.2);
  const squashH = h * (0.25 + 0.1 * t);
  c.fillStyle = e.kind === ENEMY.SPIKY ? "#5c3140" : theme.brick;
  c.beginPath();
  c.ellipse(px + w / 2, py + h - squashH / 2, w / 2 + 3, squashH / 2, 0, 0, Math.PI * 2);
  c.fill();
  c.fillStyle = rgba(theme.accent, 0.5);
  c.beginPath();
  c.ellipse(px + w / 2, py + h - squashH / 2 + 1, w / 2 - 3, squashH / 3, 0, 0, Math.PI * 2);
  c.fill();
  c.restore();
}

// drawShieldOrb is the sliding shield power-up: a pulsing energy orb.
function drawShieldOrb(c: CanvasRenderingContext2D, px: number, py: number, tick: number): void {
  const size = TILE_PX * 0.7;
  const cx = px + size / 2;
  const cy = py + size / 2;
  const pulse = Math.sin(tick / 5) * 0.15;

  const glow = c.createRadialGradient(cx, cy, 2, cx, cy, size);
  glow.addColorStop(0, "rgba(90, 190, 255, 0.6)");
  glow.addColorStop(1, "rgba(90, 190, 255, 0)");
  c.fillStyle = glow;
  c.fillRect(cx - size, cy - size, size * 2, size * 2);

  c.fillStyle = "#5abeff";
  c.beginPath();
  c.arc(cx, cy, size / 2 - 2 + pulse * 4, 0, Math.PI * 2);
  c.fill();
  c.strokeStyle = "#eaf8ff";
  c.lineWidth = 2;
  c.beginPath();
  c.arc(cx, cy, size / 2 + 1 + pulse * 6, 0, Math.PI * 2);
  c.stroke();
  c.fillStyle = "#eaf8ff";
  c.fillRect(cx - 4, cy - 4, 3, 3);
}

function drawParticles(c: CanvasRenderingContext2D, camX: number): void {
  const next: Particle[] = [];
  for (const particle of particles) {
    particle.x += particle.vx;
    particle.y += particle.vy;
    particle.vy += 0.12;
    particle.life--;
    if (particle.life <= 0) continue;
    const alpha = Math.min(1, particle.life / 18);
    c.globalAlpha = alpha;
    c.fillStyle = particle.color;
    c.fillRect(particle.x - camX * TILE_PX, particle.y, particle.size, particle.size);
    next.push(particle);
  }
  c.globalAlpha = 1;
  particles = next;
}

function drawCloud(
  c: CanvasRenderingContext2D,
  x: number,
  y: number,
  scale: number,
  theme: WorldTheme,
): void {
  c.save();
  c.globalAlpha = theme === THEMES[0] ? 0.85 : 0.35;
  c.fillStyle = "#ffffff";
  c.beginPath();
  c.ellipse(x + 22 * scale, y + 2, 34 * scale, 11 * scale, 0, 0, Math.PI * 2);
  c.arc(x + 6 * scale, y - 2, 12 * scale, 0, Math.PI * 2);
  c.arc(x + 24 * scale, y - 9 * scale, 15 * scale, 0, Math.PI * 2);
  c.arc(x + 40 * scale, y - 3, 11 * scale, 0, Math.PI * 2);
  c.fill();
  // Shaded underside gives the cloud volume.
  c.globalAlpha *= 0.35;
  c.fillStyle = "#93a9cf";
  c.beginPath();
  c.ellipse(x + 24 * scale, y + 7 * scale, 28 * scale, 5 * scale, 0, 0, Math.PI * 2);
  c.fill();
  c.restore();
}
