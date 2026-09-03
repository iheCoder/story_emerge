const views = Object.fromEntries(
  ["create", "genesis", "library", "reader"].map((name) => [name, document.querySelector(`#${name}-view`)]),
);

const elements = {
  form: document.querySelector("#creation-form"),
  idea: document.querySelector("#idea"),
  ideaCount: document.querySelector("#idea-count"),
  formError: document.querySelector("#form-error"),
  createButton: document.querySelector("#create-button"),
  quietStatus: document.querySelector("#quiet-status"),
  chapterOrbit: document.querySelector("#chapter-orbit"),
  growthActions: document.querySelector("#growth-actions"),
  spineList: document.querySelector("#spine-list"),
};

const state = {
  storyId: "",
  story: null,
  library: [],
  currentChapter: 0,
  pollTimer: null,
  toastTimer: null,
  coreFrame: null,
  // POST 返回之前就锁住入口，防止快速连点；服务端还会用任务锁拒绝其他页面的重复启动。
  growthPending: false,
};

bindEvents();
updateIdeaState();
startCoreAnimation();
routeFromHash();

function bindEvents() {
  elements.form.addEventListener("submit", createStory);
  elements.idea.addEventListener("input", updateIdeaState);
  document.querySelector("#brand-button").addEventListener("click", () => navigate("create"));
  document.querySelector("#empty-create-button").addEventListener("click", () => navigate("create"));
  document.querySelector("#back-to-story").addEventListener("click", () => navigate(`story/${state.storyId}`));
  document.querySelector("#next-chapter-button").addEventListener("click", generateNextChapter);
  document.querySelector("#complete-story-button").addEventListener("click", completeStory);
  document.querySelector("#reader-generate-next").addEventListener("click", generateNextChapter);
  document.querySelector("#reader-previous").addEventListener("click", () => openChapter(state.currentChapter - 1));
  document.querySelector("#reader-next").addEventListener("click", () => openChapter(state.currentChapter + 1));
  document.querySelectorAll("[data-route]").forEach((button) => {
    button.addEventListener("click", () => navigate(button.dataset.route));
  });
  window.addEventListener("hashchange", routeFromHash);
  window.addEventListener("resize", drawVisibleCores, { passive: true });
}

async function routeFromHash() {
  const route = decodeURIComponent(location.hash.replace(/^#\/?/, "") || "create");
  const [name, id, chapter] = route.split("/");
  stopPolling();
  try {
    if (name === "library") return showLibrary();
    if (name === "story" && id) return showStory(id);
    if (name === "read" && id && chapter) return showReaderRoute(id, Number(chapter));
    showView("create");
  } catch (error) {
    showToast(error.message);
    showView("library");
    await refreshLibrary();
  }
}

function navigate(route) {
  const next = `#/${route}`;
  if (location.hash === next) {
    routeFromHash();
    return;
  }
  location.hash = next;
}

async function showLibrary() {
  showView("library");
  await refreshLibrary();
}

async function showStory(id) {
  await loadStory(id);
  showView("genesis");
  if (state.story.running) startPolling();
}

async function showReaderRoute(id, chapter) {
  if (state.storyId !== id || !state.story) await loadStory(id);
  await openChapter(chapter, false);
  if (state.story.running) startPolling();
}

async function createStory(event) {
  event.preventDefault();
  const request = creationRequest();
  elements.formError.textContent = "";
  if (!request.idea) {
    elements.formError.textContent = "先写下一点你真正想看的故事。";
    elements.idea.focus();
    return;
  }

  setCreateBusy(true);
  try {
    const created = await api("/api/stories", { method: "POST", body: request });
    state.storyId = created.id;
    navigate(`story/${created.id}`);
  } catch (error) {
    elements.formError.textContent = error.message;
  } finally {
    setCreateBusy(false);
  }
}

function creationRequest() {
  return {
    // 原始创作授权完整传递；是否为空由服务端校验，不改写用户文本。
    idea: elements.idea.value,
    length: new FormData(elements.form).get("length"),
  };
}

function setCreateBusy(busy) {
  elements.createButton.disabled = busy;
  elements.createButton.querySelector("span:first-child").textContent = busy ? "故事正在醒来" : "开始创造";
}

async function refreshLibrary() {
  state.library = await api("/api/stories");
  renderLibrary(state.library);
  if (views.library.classList.contains("is-active")) {
    elements.quietStatus.textContent = `${state.library.length} 个故事`;
  }
}

function renderLibrary(stories) {
  const list = document.querySelector("#library-list");
  const empty = document.querySelector("#library-empty");
  list.replaceChildren();
  empty.hidden = stories.length > 0;
  list.hidden = stories.length === 0;
  stories.forEach((story, index) => list.appendChild(createBook(story, index)));
}

function createBook(story, index) {
  const button = document.createElement("button");
  button.type = "button";
  button.className = "book-entry";
  button.setAttribute("aria-label", `打开《${story.title || "正在命名"}》`);
  button.append(createBookCover(story, index));
  const title = document.createElement("strong");
  title.textContent = story.title || "正在命名";
  const meta = document.createElement("span");
  meta.textContent = `${lengthLabel(story.length)} · 已写 ${story.current_chapter} 章 · ${storyStatus(story)}`;
  button.append(title, meta);
  button.addEventListener("click", () => openLibraryStory(story));
  return button;
}

function createBookCover(story, index) {
  const cover = document.createElement("span");
  cover.className = "book-cover";
  const canvas = document.createElement("canvas");
  const progress = document.createElement("span");
  progress.className = "book-progress";
  progress.textContent = story.current_chapter ? `CH.${String(story.current_chapter).padStart(2, "0")}` : "初生";
  cover.append(canvas, progress);
  requestAnimationFrame(() => drawBookCover(canvas, story, index));
  return cover;
}

function openLibraryStory(story) {
  if (story.current_chapter < 1) {
    navigate(`story/${story.id}`);
    return;
  }
  const remembered = readPosition(story.id);
  const chapter = Math.min(Math.max(remembered || 1, 1), story.current_chapter);
  navigate(`read/${story.id}/${chapter}`);
}

async function loadStory(id) {
  state.storyId = id;
  state.story = await api(`/api/stories/${encodeURIComponent(id)}`);
  renderStory(state.story);
}

function startPolling() {
  stopPolling();
  refreshStory();
  state.pollTimer = window.setInterval(refreshStory, 1600);
}

function stopPolling() {
  if (!state.pollTimer) return;
  window.clearInterval(state.pollTimer);
  state.pollTimer = null;
}

async function refreshStory() {
  if (!state.storyId) return;
  try {
    const previousCount = state.story?.current_chapter || 0;
    state.story = await api(`/api/stories/${encodeURIComponent(state.storyId)}`);
    renderStory(state.story);
    notifyNewChapter(previousCount, state.story.current_chapter);
    if (!state.story.running) stopPolling();
  } catch (error) {
    stopPolling();
    showToast(error.message);
  }
}

function notifyNewChapter(previous, current) {
  if (previous > 0 && current > previous) showToast(`第 ${current} 章已经准备好了。`);
}

function renderStory(story) {
  document.querySelector("#story-kicker").textContent = story.running ? "故事正在形成" : "故事轨迹";
  document.querySelector("#story-title").textContent = story.title || "一个世界正在回应你";
  document.querySelector("#story-logline").textContent = story.logline || "人物、记忆与命运正在寻找彼此的位置。";
  document.querySelector("#genesis-status-text").textContent = story.phase;
  document.querySelector("#spine-title").textContent = story.title || "故事章节";

  // 只看是否已初始化、未完结且没有运行任务；前三章失败和重启后的 ready 都能继续。
  elements.growthActions.hidden = !canGrow(story);
  document.querySelector("#complete-story-button").hidden = story.current_chapter < 3;

  // 轮询收到了失败或完成状态时，页头也要同步，不能在按钮已恢复后仍显示“故事生长中”。
  if (views.genesis.classList.contains("is-active")) elements.quietStatus.textContent = viewStatus("genesis");
  renderChapterOrbit(story);
  renderSpine(story);
  if (state.currentChapter) renderReaderNavigation(story);
  drawVisibleCores();
}

function renderChapterOrbit(story) {
  elements.chapterOrbit.replaceChildren();
  const visibleCount = Math.min(
    Math.max(3, story.current_chapter + (story.running ? 1 : 0)),
    Math.max(story.current_chapter, 3),
  );
  for (let number = 1; number <= visibleCount; number += 1) {
    elements.chapterOrbit.appendChild(createOrbitChapter(story, number));
  }
}

function createOrbitChapter(story, number) {
  const chapter = chapterAt(story, number);
  const generating = story.running && number === story.current_chapter + 1;
  const button = document.createElement("button");
  button.type = "button";
  button.className = `orbit-chapter${chapter ? " is-ready" : ""}${generating ? " is-generating" : ""}`;
  button.disabled = !chapter;
  button.innerHTML = `<span class="orbit-node" aria-hidden="true"></span><strong></strong><small></small>`;
  button.querySelector("strong").textContent = chapter?.title || `第 ${number} 章`;
  button.querySelector("small").textContent = chapter ? "进入阅读" : generating ? "正在形成" : "等待故事生长";
  if (chapter) button.addEventListener("click", () => openChapter(number));
  return button;
}

function renderSpine(story) {
  elements.spineList.replaceChildren();
  story.chapters.forEach((chapter) => {
    const button = document.createElement("button");
    button.type = "button";
    button.className = `spine-chapter${chapter.number === state.currentChapter ? " is-current" : ""}`;
    button.innerHTML = `<small>CH.${String(chapter.number).padStart(2, "0")}</small><strong></strong>`;
    button.querySelector("strong").textContent = chapter.title;
    button.addEventListener("click", () => openChapter(chapter.number));
    elements.spineList.appendChild(button);
  });
}

async function openChapter(number, updateRoute = true) {
  if (!state.story || number < 1 || number > state.story.current_chapter) return;
  const chapter = await api(`/api/stories/${encodeURIComponent(state.storyId)}/chapters/${number}`);
  state.currentChapter = number;
  rememberPosition(state.storyId, number);
  renderChapter(chapter);
  renderSpine(state.story);
  renderReaderNavigation(state.story);
  showView("reader");
  if (updateRoute) history.pushState(null, "", `#/read/${encodeURIComponent(state.storyId)}/${number}`);
  window.scrollTo({ top: 0, behavior: "smooth" });
}

function renderChapter(chapter) {
  document.querySelector("#reader-number").textContent = `CHAPTER ${toRoman(chapter.number)}`;
  document.querySelector("#reader-title").textContent = chapter.title;
  document.querySelector("#reader-position").textContent = `第 ${chapter.number} 章`;
  renderChapterBody(chapter.body);
}

function renderChapterBody(markdown) {
  const container = document.querySelector("#chapter-body");
  const blocks = markdown.split(/\n\s*\n/).map((part) => part.trim()).filter(Boolean);
  container.replaceChildren();
  blocks.forEach((block, index) => appendChapterBlock(container, block, index));
}

function appendChapterBlock(container, block, index) {
  if (index === 0 && /^#\s+/.test(block)) return;
  const heading = block.match(/^#{2,4}\s+(.+)/);
  const element = document.createElement(heading ? "h3" : "p");
  element.textContent = heading ? heading[1] : block.replace(/\n/g, "");
  container.appendChild(element);
}

function renderReaderNavigation(story) {
  const previous = document.querySelector("#reader-previous");
  const next = document.querySelector("#reader-next");
  const generate = document.querySelector("#reader-generate-next");
  const ending = document.querySelector("#reader-ending");
  const hasPrevious = state.currentChapter > 1;
  const hasNext = state.currentChapter < story.current_chapter;
  previous.hidden = !hasPrevious;
  next.hidden = !hasNext;
  if (hasPrevious) document.querySelector("#previous-title").textContent = chapterAt(story, state.currentChapter - 1)?.title || "";
  if (hasNext) document.querySelector("#next-title").textContent = chapterAt(story, state.currentChapter + 1)?.title || "";
  renderLatestChapterAction(story, generate, ending, hasNext);
  renderReaderProgress(story);
}

function renderLatestChapterAction(story, generate, ending, hasNext) {
  const atLatest = state.currentChapter === story.current_chapter;
  // 最新已提交章节末尾与故事页使用相同条件，第二章读完也可以继续生长第三章。
  const canGenerate = atLatest && canGrow(story);
  generate.hidden = !canGenerate;
  ending.hidden = !(atLatest && !canGenerate && !hasNext);
  if (ending.hidden) return;
  if (story.running) ending.textContent = "下一章正在形成。";
  else if (isComplete(story)) ending.textContent = "这个故事已经抵达结局。";
  else ending.textContent = "故事轨迹仍在准备。";
}

function renderReaderProgress(story) {
  document.querySelector("#reader-progress").textContent = `第 ${state.currentChapter} 章`;
  document.querySelector("#reader-story-status").textContent = story.running ? "故事生长中" : isComplete(story) ? "已经完成" : "阅读中";
  drawReaderCore(document.querySelector("#reader-core"), state.currentChapter, Math.max(story.current_chapter, state.currentChapter));
}

async function generateNextChapter() {
  await continueStory("next", "故事正在继续生长。", true);
}

async function completeStory() {
  await continueStory("complete", "故事会在后台逐章完成。", false);
}

async function continueStory(action, message, returnToStory) {
  // 不等待轮询发现 running 才禁止点击；请求失败后释放入口，用户仍能再次继续生长。
  if (state.growthPending || !canGrow(state.story)) return;
  setGrowthPending(true);
  try {
    await api(`/api/stories/${encodeURIComponent(state.storyId)}/${action}`, { method: "POST" });
    showToast(message);
    await refreshStory();
    startPolling();
    if (returnToStory) navigate(`story/${state.storyId}`);
  } catch (error) {
    showToast(error.message);
  } finally {
    setGrowthPending(false);
  }
}

// 两个逐章入口和全书入口共用提交锁，避免同一页面同时发起不同类型的生成请求。
function setGrowthPending(pending) {
  state.growthPending = pending;
  for (const id of ["next-chapter-button", "reader-generate-next", "complete-story-button"]) {
    document.querySelector(`#${id}`).disabled = pending;
  }
}

function showView(name) {
  Object.entries(views).forEach(([key, view]) => view.classList.toggle("is-active", key === name));
  document.querySelectorAll(".nav-link").forEach((link) => {
    const active = link.dataset.route === name || (name === "genesis" && link.dataset.route === "create");
    link.classList.toggle("is-active", active);
    link.setAttribute("aria-current", active ? "page" : "false");
  });
  elements.quietStatus.textContent = viewStatus(name);
  requestAnimationFrame(drawVisibleCores);
}

function viewStatus(name) {
  if (name === "library") return `${state.library.length} 个故事`;
  if (name === "reader") return "沉浸阅读";
  if (name === "genesis") return state.story?.running ? "故事生长中" : "可以阅读";
  return "等待灵感";
}

function updateIdeaState() {
  elements.ideaCount.textContent = String([...elements.idea.value].length);
}

async function api(path, options = {}) {
  const request = { ...options, headers: { ...(options.headers || {}) } };
  if (options.body) {
    request.headers["Content-Type"] = "application/json";
    request.body = JSON.stringify(options.body);
  }
  const response = await fetch(path, request);
  const payload = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(payload.error || "请求没有完成，请稍后再试。");
  return payload;
}

function showToast(message) {
  const toast = document.querySelector("#toast");
  toast.textContent = message;
  toast.classList.add("is-visible");
  window.clearTimeout(state.toastTimer);
  state.toastTimer = window.setTimeout(() => toast.classList.remove("is-visible"), 3800);
}

function chapterAt(story, number) {
  return story.chapters.find((chapter) => chapter.number === number);
}

function isComplete(story) {
  return story.story_status === "completed";
}

// story_status 来自正式检查点：ongoing 也包括初始化完成但尚未提交正文的第 0 章。
// 不依赖内存中的 failed 标记，刷新和服务重启不会丢失继续生长的资格。
function canGrow(story) {
  return story?.story_status === "ongoing" && !story.running;
}

function lengthLabel(length) {
  return { short: "短篇", medium: "中篇", long: "长篇", epic: "超长篇" }[length] || "小说";
}

function storyStatus(story) {
  if (story.running) return "创作中";
  if (isComplete(story)) return "已完成";
  return story.current_chapter ? "等待续写" : "故事初生";
}

function rememberPosition(id, chapter) {
  localStorage.setItem(`story-position:${id}`, String(chapter));
}

function readPosition(id) {
  const value = Number(localStorage.getItem(`story-position:${id}`));
  return Number.isInteger(value) && value > 0 ? value : 0;
}

function toRoman(number) {
  const values = [[10, "X"], [9, "IX"], [5, "V"], [4, "IV"], [1, "I"]];
  let remaining = number;
  return values.map(([value, glyph]) => {
    const count = Math.floor(remaining / value);
    remaining %= value;
    return glyph.repeat(count);
  }).join("");
}

function startCoreAnimation() {
  const reduced = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
  const frame = (time) => {
    drawVisibleCores(reduced ? 0 : time);
    state.coreFrame = window.requestAnimationFrame(frame);
  };
  state.coreFrame = window.requestAnimationFrame(frame);
}

function drawVisibleCores(time = performance.now()) {
  const strength = Math.min([...elements.idea.value].length / 420, 1);
  drawCoreCanvas(document.querySelector("#story-core"), time, strength, false);
  drawCoreCanvas(document.querySelector("#genesis-canvas"), time, 0.84, true);
}

function prepareCanvas(canvas) {
  const rect = canvas.getBoundingClientRect();
  if (!rect.width || !rect.height) return null;
  const ratio = Math.min(window.devicePixelRatio || 1, 2);
  const width = Math.round(rect.width * ratio);
  const height = Math.round(rect.height * ratio);
  if (canvas.width !== width || canvas.height !== height) {
    canvas.width = width;
    canvas.height = height;
  }
  const context = canvas.getContext("2d");
  context.setTransform(ratio, 0, 0, ratio, 0, 0);
  return { context, width: rect.width, height: rect.height };
}

function drawCoreCanvas(canvas, time, strength, wide) {
  const prepared = prepareCanvas(canvas);
  if (!prepared) return;
  const { context, width, height } = prepared;
  context.clearRect(0, 0, width, height);
  const center = { x: width * (wide ? 0.72 : 0.58), y: height * (wide ? 0.55 : 0.46) };
  const radius = Math.min(width, height) * (wide ? 0.1 : 0.13);
  drawCoreGlow(context, center, radius, strength);
  drawOrbits(context, center, radius, time, width, height);
  drawCoreGlyph(context, center, radius);
}

function drawCoreGlow(context, center, radius, strength) {
  const layers = [2.05, 1.68, 1.35, 1.08, 0.82];
  layers.forEach((scale, index) => {
    context.beginPath();
    context.arc(center.x, center.y, radius * scale, 0, Math.PI * 2);
    context.fillStyle = `rgba(23, 105, 255, ${0.025 + (layers.length - index) * 0.018 * strength})`;
    context.fill();
  });
  context.beginPath();
  context.arc(center.x, center.y, radius * 0.74, 0, Math.PI * 2);
  context.fillStyle = `rgba(23, 105, 255, ${0.55 + strength * 0.24})`;
  context.fill();
}

function drawOrbits(context, center, radius, time, width, height) {
  const orbitSpecs = [
    { rx: radius * 3.5, ry: radius * 1.05, rotation: -0.24, color: "#0b0c0e", dash: [] },
    { rx: radius * 2.9, ry: radius * 1.55, rotation: 0.86, color: "#1769ff", dash: [] },
    { rx: radius * 4.3, ry: radius * 1.6, rotation: 0.08, color: "#8f949d", dash: [4, 7] },
  ];
  orbitSpecs.forEach((spec) => strokeOrbit(context, center, spec));
  drawOrbitNodes(context, center, orbitSpecs, time);
  drawPeripheralMarks(context, width, height);
}

function strokeOrbit(context, center, spec) {
  context.save();
  context.translate(center.x, center.y);
  context.rotate(spec.rotation);
  context.beginPath();
  context.ellipse(0, 0, spec.rx, spec.ry, 0, 0, Math.PI * 2);
  context.setLineDash(spec.dash);
  context.strokeStyle = spec.color;
  context.globalAlpha = spec.color === "#8f949d" ? 0.55 : 0.82;
  context.lineWidth = 1;
  context.stroke();
  context.restore();
}

function drawOrbitNodes(context, center, specs, time) {
  const colors = ["#1769ff", "#20a66a", "#f2b705", "#ff5522", "#0b0c0e"];
  colors.forEach((color, index) => {
    const spec = specs[index % specs.length];
    const angle = time * (0.000055 + index * 0.000006) + index * 1.7;
    const x0 = Math.cos(angle) * spec.rx;
    const y0 = Math.sin(angle) * spec.ry;
    const x = center.x + x0 * Math.cos(spec.rotation) - y0 * Math.sin(spec.rotation);
    const y = center.y + x0 * Math.sin(spec.rotation) + y0 * Math.cos(spec.rotation);
    context.beginPath();
    context.arc(x, y, index === 4 ? 5 : 4, 0, Math.PI * 2);
    context.fillStyle = color;
    context.fill();
  });
}

function drawCoreGlyph(context, center, radius) {
  const size = radius * 0.28;
  context.beginPath();
  context.moveTo(center.x, center.y - size);
  context.quadraticCurveTo(center.x, center.y, center.x + size, center.y);
  context.quadraticCurveTo(center.x, center.y, center.x, center.y + size);
  context.quadraticCurveTo(center.x, center.y, center.x - size, center.y);
  context.quadraticCurveTo(center.x, center.y, center.x, center.y - size);
  context.fillStyle = "#0b0c0e";
  context.fill();
}

function drawPeripheralMarks(context, width, height) {
  context.strokeStyle = "rgba(11, 12, 14, 0.18)";
  context.lineWidth = 1;
  context.beginPath();
  context.moveTo(width * 0.08, height * 0.2);
  context.lineTo(width * 0.25, height * 0.2);
  context.lineTo(width * 0.28, height * 0.14);
  context.stroke();
}

function drawBookCover(canvas, story, index) {
  const prepared = prepareCanvas(canvas);
  if (!prepared) return;
  const { context, width, height } = prepared;
  const seed = hashText(story.id) + index * 17;
  context.fillStyle = "#ffffff";
  context.fillRect(0, 0, width, height);
  const center = { x: width * (0.46 + (seed % 9) / 100), y: height * 0.43 };
  const radius = Math.min(width, height) * 0.15;
  drawCoreGlow(context, center, radius, 0.76);
  drawCoverOrbits(context, center, radius, seed);
  drawCoreGlyph(context, center, radius);
}

function drawCoverOrbits(context, center, radius, seed) {
  const colors = ["#1769ff", "#0b0c0e", "#f2b705", "#20a66a", "#ff5522"];
  for (let index = 0; index < 3; index += 1) {
    const rotation = ((seed % (31 + index * 7)) / 31) + index * 0.7;
    const spec = { rx: radius * (2.1 + index * 0.55), ry: radius * (0.72 + index * 0.22), rotation, color: colors[(seed + index) % colors.length], dash: index === 2 ? [3, 5] : [] };
    strokeOrbit(context, center, spec);
  }
}

function drawReaderCore(canvas, current, target) {
  const context = canvas.getContext("2d");
  const size = canvas.width;
  context.clearRect(0, 0, size, size);
  const center = { x: size / 2, y: size / 2 };
  [62, 48, 34].forEach((radius) => {
    context.beginPath();
    context.arc(center.x, center.y, radius, 0, Math.PI * 2);
    context.strokeStyle = radius === 48 ? "#1769ff" : "#d9dce1";
    context.lineWidth = 1;
    context.stroke();
  });
  const angle = (Math.max(current, 1) / Math.max(target, 1)) * Math.PI * 2 - Math.PI / 2;
  context.beginPath();
  context.arc(center.x + Math.cos(angle) * 48, center.y + Math.sin(angle) * 48, 5, 0, Math.PI * 2);
  context.fillStyle = "#1769ff";
  context.fill();
  drawCoreGlyph(context, center, 40);
}

function hashText(text) {
  return [...text].reduce((hash, character) => ((hash << 5) - hash + character.charCodeAt(0)) >>> 0, 7);
}
