import "./style.css";

const baseUrlInput = document.querySelector("#baseUrl");
const tabGlobal = document.querySelector("#tabGlobal");
const tabRoom = document.querySelector("#tabRoom");
const roomPicker = document.querySelector("#roomPicker");
const roomSelect = document.querySelector("#roomSelect");
const aliasesInput = document.querySelector("#aliases");
const followRow = document.querySelector("#followRow");
const enabledFollow = document.querySelector("#enabledFollow");
const enabledInput = document.querySelector("#enabled");
const cooldownInput = document.querySelector("#cooldown");
const thinkingFollowRow = document.querySelector("#thinkingFollowRow");
const thinkingFollow = document.querySelector("#thinkingFollow");
const thinkingInput = document.querySelector("#thinking");
const aliasHint = document.querySelector("#aliasHint");
const enabledHint = document.querySelector("#enabledHint");
const cooldownHint = document.querySelector("#cooldownHint");
const thinkingHint = document.querySelector("#thinkingHint");
const contextHoursInput = document.querySelector("#contextHours");
const contextLimitInput = document.querySelector("#contextLimit");
const contextHint = document.querySelector("#contextHint");
const statusNode = document.querySelector("#status");
const saveButton = document.querySelector("#save");
const reloadButton = document.querySelector("#reload");
const form = document.querySelector("#form");

let scope = "global";
let busy = false;
let globalProactiveEnabled = true;
let globalThinkingEnabled = false;

function baseUrl() {
  return baseUrlInput.value.trim().replace(/\/$/, "");
}

function setStatus(text, isError = false) {
  statusNode.textContent = text;
  statusNode.classList.toggle("error", isError);
}

function setBusy(next) {
  busy = next;
  saveButton.disabled = next;
  reloadButton.disabled = next;
  saveButton.textContent = next ? "保存中…" : "保存";
}

async function request(path, options = {}) {
  const response = await fetch(`${baseUrl()}${path}`, {
    headers: { "Content-Type": "application/json" },
    ...options,
  });
  const body = await response.json().catch(() => ({}));
  if (!response.ok || (body.code && body.code !== 200)) {
    throw new Error(body.message || `请求失败 ${response.status}`);
  }
  return body;
}

function aliasesFromText(text) {
  return text
    .split(/\n+/)
    .map((item) => item.trim())
    .filter(Boolean);
}

function optionalInt(text, min, max, message) {
  const value = text.trim();
  if (value === "") {
    return null;
  }
  const number = Number(value);
  if (!Number.isInteger(number) || number < min || number > max) {
    setStatus(message, true);
    return false;
  }
  return number;
}

function applyScope() {
  const isRoom = scope === "room";
  tabGlobal.classList.toggle("active", !isRoom);
  tabRoom.classList.toggle("active", isRoom);
  tabGlobal.setAttribute("aria-pressed", String(!isRoom));
  tabRoom.setAttribute("aria-pressed", String(isRoom));
  roomPicker.hidden = !isRoom;
  followRow.hidden = !isRoom;
  thinkingFollowRow.hidden = !isRoom;
  aliasHint.textContent = isRoom
    ? "留空表示这个群不额外增加别名，仍会加上全局里填写的别名。另外还会响应机器人的微信昵称和群昵称。"
    : "留空表示不额外增加别名，只响应机器人的微信昵称和群昵称。";
  cooldownHint.textContent = isRoom
    ? "留空时用全局的秒数。全局也留空时用 180 秒。"
    : "留空时用 180 秒。可填 30 到 3600。";
  contextHint.textContent = isRoom
    ? "留空时用全局的小时数和条数。全局也留空时用最近 6 小时、最多 300 条。"
    : "留空时用最近 6 小时、最多 300 条。只影响群聊回复时带上的流水，不影响记忆提取。";
}

function fillForm(settings) {
  const aliases = Array.isArray(settings?.chat_ai_name_aliases) ? settings.chat_ai_name_aliases : [];
  aliasesInput.value = aliases.join("\n");
  const inherited = settings?.chat_ai_proactive_enabled == null;
  const enabled = inherited ? globalProactiveEnabled : Boolean(settings.chat_ai_proactive_enabled);
  enabledFollow.checked = scope === "room" && inherited;
  enabledInput.checked = enabled;
  enabledInput.disabled = enabledFollow.checked;
  cooldownInput.value = settings?.chat_ai_proactive_cooldown ?? "";
  const thinkingInherited = settings?.chat_ai_thinking_enabled == null;
  const thinkingOn = thinkingInherited ? globalThinkingEnabled : Boolean(settings.chat_ai_thinking_enabled);
  thinkingFollow.checked = scope === "room" && thinkingInherited;
  thinkingInput.checked = thinkingOn;
  thinkingInput.disabled = thinkingFollow.checked;
  if (scope === "room" && thinkingFollow.checked) {
    thinkingHint.textContent = "这个群沿用全局。全局未填写时关闭。思考内容不会发到群里。";
  } else if (thinkingInherited) {
    thinkingHint.textContent = "当前未填写，实际关闭。打开后思考走单独通道，回复会稍慢。";
  } else if (thinkingOn) {
    thinkingHint.textContent = "当前已开启。思考内容不会发到群里，回复会稍慢。";
  } else {
    thinkingHint.textContent = "当前已关闭。";
  }
  contextHoursInput.value = settings?.chat_context_hours ?? "";
  contextLimitInput.value = settings?.chat_context_message_limit ?? "";
  if (scope === "room" && enabledFollow.checked) {
    enabledHint.textContent = "这个群沿用全局。全局未填写时默认开启。";
  } else if (inherited) {
    enabledHint.textContent = "当前未填写，实际默认开启。";
  } else {
    enabledHint.textContent = enabled ? "当前已开启。" : "当前已关闭。";
  }
}

function rememberGlobalProactive(settings) {
  globalProactiveEnabled = settings?.chat_ai_proactive_enabled == null ? true : Boolean(settings.chat_ai_proactive_enabled);
}

function rememberGlobalThinking(settings) {
  globalThinkingEnabled = settings?.chat_ai_thinking_enabled == null ? false : Boolean(settings.chat_ai_thinking_enabled);
}

async function loadGlobal() {
  const body = await request("/api/v1/robot/global-settings");
  rememberGlobalProactive(body.data || {});
  rememberGlobalThinking(body.data || {});
  fillForm(body.data || {});
  setStatus("已读取全局配置");
}

async function loadRooms() {
  const selected = roomSelect.value;
  const body = await request("/api/v1/robot/contacts?type=chat_room&page_index=1&page_size=200");
  const items = body.data?.items || [];
  roomSelect.replaceChildren();
  for (const room of items) {
    const option = document.createElement("option");
    option.value = room.wechat_id;
    const name = room.remark || room.nickname || room.wechat_id;
    option.textContent = name === room.wechat_id ? room.wechat_id : `${name}（${room.wechat_id}）`;
    roomSelect.appendChild(option);
  }
  if (selected && [...roomSelect.options].some((option) => option.value === selected)) {
    roomSelect.value = selected;
  }
  if (!items.length) {
    setStatus("没有读到群", true);
  }
}

async function loadRoom() {
  if (!roomSelect.value) {
    aliasesInput.value = "";
    setStatus("先选择一个群", true);
    return;
  }
  const body = await request(`/api/v1/robot/chat-room-settings?chat_room_id=${encodeURIComponent(roomSelect.value)}`);
  fillForm(body.data || {});
  const roomName = roomSelect.selectedOptions[0]?.textContent || roomSelect.value;
  setStatus(`已读取 ${roomName}`);
}

async function save(event) {
  event.preventDefault();
  if (busy) {
    return;
  }
  const cooldownText = cooldownInput.value.trim();
  const cooldown = cooldownText === "" ? null : Number(cooldownText);
  if (cooldown !== null && (!Number.isInteger(cooldown) || cooldown < 30 || cooldown > 3600)) {
    setStatus("冷却需要是 30 到 3600 之间的整数", true);
    return;
  }
  const contextHours = optionalInt(contextHoursInput.value, 1, 168, "上下文小时数需要是 1 到 168 之间的整数");
  if (contextHours === false) {
    return;
  }
  const contextLimit = optionalInt(contextLimitInput.value, 1, 2000, "上下文条数需要是 1 到 2000 之间的整数");
  if (contextLimit === false) {
    return;
  }
  const payload = {
    chat_ai_name_aliases: aliasesFromText(aliasesInput.value),
    proactive_enabled_set: true,
    cooldown_set: true,
    chat_ai_proactive_cooldown: cooldown,
    thinking_set: true,
    context_hours_set: true,
    chat_context_hours: contextHours,
    context_limit_set: true,
    chat_context_message_limit: contextLimit,
  };
  if (scope === "room" && thinkingFollow.checked) {
    payload.chat_ai_thinking_enabled = null;
  } else {
    payload.chat_ai_thinking_enabled = thinkingInput.checked;
  }
  if (scope === "room" && enabledFollow.checked) {
    payload.chat_ai_proactive_enabled = null;
  } else {
    payload.chat_ai_proactive_enabled = enabledInput.checked;
  }
  if (scope === "room") {
    payload.chat_room_id = roomSelect.value;
    if (!payload.chat_room_id) {
      setStatus("先选择一个群", true);
      return;
    }
  }
  setBusy(true);
  try {
    await request("/api/v1/robot/runtime-config", {
      method: "POST",
      body: JSON.stringify(payload),
    });
    if (scope === "global") {
      await loadGlobal();
    } else {
      await loadRoom();
    }
    setStatus("已保存");
  } catch (error) {
    setStatus(error.message, true);
  } finally {
    setBusy(false);
  }
}

async function showGlobal() {
  scope = "global";
  applyScope();
  try {
    await loadGlobal();
  } catch (error) {
    setStatus(error.message, true);
  }
}

async function showRoom() {
  scope = "room";
  applyScope();
  try {
    if (!roomSelect.options.length) {
      await loadRooms();
    }
    await loadRoom();
  } catch (error) {
    setStatus(error.message, true);
  }
}

tabGlobal.addEventListener("click", () => {
  if (!busy) {
    showGlobal();
  }
});
tabRoom.addEventListener("click", () => {
  if (!busy) {
    showRoom();
  }
});
document.querySelector("#reloadRooms").addEventListener("click", async () => {
  if (busy) {
    return;
  }
  try {
    await loadRooms();
    await loadRoom();
  } catch (error) {
    setStatus(error.message, true);
  }
});
roomSelect.addEventListener("change", () => loadRoom().catch((error) => setStatus(error.message, true)));
reloadButton.addEventListener("click", () => {
  if (busy) {
    return;
  }
  const task = scope === "global" ? loadGlobal() : loadRoom();
  task.catch((error) => setStatus(error.message, true));
});
thinkingFollow.addEventListener("change", () => {
  thinkingInput.disabled = thinkingFollow.checked;
  if (thinkingFollow.checked) {
    thinkingInput.checked = globalThinkingEnabled;
    thinkingHint.textContent = "这个群沿用全局。全局未填写时关闭。思考内容不会发到群里。";
  } else {
    thinkingHint.textContent = thinkingInput.checked ? "当前已开启。思考内容不会发到群里，回复会稍慢。" : "当前已关闭。";
  }
});
enabledFollow.addEventListener("change", () => {
  enabledInput.disabled = enabledFollow.checked;
  if (enabledFollow.checked) {
    enabledInput.checked = globalProactiveEnabled;
    enabledHint.textContent = "这个群沿用全局。全局未填写时默认开启。";
  } else {
    enabledHint.textContent = enabledInput.checked ? "当前已开启。" : "当前已关闭。";
  }
});
form.addEventListener("submit", save);

applyScope();
showGlobal();
