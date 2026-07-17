// Super Mario client — renders server-authoritative state on canvas.

interface MarioState {
  x: number;
  y: number;
  vx: number;
  vy: number;
  onGround: boolean;
  facing: number;
  dead: boolean;
}

interface EnemyState {
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
  tiles: number[][];
  mario: MarioState;
  enemies: EnemyState[];
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
} as const;

const TILE_PX = 32;
const LEVEL_H = 15;
const VIEW_W = 20;

const TILE_COLORS: Record<number, string> = {
  [TILE.GROUND]: "#c84c0c",
  [TILE.BRICK]: "#b83c0c",
  [TILE.QUESTION]: "#f8b800",
  [TILE.PIPE]: "#00a800",
  [TILE.FLAG]: "#f8f8f8",
  [TILE.COIN]: "#f8d878",
};

interface WorldTheme {
  skyTop: string;
  skyBottom: string;
  distant: string;
  midground: string;
  ground: string;
  brick: string;
  pipe: string;
  question: string;
  coin: string;
  accent: string;
}

const THEMES: readonly WorldTheme[] = [
  {
    skyTop: "#4ca6ea", skyBottom: "#b9edff", distant: "#83c76a",
    midground: "#3d985d", ground: "#9a5a32", brick: "#bb7744",
    pipe: "#4b9b69", question: "#e8a83a", coin: "#ffe278", accent: "#f7f4cf",
  },
  {
    skyTop: "#183c53", skyBottom: "#397080", distant: "#275565",
    midground: "#163d4b", ground: "#4f6770", brick: "#8c6c56",
    pipe: "#2f9a83", question: "#e2a34b", coin: "#f7d16e", accent: "#c7edf1",
  },
  {
    skyTop: "#31264f", skyBottom: "#b04a46", distant: "#55334f",
    midground: "#2e2639", ground: "#45404a", brick: "#76606a",
    pipe: "#80525e", question: "#d58046", coin: "#ffd36a", accent: "#ff9560",
  },
];

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
const coinsVal = el("coinsVal");
const timeVal = el("timeVal");
const livesVal = el("livesVal");
const worldVal = el("worldVal");
const stageNameEl = el("stageName");
const statusEl = el("status");
const fullscreenBtn = el<HTMLButtonElement>("fullscreenBtn");
const soundBtn = el<HTMLButtonElement>("soundBtn");
const gameOverOverlay = el("gameOverOverlay");
const winOverlay = el("winOverlay");
const pauseOverlay = el("pauseOverlay");
const stageClearOverlay = el("stageClearOverlay");
const stageClearName = el("stageClearName");

let state: Snapshot | null = null;
let previousState: Snapshot | null = null;
let ws: WebSocket | null = null;
let displayCameraX = 0;
let particles: Particle[] = [];

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
      this.context = new AudioContext();
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

  play(kind: "jump" | "point" | "stomp" | "hurt" | "clear"): void {
    if (this.muted) return;
    this.unlock();
    if (!this.context || !this.master) return;

    const settings = {
      jump: [440, 640, 0.10, "square"],
      point: [660, 990, 0.14, "sine"],
      stomp: [170, 95, 0.10, "square"],
      hurt: [220, 90, 0.22, "sawtooth"],
      clear: [523, 1046, 0.35, "triangle"],
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
    previousState = state;
    state = JSON.parse(e.data) as Snapshot;
    handleSnapshotChange(previousState, state);
    render();
  };
}
connect();

function handleSnapshotChange(previous: Snapshot | null, current: Snapshot): void {
  audio.setTheme(current.theme);
  if (!previous) {
    displayCameraX = current.cameraX;
    return;
  }

  const enemyDefeated = previous.enemies.some((previousEnemy) => {
    const currentEnemy = current.enemies.find((enemy) => enemy.id === previousEnemy.id);
    return previousEnemy.alive && currentEnemy !== undefined && !currentEnemy.alive;
  });
  const earnedPoint = current.score > previous.score && !current.stageClear;

  if (enemyDefeated) {
    audio.play("stomp");
    emitParticles(current.mario.x, current.mario.y + 0.6, 6, "#f7f4cf");
  } else if (earnedPoint) {
    audio.play("point");
    emitParticles(current.mario.x, current.mario.y, 8, themeFor(current).coin);
  }
  if (current.mario.dead && !previous.mario.dead) {
    audio.play("hurt");
    emitParticles(current.mario.x, current.mario.y, 12, "#ff795e");
  }
  if (current.stageClear && !previous.stageClear) {
    audio.play("clear");
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

async function toggleFullscreen(): Promise<void> {
  try {
    if (document.fullscreenElement) {
      await document.exitFullscreen();
    } else {
      await document.documentElement.requestFullscreen();
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

document.addEventListener("fullscreenchange", () => {
  fullscreenBtn.textContent = document.fullscreenElement ? "EXIT" : "FULL";
});

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
    case "ArrowDown":  case "KeyS": send(ACTION.DOWN); break;
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
      send(ACTION.DOWN_END);
      break;
  }
});

function render(): void {
  if (!state) return;
  const s = state;
  const theme = themeFor(s);
  displayCameraX += (s.cameraX - displayCameraX) * 0.22;
  const camX = displayCameraX;

  drawBackground(ctx, theme, camX, s.tick);

  // Tiles
  const startCol = Math.floor(camX);
  const endCol = Math.min(startCol + VIEW_W + 1, s.tiles[0]?.length ?? 0);

  for (let y = 0; y < s.tiles.length; y++) {
    const row = s.tiles[y]!;
    for (let x = startCol; x < endCol; x++) {
      const tile = row[x]!;
      if (tile === TILE.AIR) continue;
      const px = (x - camX) * TILE_PX;
      const py = y * TILE_PX;
      if (px + TILE_PX < 0 || px > board.width) continue;
      drawTile(ctx, tile, px, py, theme, s.tick);
    }
  }

  // Enemies
  for (const e of s.enemies) {
    if (!e.alive) continue;
    const px = (e.x - camX) * TILE_PX;
    const py = e.y * TILE_PX;
    if (px + TILE_PX < -32 || px > board.width + 32) continue;
    drawCrawler(ctx, px, py, s.tick, e.dir, theme);
  }

  // Mario
  if (!s.mario.dead) {
    const mx = (s.mario.x - camX) * TILE_PX;
    const my = s.mario.y * TILE_PX;
    drawRunner(ctx, mx, my, s.mario, s.tick, theme);
  }

  drawParticles(ctx, camX);

  // HUD updates
  scoreVal.textContent = String(s.score).padStart(6, "0");
  coinsVal.textContent = String(s.coins).padStart(2, "0");
  timeVal.textContent = String(s.timeLeft).padStart(3, "0");
  livesVal.textContent = String(s.lives);
  worldVal.textContent = `${s.world}-${s.stageInWorld}`;
  stageNameEl.textContent = s.stageName.toUpperCase();

  gameOverOverlay.classList.toggle("show", s.gameOver);
  winOverlay.classList.toggle("show", s.won);
  pauseOverlay.classList.toggle("show", s.paused && !s.gameOver && !s.won);
  stageClearOverlay.classList.toggle("show", s.stageClear);
  stageClearName.textContent = `${s.world}-${s.stageInWorld}  ${s.stageName.toUpperCase()}`;
}

function themeFor(s: Snapshot): WorldTheme {
  return THEMES[s.theme] ?? THEMES[0]!;
}

function drawBackground(c: CanvasRenderingContext2D, theme: WorldTheme, camX: number, tick: number): void {
  const sky = c.createLinearGradient(0, 0, 0, board.height);
  sky.addColorStop(0, theme.skyTop);
  sky.addColorStop(1, theme.skyBottom);
  c.fillStyle = sky;
  c.fillRect(0, 0, board.width, board.height);

  // Distant silhouettes scroll more slowly than the level.
  const distantOffset = -((camX * TILE_PX * 0.15) % 180);
  c.fillStyle = theme.distant;
  for (let x = distantOffset - 180; x < board.width + 180; x += 180) {
    c.beginPath();
    c.arc(x + 90, 285, 100, Math.PI, 0);
    c.arc(x + 145, 315, 68, Math.PI, 0);
    c.lineTo(x + 180, board.height);
    c.lineTo(x, board.height);
    c.fill();
  }

  const midOffset = -((camX * TILE_PX * 0.38 + tick * 0.18) % 140);
  c.fillStyle = theme.midground;
  for (let x = midOffset - 140; x < board.width + 140; x += 140) {
    c.fillRect(x + 20, 330, 16, 84);
    c.beginPath();
    c.arc(x + 28, 320, 32, 0, Math.PI * 2);
    c.fill();
    c.beginPath();
    c.arc(x + 58, 333, 27, 0, Math.PI * 2);
    c.fill();
  }

  if (theme === THEMES[0]) {
    drawCloud(c, 75 - (camX * 4) % 720, 115);
    drawCloud(c, 410 - (camX * 3) % 720, 70);
  } else if (theme === THEMES[1]) {
    c.fillStyle = "rgba(215, 250, 245, 0.2)";
    for (let x = 40; x < board.width; x += 130) c.fillRect(x, 100 + ((tick / 8) % 30), 4, 90);
  } else {
    c.fillStyle = "rgba(255, 125, 68, 0.22)";
    for (let x = 0; x < board.width; x += 90) {
      c.beginPath();
      c.arc(x + 45, 390, 25 + Math.sin((tick + x) / 10) * 6, Math.PI, 0);
      c.fill();
    }
  }
}

function drawTile(c: CanvasRenderingContext2D, tile: number, px: number, py: number, theme: WorldTheme, tick: number): void {
  const color = tile === TILE.GROUND ? theme.ground :
    tile === TILE.BRICK ? theme.brick :
    tile === TILE.QUESTION ? theme.question :
    tile === TILE.PIPE ? theme.pipe :
    tile === TILE.COIN ? theme.coin : "#d9e5de";
  const size = TILE_PX;

  if (tile === TILE.COIN) {
    const bob = Math.sin(tick / 7) * 2;
    c.fillStyle = theme.coin;
    c.beginPath();
    c.ellipse(px + size / 2, py + size / 2 + bob, size / 5, size / 4, 0, 0, Math.PI * 2);
    c.fill();
    c.strokeStyle = "rgba(85, 52, 22, 0.7)";
    c.lineWidth = 2;
    c.stroke();
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

  if (tile === TILE.PIPE) {
    c.fillStyle = theme.pipe;
    c.fillRect(px + 2, py, size - 4, size);
    c.fillStyle = theme.accent;
    c.fillRect(px, py, size, 8);
    c.fillStyle = "rgba(0, 0, 0, 0.32)";
    c.fillRect(px + 4, py + 8, 4, size - 8);
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

  // Ground / brick
  c.fillStyle = color;
  c.fillRect(px + 1, py + 1, size - 2, size - 2);
  c.fillStyle = "rgba(0,0,0,0.25)";
  c.fillRect(px + 1, py + size - 5, size - 2, 4);
  if (tile === TILE.BRICK) {
    c.strokeStyle = "rgba(0,0,0,0.3)";
    c.lineWidth = 1;
    c.strokeRect(px + 1, py + 1, size - 2, size - 2);
    c.beginPath();
    c.moveTo(px + 1, py + size / 2);
    c.lineTo(px + size - 1, py + size / 2);
    c.stroke();
  }
}

function drawRunner(
  c: CanvasRenderingContext2D,
  px: number,
  py: number,
  mario: MarioState,
  tick: number,
  theme: WorldTheme,
): void {
  const w = TILE_PX * 0.85;
  const h = TILE_PX;
  const facing = mario.facing;
  const dir = facing > 0 ? 1 : -1;

  // Ground shadow when airborne — gives depth and weight
  if (!mario.onGround) {
    const shadowW = w * 0.7;
    const shadowY = py + h - 2;
    c.fillStyle = "rgba(0,0,0,0.25)";
    c.beginPath();
    c.ellipse(px + w / 2, shadowY, shadowW / 2, 4, 0, 0, Math.PI * 2);
    c.fill();
  }

  const walking = mario.onGround && Math.abs(mario.vx) > 0.02;
  const walkFrame = walking ? Math.floor(tick / 6) % 2 : 0;
  const jumping = !mario.onGround && mario.vy < -0.05;
  const falling = !mario.onGround && mario.vy >= -0.05;

  // Subtle squash/stretch for jump arc
  let stretchY = 0;
  let squashX = 0;
  if (jumping) {
    stretchY = -1;
    squashX = 0;
  } else if (falling) {
    stretchY = 1;
    squashX = 0;
  }

  const bodyX = px + squashX / 2;
  const bodyW = w - squashX;
  const bodyY = py - stretchY;

  // --- Legs (drawn first, behind body) ---
  const legY = bodyY + h - 10;
  c.fillStyle = "#275a73";
  if (walking) {
    const stride = walkFrame === 0 ? 3 : -3;
    c.fillRect(bodyX + 6, legY + stride, 9, 10 - Math.abs(stride));
    c.fillRect(bodyX + bodyW - 15, legY - stride, 9, 10 - Math.abs(stride));
  } else if (jumping) {
    // Tucked jump pose
    c.fillRect(bodyX + 8, legY + 2, 8, 8);
    c.fillRect(bodyX + bodyW - 16, legY + 2, 8, 8);
  } else if (falling) {
    // Spread fall pose
    c.fillRect(bodyX + 4, legY, 9, 10);
    c.fillRect(bodyX + bodyW - 13, legY, 9, 10);
  } else {
    // Standing
    c.fillRect(bodyX + 7, legY, 8, 10);
    c.fillRect(bodyX + bodyW - 15, legY, 8, 10);
  }

  // Shoes
  c.fillStyle = "#6b3a1f";
  if (walking) {
    const stride = walkFrame === 0 ? 3 : -3;
    c.fillRect(bodyX + 5, legY + 8 + stride, 11, 4);
    c.fillRect(bodyX + bodyW - 16, legY + 8 - stride, 11, 4);
  } else {
    c.fillRect(bodyX + 5, legY + 8, 11, 4);
    c.fillRect(bodyX + bodyW - 16, legY + 8, 11, 4);
  }

  // Teal runner jacket and dark trousers: an original hero silhouette.
  c.fillStyle = "#275a73";
  c.fillRect(bodyX + 5, bodyY + 18, bodyW - 10, h - 28);
  // Warm jacket / arms.
  c.fillStyle = theme.accent;
  c.fillRect(bodyX + 3, bodyY + 14, bodyW - 6, 10);
  if (jumping || falling) {
    // Arms raised/out during jump
    const armY = bodyY + 12 + (jumping ? -2 : 0);
    c.fillRect(bodyX + (dir > 0 ? -2 : bodyW - 4), armY, 5, 8);
    c.fillRect(bodyX + (dir > 0 ? bodyW - 3 : 1), armY, 5, 8);
  } else if (walking) {
    const swing = walkFrame === 0 ? 2 : -2;
    c.fillRect(bodyX + (dir > 0 ? -1 : bodyW - 5), bodyY + 16 + swing, 5, 7);
    c.fillRect(bodyX + (dir > 0 ? bodyW - 4 : 0), bodyY + 16 - swing, 5, 7);
  }

  // Head / face
  c.fillStyle = "#f8a870";
  c.fillRect(bodyX + 7, bodyY + 6, bodyW - 14, 11);
  // Ear
  c.fillRect(bodyX + (dir > 0 ? bodyW - 10 : 4), bodyY + 9, 4, 5);

  // Original hood and hair band.
  c.fillStyle = "#493049";
  c.fillRect(bodyX + 5, bodyY + 2, bodyW - 5, 7);
  c.fillStyle = theme.accent;
  c.fillRect(bodyX + 5, bodyY + 7, bodyW - 5, 2);

  // Eye and hair fringe.
  c.fillStyle = "#000";
  const eyeX = dir > 0 ? bodyX + bodyW - 16 : bodyX + 10;
  c.fillRect(eyeX, bodyY + 9, 3, 4);
  c.fillStyle = "#f8a870";
  c.fillRect(eyeX + dir * 2, bodyY + 12, 4, 3);
  c.fillStyle = "#000";
  c.fillRect(bodyX + (dir > 0 ? bodyW - 18 : 8), bodyY + 14, 7, 2);
  c.fillRect(bodyX + (dir > 0 ? bodyW - 12 : 6), bodyY + 10, 3, 5);
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

function drawCloud(c: CanvasRenderingContext2D, x: number, y: number): void {
  c.fillStyle = "rgba(255,255,255,0.85)";
  c.beginPath();
  c.arc(x, y, 18, 0, Math.PI * 2);
  c.arc(x + 22, y - 6, 22, 0, Math.PI * 2);
  c.arc(x + 44, y, 18, 0, Math.PI * 2);
  c.arc(x + 22, y + 4, 16, 0, Math.PI * 2);
  c.fill();
}
