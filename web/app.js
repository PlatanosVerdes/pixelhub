// pixelhub's web UI: the app element fetches, every component below renders what it is given.
import { LitElement, html, svg, css, nothing, keyed, repeat } from "./vendor/lit-all.min.js";
import { lang, t } from "./i18n.js";
import { clockFace, lightFace } from "./faces.js";

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

const same = (a, b) => JSON.stringify(a) === JSON.stringify(b);

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

const icon = (d) => svg`<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${d}</svg>`;
const icons = {
  trash: icon(svg`<path d="M4 7h16M10 11v6M14 11v6M5 7l1 12a2 2 0 0 0 2 2h8a2 2 0 0 0 2-2l1-12M9 7V4h6v3"/>`),
  chevron: icon(svg`<path d="m6 9 6 6 6-6"/>`),
  up: icon(svg`<path d="m6 15 6-6 6 6"/>`),
  down: icon(svg`<path d="m6 9 6 6 6-6"/>`),
  plus: icon(svg`<path d="M12 5v14M5 12h14"/>`),
  check: icon(svg`<path d="m5 12.5 4.5 4.5L19 7.5"/>`),
  close: icon(svg`<path d="M6 6l12 12M18 6 6 18"/>`),
  upload: icon(svg`<path d="M12 16V4M7 9l5-5 5 5M4 16v3a1 1 0 0 0 1 1h14a1 1 0 0 0 1-1v-3"/>`),
  locate: icon(svg`<circle cx="12" cy="12" r="3.5"/><path d="M12 2v4M12 18v4M2 12h4M18 12h4"/>`),
  bookmark: icon(svg`<path d="M6 3.5h12v17l-6-4-6 4z"/>`),
  heart: icon(svg`<path d="M12 20s-7.5-4.6-7.5-10.2A4.3 4.3 0 0 1 12 7.2a4.3 4.3 0 0 1 7.5 2.6C19.5 15.4 12 20 12 20z"/>`),
  heartFull: svg`<svg viewBox="0 0 24 24" fill="currentColor" stroke="currentColor" stroke-width="1.75" stroke-linejoin="round" aria-hidden="true"><path d="M12 20s-7.5-4.6-7.5-10.2A4.3 4.3 0 0 1 12 7.2a4.3 4.3 0 0 1 7.5 2.6C19.5 15.4 12 20 12 20z"/></svg>`,
  back: icon(svg`<path d="m15 18-6-6 6-6"/>`),
  link: icon(svg`<path d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71"/><path d="M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71"/>`),
  unlink: icon(svg`<path d="m18.84 12.25 1.72-1.71a5 5 0 0 0-7.07-7.07l-1.72 1.71"/><path d="m5.17 11.75-1.71 1.71a5 5 0 0 0 7.07 7.07l1.71-1.71"/><path d="M8 2v3M2 8h3M16 22v-3M22 16h-3"/>`),
};

const base = css`
  :host { display: block; }
  :host([hidden]) { display: none; }
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
  .chain { flex: none; display: inline-flex; color: var(--muted); }
  .chain svg { width: 16px; height: 16px; }
  .chain.ok { color: var(--ok); }
  .chain.warn { color: var(--warn); }
  .chain.bad { color: var(--bad); }
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
  .btn.primary:disabled { background: var(--chip); border-color: var(--line); color: var(--muted); opacity: 1; }
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

const segmented = (options, value, pick) => html`<div class="seg" role="group">${options.map(([v, label]) =>
  html`<button type="button" aria-pressed=${String(v === value)} @click=${() => pick(v)}>${label}</button>`)}</div>`;


// A length of time: a few usual ones, and Custom for any other, typed with its unit.
class DurationPicker extends LitElement {
  static properties = { options: { attribute: false }, value: {}, max: { type: Number }, custom: { state: true }, n: { state: true }, unit: { state: true } };
  static styles = [base, css`
    .seg { align-items: center; }
    .own { display: inline-flex; align-items: center; height: 32px; border-radius: 8px; background: var(--surface); box-shadow: 0 1px 2px rgba(0, 0, 0, .12); padding: 0 4px 0 8px; }
    .own input { height: 28px; border: 0; background: none; padding: 0; text-align: center; font: inherit; font-size: 13px; font-weight: 500; color: var(--ink); -moz-appearance: textfield; }
    .own input::-webkit-inner-spin-button, .own input::-webkit-outer-spin-button { -webkit-appearance: none; margin: 0; }
    .own select { height: 28px; border: 0; background: none; padding: 0 2px 0 4px; font: inherit; font-size: 13px; font-weight: 500; color: var(--ink); cursor: pointer; }
    .own input:focus-visible, .own select:focus-visible { outline: 2px solid var(--accent); outline-offset: 1px; border-radius: 4px; }
  `];
  get known() { return this.options.find(([v]) => seconds(v) === seconds(this.value))?.[0]; }
  willUpdate(c) {
    if (c.has("value") && !this.custom && this.known === undefined && this.value !== undefined) this.openCustom();
  }
  openCustom() {
    const s = seconds(this.value);
    this.unit = s && s % 3600 === 0 ? "h" : s && s % 60 === 0 ? "m" : "s";
    this.n = s ? s / { h: 3600, m: 60, s: 1 }[this.unit] : 1;
    this.custom = true;
  }
  emit(v) { this.dispatchEvent(new CustomEvent("change", { detail: v })); }
  typed() {
    const n = Number(this.n), total = n * { h: 3600, m: 60, s: 1 }[this.unit];
    if (!(n > 0) || (this.max && total > this.max)) return;
    this.emit(`${n}${this.unit}`);
  }
  render() {
    const known = this.custom ? null : this.known;
    return html`<div class="seg" role="group">
      ${this.options.map(([v, label]) => html`<button type="button" aria-pressed=${String(v === known)} @click=${() => { this.custom = false; this.emit(v); }}>${label}</button>`)}
      ${this.custom
        ? html`<span class="own"><input type="number" min="1" step="any" aria-label=${t("Amount")} .value=${String(this.n)} style=${`width:${Math.max(2, String(this.n).length) + 1}ch`}
              @input=${(e) => { this.n = e.target.value; }} @change=${() => this.typed()}>
            <select aria-label=${t("Unit")} @change=${(e) => { this.unit = e.target.value; this.typed(); }}>
              ${[["s", "s"], ["m", "min"], ["h", "h"]].map(([u, l]) => html`<option value=${u} ?selected=${u === this.unit}>${l}</option>`)}</select></span>`
        : html`<button type="button" aria-pressed="false" @click=${() => this.openCustom()}>${t("Custom")}</button>`}
    </div>`;
  }
}
customElements.define("ph-duration", DurationPicker);

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

const chain = (tone, linked) => html`<span class="chain ${tone}">${linked ? icons.link : icons.unlink}</span>`;

const linkState = (s) => {
  if (s.released_until) return ["warn", t("Released to the phone app")];
  if (s.connected) return ["ok", t("Connected")];
  const ack = known(s.last_ack);
  if (ack && Date.now() - ack < 120000) return ["warn", t("Reconnecting")];
  return ["bad", t("Not connected")];
};

// Every change saves at once; there is nothing to save or undo by hand.
class SettingsCard extends LitElement {
  static properties = { base: { attribute: false }, levels: { state: true }, query: { state: true }, places: { state: true }, busy: { state: true } };
  static styles = [base, css`
    .grid { display: grid; gap: 22px; }
    h3 { font-size: 15px; margin: 0 0 4px; font-weight: 600; }
    .hint { margin: 0 0 10px; }
    .here { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; font-size: 15px; margin-bottom: 10px; }
    .here b { font-weight: 600; }
    .find { display: flex; gap: 8px; }
    .find input { flex: 1; min-width: 0; }
    .places { list-style: none; margin: 6px 0 0; padding: 0; border: 1px solid var(--line); border-radius: 12px; overflow: hidden; }
    .places button { width: 100%; text-align: left; border: 0; border-top: 1px solid var(--line); background: var(--surface); color: var(--ink); padding: 10px 12px; font: inherit; font-size: 14px; cursor: pointer; }
    .places li:first-child button { border-top: 0; }
    .places button:hover { background: var(--chip); }
    .places small { color: var(--muted); margin-left: 6px; }
    .level { display: grid; grid-template-columns: 84px 1fr 36px; align-items: center; gap: 10px; }
    .level span { font-size: 14px; color: var(--muted); }
    .level output { font-family: var(--font-data); font-size: 13px; text-align: right; }
  `];

  async put(body, done) {
    this.busy = true;
    try { saved(this, await api("PUT", "settings", body)); send(this, "refresh"); if (done) toast(this, done); } catch (e) { toast(this, e.message); }
    this.busy = false;
  }

  async search(e) {
    e.preventDefault();
    const q = (this.query ?? "").trim();
    if (!q) return;
    try {
      const r = await fetch(`https://geocoding-api.open-meteo.com/v1/search?count=6&language=${lang}&name=${encodeURIComponent(q)}`).then((x) => x.json());
      this.places = r.results ?? [];
      if (!this.places.length) toast(this, t("No place called {q}", { q }));
    } catch { toast(this, t("Could not look the place up")); }
  }
  async pickPlace(p) {
    this.places = null;
    this.query = "";
    await this.put({ location: { latitude: Math.round(p.latitude * 1e4) / 1e4, longitude: Math.round(p.longitude * 1e4) / 1e4, name: p.name } }, t("Location set to {name}", { name: p.name }));
  }
  locate() {
    if (!navigator.geolocation || !window.isSecureContext) { toast(this, t("The browser only gives the location to pages served over HTTPS.")); return; }
    navigator.geolocation.getCurrentPosition(
      (p) => this.put({ location: { latitude: Number(p.coords.latitude.toFixed(4)), longitude: Number(p.coords.longitude.toFixed(4)), name: t("This device's location") } }, t("Location saved")),
      (e) => toast(this, t("Could not get this device's location: {reason}", { reason: e.message })),
      { timeout: 15000 });
  }

  render() {
    const st = this.base;
    if (!st) return nothing;
    const loc = st.location, b = { ...st.brightness, ...this.levels };
    const placeName = loc.name || `${loc.latitude}, ${loc.longitude}`;
    const level = (key, label) => html`<label class="level"><span>${label}</span>
      <input type="range" min="0" max="100" .value=${String(b[key])} ?disabled=${this.busy}
        @input=${(e) => { this.levels = { ...this.levels, [key]: Number(e.target.value) }; }}
        @change=${() => { this.put({ brightness: { ...st.brightness, ...this.levels } }); this.levels = undefined; }}><output>${b[key]}</output></label>`;
    return html`<section class="card" aria-labelledby="se-h">
      <div class="head"><h2 id="se-h">${t("Settings")}</h2></div>
      <div class="grid">
        <div>
          <h3>${t("Location")}</h3>
          <p class="hint">${t("Used for the weather and to know when it is day, dusk or night.")}</p>
          <div class="here">${icons.locate}<b>${placeName}</b></div>
          <form class="find" @submit=${(e) => this.search(e)}>
            <input type="search" placeholder=${t("Search a city")} aria-label=${t("Search a city")} .value=${this.query ?? ""} @input=${(e) => { this.query = e.target.value; }}>
            <button class="btn" type="submit">${t("Search")}</button>
            <button class="btn" type="button" title=${t("Use this device's location")} aria-label=${t("Use this device's location")} @click=${() => this.locate()}>${icons.locate}</button>
          </form>
          ${this.places?.length ? html`<ul class="places">${this.places.map((p) => html`<li><button @click=${() => this.pickPlace(p)}>${p.name}<small>${[p.admin1, p.country].filter(Boolean).join(", ")}</small></button></li>`)}</ul>` : nothing}
        </div>
        <div>
          <h3>${t("Brightness")}</h3>
          <p class="hint">${t("It changes by itself with the sun.")}</p>
          ${level("day", t("Day"))}${level("dusk", t("Dusk"))}${level("night", t("Night"))}
        </div>
        ${st.channels.length > 1 ? html`<div>
          <h3>${t("Default profile")}</h3>
          <select aria-label=${t("Default profile")} .value=${st.default_channel} ?disabled=${this.busy} @change=${(e) => this.put({ default_channel: e.target.value })}>
            ${st.channels.map((c) => html`<option value=${c.name} ?selected=${c.name === st.default_channel}>${c.name}</option>`)}</select>
        </div>` : nothing}
      </div>
    </section>`;
  }
}
customElements.define("ph-settings", SettingsCard);

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

// Pokable wraps something to click for fun: a bounce, a wobble after a few quick clicks, a full spin
// after SPIN_AFTER. With turn, every click turns it round like a cover on the display instead.
// It sends "poke" with how many came in a row, and spun when it spun.
const SPIN_AFTER = 10;
class Pokable extends LitElement {
  static properties = { anim: { state: true }, label: {}, turn: { type: Boolean } };
  static styles = css`
    :host { display: block; }
    button { display: block; width: 100%; border: 0; background: none; padding: 0; cursor: pointer; color: inherit; -webkit-tap-highlight-color: transparent; }
    ::slotted(*) { width: 100% !important; }
    .boing { animation: boing .35s ease-out; }
    .wobble { animation: wobble .5s ease-in-out; }
    .spin { animation: spin .9s cubic-bezier(.3, 1.4, .5, 1); }
    .turn { animation: turn .64s; }
    @keyframes boing { 30% { transform: scale(.9, 1.08); } 60% { transform: scale(1.05, .95); } }
    @keyframes wobble { 20%, 60% { transform: rotate(-5deg); } 40%, 80% { transform: rotate(5deg); } }
    @keyframes spin { to { transform: rotate(360deg); } }
    /* Two half turns, as the display draws them: faster towards the edge, the cover coming round unmirrored. */
    @keyframes turn {
      0%, 50% { transform: perspective(480px) rotateY(0); animation-timing-function: ease-in; }
      25%, 75% { transform: perspective(480px) rotateY(90deg); animation-timing-function: steps(1, end); }
      25.01%, 75.01% { transform: perspective(480px) rotateY(-90deg); animation-timing-function: ease-out; }
      100% { transform: perspective(480px) rotateY(0); }
    }
    @media (prefers-reduced-motion: reduce) { * { animation: none !important; } }
  `;
  poke() {
    const now = Date.now();
    this.pokes = [...(this.pokes ?? []).filter((at) => now - at < 2500), now];
    const count = this.pokes.length, spun = count >= SPIN_AFTER;
    if (spun) this.pokes = [];
    this.anim = "";
    requestAnimationFrame(() => { this.anim = spun ? "spin" : this.turn ? "turn" : count >= 4 ? "wobble" : "boing"; });
    this.dispatchEvent(new CustomEvent("poke", { detail: { count, spun } }));
  }
  render() {
    return html`<button class=${this.anim ?? ""} aria-label=${this.label ?? ""} @click=${() => this.poke()} @animationend=${() => { this.anim = ""; }}><slot></slot></button>`;
  }
}
customElements.define("ph-poke", Pokable);

// The display's own modes, as its side button cycles them. Each tap is sent at once, like the button.
const MODES = ["clock", "light", "animations"];
const modeName = (m) => ({ clock: t("Clock"), light: t("Light"), animations: t("My animations"), visualizer: t("Music"), scoreboard: t("Scoreboard"), stopwatch: t("Stopwatch"), countdown: t("Countdown"), noise: t("Noise meter") })[m];
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
  static properties = { current: { attribute: false }, clock: { attribute: false }, styles: { attribute: false }, draft: { state: true }, busy: { state: true }, now: { state: true } };
  connectedCallback() { super.connectedCallback(); this.now = new Date(); this.tick = setInterval(() => { this.now = new Date(); }, 15000); }
  disconnectedCallback() { super.disconnectedCallback(); clearInterval(this.tick); }
  static styles = [base, css`
    .modes { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 8px; }
    @media (min-width: 560px) { .modes { grid-template-columns: repeat(4, minmax(0, 1fr)); } }
    .mode { border: 1px solid var(--line); background: var(--surface); border-radius: 16px; padding: 12px 6px 10px; display: flex; flex-direction: column; align-items: center; gap: 6px; cursor: pointer; color: var(--muted); font-size: 13px; font-weight: 500; min-width: 0; }
    .mode svg { width: 24px; height: 24px; }
    .mode span { text-align: center; line-height: 1.25; max-width: 100%; }
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
    .face span { text-align: center; line-height: 1.25; max-width: 100%; }
    .face:hover { color: var(--ink); }
    .face[aria-pressed=true] { border-color: var(--accent); color: var(--ink); box-shadow: 0 0 0 1px var(--accent); }
    .toggles { display: flex; flex-wrap: wrap; gap: 16px; }
    .toggles label { display: inline-flex; align-items: center; gap: 8px; font-size: 14px; cursor: pointer; }
    input[type=checkbox] { width: 18px; height: 18px; accent-color: var(--accent); margin: 0; }
    .colour { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; }
    .swatches { display: flex; gap: 6px; flex-wrap: wrap; }
    .swatch { width: 28px; height: 28px; border-radius: 50%; border: 2px solid transparent; cursor: pointer; padding: 0; display: inline-block; }
    .swatch.other { position: relative; background: conic-gradient(#ff3b30, #ffd60a, #30d158, #40c8e0, #0a84ff, #bf5af2, #ff3b30); }
    .swatch.other input { position: absolute; inset: 0; opacity: 0; width: 100%; height: 100%; cursor: pointer; }
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

  // The clock has one look, the same in Automatic and when picked by hand.
  pick(mode) { this.apply(mode === "clock" ? { mode } : this.viewFor(mode)); }

  change(patch) { this.apply({ ...this.viewFor(this.current.mode), ...patch }); }

  // The clock pixelhub shows by itself, between pictures and in Automatic; a setting, not a mode.
  async setClock(patch) {
    const next = { mode: "clock", color: "#ff1e28", ...this.clock, ...patch };
    for (const k of Object.keys(next)) if (next[k] === undefined) delete next[k];
    this.busy = true;
    try { saved(this, await api("PUT", "settings", { clock: next })); send(this, "refresh"); } catch (e) { toast(this, e.message); }
    this.busy = false;
  }

  colour(v, change = (p) => this.change(p)) {
    const presets = ["#ffffff", "#ff3b30", "#ff9f0a", "#ffd60a", "#30d158", "#40c8e0", "#0a84ff", "#bf5af2"];
    return html`<div class="colour">
      <div class="swatches" role="group" aria-label=${t("Colour")}>
        ${presets.map((c) => html`<button class="swatch" style="background:${c}" aria-label=${c} aria-pressed=${String(v.color?.toLowerCase() === c)} @click=${() => change({ color: c })}></button>`)}
        <label class="swatch other" title=${t("Other colour")} aria-pressed=${String(!!v.color && !presets.includes(v.color.toLowerCase()))}
          style=${v.color && !presets.includes(v.color.toLowerCase()) ? `background:${v.color}` : ""}>
          <input type="color" aria-label=${t("Other colour")} .value=${v.color ?? "#ffffff"} @change=${(e) => change({ color: e.target.value })}></label>
      </div>
    </div>`;
  }

  options(mode, v = this.viewFor(mode), change = (p) => this.change(p)) {
    const choices = (names, key = "style") => html`<div class="choices" role="group">${names.map((n, i) =>
      html`<button class="choice ${typeof n === "number" ? "num" : ""}" aria-pressed=${String((v[key] ?? 0) === i)} @click=${() => change({ [key]: i })}>${n}</button>`)}</div>`;
    switch (mode) {
      case "clock": return html`
        <h3>${t("Clock style")}</h3>
        <div class="faces" role="group">${CLOCK_STYLES().map((name, i) => html`<button class="face" aria-pressed=${String((v.style ?? 0) === i)} @click=${() => change({ style: i || undefined })}>
          <img class="pix" src=${clockFace(i, this.now, v.color)} alt=""><span>${name}</span></button>`)}</div>
        <div class="toggles">
          ${[["weather", t("Weather")], ["temperature", t("Temperature")], ["calendar", t("Date")]].map(([k, l]) =>
            html`<label><input type="checkbox" .checked=${!!v[k]} @change=${(e) => change({ [k]: e.target.checked || undefined })}>${l}</label>`)}
        </div>
        <div><h3>${t("Colour")}</h3></div>${this.colour(v, change)}`;
      case "light": return html`
        <h3>${t("Effect")}</h3>${choices(LIGHT_TYPES())}
        <h3>${t("Colour")}</h3>${this.colour(v)}`;
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
          <div class="foot"><button class="btn small" @click=${() => this.change({ red: undefined, blue: undefined })}>${t("Reset")}</button></div>`;
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
      ${active && !MODES.includes(active) ? html`<div class="panel"><p>${t("{app} is on. Change it in Apps or Music.", { app: modeName(active) })}</p></div>`
        : active && active !== "clock" ? html`<div class="panel">${this.options(active)}</div>`
        : html`<div class="panel">${active ? html`<p class="small muted">${t("Only the clock, until you pick something else.")}</p>` : nothing}
          ${this.options("clock", { mode: "clock", color: "#ff1e28", ...this.clock }, (p) => this.setClock(p))}</div>`}`;
  }
}
customElements.define("ph-modes", Modes);

// Brightness right now: the level of the sun's current phase, the same one Settings edits.
const phaseNow = (status, st) => {
  const b = st?.brightness;
  if (!b || status?.brightness === undefined) return null;
  return ["day", "dusk", "night"].find((k) => b[k] === status.brightness) ?? null;
};

const defaultChannel = (st) => st?.channels?.find((c) => c.name === st.default_channel);
async function saveChannel(el, st, patch) {
  const ch = defaultChannel(st);
  try { saved(el, await api("PUT", `channels/${encodeURIComponent(ch.name)}`, { ...ch, ...patch })); send(el, "refresh"); return true; }
  catch (e) { toast(el, e.message); return false; }
}
const daysText = (days) => {
  const set = parseDays(days);
  if (set.size === 0 || set.size === 7) return t("Every day");
  if (formatDays(set) === "mon-fri") return t("Weekdays");
  if (formatDays(set) === "sat,sun") return t("Weekends");
  return [...set].sort().map(dayLabel).join(" ");
};
const STEPS = [["30s", "30 s"], ["1m", "1 min"], ["2m", "2 min"], ["5m", "5 min"]];

const RULE_KINDS = () => [["off", t("Display off")], ["clock", t("Only the clock")], ["light", t("Light")], ["animations", t("My animations")]];

// What automatic shows: one usual choice, and exceptions at set times. Read as a sentence.
class Automatic extends LitElement {
  static properties = { state: { attribute: false }, lists: { attribute: false }, draft: { state: true }, busy: { state: true } };
  static styles = [base, css`
    h3 { margin: 0 0 10px; font-size: 15px; font-weight: 600; }
    .block + .block { margin-top: 22px; padding-top: 18px; border-top: 1px solid var(--line); }
    .opts { display: flex; flex-wrap: wrap; gap: 6px; }
    .opt { border: 1px solid var(--line); background: none; border-radius: 10px; min-height: 38px; padding: 0 14px; font-size: 14px; color: var(--muted); cursor: pointer; display: inline-flex; align-items: center; gap: 8px; }
    .opt small { font-size: 12px; opacity: .8; }
    .opt:hover { color: var(--ink); }
    .opt[aria-pressed=true] { color: var(--ink); border-color: var(--accent); background: color-mix(in srgb, var(--accent) 14%, transparent); }
    .rows { display: grid; gap: 12px; margin-top: 14px; }
    .row { display: grid; grid-template-columns: 120px minmax(0, 1fr); align-items: center; gap: 10px; }
    .row > span { font-size: 14px; color: var(--muted); }
    .hint { margin: 10px 0 0; }
    ul { list-style: none; margin: 0 0 12px; padding: 0; display: grid; gap: 8px; }
    li { display: grid; grid-template-columns: minmax(0, 1fr) auto; align-items: center; gap: 10px; padding: 12px 14px; border: 1px solid var(--line); border-radius: 12px; background: var(--case); font-size: 14px; }
    li span { color: var(--muted); }
    form { display: grid; gap: 12px; padding: 14px; border: 1px solid var(--accent); border-radius: 12px; }
    .line { display: flex; flex-wrap: wrap; gap: 10px; align-items: center; font-size: 14px; color: var(--muted); }
    input[type=time], select { height: 36px; }
  `];
  get channel() { return defaultChannel(this.state); }
  async save(patch, done) {
    this.busy = true;
    const ok = await saveChannel(this, this.state, patch);
    this.busy = false;
    if (ok && done) toast(this, done);
    return ok;
  }
  setAmbient(a) { return this.save({ ambient: { ...this.channel.ambient, ...a } }); }
  whatOf(s) {
    if (s.brightness === 0) return t("Display off");
    if (s.list) return s.list;
    if (s.view?.mode) return RULE_KINDS().find(([k]) => k === s.view.mode)?.[1] ?? modeName(s.view.mode);
    return s.sprite || t("Only the clock");
  }
  async add(e) {
    e.preventDefault();
    const d = this.draft;
    const scene = { name: `${d.what.replace(/^list:/, "")} ${d.from}`, days: d.days, from: d.from, to: d.to };
    if (d.what.startsWith("list:")) scene.list = d.what.slice(5);
    else if (d.what === "off") Object.assign(scene, { view: { mode: "clock" }, brightness: 0 });
    else scene.view = d.what === "light" ? { mode: "light", color: "#ffb547" } : { mode: d.what };
    if (await this.save({ scenes: [...this.channel.scenes, scene] }, t("Exception added"))) this.draft = null;
  }
  form() {
    const d = this.draft, lists = this.lists ?? [];
    return html`<form @submit=${(e) => this.add(e)}>
      <div class="line"><ph-days .value=${d.days} @change=${(e) => { this.draft = { ...d, days: e.detail }; }}></ph-days></div>
      <div class="line">${t("from")} <input type="time" aria-label=${t("from")} .value=${d.from} @change=${(e) => { this.draft = { ...d, from: e.target.value }; }}>
        ${t("to")} <input type="time" aria-label=${t("to")} .value=${d.to} @change=${(e) => { this.draft = { ...d, to: e.target.value }; }}>
        ${t("show")} <select aria-label=${t("What to show")} @change=${(e) => { this.draft = { ...d, what: e.target.value }; }}>
          ${lists.map((l) => html`<option value=${`list:${l.name}`} ?selected=${d.what === `list:${l.name}`}>${l.name}</option>`)}
          ${RULE_KINDS().map(([k, label]) => html`<option value=${k} ?selected=${d.what === k}>${label}</option>`)}</select></div>
      <div class="line"><button class="btn primary" type="submit" ?disabled=${this.busy || d.from === d.to}>${t("Save")}</button>
        <button class="btn" type="button" @click=${() => { this.draft = null; }}>${t("Cancel")}</button></div>
    </form>`;
  }
  render() {
    const ch = this.channel;
    if (!ch) return nothing;
    const a = ch.ambient, lists = this.lists ?? [];
    const playing = a.list && seconds(a.pictures) > 0 ? a.list.toLowerCase() : "";
    const list = lists.find((l) => l.name.toLowerCase() === playing);
    return html`<section class="card" aria-labelledby="au-h">
      <div class="head"><h2 id="au-h">${t("In automatic")}</h2></div>
      <div class="block">
        <h3>${t("Usually it shows")}</h3>
        <div class="opts" role="group">
          <button class="opt" aria-pressed=${String(!playing)} ?disabled=${this.busy} @click=${() => this.setAmbient({ pictures: "0s", list: "", clock: seconds(a.clock) ? a.clock : "1m" })}>${t("Only the clock")}</button>
          ${lists.map((l) => html`<button class="opt" aria-pressed=${String(l.name.toLowerCase() === playing)} ?disabled=${this.busy}
            @click=${() => this.setAmbient({ list: l.name, pictures: seconds(a.pictures) ? a.pictures : "1m" })}>${l.name}<small>${l.items.length}</small></button>`)}
        </div>
        ${!lists.length ? html`<p class="small muted hint">${t("Your lists from the Gallery show up here.")}</p>` : nothing}
        ${playing ? html`
          ${list && !list.items.length ? html`<p class="small muted hint">${t("The list {name} is empty, so only the clock shows.", { name: list.name })}</p>` : nothing}
          <div class="rows">
            <div class="row"><span>${t("Each drawing")}</span><ph-duration .options=${STEPS} .value=${a.pictures} max="86400" @change=${(e) => this.setAmbient({ pictures: e.detail })}></ph-duration></div>
            <div class="row"><span>${t("Clock between")}</span><ph-duration .options=${[["0s", t("No")], ...STEPS]} .value=${a.clock} max="86400" @change=${(e) => this.setAmbient({ clock: e.detail })}></ph-duration></div>
          </div>` : nothing}
      </div>
      <div class="block">
        <h3>${t("Except")}</h3>
        ${ch.scenes.length ? html`<ul>${ch.scenes.map((s, i) => html`<li>
            <div><span>${daysText(s.days)}, ${s.from}–${s.to}:</span> ${this.whatOf(s)}</div>
            <button class="icon trash" aria-label=${t("Remove")} ?disabled=${this.busy} @click=${() => this.save({ scenes: ch.scenes.filter((_, k) => k !== i) })}>${icons.trash}</button>
          </li>`)}</ul>` : html`<p class="small muted" style="margin:0 0 12px">${t("No exceptions: it shows the same all day.")}</p>`}
        ${this.draft ? this.form() : html`<button class="btn" @click=${() => { this.draft = { what: lists[0] ? `list:${lists[0].name}` : "clock", days: "mon-fri", from: "14:00", to: "15:00" }; }}>${icons.plus}${t("Add an exception")}</button>`}
      </div>
    </section>`;
  }
}
customElements.define("ph-automatic", Automatic);


class DisplayTab extends LitElement {
  static properties = { status: { attribute: false }, state: { attribute: false }, sprites: { attribute: false }, styles: { attribute: false }, offline: { type: Boolean }, level: { state: true }, busy: { state: true }, now: { state: true }, lists: { state: true } };
  connectedCallback() {
    super.connectedCallback(); this.now = new Date(); this.tick = setInterval(() => { this.now = new Date(); }, 15000);
    api("GET", "lists").then((r) => { this.lists = r.lists; }).catch(() => {});
  }
  disconnectedCallback() { super.disconnectedCallback(); clearInterval(this.tick); }

  // The display draws its own modes; the preview redraws them as pixelhub sent them.
  preview(s) {
    const clock = { color: "#ff1e28", ...this.state?.clock };
    const mode = s.layer === "mode" ? this.state?.mode : null;
    if (mode?.mode === "clock") return clockFace(mode.style ?? 0, this.now, mode.color ?? clock.color);
    if (mode?.mode === "light") return lightFace(mode.color);
    if (s.showing === "clock") return clockFace(clock.style ?? 0, this.now, clock.color);
    return `api/preview.png?v=${s.preview_version}`;
  }
  static styles = [base, css`
    .layout { display: grid; grid-template-columns: minmax(0, 1fr); gap: 22px; }
    @media (min-width: 900px) { .layout { grid-template-columns: minmax(300px, 400px) minmax(0, 1fr); align-items: start; gap: 32px; } .hero { position: sticky; top: 88px; } }
    .hero { display: grid; gap: 14px; justify-items: center; }
    ph-led { width: min(100%, 360px); }
    @media (max-width: 599px) { ph-led { width: min(64%, 240px); } }
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
    if (s.layer === "alert") return t("For a moment");
    if (s.layer === "mode") return t("Chosen by you");
    if (s.layer === "music") return t("Playing on Spotify");
    if (s.layer === "scene") return t("Scheduled");
    return t("Automatic");
  }
  shownName(s) {
    if (s.layer === "mode" && this.state?.mode) return modeName(this.state.mode.mode);
    const own = { clock: t("Clock"), weather: t("Weather"), temperature: t("Temperature") }[s.showing];
    return own ?? s.showing.replace(/^gallery /, "");
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
          <output>${level}</output></label>
        <span class="small muted">${t("Brightness for the {phase}, as in Settings", { phase: t(phase) })}</span>` : nothing}
      </div>
      <div class="stack">
        <section><h2 class="muted">${t("What the display shows")}</h2>
          <ph-modes .current=${this.state?.mode} .clock=${this.state?.clock} .styles=${this.styles}></ph-modes></section>
        <ph-automatic .state=${this.state} .lists=${this.lists}></ph-automatic>
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

// The list that is always there; the page shows it under its name in the reader's language.
const FAVORITES = "favorites";
const listName = (l) => (l.name === FAVORITES ? t("Favorites") : l.name);

class GalleryTab extends LitElement {
  static properties = { state: { attribute: false }, width: { state: true }, category: { state: true }, sort: { state: true }, items: { state: true }, page: { state: true },
    more: { state: true }, pages: { state: true }, loading: { state: true }, picked: { state: true }, busy: { state: true }, problem: { state: true },
    query: { state: true }, searched: { state: true }, indexing: { state: true }, source: { state: true }, lists: { state: true }, open: { state: true },
    saving: { state: true }, newList: { state: true }, creating: { state: true }, renaming: { state: true } };
  static styles = [base, css`
    .bar { display: grid; gap: 10px; margin-bottom: 14px; }
    .cats { display: flex; gap: 6px; flex-wrap: wrap; }
    @media (max-width: 599px) {
      .cats { flex-wrap: nowrap; overflow-x: auto; scrollbar-width: none; margin: 0 -16px; padding: 0 16px; scroll-padding: 16px; }
      .cats::-webkit-scrollbar { display: none; }
    }
    .find { display: flex; gap: 8px; }
    .find input { flex: 1; min-width: 0; height: 40px; border-radius: 12px; padding: 0 14px; }
    .cat { flex: none; border: 1px solid var(--line); background: none; border-radius: 999px; height: 34px; padding: 0 14px; font-size: 13px; color: var(--muted); cursor: pointer; }
    .cat[aria-pressed=true] { color: var(--on-accent); background: var(--accent); border-color: var(--accent); font-weight: 600; }
    .cat.mine { color: var(--ink); border-color: color-mix(in srgb, var(--accent) 50%, var(--line)); }
    .grid { display: grid; grid-template-columns: repeat(var(--cols, 4), minmax(0, 1fr)); gap: 8px; }
    .item { position: relative; aspect-ratio: 1; padding: 0; border: 1px solid var(--line); border-radius: 12px; background: #000; overflow: hidden; cursor: pointer; }
    .item img { width: 100%; height: 100%; display: block; }
    .item[aria-pressed=true] { border-color: var(--accent); box-shadow: 0 0 0 2px var(--accent); }
    .kept { position: absolute; right: 4px; top: 4px; width: 16px; height: 16px; color: var(--accent); filter: drop-shadow(0 0 3px #000); }
    .kept svg { width: 16px; height: 16px; }
    .more { display: flex; justify-content: center; align-items: center; gap: 6px; margin-top: 18px; flex-wrap: wrap; }
    .pg { min-width: 40px; height: 40px; border-radius: 12px; border: 1px solid var(--line); background: none; color: var(--muted); font: inherit; font-variant-numeric: tabular-nums; cursor: pointer; }
    .pg[aria-current=page] { background: var(--accent); border-color: var(--accent); color: var(--on-accent); font-weight: 600; }
    .pg:disabled { opacity: .35; cursor: default; }
    .gap { color: var(--muted); padding: 0 2px; }
    .about { margin: 6px 0 0; font-size: 13px; color: var(--muted); white-space: pre-line; max-height: 4.5em; overflow: auto; }
    .sheet { position: sticky; top: 64px; z-index: 3; margin-bottom: 14px; background: var(--surface); border: 1px solid var(--accent); border-radius: 18px; padding: 14px;
      display: grid; grid-template-columns: 96px minmax(0, 1fr); gap: 14px; align-items: start; box-shadow: 0 14px 40px -12px rgba(0, 0, 0, .6); }
    .sheet img { width: 96px; height: 96px; border-radius: 10px; background: #000; }
    .sheet h3 { margin: 0; font-size: 16px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .sheet .acts { display: flex; flex-wrap: wrap; gap: 8px; margin-top: 10px; align-items: center; }
    .sheet select { max-width: 150px; }
    .meta { display: flex; gap: 12px; flex-wrap: wrap; }
    .save[aria-expanded=true] { border-color: var(--accent); color: var(--accent); }
    .fav { width: 36px; height: 36px; padding: 0; justify-content: center; }
    .fav[aria-pressed=true] { color: #ff5d7a; border-color: color-mix(in srgb, #ff5d7a 50%, var(--line)); }
    .sheet .btn.fav svg { width: 20px; height: 20px; flex: none; }
    .btn.save svg { width: 16px; height: 16px; }
    .kept.fav-mark { color: #ff5d7a; }
    .menu-wrap { position: relative; }
    .menu { position: absolute; top: calc(100% + 6px); left: 0; z-index: 5; width: 240px; max-height: 300px; overflow-y: auto; padding: 6px; display: grid; gap: 2px;
      background: var(--surface); border: 1px solid var(--line); border-radius: 14px; box-shadow: 0 16px 40px -10px rgba(0, 0, 0, .7); }
    .menu-item { display: flex; align-items: center; gap: 10px; width: 100%; min-height: 36px; padding: 0 10px; border: 0; border-radius: 10px; background: none;
      color: var(--ink); font: inherit; font-size: 14px; text-align: left; cursor: pointer; }
    .menu-item:hover, .menu-item:focus-visible { background: var(--chip); outline: none; }
    .menu-item .name { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .menu-item .n { color: var(--muted); font-size: 12px; font-variant-numeric: tabular-nums; }
    .menu-item svg { width: 16px; height: 16px; flex: none; }
    .menu-item .tick { color: var(--accent); }
    .menu-item.new { color: var(--muted); }
    .menu hr { border: 0; border-top: 1px solid var(--line); margin: 4px 2px; }
    .menu form { display: flex; padding: 2px; }
    .menu input { flex: 1; min-width: 0; height: 34px; border-radius: 10px; padding: 0 10px; font-size: 14px; }
    .card-list .fav-name { display: inline-flex; align-items: center; gap: 6px; }
    .card-list .fav-name svg { width: 14px; height: 14px; color: #ff5d7a; }
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
  constructor() { super(); this.category = "recommended"; this.sort = "likes"; this.items = []; this.page = 1; this.lists = []; this.buffer = []; }
  connectedCallback() {
    super.connectedCallback(); this.loadLists(); if (!this.items.length) this.load(true);
    this.closeMenu = () => { if (this.saving) this.saving = false; };
    document.addEventListener("click", this.closeMenu);
  }
  disconnectedCallback() { super.disconnectedCallback(); document.removeEventListener("click", this.closeMenu); }

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
  onEvo(l) {
    const a = defaultChannel(this.state)?.ambient;
    return !!a && seconds(a.pictures) > 0 && a.list?.toLowerCase() === l.name.toLowerCase();
  }
  async play(l) {
    const a = defaultChannel(this.state).ambient;
    this.busy = true;
    if (await saveChannel(this, this.state, { ambient: { ...a, list: l.name, pictures: seconds(a.pictures) ? a.pictures : "1m" } })) toast(this, t("{name} plays on the Evo in automatic", { name: l.name }));
    this.busy = false;
  }
  async upload(l, input) {
    const file = input.files?.[0];
    input.value = "";
    if (!file) return;
    const form = new FormData();
    form.append("file", file);
    this.busy = true;
    try {
      const res = await fetch(`api/lists/${encodeURIComponent(l.name)}/upload`, { method: "POST", body: form });
      const data = await res.json();
      if (!res.ok) throw new Error(data.error);
      this.lists = data.lists;
      toast(this, t("Added to {name}", { name: l.name }));
    } catch (e) { toast(this, e.message); }
    this.busy = false;
  }
  async renameList() {
    const name = (this.renaming ?? "").trim();
    if (name && (await this.listCall("PUT", `lists/${encodeURIComponent(this.open)}`, { name }))) { this.open = this.lists.find((l) => l.name.toLowerCase() === name.toLowerCase())?.name; this.renaming = null; }
  }
  async deleteList() {
    if (!confirm(t("Delete the list {name}? Its drawings stay in the gallery.", { name: this.open }))) return;
    if (await this.listCall("DELETE", `lists/${encodeURIComponent(this.open)}`)) { this.open = null; this.picked = null; }
  }

  // Upstream pages come in uneven sizes, so pictures pile up in one buffer and pages are cut from it
  // in whole rows of the grid; the page after the one shown is always fetched ahead.
  get cols() { return Math.max(1, Math.floor(((this.width ?? 900) + 8) / (86 + 8))); }
  get perPage() { const c = this.cols; return c * Math.max(2, Math.round(60 / c)); }

  async fill(want) {
    const run = this.run;
    while (this.buffer.length < want && !this.exhausted) {
      if (!this.fetching) {
        const next = this.fetched + 1;
        this.fetching = (this.searched
          ? api("GET", `gallery/search?q=${encodeURIComponent(this.searched)}&page=${next}`)
          : api("GET", `gallery?category=${this.category}&sort=${this.sort}&page=${next}`)
        ).then((r) => {
          if (run !== this.run) return;
          const seen = new Set(this.buffer.map((i) => i.file));
          this.buffer.push(...r.items.filter((i) => !seen.has(i.file)));
          this.fetched = next;
          this.exhausted = !r.more || !r.items.length;
          this.indexing = r.of && r.indexed < r.of ? r : null;
          this.source = r.source;
        }).finally(() => { if (run === this.run) this.fetching = null; });
      }
      await this.fetching;
      if (run !== this.run) return;
    }
  }

  async load(fresh, page = 1) {
    if (this.category === "lists") return;
    if (fresh) Object.assign(this, { run: (this.run ?? 0) + 1, buffer: [], fetched: 0, exhausted: false, fetching: null });
    else { this.picked = null; this.scrollIntoView({ block: "start" }); }
    const run = this.run;
    this.loading = true;
    this.problem = "";
    try {
      await this.fill(page * this.perPage);
      if (run !== this.run) return;
      this.page = page;
      this.cut();
      this.fill((page + 1) * this.perPage).then(() => {
        this.cut();
        this.buffer.slice(page * this.perPage, (page + 1) * this.perPage).forEach((i) => { new Image().src = gifURL(i.file); });
      }).catch(() => {});
    } catch (e) { this.problem = e.message; }
    if (run === this.run) this.loading = false;
  }

  cut() {
    const n = this.perPage, from = (this.page - 1) * n;
    if (from && from >= this.buffer.length) this.page = Math.max(1, Math.ceil(this.buffer.length / n));
    const start = (this.page - 1) * n;
    const items = this.buffer.slice(start, start + n);
    // A short last page only when the gallery has nothing more to give.
    const whole = this.exhausted ? items : items.slice(0, Math.max(this.cols, items.length - (items.length % this.cols)));
    if (whole.length !== this.items.length || whole.some((i, k) => i.file !== this.items[k]?.file)) this.items = whole;
    this.more = !this.exhausted || this.buffer.length > start + n;
    this.pages = this.exhausted ? Math.max(1, Math.ceil(this.buffer.length / n)) : undefined;
  }

  firstUpdated() {
    new ResizeObserver(([e]) => {
      const before = this.perPage, first = (this.page - 1) * before;
      this.width = e.contentRect.width;
      if (this.perPage !== before && this.buffer?.length) { this.page = Math.floor(first / this.perPage) + 1; this.load(false, this.page); }
    }).observe(this);
  }

  // The gallery's own page count is unknown until it runs out, so the pager shows one page ahead.
  pager() {
    const p = this.page, last = this.pages ?? (this.more ? p + 1 : p);
    if (last <= 1) return nothing;
    const shown = [...new Set([1, p - 2, p - 1, p, p + 1, p + 2, ...(this.pages ? [last] : [])])].filter((n) => n >= 1 && n <= last).sort((a, b) => a - b);
    const btn = (n, label, aria) => html`<button class="pg" aria-label=${aria ?? t("Page {n}", { n })} aria-current=${n === p ? "page" : "false"}
      ?disabled=${this.loading || n < 1 || n > last} @click=${() => n !== p && this.load(false, n)}>${label ?? n}</button>`;
    return html`${btn(p - 1, "‹", t("Previous page"))}
      ${shown.map((n, i) => html`${i && n - shown[i - 1] > 1 ? html`<span class="gap">…</span>` : nothing}${btn(n)}`)}
      ${!this.pages && this.more ? html`<span class="gap">…</span>` : nothing}
      ${btn(p + 1, "›", t("Next page"))}`;
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

  sheet(p) {
    const inLists = this.listsWith(p.file);
    const fav = inLists.includes(FAVORITES), others = this.lists.filter((l) => l.name !== FAVORITES);
    const inOthers = inLists.filter((n) => n !== FAVORITES).length;
    return html`<div class="sheet" role="dialog" aria-label=${p.name}>
      <img class="pix" src=${gifURL(p.file)} alt="">
      <div><h3>${p.name}</h3><span class="small muted meta"><span>${p.author}</span><span>${p.likes} ♥</span>
          ${p.category && CATEGORIES.includes(p.category) ? html`<button class="cat" style="height:24px;padding:0 10px" @click=${() => this.pick("category", p.category)}>${categoryName(p.category)}</button>` : nothing}</span>
        ${p.about ? html`<p class="about">${p.about}</p>` : nothing}
        <div class="acts">
          <button class="btn small fav" aria-pressed=${String(fav)} ?disabled=${this.busy} title=${fav ? t("Remove from favorites") : t("Add to favorites")}
            aria-label=${fav ? t("Remove from favorites") : t("Add to favorites")} @click=${() => this.keep(FAVORITES, p, !fav)}>${fav ? icons.heartFull : icons.heart}</button>
          <div class="menu-wrap">
            <button class="btn small save" aria-haspopup="menu" aria-expanded=${String(!!this.saving)} @click=${(e) => { e.stopPropagation(); this.saving = !this.saving; this.creating = false; }}>
              ${icons.bookmark}${inOthers ? t("In {n} lists", { n: inOthers }) : t("Add to list")}</button>
            ${this.saving ? this.listMenu(p, others, inLists) : nothing}
          </div>
          <button class="btn primary small" ?disabled=${this.busy} @click=${() => this.show()}>${t("Show for 30 s")}</button>
          <button class="icon" aria-label=${t("Close")} @click=${() => { this.picked = null; this.saving = false; }}>${icons.close}</button>
        </div></div>
    </div>`;
  }

  // A small menu under Add to list, as apps have: a tap puts the drawing in a list or takes it out.
  listMenu(p, others, inLists) {
    return html`<div class="menu" role="menu" @click=${(e) => e.stopPropagation()} @keydown=${(e) => { if (e.key === "Escape") { this.saving = false; } }}>
      ${others.map((l) => {
        const on = inLists.includes(l.name);
        return html`<button class="menu-item" role="menuitemcheckbox" aria-checked=${String(on)} ?disabled=${this.busy} @click=${() => this.keep(l.name, p, !on)}>
          <span class="name">${l.name}</span><span class="n">${l.items.length}</span>${on ? html`<span class="tick">${icons.check}</span>` : html`<span style="width:16px"></span>`}</button>`;
      })}
      ${others.length ? html`<hr>` : nothing}
      ${this.creating
        ? html`<form @submit=${(e) => { e.preventDefault(); this.makeList(p); this.creating = false; }}>
            <input placeholder=${t("List name")} aria-label=${t("New list")} maxlength="40" .value=${this.newList ?? ""} @input=${(e) => { this.newList = e.target.value; }}
              @keydown=${(e) => { if (e.key === "Escape") { e.stopPropagation(); this.creating = false; } }}></form>`
        : html`<button class="menu-item new" @click=${async () => { this.creating = true; await this.updateComplete; this.renderRoot.querySelector(".menu input")?.focus(); }}>
            ${icons.plus}<span class="name">${t("New list")}</span></button>`}
    </div>`;
  }

  grid(items) {
    const p = this.picked;
    const favs = new Set(this.lists.find((l) => l.name === FAVORITES)?.items.map((i) => i.file) ?? []);
    const kept = new Set(this.lists.filter((l) => l.name !== FAVORITES).flatMap((l) => l.items.map((i) => i.file)));
    return html`<div class="grid" style="--cols:${this.cols}">${repeat(items, (i) => i.file, (i) => html`<button class="item" aria-pressed=${String(p?.file === i.file)} title=${i.name} aria-label=${i.name}
      @click=${() => { this.picked = i; this.saving = false; }}><img class="pix" src=${gifURL(i.file)} alt="" loading="lazy" @error=${(e) => { e.target.parentElement.hidden = true; }}>${favs.has(i.file) ? html`<span class="kept fav-mark" aria-label=${t("Favorite")}>${icons.heartFull}</span>`
        : kept.has(i.file) ? html`<span class="kept" aria-label=${t("In a list")}>${icons.bookmark}</span>` : nothing}</button>`)}</div>`;
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
            : html`<h2>${listName(l)}</h2><span class="small muted">${t("{n} drawings", { n: l.items.length })}</span>
                ${this.onEvo(l) ? html`<span class="tag on">${t("On the Evo")}</span>`
                  : html`<button class="btn small primary" ?disabled=${this.busy || !l.items.length} @click=${() => this.play(l)}>${t("Show on the Evo")}</button>`}
                <label class="btn small">${icons.plus ?? ""}${t("Upload a drawing")}<input type="file" accept="image/gif,image/png" hidden @change=${(e) => this.upload(l, e.target)}></label>
                ${l.name === FAVORITES ? nothing : html`<button class="btn small" @click=${() => { this.renaming = l.name; }}>${t("Rename")}</button>
                <button class="icon trash" aria-label=${t("Delete list")} @click=${() => this.deleteList()}>${icons.trash}</button>`}`}
        </div>
        ${this.picked ? this.sheet(this.picked) : nothing}
        ${l.items.length ? this.grid(l.items) : html`<p class="empty muted">${l.name === FAVORITES ? t("No favorites yet. Open a drawing in the gallery and tap the heart.") : t("This list is empty. Open a drawing in the gallery and tap Add to list.")}</p>`}`;
    }
    return html`
      <form class="make" style="margin-bottom:14px" @submit=${(e) => { e.preventDefault(); this.makeList(); }}>
        <input placeholder=${t("New list")} aria-label=${t("New list")} maxlength="40" .value=${this.newList ?? ""} @input=${(e) => { this.newList = e.target.value; }}>
        <button class="btn" type="submit" ?disabled=${this.busy || !(this.newList ?? "").trim()}>${t("Create list")}</button>
      </form>
      ${this.lists.length ? html`<div class="cards">${this.lists.map((x) => html`<button class="card-list" @click=${() => { this.open = x.name; this.picked = null; }}>
          <div class="thumbs">${[0, 1, 2, 3].map((k) => x.items[k] ? html`<img class="pix" src=${gifURL(x.items[k].file)} alt="" loading="lazy">` : html`<span></span>`)}</div>
          <b class=${x.name === FAVORITES ? "fav-name" : ""}>${x.name === FAVORITES ? icons.heartFull : nothing}${listName(x)}</b><span class="small muted">${t("{n} drawings", { n: x.items.length })}</span></button>`)}</div>`
        : nothing}`;
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
          ? html`<p class="small muted">${this.source === "divoom" ? t("Results for “{q}” in the Divoom gallery.", { q: this.searched })
              : t("Divoom's search did not answer: results for “{q}” among the most liked and newest of every category, and what you have browsed.", { q: this.searched })}
              ${this.indexing ? html`<br>${t("Still gathering the gallery ({n} of {of} pages); search again in a minute for more.", { n: this.indexing.indexed, of: this.indexing.of })}` : nothing}</p>`
          : mine ? nothing
          : html`<div>${segmented([["likes", t("Most liked")], ["new", t("Newest")]], this.sort, (v) => this.pick("sort", v))}</div>`}
      </div>
      ${mine ? this.myLists() : html`
        ${this.picked ? this.sheet(this.picked) : nothing}
        ${this.problem ? html`<p class="err">${this.problem}</p>` : nothing}
        ${!this.items.length && !this.loading && !this.problem ? html`<p class="empty muted">${this.searched ? t("Nothing found. Try another word, in English it finds more.") : t("Nothing in this category right now.")}</p>` : nothing}
        ${this.grid(this.items)}
        <nav class="more" aria-label=${t("Pages")}>${this.pager()}${this.loading ? html`<span class="muted small">${t("Loading")}</span>` : nothing}</nav>`}`;
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
          <label class="toggle"><input type="checkbox" .checked=${!!m.enabled} @change=${(e) => this.music({ enabled: e.target.checked })}>${t("Show on the display while it plays")}</label>
          <div><span class="label">${t("What to show")}</span>${segmented([["cover", t("Album cover")], ["visualizer", t("Visualiser")], ["animations", t("My animations")]], show,
            (v) => this.music({ view: v === "cover" ? undefined : { mode: v } }))}</div>
          ${i.devices?.length ? html`<div><span class="label">${t("Only when it plays on")}</span>
            <div class="devices">${i.devices.map((d) => html`<label class="toggle"><input type="checkbox" .checked=${(m.devices ?? []).includes(d)}
              @change=${(e) => this.music({ devices: e.target.checked ? [...(m.devices ?? []), d] : (m.devices ?? []).filter((x) => x !== d) })}>${d}</label>`)}</div>
            <p class="small muted">${(m.devices ?? []).length ? t("Music on any other device is left alone.") : t("None ticked: music on any device shows, also away from home.")}</p></div>` : nothing}
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
    ph-led, ph-poke { width: min(100%, 300px); }
    .playing.idle { padding-block: 14px; gap: 4px; }
    .playing.idle p { margin: 0; }
    .playing.idle .head { margin-bottom: 0; }
    @media (max-width: 599px) { ph-led, ph-poke { width: min(60%, 220px); } }
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
        <section class="card playing ${now ? "" : "idle"}" aria-labelledby="np-h">
          <div class="head" style="width:100%"><h2 id="np-h">${t("Now playing")}</h2></div>
          ${now ? html`
            <ph-poke turn .label=${now.track}><ph-led .src=${`api/spotify/cover.png?t=${encodeURIComponent(now.track)}`} .label=${now.track}></ph-led></ph-poke>
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

const SKIES = () => ({ 1: t("Clear"), 3: t("Cloudy"), 5: t("Storm"), 6: t("Rain"), 8: t("Snow"), 9: t("Fog") });
const mmss = (s) => `${Math.floor(s / 60)}:${String(s % 60).padStart(2, "0")}`;
const APPS = ["weather", "countdown", "stopwatch", "noise", "scoreboard"];
const appIcons = {
  weather: icon(svg`<path d="M7 18a4 4 0 1 1 .9-7.9A5.5 5.5 0 0 1 18.5 12 3 3 0 0 1 18 18Z"/>`),
  countdown: icon(svg`<path d="M6 2h12M6 22h12M7 2c0 6 10 6 10 10S7 16 7 22M17 2c0 6-10 6-10 10s10 4 10 10"/>`),
  stopwatch: icon(svg`<circle cx="12" cy="13" r="8"/><path d="M12 9v4l2.5 2M10 2h4M12 2v3"/>`),
  noise: icon(svg`<path d="M3 15v4M7 11v8M11 7v12M15 11v8M19 4v15"/>`),
  scoreboard: icon(svg`<rect x="2.5" y="6" width="19" height="12" rx="2"/><path d="M12 6v12"/>`),
};
const appName = (a) => ({ weather: t("Weather"), countdown: t("Countdown"), stopwatch: t("Stopwatch"), noise: t("Noise meter"), scoreboard: t("Scoreboard") })[a];

// The display's built-in apps, and the weather its clock can show; one app's controls at a time.
class AppsTab extends LitElement {
  static properties = { state: { attribute: false }, weather: { state: true }, busy: { state: true }, picked: { state: true }, minutes: { state: true }, seconds: { state: true }, now: { state: true } };
  static styles = [base, css`
    .apps { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 8px; }
    @media (max-width: 559px) {
      .apps { display: flex; overflow-x: auto; scrollbar-width: none; margin: 0 -16px; padding: 2px 16px; }
      .apps::-webkit-scrollbar { display: none; }
      .app { flex: 0 0 96px; }
    }
    @media (min-width: 560px) { .apps { grid-template-columns: repeat(5, minmax(0, 1fr)); } }
    .app { border: 1px solid var(--line); background: var(--surface); border-radius: 16px; padding: 12px 6px 10px; display: grid; justify-items: center; gap: 4px; cursor: pointer; color: var(--muted); min-width: 0; }
    .app svg { width: 24px; height: 24px; margin-bottom: 2px; }
    .app b { font-size: 13px; font-weight: 500; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; max-width: 100%; }
    .app:hover { color: var(--ink); border-color: color-mix(in srgb, var(--ink) 30%, var(--line)); }
    .app[aria-pressed=true] { color: var(--ink); border-color: var(--accent); background: color-mix(in srgb, var(--accent) 12%, var(--surface)); }
    .app[aria-pressed=true] svg { color: var(--accent); }
    .panel { margin-top: 10px; background: var(--surface); border: 1px solid var(--line); border-radius: 16px; padding: 18px; display: grid; gap: 18px; }
    @media (min-width: 760px) { .panel { grid-template-columns: minmax(0, 300px) minmax(0, 1fr); align-items: center; gap: 28px; padding: 24px; } }
    .lcd { background: var(--case); border: 1px solid var(--line); border-radius: 16px; min-height: 150px; display: grid; place-items: center; align-content: center; gap: 6px; padding: 18px; text-align: center; }
    .lcd output { font-family: var(--font-data); font-weight: 500; letter-spacing: -.02em; font-size: 60px; line-height: 1; color: var(--accent); text-shadow: 0 0 18px color-mix(in srgb, var(--accent) 45%, transparent); font-variant-numeric: tabular-nums; }
    .lcd output.idle { color: var(--muted); text-shadow: none; }
    .lcd span { font-size: 13px; color: var(--muted); }
    .side { display: grid; gap: 16px; min-width: 0; }
    .side h3 { margin: 0; font-size: 17px; font-weight: 600; }
    .side p { margin: 0; color: var(--muted); font-size: 14px; line-height: 1.45; }
    .actions { display: flex; gap: 10px; flex-wrap: wrap; }
    .actions .btn { height: 42px; padding: 0 18px; font-size: 15px; }
    @media (max-width: 559px) { .actions .btn { flex: 1 1 0; justify-content: center; } }
    .label { font-size: 12px; color: var(--muted); margin-bottom: 6px; display: block; }
    .presets { display: flex; flex-wrap: wrap; gap: 6px; align-items: center; }
    .choice { border: 1px solid var(--line); background: none; border-radius: 10px; height: 36px; padding: 0 14px; font-size: 14px; color: var(--muted); cursor: pointer; font-variant-numeric: tabular-nums; }
    .choice:hover { color: var(--ink); }
    .choice[aria-pressed=true] { color: var(--ink); border-color: var(--accent); background: color-mix(in srgb, var(--accent) 14%, transparent); }
    .time { display: inline-flex; align-items: center; gap: 4px; font-size: 18px; color: var(--muted); margin-left: 6px; }
    .time input { width: 64px; height: 36px; text-align: center; font-variant-numeric: tabular-nums; }
    .note { display: flex; gap: 10px; align-items: center; justify-content: space-between; flex-wrap: wrap; font-size: 13px; color: var(--muted); border-top: 1px solid var(--line); padding-top: 14px; }
    .link { border: 0; background: none; color: var(--accent); font: inherit; cursor: pointer; padding: 0; }
    .score { display: grid; grid-template-columns: 1fr 1fr; gap: 10px; }
    .team { border-radius: 14px; padding: 14px 10px; display: grid; justify-items: center; gap: 10px; background: color-mix(in srgb, var(--team) 14%, var(--case)); border: 1px solid color-mix(in srgb, var(--team) 40%, transparent); }
    .team span { font-size: 13px; color: var(--muted); }
    .team output { font-family: var(--font-data); font-weight: 500; font-size: 46px; line-height: 1; color: var(--team); font-variant-numeric: tabular-nums; }
    .team .pm { display: flex; gap: 8px; }
    .team button { width: 46px; height: 40px; border-radius: 12px; border: 1px solid color-mix(in srgb, var(--team) 40%, transparent); background: var(--surface); color: var(--ink); font-size: 20px; cursor: pointer; }
  `];
  constructor() { super(); this.minutes = 5; this.seconds = 0; }
  connectedCallback() {
    super.connectedCallback(); this.load(); this.now = Date.now();
    this.timer = setInterval(() => { if (!document.hidden) this.load(); }, 60000);
    this.tick = setInterval(() => { if (this.mode.running) this.now = Date.now(); }, 1000);
  }
  disconnectedCallback() { super.disconnectedCallback(); clearInterval(this.timer); clearInterval(this.tick); }
  async load() { try { this.weather = await api("GET", "weather"); } catch { /* the status poll shows outages */ } }

  get mode() { return this.state?.mode ?? {}; }
  get showing() { return APPS.includes(this.mode.mode) ? this.mode.mode : null; }
  // Seconds since a running tool started, from the time pixelhub sent it.
  elapsed() { return this.mode.since ? Math.max(0, Math.floor(this.now / 1000) - this.mode.since) : 0; }
  counted() { const m = this.mode; return (m.seconds ?? 0) + (m.running ? this.elapsed() : 0); }
  left() { const m = this.mode; return m.running ? Math.max(0, (m.seconds ?? 0) - this.elapsed()) : (m.seconds ?? 0); }
  get total() { return Math.min(5999, Math.max(1, (Number(this.minutes) || 0) * 60 + (Number(this.seconds) || 0))); }

  async call(method, path, body, done) {
    this.busy = true;
    try { const r = await api(method, path, body); if (path !== "weather/show" && r) saved(this, r); send(this, "refresh"); if (done) toast(this, done); } catch (e) { toast(this, e.message); }
    this.busy = false;
  }
  run(view) { return this.call("PUT", "mode", view); }
  automatic() { return this.call("DELETE", "mode"); }

  back() {
    return this.showing === this.current ? html`<div class="note"><span>${t("Showing on the Evo now.")}</span>
      <button class="btn" ?disabled=${this.busy} @click=${() => this.automatic()}>${t("Back to automatic")}</button></div>` : nothing;
  }

  weatherPanel() {
    const w = this.weather, loc = this.state?.location;
    const greenwich = loc && Math.abs(loc.latitude - 51.4769) < 0.001 && Math.abs(loc.longitude + 0.0005) < 0.001;
    return html`
      <div class="lcd">${w?.known
        ? html`<output>${w.celsius}°</output><span>${SKIES()[w.sky] ?? ""}</span>`
        : html`<output class="idle">--°</output><span>${w?.error ? t("Could not read the weather") : t("Reading the weather…")}</span>`}</div>
      <div class="side">
        <div><h3>${t("Weather")}</h3><p>${t("Shows on the Evo for a few seconds, then it goes back to what it was showing.")}</p></div>
        <div class="actions">
          <button class="btn" ?disabled=${this.busy || !w?.known} @click=${() => this.call("POST", "weather/show?what=weather", null, t("Showing the weather on the Evo"))}>${appIcons.weather}${t("Show the weather")}</button>
          <button class="btn" ?disabled=${this.busy || !w?.known} @click=${() => this.call("POST", "weather/show?what=temperature", null, t("Showing the temperature on the Evo"))}>${icon(svg`<path d="M14 14.8V5a2 2 0 1 0-4 0v9.8a4 4 0 1 0 4 0Z"/><path d="M12 9v7"/>`)}${t("Show the temperature")}</button>
        </div>
        <div class="note">
          <span>${greenwich ? t("The location is still the default, Greenwich.") : w?.at ? t("Updated at {time}, every 15 minutes.", { time: new Date(w.at).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" }) }) : ""}</span>
          <button class="link" @click=${() => { location.hash = "#settings"; }}>${greenwich ? t("Set your location") : t("Change location")}</button>
        </div>
      </div>`;
  }

  countdownPanel() {
    const m = this.mode, on = this.showing === "countdown", total = this.total;
    return html`
      <div class="lcd"><output class=${on && m.running ? "" : "idle"}>${mmss(on ? this.left() : total)}</output>
        <span>${on ? (m.running ? (this.left() ? t("Counting down") : t("Done, back to automatic in a minute")) : t("Paused")) : t("Ready")}</span></div>
      <div class="side">
        <div>
          <span class="label">${t("Length")}</span>
          <div class="presets">${[1, 5, 10, 25].map((n) => html`<button class="choice" aria-pressed=${String(total === n * 60)} @click=${() => { this.minutes = n; this.seconds = 0; }}>${n} min</button>`)}
            <span class="time" title=${t("Other length")}><input type="number" min="0" max="99" aria-label=${t("Minutes")} .value=${String(this.minutes)} @input=${(e) => { this.minutes = e.target.value; }}>:<input type="number" min="0" max="59" aria-label=${t("Seconds")} .value=${String(this.seconds).padStart(2, "0")} @input=${(e) => { this.seconds = e.target.value; }}></span></div>
        </div>
        <div class="actions">
          ${on && m.running ? html`<button class="btn primary" ?disabled=${this.busy} @click=${() => this.run({ mode: "countdown", seconds: Math.max(1, this.left()) })}>${t("Pause")}</button>`
            : html`<button class="btn primary" ?disabled=${this.busy} @click=${() => this.run({ mode: "countdown", seconds: on && m.seconds ? m.seconds : total, running: true })}>${on && m.seconds ? t("Resume") : t("Start")}</button>`}
          <button class="btn" ?disabled=${this.busy || !on} @click=${() => this.run({ mode: "countdown", seconds: total })}>${t("Reset")}</button>
        </div>
        ${this.back()}
      </div>`;
  }

  stopwatchPanel() {
    const m = this.mode, shown = this.showing === "stopwatch", on = shown && m.running, paused = shown && !m.running && m.seconds > 0;
    return html`
      <div class="lcd"><output class=${on ? "" : "idle"}>${mmss(shown ? this.counted() : 0)}</output>
        <span>${on ? t("Running") : paused ? t("Paused") : shown ? t("At zero, back to automatic in a minute") : t("Ready")}</span></div>
      <div class="side">
        <div><h3>${t("Stopwatch")}</h3><p>${t("Counts up on the Evo from zero.")}</p></div>
        <div class="actions">
          ${on ? html`<button class="btn primary" ?disabled=${this.busy} @click=${() => this.run({ mode: "stopwatch", seconds: this.counted() })}>${t("Pause")}</button>`
            : html`<button class="btn primary" ?disabled=${this.busy} @click=${() => this.run({ mode: "stopwatch", seconds: paused ? m.seconds : undefined, running: true })}>${paused ? t("Resume") : t("Start")}</button>`}
          <button class="btn" ?disabled=${this.busy || !(on || paused)} @click=${() => this.run({ mode: "stopwatch" })}>${t("Reset")}</button>
        </div>
        ${this.back()}
      </div>`;
  }

  noisePanel() {
    const shown = this.showing === "noise", on = shown && this.mode.running;
    return html`
      <div class="lcd"><output class=${on ? "" : "idle"}>${on ? t("On") : t("Off")}</output><span>${shown && !on ? t("Back to automatic in a minute") : t("Microphone")}</span></div>
      <div class="side">
        <div><h3>${t("Noise meter")}</h3><p>${t("Shows how loud the room is, with the Evo's microphone.")}</p></div>
        <div class="actions">${on
          ? html`<button class="btn primary" ?disabled=${this.busy} @click=${() => this.run({ mode: "noise" })}>${t("Stop")}</button>`
          : html`<button class="btn primary" ?disabled=${this.busy} @click=${() => this.run({ mode: "noise", running: true })}>${t("Start")}</button>`}</div>
        ${this.back()}
      </div>`;
  }

  scoreboardPanel() {
    const on = this.showing === "scoreboard", v = on ? this.mode : {};
    const set = (patch) => this.run({ mode: "scoreboard", red: v.red, blue: v.blue, ...patch });
    const team = (key, label, colour) => html`<div class="team" style="--team:${colour}">
      <span>${label}</span><output>${v[key] ?? 0}</output>
      <div class="pm"><button aria-label=${t("{team} minus one", { team: label })} ?disabled=${this.busy} @click=${() => set({ [key]: Math.max(0, (v[key] ?? 0) - 1) || undefined })}>−</button>
        <button aria-label=${t("{team} plus one", { team: label })} ?disabled=${this.busy} @click=${() => set({ [key]: Math.min(999, (v[key] ?? 0) + 1) })}>+</button></div></div>`;
    return html`
      <div class="score">${team("red", t("Red"), "#ff453a")}${team("blue", t("Blue"), "#0a84ff")}</div>
      <div class="side">
        <div><h3>${t("Scoreboard")}</h3><p>${t("Each point shows on the Evo as you add it.")}</p></div>
        <div class="actions">${on
          ? html`<button class="btn" ?disabled=${this.busy} @click=${() => set({ red: undefined, blue: undefined })}>${t("Reset")}</button>`
          : html`<button class="btn primary" ?disabled=${this.busy} @click=${() => set({})}>${t("Show")}</button>`}</div>
        ${this.back()}
      </div>`;
  }

  get current() { return this.picked ?? this.showing ?? "weather"; }

  render() {
    const cur = this.current;
    return html`
      <div class="apps" role="group" aria-label=${t("Apps")}>${APPS.map((a) => html`
        <button class="app" aria-pressed=${String(cur === a)} @click=${() => { this.picked = a; }}>${appIcons[a]}<b>${appName(a)}</b></button>`)}</div>
      <section class="panel" aria-label=${appName(cur)}>${this[`${cur}Panel`]()}</section>`;
  }
}
customElements.define("ph-apps", AppsTab);

class ConnectionCard extends LitElement {
  static properties = { status: { attribute: false }, state: { attribute: false }, busy: { state: true } };
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
  async setFor(v) {
    this.busy = true;
    try { saved(this, await api("PUT", "settings", { release_for: v })); } catch (e) { toast(this, e.message); }
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
        <dt>${t("Display")}</dt><dd>${chain(tone, s.connected)}${linkText}${!s.connected && !released
          ? html`<button class="btn small" ?disabled=${this.busy} title=${t("Look for the display now instead of at the next try")} @click=${() => this.call("reconnect")}>${t("Retry now")}</button>` : nothing}</dd>
        <dt>${t("Last answer")}</dt><dd>${ago(s.last_ack)}</dd>
        <dt>${t("Battery")}</dt><dd>${s.battery === undefined ? html`<span class="muted small">${t("This bridge cannot read it")}</span>`
          : html`${Math.min(100, (s.battery + 1) * 10)} %<span class="muted small">${ago(s.battery_at)}</span>`}</dd>
        ${s.light !== undefined ? html`<dt>${t("Room light")}</dt><dd>${Math.round(s.light / 2.55)} %<span class="muted small">${ago(s.light_at)}</span></dd>` : nothing}
        <dt>${t("Bridge")}</dt><dd class="mono small">${s.address}</dd>
      </dl>
      <div class="actions">
        ${released
          ? html`<button class="btn primary" ?disabled=${this.busy} @click=${() => this.call("claim")}>${t("Take back")}</button><span class="small muted">${t("Released until {time}", { time: until(s.released_until) })}</span>`
          : html`<button class="btn" ?disabled=${this.busy} @click=${() => this.call("release")}>${t("Lend to the phone")}</button>
            <span class="small muted">${t("for")}</span>
            <ph-duration .options=${[["30m", "30 min"], ["1h", "1 h"], ["3h", "3 h"], ["8h", "8 h"]]} .value=${this.state?.release_for} max="604800" @change=${(e) => this.setFor(e.detail)}></ph-duration>
            <p class="small muted" style="flex-basis:100%;margin:0">${t("Lets the Divoom app use the display for a while; then pixelhub takes it back.")}</p>`}
      </div>
    </section>`;
  }
}
customElements.define("ph-connection", ConnectionCard);

const lead = (m) => (m % 1440 === 0 ? t("{n} d before", { n: m / 1440 }) : m % 60 === 0 ? t("{n} h before", { n: m / 60 }) : t("{n} min before", { n: m }));

const bell = icon(svg`<path d="M6 16V11a6 6 0 0 1 12 0v5l1.5 2h-15zM10 20.5a2 2 0 0 0 4 0"/>`);
const dayKey = (d) => d.toDateString();
const dayName = (d) => {
  const today = new Date(), tomorrow = new Date(Date.now() + 86400000);
  if (dayKey(d) === dayKey(today)) return t("Today");
  if (dayKey(d) === dayKey(tomorrow)) return t("Tomorrow");
  return d.toLocaleDateString(lang, { weekday: "long", day: "numeric", month: "short" });
};
const inTime = (at) => {
  const m = Math.round((new Date(at) - Date.now()) / 60000);
  if (m <= 0) return t("now");
  if (m < 60) return t("in {n} min", { n: m });
  if (m < 1440) return t("in {h} h {m} min", { h: Math.floor(m / 60), m: m % 60 });
  return t("in {n} d", { n: Math.round(m / 1440) });
};

// Links a Google account, picks the calendars that warn, and lists what comes up with its warnings.
class CalendarTab extends LitElement {
  static properties = { info: { state: true }, id: { state: true }, secret: { state: true }, busy: { state: true } };
  static styles = [base, css`
    .layout { display: grid; grid-template-columns: minmax(0, 1fr); gap: 18px; }
    @media (min-width: 900px) { .layout { grid-template-columns: minmax(280px, 360px) minmax(0, 1fr); align-items: start; } .side { position: sticky; top: 88px; } }
    .side, .main { display: grid; gap: 18px; min-width: 0; }
    .grid { display: grid; gap: 14px; }
    .row { display: flex; gap: 8px; flex-wrap: wrap; align-items: center; }
    .row input { flex: 1 1 260px; font-family: var(--font-data); }
    ol { margin: 0; padding-left: 20px; display: grid; gap: 6px; font-size: 14px; }
    code { font-family: var(--font-data); font-size: 12px; background: var(--chip); padding: 2px 6px; border-radius: 6px; overflow-wrap: anywhere; }
    a { color: var(--accent); }
    .next { display: grid; gap: 12px; justify-items: center; text-align: center; }
    ph-led { width: min(100%, 220px); }
    .next b { font-size: 17px; font-weight: 600; }
    .when { color: var(--muted); font-size: 14px; }
    .soon { font-size: 13px; padding: 6px 12px; border-radius: 999px; background: color-mix(in srgb, var(--accent) 16%, transparent); color: var(--accent); }
    .cals { display: grid; }
    .cal { display: flex; align-items: center; gap: 12px; padding: 10px 0; border-top: 1px solid var(--line); font-size: 14px; }
    .cal:first-child { border-top: 0; padding-top: 0; }
    .cal span.name { flex: 1; min-width: 0; overflow-wrap: anywhere; }
    .swatch { width: 12px; height: 12px; border-radius: 4px; flex: none; }
    .switch { width: 40px; height: 24px; border-radius: 999px; border: 1px solid var(--line); background: var(--chip); position: relative; cursor: pointer; flex: none; padding: 0; }
    .switch::after { content: ""; position: absolute; top: 3px; left: 3px; width: 16px; height: 16px; border-radius: 50%; background: var(--muted); transition: transform .15s; }
    .switch[aria-checked=true] { background: var(--accent); border-color: var(--accent); }
    .switch[aria-checked=true]::after { transform: translateX(16px); background: var(--on-accent); }
    .switch:disabled { opacity: .5; cursor: default; }
    .account { display: flex; align-items: center; gap: 8px; justify-content: space-between; flex-wrap: wrap; margin-top: 14px; padding-top: 12px; border-top: 1px solid var(--line); }
    .list { max-height: min(70vh, 640px); overflow-y: auto; margin-right: -10px; padding-right: 10px; scrollbar-width: thin; }
    .day { margin: 18px 0 6px; font-size: 13px; font-weight: 600; color: var(--muted); text-transform: capitalize; }
    .day:first-of-type { margin-top: 0; }
    .event { display: grid; grid-template-columns: 52px 4px minmax(0, 1fr); gap: 12px; align-items: start; padding: 10px 0; border-top: 1px solid var(--line); }
    .event .time { font-family: var(--font-data); font-size: 13px; color: var(--muted); padding-top: 2px; }
    .event .bar { align-self: stretch; border-radius: 2px; }
    .event b { font-weight: 600; font-size: 15px; overflow-wrap: anywhere; }
    .event .cal-name { color: var(--muted); font-size: 12px; margin-top: 2px; }
    .chips { display: flex; flex-wrap: wrap; gap: 4px 12px; margin-top: 4px; }
    .chip { display: inline-flex; align-items: center; gap: 4px; height: auto; padding: 0; background: none; border-radius: 0; font-family: inherit; font-size: 12px; color: var(--muted); opacity: .8; }
    .chip svg { width: 11px; height: 11px; }
    .empty { margin: 0; }
  `];
  connectedCallback() { super.connectedCallback(); this.load(); this.timer = setInterval(() => { if (!document.hidden) this.load(); }, 30000); }
  disconnectedCallback() { super.disconnectedCallback(); clearInterval(this.timer); }
  async load() { try { this.info = await api("GET", "google"); } catch { /* the status poll shows outages */ } }
  async call(method, path, body) {
    this.busy = true;
    try { this.info = await api(method, path, body); } catch (e) { toast(this, e.message); }
    this.busy = false;
  }
  async show() {
    try { await api("POST", "google/show"); send(this, "refresh"); } catch (e) { toast(this, e.message); }
  }
  pick(id, on) {
    const ids = this.info.calendars.filter((c) => (c.id === id ? on : c.selected)).map((c) => c.id);
    this.info = { ...this.info, calendars: this.info.calendars.map((c) => (c.id === id ? { ...c, selected: on } : c)) };
    this.call("PUT", "google/calendars", { ids });
  }
  callback() { return `${location.origin}/api/google/callback`; }

  setup(i) {
    const secure = location.protocol === "https:" || location.hostname === "localhost" || location.hostname === "127.0.0.1";
    const warn = secure ? nothing : html`<p class="err">${t("Google only takes https addresses: open pixelhub through its https address to connect.")}</p>`;
    return html`<section class="card" aria-labelledby="gs-h">
      <div class="head"><h2 id="gs-h">${t("Google Calendar")}</h2></div>
      <div class="grid">${!i.configured ? html`
        <ol>
          <li>${t("In")} <a href="https://console.cloud.google.com/apis/library/calendar-json.googleapis.com" target="_blank" rel="noopener">Google Cloud Console</a>${t(", create a project and enable the Google Calendar API.")}</li>
          <li>${t("In Google Auth Platform, set it up as External, add the scope calendar.readonly and, under Audience, press Publish app (in Testing, Google signs you out every 7 days).")}</li>
          <li>${t("Under Clients, create a Web application client with this redirect URI:")} <code>${this.callback()}</code></li>
          <li>${t("Paste its client ID and secret here.")}</li>
        </ol>
        ${warn}
        <div class="row"><input placeholder=${t("Client ID")} aria-label=${t("Client ID")} .value=${this.id ?? ""} @input=${(e) => { this.id = e.target.value; }}></div>
        <div class="row"><input type="password" autocomplete="off" placeholder=${t("Client secret")} aria-label=${t("Client secret")} .value=${this.secret ?? ""} @input=${(e) => { this.secret = e.target.value; }}>
          <button class="btn primary" ?disabled=${this.busy || !this.id || !this.secret} @click=${() => this.call("PUT", "google", { client_id: this.id, client_secret: this.secret })}>${t("Save")}</button></div>`
      : html`
        <p class="small muted" style="margin:0">${t("The Google client must list this redirect URI:")} <code>${this.callback()}</code></p>
        ${warn}
        <div class="row"><a class="btn primary" href="api/google/login">${t("Connect Google")}</a>
          <button class="btn small" ?disabled=${this.busy} @click=${() => { this.id = this.secret = ""; this.info = { ...i, configured: false }; }}>${t("Change client")}</button></div>`}
      </div>
    </section>`;
  }

  nextCard(i) {
    const e = i.next?.find((x) => new Date(x.start) > Date.now());
    return html`<section class="card next" aria-labelledby="nx-h">
      <div class="head" style="width:100%"><h2 id="nx-h">${t("Next event")}</h2></div>
      ${e ? html`
        <ph-led .src=${`api/google/preview.png?k=${encodeURIComponent(e.start + e.title)}&m=${Math.floor(Date.now() / 60000)}`} .label=${e.title}></ph-led>
        <div><b>${e.title || t("(no title)")}</b><div class="when">${dayName(new Date(e.start))} · ${hhmm(new Date(e.start))}</div></div>
        <span class="soon">${inTime(e.start)}</span>
        <button class="btn small" @click=${() => this.show()}>${t("Show on the display")}</button>`
      : html`<p class="muted empty">${t("Nothing in the next 7 days.")}</p>`}
    </section>`;
  }

  calendarsCard(i) {
    return html`<section class="card" aria-labelledby="cl-h">
      <div class="head"><h2 id="cl-h">${t("Calendars")}</h2></div>
      <div class="cals">${i.calendars.map((c) => html`<div class="cal">
        <span class="swatch" style=${`background:${c.color || "var(--muted)"}`}></span>
        <span class="name">${c.name}</span>
        <button class="switch" role="switch" aria-checked=${String(c.selected)} aria-label=${c.name} ?disabled=${this.busy} @click=${() => this.pick(c.id, !c.selected)}></button>
      </div>`)}</div>
      <div class="account"><span class="small muted">${i.account}</span>
        <button class="btn small" ?disabled=${this.busy} @click=${() => this.call("DELETE", "google")}>${t("Disconnect")}</button></div>
    </section>`;
  }

  eventsCard(i) {
    const days = [];
    for (const e of i.next ?? []) {
      const d = new Date(e.start);
      if (days.at(-1)?.key !== dayKey(d)) days.push({ key: dayKey(d), name: dayName(d), events: [] });
      days.at(-1).events.push(e);
    }
    return html`<section class="card" aria-labelledby="ev-h">
      <div class="head"><h2 id="ev-h">${t("Coming up")}</h2></div>
      ${days.length ? html`<div class="list">${days.map((d) => html`<h3 class="day">${d.name}</h3>${d.events.map((e) => html`<div class="event">
        <span class="time">${hhmm(new Date(e.start))}</span>
        <span class="bar" style=${`background:${e.color || "var(--muted)"}`}></span>
        <div><b>${e.title || t("(no title)")}</b><div class="cal-name">${e.calendar}</div>
          <div class="chips">${(e.alarms ?? []).slice().reverse().map((m) => html`<span class="chip">${bell}${lead(m)}</span>`)}
            <span class="chip">${bell}${t("when it starts")}</span></div></div>
      </div>`)}`)}</div>`
      : html`<p class="muted empty">${t("Nothing in the next 7 days.")}</p>`}
      <p class="hint">${t("Each event warns at its reminders in Google Calendar, and again when it starts, with a chime except from 23:00 to 08:00. All-day, cancelled and declined events are left out.")}</p>
      ${i.error ? html`<p class="err">${i.error}</p>` : nothing}
    </section>`;
  }

  render() {
    const i = this.info;
    if (!i) return nothing;
    if (!i.connected) return html`<div class="layout"><div class="main" style="grid-column:1/-1;max-width:720px">${this.setup(i)}</div></div>`;
    return html`<div class="layout">
      <div class="side">${this.nextCard(i)}${this.calendarsCard(i)}</div>
      <div class="main">${this.eventsCard(i)}</div>
    </div>`;
  }
}
customElements.define("ph-calendar", CalendarTab);

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

const VOICE_EFFECTS = [
  ["", "Plain", "As it is"], ["cartoon", "Cartoon", "High and in a hurry"], ["squirrel", "Squirrel", "Very high"],
  ["monster", "Monster", "Deep and slow"], ["robot", "Robot", "Metallic"], ["nervous", "Nervous", "Shaky voice"], ["echo", "Echo", "Like in a cave"],
];
// Mosko's shapes and moods, as the API numbers them.
const SHUT = 0, SMALL = 1, HALF = 2, BLINK = 4, CALM = 0, EXCITED = 1, THINKING = 3, LAUGHING = 4;
const POKE_LINES = [
  "¡Eh! ¡Que me haces cosquillas! Jajaja.",
  "¿Otra vez? Que soy una pantalla, no un botón...",
  "¡Vale, vale! Ya estoy despierto.",
  "Si me sigues tocando, me pongo el reloj y no te hablo más.",
];
const SYNC = { min: -1000, max: 1500, step: 10, rest: 0 };
const PITCH = { min: 50, max: 200, step: 5, rest: 100 };
const BARS = 40;
// The loudest sample in each of n slices, scaled so the loudest slice fills the bar.
const peaksOf = (data, n) => {
  const size = Math.max(1, Math.floor(data.length / n)), out = [];
  for (let i = 0; i < n; i++) {
    let peak = 0;
    for (let j = i * size; j < Math.min(data.length, (i + 1) * size); j++) peak = Math.max(peak, Math.abs(data[j]));
    out.push(peak);
  }
  const top = Math.max(...out, 0.01);
  return out.map((v) => v / top);
};
const noteIcons = {
  mic: icon(svg`<rect x="9" y="3" width="6" height="11" rx="3"/><path d="M5 11a7 7 0 0 0 14 0M12 18v3"/>`),
  trash: icon(svg`<path d="M4 7h16M10 11v6M14 11v6M6 7l1 13h10l1-13M9 7V4h6v3"/>`),
  stop: icon(svg`<rect x="6" y="6" width="12" height="12" rx="2" fill="currentColor"/>`),
  play: icon(svg`<path d="M8 5v14l11-7z" fill="currentColor"/>`),
  pause: icon(svg`<path d="M7 5h3v14H7zM14 5h3v14h-3z" fill="currentColor"/>`),
  send: icon(svg`<path d="M4 12 20 4l-6 16-3-7z" fill="currentColor"/>`),
};
const faceURL = (shape, mood) => `api/talk/mosko.png?shape=${shape}&mood=${mood}`;
const at = (range, v) => ((v - range.min) / (range.max - range.min)) * 100;

// Mosko says what is typed, or a recording, on the display, his mouth and eyes moving with the voice.
// The page plays the same faces the display shows, and idles with a blink now and then.
class MoskoTab extends LitElement {
  static properties = {
    state: { attribute: false }, text: { state: true }, busy: { state: true }, rec: { state: true },
    lag: { state: true }, pitch: { state: true }, effect: { state: true }, face: { state: true }, talking: { state: true },
    sounds: { state: true }, editSounds: { state: true }, playingSound: { state: true },
  };
  static styles = [base, css`
    .layout { display: grid; grid-template-columns: minmax(0, 1fr); gap: 18px; }
    @media (min-width: 900px) { .layout { grid-template-columns: minmax(260px, 340px) minmax(0, 1fr); align-items: start; } .side { position: sticky; top: 88px; } }
    .side, .main { display: grid; gap: 18px; min-width: 0; }
    .who { display: grid; gap: 10px; justify-items: center; text-align: center; }
    ph-poke { width: min(100%, 240px); }
    @media (max-width: 599px) { ph-poke { width: min(60%, 200px); } }
    .who b { font-family: var(--font-pixel); font-size: 22px; font-weight: 600; }
    .who p { margin: 0; color: var(--muted); font-size: 13px; }
    .mood { display: inline-flex; align-items: center; gap: 8px; font-size: 12px; color: var(--muted); padding: 4px 10px; border-radius: 999px; background: var(--chip); }
    .mood i { width: 7px; height: 7px; border-radius: 50%; background: var(--muted); }
    .mood.on { color: var(--accent); background: color-mix(in srgb, var(--accent) 14%, transparent); }
    .mood.on i { background: var(--accent); animation: pulse 1s ease-in-out infinite; }
    @keyframes pulse { 50% { opacity: .25; } }
    textarea { font: inherit; font-size: 16px; color: var(--ink); background: var(--bg); border: 1px solid var(--line); border-radius: 10px; padding: 10px 12px; width: 100%; min-height: 96px; resize: vertical; }
    .row { display: flex; gap: 8px; flex-wrap: wrap; align-items: center; margin-top: 10px; }
    .row .count { margin-left: auto; font-size: 12px; color: var(--muted); font-family: var(--font-data); }
    .note { display: flex; align-items: center; gap: 10px; margin-top: 10px; padding: 6px; border: 1px solid var(--line); border-radius: 999px; background: var(--bg); }
    .note.recording { border-color: color-mix(in srgb, var(--bad) 45%, var(--line)); }
    .note .round { width: 38px; height: 38px; flex: none; border-radius: 50%; border: 0; background: none; color: var(--muted); cursor: pointer; display: inline-flex; align-items: center; justify-content: center; padding: 0; }
    .note .round:hover { color: var(--ink); background: var(--chip); }
    .note .round svg { width: 20px; height: 20px; }
    .note .stop { color: var(--bad); }
    .note .play { background: var(--chip); color: var(--ink); }
    .note .send { background: var(--accent); color: var(--on-accent); }
    .note .send:hover { background: var(--accent); filter: brightness(1.08); color: var(--on-accent); }
    .note .send:disabled { opacity: .5; cursor: default; }
    .note .dot { width: 10px; height: 10px; flex: none; border-radius: 50%; background: var(--bad); animation: pulse 1s ease-in-out infinite; }
    .note .clock { font-family: var(--font-data); font-size: 13px; color: var(--muted); font-variant-numeric: tabular-nums; min-width: 36px; }
    .wave { flex: 1; min-width: 0; height: 30px; display: flex; align-items: center; gap: 2px; overflow: hidden; }
    .wave i { flex: 1; min-width: 2px; max-width: 4px; border-radius: 2px; background: color-mix(in srgb, var(--muted) 55%, transparent); transition: height .08s linear; }
    .wave i.on { background: var(--accent); }
    .wave.live i { background: var(--bad); }

    .voice { display: grid; gap: 22px; }
    .voice h2 { margin: 0; }
    .field { display: grid; gap: 10px; }
    .field + .field { border-top: 1px solid var(--line); padding-top: 20px; }
    .field-head { display: flex; align-items: baseline; justify-content: space-between; gap: 12px; }
    .field-head .name { font-size: 14px; font-weight: 600; }
    .field-head .sub { font-size: 12px; color: var(--muted); }
    .value { font-family: var(--font-data); font-size: 13px; color: var(--accent); background: color-mix(in srgb, var(--accent) 12%, transparent); padding: 3px 10px; border-radius: 8px; font-variant-numeric: tabular-nums; }
    .slide { display: grid; grid-template-columns: 32px minmax(0, 1fr) 32px; gap: 10px; align-items: center; }
    .step { width: 32px; height: 32px; border-radius: 9px; border: 1px solid var(--line); background: var(--bg); color: var(--muted); font-size: 18px; line-height: 1; cursor: pointer; padding: 0; }
    .step:hover { color: var(--ink); border-color: var(--muted); }
    .track { position: relative; height: 28px; display: flex; align-items: center; }
    .track::before { content: ""; position: absolute; left: var(--rest); top: 4px; bottom: 4px; width: 2px; margin-left: -1px; border-radius: 1px; background: var(--muted); opacity: .5; }
    input.range { position: relative; z-index: 1; -webkit-appearance: none; appearance: none; width: 100%; height: 6px; border-radius: 999px; margin: 0; cursor: pointer; border: 0; padding: 0;
      background: linear-gradient(to right, var(--line) var(--lo), var(--accent) var(--lo), var(--accent) var(--hi), var(--line) var(--hi)); }
    input.range::-webkit-slider-thumb { -webkit-appearance: none; width: 20px; height: 20px; border-radius: 50%; background: var(--ink); border: 4px solid var(--accent); box-shadow: 0 0 0 4px color-mix(in srgb, var(--accent) 20%, transparent); }
    input.range::-moz-range-thumb { width: 12px; height: 12px; border-radius: 50%; background: var(--ink); border: 4px solid var(--accent); box-shadow: 0 0 0 4px color-mix(in srgb, var(--accent) 20%, transparent); }
    .scale { display: flex; justify-content: space-between; font-size: 11px; color: var(--muted); padding: 0 42px; }
    .field-foot { display: flex; align-items: center; justify-content: space-between; gap: 12px; flex-wrap: wrap; }
    .field-foot .hint { margin: 0; flex: 1 1 220px; }
    .link { border: 0; background: none; color: var(--accent); font: inherit; font-size: 12px; cursor: pointer; padding: 0; }
    .effects { display: grid; grid-template-columns: repeat(auto-fill, minmax(132px, 1fr)); gap: 8px; }
    .effect { text-align: left; border: 1px solid var(--line); background: var(--bg); border-radius: 12px; padding: 10px 12px; cursor: pointer; display: grid; gap: 2px; color: var(--ink); }
    .effect span { font-size: 12px; color: var(--muted); }
    .effect:hover { border-color: var(--muted); }
    .effect[aria-pressed=true] { border-color: var(--accent); background: color-mix(in srgb, var(--accent) 12%, var(--bg)); }
    .effect[aria-pressed=true] b { color: var(--accent); }
    .pads { display: grid; grid-template-columns: repeat(auto-fill, minmax(118px, 1fr)); gap: 8px; }
    .pad { position: relative; min-height: 64px; text-align: left; border: 1px solid var(--line); background: var(--bg); border-radius: 12px; padding: 10px 12px; cursor: pointer; display: grid; gap: 4px; align-content: start; color: var(--ink); }
    .pad:hover:not(:disabled) { border-color: var(--muted); }
    .pad:active:not(:disabled) { transform: scale(.97); }
    .pad:disabled { opacity: .5; cursor: default; }
    .pad b { font-size: 14px; font-weight: 600; overflow-wrap: anywhere; padding-right: 18px; }
    .pad span { font-size: 11px; color: var(--muted); display: flex; align-items: center; gap: 4px; }
    .pad span svg { width: 12px; height: 12px; }
    .pad.on { border-color: var(--accent); background: color-mix(in srgb, var(--accent) 12%, var(--bg)); }
    .padwrap { position: relative; }
    .padwrap .pad { width: 100%; height: 100%; }
    .padwrap .x { position: absolute; top: 6px; right: 6px; width: 26px; height: 26px; border-radius: 7px; border: 0; background: var(--chip); color: var(--muted); display: inline-flex; align-items: center; justify-content: center; padding: 0; cursor: pointer; }
    .padwrap .x:hover { color: var(--bad); }
    .padwrap .x svg { width: 14px; height: 14px; }
    .file { position: relative; overflow: hidden; }
    .file input { position: absolute; inset: 0; opacity: 0; cursor: pointer; }
  `];
  constructor() { super(); this.text = ""; this.face = [HALF, CALM]; this.timers = []; this.sounds = null; }
  connectedCallback() {
    super.connectedCallback();
    api("GET", "sounds").then((r) => { this.sounds = r.sounds; }).catch(() => { this.sounds = []; });
    for (let s = 0; s <= 4; s++) for (let m = 0; m <= 4; m++) new Image().src = faceURL(s, m);
    this.idle();
  }
  disconnectedCallback() { super.disconnectedCallback(); this.stopRecording(true); this.discard(); this.clear(); clearTimeout(this.saving); }
  willUpdate(c) {
    if (c.has("state") && this.state && !this.editing) {
      const tune = this.state.talk ?? {};
      this.lag = tune.lag_ms ?? 250; this.pitch = tune.pitch || 100; this.effect = tune.effect ?? "";
    }
  }

  clear() { this.timers.forEach(clearTimeout); this.timers = []; }
  later(ms, fn) { this.timers.push(setTimeout(fn, ms)); }
  // Idle: shut mouth, a blink every few seconds, now and then a look aside.
  idle() {
    this.clear();
    this.talking = false;
    this.face = [SHUT, CALM];
    const next = () => this.later(2500 + Math.random() * 3500, () => {
      if (Math.random() < 0.25) {
        this.face = [SHUT, THINKING];
        this.later(1200, () => { this.face = [SHUT, CALM]; next(); });
      } else {
        this.face = [BLINK, CALM];
        this.later(140, () => { this.face = [SHUT, CALM]; next(); });
      }
    });
    next();
  }
  thinking() { this.clear(); this.face = [SHUT, THINKING]; }
  // A poke startles him; a few in a row tickle him; keep going and he complains out loud on the display.
  // A poke startles him, a few in a row tickle him, and a full turn makes him complain out loud on the display.
  poked({ count, spun }) {
    if (spun && !this.busy && !this.talking) {
      this.say({ text: POKE_LINES[Math.floor(Math.random() * POKE_LINES.length)] });
      return;
    }
    if (this.busy || this.talking) return;
    this.clear();
    if (count >= 4) {
      this.face = [HALF, LAUGHING];
      this.later(900, () => this.idle());
    } else {
      this.face = [SMALL, EXCITED];
      this.later(600, () => this.idle());
    }
  }
  // Plays the faces the display got, each at its time from the start, so they do not drift.
  play(faces, after) {
    this.clear();
    this.talking = true;
    let t = after;
    for (const f of faces) {
      this.later(t, () => { this.face = [f.shape, f.mood]; });
      t += f.ms;
    }
    this.later(t + 300, () => this.idle());
  }

  async say(body, path = "talk") {
    this.busy = true;
    this.thinking();
    try {
      const r = await api("POST", path, body);
      this.play(r.faces ?? [], r.faces_after_ms ?? 0);
      send(this, "refresh");
      return true;
    } catch (e) { toast(this, e.message); this.idle(); return false; }
    finally { this.busy = false; }
  }
  async sayText() {
    const text = this.text.trim();
    if (text && await this.say({ text })) this.text = "";
  }
  async playSound(s) {
    this.playingSound = s.id;
    await this.say(undefined, `sounds/${s.id}/play`);
    this.playingSound = null;
  }
  // A sound needs a name; the phrase's first words or the file's name are offered.
  async keepSound(body, suggested) {
    const name = prompt(t("Name for this sound"), suggested.slice(0, 30));
    if (!name?.trim()) return false;
    if (body instanceof FormData) body.append("name", name.trim());
    else body.name = name.trim();
    try {
      this.sounds = (await api("POST", "sounds", body)).sounds;
      toast(this, t("Added to the sounds"));
      return true;
    } catch (e) { toast(this, e.message); return false; }
  }
  keepText() {
    const text = this.text.trim();
    if (text) this.keepSound({ text }, text);
  }
  keepNote() {
    const blob = this.rec?.blob;
    if (!blob) return;
    const form = new FormData();
    form.append("audio", blob, "note");
    this.keepSound(form, t("Voice note"));
  }
  keepFile(e) {
    const file = e.target.files?.[0];
    e.target.value = "";
    if (!file) return;
    const form = new FormData();
    form.append("audio", file, file.name);
    this.keepSound(form, file.name.replace(/\.[^.]+$/, ""));
  }
  async dropSound(s) {
    if (!confirm(t("Delete {name}?", { name: s.name }))) return;
    try { this.sounds = (await api("DELETE", `sounds/${s.id}`)).sounds; } catch (e) { toast(this, e.message); }
  }
  soundboard() {
    const list = this.sounds ?? [];
    return html`<div class="card">
      <div class="head"><h2>${t("Sounds")}</h2>
        ${list.length ? html`<button class="btn small" aria-pressed=${String(!!this.editSounds)} @click=${() => { this.editSounds = !this.editSounds; }}>${this.editSounds ? t("Done") : t("Edit")}</button>` : nothing}
        <label class="btn small file">${icons.upload}${t("Upload")}<input type="file" accept="audio/*,video/*" @change=${(e) => this.keepFile(e)}></label>
      </div>
      ${this.sounds === null ? html`<p class="hint">${t("Loading")}</p>` : !list.length
        ? html`<p class="hint">${t("Keep phrases and recordings here to play them with a tap: save what you write or record above, or upload an audio file.")}</p>`
        : html`<div class="pads">${list.map((s) => html`<div class="padwrap">
            <button class="pad ${this.playingSound === s.id ? "on" : ""}" ?disabled=${this.busy && this.playingSound !== s.id} title=${s.text ?? ""}
              @click=${() => (this.editSounds ? this.dropSound(s) : this.playSound(s))}>
              <b>${s.name}</b><span>${s.audio ? noteIcons.mic : noteIcons.send}${s.audio ? t("Recording") : t("Phrase")}</span>
            </button>
            ${this.editSounds ? html`<button class="x" aria-label=${t("Delete")} @click=${() => this.dropSound(s)}>${noteIcons.trash}</button>` : nothing}
          </div>`)}</div>`}
      <p class="hint">${t("They play with the voice settings below.")}</p>
    </div>`;
  }
  get canRecord() { return !!(navigator.mediaDevices?.getUserMedia && window.MediaRecorder); }
  // A voice note, as in a chat: record with live levels, then listen, delete or send it.
  async startRecording() {
    let stream;
    try { stream = await navigator.mediaDevices.getUserMedia({ audio: true }); }
    catch (e) { toast(this, t("No microphone: {reason}", { reason: e.message })); return; }
    const chunks = [];
    const recorder = new MediaRecorder(stream);
    const audio = new AudioContext();
    const analyser = audio.createAnalyser();
    analyser.fftSize = 512;
    audio.createMediaStreamSource(stream).connect(analyser);
    const samples = new Uint8Array(analyser.fftSize);
    this.rec = { phase: "recording", started: Date.now(), levels: [], elapsed: 0 };
    const tick = () => {
      if (this.rec?.phase !== "recording") return;
      analyser.getByteTimeDomainData(samples);
      let peak = 0;
      for (const v of samples) peak = Math.max(peak, Math.abs(v - 128) / 128);
      this.rec = { ...this.rec, levels: [...this.rec.levels, peak].slice(-BARS), elapsed: (Date.now() - this.rec.started) / 1000 };
      this.frame = requestAnimationFrame(tick);
    };
    recorder.ondataavailable = (e) => chunks.push(e.data);
    recorder.onstop = async () => {
      stream.getTracks().forEach((tr) => tr.stop());
      cancelAnimationFrame(this.frame);
      if (this.dropRecording) { audio.close(); this.rec = null; return; }
      const blob = new Blob(chunks, { type: recorder.mimeType });
      let peaks = [], duration = this.rec?.elapsed ?? 0;
      try {
        const decoded = await audio.decodeAudioData(await blob.arrayBuffer());
        duration = decoded.duration;
        peaks = peaksOf(decoded.getChannelData(0), BARS);
      } catch { /* the bars stay flat; the note still plays and sends */ }
      audio.close();
      this.rec = { phase: "review", blob, url: URL.createObjectURL(blob), duration, peaks, progress: 0, playing: false };
    };
    this.recorder = recorder;
    this.dropRecording = false;
    recorder.start();
    this.frame = requestAnimationFrame(tick);
    this.limit = setTimeout(() => this.stopRecording(), 30000);
  }
  stopRecording(drop = false) {
    clearTimeout(this.limit);
    if (!this.recorder) return;
    this.dropRecording = drop;
    if (this.recorder.state !== "inactive") this.recorder.stop();
    this.recorder = null;
  }
  togglePlay() {
    const r = this.rec;
    if (!r) return;
    if (!this.player) {
      this.player = new Audio(r.url);
      this.player.ontimeupdate = () => { if (this.rec) this.rec = { ...this.rec, progress: this.player.currentTime / (this.rec.duration || this.player.duration || 1) }; };
      this.player.onended = () => { if (this.rec) this.rec = { ...this.rec, playing: false, progress: 0 }; };
    }
    if (r.playing) this.player.pause(); else this.player.play();
    this.rec = { ...r, playing: !r.playing };
  }
  discard() {
    this.player?.pause();
    this.player = null;
    if (this.rec?.url) URL.revokeObjectURL(this.rec.url);
    this.rec = null;
  }
  async sendNote() {
    const blob = this.rec?.blob;
    if (!blob) return;
    const form = new FormData();
    form.append("audio", blob, "note");
    if (await this.say(form)) this.discard();
  }
  noteBar() {
    const r = this.rec;
    const clock = (s) => `${Math.floor(s / 60)}:${String(Math.floor(s % 60)).padStart(2, "0")}`;
    if (r.phase === "recording") {
      const levels = [...Array(BARS - r.levels.length).fill(0), ...r.levels];
      return html`<div class="note recording">
        <button class="round" aria-label=${t("Cancel")} @click=${() => this.stopRecording(true)}>${noteIcons.trash}</button>
        <span class="dot"></span><span class="clock">${clock(r.elapsed)}</span>
        <div class="wave live">${levels.map((v) => html`<i style="height:${Math.max(8, Math.min(100, v * 160))}%"></i>`)}</div>
        <button class="round stop" aria-label=${t("Stop")} @click=${() => this.stopRecording()}>${noteIcons.stop}</button>
      </div>`;
    }
    const played = Math.round(r.progress * BARS);
    const peaks = r.peaks.length ? r.peaks : Array(BARS).fill(0.15);
    return html`<div class="note">
      <button class="round" aria-label=${t("Delete")} @click=${() => this.discard()}>${noteIcons.trash}</button>
      <button class="round play" aria-label=${r.playing ? t("Pause") : t("Listen")} @click=${() => this.togglePlay()}>${r.playing ? noteIcons.pause : noteIcons.play}</button>
      <div class="wave">${peaks.map((v, i) => html`<i class=${i < played ? "on" : ""} style="height:${Math.max(10, v * 100)}%"></i>`)}</div>
      <span class="clock">${clock(r.duration)}</span>
      <button class="round" title=${t("Keep it among the sounds")} aria-label=${t("Keep it among the sounds")} @click=${() => this.keepNote()}>${icons.bookmark}</button>
      <button class="round send" ?disabled=${this.busy} aria-label=${t("Send to Mosko")} @click=${() => this.sendNote()}>${noteIcons.send}</button>
    </div>`;
  }

  // Sliders and steps save a moment after the last change.
  tune(change, wait = 400) {
    this.editing = true;
    Object.assign(this, change);
    clearTimeout(this.saving);
    this.saving = setTimeout(async () => {
      try { saved(this, await api("PUT", "settings", { talk: { lag_ms: this.lag, pitch: this.pitch, effect: this.effect } })); }
      catch (e) { toast(this, e.message); }
      this.editing = false;
    }, wait);
  }
  slider(range, value, set) {
    const clamp = (v) => Math.min(range.max, Math.max(range.min, v));
    const p = at(range, value), rest = at(range, range.rest);
    return html`<div class="slide">
      <button class="step" aria-label=${t("Less")} @click=${() => set(clamp(value - range.step))}>−</button>
      <div class="track" style="--rest:${rest}%">
        <input class="range" type="range" min=${range.min} max=${range.max} step=${range.step} .value=${String(value)}
          style="--lo:${Math.min(p, rest)}%;--hi:${Math.max(p, rest)}%" @input=${(e) => set(Number(e.target.value))}>
      </div>
      <button class="step" aria-label=${t("More")} @click=${() => set(clamp(value + range.step))}>+</button>
    </div>`;
  }
  status() {
    if (this.busy) return [true, t("Thinking…")];
    if (this.talking) return [true, t("Talking")];
    return [false, t("Waiting for you")];
  }

  render() {
    const lag = this.lag ?? 250, pitch = this.pitch ?? 100;
    const [on, mood] = this.status();
    return html`<div class="layout">
      <div class="side">
        <div class="card who">
          <ph-poke .label=${t("Poke Mosko")} @poke=${(e) => this.poked(e.detail)}><ph-led .src=${faceURL(...this.face)} .label=${"Mosko"}></ph-led></ph-poke>
          <b>Mosko</b>
          <span class="mood ${on ? "on" : ""}"><i></i>${mood}</span>
          <p>${t("Lives in the display and says whatever you tell him.")}</p>
        </div>
      </div>
      <div class="main">
        <div class="card">
          <div class="head"><h2>${t("What should he say?")}</h2></div>
          <textarea maxlength="400" .value=${this.text} placeholder=${t("Hola, soy Mosko")}
            @input=${(e) => { this.text = e.target.value; }}
            @keydown=${(e) => { if (e.key === "Enter" && !e.shiftKey) { e.preventDefault(); this.sayText(); } }}></textarea>
          ${this.rec ? this.noteBar() : html`<div class="row">
            <button class="btn primary" ?disabled=${this.busy || !this.text.trim()} @click=${() => this.sayText()}>${this.busy ? t("Thinking…") : t("Say it")}</button>
            ${this.canRecord ? html`<button class="btn" ?disabled=${this.busy} @click=${() => this.startRecording()}>${noteIcons.mic}${t("Record")}</button>` : nothing}
            <button class="btn" title=${t("Keep it among the sounds")} ?disabled=${!this.text.trim()} @click=${() => this.keepText()}>${icons.bookmark}${t("Keep")}</button>
            <span class="count">${this.text.length}/400</span>
          </div>`}
          <p class="hint">${t("Up to 30 seconds. The mouth follows the voice: shut in the pauses, wider the louder.")}</p>
          <p class="hint">${t("He gestures with the text: ! raises his eyebrows, ? raises one, ... makes him think, jaja makes him laugh.")}</p>
        </div>
        ${this.soundboard()}
        <div class="card voice">
          <h2>${t("Voice")}</h2>
          <div class="field">
            <div class="field-head"><div class="name">${t("Effect")}</div></div>
            <div class="effects">${VOICE_EFFECTS.map(([v, name, desc]) => html`<button class="effect" aria-pressed=${String((this.effect ?? "") === v)}
              @click=${() => this.tune({ effect: v }, 0)}><b>${t(name)}</b><span>${t(desc)}</span></button>`)}</div>
          </div>
          <div class="field">
            <div class="field-head">
              <div><div class="name">${t("Pitch")}</div>${pitch !== 100 ? html`<button class="link" @click=${() => this.tune({ pitch: 100 }, 0)}>${t("Back to 100%")}</button>` : html`<div class="sub">${t("as the voice is")}</div>`}</div>
              <span class="value">${pitch}%</span>
            </div>
            ${this.slider(PITCH, pitch, (v) => this.tune({ pitch: v }))}
            <div class="scale"><span>${t("Deeper")}</span><span>${t("Higher")}</span></div>
          </div>
          <div class="field">
            <div class="field-head">
              <div><div class="name">${t("Sync")}</div><div class="sub">${lag === 0 ? t("in step") : lag > 0 ? t("the voice comes later") : t("the voice comes first")}</div></div>
              <span class="value">${lag > 0 ? "+" : ""}${lag} ms</span>
            </div>
            ${this.slider(SYNC, lag, (v) => this.tune({ lag: v }))}
            <div class="scale"><span>${t("Voice first")}</span><span>${t("Voice later")}</span></div>
            <div class="field-foot">
              <p class="hint">${t("If the mouth moves before the sound, move it right; if after, left.")}</p>
              <button class="btn small" ?disabled=${this.busy} @click=${() => this.say({ text: "¡Pa! ¡Pa! ¡Pa! ¡Pa! ¡Pa! ¡Pa!" })}>${t("Test the sync")}</button>
            </div>
          </div>
        </div>
      </div>
    </div>`;
  }
}
customElements.define("ph-mosko", MoskoTab);

// The drawing tab: animations drawn pixel by pixel, kept in pixelhub and shown on the display.
const SIDE = 16;
const BLACK = "#000000";
const PALETTE = ["#ffffff", "#ff0000", "#ff7a00", "#ffd400", "#9dff00", "#00d43a", "#00e0b0", "#00c8ff",
  "#0050ff", "#5a2dff", "#b026ff", "#ff3fa4", "#ffb38a", "#8a4b1c", "#7a7a7a", BLACK];
const SPEEDS = [[1000, "1"], [500, "2"], [250, "4"], [125, "8"], [83, "12"]];
const PROJECT_FOR = [["1m", "1 min"], ["10m", "10 min"], ["1h", "1 h"]];
const MAX_FRAMES = 60;
const DRAFT_KEY = "pixelhub.draft";
const blank = () => Array(SIDE * SIDE).fill(BLACK);
const toHex = (frame) => frame.map((c) => c.slice(1)).join("");
const fromHex = (s) => Array.from({ length: SIDE * SIDE }, (_, i) => `#${s.slice(i * 6, i * 6 + 6)}`);
const drawIcon = {
  pen: icon(svg`<path d="M4 20l4-1L19 8l-3-3L5 16z"/><path d="m14 7 3 3"/>`),
  eraser: icon(svg`<path d="m8 20 12-12-4-4L4 16l4 4h12"/><path d="m9 11 4 4"/>`),
  fill: icon(svg`<path d="m11 4 8 8-7 7-8-8z"/><path d="M4 11h15M20 15s1.5 2 1.5 3a1.5 1.5 0 0 1-3 0c0-1 1.5-3 1.5-3z"/>`),
  pick: icon(svg`<path d="m14 4 6 6-2 2-6-6zM12 8l-7 7v4h4l7-7"/>`),
  mirror: icon(svg`<path d="M12 3v18" stroke-dasharray="2 2"/><path d="M9 7 4 12l5 5zM15 7l5 5-5 5z"/>`),
  undo: icon(svg`<path d="M9 14 4 9l5-5"/><path d="M4 9h10a6 6 0 0 1 0 12h-3"/>`),
  redo: icon(svg`<path d="m15 14 5-5-5-5"/><path d="M20 9H10a6 6 0 0 0 0 12h3"/>`),
  onion: icon(svg`<rect x="3" y="7" width="11" height="11" rx="2" stroke-dasharray="2 2"/><rect x="9" y="4" width="11" height="11" rx="2"/>`),
  copy: icon(svg`<rect x="8" y="8" width="12" height="12" rx="2"/><path d="M16 8V5a1 1 0 0 0-1-1H5a1 1 0 0 0-1 1v10a1 1 0 0 0 1 1h3"/>`),
  left: icon(svg`<path d="m15 18-6-6 6-6"/>`),
  right: icon(svg`<path d="m9 18 6-6-6-6"/>`),
  cast: icon(svg`<rect x="3" y="4" width="18" height="13" rx="2"/><path d="M8 21h8M12 17v4"/>`),
  live: icon(svg`<circle cx="12" cy="12" r="2.5" fill="currentColor"/><path d="M7.8 7.8a6 6 0 0 0 0 8.4M16.2 7.8a6 6 0 0 1 0 8.4M5 5a10 10 0 0 0 0 14M19 5a10 10 0 0 1 0 14"/>`),
  stop: icon(svg`<rect x="6" y="6" width="12" height="12" rx="2"/>`),
};
const decoded = new WeakMap();
const framesOf = (d) => { let f = decoded.get(d); if (!f) decoded.set(d, (f = d.frames.map(fromHex))); return f; };
const frameURLs = new Map();
// A frame as a 16x16 PNG data URL, for thumbnails and the LED preview; kept, since frames repeat.
const frameURL = (frame) => {
  const key = frame.join("");
  let url = frameURLs.get(key);
  if (url) return url;
  const c = document.createElement("canvas");
  c.width = c.height = SIDE;
  const ctx = c.getContext("2d");
  const img = ctx.createImageData(SIDE, SIDE);
  frame.forEach((hex, i) => {
    const n = parseInt(hex.slice(1), 16);
    img.data.set([n >> 16, (n >> 8) & 255, n & 255, 255], i * 4);
  });
  ctx.putImageData(img, 0, 0);
  url = c.toDataURL();
  if (frameURLs.size > 400) frameURLs.delete(frameURLs.keys().next().value);
  frameURLs.set(key, url);
  return url;
};

class DrawTab extends LitElement {
  static properties = {
    frames: { state: true }, cur: { state: true }, color: { state: true }, tool: { state: true }, mirror: { state: true }, onion: { state: true },
    delay: { state: true }, name: { state: true }, id: { state: true }, dirty: { state: true }, live: { state: true }, busy: { state: true },
    drawings: { state: true }, lists: { state: true }, showFor: { state: true }, beat: { state: true }, recent: { state: true },
  };
  static styles = [base, css`
    .layout { display: grid; grid-template-columns: minmax(0, 1fr); gap: 18px; }
    @media (min-width: 900px) { .layout { grid-template-columns: minmax(0, 560px) minmax(0, 1fr); align-items: start; } .side { position: sticky; top: 88px; } }
    .main, .side { display: grid; gap: 14px; min-width: 0; align-content: start; }
    .board { display: block; width: 100%; aspect-ratio: 1; border-radius: 12px; background: var(--case); touch-action: none; cursor: crosshair; }
    .tools { display: flex; flex-wrap: wrap; gap: 8px; align-items: center; margin-top: 12px; }
    .tools .sp { flex: 1; }
    .tool { width: 40px; height: 40px; border-radius: 10px; border: 1px solid var(--line); background: var(--bg); color: var(--muted); cursor: pointer; display: inline-flex; align-items: center; justify-content: center; padding: 0; }
    .tool:hover:not(:disabled) { color: var(--ink); border-color: var(--muted); }
    .tool[aria-pressed=true] { color: var(--accent); border-color: var(--accent); background: color-mix(in srgb, var(--accent) 12%, var(--bg)); }
    .tool:disabled { opacity: .35; cursor: default; }
    .tool svg { width: 20px; height: 20px; }
    .group { display: inline-flex; gap: 4px; }
    .palette { display: grid; grid-template-columns: repeat(8, minmax(0, 1fr)); gap: 6px; margin-top: 12px; }
    .swatch { aspect-ratio: 1; border-radius: 8px; border: 2px solid transparent; cursor: pointer; padding: 0; box-shadow: inset 0 0 0 1px rgba(255, 255, 255, .12); }
    .swatch[aria-pressed=true] { border-color: var(--ink); box-shadow: 0 0 0 2px var(--accent); }
    .custom { display: flex; gap: 8px; align-items: center; margin-top: 10px; flex-wrap: wrap; }
    .custom .recent { display: flex; gap: 6px; }
    .custom .recent .swatch { width: 28px; }
    .frames { display: flex; gap: 8px; overflow-x: auto; padding: 4px 2px 8px; scroll-snap-type: x proximity; }
    .frame { flex: none; width: 56px; display: grid; gap: 3px; justify-items: center; border: 0; background: none; cursor: pointer; padding: 0; color: var(--muted); font-size: 11px; font-family: var(--font-data); scroll-snap-align: start; }
    .frame img { width: 56px; height: 56px; border-radius: 8px; border: 2px solid var(--line); background: #000; }
    .frame[aria-current=true] img { border-color: var(--accent); }
    .frame[aria-current=true] { color: var(--accent); }
    .frame.add { width: 56px; height: 56px; border: 2px dashed var(--line); border-radius: 8px; align-content: center; }
    .frame.add:hover { color: var(--ink); border-color: var(--muted); }
    .frame-ops { display: flex; gap: 6px; flex-wrap: wrap; align-items: center; }
    .frame-ops .count { margin-left: auto; font-size: 12px; color: var(--muted); font-family: var(--font-data); }
    ph-led { width: min(100%, 260px); margin: 0 auto; }
    .preview { display: grid; gap: 12px; justify-items: center; }
    .row { display: flex; gap: 8px; flex-wrap: wrap; align-items: center; }
    .row input[type=text] { flex: 1 1 160px; }
    .live[aria-pressed=true] { background: color-mix(in srgb, var(--bad) 18%, var(--surface)); border-color: var(--bad); color: var(--ink); }
    .live[aria-pressed=true] svg { color: var(--bad); animation: pulse 1.2s ease-in-out infinite; }
    @keyframes pulse { 50% { opacity: .3; } }
    .saved { display: grid; grid-template-columns: repeat(auto-fill, minmax(104px, 1fr)); gap: 10px; }
    .item { position: relative; display: grid; gap: 4px; border: 1px solid var(--line); background: var(--bg); border-radius: 12px; padding: 8px; cursor: pointer; text-align: left; color: var(--ink); }
    .item[aria-current=true] { border-color: var(--accent); }
    .item img { width: 100%; aspect-ratio: 1; border-radius: 6px; background: #000; }
    .item b { font-size: 13px; font-weight: 600; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
    .item span { font-size: 11px; color: var(--muted); }
    .item .icon { position: absolute; top: 4px; right: 4px; background: color-mix(in srgb, var(--bg) 80%, transparent); }
    .empty { font-size: 13px; color: var(--muted); margin: 0; }
  `];

  constructor() {
    super();
    this.color = "#ff0000"; this.tool = "pen"; this.mirror = false; this.onion = false; this.showFor = "10m";
    this.drawings = null; this.lists = []; this.beat = 0; this.live = false;
    this.undos = []; this.redos = [];
    this.recent = [];
    let draft = null;
    try { draft = JSON.parse(localStorage.getItem(DRAFT_KEY)); } catch { /* no draft */ }
    if (draft?.frames?.length) this.load(draft, draft.dirty);
    else this.fresh();
  }
  connectedCallback() {
    super.connectedCallback();
    api("GET", "drawings").then((r) => { this.drawings = r.drawings; }).catch((e) => { this.drawings = []; toast(this, e.message); });
    api("GET", "lists").then((r) => { this.lists = r.lists; }).catch(() => {});
    this.ticker = setInterval(() => { this.beat++; }, 50);
    this.keys = (e) => {
      if (!(e.metaKey || e.ctrlKey) || e.key.toLowerCase() !== "z" || e.composedPath()[0]?.tagName === "INPUT") return;
      e.preventDefault();
      if (e.shiftKey) this.redo(); else this.undo();
    };
    addEventListener("keydown", this.keys);
  }
  disconnectedCallback() {
    super.disconnectedCallback();
    clearInterval(this.ticker); clearTimeout(this.liveTimer); clearTimeout(this.draftTimer);
    removeEventListener("keydown", this.keys);
    this.resize?.disconnect();
  }
  firstUpdated() {
    const board = this.renderRoot.querySelector(".board");
    this.resize = new ResizeObserver(() => this.paint());
    this.resize.observe(board);
  }
  updated(c) {
    if (c.has("frames") || c.has("cur") || c.has("onion")) this.paint();
    if (c.has("frames") || c.has("delay") || c.has("name") || c.has("id")) this.keepDraft();
  }

  fresh() {
    this.frames = [blank()]; this.cur = 0; this.delay = 250; this.name = ""; this.id = null; this.dirty = false;
    this.undos = []; this.redos = [];
  }
  load(d, dirty = false) {
    this.frames = d.frames.map((f) => (typeof f === "string" ? fromHex(f) : [...f]));
    this.cur = 0; this.delay = d.delay_ms ?? d.delay ?? 250; this.name = d.name ?? ""; this.id = d.id ?? null; this.dirty = dirty;
    this.undos = []; this.redos = [];
  }
  keepDraft() {
    clearTimeout(this.draftTimer);
    this.draftTimer = setTimeout(() => {
      try { localStorage.setItem(DRAFT_KEY, JSON.stringify({ frames: this.frames.map(toHex), delay_ms: this.delay, name: this.name, id: this.id, dirty: this.dirty })); }
      catch { /* the draft is a convenience */ }
    }, 400);
  }
  confirmLeave() { return !this.dirty || confirm(t("Drop the changes to this drawing?")); }

  // Every change goes through here: undo keeps the frames before it.
  change(frames, cur = this.cur) {
    this.undos = [...this.undos.slice(-49), { frames: this.frames, cur: this.cur }];
    this.redos = [];
    this.frames = frames; this.cur = Math.max(0, Math.min(cur, frames.length - 1)); this.dirty = true;
    this.sendLive();
  }
  undo() {
    const last = this.undos.at(-1);
    if (!last) return;
    this.redos = [...this.redos, { frames: this.frames, cur: this.cur }];
    this.undos = this.undos.slice(0, -1);
    this.frames = last.frames; this.cur = last.cur; this.dirty = true;
    this.sendLive();
  }
  redo() {
    const next = this.redos.at(-1);
    if (!next) return;
    this.undos = [...this.undos, { frames: this.frames, cur: this.cur }];
    this.redos = this.redos.slice(0, -1);
    this.frames = next.frames; this.cur = next.cur; this.dirty = true;
    this.sendLive();
  }

  paint() {
    const board = this.renderRoot.querySelector(".board");
    if (!board || !this.frames) return;
    const px = Math.round(board.clientWidth * devicePixelRatio) || 320;
    if (board.width !== px) board.width = board.height = px;
    const ctx = board.getContext("2d"), cell = px / SIDE, frame = this.stroke ?? this.frames[this.cur];
    const under = this.onion && this.cur > 0 ? this.frames[this.cur - 1] : null;
    ctx.fillStyle = "#07060d";
    ctx.fillRect(0, 0, px, px);
    for (let i = 0; i < SIDE * SIDE; i++) {
      const x = (i % SIDE) * cell, y = Math.floor(i / SIDE) * cell;
      if (frame[i] !== BLACK) { ctx.globalAlpha = 1; ctx.fillStyle = frame[i]; }
      else if (under && under[i] !== BLACK) { ctx.globalAlpha = 0.28; ctx.fillStyle = under[i]; }
      else continue;
      ctx.fillRect(x, y, cell, cell);
    }
    ctx.globalAlpha = 1;
    ctx.strokeStyle = "rgba(255,255,255,.07)";
    ctx.lineWidth = Math.max(1, devicePixelRatio * 0.5);
    ctx.beginPath();
    for (let k = 1; k < SIDE; k++) { const p = Math.round(k * cell) + 0.5; ctx.moveTo(p, 0); ctx.lineTo(p, px); ctx.moveTo(0, p); ctx.lineTo(px, p); }
    ctx.stroke();
    ctx.strokeStyle = "rgba(255,255,255,.16)";
    ctx.beginPath();
    const mid = Math.round(8 * cell) + 0.5;
    ctx.moveTo(mid, 0); ctx.lineTo(mid, px); ctx.moveTo(0, mid); ctx.lineTo(px, mid);
    ctx.stroke();
  }

  cellAt(e) {
    const r = e.currentTarget.getBoundingClientRect();
    const x = Math.floor(((e.clientX - r.left) / r.width) * SIDE), y = Math.floor(((e.clientY - r.top) / r.height) * SIDE);
    return x >= 0 && y >= 0 && x < SIDE && y < SIDE ? [x, y] : null;
  }
  plot(frame, x, y, c) {
    frame[y * SIDE + x] = c;
    if (this.mirror) frame[y * SIDE + (SIDE - 1 - x)] = c;
  }
  down(e) {
    const at = this.cellAt(e);
    if (!at || e.button > 0) return;
    const [x, y] = at, frame = this.frames[this.cur];
    if (this.tool === "pick") {
      this.color = frame[y * SIDE + x]; this.tool = "pen";
      return;
    }
    if (this.tool === "fill") {
      const next = [...frame], from = frame[y * SIDE + x], to = this.color;
      if (from === to) return;
      const stack = [[x, y]];
      while (stack.length) {
        const [cx, cy] = stack.pop();
        if (cx < 0 || cy < 0 || cx >= SIDE || cy >= SIDE || next[cy * SIDE + cx] !== from) continue;
        next[cy * SIDE + cx] = to;
        stack.push([cx + 1, cy], [cx - 1, cy], [cx, cy + 1], [cx, cy - 1]);
      }
      this.change(this.frames.map((f, i) => (i === this.cur ? next : f)));
      this.pickedColor();
      return;
    }
    e.currentTarget.setPointerCapture(e.pointerId);
    this.stroke = [...frame];
    this.last = at;
    this.plot(this.stroke, x, y, this.tool === "eraser" ? BLACK : this.color);
    this.paint();
  }
  move(e) {
    if (!this.stroke) return;
    const at = this.cellAt(e);
    if (!at) return;
    const c = this.tool === "eraser" ? BLACK : this.color;
    // A line from the last cell, so a fast stroke leaves no gaps.
    let [x0, y0] = this.last;
    const [x1, y1] = at, dx = Math.abs(x1 - x0), dy = -Math.abs(y1 - y0), sx = x0 < x1 ? 1 : -1, sy = y0 < y1 ? 1 : -1;
    let err = dx + dy;
    for (;;) {
      this.plot(this.stroke, x0, y0, c);
      if (x0 === x1 && y0 === y1) break;
      const e2 = 2 * err;
      if (e2 >= dy) { err += dy; x0 += sx; }
      if (e2 <= dx) { err += dx; y0 += sy; }
    }
    this.last = at;
    this.paint();
  }
  up() {
    if (!this.stroke) return;
    const done = this.stroke;
    this.stroke = null;
    if (done.join() !== this.frames[this.cur].join()) {
      this.change(this.frames.map((f, i) => (i === this.cur ? done : f)));
      if (this.tool === "pen") this.pickedColor();
    }
  }
  pickedColor() {
    if (PALETTE.includes(this.color)) return;
    this.recent = [this.color, ...this.recent.filter((c) => c !== this.color)].slice(0, 6);
  }

  addFrame(copy) {
    if (this.frames.length >= MAX_FRAMES) { toast(this, t("The display plays up to {n} frames", { n: MAX_FRAMES })); return; }
    const f = copy ? [...this.frames[this.cur]] : blank();
    this.change([...this.frames.slice(0, this.cur + 1), f, ...this.frames.slice(this.cur + 1)], this.cur + 1);
  }
  dropFrame() {
    if (this.frames.length === 1) { this.change([blank()], 0); return; }
    this.change(this.frames.filter((_, i) => i !== this.cur), this.cur - (this.cur === this.frames.length - 1 ? 1 : 0));
  }
  moveFrame(by) {
    const to = this.cur + by;
    if (to < 0 || to >= this.frames.length) return;
    const next = [...this.frames];
    [next[this.cur], next[to]] = [next[to], next[this.cur]];
    this.change(next, to);
  }
  // Moves the whole frame a pixel, wrapping round: handy for scrolling animations.
  shift(dx, dy) {
    const f = this.frames[this.cur], next = blank();
    for (let y = 0; y < SIDE; y++) for (let x = 0; x < SIDE; x++) next[((y + dy + SIDE) % SIDE) * SIDE + ((x + dx + SIDE) % SIDE)] = f[y * SIDE + x];
    this.change(this.frames.map((g, i) => (i === this.cur ? next : g)));
  }

  body() { return { name: this.name.trim() || t("Untitled"), delay_ms: this.delay, frames: this.frames.map(toHex) }; }
  async save() {
    this.busy = true;
    try {
      const body = this.body();
      const r = this.id ? await api("PUT", `drawings/${this.id}`, body) : await api("POST", "drawings", body);
      this.drawings = r.drawings; this.id = r.drawing.id; this.name = r.drawing.name; this.dirty = false;
      toast(this, t("Saved"));
      return true;
    } catch (e) { toast(this, e.message); return false; }
    finally { this.busy = false; }
  }
  async project(body = this.body(), quiet = false) {
    try {
      await api("POST", "drawings/show", { ...body, for: this.live ? "10m" : this.showFor });
      send(this, "refresh");
      if (!quiet) toast(this, t("On the display for {d}", { d: PROJECT_FOR.find(([v]) => v === this.showFor)?.[1] ?? this.showFor }));
    } catch (e) { toast(this, e.message); }
  }
  async stop() {
    this.live = false;
    try { await api("DELETE", "show"); send(this, "refresh"); } catch (e) { toast(this, e.message); }
  }
  toggleLive() {
    this.live = !this.live;
    if (this.live) this.project(this.body(), true); else this.stop();
  }
  sendLive() {
    if (!this.live) return;
    clearTimeout(this.liveTimer);
    this.liveTimer = setTimeout(() => this.project(this.body(), true), 350);
  }
  async addToList(list) {
    if (!list) return;
    if ((this.dirty || !this.id) && !(await this.save())) return;
    try {
      this.lists = (await api("POST", `drawings/${this.id}/list`, { list })).lists;
      const l = this.lists.find((x) => x.name === list);
      toast(this, t("Added to {list}", { list: l ? listName(l) : list }));
    } catch (e) { toast(this, e.message); }
  }
  open(d) {
    if (d.id === this.id && !this.dirty) return;
    if (!this.confirmLeave()) return;
    this.load(d);
    scrollTo({ top: 0, behavior: "smooth" });
    this.sendLive();
  }
  startNew() {
    if (!this.confirmLeave()) return;
    this.fresh();
    this.sendLive();
  }
  async deleteDrawing(d) {
    if (!confirm(t("Delete {name}?", { name: d.name }))) return;
    try {
      this.drawings = (await api("DELETE", `drawings/${d.id}`)).drawings;
      if (d.id === this.id) { this.id = null; this.dirty = true; }
    } catch (e) { toast(this, e.message); }
  }
  // The frame a looping animation is on now, from the shared 50 ms beat.
  playing(frames, delay) { return frames[Math.floor((this.beat * 50) / Math.max(delay, 50)) % frames.length]; }

  render() {
    const frames = this.frames, n = frames.length;
    const tools = [["pen", t("Pencil"), drawIcon.pen], ["eraser", t("Eraser"), drawIcon.eraser], ["fill", t("Fill"), drawIcon.fill], ["pick", t("Pick a colour"), drawIcon.pick]];
    return html`<div class="layout">
      <div class="main">
        <div class="card">
          <canvas class="board" role="img" aria-label=${t("Frame {i} of {n}", { i: this.cur + 1, n })}
            @pointerdown=${(e) => this.down(e)} @pointermove=${(e) => this.move(e)} @pointerup=${() => this.up()} @pointercancel=${() => this.up()}
            @contextmenu=${(e) => e.preventDefault()}></canvas>
          <div class="tools">
            <div class="group" role="group" aria-label=${t("Tool")}>${tools.map(([id, label, glyph]) =>
              html`<button class="tool" title=${label} aria-label=${label} aria-pressed=${String(this.tool === id)} @click=${() => { this.tool = id; }}>${glyph}</button>`)}</div>
            <button class="tool" title=${t("Mirror")} aria-label=${t("Mirror")} aria-pressed=${String(this.mirror)} @click=${() => { this.mirror = !this.mirror; }}>${drawIcon.mirror}</button>
            <button class="tool" title=${t("See the frame before")} aria-label=${t("See the frame before")} aria-pressed=${String(this.onion)} @click=${() => { this.onion = !this.onion; }}>${drawIcon.onion}</button>
            <span class="sp"></span>
            <button class="tool" title=${t("Undo")} aria-label=${t("Undo")} ?disabled=${!this.undos.length} @click=${() => this.undo()}>${drawIcon.undo}</button>
            <button class="tool" title=${t("Redo")} aria-label=${t("Redo")} ?disabled=${!this.redos.length} @click=${() => this.redo()}>${drawIcon.redo}</button>
          </div>
          <div class="palette">${PALETTE.map((c) => html`<button class="swatch" style="background:${c}" aria-label=${c} aria-pressed=${String(this.color === c && this.tool !== "eraser")}
            @click=${() => { this.color = c; if (this.tool !== "fill") this.tool = "pen"; }}></button>`)}</div>
          <div class="custom">
            <input type="color" aria-label=${t("Another colour")} .value=${this.color} @input=${(e) => { this.color = e.target.value; if (this.tool !== "fill") this.tool = "pen"; }}>
            <div class="recent">${this.recent.map((c) => html`<button class="swatch" style="background:${c}" aria-label=${c} aria-pressed=${String(this.color === c)} @click=${() => { this.color = c; }}></button>`)}</div>
            <span class="sp" style="flex:1"></span>
            <div class="group" role="group" aria-label=${t("Move the frame")}>
              <button class="tool" aria-label=${t("Shift left")} @click=${() => this.shift(-1, 0)}>${drawIcon.left}</button>
              <button class="tool" aria-label=${t("Shift up")} @click=${() => this.shift(0, -1)}>${icons.up}</button>
              <button class="tool" aria-label=${t("Shift down")} @click=${() => this.shift(0, 1)}>${icons.down}</button>
              <button class="tool" aria-label=${t("Shift right")} @click=${() => this.shift(1, 0)}>${drawIcon.right}</button>
            </div>
          </div>
        </div>
        <div class="card">
          <div class="head"><h2>${t("Frames")}</h2></div>
          <div class="frames">
            ${frames.map((f, i) => html`<button class="frame" aria-current=${String(i === this.cur)} @click=${() => { this.cur = i; }}><img class="pix" src=${frameURL(f)} alt=""><span>${i + 1}</span></button>`)}
            <button class="frame add" title=${t("Copy this frame")} aria-label=${t("Copy this frame")} @click=${() => this.addFrame(true)}>${icons.plus}</button>
          </div>
          <div class="frame-ops">
            <button class="btn small" @click=${() => this.addFrame(true)}>${drawIcon.copy}${t("Copy")}</button>
            <button class="btn small" @click=${() => this.addFrame(false)}>${icons.plus}${t("Blank")}</button>
            <button class="icon" aria-label=${t("Move it before")} ?disabled=${this.cur === 0} @click=${() => this.moveFrame(-1)}>${drawIcon.left}</button>
            <button class="icon" aria-label=${t("Move it after")} ?disabled=${this.cur === n - 1} @click=${() => this.moveFrame(1)}>${drawIcon.right}</button>
            <button class="icon trash" aria-label=${t("Delete this frame")} @click=${() => this.dropFrame()}>${icons.trash}</button>
            <span class="count">${this.cur + 1}/${n}</span>
          </div>
        </div>
      </div>
      <div class="side">
        <div class="card preview">
          <ph-led .src=${frameURL(this.stroke && n === 1 ? this.stroke : this.playing(frames, this.delay))} .label=${t("Preview")}></ph-led>
          ${n > 1 ? html`<div class="row"><span class="small muted">${t("Frames per second")}</span>
            ${segmented(SPEEDS, this.delay, (v) => { this.delay = v; this.dirty = true; this.sendLive(); })}</div>` : nothing}
        </div>
        <div class="card">
          <div class="head"><h2>${t("On the display")}</h2></div>
          <div class="row">
            <button class="btn live" aria-pressed=${String(this.live)} @click=${() => this.toggleLive()}>${drawIcon.live}${this.live ? t("Live: on") : t("Live")}</button>
            <button class="btn primary" @click=${() => this.project()}>${drawIcon.cast}${t("Show it")}</button>
            ${segmented(PROJECT_FOR, this.showFor, (v) => { this.showFor = v; })}
            <button class="btn small" @click=${() => this.stop()}>${drawIcon.stop}${t("Take it off")}</button>
          </div>
          <p class="hint">${t("Live sends every stroke to the display as you draw.")}</p>
        </div>
        <div class="card">
          <div class="head"><h2>${this.id ? t("Editing") : t("New drawing")}</h2>${this.dirty ? html`<span class="tag on">${t("Not saved")}</span>` : nothing}</div>
          <div class="row">
            <input type="text" maxlength="40" .value=${this.name} placeholder=${t("Name")} aria-label=${t("Name")}
              @input=${(e) => { this.name = e.target.value; this.dirty = true; }} @keydown=${(e) => { if (e.key === "Enter") this.save(); }}>
            <button class="btn primary" ?disabled=${this.busy} @click=${() => this.save()}>${t("Save")}</button>
          </div>
          <div class="row" style="margin-top:10px">
            <select aria-label=${t("Add to a list")} @change=${(e) => { const v = e.target.value; e.target.value = ""; this.addToList(v); }}>
              <option value="">${t("Add to a list…")}</option>
              ${this.lists.map((l) => html`<option value=${l.name}>${listName(l)}</option>`)}
            </select>
            <button class="btn" @click=${() => this.startNew()}>${icons.plus}${t("New drawing")}</button>
          </div>
          <p class="hint">${t("In a list, it joins the pictures the display shows in automatic.")}</p>
        </div>
        <div class="card">
          <div class="head"><h2>${t("Your drawings")}</h2></div>
          ${this.drawings === null ? html`<p class="empty">${t("Loading")}</p>` : !this.drawings.length ? html`<p class="empty">${t("Nothing saved yet.")}</p>` : html`<div class="saved">
            ${repeat(this.drawings, (d) => d.id, (d) => {
              const fs = framesOf(d);
              return html`<div class="item" role="button" tabindex="0" aria-current=${String(d.id === this.id)} @click=${() => this.open(d)} @keydown=${(e) => { if (e.key === "Enter") this.open(d); }}>
                <img class="pix" src=${frameURL(this.playing(fs, d.delay_ms))} alt="">
                <b>${d.name}</b><span>${t("{n} frames", { n: fs.length })}</span>
                <button class="icon trash" aria-label=${t("Delete")} @click=${(e) => { e.stopPropagation(); this.deleteDrawing(d); }}>${icons.trash}</button>
              </div>`;
            })}</div>`}
        </div>
      </div>
    </div>`;
  }
}
customElements.define("ph-draw", DrawTab);

const TABS = [
  ["display", "Display", icon(svg`<rect x="4" y="4" width="16" height="16" rx="3"/><path d="M8.5 8.5h.01M12 8.5h.01M15.5 8.5h.01M8.5 12h.01M12 12h.01M15.5 12h.01M8.5 15.5h.01M12 15.5h.01M15.5 15.5h.01" stroke-width="2.6"/>`)],
  ["gallery", "Gallery", icon(svg`<rect x="3.5" y="3.5" width="7" height="7" rx="1.5"/><rect x="13.5" y="3.5" width="7" height="7" rx="1.5"/><rect x="3.5" y="13.5" width="7" height="7" rx="1.5"/><path d="m14 20 2.5-3.5 2 2.5 1.5-2"/>`)],
  ["draw", "Draw", icon(svg`<path d="M4 20l4-1L19 8l-3-3L5 16z"/><path d="m14 7 3 3M15 20h5"/>`)],
  ["music", "Music", icon(svg`<path d="M9 18V5l11-2v13"/><circle cx="6" cy="18" r="3"/><circle cx="17" cy="16" r="3"/>`)],
  ["calendar", "Calendar", icon(svg`<rect x="3.5" y="5" width="17" height="15.5" rx="2.5"/><path d="M3.5 10h17M8 3v4M16 3v4"/>`)],
  ["mosko", "Mosko", icon(svg`<rect x="3.5" y="3.5" width="17" height="17" rx="3"/><path d="M7 9.5h3M14 9.5h3M9 15h6"/>`)],
  ["apps", "Apps", icon(svg`<rect x="3.5" y="3.5" width="7" height="7" rx="2"/><rect x="13.5" y="3.5" width="7" height="7" rx="2"/><rect x="3.5" y="13.5" width="7" height="7" rx="2"/><path d="M17 13.5v7M13.5 17h7"/>`)],
  ["settings", "Settings", icon(svg`<circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.7 1.7 0 0 0 .3 1.8l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.7 1.7 0 0 0-1.8-.3 1.7 1.7 0 0 0-1 1.5V21a2 2 0 1 1-4 0v-.1a1.7 1.7 0 0 0-1.1-1.5 1.7 1.7 0 0 0-1.8.3l-.1.1a2 2 0 1 1-2.8-2.8l.1-.1a1.7 1.7 0 0 0 .3-1.8 1.7 1.7 0 0 0-1.5-1H3a2 2 0 1 1 0-4h.1a1.7 1.7 0 0 0 1.5-1.1 1.7 1.7 0 0 0-.3-1.8l-.1-.1a2 2 0 1 1 2.8-2.8l.1.1a1.7 1.7 0 0 0 1.8.3H9a1.7 1.7 0 0 0 1-1.5V3a2 2 0 1 1 4 0v.1a1.7 1.7 0 0 0 1 1.5 1.7 1.7 0 0 0 1.8-.3l.1-.1a2 2 0 1 1 2.8 2.8l-.1.1a1.7 1.7 0 0 0-.3 1.8V9a1.7 1.7 0 0 0 1.5 1H21a2 2 0 1 1 0 4h-.1a1.7 1.7 0 0 0-1.5 1Z"/>`)],
];

class PixelhubApp extends LitElement {
  static properties = { st: { state: true }, status: { state: true }, sprites: { state: true }, modes: { state: true }, version: { state: true }, offline: { state: true }, gone: { state: true }, tab: { state: true }, typing: { state: true } };
  static styles = [base, css`
    header { position: sticky; top: 0; z-index: 5; background: color-mix(in srgb, var(--bg) 85%, transparent); backdrop-filter: blur(12px); }
    .bar { max-width: 1120px; margin: 0 auto; padding: 12px 16px; display: flex; align-items: center; gap: 16px; }
    .brand { font-family: var(--font-pixel); font-size: 20px; display: flex; align-items: center; gap: 10px; letter-spacing: .02em; }
    .brand img { width: 24px; height: 24px; }
    .link { display: flex; align-items: center; gap: 8px; font-size: 13px; color: var(--muted); white-space: nowrap; margin-left: auto; }
    button.link { border: 0; background: none; font: inherit; font-size: 13px; cursor: pointer; padding: 6px 8px; margin-right: -8px; border-radius: 8px; }
    button.link:hover { background: var(--chip); color: var(--ink); }
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
      .bottom { display: grid; grid-template-columns: repeat(8, minmax(0, 1fr)); position: fixed; left: 0; right: 0; bottom: 0; z-index: 6; gap: 0;
        padding: 6px 8px max(8px, env(safe-area-inset-bottom, 0px)); background: color-mix(in srgb, var(--bg) 90%, transparent); backdrop-filter: blur(12px); border-top: 1px solid var(--line); }
      .bottom button { flex-direction: column; gap: 3px; font-size: 10.5px; padding: 6px 0; min-width: 0; }
      .bottom button span { max-width: 100%; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
      .bottom.typing { display: none; }
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
    // iOS leaves fixed bars where the keyboard pushed them once it closes; hide the bar while typing and nudge the page after.
    const typingIn = (e) => {
      const el = e.composedPath()[0];
      return el?.tagName === "TEXTAREA" || el?.tagName === "SELECT" || (el?.tagName === "INPUT" && !["button", "checkbox", "radio", "range", "color", "file"].includes(el.type));
    };
    this.focusIn = (e) => { if (typingIn(e)) { clearTimeout(this.unfocus); this.typing = true; } };
    this.focusOut = (e) => {
      if (!typingIn(e)) return;
      clearTimeout(this.unfocus);
      this.unfocus = setTimeout(() => { this.typing = false; requestAnimationFrame(() => { scrollBy(0, 1); scrollBy(0, -1); }); }, 300);
    };
    this.leave = () => this.toasts.flush();
  }

  connectedCallback() {
    super.connectedCallback();
    const params = new URLSearchParams(location.search);
    const problem = params.get("spotify_error") ?? params.get("google_error");
    if (problem) {
      const text = params.has("spotify_error") ? t("Spotify did not connect: {reason}", { reason: problem }) : t("Google did not connect: {reason}", { reason: problem });
      this.toasts.show({ text });
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
    document.addEventListener("focusin", this.focusIn);
    document.addEventListener("focusout", this.focusOut);
    this.hash = () => { const id = location.hash.slice(1); if (TABS.some(([t]) => t === id)) this.tab = id; };
    addEventListener("hashchange", this.hash);
  }
  disconnectedCallback() {
    super.disconnectedCallback();
    this.timers.forEach(clearInterval);
    document.removeEventListener("visibilitychange", this.visible);
    removeEventListener("pagehide", this.leave);
    document.removeEventListener("focusin", this.focusIn);
    document.removeEventListener("focusout", this.focusOut);
    removeEventListener("hashchange", this.hash);
  }

  async pollStatus() {
    try { this.status = await api("GET", "status"); this.offline = false; } catch { this.offline = true; }
  }
  async retry() {
    try { this.status = await api("POST", "reconnect"); this.toasts.show({ text: t("Looking for the display") }); }
    catch (e) { this.toasts.show({ text: e.message }); }
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

  // The gallery stays mounted, hidden, so its first page is ready before it is opened.
  page(view) {
    const gallery = html`<ph-gallery .state=${view} ?hidden=${this.tab !== "gallery"}></ph-gallery>`;
    switch (this.tab) {
      case "gallery": return gallery;
      case "apps": return html`<ph-apps .state=${view}></ph-apps>${gallery}`;
      case "music": return html`<ph-music .state=${view} .status=${this.status} .styles=${this.modes}></ph-music>${gallery}`;
      case "settings": return html`<div class="tab two">${view ? html`<ph-settings .base=${view}></ph-settings>` : nothing}<ph-connection .status=${this.status} .state=${view}></ph-connection></div>${gallery}`;
      case "calendar": return html`<ph-calendar></ph-calendar>${gallery}`;
      case "mosko": return html`<ph-mosko .state=${view}></ph-mosko>${gallery}`;
      case "draw": return html`<ph-draw></ph-draw>${gallery}`;
      default: return html`<ph-display .status=${this.status} .state=${view} .sprites=${this.sprites} .styles=${this.modes} ?offline=${this.offline}></ph-display>${gallery}`;
    }
  }

  render() {
    const view = this.view;
    const [tone, linkText] = this.status ? linkState(this.status) : ["", t("Loading")];
    return html`
      <header><div class="bar">
        <div class="brand"><img src="favicon.svg" alt="">pixelhub</div>
        ${this.nav("top")}
        ${this.status && !this.offline && !this.status.connected && !this.status.released_until
          ? html`<button class="link retry" title=${t("Look for the display now instead of at the next try")} @click=${() => this.retry()}>${chain(tone, false)}<span>${linkText} · ${t("Retry now")}</span></button>`
          : html`<div class="link" title=${linkText}>${chain(this.offline ? "bad" : tone, !this.offline && !!this.status?.connected)}<span>${this.offline ? t("No answer") : linkText}</span></div>`}
      </div></header>
      <main>${this.page(view)}</main>
      <footer>pixelhub ${this.version ?? ""}</footer>
      ${this.nav(this.typing ? "bottom typing" : "bottom")}
      ${this.toasts.view()}`;
  }
}
customElements.define("pixelhub-app", PixelhubApp);
