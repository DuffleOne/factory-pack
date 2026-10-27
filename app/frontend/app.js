// The window. Everything it does goes through the Go service; this keeps
// the settings in the fields, the ticks in the list, and says how a pack is
// coming along.

import { Call, Events } from "/wails/runtime.js";

const go = (name, ...args) => Call.ByName(`main.Service.${name}`, ...args);
const $ = (id) => document.getElementById(id);

let state = null;
// The mods ticked, by name. Null until the game has been found once, and
// then whatever is ticked in the list.
let ticked = null;

function config() {
  const c = { ...(state?.config ?? {}) };
  for (const el of document.querySelectorAll("[data-key]")) {
    const key = el.dataset.key;
    if (el.type === "checkbox") c[key] = el.checked;
    else c[key] = el.value.trim() || undefined;
  }
  const kind = document.querySelector("input[name=settings]:checked")?.value;
  c.settings = kind === "file" ? $("settings").value.trim() || undefined : kind;
  // Ticked by hand, the list is the mods; left as the game has them, it's
  // what the game has switched on each time.
  c.mods = ticked && !same(ticked, gameOn()) ? [...ticked].sort() : undefined;
  return c;
}

function gameOn() {
  return new Set((state?.mods ?? []).filter((m) => m.enabled).map((m) => m.name));
}

function same(a, b) {
  if (a.size !== b.size) return false;
  for (const x of a) if (!b.has(x)) return false;
  return true;
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
  $("settings-file").hidden = kind !== "file";
}

function show(st) {
  state = st;
  $("config-path").textContent = st.configPath;
  const g = st.game;
  if (g) {
    const kind = g.headless ? ", the headless build: it has no pictures" : "";
    $("game").textContent = `Factorio ${g.version || ""}${kind}, at ${g.binary}`;
    $("game").classList.remove("bad");
  } else {
    $("game").textContent = st.problem || "no copy of Factorio found.";
    $("game").classList.add("bad");
    $("where").open = true;
  }
  if (st.config.mods?.length) ticked = new Set(st.config.mods);
  else ticked = gameOn();
  list();
  problem(g ? st.problem : "");
}

function list() {
  const ul = $("mods");
  const find = $("filter").value.trim().toLowerCase();
  ul.replaceChildren();
  for (const m of state?.mods ?? []) {
    const words = `${m.name} ${m.title ?? ""}`.toLowerCase();
    if (find && !words.includes(find)) continue;
    const li = document.createElement("li");
    const label = document.createElement("label");
    const box = document.createElement("input");
    box.type = "checkbox";
    box.checked = m.name === "base" || ticked.has(m.name);
    box.disabled = m.name === "base" || running;
    box.addEventListener("change", () => {
      if (box.checked) ticked.add(m.name);
      else ticked.delete(m.name);
      count();
    });
    const name = document.createElement("span");
    name.className = "name";
    name.textContent = m.title && m.title !== m.name ? m.title : m.name;
    const aside = document.createElement("span");
    aside.className = "muted";
    aside.textContent = `${m.title && m.title !== m.name ? `${m.name} ` : ""}${m.version}${m.builtin ? ", comes with the game" : ""}`;
    label.append(box, name, aside);
    li.append(label);
    ul.append(li);
  }
  count();
}

function count() {
  const all = state?.mods?.length ?? 0;
  const n = (state?.mods ?? []).filter((m) => m.name === "base" || ticked.has(m.name)).length;
  $("count").textContent = all ? `${n} of ${all} ticked` : "";
}

function problem(text) {
  $("problem").textContent = text || "";
  $("problem").hidden = !text;
}

let running = false;

function busy(on) {
  running = on;
  $("make").hidden = on;
  $("stop").hidden = !on;
  for (const el of document.querySelectorAll("input, button:not(#stop)")) el.disabled = on;
  if (!on) list();
}

function step(s) {
  const li = document.createElement("li");
  li.textContent = s.of ? `${s.stage} (${s.done} of ${s.of})` : s.stage;
  const last = $("steps").lastElementChild;
  // Pictures going up, or the planner still building, is one line that
  // keeps count rather than a new line each time.
  if (last && last.dataset.stage === s.stage) last.replaceWith(li);
  else $("steps").append(li);
  li.dataset.stage = s.stage;
}

async function make() {
  problem("");
  $("done").hidden = true;
  $("steps").replaceChildren();
  busy(true);
  try {
    const d = await go("Make", config());
    $("code").textContent = d.pretty;
    $("code").dataset.code = d.code;
    $("open").dataset.link = d.link;
    const kept = d.kept ? ` a copy of all of it is in ${d.kept}.` : "";
    const missing = d.missing ? ` ${d.missing} pictures it asked for couldn't be found, so those go without.` : "";
    $("sent").textContent = `sent ${size(d.size)} of the game's data and ${d.pictures} pictures.${missing}${kept}`;
    $("done").hidden = false;
    $("done").scrollIntoView({ behavior: "smooth" });
  } catch (err) {
    problem(String(err?.message ?? err));
  } finally {
    busy(false);
  }
}

function size(n) {
  if (n >= 1 << 20) return `${(n / (1 << 20)).toFixed(1)} MB`;
  return `${Math.round(n / 1024)} KB`;
}

async function look() {
  show(await go("Look", { ...config(), mods: undefined }));
}

Events.On("progress", (e) => step(e.data));

$("make").addEventListener("click", make);
$("stop").addEventListener("click", () => go("Stop"));
$("look").addEventListener("click", look);
$("filter").addEventListener("input", list);
$("copy").addEventListener("click", async () => {
  if (await go("Copy", $("code").textContent)) {
    $("copy").textContent = "copied";
    setTimeout(() => ($("copy").textContent = "copy it"), 1500);
  }
});
$("open").addEventListener("click", () => go("Open", $("open").dataset.link));

for (const b of document.querySelectorAll("[data-set]")) {
  b.addEventListener("click", () => {
    const set = b.dataset.set;
    if (set === "game") ticked = gameOn();
    else if (set === "all") ticked = new Set(state.mods.map((m) => m.name));
    else ticked = new Set(["base"]);
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
    problem(missing.length ? `that list has mods your mods folder doesn't: ${missing.join(", ")}. get them from the mod portal, or the pack goes without them.` : "");
  } catch (err) {
    problem(String(err?.message ?? err));
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
  });
}

for (const r of document.querySelectorAll("input[name=settings]")) {
  r.addEventListener("change", () => {
    $("settings-file").hidden = r.value !== "file" || !r.checked;
  });
}

const start = await go("Load");
fill(start.config);
show(start);
