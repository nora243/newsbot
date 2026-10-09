"use client";

import { memo, useCallback, useEffect, useRef, useState } from "react";

type Asset = { id: string; article_id: number; kind: "image" | "video"; source_name: string; article_title: string };
type Props = { articleIDs: number[]; initialIDs?: string[] | null; submitting: boolean; error: string; onCancel: () => void; onConfirm: (ids: string[]) => void };
const mediaURL = (id: string) => `/api/backend/media/${id}/file`;

function ActiveVideo({id,title,onFail}: {id:string;title:string;onFail:(id:string)=>void}) {
 const ref=useRef<HTMLVideoElement|null>(null);
 useEffect(()=>{const video=ref.current;return ()=>{if(video){video.pause();video.removeAttribute("src");video.load()}}},[]);
 return <video ref={ref} controls autoPlay playsInline preload="none" src={mediaURL(id)} aria-label={title} onError={()=>onFail(id)} />;
}

const MediaPreview=memo(function MediaPreview({id,kind,title,active,onPlay,onFail}:{id:string;kind:"image"|"video";title:string;active:boolean;onPlay:(id:string)=>void;onFail:(id:string)=>void}) {
 const [posterFailed,setPosterFailed]=useState(false);
 if(kind==="video" && active)return <ActiveVideo id={id} title={title} onFail={onFail}/>;
 const preview=<img src={`/api/backend/media/${id}/preview`} alt={title} loading="lazy" decoding="async" width={640} height={360} onError={()=>{if(kind==="image")onFail(id);else setPosterFailed(true)}}/>;
 if(kind==="image")return preview;
 return <button type="button" className="media-video-preview" aria-label={`Xem ${title}`} onClick={()=>onPlay(id)}>{posterFailed ? <span className="media-preview-placeholder">Video từ bài viết</span> : preview}<span className="media-play-button">▶ Xem video</span></button>;
});

export default function MediaPicker({ articleIDs, initialIDs, submitting, error, onCancel, onConfirm }: Props) {
  const [assets, setAssets] = useState<Asset[]>([]);
  const [selected, setSelected] = useState<string[]>(initialIDs ?? []);
  const [warnings, setWarnings] = useState<string[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState("");
  const [failed, setFailed] = useState<string[]>([]);
  const [attempt, setAttempt] = useState(0);
 const [activeVideo,setActiveVideo]=useState<string|null>(null);
 const playVideo=useCallback((id:string)=>setActiveVideo(id),[]);
  useEffect(() => {
    const controller = new AbortController();
    setLoading(true); setLoadError(""); setFailed([]);
    void (async () => {
      try {
        const response = await fetch("/api/backend/media/discover", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ article_ids: articleIDs }), signal: controller.signal });
        const data = await response.json();
        if (!response.ok) throw new Error(data.error || "Không tải được danh sách ảnh/video");
        if (controller.signal.aborted) return;
        setAssets(data.assets); setWarnings(data.warnings);
        setSelected(initialIDs == null ? [] : initialIDs.filter(id => data.assets.some((a: Asset) => a.id === id)));
      } catch (e) { if (!controller.signal.aborted) setLoadError((e as Error).message); }
      finally { if (!controller.signal.aborted) setLoading(false); }
    })();
    return () => controller.abort();
  }, [articleIDs, initialIDs, attempt]);
  useEffect(() => {
    const previous = document.body.style.overflow; document.body.style.overflow = "hidden";
    return () => { document.body.style.overflow = previous; };
  }, []);
  const fail = useCallback((id: string) => { setFailed(old => old.includes(id) ? old : [...old, id]); setSelected(old => old.includes(id) ? old.filter(value => value !== id) : old); },[]);
  return <div className="media-backdrop"><section className="media-dialog" role="dialog" aria-modal="true" aria-labelledby="media-title">
    <header className="panel-heading"><div><p className="eyebrow">CHỌN NỘI DUNG MINH HỌA</p><h2 id="media-title">Ảnh và video từ nguồn</h2><p className="fine">Chọn tối đa 24 mục. Các mục được chọn sẽ xuất hiện theo thứ tự chọn, chia đều thời lượng video. Video nguồn được tắt tiếng, dùng tối đa 90 giây đầu.</p></div><button autoFocus type="button" className="secondary compact" disabled={submitting} onClick={onCancel}>Đóng ×</button></header>
    <div className="media-body">
      {loading && <p role="status">Đang đọc ảnh và video từ các bài đã chọn… Chưa bắt đầu tạo video.</p>}
      {loadError && <div role="alert"><p>{loadError}</p><button className="secondary" onClick={() => setAttempt(n => n + 1)}>Thử lại</button></div>}
      {warnings.map((warning, i) => <p className="alert" key={i}>{warning}</p>)}
      {!loading && !loadError && <><div className="media-actions"><button type="button" className="text-button" disabled={submitting} onClick={() => setSelected(assets.filter(a => !failed.includes(a.id)).slice(0, 24).map(a => a.id))}>Chọn tất cả{assets.length > 24 ? " (24 mục đầu)" : ""}</button><button type="button" className="text-button" disabled={submitting} onClick={() => setSelected([])}>Bỏ chọn tất cả</button></div>
        {articleIDs.map(articleID => { const items = assets.filter(a => a.article_id === articleID); return <section className="media-group" key={articleID}><h3>{items[0]?.source_name || `Bài #${articleID}`}</h3><p className="fine">{items[0]?.article_title}</p>{!items.length && <p>Không có ảnh/video hỗ trợ.</p>}<div className="media-grid">{items.map((a, index) => <article className={`media-card ${selected.includes(a.id) ? "chosen" : ""}`} key={a.id}>
          <MediaPreview id={a.id} kind={a.kind} title={`${a.kind==="image" ? "Ảnh" : "Video"} ${index+1} — ${a.article_title}`} active={activeVideo===a.id} onPlay={playVideo} onFail={fail}/>
          {a.kind==="video" && activeVideo===a.id && <button type="button" className="text-button" onClick={()=>setActiveVideo(null)}>Đóng xem trước</button>}
          <label className="check-label"><input type="checkbox" checked={selected.includes(a.id)} disabled={submitting || failed.includes(a.id) || (!selected.includes(a.id) && selected.length >= 24)} onChange={e => {const checked=e.currentTarget.checked;setSelected(old => checked ? (old.includes(a.id) || old.length>=24 ? old : [...old,a.id]) : old.filter(id => id !== a.id));}} />{a.kind === "image" ? "Ảnh" : "Video"} {index + 1}{selected.includes(a.id) && <span className="badge">Thứ tự {selected.indexOf(a.id) + 1}</span>}</label>
          {failed.includes(a.id) && <p className="source-error">Không tải được bản xem trước. Mục này đã được bỏ chọn.</p>}
        </article>)}</div></section>; })}</>}
    </div>
    <footer className="media-footer">{error && <p role="alert" className="source-error">{error}</p>}<div><span><strong>{selected.length}/24</strong> mục được chọn{selected.length === 0 && " · Video sẽ chỉ dùng thẻ chữ"}</span><button type="button" className="primary" disabled={loading || !!loadError || submitting} onClick={() => onConfirm(selected)}>{submitting ? "Đang gửi…" : "Tạo video với lựa chọn này →"}</button></div></footer>
  </section></div>;
}
