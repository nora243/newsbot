"use client";

import { useCallback, useEffect, useState } from "react";

type Source = { id: number; name: string; rss_url: string; language: string; category: string; active: boolean; license_notes: string; last_error: string | null; last_fetched_at: string | null };
type Article = { id: number; source_id: number; source_name: string; url: string; title: string; content: string; published_at: string | null };
type Script = { title: string; summary: string; uncertainties: string[]; claims: { id: string; text: string; evidence: { article_id: number; quote: string }[] }[]; scenes: { narration: string; subtitle: string; claim_ids: string[] }[] };
type Video = { id: string; status: string; script: Script | null; fact_check: { scenes: { scene_index: number; supported: boolean; reason: string }[] } | null; sources?: Article[]; last_error: string | null; duration_seconds: number | null; created_at: string; parent_id: string | null };
type Overview = { stats: { sources: number; active_sources: number; articles: number; videos: number; pending_review: number; processing: number; failed: number }; ai: { provider: string; model: string; tts_provider: string; tts_model: string; voice: string; ready: boolean } };
type Tab = "overview" | "articles" | "videos" | "sources";

const labels: Record<string, string> = { queued: "Trong hàng đợi", processing: "Đang sản xuất", pending_review: "Chờ duyệt", approved: "Đã duyệt", rejected: "Đã từ chối", failed: "Thất bại" };
const categories: Record<string, string> = { TECHNOLOGY: "Công nghệ", WORLD: "Thế giới", BUSINESS: "Kinh tế", VIETNAM: "Việt Nam", SCIENCE: "Khoa học", SPORTS: "Thể thao", GENERAL: "Tổng hợp" };
const date = (s?: string | null) => s ? new Intl.DateTimeFormat("vi-VN", { timeZone: "Asia/Bangkok", day: "2-digit", month: "2-digit", hour: "2-digit", minute: "2-digit" }).format(new Date(s)) : "Chưa có";
const fileURL = (id: string) => `/api/backend/videos/${id}/file`;

function Badge({ status }: { status: string }) { return <span className={`badge ${status}`}>{labels[status] || status}</span>; }
function Empty({ title, children }: { title: string; children: React.ReactNode }) { return <div className="empty"><span className="empty-icon">◇</span><h3>{title}</h3><p>{children}</p></div>; }

export default function Dashboard({ initialAuthenticated }: { initialAuthenticated: boolean }) {
  const [authenticated, setAuthenticated] = useState(initialAuthenticated);
  const [token, setToken] = useState("");
  const [tab, setTab] = useState<Tab>("overview");
  const [overview, setOverview] = useState<Overview | null>(null);
  const [sources, setSources] = useState<Source[]>([]);
  const [articles, setArticles] = useState<Article[]>([]);
  const [videos, setVideos] = useState<Video[]>([]);
  const [selected, setSelected] = useState<number[]>([]);
  const [selectedVideo, setSelectedVideo] = useState<string | null>(null);
  const [detail, setDetail] = useState<Video | null>(null);
  const [draft, setDraft] = useState<Script | null>(null);
  const [editing, setEditing] = useState(false);
  const [sourceEdit, setSourceEdit] = useState<Source | null>(null);
  const [newSource, setNewSource] = useState({ name: "", rss_url: "", language: "vi", category: "TECHNOLOGY", license_notes: "" });
  const [search, setSearch] = useState("");
  const [category, setCategory] = useState("");
  const [status, setStatus] = useState("");
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [loading, setLoading] = useState(false);
  const [updatedAt, setUpdatedAt] = useState<string | null>(null);

  const api = useCallback(async <T,>(path: string, options?: RequestInit): Promise<T> => {
    const response = await fetch(`/api/backend/${path}`, { ...options, headers: { "Content-Type": "application/json", ...options?.headers }, cache: "no-store" });
    const data = await response.json();
    if (response.status === 401) { setAuthenticated(false); setDetail(null); setEditing(false); }
    if (!response.ok) throw new Error(data.error || "Thao tác chưa thành công");
    return data;
  }, []);

  const refresh = useCallback(async () => {
    const [o, s, a, v] = await Promise.all([api<Overview>("overview"), api<Source[]>("sources"), api<Article[]>("articles"), api<Video[]>("videos")]);
    setOverview(o); setSources(s); setArticles(a); setVideos(v); setUpdatedAt(new Date().toISOString());
  }, [api]);

  useEffect(() => {
    if (!authenticated) return;
    let stopped = false; let running = false;
    const load = async () => {
      if (running || stopped) return; running = true;
      try { await refresh(); } catch (e) { if (!stopped) setError((e as Error).message); }
      finally { running = false; if (!stopped) setLoading(false); }
    };
    setLoading(true); void load();
    const interval = setInterval(() => { if (document.visibilityState === "visible") void load(); }, 10000);
    return () => { stopped = true; clearInterval(interval); };
  }, [authenticated, refresh]);

  useEffect(() => {
    if (!authenticated || !selectedVideo) return;
    let stopped = false; let running = false;
    setDetail(null); setEditing(false); setDraft(null);
    const load = async () => {
      if (stopped || running) return; running = true;
      try { const v = await api<Video>(`videos/${selectedVideo}`); if (!stopped) setDetail(v); }
      catch (e) { if (!stopped) setError((e as Error).message); } finally { running = false; }
    };
    void load(); const interval = setInterval(() => { if (document.visibilityState === "visible") void load(); }, 10000);
    return () => { stopped = true; clearInterval(interval); };
  }, [authenticated, selectedVideo, api]);

  async function action(name: string, fn: () => Promise<void>) {
    if (busy) return; setBusy(name); setError(""); setNotice("");
    try { await fn(); } catch (e) { setError((e as Error).message); } finally { setBusy(""); }
  }

  async function login(e: React.FormEvent) {
    e.preventDefault(); const credential = token; setToken("");
    await action("login", async () => {
      const response = await fetch("/api/session", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ token: credential }) });
      if (!response.ok) throw new Error((await response.json()).error);
      setAuthenticated(true);
    });
  }

  async function logout() {
    await action("logout", async () => {
      const response = await fetch("/api/session", { method: "DELETE" });
      if (!response.ok) throw new Error("Chưa đăng xuất được");
      setAuthenticated(false); setDetail(null); setSelectedVideo(null); setSelected([]);
    });
  }

  async function generate() {
    await action("generate", async () => {
      const v = await api<{ id: string }>("videos", { method: "POST", body: JSON.stringify({ article_ids: selected }) });
      setSelected([]); setTab("videos"); setSelectedVideo(v.id); setNotice("Đã đưa video vào hàng đợi sản xuất."); await refresh();
    });
  }

  async function review(decision: string) {
    if (!detail) return;
    await action("review", async () => {
      await api(`videos/${detail.id}/review`, { method: "POST", body: JSON.stringify({ decision }) });
      setDetail(await api<Video>(`videos/${detail.id}`)); setNotice(decision === "approved" ? "Đã duyệt video. Bạn có thể tải để đăng." : "Đã từ chối video."); await refresh();
    });
  }

  if (!authenticated) return <main className="login-shell"><div className="login-card"><div className="brand-mark">N</div><p className="eyebrow">NEWSBOT / PHÒNG BIÊN TẬP</p><h1>Từ nguồn tin<br />đến bản tin của bạn.</h1><p className="muted">Đăng nhập để quản lý nguồn tin, sản xuất và duyệt video.</p><form onSubmit={login}><label>Mã quản trị<input type="password" autoComplete="current-password" value={token} onChange={e => setToken(e.target.value)} required placeholder="Nhập ADMIN_TOKEN trong .env" /></label><button className="primary" disabled={!!busy}>{busy ? "Đang đăng nhập…" : "Vào phòng biên tập →"}</button></form>{error && <p role="alert" className="alert error">{error}</p>}<p className="fine">Phiên đăng nhập có hiệu lực trong 12 giờ.</p></div><div className="login-art"><div className="orb" /><p>THU THẬP. KIỂM CHỨNG.<br /><strong>KỂ MỘT CÂU CHUYỆN.</strong></p></div></main>;

  const filteredArticles = articles.filter(a => `${a.title} ${a.source_name}`.toLocaleLowerCase("vi").includes(search.toLocaleLowerCase("vi")) && (!category || sources.find(s => s.id === a.source_id)?.category === category));
  const filteredVideos = videos.filter(v => !status || v.status === status);
  const stats = overview?.stats;
  const availableFile = detail && ["pending_review", "approved", "rejected"].includes(detail.status);

  return <div className="app-shell">
    <aside className="sidebar"><a className="brand" href="/"><span className="brand-mark small">N</span><span>newsbot<span className="brand-sub">PHÒNG BIÊN TẬP</span></span></a><p className="nav-label">KHÔNG GIAN LÀM VIỆC</p><nav aria-label="Điều hướng chính">{([ ["overview", "◫", "Tổng quan"], ["articles", "▤", "Tin đã thu thập"], ["videos", "▷", "Sản xuất video"], ["sources", "◎", "Nguồn tin"] ] as const).map(([id, icon, name]) => <button key={id} className={`nav-item ${tab === id ? "active" : ""}`} onClick={() => setTab(id)}><span aria-hidden="true">{icon}</span>{name}{id === "videos" && !!stats?.pending_review && <span className="nav-count">{stats.pending_review}</span>}</button>)}</nav><div className="sidebar-bottom"><span className="live-dot" /> Duyệt trước khi xuất bản<p>Video gốc. Nguồn rõ ràng.</p><button className="text-button" disabled={!!busy} onClick={() => void logout()}>Đăng xuất ↗</button></div></aside>
    <main className="main"><header className="topbar"><span>NEWSROOM <span className="divider">/</span> { { overview: "Tổng quan", articles: "Tin đã thu thập", videos: "Sản xuất video", sources: "Nguồn tin" }[tab]}</span><div className="topbar-actions"><button className="secondary compact" disabled={loading || !!busy} onClick={() => void action("refresh", refresh)}>↻ Làm mới</button><button className="text-button" disabled={!!busy} onClick={() => void logout()}>Đăng xuất</button></div></header>
      <div className="content"><div className="page-heading"><div><p className="eyebrow">BIÊN TẬP CÓ KIỂM CHỨNG</p><h1>{{ overview: "Một góc nhìn, mọi bản tin.", articles: "Chọn tin để kể.", videos: "Từ kịch bản đến màn hình.", sources: "Bắt đầu từ nguồn đáng tin." }[tab]}</h1><p className="muted">{{ overview: "Theo dõi nguồn tin và những video đang chờ quyết định của bạn.", articles: "Chọn tối đa 6 bài cùng sự kiện để tổng hợp thành một video.", videos: "Xem lại lời đọc, đối chiếu nguồn và duyệt từng bản tin.", sources: "Thêm RSS, ghi quyền sử dụng và chủ động bật từng nguồn." }[tab]}</p></div><span className="fine updated">Cập nhật {date(updatedAt)}</span></div>
      {error && <div className="alert error" role="alert">{error}<button aria-label="Đóng thông báo lỗi" onClick={() => setError("")}>×</button></div>}{notice && <div className="alert success" role="status">{notice}<button aria-label="Đóng thông báo" onClick={() => setNotice("")}>×</button></div>}
      {loading && !overview && <p className="muted" role="status">Đang tải phòng biên tập…</p>}

      {tab === "overview" && <>
        <div className="stats-grid">{[["Nguồn đang bật", stats?.active_sources, "sources"], ["Tin đã thu thập", stats?.articles, "articles"], ["Video chờ duyệt", stats?.pending_review, "videos"], ["Đang sản xuất", stats?.processing, "videos"]].map(([label, value, target], i) => <button key={String(label)} className={`stat-card stat-${i}`} onClick={() => { setTab(target as Tab); if (i === 2) setStatus("pending_review"); else if (i === 3) setStatus(""); }}><span>{label}</span><strong>{value ?? "—"}</strong><span className="stat-footer">Mở danh sách <span>↗</span></span></button>)}</div>
        <div className="overview-grid"><section className="panel"><div className="panel-heading"><h2>Bàn duyệt video</h2><button className="text-button" onClick={() => setTab("videos")}>Xem tất cả →</button></div>{videos.filter(v => v.status === "pending_review").length ? videos.filter(v => v.status === "pending_review").slice(0, 5).map(v => <button key={v.id} className="review-row" onClick={() => { setSelectedVideo(v.id); setStatus(""); setTab("videos"); }}><span className="video-thumb">▷</span><span><strong>{v.script?.title || "Video mới"}</strong><small>{date(v.created_at)} · {Math.round(v.duration_seconds || 0)} giây</small></span><Badge status={v.status} /></button>) : <Empty title="Bàn duyệt đang trống">Khi video sản xuất xong, bạn sẽ thấy bản xem trước ở đây.</Empty>}</section><section className="panel production-card"><p className="eyebrow">DÂY CHUYỀN SẢN XUẤT</p><h2>Tin có nguồn.<br />Video có người duyệt.</h2><ol className="pipeline-list"><li>Thu thập RSS</li><li>Kịch bản & kiểm tra dữ kiện</li><li>Giọng đọc & video</li><li>Bạn quyết định xuất bản</li></ol><button className="primary" onClick={() => setTab("articles")}>Chọn tin đầu tiên →</button></section></div>
        <section className="panel provider-panel"><span className={`live-dot ${overview?.ai.ready ? "" : "offline"}`} /><div><strong>{overview?.ai.ready ? "AI đã được cấu hình" : "Chưa đủ khóa API cho pipeline"}</strong><p className="muted">Kịch bản: {overview?.ai.provider} / {overview?.ai.model} · Giọng đọc: {overview?.ai.tts_provider} / {overview?.ai.voice}</p></div><span className="fine">Đổi cấu hình trong .env</span></section>
      </>}

      {tab === "articles" && <section className="panel"><div className="toolbar"><input aria-label="Tìm tin" placeholder="Tìm tiêu đề hoặc tên nguồn…" value={search} onChange={e => setSearch(e.target.value)} /><select aria-label="Lọc chủ đề" value={category} onChange={e => setCategory(e.target.value)}><option value="">Mọi chủ đề</option>{Array.from(new Set(sources.map(s => s.category))).map(c => <option key={c} value={c}>{categories[c] || c}</option>)}</select></div><div className="selection-bar"><span><strong>{selected.length}/6</strong> bài được chọn</span><div><button className="text-button" disabled={!selected.length || !!busy} onClick={() => setSelected([])}>Bỏ chọn</button><button className="primary compact" disabled={!selected.length || !!busy || !overview?.ai.ready} onClick={() => void generate()}>{busy === "generate" ? "Đang tạo…" : "Tạo video →"}</button></div></div>{!filteredArticles.length ? <Empty title={articles.length ? "Không tìm thấy tin phù hợp" : "Chưa có tin nào"}>{articles.length ? "Thử thay đổi từ khóa hoặc chủ đề." : "Bật một nguồn ở mục Nguồn tin, rồi chọn Thu thập ngay."}</Empty> : <div className="article-list">{filteredArticles.map(a => <article key={a.id} className={`article-row ${selected.includes(a.id) ? "selected" : ""}`}><input type="checkbox" aria-label={`Chọn ${a.title}`} checked={selected.includes(a.id)} disabled={!!busy || (!selected.includes(a.id) && selected.length >= 6)} onChange={e => setSelected(prev => e.target.checked ? [...prev, a.id] : prev.filter(id => id !== a.id))} /><div><div className="article-meta"><span>{a.source_name}</span><span>{date(a.published_at)}</span></div><h3><a href={a.url} target="_blank" rel="noreferrer">{a.title} ↗</a></h3><p>{a.content || "Nguồn chưa cung cấp mô tả."}</p></div></article>)}</div>}<p className="list-note">Hiển thị tối đa 100 bài mới nhất. Các bài được chọn cần nói về cùng một sự kiện.</p></section>}

      {tab === "sources" && <div className="sources-layout"><section className="panel"><div className="panel-heading"><h2>Danh sách nguồn</h2><span className="fine">{sources.length} nguồn</span></div>{sources.length ? sources.map(s => <article key={s.id} className="source-row"><div className="source-title"><span className={`live-dot ${s.active ? "" : "offline"}`} /><h3>{s.name}</h3><span className="badge">{s.active ? "Đang bật" : "Đã tắt"}</span></div><p className="source-url">{s.rss_url}</p><p className="fine">{categories[s.category] || s.category} · {s.language.toUpperCase()} · Thu thập: {date(s.last_fetched_at)}</p>{s.last_error && <p className="source-error">{s.last_error}</p>}<div className="source-actions"><button className="secondary compact" disabled={!!busy} onClick={() => setSourceEdit({ ...s })}>Thiết lập</button><button className="text-button" disabled={!s.active || !!busy} onClick={() => void action(`collect-${s.id}`, async () => { const result = await api<{ inserted: number }>(`sources/${s.id}/collect`, { method: "POST", body: "{}" }); setNotice(`Đã thêm ${result.inserted} bài mới từ ${s.name}.`); await refresh(); })}>{busy === `collect-${s.id}` ? "Đang thu thập…" : "Thu thập ngay ↗"}</button></div></article>) : <Empty title="Chưa có nguồn">Thêm nguồn RSS ở biểu mẫu bên cạnh.</Empty>}</section>
        <section className="panel source-form"><h2>{sourceEdit ? `Thiết lập ${sourceEdit.name}` : "Thêm nguồn RSS"}</h2>{sourceEdit ? <form onSubmit={e => { e.preventDefault(); void action("source-save", async () => { await api(`sources/${sourceEdit.id}`, { method: "PATCH", body: JSON.stringify({ active: sourceEdit.active, license_notes: sourceEdit.license_notes }) }); setSourceEdit(null); setNotice("Đã lưu cấu hình nguồn."); await refresh(); }); }}><label className="check-label"><input type="checkbox" checked={sourceEdit.active} onChange={e => setSourceEdit({ ...sourceEdit, active: e.target.checked })} />Bật thu thập nguồn này</label><label>Ghi chú quyền sử dụng<textarea required minLength={sourceEdit.active ? 10 : undefined} rows={5} value={sourceEdit.license_notes} onChange={e => setSourceEdit({ ...sourceEdit, license_notes: e.target.value })} /></label><p className="fine">Ghi căn cứ sử dụng thực tế trước khi bật nguồn.</p><button className="primary" disabled={!!busy}>Lưu thiết lập</button><button type="button" className="text-button" onClick={() => setSourceEdit(null)}>Hủy</button></form> : <form onSubmit={e => { e.preventDefault(); void action("source-add", async () => { await api("sources", { method: "POST", body: JSON.stringify(newSource) }); setNewSource({ name: "", rss_url: "", language: "vi", category: "TECHNOLOGY", license_notes: "" }); setNotice("Đã thêm nguồn. Mở Thiết lập để bật thu thập."); await refresh(); }); }}><label>Tên nguồn<input required maxLength={200} value={newSource.name} onChange={e => setNewSource({ ...newSource, name: e.target.value })} placeholder="Tên báo hoặc nguồn của bạn" /></label><label>Địa chỉ RSS<input type="url" required value={newSource.rss_url} onChange={e => setNewSource({ ...newSource, rss_url: e.target.value })} placeholder="https://…/feed.xml" /></label><div className="form-columns"><label>Ngôn ngữ<select value={newSource.language} onChange={e => setNewSource({ ...newSource, language: e.target.value })}><option value="vi">Tiếng Việt</option><option value="en">Tiếng Anh</option></select></label><label>Chủ đề<select value={newSource.category} onChange={e => setNewSource({ ...newSource, category: e.target.value })}>{Object.entries(categories).map(([id, label]) => <option key={id} value={id}>{label}</option>)}</select></label></div><label>Ghi chú quyền sử dụng<textarea rows={3} value={newSource.license_notes} onChange={e => setNewSource({ ...newSource, license_notes: e.target.value })} placeholder="Điều khoản hoặc thỏa thuận sử dụng nguồn" /></label><button className="primary" disabled={!!busy}>Thêm nguồn →</button><p className="fine">Nguồn mới mặc định tắt.</p></form>}</section></div>}

      {tab === "videos" && <div className={`videos-layout ${selectedVideo ? "with-detail" : ""}`}><section className="panel video-list"><div className="toolbar"><h2>Thư viện video</h2><select aria-label="Lọc trạng thái video" value={status} onChange={e => setStatus(e.target.value)}><option value="">Mọi trạng thái</option>{Object.entries(labels).map(([id, label]) => <option key={id} value={id}>{label}</option>)}</select></div>{filteredVideos.length ? filteredVideos.map(v => <button key={v.id} className={`video-row ${selectedVideo === v.id ? "selected" : ""}`} onClick={() => setSelectedVideo(v.id)}><span className="video-thumb">▷</span><span className="video-row-body"><strong>{v.script?.title || "Đang chuẩn bị kịch bản…"}</strong><small>{date(v.created_at)}{v.parent_id ? " · Bản chỉnh sửa" : ""}</small><Badge status={v.status} /></span><span>→</span></button>) : <Empty title="Chưa có video phù hợp">Chọn bài trong mục Tin đã thu thập để bắt đầu sản xuất.</Empty>}<p className="list-note">Tối đa 100 video gần nhất.</p></section>
        {selectedVideo && <section className="panel video-detail"><div className="panel-heading"><h2>Chi tiết bản tin</h2><button className="text-button" onClick={() => setSelectedVideo(null)}>Đóng ×</button></div>{!detail ? <p role="status" className="muted">Đang tải video…</p> : <><div className="detail-heading"><h2>{detail.script?.title || "Video đang sản xuất"}</h2><Badge status={detail.status} /></div>{availableFile ? <div className="player"><video key={detail.id} controls preload="metadata" src={fileURL(detail.id)} aria-label="Xem trước video" /></div> : <div className="video-placeholder"><span>▷</span><p>{detail.status === "failed" ? "Video chưa render thành công" : "Worker đang chuẩn bị bản tin"}</p></div>}{detail.last_error && <p className="alert error">{detail.last_error}</p>}<div className="detail-actions">{availableFile && <a className="secondary compact" href={`${fileURL(detail.id)}?download=1`}>↓ Tải MP4</a>}{detail.status === "pending_review" && !editing && <><button className="primary compact" disabled={!!busy} onClick={() => void review("approved")}>✓ Duyệt</button><button className="danger compact" disabled={!!busy} onClick={() => void review("rejected")}>Từ chối</button></>}{detail.script && !["queued", "processing"].includes(detail.status) && !editing && <button className="text-button" onClick={() => { setDraft(structuredClone(detail.script!)); setEditing(true); }}>Sửa kịch bản</button>}</div>
          {editing && draft ? <form className="edit-script" onSubmit={e => { e.preventDefault(); void action("revision", async () => { const v = await api<{ id: string }>(`videos/${detail.id}/revisions`, { method: "POST", body: JSON.stringify({ script: draft }) }); setEditing(false); setSelectedVideo(v.id); setNotice("Đã tạo phiên bản mới. Kịch bản sẽ được kiểm tra lại trước khi render."); await refresh(); }); }}><h3>Chỉnh sửa kịch bản</h3><p className="fine">Lưu sẽ tạo video mới và kiểm tra lại dữ kiện. Giữ nguyên video hiện tại.</p><label>Tiêu đề<input required maxLength={160} value={draft.title} onChange={e => setDraft({ ...draft, title: e.target.value })} /></label>{draft.scenes.map((scene, i) => <label key={i}>Lời đọc cảnh {i + 1}<textarea required rows={4} value={scene.narration} onChange={e => setDraft({ ...draft, scenes: draft.scenes.map((s, n) => n === i ? { ...s, narration: e.target.value, subtitle: e.target.value } : s) })} /></label>)}<p className="fine">{draft.scenes.reduce((n, s) => n + s.narration.trim().split(/\s+/).filter(Boolean).length, 0)} / 70–240 từ · Phụ đề cập nhật theo lời đọc.</p><button className="primary" disabled={!!busy}>Lưu và tạo phiên bản mới →</button><button type="button" className="text-button" onClick={() => setEditing(false)}>Hủy chỉnh sửa</button></form> : detail.script && <div className="script-view"><h3>Kịch bản</h3><p className="muted">{detail.script.summary}</p>{detail.script.scenes.map((s, i) => <div key={i} className="scene"><span>CẢNH {String(i + 1).padStart(2, "0")}</span><p>{s.narration}</p>{detail.fact_check?.scenes.find(c => c.scene_index === i) && <small className={detail.fact_check.scenes.find(c => c.scene_index === i)?.supported ? "supported" : "unsupported"}>{detail.fact_check.scenes.find(c => c.scene_index === i)?.supported ? "✓ Được nguồn hỗ trợ" : "✕ Chưa được nguồn hỗ trợ"} · {detail.fact_check.scenes.find(c => c.scene_index === i)?.reason}</small>}</div>)}{detail.script.uncertainties.length > 0 && <div className="uncertainties"><h3>Thông tin còn chưa rõ</h3><ul>{detail.script.uncertainties.map((u, i) => <li key={i}>{u}</li>)}</ul></div>}</div>}
          {!!detail.script?.claims.length && <details className="evidence"><summary>Dữ kiện và trích dẫn ({detail.script.claims.length})</summary>{detail.script.claims.map(c => <div key={c.id}><p><strong>{c.text}</strong></p>{c.evidence.map((e, i) => <blockquote key={i}>{e.quote}<cite>Bài #{e.article_id}</cite></blockquote>)}</div>)}</details>}<div className="provenance"><h3>Nguồn của video</h3>{detail.sources?.map(s => <a key={s.id} href={s.url} target="_blank" rel="noreferrer"><strong>{s.source_name}</strong><span>{s.title} ↗</span></a>)}<p className="fine">Kết quả AI chỉ đối chiếu nguồn đầu vào. Hãy xem bài gốc trước khi duyệt.</p>{detail.parent_id && <button className="text-button" onClick={() => setSelectedVideo(detail.parent_id)}>← Xem phiên bản trước</button>}</div></>}</section>}
      </div>}
      </div><footer className="footer">NEWSBOT <span>Thu thập → Kiểm chứng → Sản xuất → Duyệt</span></footer>
    </main>
  </div>;
}
