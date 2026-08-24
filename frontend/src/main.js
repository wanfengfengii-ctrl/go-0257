// RiceGuard browser inspection console: reads live backend state from the Go
// API, submits task-creation requests and drives the inspection pipeline.
const statusNames = {
  pending_create: "待建检",
  pending_sampling: "待抽样确认",
  blind_split: "盲码分管中",
  occupying: "舱位占用中",
  germinating: "发芽观察中",
  pathogen_checking: "病原核验中",
  moisture_checking: "含水复测中",
  pending_review: "待独立复核",
  releasable: "可放播",
  released: "已放播",
  quarantined: "污染隔离",
  cancelled: "已取消",
};

let selectedTask = null;

async function api(path, opts) {
  const res = await fetch(path, opts);
  const data = await res.json().catch(() => ({}));
  return { ok: res.ok, status: res.status, data };
}

function setResult(el, text, ok) {
  el.textContent = text;
  el.className = "result " + (ok ? "ok" : "err");
}

async function refreshHealth() {
  const el = document.getElementById("health");
  const { data } = await api("/api/health");
  if (data.status === "ok") {
    el.textContent = "● 在线";
    el.className = "health ok";
  } else {
    el.textContent = "● 异常";
    el.className = "health err";
  }
}

async function loadCatalog() {
  const { data } = await api("/api/catalog");
  const sel = document.getElementById("variety-select");
  sel.innerHTML = "";
  for (const v of data.varieties || []) {
    const opt = document.createElement("option");
    opt.value = v.ID;
    opt.textContent = `${v.ID}（${(v.MoistureMax / 100).toFixed(2)}% 水分上限）`;
    sel.appendChild(opt);
  }
}

function csvList(value) {
  return value.split(",").map((s) => s.trim()).filter(Boolean);
}

async function refreshTasks() {
  const body = document.getElementById("tasks-body");
  const { data } = await api("/api/tasks");
  body.innerHTML = "";
  for (const t of data.tasks || []) {
    const tr = document.createElement("tr");
    tr.innerHTML =
      `<td>${t.ID}</td><td>${t.SeedLot}</td><td>${t.Variety}</td>` +
      `<td>${statusNames[t.Status] || t.Status}</td><td>${t.Generation}</td>` +
      `<td><button data-id="${t.ID}">查看</button></td>`;
    tr.querySelector("button").addEventListener("click", () => viewTask(t.ID));
    body.appendChild(tr);
  }
}

async function viewTask(id) {
  selectedTask = id;
  const { data } = await api(`/api/tasks/${id}`);
  if (!data.task) {
    document.getElementById("detail-card").hidden = true;
    return;
  }
  document.getElementById("detail-card").hidden = false;
  document.getElementById("detail-title").textContent = `任务详情 · ${id}`;

  const s = data.summary || {};
  const summaryEl = document.getElementById("detail-summary");
  summaryEl.innerHTML =
    `<div><strong>状态：</strong>${s.status_text || s.status}</div>` +
    `<div><strong>代次：</strong>${s.generation}</div>` +
    `<div><strong>发芽覆盖：</strong>${s.germination_covered ? "是" : "否"}</div>` +
    `<div><strong>发芽率：</strong>${(s.germination_rate_bp / 100).toFixed(2)}%（下限 ${(s.germination_rate_min_bp / 100).toFixed(2)}%）</div>` +
    `<div><strong>病原：</strong>${s.pathogen_clean ? "洁净" : "阳性/污染"}（覆盖：${s.pathogen_covered ? "是" : "否"}）</div>` +
    `<div><strong>含水/净度：</strong>${s.moisture_passed ? "达标" : "未达标"}</div>` +
    `<div><strong>复核通过：</strong>${s.approvals}/2</div>` +
    `<div><strong>可放播：</strong>${s.releasable ? "是" : "否"}</div>`;

  renderActions(data);
  document.getElementById("detail-raw").textContent = JSON.stringify(data, null, 2);
}

function renderActions(data) {
  const box = document.getElementById("detail-actions");
  box.innerHTML = "";
  const t = data.task || {};
  const status = t.Status;
  const id = t.ID;

  const add = (label, fn) => {
    const b = document.createElement("button");
    b.textContent = label;
    b.addEventListener("click", fn);
    box.appendChild(b);
  };

  if (status === "pending_sampling") {
    add("双人抽样确认", () => confirmSampling(id, "sampler-a", "sampler-b"));
  } else if (status === "blind_split") {
    add("盲码三联分管", () => splitSamples(id));
  } else if (status === "occupying") {
    add("占用舱位/板孔", () => occupy(id));
  } else if (status === "germinating") {
    add("记录全部发芽日龄", () => recordGermination(id, t));
  } else if (status === "pathogen_checking") {
    add("记录病原读数（阴性）", () => recordPathogen(id, t));
  } else if (status === "moisture_checking") {
    add("含水净度复测", () => recordMoisture(id));
  } else if (status === "pending_review") {
    add("独立复核 ×2", () => review(id));
  } else if (status === "releasable") {
    add("终局：放播", () => finalize(id, ""));
    add("终局：污染隔离", () => finalize(id, "quarantined"));
    add("终局：取消", () => finalize(id, "cancelled"));
  }
}

async function post(id, path, body) {
  const result = document.getElementById("create-result");
  const { ok, data } = await api(`/api/tasks/${id}/${path}`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  if (ok) {
    setResult(result, `成功（代次 ${data.generation}）`, true);
  } else {
    const e = data.error || {};
    setResult(result, `${e.code}: ${(e.reasons || []).join(", ")}`, false);
  }
  await refresh();
}

async function confirmSampling(id, a, b) {
  const field = "field-01";
  const seedLot = (await api(`/api/tasks/${id}`)).data.task.SeedLot;
  await post(id, "sampling-confirmations", { operation_id: `s1-${Date.now()}`, reviewer: a, field, seed_lot: seedLot, blind_seal: "seal-1", sample_count: 180 });
  await post(id, "sampling-confirmations", { operation_id: `s2-${Date.now()}`, reviewer: b, field, seed_lot: seedLot, blind_seal: "seal-1", sample_count: 180 });
}

async function splitSamples(id) {
  await post(id, "split-blind-samples", { operation_id: `sp-${Date.now()}` });
}

async function occupy(id) {
  await post(id, "occupancies", { operation_id: `oc-${Date.now()}` });
}

async function recordGermination(id, t) {
  for (const d of t.DayAges || []) {
    await post(id, "germination-observations", {
      operation_id: `g-${Date.now()}-${d}`, blind_code: (t.BlindAllocs || [])[0]?.Code || "b1",
      day_age: d, normal: 95, abnormal: 3, dead: 2, collector: "germinator-c",
    });
  }
}

async function recordPathogen(id, t) {
  for (const w of t.Wells || []) {
    await post(id, "pathogen-readings", {
      operation_id: `p-${Date.now()}-${w}`, blind_code: (t.BlindAllocs || [])[0]?.Code || "b1",
      plate: t.Plate, well: w, verifier: "pathologist-d", reading: 10,
    });
  }
}

async function recordMoisture(id) {
  await post(id, "measurements/moisture-purity", {
    operation_id: `m-${Date.now()}`, moisture: "12.50", purity_grains: 98,
    total_grains: 100, thousand_grain: 25000, collector: "metrologist-e",
  });
}

async function review(id) {
  await post(id, "reviews", { operation_id: `r1-${Date.now()}`, reviewer: "reviewer-f", conclusion: "approve" });
  await post(id, "reviews", { operation_id: `r2-${Date.now()}`, reviewer: "reviewer-g", conclusion: "approve" });
}

async function finalize(id, outcome) {
  await post(id, "finalize", { operation_id: `f-${Date.now()}`, outcome, reason: outcome === "cancelled" ? "operator" : "" });
}

async function refresh() {
  await refreshHealth();
  await refreshTasks();
  if (selectedTask) await viewTask(selectedTask);
}

document.getElementById("create-form").addEventListener("submit", async (event) => {
  event.preventDefault();
  const form = event.target;
  const codes = csvList(form.blind_codes.value);
  const blindAllocs = codes.map((c) => ({ code: c, germination: 100, pathogen: 50, moisture: 30 }));
  const payload = {
    operation_id: form.operation_id.value,
    seed_lot: form.seed_lot.value,
    field: form.field.value,
    variety: form.variety.value,
    female_cert_revision: Number(form.female_cert_revision.value),
    male_cert_revision: Number(form.male_cert_revision.value),
    blind_allocations: blindAllocs,
    chamber: form.chamber.value,
    chamber_start: Number(form.chamber_start.value),
    chamber_end: Number(form.chamber_end.value),
    plate: form.plate.value,
    wells: csvList(form.wells.value),
    reviewer_roster: csvList(form.reviewer_roster.value),
  };
  const result = document.getElementById("create-result");
  const { ok, data } = await api("/api/tasks", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
  if (ok) {
    setResult(result, `已建检锁定：${data.task_id}（代次 ${data.generation}）`, true);
  } else {
    const e = data.error || {};
    setResult(result, `${e.code}: ${(e.reasons || []).join(", ")}`, false);
  }
  await refreshTasks();
});

(async () => {
  await loadCatalog();
  await refresh();
})();
