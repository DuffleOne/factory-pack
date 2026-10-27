// The window. Everything it does goes through the Go service; this keeps
// the settings in the fields and the ticks in the list, and shows a pack
// being made and what it came to.

import { Call, Events } from "/wails/runtime.js";

import { drawIcons, icon } from "./icons.js";

const go = (name, ...args) => Call.ByName(`main.Service.${name}`, ...args);
const $ = (id) => document.getElementById(id);
const number = new Intl.NumberFormat("en-GB");

let state = null;
// The mods ticked, by name.
let ticked = new Set();
let running = false;
// The steps of the pack being made, or last made: what each said, and when
// it started and ended.
let steps = [];

// settings

function config() {
  const c = { ...(state?.config ?? {}) };
  for (const el of document.querySelectorAll("[data-key]")) {
    const key = el.dataset.key;
    if (el.type === "checkbox") c[key] = el.checked;
    else c[key] = el.value.trim() || undefined;
  }
  const kind = document.querySelector("input[name=settings]:checked")?.value;
  c.settings = kind === "file" ? $("settings").value.trim() || undefined : kind;
  // Left as the game has them, the mods are whatever the game has switched
  // on at the time; ticked by hand, they're the list.
  c.mods = same(ticked, gameOn()) ? undefined : [...ticked].sort();
  return c;
}

function fill(c) {
  for (const el of document.querySelectorAll("[data-key]")) {
    const v = c[el.dataset.key];
    if (el.type === "checkbox") el.checked = !!v;
    else el.value = v ?? "";
  }
  const s = c.settings ?? "mine";
  const kind = s === "mine" || s === "defaults" ? s : "file";
  document.querySelector(`input[name=settings][value=${kind}]`).checked = true;
  $("settings").value = kind === "file" ? s : "";
  showSettingsFile();
  sayMore();
}

function showSettingsFile() {
  const file = document.querySelector("input[name=settings]:checked")?.value === "file";
  for (const el of document.querySelectorAll(".settings-file")) el.hidden = !file;
}

function host(c = config()) {
  try {
    return new URL(c.server || "https://factory.duffle.one").host;
  } catch {
    return c.server;
  }
}

function settingsSaid(c = config()) {
  if (!c.settings || c.settings === "mine") return "your mod settings";
  if (c.settings === "defaults") return "every mod's defaults";
  return "mod settings from a file";
}

// What "more" holds, said in a line beside it.
function sayMore() {
  const c = config();
  const names = c.locale ? `names in ${c.locale}` : "names in the game's language";
  const kept = c.keep ? `, a copy kept in ${short(c.keep)}` : "";
  $("more-says").textContent = `${settingsSaid(c)}, ${names}, sent to ${host(c)}${kept}`;
}

// A path as people read it: home as ~, and the middle of a long one left
// out.
function short(path) {
  if (!path) return "";
  const home = state?.home;
  let p = home && path.startsWith(home) ? `~${path.slice(home.length)}` : path;
  const sep = p.includes("\\") && !p.includes("/") ? "\\" : "/";
  const parts = p.split(sep);
  if (parts.length > 6) p = [...parts.slice(0, 4), "…", ...parts.slice(-2)].join(sep);
  return p;
}

// the game and its mods

function gameOn() {
  return new Set((state?.mods ?? []).filter((m) => m.enabled).map((m) => m.name));
}

function same(a, b) {
  if (a.size !== b.size) return false;
  for (const x of a) if (!b.has(x)) return false;
  return true;
}

function show(st) {
  state = st;
  document.documentElement.dataset.os = st.os ?? "";
  $("config-path").textContent = short(st.configPath);
  const g = st.game;
  $("game-status").className = `status ${g ? "" : "down"}`;
  if (g) {
    $("game-name").textContent = `Factorio ${g.version || ""}`.trim();
    $("game-where").textContent = g.headless
      ? `${short(st.where)}, the headless build: it has no pictures`
      : short(st.where);
  } else {
    $("game-name").textContent = "Factorio isn't where it usually is";
    $("game-where").textContent = st.problem || "say where it is under more";
  }
  ticked = new Set(st.config.mods?.length ? st.config.mods : st.picked);
  list();
}

function list() {
  const box = $("mods");
  const find = $("filter").value.trim().toLowerCase();
  const mods = (state?.mods ?? []).filter(
    (m) => !find || `${m.name} ${m.title ?? ""}`.toLowerCase().includes(find),
  );
  box.replaceChildren();
  const kinds = [
    ["comes with the game", mods.filter((m) => m.builtin)],
    ["from the mod portal", mods.filter((m) => !m.builtin)],
  ];
  for (const [said, some] of kinds) {
    if (!some.length) continue;
    const head = document.createElement("p");
    head.className = "kind";
    head.textContent = said;
    box.append(head);
    for (const m of some) box.append(row(m));
  }
  if (!mods.length) {
    const none = document.createElement("p");
    none.className = "none";
    none.textContent = state?.mods?.length ? "no mod by that name" : "no mods yet: the game hasn't been found";
    box.append(none);
  }
  count();
}

function row(m) {
  const label = document.createElement("label");
  label.className = "mod check";
  const box = document.createElement("input");
  box.type = "checkbox";
  box.checked = m.name === "base" || ticked.has(m.name);
  // base is always loaded, as the game always loads it.
  box.disabled = m.name === "base" || running;
  box.addEventListener("change", () => {
    if (box.checked) ticked.add(m.name);
    else ticked.delete(m.name);
    count();
  });
  const name = document.createElement("span");
  name.className = "name";
  name.textContent = m.title || m.name;
  const what = document.createElement("span");
  what.className = "what";
  what.textContent = `${m.name} ${m.version}`;
  label.append(box, name, what);
  return label;
}

// A line under the list, about the last thing done to it.
function note(text) {
  $("mods-note").textContent = text;
  $("mods-note").hidden = !text;
}

function count() {
  const all = state?.mods ?? [];
  const n = all.filter((m) => m.name === "base" || ticked.has(m.name)).length;
  $("count").textContent = all.length ? `${n} of ${all.length} ticked` : "";
  // Which of the three the ticks are, if any.
  const as = same(ticked, gameOn())
    ? "game"
    : n === all.length
      ? "all"
      : n === 1
        ? "none"
        : "";
  for (const b of document.querySelectorAll("[data-set]")) b.setAttribute("aria-pressed", String(b.dataset.set === as));
}

// the three views

function view(which) {
  for (const v of ["setup", "making", "done"]) $(v).hidden = v !== which;
  const bar = which === "making" && $("failed").hidden === false ? "failed" : which;
  for (const b of ["setup", "making", "failed", "done"]) $(`bar-${b}`).hidden = b !== bar;
  $("page").scrollTop = 0;
}

function problem(text) {
  $("failed-why").textContent = text;
  $("failed").hidden = !text;
}

// steps

function step(s) {
  if (s.key === "note") {
    $("note").textContent = s.stage;
    $("note").hidden = false;
    return;
  }
  const now = performance.now();
  const last = steps.at(-1);
  if (last && last.key === s.key) Object.assign(last, s);
  else {
    if (last) last.ended = now;
    steps.push({ ...s, started: now });
  }
  drawSteps($("steps"), true);
}

function drawSteps(ol, live) {
  ol.replaceChildren();
  steps.forEach((s, i) => {
    const li = document.createElement("li");
    const now = live && i === steps.length - 1 && !s.ended;
    li.className = `step${now ? " now" : ""}${s.failed ? " bad" : ""}`;
    const mark = document.createElement("span");
    mark.className = "mark-icon";
    mark.innerHTML = icon(s.failed ? "cross" : now ? "loader" : "check");
    const text = document.createElement("span");
    text.textContent = s.stage;
    const cnt = document.createElement("span");
    cnt.className = "count";
    cnt.textContent = counted(s, live);
    li.append(mark, text, cnt);
    if (now && s.of > 0 && (s.key === "export" || s.key === "pictures")) {
      const meter = document.createElement("div");
      meter.className = "meter";
      const fillBar = document.createElement("span");
      fillBar.style.width = `${Math.min(100, (100 * s.done) / s.of)}%`;
      meter.append(fillBar);
      li.append(meter);
    }
    if (!live && s.ended) cnt.textContent = took(s.ended - s.started);
    ol.append(li);
  });
}

function counted(s, live) {
  if (!live || !s.of) return s.of && ["dump", "names", "fluids"].includes(s.key) ? `${s.done} of ${s.of}` : "";
  if (s.bytes) return `${size(s.done)} of ${size(s.of)}`;
  return `${number.format(s.done)} of ${number.format(s.of)}`;
}

function size(n) {
  if (n >= 1 << 20) return `${(n / (1 << 20)).toFixed(1)} MB`;
  return `${Math.max(1, Math.round(n / 1024))} KB`;
}

function took(ms) {
  const s = Math.round(ms / 1000);
  if (s < 60) return `${s} s`;
  return `${Math.floor(s / 60)} min ${s % 60} s`;
}

// making a pack

async function make() {
  if (running) return;
  const c = config();
  steps = [];
  problem("");
  $("note").hidden = true;
  $("steps").replaceChildren();
  const n = (state?.mods ?? []).filter((m) => m.name === "base" || ticked.has(m.name)).length;
  const version = state?.game?.version ? ` on Factorio ${state.game.version}` : "";
  $("making-what").textContent = `${n} mods${version}, ${settingsSaid(c)}, going to ${host(c)}`;
  running = true;
  view("making");
  const started = performance.now();
  try {
    const d = await go("Make", c);
    const last = steps.at(-1);
    if (last) last.ended = performance.now();
    done(d, performance.now() - started);
  } catch (err) {
    const why = String(err?.message ?? err);
    if (why === "stopped") {
      view("setup");
      return;
    }
    const last = steps.at(-1);
    if (last) {
      last.failed = true;
      last.ended = performance.now();
    }
    drawSteps($("steps"), true);
    problem(why);
    view("making");
  } finally {
    running = false;
    list();
  }
}

function done(d, ms) {
  $("code").textContent = d.pretty;
  $("open").dataset.link = d.link;
  const missing = d.missing ? ` ${number.format(d.missing)} it asked for couldn't be found, so those go without.` : "";
  const kept = d.kept ? ` a copy of all of it is in ${short(d.kept)}.` : "";
  $("sent").textContent = `sent ${size(d.size)} of the game's data and ${number.format(d.pictures)} pictures.${missing}${kept}`;
  $("kept-until").textContent = d.expires ? `kept until ${until(d.expires)}` : "";
  drawSteps($("went"), false);
  $("went-says").textContent = `${steps.length} steps, ${took(ms)}`;
  view("done");
}

function until(iso) {
  const t = new Date(iso);
  if (Number.isNaN(t.getTime())) return iso;
  const day = new Intl.DateTimeFormat("en-GB", { day: "numeric", month: "short" }).format(t);
  const time = new Intl.DateTimeFormat("en-GB", { hour: "2-digit", minute: "2-digit", hour12: false }).format(t);
  return `${day}, ${time}`;
}

async function look() {
  show(await go("Look", { ...config(), mods: undefined }));
  sayMore();
}

// wiring

drawIcons();

Events.On("progress", (e) => step(e.data));

$("make").addEventListener("click", make);
$("again").addEventListener("click", make);
$("stop").addEventListener("click", () => go("Stop"));
$("look").addEventListener("click", look);
$("filter").addEventListener("input", list);
for (const b of document.querySelectorAll("[data-back]")) b.addEventListener("click", () => view("setup"));

$("elsewhere").addEventListener("click", () => {
  $("more").open = true;
  $("where").scrollIntoView({ behavior: "smooth", block: "start" });
  $("binary").focus({ preventScroll: true });
});

$("copy").addEventListener("click", async () => {
  if (await go("Copy", $("code").textContent)) {
    const label = $("copy").lastChild;
    label.textContent = " copied";
    setTimeout(() => (label.textContent = "copy it"), 1500);
  }
});

$("open").addEventListener("click", () => go("Open", $("open").dataset.link));

for (const b of document.querySelectorAll("[data-set]")) {
  b.addEventListener("click", () => {
    const set = b.dataset.set;
    if (set === "game") ticked = gameOn();
    else if (set === "all") ticked = new Set(state.mods.map((m) => m.name));
    else ticked = new Set(["base"]);
    note("");
    list();
  });
}

$("from-list").addEventListener("click", async () => {
  const path = await go("Browse", "modlist", "");
  if (!path) return;
  try {
    const names = await go("ModList", path);
    const have = new Set(state.mods.map((m) => m.name));
    const missing = names.filter((n) => !have.has(n));
    ticked = new Set(["base", ...names.filter((n) => have.has(n))]);
    list();
    note(
      missing.length
        ? `that list has mods your mods folder doesn't: ${missing.join(", ")}. get them from the mod portal, or the pack goes without them.`
        : `ticked the ${names.length} mods ${short(path)} has on.`,
    );
  } catch (err) {
    note(String(err?.message ?? err));
  }
});

for (const b of document.querySelectorAll("[data-browse]")) {
  b.addEventListener("click", async () => {
    const what = b.dataset.browse;
    const field = what === "game" ? $("binary") : $(what);
    const got = await go("Browse", what, "");
    if (!got) return;
    field.value = got;
    if (["game", "data", "user"].includes(what)) await look();
    else sayMore();
  });
}

for (const r of document.querySelectorAll("input[name=settings]")) {
  r.addEventListener("change", () => {
    showSettingsFile();
    sayMore();
  });
}

for (const el of document.querySelectorAll(".form input")) el.addEventListener("input", sayMore);

const start = await go("Load");
show(start);
fill(start.config);
