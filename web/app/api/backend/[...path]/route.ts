import { authenticated } from "../../../../lib/auth";
import { allowedPath, sameOrigin } from "../../../../lib/session.mjs";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

async function proxy(request: Request, context: { params: Promise<{ path: string[] }> }) {
  if (!(await authenticated())) return Response.json({ error: "Phiên đăng nhập đã hết hạn" }, { status: 401 });
  const readOnly = request.method === "GET" || request.method === "HEAD";
  if (!readOnly && !sameOrigin(request)) return Response.json({ error: "Yêu cầu không hợp lệ" }, { status: 403 });
  const path = (await context.params).path.join("/");
  if (!allowedPath(readOnly ? "GET" : request.method, path)) return Response.json({ error: "Không có thao tác này" }, { status: 404 });
  const headers: Record<string, string> = { Authorization: `Bearer ${process.env.ADMIN_TOKEN}` };
  const range = request.headers.get("range"); if (range) headers.Range = range;
  let body;
  if (!readOnly) {
    body = await request.text();
    if (new TextEncoder().encode(body).length > (path === "sources/import" ? 1048576 : 32768)) return Response.json({ error: "Yêu cầu quá lớn" }, { status: 413 });
    headers["Content-Type"] = "application/json";
  }
  const articleParams = new URLSearchParams();
  if ((path === "articles" || path === "videos") && readOnly) {
    const incoming = new URL(request.url).searchParams;
    for (const key of (path === "videos" ? ["q", "status"] : ["q", "source_id", "source_ids", "category", "offset", "usage"])) {
      const value = incoming.get(key); if (value !== null) articleParams.set(key, value);
    }
  }
  const query = articleParams.size ? `?${articleParams}` : "";
  try {
    const upstream = await fetch(`${(process.env.API_INTERNAL_URL || "http://127.0.0.1:8080").replace(/\/$/, "")}/api/${path}${query}`, {
      method: request.method, headers, body, cache: "no-store", redirect: "error", signal: AbortSignal.timeout(path.startsWith("media/") ? 180000 : 45000),
    });
    const resultHeaders = new Headers({ "Cache-Control": "no-store" });
    for (const key of ["content-type", "content-length", "content-range", "accept-ranges", "content-disposition"]) {
      const value = upstream.headers.get(key); if (value) resultHeaders.set(key, value);
    }
    if (path.startsWith("videos/") && path.endsWith("/file") && upstream.ok) {
      resultHeaders.set("Content-Disposition", new URL(request.url).searchParams.get("download") === "1" ? 'attachment; filename="newsbot.mp4"' : 'inline; filename="newsbot.mp4"');
    }
    return new Response(upstream.body, { status: upstream.status, headers: resultHeaders });
  } catch {
    return Response.json({ error: "Không kết nối được Go API. Kiểm tra dịch vụ api và thử lại." }, { status: 502 });
  }
}

export { proxy as GET, proxy as HEAD, proxy as POST, proxy as PATCH, proxy as DELETE };
