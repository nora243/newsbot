import { cookies } from "next/headers";
import { createSession, equalToken, sameOrigin, SESSION_COOKIE, SESSION_SECONDS } from "../../../lib/session.mjs";

export const runtime = "nodejs";

export async function POST(request: Request) {
  if (!sameOrigin(request)) return Response.json({ error: "Yêu cầu không hợp lệ" }, { status: 403 });
  const token = process.env.ADMIN_TOKEN;
  if (!token || token.length < 24) return Response.json({ error: "Dashboard chưa cấu hình ADMIN_TOKEN" }, { status: 503 });
  const body = await request.text();
  if (body.length > 4096) return Response.json({ error: "Yêu cầu quá lớn" }, { status: 413 });
  let input;
  try { input = JSON.parse(body); } catch { return Response.json({ error: "Yêu cầu không hợp lệ" }, { status: 400 }); }
  if (!equalToken(input?.token, token)) return Response.json({ error: "Mã quản trị không đúng" }, { status: 401 });
  (await cookies()).set(SESSION_COOKIE, createSession(token), {
    httpOnly: true, sameSite: "strict", secure: new URL(process.env.WEB_ORIGIN || request.url).protocol === "https:",
    path: "/", maxAge: SESSION_SECONDS,
  });
  return Response.json({ ok: true }, { headers: { "Cache-Control": "no-store" } });
}

export async function DELETE(request: Request) {
  if (!sameOrigin(request)) return Response.json({ error: "Yêu cầu không hợp lệ" }, { status: 403 });
  (await cookies()).delete(SESSION_COOKIE);
  return Response.json({ ok: true });
}
