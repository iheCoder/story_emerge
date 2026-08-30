const views = {
  create: document.querySelector("#create-view"),
  genesis: document.querySelector("#genesis-view"),
  reader: document.querySelector("#reader-view"),
};

const state = {
  storyId: "",
  story: null,
  currentChapter: 0,
  pollTimer: null,
  toastTimer: null,
};

const form = document.querySelector("#creation-form");
const ideaInput = document.querySelector("#idea");
const formError = document.querySelector("#form-error");
const createButton = document.querySelector("#create-button");
const chapterNodes = [...document.querySelectorAll(".chapter-node")];
const statusText = document.querySelector("#genesis-status-text");
const quietStatus = document.querySelector("#quiet-status");
const previewDecision = document.querySelector("#preview-decision");

form.addEventListener("submit", createStory);
ideaInput.addEventListener("input", updateIdeaAtmosphere);
document.querySelector("#brand-button").addEventListener("click", () => showView("create"));
document.querySelector("#back-to-genesis").addEventListener("click", () => showView("genesis"));
document.querySelector("#next-chapter-button").addEventListener("click", generateNextChapter);
document.querySelector("#complete-story-button").addEventListener("click", completeStory);
document.querySelector("#reader-generate-next").addEventListener("click", generateNextChapter);
document.querySelector("#reader-previous").addEventListener("click", () => openChapter(state.currentChapter - 1));
document.querySelector("#reader-next").addEventListener("click", () => openChapter(state.currentChapter + 1));
chapterNodes.forEach((node) => node.addEventListener("click", () => openChapter(Number(node.dataset.chapter))));

async function createStory(event) {
  event.preventDefault();
  const idea = ideaInput.value.trim();
  const length = new FormData(form).get("length");
  formError.textContent = "";

  if (!idea) {
    formError.textContent = "先写下一点你真正想看的故事。";
    ideaInput.focus();
    return;
  }

  setButtonBusy(createButton, true, "故事正在醒来");
  try {
    const response = await api("/api/stories", { method: "POST", body: { idea, length } });
    state.storyId = response.id;
    showView("genesis");
    startPolling();
  } catch (error) {
    formError.textContent = error.message;
  } finally {
    setButtonBusy(createButton, false, "开始创造");
  }
}

function startPolling() {
  stopPolling();
  refreshStory();
  state.pollTimer = window.setInterval(refreshStory, 1400);
}

function stopPolling() {
  if (state.pollTimer) {
    window.clearInterval(state.pollTimer);
    state.pollTimer = null;
  }
}

async function refreshStory() {
  if (!state.storyId) return;
  try {
    const previousCount = state.story?.current_chapter || 0;
    state.story = await api(`/api/stories/${state.storyId}`);
    renderStory(state.story);
    if (state.story.current_chapter > previousCount && previousCount > 0) {
      showToast(`第 ${state.story.current_chapter} 章已经准备好了。`);
    }
    if (state.story.status === "failed") {
      stopPolling();
      showToast(state.story.error || "故事暂时停住了，请稍后重试。");
    }
  } catch (error) {
    showToast(error.message);
  }
}

function renderStory(story) {
  statusText.textContent = story.phase;
  quietStatus.textContent = story.status === "failed" ? "需要处理" : story.running ? "故事生长中" : "可以阅读";
  document.querySelector("#story-kicker").textContent = story.status === "failed" ? "故事暂时停住" : story.running ? "故事正在形成" : "三章试读";
  document.querySelector("#story-title").textContent = story.title || "一个世界正在回应你";
  document.querySelector("#story-logline").textContent = story.logline || "人物、记忆与命运正在寻找彼此的位置。";
  previewDecision.hidden = story.current_chapter < 3 || story.running;
  renderChapterNodes(story);
  renderLaterChapters(story);
  renderReaderContinuation(story);
}

function renderChapterNodes(story) {
  const completed = new Map(story.chapters.map((chapter) => [chapter.number, chapter]));
  chapterNodes.forEach((node, index) => {
    const number = index + 1;
    const chapter = completed.get(number);
    const isGenerating = story.running && number === story.current_chapter + 1;
    node.classList.toggle("is-ready", Boolean(chapter));
    node.classList.toggle("is-generating", isGenerating);
    node.disabled = !chapter;
    node.querySelector("strong").textContent = chapter?.title || `第${toChineseNumber(number)}章`;
    node.querySelector("small").textContent = chapter ? "点击进入阅读" : isGenerating ? "正在形成" : "尚未开始";
  });

  const progress = Math.min(story.current_chapter / 3, 1) * 100;
  const progressElement = document.querySelector("#track-progress");
  progressElement.style.setProperty("--track-progress", `${progress}%`);
}

function renderLaterChapters(story) {
  const container = document.querySelector("#later-chapters");
  const later = story.chapters.filter((chapter) => chapter.number > 3);
  const generatingNumber = story.running && story.current_chapter >= 3 ? story.current_chapter + 1 : 0;
  container.replaceChildren();

  later.forEach((chapter) => container.appendChild(createLaterChapter(chapter.number, chapter.title, true)));
  if (generatingNumber > 3 && generatingNumber <= story.target_chapters) {
    container.appendChild(createLaterChapter(generatingNumber, "正在形成", false));
  }
  container.hidden = container.childElementCount === 0;
}

function createLaterChapter(number, title, ready) {
  const button = document.createElement("button");
  button.type = "button";
  button.className = "later-chapter";
  button.disabled = !ready;
  const heading = document.createElement("strong");
  const status = document.createElement("small");
  heading.textContent = title;
  status.textContent = ready ? `第 ${number} 章，点击阅读` : `第 ${number} 章正在生长`;
  button.append(heading, status);
  if (ready) button.addEventListener("click", () => openChapter(number));
  return button;
}

async function openChapter(number) {
  if (!state.story || number < 1 || number > state.story.current_chapter) return;
  try {
    const chapter = await api(`/api/stories/${state.storyId}/chapters/${number}`);
    state.currentChapter = number;
    document.querySelector("#reader-number").textContent = `CHAPTER ${toRoman(number)}`;
    document.querySelector("#reader-title").textContent = chapter.title;
    document.querySelector("#reader-position").textContent = `第 ${number} 章`;
    renderChapterBody(chapter.body);
    renderReaderContinuation(state.story);
    showView("reader");
    window.scrollTo({ top: 0, behavior: "smooth" });
  } catch (error) {
    showToast(error.message);
  }
}

function renderChapterBody(markdown) {
  const container = document.querySelector("#chapter-body");
  container.replaceChildren();
  const blocks = markdown.split(/\n\s*\n/).map((part) => part.trim()).filter(Boolean);
  blocks.forEach((block, index) => {
    if (index === 0 && /^#\s+/.test(block)) return;
    const heading = block.match(/^#{2,4}\s+(.+)/);
    const element = document.createElement(heading ? "h3" : "p");
    element.textContent = heading ? heading[1] : block.replace(/\n/g, "");
    container.appendChild(element);
  });
}

function renderReaderContinuation(story) {
  if (!story || !state.currentChapter) return;
  const hasNext = state.currentChapter < story.current_chapter;
  const atLatest = state.currentChapter === story.current_chapter;
  const previewComplete = story.current_chapter >= 3;
  const readerNext = document.querySelector("#reader-next");
  const previous = document.querySelector("#reader-previous");
  const generate = document.querySelector("#reader-generate-next");
  const text = document.querySelector("#reader-continuation-text");

  previous.disabled = state.currentChapter <= 1;
  readerNext.disabled = !hasNext;
  generate.hidden = !(atLatest && previewComplete && !story.running && story.current_chapter < story.target_chapters);
  if (hasNext) {
    text.textContent = "下一章已经准备好了。";
  } else if (story.running) {
    text.textContent = "下一章正在故事轨迹中形成。";
  } else if (story.current_chapter >= story.target_chapters) {
    text.textContent = "这个故事已经抵达结局。";
  } else {
    text.textContent = "读到这里，你可以让故事继续。";
  }
}

async function generateNextChapter() {
  await continueStory("next", "下一章开始形成。");
}

async function completeStory() {
  await continueStory("complete", "故事会在后台逐章完成，已写好的章节仍然可以立即阅读。");
}

async function continueStory(action, message) {
  try {
    await api(`/api/stories/${state.storyId}/${action}`, { method: "POST" });
    previewDecision.hidden = true;
    showToast(message);
    startPolling();
  } catch (error) {
    showToast(error.message);
  }
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

function showView(name) {
  Object.entries(views).forEach(([key, view]) => view.classList.toggle("is-active", key === name));
  if (name === "create") quietStatus.textContent = "灵感入口";
  if (name === "genesis" && state.story) quietStatus.textContent = state.story.running ? "故事生长中" : "可以阅读";
  if (name === "reader") quietStatus.textContent = "沉浸阅读";
}

function setButtonBusy(button, busy, label) {
  button.disabled = busy;
  button.querySelector("span:first-child").textContent = label;
}

function showToast(message) {
  const toast = document.querySelector("#toast");
  toast.textContent = message;
  toast.classList.add("is-visible");
  window.clearTimeout(state.toastTimer);
  state.toastTimer = window.setTimeout(() => toast.classList.remove("is-visible"), 4200);
}

function toChineseNumber(number) {
  return ["零", "一", "二", "三", "四", "五", "六", "七", "八", "九", "十"][number] || String(number);
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

function updateIdeaAtmosphere() {
  const text = ideaInput.value;
  const candidates = [...new Set(text.match(/[\u4e00-\u9fa5]{2,4}/g) || [])];
  ["#fragment-a", "#fragment-b", "#fragment-c"].forEach((selector, index) => {
    const fragment = document.querySelector(selector);
    fragment.textContent = candidates[(index * 3 + 2) % Math.max(candidates.length, 1)] || ["记忆", "重逢", "雨夜"][index];
    fragment.style.opacity = String(Math.min(0.45 + text.length / 800, 1));
    fragment.style.transform = `translateY(${Math.min(text.length / 80, 8)}px)`;
  });
}

function createAmbientField() {
  if (window.matchMedia("(prefers-reduced-motion: reduce)").matches) return;
  const canvas = document.querySelector("#ambient-canvas");
  const context = canvas.getContext("2d");
  const particles = Array.from({ length: 44 }, (_, index) => ({
    x: ((index * 73) % 101) / 100,
    y: ((index * 47) % 97) / 96,
    radius: 0.45 + (index % 4) * 0.22,
    speed: 0.000035 + (index % 5) * 0.000008,
  }));

  function resize() {
    const ratio = Math.min(window.devicePixelRatio || 1, 2);
    canvas.width = window.innerWidth * ratio;
    canvas.height = window.innerHeight * ratio;
    context.setTransform(ratio, 0, 0, ratio, 0, 0);
  }

  function draw(time) {
    context.clearRect(0, 0, window.innerWidth, window.innerHeight);
    particles.forEach((particle) => {
      const y = (particle.y + time * particle.speed) % 1;
      context.beginPath();
      context.arc(particle.x * window.innerWidth, y * window.innerHeight, particle.radius, 0, Math.PI * 2);
      context.fillStyle = "rgba(83, 99, 217, 0.28)";
      context.fill();
    });
    window.requestAnimationFrame(draw);
  }

  resize();
  window.addEventListener("resize", resize, { passive: true });
  window.requestAnimationFrame(draw);
}

updateIdeaAtmosphere();
createAmbientField();
