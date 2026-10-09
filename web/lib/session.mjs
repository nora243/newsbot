import { createHmac, randomBytes, timingSafeEqual } from "node:crypto";

export const SESSION_COOKIE = "newsbot_session";
export const SESSION_SECONDS = 12 * 60 * 60;

function signature(value, secret) {
  return createHmac("sha256", secret).update(`newsbot-session:${value}`).digest("base64url");
}

export function equalToken(input, expected) {
  if (typeof input !== "string" || typeof expected !== "string" || expected.length < 24) return false;
  // Fixed-size hashes avoid length-sensitive timing comparisons.
  const digest = (v) => createHmac("sha256", "newsbot-token-comparison").update(v).digest();
  return timingSafeEqual(digest(input), digest(expected));
}

export function createSession(secret, now = Date.now()) {
  if (!secret || secret.length < 24) throw new Error("ADMIN_TOKEN needs at least 24 characters");
  const value = `${Math.floor(now / 1000) + SESSION_SECONDS}.${randomBytes(16).toString("hex")}`;
  return `${value}.${signature(value, secret)}`;
}

export function validSession(cookie, secret, now = Date.now()) {
  if (!secret || secret.length < 24 || typeof cookie !== "string") return false;
  const match = cookie.match(/^(\d{10})\.([a-f0-9]{32})\.([a-zA-Z0-9_-]{43})$/);
  if (!match) return false;
  const expires = Number(match[1]);
  if (expires <= Math.floor(now / 1000) || expires > Math.floor(now / 1000) + SESSION_SECONDS) return false;
  const actual = Buffer.from(match[3], "base64url");
  const expected = Buffer.from(signature(`${match[1]}.${match[2]}`, secret), "base64url");
  return actual.length === expected.length && timingSafeEqual(actual, expected);
}

export function allowedPath(method, path) {
  if (method === "GET") return /^(overview|sources|articles|media\/[a-f0-9]{32}\/(file|preview)|videos|videos\/[a-f0-9]{32}(\/file)?)$/.test(path);
  if (method === "POST") return /^(sources|videos|media\/discover|sources\/[1-9][0-9]*\/collect|videos\/[a-f0-9]{32}\/(review|revisions))$/.test(path);
  if (method === "DELETE") return /^sources\/[1-9][0-9]*$/.test(path);
  if (method === "PATCH") return /^sources\/[1-9][0-9]*$/.test(path);
  return false;
}

export function sameOrigin(request) {
  const origin = request.headers.get("origin");
  const expected = process.env.WEB_ORIGIN || new URL(request.url).origin;
  return origin === expected;
}
