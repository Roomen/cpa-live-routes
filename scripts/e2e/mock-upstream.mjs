#!/usr/bin/env node
// Mock OpenAI-compatible / Anthropic-style upstream for end-to-end tests. No dependencies.
//
// Model name behaviour:
//   mock-fast   immediate answer
//   mock-slow   first token after MOCK_SLOW_TTFT_MS (default 1500), total MOCK_SLOW_TOTAL_MS (default 8000)
//   mock-429    always 429 with Retry-After: 30
//   mock-<name>-ttft<ms>-total<ms>   custom timings, e.g. mock-x-ttft200-total3000
// Env: PORT (default 0 = random, printed), MOCK_429_RATIO (0..1, random 429 for other models).
import http from "node:http";

const port = Number(process.env.PORT || 0);
const ratio429 = Number(process.env.MOCK_429_RATIO || 0);
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

function timings(model) {
  let ttft = 0, total = 0;
  if (model === "mock-slow") { ttft = Number(process.env.MOCK_SLOW_TTFT_MS || 1500); total = Number(process.env.MOCK_SLOW_TOTAL_MS || 8000); }
  const m = /-ttft(\d+)-total(\d+)$/.exec(model || "");
  if (m) { ttft = Number(m[1]); total = Number(m[2]); }
  return { ttft, total: Math.max(total, ttft) };
}

function readBody(req) {
  return new Promise((resolve) => {
    const chunks = [];
    req.on("data", (c) => chunks.push(c));
    req.on("end", () => resolve(Buffer.concat(chunks)));
  });
}

function json(res, status, obj, headers = {}) {
  const b = JSON.stringify(obj);
  res.writeHead(status, { "content-type": "application/json", "content-length": Buffer.byteLength(b), ...headers });
  res.end(b);
}

function limited(model) {
  return model === "mock-429" || (ratio429 > 0 && Math.random() < ratio429);
}

function reject429(res) {
  json(res, 429, { error: { message: "rate limit exceeded (mock)", type: "rate_limit_error", code: "rate_limit_exceeded" } }, { "retry-after": "30" });
}

const WORDS = ["Live", "routes", "mock", "reply", "streaming", "token", "by", "token", "."];

async function chat(req, res, body) {
  const model = body.model || "mock-fast";
  if (limited(model)) return reject429(res);
  const { ttft, total } = timings(model);
  const usage = { prompt_tokens: 120, completion_tokens: WORDS.length, total_tokens: 120 + WORDS.length };
  const id = "chatcmpl-mock-" + Date.now();
  if (!body.stream) {
    await sleep(total);
    return json(res, 200, { id, object: "chat.completion", created: Math.floor(Date.now() / 1000), model,
      choices: [{ index: 0, message: { role: "assistant", content: WORDS.join(" ") }, finish_reason: "stop" }], usage });
  }
  res.writeHead(200, { "content-type": "text/event-stream", "cache-control": "no-cache" });
  const send = (o) => res.write("data: " + JSON.stringify(o) + "\n\n");
  const base = { id, object: "chat.completion.chunk", created: Math.floor(Date.now() / 1000), model };
  await sleep(ttft);
  send({ ...base, choices: [{ index: 0, delta: { role: "assistant", content: "" }, finish_reason: null }] });
  const step = WORDS.length > 0 ? (total - ttft) / WORDS.length : 0;
  for (const w of WORDS) {
    if (res.destroyed) return;
    if (step > 0) await sleep(step);
    send({ ...base, choices: [{ index: 0, delta: { content: w + " " }, finish_reason: null }] });
  }
  send({ ...base, choices: [{ index: 0, delta: {}, finish_reason: "stop" }], usage });
  res.write("data: [DONE]\n\n");
  res.end();
}

async function messages(req, res, body) {
  const model = body.model || "mock-fast";
  if (limited(model)) {
    return json(res, 429, { type: "error", error: { type: "rate_limit_error", message: "rate limit exceeded (mock)" } }, { "retry-after": "30" });
  }
  const { ttft, total } = timings(model);
  const id = "msg_mock_" + Date.now();
  if (!body.stream) {
    await sleep(total);
    return json(res, 200, { id, type: "message", role: "assistant", model, content: [{ type: "text", text: WORDS.join(" ") }],
      stop_reason: "end_turn", usage: { input_tokens: 120, output_tokens: WORDS.length } });
  }
  res.writeHead(200, { "content-type": "text/event-stream", "cache-control": "no-cache" });
  const ev = (name, o) => res.write(`event: ${name}\ndata: ${JSON.stringify(o)}\n\n`);
  await sleep(ttft);
  ev("message_start", { type: "message_start", message: { id, type: "message", role: "assistant", model, content: [], usage: { input_tokens: 120, output_tokens: 1 } } });
  ev("content_block_start", { type: "content_block_start", index: 0, content_block: { type: "text", text: "" } });
  const step = (total - ttft) / WORDS.length;
  for (const w of WORDS) {
    if (res.destroyed) return;
    if (step > 0) await sleep(step);
    ev("content_block_delta", { type: "content_block_delta", index: 0, delta: { type: "text_delta", text: w + " " } });
  }
  ev("content_block_stop", { type: "content_block_stop", index: 0 });
  ev("message_delta", { type: "message_delta", delta: { stop_reason: "end_turn" }, usage: { output_tokens: WORDS.length } });
  ev("message_stop", { type: "message_stop" });
  res.end();
}

const MODELS = ["mock-fast", "mock-slow", "mock-429"];

const server = http.createServer(async (req, res) => {
  const url = new URL(req.url, "http://x");
  try {
    if (req.method === "GET" && (url.pathname === "/v1/models" || url.pathname === "/models")) {
      return json(res, 200, { object: "list", data: MODELS.map((id) => ({ id, object: "model", created: 0, owned_by: "mock" })) });
    }
    if (req.method === "GET" && url.pathname === "/healthz") return json(res, 200, { ok: true });
    if (req.method === "POST") {
      const raw = await readBody(req);
      let body = {};
      try { body = JSON.parse(raw.toString() || "{}"); } catch { return json(res, 400, { error: { message: "invalid json" } }); }
      if (url.pathname.endsWith("/chat/completions")) return await chat(req, res, body);
      if (url.pathname.endsWith("/messages")) return await messages(req, res, body);
    }
    json(res, 404, { error: { message: "not found: " + url.pathname } });
  } catch (e) {
    if (!res.headersSent) json(res, 500, { error: { message: String(e) } }); else res.end();
  }
});

server.listen(port, "127.0.0.1", () => {
  console.log("mock-upstream listening on " + server.address().port);
});
