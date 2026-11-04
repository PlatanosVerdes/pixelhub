// pixelhub's web UI: the app element fetches, every component below renders what it is given.
import { LitElement, html, svg, css, nothing, keyed, repeat } from "./vendor/lit-all.min.js";
import { lang, t } from "./i18n.js";

document.documentElement.lang = lang;

const STATUS_MS = 3000;
const STATE_MS = 15000;

class ApiError extends Error {
  constructor(message, status = 0) { super(message); this.status = status; }
}

async function api(method, path, body, init = {}) {
  const req = { method, headers: {}, ...init };
  if (body instanceof FormData) req.body = body;
  else if (body !== undefined) { req.body = JSON.stringify(body); req.headers["Content-Type"] = "application/json"; }
  let res;
  try { res = await fetch(`api/${path}`, req); } catch { throw new ApiError(t("Could not reach pixelhub")); }
  const text = await res.text();
  let data = null;
  try { data = text ? JSON.parse(text) : null; } catch { /* not JSON */ }
  if (!res.ok) throw new ApiError(data?.error ?? t("pixelhub answered HTTP {status}", { status: res.status }), res.status);
  return data;
}

const clone = (x) => structuredClone(x);
const same = (a, b) => JSON.stringify(a) === JSON.stringify(b);
const errorLines = (e) => String(e.message ?? e).split("\n").map((s) => s.trim().replace(/^(invalid|conflict|not found): /, "")).filter(Boolean);

// Times and durations.
const known = (at) => { const d = at ? new Date(at) : null; return d && !isNaN(d) && d.getFullYear() > 2000 ? d : null; };
const hhmm = (d) => d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", hourCycle: "h23" });
const weekday = (d) => d.toLocaleDateString(lang, { weekday: "short" }).replace(".", "");
const until = (at) => {
  const d = known(at);
  if (!d) return "";
  const today = new Date();
  return d.toDateString() === today.toDateString() ? hhmm(d) : `${weekday(d)} ${hhmm(d)}`;
};
const ago = (at) => {
  const d = known(at);
  if (!d) return t("never");
  const s = Math.max(0, Math.round((Date.now() - d) / 1000));
  if (s < 60) return t("{s} s ago", { s });
  if (s < 3600) return t("{m} min ago", { m: Math.round(s / 60) });
  return t("{h} h {m} min ago", { h: Math.floor(s / 3600), m: Math.round((s % 3600) / 60) });
};
const seconds = (d) => {
  let total = 0;
  for (const [, n, unit] of String(d ?? "").matchAll(/([\d.]+)(ms|h|m|s)/g)) total += Number(n) * { h: 3600, m: 60, s: 1, ms: 0.001 }[unit];
  return total;
};
const span = (d) => {
  const s = seconds(d);
  if (s >= 3600) return t("{n} h", { n: s / 3600 });
  return s >= 60 ? t("{n} min", { n: Math.round(s / 60) }) : t("{n} s", { n: Math.round(s) });
};
const minutesOf = (d) => Math.round((seconds(d) / 60) * 100) / 100;
const durationOf = (minutes) => `${Math.round(Number(minutes) * 60)}s`;
const minutesLeftToday = () => { const n = new Date(); return Math.max(1, Math.ceil((new Date(n.getFullYear(), n.getMonth(), n.getDate() + 1) - n) / 60000)); };

// Weekdays, Monday first, and a window's clock times in minutes after midnight.
const DAY_KEYS = ["mon", "tue", "wed", "thu", "fri", "sat", "sun"];
const dayLabel = (i) => new Date(2024, 0, 1 + i).toLocaleDateString(lang, { weekday: "short" }).replace(".", "");
const parseDays = (text) => {
  const set = new Set();
  for (const raw of String(text ?? "").toLowerCase().split(",")) {
    const part = raw.trim();
    if (!part || part === "daily") continue;
    const [a, b] = part.split("-").map((d) => DAY_KEYS.indexOf(d.trim()));
    const end = part.includes("-") ? b : a;
    if (a < 0 || end < 0) continue;
    for (let i = a; ; i = (i + 1) % 7) { set.add(i); if (i === end) break; }
  }
  return set.size ? set : new Set([0, 1, 2, 3, 4, 5, 6]);
};
const formatDays = (set) => {
  if (set.size === 0 || set.size === 7) return "daily";
  const parts = [];
  for (let i = 0; i < 7;) {
    if (!set.has(i)) { i++; continue; }
    let j = i;
    while (set.has(j + 1) && j + 1 < 7) j++;
    if (j - i >= 2) parts.push(`${DAY_KEYS[i]}-${DAY_KEYS[j]}`);
    else for (let k = i; k <= j; k++) parts.push(DAY_KEYS[k]);
    i = j + 1;
  }
  return parts.join(",");
};
const clock = (text) => {
  const m = /^(\d{1,2}):(\d{2})$/.exec(String(text ?? "").trim());
  return m ? Number(m[1]) * 60 + Number(m[2]) : NaN;
};
const tidyTime = (text) => {
  const digits = String(text).trim();
  if (/^\d{1,2}:\d{2}$/.test(digits)) return digits.padStart(5, "0");
  const m = /^(\d{1,2})(\d{2})?$/.exec(digits);
  return m ? `${m[1].padStart(2, "0")}:${m[2] ?? "00"}` : digits;
};
const windowActive = (w, day, minute) => {
  const from = clock(w.from), to = clock(w.to);
  if (isNaN(from) || isNaN(to)) return false;
  if (from < to) return w.set.has(day) && minute >= from && minute < to;
  if (minute >= from) return w.set.has(day);
  return minute < to && w.set.has((day + 6) % 7);
};
// Which slot of the schedule holds this moment: its index, or -1 for the default channel.
const matchingSlot = (slots, day, minute) => slots.findIndex((s) => windowActive(s, day, minute));
const withSets = (slots) => slots.map((s) => ({ ...s, set: parseDays(s.days) }));
const dayOf = (d) => (d.getDay() + 6) % 7;
const minuteOf = (d) => d.getHours() * 60 + d.getMinutes();
const channelColor = (names, name) => ["#4b4fd6", "#11a36a", "#e08a2c", "#d4477b", "#2f9bd6", "#8a63d2", "#b8a21f", "#5f7f8f"][Math.max(0, names.indexOf(name)) % 8];

const icon = (d) => svg`<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${d}</svg>`;
const icons = {
  trash: icon(svg`<path d="M4 7h16M10 11v6M14 11v6M5 7l1 12a2 2 0 0 0 2 2h8a2 2 0 0 0 2-2l1-12M9 7V4h6v3"/>`),
  chevron: icon(svg`<path d="m6 9 6 6 6-6"/>`),
  up: icon(svg`<path d="m6 15 6-6 6 6"/>`),
  down: icon(svg`<path d="m6 9 6 6 6-6"/>`),
  plus: icon(svg`<path d="M12 5v14M5 12h14"/>`),
  close: icon(svg`<path d="M6 6l12 12M18 6 6 18"/>`),
  upload: icon(svg`<path d="M12 16V4M7 9l5-5 5 5M4 16v3a1 1 0 0 0 1 1h14a1 1 0 0 0 1-1v-3"/>`),
  locate: icon(svg`<circle cx="12" cy="12" r="3.5"/><path d="M12 2v4M12 18v4M2 12h4M18 12h4"/>`),
};

const base = css`
  :host { display: block; }
  * { box-sizing: border-box; }
  .card { background: var(--surface); border: 1px solid var(--line); border-radius: 14px; padding: 18px; min-width: 0; }
  .muted { color: var(--muted); }
  .mono { font-family: var(--font-data); font-variant-numeric: tabular-nums; }
  .head { display: flex; align-items: center; gap: 10px; margin: 0 0 12px; min-height: 28px; }
  h2 { font-size: 14px; color: var(--muted); margin: 0; font-weight: 600; flex: 1; }
  .small { font-size: 12px; }
  .label { display: block; font-size: 12px; color: var(--muted); margin-bottom: 4px; font-weight: 500; }
  .hint { font-size: 12px; color: var(--muted); margin: 8px 0 0; }
  .err { font-size: 12px; color: var(--bad); margin: 4px 0 0; }
  .errors { margin: 12px 0 0; padding: 0; list-style: none; }
  .dot { width: 8px; height: 8px; border-radius: 50%; flex: none; display: inline-block; background: var(--muted); }
  .dot.ok { background: var(--ok); box-shadow: 0 0 0 3px color-mix(in srgb, var(--ok) 25%, transparent); }
  .dot.warn { background: var(--warn); box-shadow: 0 0 0 3px color-mix(in srgb, var(--warn) 25%, transparent); }
  .dot.bad { background: var(--bad); box-shadow: 0 0 0 3px color-mix(in srgb, var(--bad) 25%, transparent); }
  svg { width: 18px; height: 18px; display: block; }

  input, select { font: inherit; font-size: 16px; color: var(--ink); background: var(--bg); border: 1px solid var(--line); border-radius: 9px; height: 38px; padding: 0 10px; min-width: 0; }
  input[type=number] { font-family: var(--font-data); }
  input.time { font-family: var(--font-data); width: 68px; text-align: center; padding: 0 6px; }
  input:focus-visible, select:focus-visible, button:focus-visible, textarea:focus-visible { outline: 2px solid var(--accent); outline-offset: 1px; }
  input.bad, select.bad { border-color: var(--bad); }
  input[type=range] { height: 24px; padding: 0; accent-color: var(--accent); background: none; border: 0; }
  input[type=color] { padding: 2px; width: 44px; cursor: pointer; }
  @media (min-width: 700px) { input, select { font-size: 14px; height: 34px; } }

  button { font: inherit; color: inherit; }
  .btn { height: 38px; padding: 0 14px; border-radius: 10px; border: 1px solid var(--line); background: var(--surface); font-size: 14px; font-weight: 500; cursor: pointer; white-space: nowrap; display: inline-flex; align-items: center; gap: 6px; }
  .btn:hover { border-color: var(--muted); }
  .btn.primary { background: var(--accent); border-color: var(--accent); color: var(--on-accent); font-weight: 600; }
  .btn.primary:hover { filter: brightness(1.08); }
  .btn:disabled { opacity: .45; cursor: default; filter: none; }
  .btn.small { height: 32px; padding: 0 11px; font-size: 13px; }
  .btn svg { width: 16px; height: 16px; }
  @media (min-width: 700px) { .btn { height: 34px; } .btn.small { height: 30px; } }
  .icon { width: 32px; height: 32px; padding: 0; border: 0; border-radius: 8px; background: none; color: var(--muted); cursor: pointer; display: inline-flex; align-items: center; justify-content: center; flex: none; }
  .icon:hover { background: var(--chip); color: var(--ink); }
  .icon:disabled, .icon[aria-disabled=true] { opacity: .35; cursor: default; }
  .icon:disabled:hover, .icon[aria-disabled=true]:hover { background: none; color: var(--muted); }
  .icon.trash:hover:not(:disabled) { color: var(--bad); }

  .chip { display: inline-flex; align-items: center; gap: 2px; background: var(--chip); border-radius: 999px; padding: 0 4px 0 10px; height: 26px; font-family: var(--font-data); font-size: 12px; }
  .chip.plain { padding: 0 10px; }
  .chip button { border: 0; background: none; padding: 0; width: 20px; height: 20px; border-radius: 50%; color: var(--muted); cursor: pointer; display: inline-flex; align-items: center; justify-content: center; }
  .chip button:hover { color: var(--ink); background: color-mix(in srgb, var(--muted) 20%, transparent); }
  .chip button svg { width: 12px; height: 12px; }
  .tag { display: inline-block; font-size: 11px; font-weight: 600; padding: 1px 8px; border-radius: 999px; background: var(--chip); color: var(--muted); }
  .tag.on { background: color-mix(in srgb, var(--accent) 16%, transparent); color: var(--accent); }

  .seg { display: inline-flex; background: var(--chip); border-radius: 10px; padding: 2px; gap: 2px; flex-wrap: wrap; }
  .seg button { border: 0; background: none; height: 32px; padding: 0 12px; border-radius: 8px; font-size: 13px; font-weight: 500; color: var(--muted); cursor: pointer; }
  .seg button[aria-pressed=true] { background: var(--surface); color: var(--ink); box-shadow: 0 1px 2px rgba(0, 0, 0, .12); }
  .seg button:hover { color: var(--ink); }

  .days { display: inline-flex; gap: 3px; }
  .days button { width: 30px; height: 30px; border: 1px solid var(--line); background: none; border-radius: 7px; font-size: 12px; color: var(--muted); cursor: pointer; padding: 0; text-transform: capitalize; }
  .days button[aria-pressed=true] { background: color-mix(in srgb, var(--accent) 16%, transparent); border-color: color-mix(in srgb, var(--accent) 50%, var(--line)); color: var(--accent); font-weight: 600; }

  .pix { image-rendering: pixelated; }
  @media (prefers-reduced-motion: reduce) { * { animation: none !important; transition: none !important; } }
`;

// Every component sends what it did upward: a toast to show, the state the API answered with, or a request to read the status again.
const send = (el, name, detail) => el.dispatchEvent(new CustomEvent(name, { detail, bubbles: true, composed: true }));
const toast = (el, text, opts = {}) => send(el, "toast", { text, ...opts });
const saved = (el, st) => send(el, "state", st);

// Messages the API wrote about a field: each goes to the first matcher it fits, the rest are returned.
function route(lines, matchers) {
  const by = {};
  const rest = [];
  for (const line of lines) {
    const hit = Object.entries(matchers).find(([, re]) => re.test(line));
    if (hit) (by[hit[0]] ??= []).push(line);
    else rest.push(line);
  }
  return { by, rest };
}
const fieldErr = (lines) => lines?.length ? html`${lines.map((l) => html`<p class="err">${l}</p>`)}` : nothing;

const segmented = (options, value, pick) => html`<div class="seg" role="group">${options.map(([v, label]) =>
  html`<button type="button" aria-pressed=${String(v === value)} @click=${() => pick(v)}>${label}</button>`)}</div>`;

class SpriteGrid extends LitElement {
  static properties = { sprites: { attribute: false }, value: {}, withClock: { type: Boolean } };
  static styles = [base, css`
    .grid { display: flex; flex-wrap: wrap; gap: 6px; }
    button { width: 54px; height: 54px; padding: 3px; border-radius: 10px; border: 1px solid var(--line); background: #000; cursor: pointer; position: relative; display: flex; align-items: center; justify-content: center; }
    button:hover { border-color: var(--muted); }
    button[aria-pressed=true] { border-color: var(--accent); box-shadow: 0 0 0 2px var(--accent); }
    img { width: 46px; height: 46px; display: block; }
    .clock { color: #d8dcf0; font-family: var(--font-data); font-size: 11px; }
  `];
  pick(v) { this.dispatchEvent(new CustomEvent("pick", { detail: v })); }
  render() {
    return html`<div class="grid" role="group">
      ${this.withClock ? html`<button type="button" class="clock" aria-pressed=${String(!this.value)} title=${t("Clock")} @click=${() => this.pick("")}>${t("Clock")}</button>` : nothing}
      ${(this.sprites ?? []).map((n) => html`<button type="button" aria-pressed=${String(this.value === n)} title=${n} aria-label=${n} @click=${() => this.pick(n)}><img class="pix" src="api/sprites/${encodeURIComponent(n)}.png" alt="" loading="lazy"></button>`)}
    </div>`;
  }
}
customElements.define("ph-sprite-grid", SpriteGrid);

class DaysPicker extends LitElement {
  static properties = { value: {} };
  static styles = [base];
  toggle(i) {
    const set = parseDays(this.value);
    if (set.has(i)) set.delete(i); else set.add(i);
    this.dispatchEvent(new CustomEvent("change", { detail: formatDays(set) }));
  }
  render() {
    const set = parseDays(this.value);
    return html`<div class="days" role="group" aria-label=${t("Days")}>${DAY_KEYS.map((_, i) =>
      html`<button type="button" aria-pressed=${String(set.has(i))} @click=${() => this.toggle(i)}>${dayLabel(i)}</button>`)}</div>`;
  }
}
customElements.define("ph-days", DaysPicker);

const timeField = (value, label, onchange, bad) => html`<input class="time ${bad ? "bad" : ""}" type="text" inputmode="numeric" maxlength="5" placeholder="09:00" aria-label=${label}
  .value=${value} @change=${(e) => { e.target.value = tidyTime(e.target.value); onchange(e.target.value); }}>`;

const linkState = (s) => {
  if (s.released_until) return ["warn", t("Released to the phone app")];
  if (s.connected) return ["ok", t("Connected")];
  const ack = known(s.last_ack);
  if (ack && Date.now() - ack < 120000) return ["warn", t("Reconnecting")];
  return ["bad", t("Not connected")];
};

// The channel the schedule picks right now, and the slot that picked it (-1: the default channel).
const scheduledNow = (st, at = new Date()) => {
  const slots = withSets(st.schedule ?? []);
  const i = matchingSlot(slots, dayOf(at), minuteOf(at));
  return { index: i, channel: i < 0 ? st.default_channel : slots[i].channel };
};

class NowCard extends LitElement {
  static properties = { status: { attribute: false }, state: { attribute: false }, offline: { type: Boolean }, busy: { state: true } };
  static styles = [base, css`
    .stage { display: flex; justify-content: center; margin: 2px 0 16px; }
    .frame { background: #05060b; border-radius: 18px; padding: 12px; box-shadow: inset 0 0 0 1px rgba(255, 255, 255, .08), 0 6px 24px rgba(0, 0, 0, .18); position: relative; }
    .frame img { width: 160px; height: 160px; display: block; border-radius: 4px; background: #000; transition: opacity .3s; }
    .frame.released img { opacity: .35; }
    .frame .over { position: absolute; inset: 12px; display: flex; align-items: center; justify-content: center; color: #e9ebf5; font-size: 12px; text-align: center; padding: 8px; }
    @media (min-width: 980px) { .frame img { width: 320px; height: 320px; } .frame { padding: 16px; } .frame .over { inset: 16px; } }
    dl { margin: 0; display: grid; grid-template-columns: max-content 1fr; }
    dt, dd { margin: 0; padding: 8px 0; border-top: 1px solid var(--line); min-width: 0; }
    dt { color: var(--muted); font-size: 12px; padding-right: 16px; display: flex; align-items: center; }
    dd { font-size: 14px; display: flex; align-items: center; gap: 8px; flex-wrap: wrap; justify-content: flex-end; text-align: right; overflow-wrap: anywhere; }
    .actions { margin-top: 14px; display: flex; flex-wrap: wrap; gap: 8px; align-items: center; }
    .banner { margin: 0 0 12px; padding: 10px 12px; border-radius: 10px; font-size: 13px; background: color-mix(in srgb, var(--warn) 14%, transparent); }
    .banner.bad { background: color-mix(in srgb, var(--bad) 14%, transparent); }
  `];

  async call(path, method = "POST") {
    this.busy = true;
    try { await api(method, path); send(this, "refresh"); } catch (e) { toast(this, e.message); }
    this.busy = false;
  }

  why(s) {
    if (s.channel_reason === "override") {
      return s.override_until ? t("Manual override until {time}", { time: until(s.override_until) }) : t("Manual override until the schedule changes");
    }
    const m = this.state ? scheduledNow(this.state) : null;
    return m && m.index < 0 ? t("Default channel, no schedule slot matches now") : t("By the schedule");
  }

  render() {
    const s = this.status;
    if (!s) return html`<section class="card"><div class="head"><h2>${t("Now")}</h2></div><p class="muted">${this.offline ? t("Could not reach pixelhub") : t("Loading")}</p></section>`;
    const [tone, linkText] = linkState(s);
    const released = !!s.released_until;
    return html`<section class="card" aria-labelledby="now-h">
      <div class="head"><h2 id="now-h">${t("Now")}</h2>${s.channel_reason === "override" ? html`<span class="tag on">${t("Override")}</span>` : nothing}</div>
      ${this.offline ? html`<p class="banner bad" role="alert">${t("No answer from pixelhub. Showing the last status.")}</p>` : nothing}
      <div class="stage"><div class="frame ${released ? "released" : ""}">
        <img class="pix" src="api/preview.png?v=${s.preview_version}" alt=${t("What the display should show: {name}", { name: s.showing })}>
        ${released ? html`<div class="over">${t("The phone app has the display")}</div>` : nothing}
      </div></div>
      <dl>
        <dt>${t("Channel")}</dt><dd><strong>${s.channel}</strong></dd>
        <dt>${t("Chosen by")}</dt><dd>${this.why(s)}</dd>
        <dt>${t("Layer")}</dt><dd>${{ alert: t("Alert"), scene: t("Scene"), ambient: t("Ambient") }[s.layer] ?? s.layer}</dd>
        <dt>${t("Showing")}</dt><dd class="mono">${s.showing}</dd>
        <dt>${t("Brightness")}</dt><dd class="mono">${s.brightness}</dd>
        <dt>${t("Connection")}</dt><dd><span class="dot ${tone}"></span>${linkText}</dd>
        <dt>${t("Last ack")}</dt><dd><span title=${known(s.last_ack) ? known(s.last_ack).toLocaleString() : ""}>${ago(s.last_ack)}</span></dd>
        <dt>${t("Bridge")}</dt><dd class="mono small">${s.address}</dd>
      </dl>
      <div class="actions">
        ${released
          ? html`<button class="btn primary" ?disabled=${this.busy} @click=${() => this.call("claim")}>${t("Take back")}</button>
              <span class="small muted">${t("Released until {time}", { time: until(s.released_until) })}</span>`
          : html`<button class="btn" ?disabled=${this.busy} @click=${() => this.call("release")}>${t("Release")}</button>
              <span class="small muted">${t("Lets the phone app use the display.")}</span>`}
      </div>
    </section>`;
  }
}
customElements.define("ph-now", NowCard);

class QuickSwitch extends LitElement {
  static properties = { state: { attribute: false }, status: { attribute: false }, span: { state: true }, busy: { state: true } };
  static styles = [base, css`
    .row { margin-bottom: 12px; }
    .row .label { margin-bottom: 6px; }
    .row .seg { display: flex; }
    .row .seg button { flex: 1 1 auto; padding: 0 8px; white-space: nowrap; }
    .tiles { display: grid; grid-template-columns: repeat(auto-fill, minmax(132px, 1fr)); gap: 8px; }
    .tile { text-align: left; height: auto; min-height: 56px; padding: 9px 12px; border-radius: 12px; border: 1px solid var(--line); background: var(--surface); cursor: pointer; display: flex; flex-direction: column; gap: 2px; min-width: 0; }
    .tile:hover { border-color: var(--muted); }
    .tile[aria-pressed=true] { border-color: var(--accent); box-shadow: 0 0 0 1px var(--accent); }
    .tile b { font-weight: 600; font-size: 14px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .tile span { font-size: 12px; color: var(--muted); }
    .back { margin-top: 12px; display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
  `];
  constructor() { super(); this.span = "schedule"; }

  async pick(name) {
    const body = { name };
    if (this.span === "1h" || this.span === "4h") body.for = this.span;
    if (this.span === "day") body.for = `${minutesLeftToday()}m`;
    this.busy = true;
    try { await api("POST", "channel", body); send(this, "refresh"); } catch (e) { toast(this, e.message); }
    this.busy = false;
  }
  async back() {
    this.busy = true;
    try { await api("DELETE", "channel"); send(this, "refresh"); } catch (e) { toast(this, e.message); }
    this.busy = false;
  }

  render() {
    const st = this.state;
    if (!st) return nothing;
    const active = this.status?.channel;
    const overridden = this.status?.channel_reason === "override";
    const scheduled = scheduledNow(st).channel;
    const spans = [["schedule", t("Next schedule change")], ["1h", span("1h")], ["4h", span("4h")], ["day", t("End of day")]];
    return html`<section class="card" aria-labelledby="sw-h">
      <div class="head"><h2 id="sw-h">${t("Quick switch")}</h2></div>
      <div class="row"><span class="label">${t("For")}</span>${segmented(spans, this.span, (v) => { this.span = v; })}</div>
      <div class="tiles">${st.channels.map((c) => html`<button type="button" class="tile" aria-pressed=${String(c.name === active)} ?disabled=${this.busy} @click=${() => this.pick(c.name)}>
        <b>${c.name}</b><span>${c.name === active ? (overridden ? t("Override") : t("Showing")) : c.name === scheduled ? t("Scheduled now") : ""}&nbsp;</span></button>`)}</div>
      <div class="back"><button class="btn small" ?disabled=${!overridden || this.busy} @click=${() => this.back()}>${t("Back to schedule")}</button>
        <span class="small muted">${overridden ? t("The schedule says {name} now.", { name: scheduled }) : t("No override is active.")}</span></div>
    </section>`;
  }
}
customElements.define("ph-switch", QuickSwitch);

class NotifyCard extends LitElement {
  static properties = { sprites: { attribute: false }, mode: { state: true }, sprite: { state: true }, color: { state: true }, priority: { state: true }, duration: { state: true }, busy: { state: true } };
  static styles = [base, css`
    .form { display: grid; gap: 14px; }
    .line { display: flex; gap: 14px; flex-wrap: wrap; align-items: flex-end; }
    input[type=number] { width: 72px; }
    .foot { display: flex; gap: 10px; align-items: center; flex-wrap: wrap; }
  `];
  constructor() { super(); this.mode = "sprite"; this.sprite = "heart"; this.color = "#ff3b30"; this.priority = 5; this.duration = "10s"; }

  async send() {
    const body = { priority: Number(this.priority), duration: this.duration, source: "ui" };
    if (this.mode === "sprite") body.sprite = this.sprite;
    else body.color = [1, 3, 5].map((i) => parseInt(this.color.slice(i, i + 2), 16));
    this.busy = true;
    try {
      const r = await api("POST", "notify", body);
      if (r.dropped) toast(this, t("Dropped: {channel} does not accept the source ui.", { channel: r.channel }));
      else toast(this, t("Showing {name} on {channel} for {time}", { name: r.showing, channel: r.channel, time: span(r.duration) }));
      send(this, "refresh");
    } catch (e) { toast(this, e.message); }
    this.busy = false;
  }

  render() {
    const names = this.sprites ?? [];
    return html`<section class="card" aria-labelledby="no-h">
      <div class="head"><h2 id="no-h">${t("Test alert")}</h2></div>
      <div class="form">
        <div>${segmented([["sprite", t("Sprite")], ["color", t("Color")]], this.mode, (v) => { this.mode = v; })}</div>
        ${this.mode === "sprite"
          ? html`<ph-sprite-grid .sprites=${names} .value=${this.sprite} @pick=${(e) => { this.sprite = e.detail; }}></ph-sprite-grid>`
          : html`<div class="line"><div><label class="label" for="col">${t("Color")}</label><input id="col" type="color" .value=${this.color} @input=${(e) => { this.color = e.target.value; }}></div></div>`}
        <div class="line">
          <div><label class="label" for="pr">${t("Priority")}</label><input id="pr" type="number" min="0" max="10" step="1" .value=${String(this.priority)} @input=${(e) => { this.priority = e.target.value; }}></div>
          <div><label class="label" for="du">${t("Duration")}</label>
            <select id="du" .value=${this.duration} @change=${(e) => { this.duration = e.target.value; }}>
              ${["10s", "30s", "1m", "5m", "15m", "1h"].map((v) => html`<option value=${v} ?selected=${v === this.duration}>${span(v)}</option>`)}
            </select></div>
        </div>
        <div class="foot"><button class="btn primary" ?disabled=${this.busy} @click=${() => this.send()}>${t("Send alert")}</button>
          <span class="small muted">${t("Sent with the source ui; the active channel must accept it.")}</span></div>
      </div>
    </section>`;
  }
}
customElements.define("ph-notify", NotifyCard);

const releaseDraft = (d) => { const s = seconds(d); return s % 3600 === 0 ? { n: s / 3600, unit: "h" } : { n: Math.round((s / 60) * 100) / 100, unit: "m" }; };
const settingsDraft = (st) => ({
  latitude: String(st.location.latitude), longitude: String(st.location.longitude),
  day: st.brightness.day, dusk: st.brightness.dusk, night: st.brightness.night,
  release: releaseDraft(st.release_for), def: st.default_channel,
});

class SettingsCard extends LitElement {
  static properties = { base: { attribute: false }, draft: { state: true }, errors: { state: true }, busy: { state: true } };
  static styles = [base, css`
    .grid { display: grid; gap: 18px; }
    .pair { display: grid; grid-template-columns: 1fr 1fr; gap: 10px; }
    .pair input { width: 100%; }
    .level { display: grid; grid-template-columns: 52px 1fr 36px; align-items: center; gap: 10px; }
    .level span:first-child { font-size: 13px; color: var(--muted); }
    .level output { font-family: var(--font-data); font-size: 13px; text-align: right; }
    .unit { display: flex; gap: 8px; }
    .unit input { width: 90px; }
    .foot { display: flex; gap: 8px; margin-top: 18px; align-items: center; }
    .where { display: flex; align-items: center; gap: 10px; margin-top: 10px; flex-wrap: wrap; }
    h3 { font-size: 13px; margin: 0 0 8px; font-weight: 600; }
  `];
  willUpdate(c) {
    if (c.has("base") && this.base && (!this.draft || same(this.draft, c.get("base") ? settingsDraft(c.get("base")) : null))) this.draft = settingsDraft(this.base);
  }
  get dirty() { return this.draft && this.base && !same(this.draft, settingsDraft(this.base)); }
  set(key, value) { this.draft = { ...this.draft, [key]: value }; }

  locate() {
    if (!navigator.geolocation || !window.isSecureContext) { toast(this, t("The browser only gives the location to pages served over HTTPS.")); return; }
    navigator.geolocation.getCurrentPosition(
      (p) => { this.draft = { ...this.draft, latitude: p.coords.latitude.toFixed(4), longitude: p.coords.longitude.toFixed(4) }; },
      (e) => toast(this, t("Could not get this device's location: {reason}", { reason: e.message })),
      { timeout: 15000 });
  }

  async save() {
    const d = this.draft;
    this.busy = true;
    this.errors = [];
    try {
      const st = await api("PUT", "settings", {
        location: { latitude: Number(d.latitude), longitude: Number(d.longitude) },
        brightness: { day: Number(d.day), dusk: Number(d.dusk), night: Number(d.night) },
        release_for: `${d.release.n}${d.release.unit}`,
        default_channel: d.def,
      });
      this.draft = settingsDraft(st);
      saved(this, st);
      toast(this, t("Settings saved"));
    } catch (e) {
      this.errors = errorLines(e);
      toast(this, this.errors[0]);
    }
    this.busy = false;
  }

  render() {
    const d = this.draft;
    if (!d) return nothing;
    const { by, rest } = route(this.errors ?? [], { location: /^location/, brightness: /^brightness/, release: /^release_for/, def: /^default_channel/ });
    const level = (key, label) => html`<div class="level"><span>${label}</span>
      <input type="range" min="0" max="100" .value=${String(d[key])} aria-label=${label} @input=${(e) => this.set(key, Number(e.target.value))}><output>${d[key]}</output></div>`;
    return html`<section class="card" aria-labelledby="se-h">
      <div class="head"><h2 id="se-h">${t("Settings")}</h2></div>
      <div class="grid">
        <div><h3>${t("Location")}</h3>
          <div class="pair">
            <div><label class="label" for="lat">${t("Latitude")}</label><input id="lat" type="number" step="any" class=${by.location ? "bad" : ""} .value=${d.latitude} @input=${(e) => this.set("latitude", e.target.value)}></div>
            <div><label class="label" for="lon">${t("Longitude")}</label><input id="lon" type="number" step="any" class=${by.location ? "bad" : ""} .value=${d.longitude} @input=${(e) => this.set("longitude", e.target.value)}></div>
          </div>
          <div class="where"><button class="btn small" @click=${() => this.locate()}>${icons.locate}${t("Use this device's location")}</button></div>
          <p class="hint">${t("The sun at this place decides between day, dusk and night brightness.")}</p>
          ${fieldErr(by.location)}</div>
        <div><h3>${t("Brightness")}</h3>${level("day", t("Day"))}${level("dusk", t("Dusk"))}${level("night", t("Night"))}${fieldErr(by.brightness)}</div>
        <div class="pair">
          <div><label class="label" for="rel">${t("Release duration")}</label>
            <div class="unit"><input id="rel" type="number" min="1" step="any" class=${by.release ? "bad" : ""} .value=${String(d.release.n)} @input=${(e) => this.set("release", { ...d.release, n: e.target.value })}>
              <select aria-label=${t("Unit")} .value=${d.release.unit} @change=${(e) => this.set("release", { ...d.release, unit: e.target.value })}>
                <option value="m" ?selected=${d.release.unit === "m"}>${t("minutes")}</option><option value="h" ?selected=${d.release.unit === "h"}>${t("hours")}</option></select></div>
            ${fieldErr(by.release)}</div>
          <div><label class="label" for="def">${t("Default channel")}</label>
            <select id="def" class=${by.def ? "bad" : ""} .value=${d.def} @change=${(e) => this.set("def", e.target.value)}>
              ${this.base.channels.map((c) => html`<option value=${c.name} ?selected=${c.name === d.def}>${c.name}</option>`)}</select>
            ${fieldErr(by.def)}</div>
        </div>
      </div>
      ${rest.length ? html`<ul class="errors">${rest.map((l) => html`<li class="err">${l}</li>`)}</ul>` : nothing}
      <div class="foot"><button class="btn primary" ?disabled=${!this.dirty || this.busy} @click=${() => this.save()}>${t("Save settings")}</button>
        <button class="btn" ?disabled=${!this.dirty || this.busy} @click=${() => { this.draft = settingsDraft(this.base); this.errors = []; }}>${t("Revert")}</button></div>
    </section>`;
  }
}
customElements.define("ph-settings", SettingsCard);

class Pictures extends LitElement {
  static properties = { channel: {}, files: { state: true }, gone: { state: true }, busy: { state: true }, over: { state: true } };
  static styles = [base, css`
    .grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(92px, 1fr)); gap: 10px; }
    .pic { position: relative; min-width: 0; }
    .pic .img { aspect-ratio: 1; background: #000; border-radius: 10px; border: 1px solid var(--line); overflow: hidden; display: block; }
    .pic img { width: 100%; height: 100%; display: block; }
    .pic .name { font-family: var(--font-data); font-size: 11px; color: var(--muted); margin-top: 4px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .pic .icon { position: absolute; top: 4px; right: 4px; width: 28px; height: 28px; background: rgba(5, 6, 11, .72); color: #e9ebf5; }
    .pic .icon:hover { background: rgba(5, 6, 11, .9); color: #ff9c9c; }
    .pic .icon svg { width: 16px; height: 16px; }
    .add { aspect-ratio: 1; border: 1.5px dashed var(--line); border-radius: 10px; background: none; color: var(--muted); cursor: pointer; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 4px; font-size: 12px; padding: 4px; text-align: center; }
    .add:hover, .grid.over .add { border-color: var(--accent); color: var(--accent); }
    .grid.over { outline: 2px dashed var(--accent); outline-offset: 6px; border-radius: 6px; }
    input[type=file] { display: none; }
  `];
  connectedCallback() { super.connectedCallback(); this.gone = new Set(); this.load(); }
  async load() {
    try { this.files = (await api("GET", `channels/${encodeURIComponent(this.channel)}/pictures`)).pictures ?? []; }
    catch (e) { this.files = this.files ?? []; toast(this, e.message); }
  }
  async upload(list) {
    this.busy = true;
    for (const file of list) {
      const form = new FormData();
      form.append("file", file);
      try { await api("POST", `channels/${encodeURIComponent(this.channel)}/pictures`, form); }
      catch (e) { toast(this, `${file.name}: ${e.message}`); }
    }
    await this.load();
    this.busy = false;
  }
  remove(file) {
    this.gone = new Set(this.gone).add(file);
    const unhide = () => { const h = new Set(this.gone); h.delete(file); this.gone = h; };
    const path = `channels/${encodeURIComponent(this.channel)}/pictures/${encodeURIComponent(file)}`;
    toast(this, t("{file} deleted", { file }), {
      action: { label: t("Undo"), run: unhide },
      expire: async () => {
        try { await api("DELETE", path, undefined, { keepalive: true }); } catch (e) { toast(this, e.message); }
        await this.load();
        unhide();
      },
    });
  }
  render() {
    if (!this.files) return html`<p class="muted small">${t("Loading")}</p>`;
    const shown = this.files.filter((f) => !this.gone.has(f));
    return html`
      <div class="grid ${this.over ? "over" : ""}" @dragover=${(e) => { e.preventDefault(); this.over = true; }} @dragleave=${(e) => { if (!e.currentTarget.contains(e.relatedTarget)) this.over = false; }}
        @drop=${(e) => { e.preventDefault(); this.over = false; this.upload([...e.dataTransfer.files]); }}>
        ${repeat(shown, (f) => f, (f) => html`<div class="pic"><span class="img"><img class="pix" loading="lazy" alt=${f} src="api/channels/${encodeURIComponent(this.channel)}/pictures/${encodeURIComponent(f)}"></span>
          <button class="icon trash" aria-label=${t("Delete {file}", { file: f })} title=${t("Delete")} @click=${() => this.remove(f)}>${icons.trash}</button>
          <div class="name" title=${f}>${f}</div></div>`)}
        <button class="add" ?disabled=${this.busy} @click=${(e) => e.currentTarget.nextElementSibling.click()}>${icons.upload}<span>${this.busy ? t("Uploading…") : t("Add GIF or PNG")}</span></button>
        <input type="file" accept="image/gif,image/png" multiple @change=${(e) => { this.upload([...e.target.files]); e.target.value = ""; }}>
      </div>
      <p class="hint">${shown.length ? t("Drop files here or use the button. Up to 1 MB and 512 by 512 pixels.") : t("No pictures. The display uses the shared ones when there are any. Drop GIF or PNG files here or use the button.")}</p>`;
  }
}
customElements.define("ph-pictures", Pictures);

const brightnessMode = (b) => (b === undefined || b === null ? "auto" : b === 0 ? "off" : "set");
const editorDraft = (c) => ({
  name: c.name,
  sources: [...(c.sources ?? [])],
  ambient: { pictures: minutesOf(c.ambient.pictures), clock: minutesOf(c.ambient.clock) },
  scenes: (c.scenes ?? []).map((s) => ({
    name: s.name, days: s.days, from: s.from, to: s.to, sprite: s.sprite ?? "",
    bmode: brightnessMode(s.brightness), bval: s.brightness > 0 ? s.brightness : 50,
  })),
});
const editorBody = (d) => ({
  name: d.name.trim(),
  sources: d.sources,
  ambient: { pictures: durationOf(d.ambient.pictures || 0), clock: durationOf(d.ambient.clock || 0) },
  scenes: d.scenes.map((s) => {
    const out = { name: s.name, days: s.days, from: s.from, to: s.to };
    if (s.sprite) out.sprite = s.sprite;
    if (s.bmode === "off") out.brightness = "off";
    if (s.bmode === "set") out.brightness = Math.round(Number(s.bval));
    return out;
  }),
});

class ChannelEditor extends LitElement {
  static properties = { channel: { attribute: false }, sprites: { attribute: false }, draft: { state: true }, errors: { state: true }, busy: { state: true }, source: { state: true } };
  static styles = [base, css`
    .sec { padding: 16px 0; border-top: 1px solid var(--line); }
    .sec:first-child { border-top: 0; padding-top: 4px; }
    h3 { font-size: 13px; margin: 0 0 10px; font-weight: 600; display: flex; align-items: center; gap: 8px; }
    h3 .grow { flex: 1; }
    .name { max-width: 280px; width: 100%; font-family: var(--font-data); }
    .chips { display: flex; flex-wrap: wrap; gap: 6px; align-items: center; }
    .chips input { width: 150px; }
    .ambient { display: flex; gap: 14px; flex-wrap: wrap; align-items: flex-end; }
    .ambient input { width: 84px; }
    .mix { display: flex; height: 8px; border-radius: 4px; overflow: hidden; background: var(--chip); margin-top: 12px; max-width: 280px; }
    .mix i { display: block; height: 100%; }
    .scene { border: 1px solid var(--line); border-radius: 12px; padding: 12px; margin-bottom: 10px; display: grid; gap: 12px; }
    .scene .top { display: flex; gap: 8px; align-items: center; }
    .scene .top input { flex: 1; }
    .when { display: flex; gap: 12px 16px; flex-wrap: wrap; align-items: flex-end; }
    .bright { display: flex; gap: 8px; align-items: center; }
    .bright input[type=range] { width: 130px; }
    .bright output { font-family: var(--font-data); font-size: 13px; min-width: 28px; }
    .foot { display: flex; gap: 8px; padding-top: 16px; border-top: 1px solid var(--line); align-items: center; flex-wrap: wrap; }
  `];

  willUpdate(c) {
    if (c.has("channel") && this.channel && (!this.draft || same(this.draft, c.get("channel") ? editorDraft(c.get("channel")) : null))) this.draft = editorDraft(this.channel);
  }
  get dirty() { return this.draft && !same(this.draft, editorDraft(this.channel)); }
  patch(change) { this.draft = { ...this.draft, ...change }; }
  setScene(i, change) { this.patch({ scenes: this.draft.scenes.map((s, j) => (j === i ? { ...s, ...change } : s)) }); }

  addSource(text) {
    const add = String(text ?? "").split(/[\s,]+/).filter(Boolean).filter((s) => !this.draft.sources.includes(s));
    if (add.length) this.patch({ sources: [...this.draft.sources, ...add] });
    this.source = "";
  }
  addScene() {
    const n = this.draft.scenes.length + 1;
    this.patch({ scenes: [...this.draft.scenes, { name: t("Scene {n}", { n }), days: "daily", from: "09:00", to: "18:00", sprite: "", bmode: "auto", bval: 50 }] });
  }
  removeScene(i) {
    const scene = this.draft.scenes[i];
    this.patch({ scenes: this.draft.scenes.filter((_, j) => j !== i) });
    toast(this, t("Scene {name} removed", { name: scene.name }), {
      action: { label: t("Undo"), run: () => { const list = [...this.draft.scenes]; list.splice(i, 0, scene); this.patch({ scenes: list }); } },
    });
  }

  async save() {
    this.busy = true;
    this.errors = [];
    try {
      const st = await api("PUT", `channels/${encodeURIComponent(this.channel.name)}`, editorBody(this.draft));
      const name = this.draft.name.trim();
      if (name !== this.channel.name) send(this, "renamed", name);
      saved(this, st);
      toast(this, t("{name} saved", { name }));
    } catch (e) {
      this.errors = errorLines(e);
      toast(this, this.errors[0]);
    }
    this.busy = false;
  }

  render() {
    const d = this.draft;
    if (!d) return nothing;
    const mine = { name: [], ambient: [], scenes: {}, rest: [] };
    const sceneOf = (line) => {
      const m = /scenes\[(\d+)\]/.exec(line);
      return m ? Number(m[1]) : d.scenes.findIndex((s) => line.includes(`scene "${s.name}"`));
    };
    for (const line of this.errors ?? []) {
      const i = /scene/.test(line) ? sceneOf(line) : -1;
      if (i >= 0) (mine.scenes[i] ??= []).push(line);
      else if (/ambient/.test(line)) mine.ambient.push(line);
      else if (/channel name|already exists/.test(line)) mine.name.push(line);
      else mine.rest.push(line);
    }
    const total = (d.ambient.pictures || 0) + (d.ambient.clock || 0);
    const quiet = d.sources.length === 0;
    return html`
      <div class="sec">
        <label class="label" for="n-${this.channel.name}">${t("Name")}</label>
        <input id="n-${this.channel.name}" class="name ${mine.name.length ? "bad" : ""}" maxlength="32" autocapitalize="none" spellcheck="false" .value=${d.name} @input=${(e) => this.patch({ name: e.target.value })}>
        ${mine.name.length ? fieldErr(mine.name) : html`<p class="hint">${t("Renaming also renames its schedule slots, its pictures and an override on it.")}</p>`}
      </div>
      <div class="sec">
        <h3>${t("Accepted alert sources")}</h3>
        <div class="chips">
          ${d.sources.map((s) => html`<span class="chip">${s === "*" ? t("* all sources") : s}<button aria-label=${t("Remove {name}", { name: s })} @click=${() => this.patch({ sources: d.sources.filter((x) => x !== s) })}>${icons.close}</button></span>`)}
          <input type="text" placeholder=${t("Add a source")} aria-label=${t("Add a source")} autocapitalize="none" spellcheck="false" .value=${this.source ?? ""}
            @input=${(e) => { this.source = e.target.value; }}
            @keydown=${(e) => { if (e.key === "Enter" || e.key === ",") { e.preventDefault(); this.addSource(e.target.value); } }} @blur=${(e) => this.addSource(e.target.value)}>
          ${d.sources.includes("*") ? nothing : html`<button class="btn small" @click=${() => this.addSource("*")}>${t("Accept all")}</button>`}
        </div>
        <p class="hint">${quiet ? t("No sources: this channel is quiet and shows no alerts.") : t("An alert shows only when its source is listed here. * accepts every source, including alerts without one.")}</p>
      </div>
      <div class="sec">
        <h3>${t("Ambient rotation")}</h3>
        <div class="ambient">
          <div><label class="label" for="ap-${d.name}">${t("Pictures, minutes")}</label><input id="ap-${d.name}" type="number" min="0" step="any" class=${mine.ambient.length ? "bad" : ""} .value=${String(d.ambient.pictures)} @input=${(e) => this.patch({ ambient: { ...d.ambient, pictures: e.target.value === "" ? "" : Number(e.target.value) } })}></div>
          <div><label class="label" for="ac-${d.name}">${t("Clock, minutes")}</label><input id="ac-${d.name}" type="number" min="0" step="any" class=${mine.ambient.length ? "bad" : ""} .value=${String(d.ambient.clock)} @input=${(e) => this.patch({ ambient: { ...d.ambient, clock: e.target.value === "" ? "" : Number(e.target.value) } })}></div>
        </div>
        ${total > 0 ? html`<div class="mix" aria-hidden="true"><i style="width:${(100 * (d.ambient.pictures || 0)) / total}%;background:var(--accent)"></i></div>` : nothing}
        <p class="hint">${t("Either can be 0 to skip it. Together they must add up to 10 seconds or more.")}</p>
        ${fieldErr(mine.ambient)}
      </div>
      <div class="sec">
        <h3><span class="grow">${t("Scenes")}</span><button class="btn small" @click=${() => this.addScene()}>${icons.plus}${t("Add scene")}</button></h3>
        ${d.scenes.length ? nothing : html`<p class="hint" style="margin:0">${t("No scenes. Ambient plays all day.")}</p>`}
        ${d.scenes.map((s, i) => html`<div class="scene">
          <div class="top"><input aria-label=${t("Scene name")} .value=${s.name} @input=${(e) => this.setScene(i, { name: e.target.value })}>
            <button class="icon trash" aria-label=${t("Remove scene {name}", { name: s.name })} title=${t("Remove")} @click=${() => this.removeScene(i)}>${icons.trash}</button></div>
          <div class="when">
            <div><span class="label">${t("Days")}</span><ph-days .value=${s.days} @change=${(e) => this.setScene(i, { days: e.detail })}></ph-days></div>
            <div><span class="label">${t("From")}</span>${timeField(s.from, t("From"), (v) => this.setScene(i, { from: v }), mine.scenes[i])}</div>
            <div><span class="label">${t("To")}</span>${timeField(s.to, t("To"), (v) => this.setScene(i, { to: v }), mine.scenes[i])}</div>
          </div>
          <div><span class="label">${t("Shows")}</span><ph-sprite-grid .withClock=${true} .sprites=${this.sprites} .value=${s.sprite} @pick=${(e) => this.setScene(i, { sprite: e.detail })}></ph-sprite-grid></div>
          <div><span class="label">${t("Brightness")}</span><div class="bright">
            <select aria-label=${t("Brightness")} .value=${s.bmode} @change=${(e) => this.setScene(i, { bmode: e.target.value })}>
              <option value="auto" ?selected=${s.bmode === "auto"}>${t("By the sun")}</option><option value="set" ?selected=${s.bmode === "set"}>${t("Fixed")}</option><option value="off" ?selected=${s.bmode === "off"}>${t("Off")}</option></select>
            ${s.bmode === "set" ? html`<input type="range" min="1" max="100" .value=${String(s.bval)} aria-label=${t("Brightness")} @input=${(e) => this.setScene(i, { bval: Number(e.target.value) })}><output>${s.bval}</output>` : nothing}
          </div></div>
          ${fieldErr(mine.scenes[i])}
        </div>`)}
      </div>
      <div class="sec">
        <h3>${t("Pictures")}</h3>
        ${keyed(this.channel.name, html`<ph-pictures channel=${this.channel.name}></ph-pictures>`)}
      </div>
      ${mine.rest.length ? html`<ul class="errors">${mine.rest.map((l) => html`<li class="err">${l}</li>`)}</ul>` : nothing}
      <div class="foot"><button class="btn primary" ?disabled=${!this.dirty || this.busy} @click=${() => this.save()}>${t("Save channel")}</button>
        <button class="btn" ?disabled=${!this.dirty || this.busy} @click=${() => { this.draft = editorDraft(this.channel); this.errors = []; }}>${t("Revert")}</button>
        <span class="small muted">${t("Pictures upload and delete at once; the rest waits for Save.")}</span></div>`;
  }
}
customElements.define("ph-channel-editor", ChannelEditor);

const ambientSummary = (a) => {
  const p = minutesOf(a.pictures), c = minutesOf(a.clock);
  if (p && c) return t("{p} min pictures, {c} min clock", { p, c });
  if (p) return t("Pictures only");
  return t("Clock only");
};

class Channels extends LitElement {
  static properties = { state: { attribute: false }, status: { attribute: false }, sprites: { attribute: false }, open: { state: true }, name: { state: true }, error: { state: true }, busy: { state: true } };
  static styles = [base, css`
    .create { display: flex; gap: 8px; flex-wrap: wrap; }
    .create input { flex: 1; min-width: 160px; max-width: 320px; font-family: var(--font-data); }
    ul.list { list-style: none; margin: 14px 0 0; padding: 0; }
    li.item { border-top: 1px solid var(--line); }
    .row { display: flex; align-items: center; gap: 4px; }
    .toggle { flex: 1; min-width: 0; display: flex; align-items: center; gap: 10px; text-align: left; background: none; border: 0; padding: 12px 0; cursor: pointer; }
    .toggle svg { color: var(--muted); transition: transform .15s; flex: none; }
    .item.open .toggle svg { transform: rotate(180deg); }
    .title { min-width: 0; flex: 1; }
    .title b { font-size: 15px; font-weight: 600; display: inline-block; margin-right: 8px; }
    .facts { display: flex; flex-wrap: wrap; gap: 2px 18px; margin-top: 3px; font-size: 12px; color: var(--muted); }
    .facts span b { font-weight: 500; color: var(--ink); margin-left: 4px; font-size: 12px; margin-right: 0; }
    .body { padding: 4px 0 18px; }
  `];

  async create(e) {
    e.preventDefault();
    const name = (this.name ?? "").trim();
    if (!name) return;
    this.busy = true;
    this.error = "";
    try {
      const st = await api("POST", "channels", { name, sources: ["*"] });
      saved(this, st);
      this.name = "";
      this.open = name;
    } catch (err) { this.error = errorLines(err).join(" "); }
    this.busy = false;
  }

  noDelete(c) {
    const st = this.state;
    if (st.channels.length <= 1) return t("It is the only channel.");
    if (c.name === st.default_channel) return t("{name} is the default channel. Pick another default in Settings first.", { name: c.name });
    return "";
  }

  render() {
    const st = this.state;
    if (!st) return nothing;
    return html`<section class="card" aria-labelledby="ch-h">
      <div class="head"><h2 id="ch-h">${t("Channels")}</h2></div>
      <form class="create" @submit=${(e) => this.create(e)}>
        <input aria-label=${t("New channel name")} placeholder=${t("New channel name")} maxlength="32" autocapitalize="none" spellcheck="false" class=${this.error ? "bad" : ""} .value=${this.name ?? ""} @input=${(e) => { this.name = e.target.value; this.error = ""; }}>
        <button class="btn" type="submit" ?disabled=${this.busy || !(this.name ?? "").trim()}>${icons.plus}${t("Create channel")}</button>
      </form>
      ${this.error ? html`<p class="err">${this.error}</p>` : html`<p class="hint">${t("1 to 32 lowercase letters, digits, - or _.")}</p>`}
      <ul class="list">${repeat(st.channels, (c) => c.name, (c) => {
        const open = this.open === c.name;
        const why = this.noDelete(c);
        const sources = c.sources?.length ? (c.sources.includes("*") ? t("All") : c.sources.join(", ")) : t("None");
        return html`<li class="item ${open ? "open" : ""}">
          <div class="row">
            <button class="toggle" aria-expanded=${String(open)} @click=${() => { this.open = open ? null : c.name; }}>
              <span class="title"><b>${c.name}</b>${this.status?.channel === c.name ? html`<span class="tag on">${t("Showing")}</span>` : st.default_channel === c.name ? html`<span class="tag">${t("Default")}</span>` : nothing}
                <span class="facts"><span>${t("Sources")}<b>${sources}</b></span><span>${t("Ambient")}<b>${ambientSummary(c.ambient)}</b></span><span>${t("Scenes")}<b>${(c.scenes ?? []).length}</b></span></span></span>
              ${icons.chevron}</button>
            <button class="icon trash" aria-disabled=${String(!!why)} title=${why || t("Delete {name}", { name: c.name })} aria-label=${t("Delete {name}", { name: c.name })}
              @click=${() => (why ? toast(this, why) : send(this, "remove-channel", c.name))}>${icons.trash}</button>
          </div>
          ${open ? html`<div class="body"><ph-channel-editor .channel=${c} .sprites=${this.sprites} @renamed=${(e) => { this.open = e.detail; }}></ph-channel-editor></div>` : nothing}
        </li>`;
      })}</ul>
    </section>`;
  }
}
customElements.define("ph-channels", Channels);

class WeekView extends LitElement {
  static properties = { slots: { attribute: false }, names: { attribute: false }, fallback: {} };
  static styles = [base, css`
    .week { display: grid; grid-template-columns: 34px 1fr; gap: 5px 8px; align-items: center; }
    .axis { grid-column: 2; display: flex; justify-content: space-between; font-family: var(--font-data); font-size: 10px; color: var(--muted); }
    .day { font-size: 12px; color: var(--muted); text-transform: capitalize; }
    .day.today { color: var(--ink); font-weight: 600; }
    .track { position: relative; height: 22px; border-radius: 5px; overflow: hidden; background: var(--chip); }
    .band { position: absolute; top: 0; bottom: 0; color: #fff; font-size: 10px; line-height: 22px; padding: 0 5px; overflow: hidden; white-space: nowrap; text-overflow: ellipsis; border-right: 1px solid color-mix(in srgb, var(--surface) 70%, transparent); background: none; border-radius: 0; gap: 0; }
    .now { position: absolute; top: -1px; bottom: -1px; width: 2px; background: var(--ink); }
    .legend { display: flex; flex-wrap: wrap; gap: 4px 16px; margin-top: 12px; font-size: 12px; }
    .legend span { display: inline-flex; align-items: center; gap: 6px; }
    .legend i { width: 10px; height: 10px; border-radius: 3px; display: inline-block; }
  `];
  segments(slots, day) {
    const out = [];
    for (let m = 0; m < 1440; m++) {
      const i = matchingSlot(slots, day, m);
      const channel = i < 0 ? this.fallback : slots[i].channel;
      const last = out.at(-1);
      if (last && last.channel === channel && last.slot === i) last.to = m + 1;
      else out.push({ channel, slot: i, from: m, to: m + 1 });
    }
    return out;
  }
  render() {
    const slots = withSets(this.slots ?? []);
    const now = new Date();
    const today = dayOf(now);
    const fmt = (m) => `${String(Math.floor(m / 60) % 24).padStart(2, "0")}:${String(m % 60).padStart(2, "0")}`;
    const used = [...new Set([this.fallback, ...(this.slots ?? []).map((s) => s.channel)])];
    return html`<div class="week" role="img" aria-label=${t("Which channel is active at each hour of the week")}>
      <span></span><div class="axis"><span>0</span><span>6</span><span>12</span><span>18</span><span>24</span></div>
      ${DAY_KEYS.map((_, d) => html`<span class="day ${d === today ? "today" : ""}">${dayLabel(d)}</span>
        <div class="track">${this.segments(slots, d).map((s) => html`<span class="band" style="left:${(s.from / 1440) * 100}%;width:${((s.to - s.from) / 1440) * 100}%;background:${channelColor(this.names, s.channel)}"
          title="${s.channel} ${fmt(s.from)} - ${fmt(s.to)}">${s.to - s.from > 150 ? s.channel : ""}</span>`)}
          ${d === today ? html`<span class="now" style="left:${(minuteOf(now) / 1440) * 100}%"></span>` : nothing}</div>`)}
    </div>
    <div class="legend">${used.map((n) => html`<span><i style="background:${channelColor(this.names, n)}"></i>${n}${n === this.fallback ? html`<span class="muted">${t("default")}</span>` : nothing}</span>`)}</div>`;
  }
}
customElements.define("ph-week", WeekView);

const slotDraft = (s) => ({ channel: s.channel, days: s.days, from: s.from, to: s.to });

class Schedule extends LitElement {
  static properties = { base: { attribute: false }, draft: { state: true }, errors: { state: true }, busy: { state: true } };
  static styles = [base, css`
    ol { list-style: none; margin: 0; padding: 0; counter-reset: slot; }
    li { border-top: 1px solid var(--line); padding: 12px 0; display: flex; flex-wrap: wrap; gap: 10px 12px; align-items: flex-end; }
    li .n { font-family: var(--font-data); font-size: 12px; color: var(--muted); width: 20px; align-self: center; }
    li .chan { flex: 1; min-width: 120px; max-width: 140px; }
    li .chan select { width: 100%; }
    li .actions { margin-left: auto; display: flex; align-self: center; }
    li { position: relative; }
    @media (min-width: 700px) { li { padding-right: 104px; } li .actions { position: absolute; right: 0; top: 50%; transform: translateY(-50%); margin: 0; } }
    li .errs { flex-basis: 100%; }
    .foot { display: flex; gap: 8px; margin-top: 14px; align-items: center; flex-wrap: wrap; }
    .week-wrap { margin-top: 18px; padding-top: 16px; border-top: 1px solid var(--line); }
    .empty { margin: 0 0 4px; font-size: 13px; color: var(--muted); }
  `];
  willUpdate(c) {
    if (c.has("base") && this.base && (!this.draft || same(this.draft, c.get("base") ? c.get("base").schedule.map(slotDraft) : null))) this.draft = this.base.schedule.map(slotDraft);
  }
  get dirty() { return this.draft && this.base && !same(this.draft, this.base.schedule.map(slotDraft)); }
  setSlot(i, change) { this.draft = this.draft.map((s, j) => (j === i ? { ...s, ...change } : s)); }
  move(i, by) {
    const list = [...this.draft];
    [list[i], list[i + by]] = [list[i + by], list[i]];
    this.draft = list;
  }
  add() { this.draft = [...this.draft, { channel: this.base.default_channel, days: "daily", from: "09:00", to: "18:00" }]; }
  remove(i) {
    const slot = this.draft[i];
    this.draft = this.draft.filter((_, j) => j !== i);
    toast(this, t("Slot {n} removed", { n: i + 1 }), {
      action: { label: t("Undo"), run: () => { const list = [...this.draft]; list.splice(i, 0, slot); this.draft = list; } },
    });
  }
  async save() {
    this.busy = true;
    this.errors = [];
    try {
      const st = await api("PUT", "schedule", this.draft);
      saved(this, st);
      toast(this, t("Schedule saved"));
    } catch (e) {
      this.errors = errorLines(e);
      toast(this, this.errors[0]);
    }
    this.busy = false;
  }
  render() {
    if (!this.draft) return nothing;
    const names = this.base.channels.map((c) => c.name);
    const mine = {};
    const rest = [];
    for (const line of this.errors ?? []) {
      const i = /schedule\[(\d+)\]/.exec(line)?.[1];
      if (i !== undefined) (mine[i] ??= []).push(line.replace(/^schedule\[\d+\]:\s*/, ""));
      else rest.push(line);
    }
    return html`<section class="card" aria-labelledby="sc-h">
      <div class="head"><h2 id="sc-h">${t("Schedule")}</h2><button class="btn small" @click=${() => this.add()}>${icons.plus}${t("Add slot")}</button></div>
      <p class="hint" style="margin:0 0 8px">${t("The first slot that matches wins. When none match, the default channel shows.")}</p>
      ${this.draft.length ? nothing : html`<p class="empty">${t("No slots: the default channel always shows.")}</p>`}
      <ol>${this.draft.map((s, i) => html`<li>
        <span class="n">${i + 1}</span>
        <div class="chan"><span class="label">${t("Channel")}</span>
          <select aria-label=${t("Channel")} class=${mine[i] ? "bad" : ""} .value=${s.channel} @change=${(e) => this.setSlot(i, { channel: e.target.value })}>
            ${(names.includes(s.channel) ? names : [s.channel, ...names]).map((n) => html`<option value=${n} ?selected=${n === s.channel}>${n}</option>`)}</select></div>
        <div class="actions">
          <button class="icon" aria-label=${t("Move up")} title=${t("Move up")} ?disabled=${i === 0} @click=${() => this.move(i, -1)}>${icons.up}</button>
          <button class="icon" aria-label=${t("Move down")} title=${t("Move down")} ?disabled=${i === this.draft.length - 1} @click=${() => this.move(i, 1)}>${icons.down}</button>
          <button class="icon trash" aria-label=${t("Remove slot {n}", { n: i + 1 })} title=${t("Remove")} @click=${() => this.remove(i)}>${icons.trash}</button>
        </div>
        <div><span class="label">${t("Days")}</span><ph-days .value=${s.days} @change=${(e) => this.setSlot(i, { days: e.detail })}></ph-days></div>
        <div><span class="label">${t("From")}</span>${timeField(s.from, t("From"), (v) => this.setSlot(i, { from: v }), mine[i])}</div>
        <div><span class="label">${t("To")}</span>${timeField(s.to, t("To"), (v) => this.setSlot(i, { to: v }), mine[i])}</div>
        <div class="errs">${fieldErr(mine[i])}</div>
      </li>`)}</ol>
      ${rest.length ? html`<ul class="errors">${rest.map((l) => html`<li class="err">${l}</li>`)}</ul>` : nothing}
      <div class="foot"><button class="btn primary" ?disabled=${!this.dirty || this.busy} @click=${() => this.save()}>${t("Save schedule")}</button>
        <button class="btn" ?disabled=${!this.dirty || this.busy} @click=${() => { this.draft = this.base.schedule.map(slotDraft); this.errors = []; }}>${t("Revert")}</button></div>
      <div class="week-wrap"><ph-week .slots=${this.draft} .names=${names} fallback=${this.base.default_channel}></ph-week></div>
    </section>`;
  }
}
customElements.define("ph-schedule", Schedule);

// A toast hides by itself, but not while the pointer or focus is on it, so its action stays reachable.
// `expire` is the work an Undo cancels: it runs when the toast goes away any other way.
class Toasts {
  constructor(host) { this.host = host; this.note = null; }
  show(note) { this.flush(); this.note = { ...note, ms: note.action ? 8000 : 6000 }; this.arm(); this.host.requestUpdate(); }
  arm() { clearTimeout(this.timer); if (this.note) this.timer = setTimeout(() => this.close(), this.note.ms); }
  hold() { clearTimeout(this.timer); }
  flush() { const run = this.note?.expire; if (run) { this.note.expire = null; run(); } }
  close() { clearTimeout(this.timer); this.flush(); this.note = null; this.host.requestUpdate(); }
  undo() {
    const { action } = this.note;
    clearTimeout(this.timer);
    this.note = null;
    action.run();
    this.host.requestUpdate();
  }
  view() {
    const n = this.note;
    if (!n) return nothing;
    return html`<div class="toast" role="status" @pointerenter=${() => this.hold()} @pointerleave=${() => this.arm()} @focusin=${() => this.hold()} @focusout=${() => this.arm()}>
      <span>${n.text}</span>${n.action ? html`<button class="act" @click=${() => this.undo()}>${n.action.label}</button>` : nothing}
      <button class="x" aria-label=${t("Dismiss")} @click=${() => this.close()}>${icons.close}</button></div>`;
  }
}

class PixelhubApp extends LitElement {
  static properties = { st: { state: true }, status: { state: true }, sprites: { state: true }, version: { state: true }, offline: { state: true }, gone: { state: true } };
  static styles = [base, css`
    header { position: sticky; top: 0; z-index: 5; background: color-mix(in srgb, var(--bg) 88%, transparent); backdrop-filter: blur(10px); border-bottom: 1px solid var(--line); }
    .bar { max-width: 1180px; margin: 0 auto; padding: 10px 16px; display: flex; align-items: center; gap: 16px; }
    .brand { font-family: var(--font-display); font-weight: 700; font-size: 16px; letter-spacing: -.01em; display: flex; align-items: center; gap: 10px; }
    .brand img { width: 24px; height: 24px; }
    nav { display: flex; gap: 2px; margin-left: auto; overflow-x: auto; min-width: 0; scrollbar-width: none; }
    nav button { border: 0; background: none; padding: 6px 10px; border-radius: 8px; font-size: 13px; font-weight: 500; color: var(--muted); cursor: pointer; white-space: nowrap; }
    nav button:hover { color: var(--ink); background: var(--chip); }
    .link { display: flex; align-items: center; gap: 8px; font-size: 12px; color: var(--muted); white-space: nowrap; }
    @media (max-width: 559px) { .link .word { display: none; } .brand span { display: none; } }
    main { max-width: 1180px; margin: 0 auto; padding: 16px 16px 48px; display: grid; grid-template-columns: minmax(0, 1fr); gap: 16px; }
    main > * { scroll-margin-top: 64px; min-width: 0; }
    .col { display: contents; }
    .a-now { order: 1; } .a-switch { order: 2; } .a-channels { order: 3; } .a-schedule { order: 4; } .a-settings { order: 5; } .a-notify { order: 6; }
    @media (min-width: 980px) {
      main { grid-template-columns: 400px minmax(0, 1fr); gap: 18px; }
      .col { display: flex; flex-direction: column; gap: 18px; min-width: 0; }
      .col > * { order: 0; }
    }
    footer { max-width: 1180px; margin: 0 auto; padding: 0 16px 32px; font-size: 12px; color: var(--muted); }
    .toast { position: fixed; left: max(16px, env(safe-area-inset-left, 0px)); bottom: max(16px, env(safe-area-inset-bottom, 0px)); z-index: 10; max-width: min(440px, calc(100vw - 32px));
      display: flex; align-items: center; gap: 10px; padding: 10px 10px 10px 14px; font-size: 13px; background: var(--surface); color: var(--ink); border: 1px solid var(--line); border-radius: 12px;
      box-shadow: 0 12px 40px rgba(0, 0, 0, .3); animation: rise .18s ease-out; }
    @keyframes rise { from { transform: translateY(8px); opacity: 0; } }
    .toast span { flex: 1; min-width: 0; overflow-wrap: anywhere; }
    .toast button { border: 0; background: none; cursor: pointer; flex: none; }
    .toast .act { font-size: 13px; font-weight: 600; color: var(--accent); padding: 4px 8px; border-radius: 6px; }
    .toast .act:hover { background: var(--chip); }
    .toast .x { width: 24px; height: 24px; display: inline-flex; align-items: center; justify-content: center; color: var(--muted); border-radius: 6px; padding: 0; }
    .toast .x:hover { color: var(--ink); background: var(--chip); }
    .toast .x svg { width: 14px; height: 14px; }
  `];

  constructor() {
    super();
    this.gone = new Set();
    this.toasts = new Toasts(this);
    this.addEventListener("toast", (e) => this.toasts.show(e.detail));
    this.addEventListener("state", (e) => { this.st = e.detail; });
    this.addEventListener("refresh", () => this.pollStatus());
    this.addEventListener("remove-channel", (e) => this.removeChannel(e.detail));
    this.visible = () => { if (!document.hidden) { this.pollStatus(); this.loadState(); } };
    this.leave = () => this.toasts.flush();
  }

  connectedCallback() {
    super.connectedCallback();
    this.pollStatus();
    this.loadState();
    api("GET", "sprites").then((r) => { this.sprites = r.sprites; }).catch(() => {});
    fetch("healthz").then((r) => r.json()).then((r) => { this.version = r.version; }).catch(() => {});
    this.timers = [setInterval(() => { if (!document.hidden) this.pollStatus(); }, STATUS_MS), setInterval(() => { if (!document.hidden) this.loadState(); }, STATE_MS)];
    document.addEventListener("visibilitychange", this.visible);
    addEventListener("pagehide", this.leave);
  }
  disconnectedCallback() {
    super.disconnectedCallback();
    this.timers.forEach(clearInterval);
    document.removeEventListener("visibilitychange", this.visible);
    removeEventListener("pagehide", this.leave);
  }

  async pollStatus() {
    try { this.status = await api("GET", "status"); this.offline = false; } catch { this.offline = true; }
  }
  async loadState() {
    try { this.st = await api("GET", "state"); } catch { /* the status poll shows the outage */ }
  }

  removeChannel(name) {
    const drop = (set) => { const h = new Set(set); h.delete(name); return h; };
    this.gone = new Set(this.gone).add(name);
    this.toasts.show({
      text: t("Channel {name} deleted", { name }),
      action: { label: t("Undo"), run: () => { this.gone = drop(this.gone); } },
      expire: async () => {
        try { this.st = await api("DELETE", `channels/${encodeURIComponent(name)}`, undefined, { keepalive: true }); }
        catch (e) { this.toasts.show({ text: e.message }); }
        this.gone = drop(this.gone);
      },
    });
  }

  get view() {
    const st = this.st;
    if (!st || !this.gone.size) return st;
    return { ...st, channels: st.channels.filter((c) => !this.gone.has(c.name)), schedule: st.schedule.filter((s) => !this.gone.has(s.channel)) };
  }

  go(id) { this.renderRoot.querySelector(`#${id}`)?.scrollIntoView({ behavior: matchMedia("(prefers-reduced-motion: reduce)").matches ? "auto" : "smooth", block: "start" }); }

  render() {
    const view = this.view;
    const [tone, linkText] = this.status ? linkState(this.status) : ["", t("Loading")];
    return html`
      <header><div class="bar">
        <div class="brand"><img src="favicon.svg" alt=""><span>pixelhub</span></div>
        <nav aria-label=${t("Sections")}>${[["now", t("Now")], ["channels", t("Channels")], ["schedule", t("Schedule")], ["settings", t("Settings")]].map(([id, l]) => html`<button @click=${() => this.go(id)}>${l}</button>`)}</nav>
        <div class="link" title=${linkText}><span class="dot ${this.offline ? "bad" : tone}"></span><span class="word">${this.offline ? t("No answer") : linkText}</span></div>
      </div></header>
      <main>
        <div class="col">
          <div class="a-now" id="now"><ph-now .status=${this.status} .state=${view} ?offline=${this.offline}></ph-now></div>
          <div class="a-switch"><ph-switch .state=${view} .status=${this.status}></ph-switch></div>
          <div class="a-notify"><ph-notify .sprites=${this.sprites}></ph-notify></div>
          <div class="a-settings" id="settings">${view ? html`<ph-settings .base=${view}></ph-settings>` : nothing}</div>
        </div>
        <div class="col">
          <div class="a-channels" id="channels"><ph-channels .state=${view} .status=${this.status} .sprites=${this.sprites}></ph-channels></div>
          <div class="a-schedule" id="schedule">${view ? html`<ph-schedule .base=${view}></ph-schedule>` : nothing}</div>
        </div>
      </main>
      <footer>pixelhub ${this.version ?? ""}</footer>
      ${this.toasts.view()}`;
  }
}
customElements.define("pixelhub-app", PixelhubApp);
