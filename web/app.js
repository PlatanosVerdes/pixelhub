// pixelhub's web UI: the app element fetches, every component below renders what it is given.
import { LitElement, html, svg, css, nothing, keyed, repeat } from "./vendor/lit-all.min.js";
import { lang, t } from "./i18n.js";
import { clockFace } from "./faces.js";

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
  bookmark: icon(svg`<path d="M6 3.5h12v17l-6-4-6 4z"/>`),
  back: icon(svg`<path d="m15 18-6-6 6-6"/>`),
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
          <span class="small muted">${t("Sent with the source ui; the active profile must accept it.")}</span></div>
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
          <div><label class="label" for="def">${t("Default profile")}</label>
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
    if (c.has("channel") && this.channel && (!this.draft || same(this.draft, c.get("profile") ? editorDraft(c.get("profile")) : null))) this.draft = editorDraft(this.channel);
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
        <p class="hint">${quiet ? t("No sources: this profile is quiet and shows no alerts.") : t("An alert shows only when its source is listed here. * accepts every source, including alerts without one.")}</p>
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
      <div class="foot"><button class="btn primary" ?disabled=${!this.dirty || this.busy} @click=${() => this.save()}>${t("Save profile")}</button>
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
    if (st.channels.length <= 1) return t("It is the only profile.");
    if (c.name === st.default_channel) return t("{name} is the default profile. Pick another default in Settings first.", { name: c.name });
    return "";
  }

  render() {
    const st = this.state;
    if (!st) return nothing;
    return html`<section class="card" aria-labelledby="ch-h">
      <div class="head"><h2 id="ch-h">${t("Profiles")}</h2></div>
      <form class="create" @submit=${(e) => this.create(e)}>
        <input aria-label=${t("New profile name")} placeholder=${t("New profile name")} maxlength="32" autocapitalize="none" spellcheck="false" class=${this.error ? "bad" : ""} .value=${this.name ?? ""} @input=${(e) => { this.name = e.target.value; this.error = ""; }}>
        <button class="btn" type="submit" ?disabled=${this.busy || !(this.name ?? "").trim()}>${icons.plus}${t("Create profile")}</button>
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
    return html`<div class="week" role="img" aria-label=${t("Which profile is active at each hour of the week")}>
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
      <p class="hint" style="margin:0 0 8px">${t("The first slot that matches wins. When none match, the default profile shows.")}</p>
      ${this.draft.length ? nothing : html`<p class="empty">${t("No slots: the default profile always shows.")}</p>`}
      <ol>${this.draft.map((s, i) => html`<li>
        <span class="n">${i + 1}</span>
        <div class="chan"><span class="label">${t("Profile")}</span>
          <select aria-label=${t("Profile")} class=${mine[i] ? "bad" : ""} .value=${s.channel} @change=${(e) => this.setSlot(i, { channel: e.target.value })}>
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
// The display drawn as what it is: a grid of round LEDs, lit from the preview, dark where off.
const LED_SIZE = 16;
class LedMatrix extends LitElement {
  static properties = { src: {}, dim: { type: Boolean }, label: {} };
  static styles = [base, css`
    :host { display: block; }
    .case { background: var(--case); border-radius: 22px; padding: 14px; box-shadow: inset 0 1px 0 rgba(255, 255, 255, .06), 0 18px 50px -18px rgba(0, 0, 0, .7); }
    canvas { display: block; width: 100%; aspect-ratio: 1; transition: opacity .3s; }
    .dim canvas { opacity: .3; }
  `];
  updated(c) { if (c.has("src")) this.load(); }
  load() {
    const img = new Image(), src = this.src;
    img.onload = () => { if (src === this.src) { this.img = img; this.draw(); } };
    img.src = src;
  }
  draw() {
    const canvas = this.renderRoot.querySelector("canvas");
    if (!canvas || !this.img) return;
    const px = Math.round(canvas.clientWidth * devicePixelRatio) || 320;
    canvas.width = canvas.height = px;
    const ctx = canvas.getContext("2d");
    const sample = document.createElement("canvas");
    sample.width = sample.height = LED_SIZE;
    const sctx = sample.getContext("2d", { willReadFrequently: true });
    sctx.imageSmoothingEnabled = false;
    sctx.drawImage(this.img, 0, 0, LED_SIZE, LED_SIZE);
    const data = sctx.getImageData(0, 0, LED_SIZE, LED_SIZE).data;
    const cell = px / LED_SIZE, r = cell * 0.4;
    ctx.clearRect(0, 0, px, px);
    for (let y = 0; y < LED_SIZE; y++) for (let x = 0; x < LED_SIZE; x++) {
      const i = (y * LED_SIZE + x) * 4;
      const [cr, cg, cb] = [data[i], data[i + 1], data[i + 2]];
      const lit = cr + cg + cb > 24;
      const cx = x * cell + cell / 2, cy = y * cell + cell / 2;
      ctx.beginPath();
      ctx.arc(cx, cy, r, 0, Math.PI * 2);
      if (lit) {
        ctx.shadowColor = `rgb(${cr},${cg},${cb})`;
        ctx.shadowBlur = cell * 0.55;
        ctx.fillStyle = `rgb(${cr},${cg},${cb})`;
      } else {
        ctx.shadowBlur = 0;
        ctx.fillStyle = "rgba(255,255,255,.045)";
      }
      ctx.fill();
    }
  }
  connectedCallback() { super.connectedCallback(); this.resize = new ResizeObserver(() => this.draw()); }
  firstUpdated() { this.resize.observe(this.renderRoot.querySelector("canvas")); }
  disconnectedCallback() { super.disconnectedCallback(); this.resize.disconnect(); }
  render() { return html`<div class="case ${this.dim ? "dim" : ""}"><canvas role="img" aria-label=${this.label ?? ""}></canvas></div>`; }
}
customElements.define("ph-led", LedMatrix);

// The display's own modes, as its side button cycles them. Each tap is sent at once, like the button.
const MODES = ["clock", "light", "animations", "visualizer", "scoreboard"];
const modeName = (m) => ({ clock: t("Clock"), light: t("Light"), animations: t("My animations"), visualizer: t("Music"), scoreboard: t("Scoreboard") })[m];
const modeIcons = {
  clock: icon(svg`<circle cx="12" cy="12" r="8.5"/><path d="M12 7.5V12l3 2"/>`),
  light: icon(svg`<path d="M9 18h6M10 21h4M12 3a6 6 0 0 0-3.5 10.9c.6.4 1 1.1 1 1.8V16h5v-.3c0-.7.4-1.4 1-1.8A6 6 0 0 0 12 3Z"/>`),
  animations: icon(svg`<rect x="3.5" y="6.5" width="13" height="13" rx="2"/><path d="M7.5 3.5h11a2 2 0 0 1 2 2v11M8.5 10.5v5l4-2.5z"/>`),
  visualizer: icon(svg`<path d="M5 15v4M9 9v10M13 12v7M17 5v14M21 10v9M1 17v2"/>`),
  scoreboard: icon(svg`<rect x="2.5" y="6" width="19" height="12" rx="2"/><path d="M12 6v12M6.5 10.5h2v3M15.5 10.5h2.5v1.5h-2.5v1.5H18"/>`),
};
// Named from photos of the real display; the docs had 3 and 5 the other way round.
const CLOCK_STYLES = () => [t("Full screen"), t("Rainbow"), t("Boxed"), t("Analog round"), t("Negative"), t("Analog square")];
const LIGHT_TYPES = () => [t("Plain"), t("Love"), t("Plants"), t("No mosquitoes"), t("Sleep")];

class Modes extends LitElement {
  static properties = { current: { attribute: false }, styles: { attribute: false }, draft: { state: true }, busy: { state: true }, now: { state: true } };
  connectedCallback() { super.connectedCallback(); this.now = new Date(); this.tick = setInterval(() => { this.now = new Date(); }, 15000); }
  disconnectedCallback() { super.disconnectedCallback(); clearInterval(this.tick); }
  static styles = [base, css`
    .modes { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 8px; }
    @media (min-width: 560px) { .modes { grid-template-columns: repeat(6, minmax(0, 1fr)); } }
    .mode { border: 1px solid var(--line); background: var(--surface); border-radius: 16px; padding: 12px 6px 10px; display: flex; flex-direction: column; align-items: center; gap: 6px; cursor: pointer; color: var(--muted); font-size: 13px; font-weight: 500; min-width: 0; }
    .mode svg { width: 24px; height: 24px; }
    .mode span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; max-width: 100%; }
    .mode:hover { color: var(--ink); border-color: color-mix(in srgb, var(--ink) 30%, var(--line)); }
    .mode[aria-pressed=true] { color: var(--ink); border-color: var(--accent); background: color-mix(in srgb, var(--accent) 12%, var(--surface)); }
    .mode[aria-pressed=true] svg { color: var(--accent); filter: drop-shadow(0 0 6px color-mix(in srgb, var(--accent) 70%, transparent)); }
    .mode.auto[aria-pressed=true] svg { color: var(--ok); filter: none; }
    .panel { margin-top: 10px; background: var(--surface); border: 1px solid var(--line); border-radius: 16px; padding: 16px; display: grid; gap: 16px; }
    .panel h3 { margin: 0; font-size: 15px; font-weight: 600; }
    .panel p { margin: 0; }
    .choices { display: flex; flex-wrap: wrap; gap: 6px; }
    .choice { border: 1px solid var(--line); background: none; border-radius: 999px; height: 34px; padding: 0 14px; font-size: 13px; color: var(--muted); cursor: pointer; }
    .choice:hover { color: var(--ink); }
    .choice[aria-pressed=true] { color: var(--on-accent); background: var(--accent); border-color: var(--accent); font-weight: 600; }
    .choice.num { width: 40px; padding: 0; }
    .faces { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 8px; }
    @media (min-width: 560px) { .faces { grid-template-columns: repeat(6, minmax(0, 1fr)); } }
    .face { border: 1px solid var(--line); background: none; border-radius: 14px; padding: 8px 6px; display: grid; justify-items: center; gap: 6px; cursor: pointer; color: var(--muted); font-size: 12px; min-width: 0; }
    .face img { width: 100%; max-width: 72px; aspect-ratio: 1; border-radius: 6px; background: #000; }
    .face span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; max-width: 100%; }
    .face:hover { color: var(--ink); }
    .face[aria-pressed=true] { border-color: var(--accent); color: var(--ink); box-shadow: 0 0 0 1px var(--accent); }
    .toggles { display: flex; flex-wrap: wrap; gap: 16px; }
    .toggles label { display: inline-flex; align-items: center; gap: 8px; font-size: 14px; cursor: pointer; }
    input[type=checkbox] { width: 18px; height: 18px; accent-color: var(--accent); margin: 0; }
    .colour { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
    .swatches { display: flex; gap: 6px; flex-wrap: wrap; }
    .swatch { width: 30px; height: 30px; border-radius: 50%; border: 2px solid transparent; cursor: pointer; padding: 0; }
    .swatch[aria-pressed=true] { border-color: var(--ink); }
    .score { display: grid; grid-template-columns: 1fr 1fr; gap: 10px; }
    .team { border-radius: 14px; padding: 12px; display: grid; justify-items: center; gap: 8px; background: color-mix(in srgb, var(--team) 16%, transparent); border: 1px solid color-mix(in srgb, var(--team) 45%, transparent); }
    .team output { font-family: var(--font-pixel); font-size: 40px; line-height: 1; color: var(--team); }
    .team .row { display: flex; gap: 8px; }
    .team button { width: 44px; height: 40px; border-radius: 12px; border: 1px solid color-mix(in srgb, var(--team) 45%, transparent); background: var(--surface); font-size: 20px; cursor: pointer; }
    .foot { display: flex; gap: 8px; flex-wrap: wrap; align-items: center; }
  `];

  // Each mode keeps its last options while another one is picked.
  viewFor(mode) {
    if (this.current?.mode === mode) return { ...this.current };
    const kept = this.draft?.[mode];
    return kept ? { ...kept } : { mode, ...(mode === "light" ? { color: "#ff3b30" } : {}) };
  }

  async apply(view) {
    this.draft = { ...this.draft, [view.mode]: view };
    this.busy = true;
    try { saved(this, await api("PUT", "mode", view)); send(this, "refresh"); } catch (e) { toast(this, e.message); }
    this.busy = false;
  }

  async automatic() {
    this.busy = true;
    try { saved(this, await api("DELETE", "mode")); send(this, "refresh"); } catch (e) { toast(this, e.message); }
    this.busy = false;
  }

  pick(mode) { this.apply(this.viewFor(mode)); }

  change(patch) { this.apply({ ...this.viewFor(this.current.mode), ...patch }); }

  colour(v, withNone) {
    const presets = ["#ffffff", "#ff3b30", "#ff9f0a", "#ffd60a", "#30d158", "#40c8e0", "#0a84ff", "#bf5af2", "#ff375f"];
    return html`<div class="colour">
      <div class="swatches" role="group" aria-label=${t("Colour")}>
        ${withNone ? html`<button class="choice" aria-pressed=${String(!v.color)} @click=${() => this.change({ color: undefined })}>${t("Default")}</button>` : nothing}
        ${presets.map((c) => html`<button class="swatch" style="background:${c}" aria-label=${c} aria-pressed=${String(v.color?.toLowerCase() === c)} @click=${() => this.change({ color: c })}></button>`)}
      </div>
      <input type="color" aria-label=${t("Other colour")} .value=${v.color ?? "#ffffff"} @change=${(e) => this.change({ color: e.target.value })}>
    </div>`;
  }

  options(mode) {
    const v = this.viewFor(mode);
    const choices = (names, key = "style") => html`<div class="choices" role="group">${names.map((n, i) =>
      html`<button class="choice ${typeof n === "number" ? "num" : ""}" aria-pressed=${String((v[key] ?? 0) === i)} @click=${() => this.change({ [key]: i })}>${n}</button>`)}</div>`;
    switch (mode) {
      case "clock": return html`
        <h3>${t("Clock style")}</h3>
        <div class="faces" role="group">${CLOCK_STYLES().map((name, i) => html`<button class="face" aria-pressed=${String((v.style ?? 0) === i)} @click=${() => this.change({ style: i || undefined })}>
          <img class="pix" src=${clockFace(i, this.now, v.color)} alt=""><span>${name}</span></button>`)}</div>
        <div class="toggles">
          ${[["weather", t("Weather")], ["temperature", t("Temperature")], ["calendar", t("Date")]].map(([k, l]) =>
            html`<label><input type="checkbox" .checked=${!!v[k]} @change=${(e) => this.change({ [k]: e.target.checked || undefined })}>${l}</label>`)}
        </div>
        <div><h3>${t("Colour")}</h3></div>${this.colour(v, true)}`;
      case "light": return html`
        <h3>${t("Effect")}</h3>${choices(LIGHT_TYPES())}
        <h3>${t("Colour")}</h3>${this.colour(v, false)}`;
      case "visualizer": return html`
        <h3>${t("Visualiser")}</h3>${choices(Array.from({ length: this.styles?.visualizer ?? 12 }, (_, i) => i + 1))}
        <p class="small muted">${t("It follows what the Evo plays as a speaker, not the room's sound.")}</p>`;
      case "animations": return html`
        <h3>${t("My animations")}</h3>
        <p class="muted">${t("Plays the list you keep in the Divoom app. pixelhub only switches to it and never changes it.")}</p>`;
      case "scoreboard": {
        const team = (key, label, colour) => html`<div class="team" style="--team:${colour}">
          <span class="small">${label}</span><output>${v[key] ?? 0}</output>
          <div class="row"><button aria-label=${t("{team} minus one", { team: label })} @click=${() => this.change({ [key]: Math.max(0, (v[key] ?? 0) - 1) || undefined })}>−</button>
            <button aria-label=${t("{team} plus one", { team: label })} @click=${() => this.change({ [key]: Math.min(999, (v[key] ?? 0) + 1) })}>+</button></div></div>`;
        return html`<div class="score">${team("red", t("Red"), "#ff453a")}${team("blue", t("Blue"), "#0a84ff")}</div>
          <div class="foot"><button class="btn small" @click=${() => this.change({ red: undefined, blue: undefined })}>${t("Reset to 0")}</button></div>`;
      }
    }
    return nothing;
  }

  render() {
    const active = this.current?.mode;
    return html`
      <div class="modes" role="group" aria-label=${t("Display modes")}>
        <button class="mode auto" aria-pressed=${String(!active)} ?disabled=${this.busy} @click=${() => this.automatic()} title=${t("Alerts, scenes and pictures, by the schedule")}>
          ${icon(svg`<path d="M4 12a8 8 0 0 1 14-5.3M20 12a8 8 0 0 1-14 5.3M18 3v4h-4M6 21v-4h4"/>`)}<span>${t("Automatic")}</span></button>
        ${MODES.map((m) => html`<button class="mode" aria-pressed=${String(active === m)} ?disabled=${this.busy} @click=${() => this.pick(m)}>${modeIcons[m]}<span>${modeName(m)}</span></button>`)}
      </div>
      ${active ? html`<div class="panel">${this.options(active)}</div>` : nothing}`;
  }
}
customElements.define("ph-modes", Modes);

// Brightness right now: the level of the sun's current phase, the same one Settings edits.
const phaseNow = (status, st) => {
  const b = st?.brightness;
  if (!b || status?.brightness === undefined) return null;
  return ["day", "dusk", "night"].find((k) => b[k] === status.brightness) ?? null;
};

class DisplayTab extends LitElement {
  static properties = { status: { attribute: false }, state: { attribute: false }, sprites: { attribute: false }, styles: { attribute: false }, offline: { type: Boolean }, level: { state: true }, busy: { state: true }, now: { state: true } };
  connectedCallback() { super.connectedCallback(); this.now = new Date(); this.tick = setInterval(() => { this.now = new Date(); }, 15000); }
  disconnectedCallback() { super.disconnectedCallback(); clearInterval(this.tick); }

  // The display draws its clock itself; the preview redraws the same face with the time.
  preview(s) {
    if (s.layer === "mode" && this.state?.mode?.mode === "clock") return clockFace(this.state.mode.style ?? 0, this.now, this.state.mode.color);
    if (s.showing === "clock") return clockFace(0, this.now);
    return `api/preview.png?v=${s.preview_version}`;
  }
  static styles = [base, css`
    .layout { display: grid; grid-template-columns: minmax(0, 1fr); gap: 22px; }
    @media (min-width: 900px) { .layout { grid-template-columns: minmax(300px, 400px) minmax(0, 1fr); align-items: start; gap: 32px; } .hero { position: sticky; top: 88px; } }
    .hero { display: grid; gap: 14px; justify-items: center; }
    ph-led { width: min(100%, 360px); }
    .now { text-align: center; display: grid; gap: 4px; }
    .now b { font-family: var(--font-pixel); font-weight: 400; font-size: 22px; letter-spacing: .02em; }
    .now span { font-size: 13px; color: var(--muted); }
    .level { display: grid; grid-template-columns: auto 1fr auto; align-items: center; gap: 12px; width: min(100%, 360px); }
    .level svg { color: var(--muted); }
    .level output { font-variant-numeric: tabular-nums; font-size: 13px; color: var(--muted); min-width: 28px; text-align: right; }
    input[type=range] { width: 100%; }
    section > h2 { margin: 0 0 10px; font-size: 13px; }
    .banner { padding: 10px 12px; border-radius: 12px; font-size: 13px; background: color-mix(in srgb, var(--warn) 16%, transparent); display: flex; gap: 10px; align-items: center; justify-content: space-between; flex-wrap: wrap; width: 100%; }
    .banner.bad { background: color-mix(in srgb, var(--bad) 16%, transparent); }
    .stack { display: grid; gap: 22px; min-width: 0; }
  `];

  what(s) {
    if (s.layer === "alert") return t("Alert");
    if (s.layer === "mode") return t("Chosen by you");
    if (s.layer === "music") return t("Playing on Spotify");
    if (s.layer === "scene") return t("Scene of {channel}", { channel: s.channel });
    return t("Automatic, {channel}", { channel: s.channel });
  }
  shownName(s) {
    if (s.layer === "mode" && this.state?.mode) return modeName(this.state.mode.mode);
    return s.showing;
  }

  async setLevel(value) {
    const phase = phaseNow(this.status, this.state);
    if (!phase) return;
    this.busy = true;
    try { saved(this, await api("PUT", "settings", { brightness: { ...this.state.brightness, [phase]: value } })); send(this, "refresh"); } catch (e) { toast(this, e.message); }
    this.busy = false;
    this.level = undefined;
  }

  async claim() {
    try { await api("POST", "claim"); send(this, "refresh"); } catch (e) { toast(this, e.message); }
  }

  render() {
    const s = this.status;
    const released = !!s?.released_until;
    const phase = phaseNow(s, this.state);
    const level = this.level ?? s?.brightness ?? 0;
    return html`<div class="layout">
      <div class="hero">
        ${this.offline ? html`<p class="banner bad" role="alert">${t("No answer from pixelhub. Showing the last status.")}</p>` : nothing}
        ${released ? html`<p class="banner">${t("The phone app has the display until {time}.", { time: until(s.released_until) })}<button class="btn small" @click=${() => this.claim()}>${t("Take back")}</button></p>` : nothing}
        <ph-led .src=${s ? this.preview(s) : ""} ?dim=${released || (s && !s.connected)} .label=${s ? t("What the display should show: {name}", { name: s.showing }) : ""}></ph-led>
        ${s ? html`<div class="now" aria-live="polite"><b>${this.shownName(s)}</b><span>${this.what(s)}</span></div>` : nothing}
        ${phase ? html`<label class="level" title=${t("Brightness for the {phase}", { phase: t(phase) })}>
          ${icon(svg`<circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4"/>`)}
          <input type="range" min="0" max="100" .value=${String(level)} aria-label=${t("Brightness")} ?disabled=${this.busy}
            @input=${(e) => { this.level = Number(e.target.value); }} @change=${(e) => this.setLevel(Number(e.target.value))}>
          <output>${level}</output></label>` : nothing}
      </div>
      <div class="stack">
        <section><h2 class="muted">${t("What the display shows")}</h2>
          <ph-modes .current=${this.state?.mode} .styles=${this.styles}></ph-modes></section>
        <ph-notify .sprites=${this.sprites}></ph-notify>
      </div>
    </div>`;
  }
}
customElements.define("ph-display", DisplayTab);



// The Divoom community gallery: browse by category, show a picture now or keep it in a profile.
const CATEGORIES = ["recommended", "top", "new", "animal", "food", "character", "emoji", "nature", "plant", "person", "festival", "pattern", "symbol", "creative", "daily"];
const categoryName = (c) => ({ recommended: t("Recommended"), top: t("Top"), new: t("New"), animal: t("Animals"), food: t("Food"), character: t("Characters"),
  emoji: t("Emoji"), nature: t("Nature"), plant: t("Plants"), person: t("People"), festival: t("Festivals"), pattern: t("Patterns"), symbol: t("Symbols"),
  creative: t("Creative"), daily: t("Everyday") })[c] ?? c;
const gifURL = (file) => `api/gallery/gif?file=${encodeURIComponent(file)}`;

class GalleryTab extends LitElement {
  static properties = { state: { attribute: false }, category: { state: true }, sort: { state: true }, items: { state: true }, page: { state: true },
    more: { state: true }, loading: { state: true }, picked: { state: true }, target: { state: true }, busy: { state: true }, problem: { state: true },
    query: { state: true }, searched: { state: true }, indexing: { state: true }, lists: { state: true }, open: { state: true },
    saving: { state: true }, newList: { state: true }, renaming: { state: true } };
  static styles = [base, css`
    .bar { display: grid; gap: 10px; margin-bottom: 14px; }
    .cats { display: flex; gap: 6px; flex-wrap: wrap; }
    .find { display: flex; gap: 8px; }
    .find input { flex: 1; min-width: 0; height: 40px; border-radius: 12px; padding: 0 14px; }
    .cat { flex: none; border: 1px solid var(--line); background: none; border-radius: 999px; height: 34px; padding: 0 14px; font-size: 13px; color: var(--muted); cursor: pointer; }
    .cat[aria-pressed=true] { color: var(--on-accent); background: var(--accent); border-color: var(--accent); font-weight: 600; }
    .cat.mine { color: var(--ink); border-color: color-mix(in srgb, var(--accent) 50%, var(--line)); }
    .grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(86px, 1fr)); gap: 8px; }
    .item { position: relative; aspect-ratio: 1; padding: 0; border: 1px solid var(--line); border-radius: 12px; background: #000; overflow: hidden; cursor: pointer; }
    .item img { width: 100%; height: 100%; display: block; }
    .item[aria-pressed=true] { border-color: var(--accent); box-shadow: 0 0 0 2px var(--accent); }
    .kept { position: absolute; right: 4px; top: 4px; width: 16px; height: 16px; color: var(--accent); filter: drop-shadow(0 0 3px #000); }
    .kept svg { width: 16px; height: 16px; }
    .more { display: flex; justify-content: center; margin-top: 16px; }
    .sheet { position: sticky; top: 64px; z-index: 3; margin-bottom: 14px; background: var(--surface); border: 1px solid var(--accent); border-radius: 18px; padding: 14px;
      display: grid; grid-template-columns: 96px minmax(0, 1fr); gap: 14px; align-items: start; box-shadow: 0 14px 40px -12px rgba(0, 0, 0, .6); }
    .sheet img { width: 96px; height: 96px; border-radius: 10px; background: #000; }
    .sheet h3 { margin: 0; font-size: 16px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .sheet .acts { display: flex; flex-wrap: wrap; gap: 8px; margin-top: 10px; align-items: center; }
    .sheet select { max-width: 150px; }
    .meta { display: flex; gap: 12px; flex-wrap: wrap; }
    .save[aria-expanded=true] { border-color: var(--accent); color: var(--accent); }
    .saveto { grid-column: 1 / -1; border-top: 1px solid var(--line); padding-top: 12px; display: grid; gap: 8px; }
    .saveto label { display: flex; align-items: center; gap: 10px; font-size: 14px; cursor: pointer; }
    .saveto input[type=checkbox] { width: 18px; height: 18px; accent-color: var(--accent); margin: 0; }
    .make { display: flex; gap: 8px; }
    .make input { flex: 1; min-width: 0; }
    .cards { display: grid; grid-template-columns: repeat(auto-fill, minmax(150px, 1fr)); gap: 10px; }
    .card-list { border: 1px solid var(--line); border-radius: 16px; background: var(--surface); padding: 10px; cursor: pointer; text-align: left; display: grid; gap: 8px; color: inherit; }
    .card-list:hover { border-color: color-mix(in srgb, var(--ink) 30%, var(--line)); }
    .thumbs { display: grid; grid-template-columns: repeat(2, 1fr); gap: 3px; aspect-ratio: 1; background: #000; border-radius: 10px; overflow: hidden; }
    .thumbs img { width: 100%; height: 100%; display: block; }
    .card-list b { font-weight: 600; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .head-list { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; margin-bottom: 12px; }
    .head-list h2 { margin: 0; font-size: 18px; flex: 1; min-width: 0; color: var(--ink); }
    .empty { padding: 30px 0; text-align: center; }
  `];
  constructor() { super(); this.category = "recommended"; this.sort = "likes"; this.items = []; this.page = 1; this.lists = []; }
  connectedCallback() { super.connectedCallback(); this.loadLists(); if (!this.items.length) this.load(true); }

  async loadLists() { try { this.lists = (await api("GET", "lists")).lists; } catch { /* the status poll shows outages */ } }
  listsWith(file) { return this.lists.filter((l) => l.items.some((i) => i.file === file)).map((l) => l.name); }
  get current() { return this.lists.find((l) => l.name === this.open); }

  async listCall(method, path, body) {
    this.busy = true;
    try { this.lists = (await api(method, path, body)).lists; return true; } catch (e) { toast(this, e.message); return false; } finally { this.busy = false; }
  }
  async keep(list, item, on) {
    const path = `lists/${encodeURIComponent(list)}/items`;
    await (on ? this.listCall("POST", path, { file: item.file, name: item.name, author: item.author, likes: item.likes })
      : this.listCall("DELETE", `${path}?file=${encodeURIComponent(item.file)}`));
    if (this.open === list && !on) this.picked = null;
  }
  async makeList(item) {
    const name = (this.newList ?? "").trim();
    if (!name || !(await this.listCall("POST", "lists", { name }))) return;
    this.newList = "";
    if (item) await this.keep(this.lists.at(-1).name, item, true);
  }
  async renameList() {
    const name = (this.renaming ?? "").trim();
    if (name && (await this.listCall("PUT", `lists/${encodeURIComponent(this.open)}`, { name }))) { this.open = this.lists.find((l) => l.name.toLowerCase() === name.toLowerCase())?.name; this.renaming = null; }
  }
  async deleteList() {
    if (!confirm(t("Delete the list {name}? Its drawings stay in the gallery.", { name: this.open }))) return;
    if (await this.listCall("DELETE", `lists/${encodeURIComponent(this.open)}`)) { this.open = null; this.picked = null; }
  }

  async load(fresh) {
    if (this.category === "lists") return;
    this.loading = true;
    this.problem = "";
    const page = fresh ? 1 : this.page + 1;
    try {
      const r = this.searched
        ? await api("GET", `gallery/search?q=${encodeURIComponent(this.searched)}`)
        : await api("GET", `gallery?category=${this.category}&sort=${this.sort}&page=${page}`);
      const seen = new Set(fresh ? [] : this.items.map((i) => i.file));
      this.items = [...(fresh ? [] : this.items), ...r.items.filter((i) => !seen.has(i.file))];
      this.page = page;
      this.more = r.more;
      this.indexing = r.of && r.indexed < r.of ? r : null;
    } catch (e) { this.problem = e.message; }
    this.loading = false;
  }
  pick(field, value) {
    this[field] = value;
    Object.assign(this, { picked: null, searched: "", query: "", open: null, saving: false, renaming: null });
    this.load(true);
  }
  search(e) {
    e.preventDefault();
    this.searched = (this.query ?? "").trim();
    if (this.category === "lists") this.category = "recommended";
    Object.assign(this, { picked: null, open: null });
    this.load(true);
  }

  async show() {
    this.busy = true;
    try { await api("POST", "gallery/show", { file: this.picked.file, name: this.picked.name, for: "30s" }); send(this, "refresh"); toast(this, t("Showing {name} for 30 s", { name: this.picked.name })); }
    catch (e) { toast(this, e.message); }
    this.busy = false;
  }
  async save() {
    const channel = this.target ?? this.state?.channels?.[0]?.name;
    this.busy = true;
    try { await api("POST", "gallery/save", { file: this.picked.file, name: this.picked.name, channel }); toast(this, t("Saved in {channel}", { channel })); }
    catch (e) { toast(this, e.message); }
    this.busy = false;
  }

  sheet(p) {
    const channels = this.state?.channels ?? [];
    const inLists = this.listsWith(p.file);
    return html`<div class="sheet" role="dialog" aria-label=${p.name}>
      <img class="pix" src=${gifURL(p.file)} alt="">
      <div><h3>${p.name}</h3><span class="small muted meta"><span>${p.author}</span><span>${p.likes} ♥</span></span>
        <div class="acts">
          <button class="btn small save" aria-expanded=${String(!!this.saving)} @click=${() => { this.saving = !this.saving; }}>
            ${icons.bookmark}${inLists.length ? t("Saved in {n}", { n: inLists.length }) : t("Save")}</button>
          <button class="btn primary small" ?disabled=${this.busy} @click=${() => this.show()}>${t("Show now")}</button>
          ${channels.length ? html`<select aria-label=${t("Profile")} @change=${(e) => { this.target = e.target.value; }}>
              ${channels.map((c) => html`<option value=${c.name} ?selected=${c.name === (this.target ?? channels[0].name)}>${c.name}</option>`)}</select>
            <button class="btn small" ?disabled=${this.busy} @click=${() => this.save()}>${t("Add to profile")}</button>` : nothing}
          <button class="icon" aria-label=${t("Close")} @click=${() => { this.picked = null; this.saving = false; }}>${icons.close}</button>
        </div></div>
      ${this.saving ? html`<div class="saveto">
        ${this.lists.map((l) => html`<label><input type="checkbox" .checked=${inLists.includes(l.name)} ?disabled=${this.busy}
          @change=${(e) => this.keep(l.name, p, e.target.checked)}>${l.name}<span class="small muted">${l.items.length}</span></label>`)}
        <form class="make" @submit=${(e) => { e.preventDefault(); this.makeList(p); }}>
          <input placeholder=${t("New list")} aria-label=${t("New list")} maxlength="40" .value=${this.newList ?? ""} @input=${(e) => { this.newList = e.target.value; }}>
          <button class="btn small" type="submit" ?disabled=${this.busy || !(this.newList ?? "").trim()}>${t("Create and save")}</button>
        </form></div>` : nothing}
    </div>`;
  }

  grid(items) {
    const p = this.picked;
    const kept = new Set(this.lists.flatMap((l) => l.items.map((i) => i.file)));
    return html`<div class="grid">${repeat(items, (i) => i.file, (i) => html`<button class="item" aria-pressed=${String(p?.file === i.file)} title=${i.name} aria-label=${i.name}
      @click=${() => { this.picked = i; this.saving = false; }}><img class="pix" src=${gifURL(i.file)} alt="" loading="lazy">${kept.has(i.file) ? html`<span class="kept" aria-label=${t("Saved")}>${icons.bookmark}</span>` : nothing}</button>`)}</div>`;
  }

  myLists() {
    const l = this.current;
    if (l) {
      return html`<div class="head-list">
          <button class="icon" aria-label=${t("All my lists")} @click=${() => { this.open = null; this.picked = null; }}>${icons.back}</button>
          ${this.renaming !== null && this.renaming !== undefined
            ? html`<form class="make" style="flex:1" @submit=${(e) => { e.preventDefault(); this.renameList(); }}>
                <input aria-label=${t("List name")} maxlength="40" .value=${this.renaming} @input=${(e) => { this.renaming = e.target.value; }}>
                <button class="btn small" type="submit">${t("Rename")}</button><button class="btn small" type="button" @click=${() => { this.renaming = null; }}>${t("Cancel")}</button></form>`
            : html`<h2>${l.name}</h2><span class="small muted">${t("{n} drawings", { n: l.items.length })}</span>
                <button class="btn small" @click=${() => { this.renaming = l.name; }}>${t("Rename")}</button>
                <button class="icon trash" aria-label=${t("Delete list")} @click=${() => this.deleteList()}>${icons.trash}</button>`}
        </div>
        ${this.picked ? this.sheet(this.picked) : nothing}
        ${l.items.length ? this.grid(l.items) : html`<p class="empty muted">${t("This list is empty. Open a drawing in the gallery and tap Save.")}</p>`}`;
    }
    return html`
      <form class="make" style="margin-bottom:14px" @submit=${(e) => { e.preventDefault(); this.makeList(); }}>
        <input placeholder=${t("New list")} aria-label=${t("New list")} maxlength="40" .value=${this.newList ?? ""} @input=${(e) => { this.newList = e.target.value; }}>
        <button class="btn" type="submit" ?disabled=${this.busy || !(this.newList ?? "").trim()}>${t("Create list")}</button>
      </form>
      ${this.lists.length ? html`<div class="cards">${this.lists.map((x) => html`<button class="card-list" @click=${() => { this.open = x.name; this.picked = null; }}>
          <div class="thumbs">${[0, 1, 2, 3].map((k) => x.items[k] ? html`<img class="pix" src=${gifURL(x.items[k].file)} alt="" loading="lazy">` : html`<span></span>`)}</div>
          <b>${x.name}</b><span class="small muted">${t("{n} drawings", { n: x.items.length })}</span></button>`)}</div>`
        : html`<p class="empty muted">${t("No lists yet. Create one here, or open a drawing and tap Save.")}</p>`}`;
  }

  render() {
    const mine = this.category === "lists" && !this.searched;
    return html`
      <div class="bar">
        <form class="find" role="search" @submit=${(e) => this.search(e)}>
          <input type="search" placeholder=${t("Search: cat, heart, pumpkin…")} aria-label=${t("Search the gallery")} .value=${this.query ?? ""} @input=${(e) => { this.query = e.target.value; }}>
          <button class="btn primary" type="submit">${t("Search")}</button>
        </form>
        <div class="cats" role="group" aria-label=${t("Categories")}>
          <button class="cat mine" aria-pressed=${String(mine)} @click=${() => this.pick("category", "lists")}>${t("My lists")}</button>
          ${CATEGORIES.map((c) => html`<button class="cat" aria-pressed=${String(!this.searched && c === this.category)} @click=${() => this.pick("category", c)}>${categoryName(c)}</button>`)}</div>
        ${this.searched
          ? html`<p class="small muted">${t("Results for “{q}” among the most liked of every category.", { q: this.searched })}
              ${this.indexing ? html`<br>${t("Still gathering the gallery ({n} of {of} pages); search again in a minute for more.", { n: this.indexing.indexed, of: this.indexing.of })}` : nothing}</p>`
          : mine ? nothing
          : html`<div>${segmented([["likes", t("Most liked")], ["new", t("Newest")]], this.sort, (v) => this.pick("sort", v))}</div>`}
      </div>
      ${mine ? this.myLists() : html`
        ${this.picked ? this.sheet(this.picked) : nothing}
        ${this.problem ? html`<p class="err">${this.problem}</p>` : nothing}
        ${!this.items.length && !this.loading && !this.problem ? html`<p class="empty muted">${this.searched ? t("Nothing found. Try another word, in English it finds more.") : t("Nothing in this category right now.")}</p>` : nothing}
        ${this.grid(this.items)}
        <div class="more">${this.loading ? html`<span class="muted small">${t("Loading")}</span>`
          : this.more ? html`<button class="btn" @click=${() => this.load(false)}>${t("Load more")}</button>` : nothing}</div>`}`;
  }
}
customElements.define("ph-gallery", GalleryTab);

// Links a Spotify account; what it plays can then show on the display, over scenes and pictures.
class SpotifyCard extends LitElement {
  static properties = { state: { attribute: false }, info: { state: true }, draft: { state: true }, busy: { state: true } };
  static styles = [base, css`
    .grid { display: grid; gap: 14px; }
    .row { display: flex; gap: 8px; flex-wrap: wrap; align-items: center; }
    .row input { flex: 1 1 260px; font-family: var(--font-data); }
    ol { margin: 0; padding-left: 20px; display: grid; gap: 6px; font-size: 14px; }
    code { font-family: var(--font-data); font-size: 12px; background: var(--chip); padding: 2px 6px; border-radius: 6px; overflow-wrap: anywhere; }
    a { color: var(--accent); }
    .now { display: grid; gap: 2px; padding: 12px; border-radius: 12px; background: var(--chip); }
    .now b { font-weight: 600; }
    .toggle { display: inline-flex; align-items: center; gap: 8px; font-size: 14px; cursor: pointer; }
    .devices { display: flex; flex-wrap: wrap; gap: 8px 18px; margin: 4px 0 6px; }
    input[type=checkbox] { width: 18px; height: 18px; accent-color: var(--accent); margin: 0; }
  `];
  connectedCallback() { super.connectedCallback(); this.load(); this.timer = setInterval(() => { if (!document.hidden) this.load(); }, 5000); }
  disconnectedCallback() { super.disconnectedCallback(); clearInterval(this.timer); }
  async load() { try { this.info = await api("GET", "spotify"); } catch { /* the status poll shows outages */ } }
  async call(method, body) {
    this.busy = true;
    try { this.info = await api(method, "spotify", body); } catch (e) { toast(this, e.message); }
    this.busy = false;
  }
  async music(patch) {
    const next = { ...(this.state?.music ?? { enabled: false }), ...patch };
    if (!next.view) delete next.view;
    if (!next.devices?.length) delete next.devices;
    try { saved(this, await api("PUT", "settings", { music: next })); send(this, "refresh"); } catch (e) { toast(this, e.message); }
  }
  callback() {
    const host = location.hostname === "localhost" ? "127.0.0.1" : location.hostname;
    return `${location.protocol}//${host}${location.port ? `:${location.port}` : ""}/api/spotify/callback`;
  }

  render() {
    const i = this.info;
    if (!i) return nothing;
    const m = this.state?.music ?? {};
    const show = m.view?.mode ?? "cover";
    return html`<section class="card" aria-labelledby="sp-h">
      <div class="head"><h2 id="sp-h">Spotify</h2>${i.connected ? html`<span class="tag on">${t("Connected")}</span>` : nothing}</div>
      <div class="grid">
        ${!i.configured ? html`
          <ol>
            <li>${t("Create an app at")} <a href="https://developer.spotify.com/dashboard" target="_blank" rel="noopener">developer.spotify.com/dashboard</a> ${t("with the Web API.")}</li>
            <li>${t("Add this redirect URI to it:")} <code>${this.callback()}</code></li>
            <li>${t("Paste its client ID here.")}</li>
          </ol>
          <div class="row"><input placeholder=${t("Client ID")} aria-label=${t("Client ID")} .value=${this.draft ?? ""} @input=${(e) => { this.draft = e.target.value; }}>
            <button class="btn primary" ?disabled=${this.busy || !this.draft} @click=${() => this.call("PUT", { client_id: this.draft })}>${t("Save")}</button></div>`
        : !i.connected ? html`
          <p class="small muted">${t("The Spotify app must list this redirect URI:")} <code>${this.callback()}</code></p>
          <div class="row"><a class="btn primary" href="api/spotify/login">${t("Connect Spotify")}</a></div>`
        : html`
          <p class="small muted">${t("Signed in as {user}.", { user: i.user ?? "?" })}</p>
          ${i.now ? html`<div class="now"><b>${i.now.track}</b><span class="small">${i.now.artists}</span>
            <span class="small muted">${i.now.playing ? t("Playing on {device}", { device: i.now.device || "?" }) : t("Paused")}</span></div>`
            : html`<p class="small muted">${t("Nothing is playing.")}</p>`}
          <label class="toggle"><input type="checkbox" .checked=${!!m.enabled} @change=${(e) => this.music({ enabled: e.target.checked })}>${t("Show on the display while it plays")}</label>
          <div><span class="label">${t("What to show")}</span>${segmented([["cover", t("Album cover")], ["visualizer", t("Music")], ["animations", t("My animations")]], show,
            (v) => this.music({ view: v === "cover" ? undefined : { mode: v } }))}</div>
          ${i.devices?.length ? html`<div><span class="label">${t("Only when it plays on")}</span>
            <div class="devices">${i.devices.map((d) => html`<label class="toggle"><input type="checkbox" .checked=${(m.devices ?? []).includes(d)}
              @change=${(e) => this.music({ devices: e.target.checked ? [...(m.devices ?? []), d] : (m.devices ?? []).filter((x) => x !== d) })}>${d}</label>`)}</div>
            <p class="small muted">${(m.devices ?? []).length ? t("Music on any other device is left alone.") : t("None ticked: music on any device shows, also away from home.")}</p></div>` : nothing}
          <p class="small muted">${t("Alerts and a mode picked by hand stay on top.")}</p>
          ${i.error ? html`<p class="err">${i.error}</p>` : nothing}
          <div class="row"><button class="btn small" ?disabled=${this.busy} @click=${() => this.call("DELETE")}>${t("Disconnect")}</button></div>`}
      </div>
    </section>`;
  }
}
customElements.define("ph-spotify", SpotifyCard);


// Everything about music in one place: what plays, how it shows, the account, the display's own visualiser.
class MusicTab extends LitElement {
  static properties = { state: { attribute: false }, status: { attribute: false }, styles: { attribute: false }, info: { state: true }, busy: { state: true } };
  static styles = [base, css`
    .layout { display: grid; grid-template-columns: minmax(0, 1fr); gap: 18px; }
    @media (min-width: 900px) { .layout { grid-template-columns: minmax(280px, 360px) minmax(0, 1fr); align-items: start; } .side { position: sticky; top: 88px; } }
    .side, .main { display: grid; gap: 18px; min-width: 0; }
    .playing { display: grid; gap: 12px; justify-items: center; text-align: center; }
    ph-led { width: min(100%, 300px); }
    .playing b { font-size: 17px; font-weight: 600; }
    .playing .who { color: var(--muted); font-size: 14px; }
    .state { font-size: 13px; padding: 6px 12px; border-radius: 999px; background: var(--chip); }
    .state.on { background: color-mix(in srgb, var(--ok) 18%, transparent); color: var(--ok); }
    .choices { display: flex; flex-wrap: wrap; gap: 6px; }
    .choice { border: 1px solid var(--line); background: none; border-radius: 999px; height: 36px; width: 44px; font-size: 14px; color: var(--muted); cursor: pointer; }
    .choice[aria-pressed=true] { color: var(--on-accent); background: var(--accent); border-color: var(--accent); font-weight: 600; }
    .row { display: flex; gap: 10px; align-items: center; flex-wrap: wrap; margin-top: 12px; }
  `];
  connectedCallback() { super.connectedCallback(); this.load(); this.timer = setInterval(() => { if (!document.hidden) this.load(); }, 5000); }
  disconnectedCallback() { super.disconnectedCallback(); clearInterval(this.timer); }
  async load() { try { this.info = await api("GET", "spotify"); } catch { /* the status poll shows outages */ } }

  // Why the music shows on the display or not, in the user's terms.
  why() {
    const i = this.info, m = this.state?.music ?? {}, s = this.status;
    if (s?.layer === "music") return [true, t("On the display now")];
    if (!i?.now?.playing) return [false, t("Paused")];
    if (!m.enabled) return [false, t("Not shown: showing music on the display is off")];
    if (m.devices?.length && !m.devices.includes(i.now.device)) return [false, t("Not shown: {device} is not one of the picked devices", { device: i.now.device })];
    if (s?.layer === "alert" || s?.layer === "mode") return [false, t("Not shown: an alert or a mode picked by hand is on top")];
    return [false, t("Not shown yet")];
  }

  async visualizer(style) {
    this.busy = true;
    try { saved(this, await api("PUT", "mode", { mode: "visualizer", ...(style ? { style } : {}) })); send(this, "refresh"); } catch (e) { toast(this, e.message); }
    this.busy = false;
  }
  async automatic() {
    this.busy = true;
    try { saved(this, await api("DELETE", "mode")); send(this, "refresh"); } catch (e) { toast(this, e.message); }
    this.busy = false;
  }

  render() {
    const now = this.info?.now;
    const mode = this.state?.mode;
    const vis = mode?.mode === "visualizer" ? (mode.style ?? 0) : -1;
    const [on, why] = this.why();
    return html`<div class="layout">
      <div class="side">
        <section class="card playing" aria-labelledby="np-h">
          <div class="head" style="width:100%"><h2 id="np-h">${t("Now playing")}</h2></div>
          ${now ? html`
            <ph-led .src=${`api/spotify/cover.png?t=${encodeURIComponent(now.track)}`} .label=${now.track}></ph-led>
            <div><b>${now.track}</b><div class="who">${now.artists}</div><div class="small muted">${now.device}</div></div>
            <span class="state ${on ? "on" : ""}">${why}</span>`
          : html`<p class="muted">${this.info?.connected ? t("Nothing is playing.") : t("Connect Spotify below to see what plays.")}</p>`}
        </section>
      </div>
      <div class="main">
        <ph-spotify .state=${this.state}></ph-spotify>
        <section class="card" aria-labelledby="vz-h">
          <div class="head"><h2 id="vz-h">${t("The Evo's visualiser")}</h2></div>
          <div class="choices" role="group" aria-label=${t("Visualiser")}>${Array.from({ length: this.styles?.visualizer ?? 12 }, (_, k) =>
            html`<button class="choice" aria-pressed=${String(vis === k)} ?disabled=${this.busy} @click=${() => this.visualizer(k)}>${k + 1}</button>`)}</div>
          <p class="small muted">${t("It follows what the Evo plays as a speaker, not the room's sound.")}</p>
          ${vis >= 0 ? html`<div class="row"><button class="btn small" ?disabled=${this.busy} @click=${() => this.automatic()}>${t("Back to automatic")}</button></div>` : nothing}
        </section>
      </div>
    </div>`;
  }
}
customElements.define("ph-music", MusicTab);

class ConnectionCard extends LitElement {
  static properties = { status: { attribute: false }, busy: { state: true } };
  static styles = [base, css`
    dl { margin: 0; display: grid; grid-template-columns: max-content 1fr; }
    dt, dd { margin: 0; padding: 8px 0; border-top: 1px solid var(--line); min-width: 0; }
    dt { color: var(--muted); font-size: 13px; padding-right: 16px; }
    dd { font-size: 14px; display: flex; align-items: center; gap: 8px; justify-content: flex-end; text-align: right; overflow-wrap: anywhere; }
    .actions { margin-top: 14px; display: flex; flex-wrap: wrap; gap: 10px; align-items: center; }
  `];
  async call(path) {
    this.busy = true;
    try { await api("POST", path); send(this, "refresh"); } catch (e) { toast(this, e.message); }
    this.busy = false;
  }
  render() {
    const s = this.status;
    if (!s) return nothing;
    const [tone, linkText] = linkState(s);
    const released = !!s.released_until;
    return html`<section class="card" aria-labelledby="co-h">
      <div class="head"><h2 id="co-h">${t("Connection")}</h2></div>
      <dl>
        <dt>${t("Display")}</dt><dd><span class="dot ${tone}"></span>${linkText}</dd>
        <dt>${t("Last answer")}</dt><dd>${ago(s.last_ack)}</dd>
        <dt>${t("Bridge")}</dt><dd class="mono small">${s.address}</dd>
      </dl>
      <div class="actions">
        ${released
          ? html`<button class="btn primary" ?disabled=${this.busy} @click=${() => this.call("claim")}>${t("Take back")}</button><span class="small muted">${t("Released until {time}", { time: until(s.released_until) })}</span>`
          : html`<button class="btn" ?disabled=${this.busy} @click=${() => this.call("release")}>${t("Lend to the phone")}</button><span class="small muted">${t("Lets the Divoom app use the display for a while.")}</span>`}
      </div>
    </section>`;
  }
}
customElements.define("ph-connection", ConnectionCard);

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

const TABS = [
  ["display", "Display", icon(svg`<rect x="4" y="4" width="16" height="16" rx="3"/><path d="M8.5 8.5h.01M12 8.5h.01M15.5 8.5h.01M8.5 12h.01M12 12h.01M15.5 12h.01M8.5 15.5h.01M12 15.5h.01M15.5 15.5h.01" stroke-width="2.6"/>`)],
  ["gallery", "Gallery", icon(svg`<rect x="3.5" y="3.5" width="7" height="7" rx="1.5"/><rect x="13.5" y="3.5" width="7" height="7" rx="1.5"/><rect x="3.5" y="13.5" width="7" height="7" rx="1.5"/><path d="m14 20 2.5-3.5 2 2.5 1.5-2"/>`)],
  ["music", "Music", icon(svg`<path d="M9 18V5l11-2v13"/><circle cx="6" cy="18" r="3"/><circle cx="17" cy="16" r="3"/>`)],
  ["profiles", "Profiles", icon(svg`<path d="M4 6h16M4 12h10M4 18h7"/><circle cx="18" cy="16" r="3"/>`)],
  ["schedule", "Schedule", icon(svg`<rect x="3.5" y="5" width="17" height="15" rx="2"/><path d="M3.5 9.5h17M8 3v4M16 3v4"/>`)],
  ["settings", "Settings", icon(svg`<circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.7 1.7 0 0 0 .3 1.8l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.7 1.7 0 0 0-1.8-.3 1.7 1.7 0 0 0-1 1.5V21a2 2 0 1 1-4 0v-.1a1.7 1.7 0 0 0-1.1-1.5 1.7 1.7 0 0 0-1.8.3l-.1.1a2 2 0 1 1-2.8-2.8l.1-.1a1.7 1.7 0 0 0 .3-1.8 1.7 1.7 0 0 0-1.5-1H3a2 2 0 1 1 0-4h.1a1.7 1.7 0 0 0 1.5-1.1 1.7 1.7 0 0 0-.3-1.8l-.1-.1a2 2 0 1 1 2.8-2.8l.1.1a1.7 1.7 0 0 0 1.8.3H9a1.7 1.7 0 0 0 1-1.5V3a2 2 0 1 1 4 0v.1a1.7 1.7 0 0 0 1 1.5 1.7 1.7 0 0 0 1.8-.3l.1-.1a2 2 0 1 1 2.8 2.8l-.1.1a1.7 1.7 0 0 0-.3 1.8V9a1.7 1.7 0 0 0 1.5 1H21a2 2 0 1 1 0 4h-.1a1.7 1.7 0 0 0-1.5 1Z"/>`)],
];

class PixelhubApp extends LitElement {
  static properties = { st: { state: true }, status: { state: true }, sprites: { state: true }, modes: { state: true }, version: { state: true }, offline: { state: true }, gone: { state: true }, tab: { state: true } };
  static styles = [base, css`
    header { position: sticky; top: 0; z-index: 5; background: color-mix(in srgb, var(--bg) 85%, transparent); backdrop-filter: blur(12px); }
    .bar { max-width: 1120px; margin: 0 auto; padding: 12px 16px; display: flex; align-items: center; gap: 16px; }
    .brand { font-family: var(--font-pixel); font-size: 20px; display: flex; align-items: center; gap: 10px; letter-spacing: .02em; }
    .brand img { width: 24px; height: 24px; }
    .link { display: flex; align-items: center; gap: 8px; font-size: 13px; color: var(--muted); white-space: nowrap; margin-left: auto; }
    nav { display: flex; gap: 4px; }
    nav button { border: 0; background: none; border-radius: 12px; color: var(--muted); cursor: pointer; display: flex; align-items: center; gap: 8px; font-size: 14px; font-weight: 500; padding: 8px 12px; }
    nav button svg { width: 20px; height: 20px; }
    nav button:hover { color: var(--ink); }
    nav button[aria-current=page] { color: var(--ink); background: var(--chip); }
    nav button[aria-current=page] svg { color: var(--accent); }
    .top { margin-left: 8px; }
    .bottom { display: none; }
    @media (max-width: 759px) {
      .top { display: none; }
      .bottom { display: grid; grid-template-columns: repeat(6, 1fr); position: fixed; left: 0; right: 0; bottom: 0; z-index: 6; gap: 0;
        padding: 6px 8px max(8px, env(safe-area-inset-bottom, 0px)); background: color-mix(in srgb, var(--bg) 90%, transparent); backdrop-filter: blur(12px); border-top: 1px solid var(--line); }
      .bottom button { flex-direction: column; gap: 3px; font-size: 11px; padding: 6px 2px; }
      .bottom button[aria-current=page] { background: none; }
      main { padding-bottom: 96px !important; }
    }
    main { max-width: 1120px; margin: 0 auto; padding: 8px 16px 48px; }
    .tab { display: grid; grid-template-columns: minmax(0, 1fr); gap: 18px; min-width: 0; }
    .tab.two { grid-template-columns: minmax(0, 1fr); }
    @media (min-width: 900px) { .tab.two { grid-template-columns: minmax(0, 1fr) minmax(0, 1fr); align-items: start; } }
    footer { max-width: 1120px; margin: 0 auto; padding: 0 16px 32px; font-size: 12px; color: var(--muted); }
    @media (max-width: 759px) { footer { padding-bottom: 96px; } }
    .toast { position: fixed; left: max(16px, env(safe-area-inset-left, 0px)); bottom: max(16px, env(safe-area-inset-bottom, 0px)); z-index: 10; max-width: min(440px, calc(100vw - 32px));
      display: flex; align-items: center; gap: 10px; padding: 10px 10px 10px 14px; font-size: 13px; background: var(--surface); color: var(--ink); border: 1px solid var(--line); border-radius: 12px;
      box-shadow: 0 12px 40px rgba(0, 0, 0, .35); animation: rise .18s ease-out; }
    @media (max-width: 759px) { .toast { bottom: calc(80px + env(safe-area-inset-bottom, 0px)); } }
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
    this.tab = TABS.some(([id]) => id === location.hash.slice(1)) ? location.hash.slice(1) : "display";
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
    const problem = new URLSearchParams(location.search).get("spotify_error");
    if (problem) {
      this.toasts.show({ text: t("Spotify did not connect: {reason}", { reason: problem }) });
      history.replaceState(null, "", location.pathname + location.hash);
    }
    this.pollStatus();
    this.loadState();
    api("GET", "sprites").then((r) => { this.sprites = r.sprites; }).catch(() => {});
    api("GET", "modes").then((r) => { this.modes = r.styles; }).catch(() => {});
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
      text: t("Profile {name} deleted", { name }),
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

  go(id) {
    this.tab = id;
    history.replaceState(null, "", `#${id}`);
    scrollTo({ top: 0 });
  }

  nav(cls) {
    return html`<nav class=${cls} aria-label=${t("Sections")}>${TABS.map(([id, label, glyph]) =>
      html`<button aria-current=${this.tab === id ? "page" : "false"} @click=${() => this.go(id)}>${glyph}<span>${t(label)}</span></button>`)}</nav>`;
  }

  page(view) {
    switch (this.tab) {
      case "gallery": return html`<ph-gallery .state=${view}></ph-gallery>`;
      case "music": return html`<ph-music .state=${view} .status=${this.status} .styles=${this.modes}></ph-music>`;
      case "profiles": return html`<div class="tab"><ph-switch .state=${view} .status=${this.status}></ph-switch>
        <ph-channels .state=${view} .status=${this.status} .sprites=${this.sprites}></ph-channels></div>`;
      case "schedule": return html`<div class="tab">${view ? html`<ph-schedule .base=${view}></ph-schedule>` : nothing}</div>`;
      case "settings": return html`<div class="tab two">${view ? html`<ph-settings .base=${view}></ph-settings>` : nothing}<ph-connection .status=${this.status}></ph-connection></div>`;
      default: return html`<ph-display .status=${this.status} .state=${view} .sprites=${this.sprites} .styles=${this.modes} ?offline=${this.offline}></ph-display>`;
    }
  }

  render() {
    const view = this.view;
    const [tone, linkText] = this.status ? linkState(this.status) : ["", t("Loading")];
    return html`
      <header><div class="bar">
        <div class="brand"><img src="favicon.svg" alt="">pixelhub</div>
        ${this.nav("top")}
        <div class="link" title=${linkText}><span class="dot ${this.offline ? "bad" : tone}"></span><span>${this.offline ? t("No answer") : linkText}</span></div>
      </div></header>
      <main>${this.page(view)}</main>
      <footer>pixelhub ${this.version ?? ""}</footer>
      ${this.nav("bottom")}
      ${this.toasts.view()}`;
  }
}
customElements.define("pixelhub-app", PixelhubApp);
