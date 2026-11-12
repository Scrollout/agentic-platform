// contract-lint: Spectral behind a small HTTP front (T038e, research D28). Spectral has no server
// mode, so this file is the whole service. It lints slice contracts with OpenAPI's standard rules
// (spectral:oas, recommended), never the engine's own ruleset.
//
//   POST /lint     Authorization: Bearer $LINT_TOKEN; body: the contract (YAML or JSON), at most
//                  1 MiB. Answers 200 with [{level, code, text, path}], level being error, warn,
//                  info or hint; 401 without the token, 413 above 1 MiB, 405 for another method,
//                  500 when Spectral fails or takes longer than 30 s.
//   GET /health    Open; answers 200. Every other path is 404.
//
// Environment: TOOLS, the folder holding the pinned node_modules (scripts/tools/package-lock.json);
// LINT_TOKEN, the bearer token. It listens on port 8080.
import http from "node:http";
import { createHash, timingSafeEqual } from "node:crypto";
import { createRequire } from "node:module";

const require = createRequire(process.env.TOOLS + "/");
const { Spectral, Document } = require("@stoplight/spectral-core");
const Parsers = require("@stoplight/spectral-parsers");
const { oas } = require("@stoplight/spectral-rulesets");
const { Resolver } = require("@stoplight/spectral-ref-resolver");

const token = process.env.LINT_TOKEN;
if (!token) throw new Error("LINT_TOKEN is not set");
const digest = (v) => createHash("sha256").update(String(v)).digest();
const wanted = digest(`Bearer ${token}`);
const authorized = (h) => timingSafeEqual(digest(h ?? ""), wanted); // constant time
const maxBody = 1 << 20; // 1 MiB
const limitMs = 30000;

// No http, https or file resolvers: a contract written by a model can only reference itself
// (#/...). Every other $ref is an invalid-ref error and nothing is read or fetched.
const spectral = new Spectral({ resolver: new Resolver({ resolvers: {} }) });
spectral.setRuleset({ extends: [[oas, "recommended"]] });
const levels = ["error", "warn", "info", "hint"]; // Spectral's severities 0..3

async function lint(text) {
  // The YAML parser reads JSON too.
  const doc = new Document(text, Parsers.Yaml, "contract.yaml");
  let timer;
  const late = new Promise((_, reject) => { timer = setTimeout(() => reject(new Error("lint took longer than 30 s")), limitMs); });
  try {
    const found = await Promise.race([spectral.run(doc), late]);
    return found.map((d) => ({ level: levels[d.severity] ?? "error", code: String(d.code), text: d.message, path: d.path.join(".") }));
  } finally {
    clearTimeout(timer);
  }
}

const server = http.createServer((req, res) => {
  const reply = (status, body, headers = {}) => {
    res.writeHead(status, { "content-type": "application/json", ...headers });
    res.end(JSON.stringify(body));
  };
  if (req.url === "/health") {
    return req.method === "GET" ? reply(200, { status: "ok" }) : reply(405, { error: "method not allowed" }, { allow: "GET" });
  }
  if (req.url !== "/lint") return reply(404, { error: "not found" });
  if (!authorized(req.headers.authorization)) return reply(401, { error: "unauthorized" });
  if (req.method !== "POST") return reply(405, { error: "method not allowed" }, { allow: "POST" });

  // Read at most 1 MiB. A larger body is read to its end and dropped, so the client gets the 413
  // instead of a reset connection; requestTimeout bounds how long that can take.
  const chunks = [];
  let size = 0;
  req.on("data", (c) => {
    size += c.length;
    if (size <= maxBody) chunks.push(c);
  });
  req.on("end", async () => {
    if (size > maxBody) return reply(413, { error: "contract above 1 MiB" });
    try {
      reply(200, await lint(Buffer.concat(chunks).toString("utf8")));
    } catch (e) {
      // The detail goes to the service's log only, never to the caller.
      console.error("lint failed:", e && e.message);
      reply(500, { error: "lint failed" });
    }
  });
});
server.requestTimeout = limitMs; // the whole request, body included, must arrive within 30 s
server.headersTimeout = 10000;
server.listen(8080);
