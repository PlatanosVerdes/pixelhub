// The display's six clock faces, redrawn from photos of the real screen so the page can show the
// time the way the display does. Style numbers are the ones the display takes.

const SIZE = 16;
// A colour picked for the clock replaces every part that is red by default.
const RED = "red", DARK_RED = "dark-red";
const WHITE = [225, 225, 255], CYAN = [40, 200, 255], BLUE = [20, 60, 255];

const BIG = {
  0: ["01110", "10001", "10011", "10101", "11001", "10001", "01110"],
  1: ["00100", "01100", "00100", "00100", "00100", "00100", "11111"],
  2: ["01110", "10001", "00001", "00010", "00100", "01000", "11111"],
  3: ["11111", "00010", "00100", "00010", "00001", "10001", "01110"],
  4: ["00010", "00110", "01010", "10010", "11111", "00010", "00010"],
  5: ["11111", "10000", "11110", "00001", "00001", "10001", "01110"],
  6: ["00110", "01000", "10000", "11110", "10001", "10001", "01110"],
  7: ["11111", "00001", "00010", "00100", "01000", "01000", "01000"],
  8: ["01110", "10001", "10001", "01110", "10001", "10001", "01110"],
  9: ["01110", "10001", "10001", "01111", "00001", "00010", "01100"],
};
const SMALL = {
  0: ["111", "101", "101", "101", "111"], 1: ["010", "110", "010", "010", "111"], 2: ["111", "001", "111", "100", "111"],
  3: ["111", "001", "111", "001", "111"], 4: ["101", "101", "111", "001", "001"], 5: ["111", "100", "111", "001", "111"],
  6: ["111", "100", "111", "101", "111"], 7: ["111", "001", "010", "010", "010"], 8: ["111", "101", "111", "101", "111"],
  9: ["111", "101", "111", "001", "111"],
};

class Grid {
  constructor(fill = [0, 0, 0]) { this.px = Array.from({ length: SIZE * SIZE }, () => fill); }
  set(x, y, c) { if (x >= 0 && y >= 0 && x < SIZE && y < SIZE) this.px[y * SIZE + x] = c; }
  glyph(rows, x, y, c) { rows.forEach((row, dy) => [...row].forEach((b, dx) => { if (b === "1") this.set(x + dx, y + dy, c); })); }
  line(x0, y0, x1, y1, c) {
    const steps = Math.max(Math.abs(x1 - x0), Math.abs(y1 - y0), 1);
    for (let i = 0; i <= steps; i++) this.set(Math.round(x0 + ((x1 - x0) * i) / steps), Math.round(y0 + ((y1 - y0) * i) / steps), c);
  }
}

const digits = (n) => String(n).padStart(2, "0").split("").map(Number);

function stacked(g, d, colour) {
  const [h1, h2] = digits(d.getHours()), [m1, m2] = digits(d.getMinutes());
  g.glyph(BIG[h1], 3, 1, colour); g.glyph(BIG[h2], 9, 1, colour);
  g.set(1, 10, colour); g.set(1, 13, colour);
  g.glyph(BIG[m1], 3, 9, colour); g.glyph(BIG[m2], 9, 9, colour);
}

function hands(g, d, cx, cy, hour, minute) {
  const m = d.getMinutes(), h = (d.getHours() % 12) + m / 60;
  const tip = (turns, len) => [cx + Math.sin(turns * 2 * Math.PI) * len, cy - Math.cos(turns * 2 * Math.PI) * len];
  g.line(cx, cy, ...tip(h / 12, 3.2), hour);
  g.line(cx, cy, ...tip(m / 60, 5.2), minute);
}

const faces = [
  (g, d) => stacked(g, d, RED),
  (g, d) => {
    for (let x = 0; x < SIZE; x++) {
      const hue = (x / SIZE) * 360;
      g.set(x, 3, hsl(200 - hue)); g.set(x, 11, hsl(240 + hue));
    }
    [...digits(d.getHours()), ...digits(d.getMinutes())].forEach((n, i) => g.glyph(SMALL[n], i * 4 + (i > 1 ? 1 : 0), 5, RED));
  },
  (g, d) => {
    for (let i = 0; i < SIZE; i++) { g.set(i, 0, CYAN); g.set(i, 15, CYAN); g.set(0, i, CYAN); g.set(15, i, CYAN); }
    const [h1, h2] = digits(d.getHours()), [m1, m2] = digits(d.getMinutes());
    g.glyph(SMALL[h1], 5, 2, RED); g.glyph(SMALL[h2], 9, 2, RED);
    g.set(3, 9, RED); g.set(3, 11, RED);
    g.glyph(SMALL[m1], 5, 8, RED); g.glyph(SMALL[m2], 9, 8, RED);
  },
  (g, d) => {
    for (let y = 0; y < SIZE; y++) for (let x = 0; x < SIZE; x++) {
      const r = Math.hypot(x - 7.5, y - 7.5);
      if (r <= 6.6) g.set(x, y, DARK_RED); else if (r <= 7.9) g.set(x, y, WHITE);
    }
    hands(g, d, 7, 8, [255, 120, 60], BLUE);
  },
  (g, d) => {
    g.px = g.px.map(() => RED);
    stacked(g, d, [0, 0, 0]);
  },
  (g, d) => {
    for (let i = 0; i < 4; i++) {
      for (const [x, y] of [[1 + i, 0], [0, 1 + i], [14 - i, 0], [15, 1 + i], [1 + i, 15], [0, 14 - i], [14 - i, 15], [15, 14 - i]]) g.set(x, y, RED);
    }
    for (const [x, y, w, h] of [[6, 0, 4, 2], [6, 14, 4, 2], [0, 6, 2, 4], [14, 6, 2, 4]]) {
      for (let dy = 0; dy < h; dy++) for (let dx = 0; dx < w; dx++) g.set(x + dx, y + dy, WHITE);
    }
    g.set(7, 0, RED); g.set(8, 0, RED); g.set(7, 15, RED); g.set(8, 15, RED); g.set(0, 7, RED); g.set(0, 8, RED); g.set(15, 7, RED); g.set(15, 8, RED);
    for (const [x, y] of [[7, 8], [8, 8], [7, 9], [8, 9]]) g.set(x, y, WHITE);
    hands(g, d, 7.5, 7.5, RED, BLUE);
  },
];

function hsl(h) {
  const k = (n) => (n + ((((h % 360) + 360) % 360) / 30)) % 12;
  const f = (n) => 1 - Math.max(-1, Math.min(k(n) - 3, 9 - k(n), 1));
  return [f(0), f(8), f(4)].map((v) => Math.round(v * 255));
}

const rgb = (hex) => /^#[0-9a-f]{6}$/i.test(hex ?? "") ? [1, 3, 5].map((i) => parseInt(hex.slice(i, i + 2), 16)) : null;

export const CLOCK_STYLES = faces.length;

// A PNG data URL of clock face `style` at time d, at the display's own 16x16 size.
export function clockFace(style, d = new Date(), colour = "") {
  const g = new Grid();
  (faces[style] ?? faces[0])(g, d);
  const red = rgb(colour) ?? [255, 30, 40];
  const tints = { [RED]: red, [DARK_RED]: red.map((v) => Math.round(v * 0.75)) };
  g.px = g.px.map((c) => tints[c] ?? c);
  const canvas = document.createElement("canvas");
  canvas.width = canvas.height = SIZE;
  const ctx = canvas.getContext("2d");
  const img = ctx.createImageData(SIZE, SIZE);
  g.px.forEach(([r, gr, b], i) => img.data.set([r, gr, b, 255], i * 4));
  ctx.putImageData(img, 0, 0);
  return canvas.toDataURL();
}
