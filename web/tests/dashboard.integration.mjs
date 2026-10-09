import { test } from "node:test";
import assert from "node:assert/strict";
import http from "node:http";
import { spawn } from "node:child_process";

test("production dashboard login, authenticated proxy, video ranges and logout", { timeout: 60000 }, async () => {
  const token = "integration-test-admin-token-32-chars";
  const id = "a".repeat(32);
  let upstreamCalls = 0;
  let lastUpstreamURL = "";
  const backend = http.createServer(async (req, res) => {
    upstreamCalls++; lastUpstreamURL = req.url;
    assert.equal(req.headers.authorization, `Bearer ${token}`);
    assert.equal(req.headers.cookie, undefined);
    if (req.url === `/api/videos/${id}/file`) {
      assert.equal(req.headers.range, "bytes=0-3");
      res.writeHead(206, { "Content-Type": "video/mp4", "Content-Range": "bytes 0-3/12", "Content-Length": "4", "Accept-Ranges": "bytes" });
      res.end("test"); return;
    }
    res.setHeader("Content-Type", "application/json");
    if (req.url === "/api/overview") res.end(JSON.stringify({ stats: { active_sources: 0, articles: 0, pending_review: 0, processing: 0 }, ai: { ready: true, provider: "google", model: "test", tts_provider: "openai", voice: "coral" } }));
    else if (req.method === "POST") { let body = ""; for await (const chunk of req) body += chunk; res.end(JSON.stringify({ id, received: JSON.parse(body) })); }
    else res.end("[]");
  });
  await new Promise(resolve => backend.listen(0, "127.0.0.1", resolve));
  const backendPort = backend.address().port;
  // Reserve an ephemeral port, release it, then launch the production server there.
  const reservation = http.createServer(); await new Promise(resolve => reservation.listen(0, "127.0.0.1", resolve));
  const webPort = reservation.address().port; await new Promise(resolve => reservation.close(resolve));
  const origin = `http://127.0.0.1:${webPort}`;
  const server = spawn(process.execPath, [".next/standalone/server.js"], { env: { ...process.env, PORT: String(webPort), HOSTNAME: "127.0.0.1", ADMIN_TOKEN: token, WEB_ORIGIN: origin, API_INTERNAL_URL: `http://127.0.0.1:${backendPort}`, NEXT_TELEMETRY_DISABLED: "1" }, stdio: ["ignore", "pipe", "pipe"] });
  let log = ""; server.stdout.on("data", b => log += b); server.stderr.on("data", b => log += b);
  try {
    let ready = false;
    for (let i = 0; i < 100; i++) {
      try { const r = await fetch(origin); if (r.ok) { ready = true; break; } } catch {}
      await new Promise(resolve => setTimeout(resolve, 200));
    }
    assert.equal(ready, true, log);
    const loginPage = await (await fetch(origin)).text(); assert.match(loginPage, /Mã quản trị/);
    assert.equal((await fetch(`${origin}/api/backend/overview`)).status, 401);
    assert.equal(upstreamCalls, 0);
    const login = (credential, requestOrigin = origin) => fetch(`${origin}/api/session`, { method: "POST", headers: { "Content-Type": "application/json", Origin: requestOrigin }, body: JSON.stringify({ token: credential }) });
    assert.equal((await login(token, "https://other.example")).status, 403);
    assert.equal((await login("wrong")).status, 401);
    const response = await login(token); assert.equal(response.status, 200);
    const setCookie = response.headers.get("set-cookie"); assert.match(setCookie, /HttpOnly/i); assert.match(setCookie, /SameSite=strict/i);
    const cookie = setCookie.split(";")[0]; assert.equal(cookie.includes(token), false);
    const headers = { Cookie: cookie, Origin: origin, "Content-Type": "application/json" };
    const overview = await fetch(`${origin}/api/backend/overview`, { headers }); assert.equal(overview.status, 200); assert.equal((await overview.json()).ai.provider, "google");
    assert.equal(overview.headers.get("cache-control"), "no-store");
    const filters = new URLSearchParams({ q: "Hòa bình", source_id: "2", category: "WORLD", offset: "100" });
    const articles = await fetch(`${origin}/api/backend/articles?${filters}&ignored=1`, { headers });
    assert.equal(articles.status, 200);
    assert.equal(lastUpstreamURL, `/api/articles?${filters}`);
    const multiFilters = new URLSearchParams({ source_id: "1,2" });
    const multiArticles = await fetch(`${origin}/api/backend/articles?${multiFilters}`, { headers });
    assert.equal(multiArticles.status, 200);
    assert.equal(lastUpstreamURL, `/api/articles?${multiFilters}`);
    const dashboard = await (await fetch(origin, { headers })).text(); assert.match(dashboard, /Một góc nhìn/); assert.equal(dashboard.includes(token), false);
    assert.equal((await fetch(`${origin}/api/backend/sources`, { method: "POST", headers: { ...headers, Origin: "https://other.example" }, body: "{}" })).status, 403);
    const revision = await fetch(`${origin}/api/backend/videos/${id}/revisions`, { method: "POST", headers, body: JSON.stringify({ script: { title: "Revised" } }) });
    assert.equal(revision.status, 200); assert.equal((await revision.json()).received.script.title, "Revised");
    const media = await fetch(`${origin}/api/backend/videos/${id}/file`, { headers: { ...headers, Range: "bytes=0-3" } });
    assert.equal(media.status, 206); assert.equal(media.headers.get("content-range"), "bytes 0-3/12"); assert.match(media.headers.get("content-disposition"), /inline/); assert.equal(await media.text(), "test");
    const preview = await fetch(`${origin}/api/backend/media/${id}/preview`, { headers }); assert.equal(preview.status,200); assert.equal(preview.headers.get("content-disposition"),null); assert.equal(lastUpstreamURL,`/api/media/${id}/preview`);
    assert.equal((await fetch(`${origin}/api/backend/secrets`, { headers })).status, 404);
    assert.equal((await fetch(`${origin}/api/backend/videos?q=Target&status=approved`, { headers })).status, 200);
    assert.equal(lastUpstreamURL, "/api/videos?q=Target&status=approved");
    assert.equal((await fetch(`${origin}/api/backend/sources/12`, { method: "DELETE", headers })).status, 200);
    assert.equal(lastUpstreamURL, "/api/sources/12");
    assert.equal((await fetch(`${origin}/api/backend/sources/12`, { method: "DELETE", headers: { ...headers, Origin: "https://foreign.example" } })).status, 403);
    const logout = await fetch(`${origin}/api/session`, { method: "DELETE", headers }); assert.equal(logout.status, 200); assert.match(logout.headers.get("set-cookie"), /newsbot_session=;/);
  } finally {
    server.kill("SIGTERM");
    await new Promise(resolve => backend.close(resolve));
  }
});
