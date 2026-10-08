import { test } from "node:test";
import assert from "node:assert/strict";
import { allowedPath, createSession, equalToken, SESSION_SECONDS, validSession } from "../lib/session.mjs";

test("session expires, rejects tampering and contains no admin token", () => {
  const secret = "test-admin-token-at-least-24-characters";
  const now = Date.now();
  const cookie = createSession(secret, now);
  assert.equal(cookie.includes(secret), false);
  assert.equal(validSession(cookie, secret, now), true);
  assert.equal(validSession(cookie, secret, now + SESSION_SECONDS * 1000), false);
  assert.equal(validSession(cookie, "different-key-with-more-than-24-characters", now), false);
  assert.equal(validSession(cookie.replace(/\d/, "0"), secret, now), false);
  assert.equal(validSession(undefined, secret, now), false);
  assert.equal(equalToken(secret, secret), true);
  assert.equal(equalToken("", secret), false);
});

test("proxy allows only supported API operations and safe IDs", () => {
  const id = "a".repeat(32);
  for (const [method, path] of [["GET", "overview"], ["GET", `videos/${id}/file`], ["POST", `videos/${id}/revisions`], ["PATCH", "sources/12"], ["POST", "sources/12/collect"]]) assert.equal(allowedPath(method, path), true);
  for (const [method, path] of [["GET", "../healthz"], ["GET", "https://example.com"], ["POST", "videos/invalid/review"], ["DELETE", "sources/1"], ["GET", "sources/1/collect"], ["POST", "sources/0/collect"]]) assert.equal(allowedPath(method, path), false);
});
