"use client";

import { memo, useCallback, useEffect, useRef, useState } from "react";

type Asset = { uploaded?: boolean; id: string; article_id: number; kind: "image" | "video"; source_name: string; article_title: string };
type Props = { showMediaSource: boolean; onMediaSourceChange: (enabled: boolean) => void; factCheckEnabled: boolean; onFactCheckChange: (enabled: boolean) => void; articleIDs: number[]; initialIDs?: string[] | null; submitting: boolean; error: string; onCancel: () => void; onConfirm: (ids: string[]) => void };
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

export default function MediaPicker({ showMediaSource, onMediaSourceChange, factCheckEnabled, onFactCheckChange, articleIDs, initialIDs, submitting, error, onCancel, onConfirm }: Props) {
  const [uploading, setUploading] = useState(false);
  const [uploadError, setUploadError] = useState("");
  const uploadInput = useRef<HTMLInputElement>(null);
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
  async function uploadFiles(files: FileList | null) {
    if (!files?.length) return;
    setUploading(true); setUploadError("");
    try {
      for (const file of Array.from(files)) {
        if (file.size > 100 * 1024 * 1024) throw new Error(file.name + ": tệp tối đa 100 MB");
        const body = new FormData(); body.append("article_id", String(articleIDs[0])); body.append("file", file);
        const response = await fetch("/api/backend/media/upload", { method: "POST", body });
        const data = await response.json();
        if (!response.ok) throw new Error(data.error || "Không tải lên được tệp");
        setAssets(old => [...old, data]);
        setSelected(old => old.length < 24 ? [...old, data.id] : old);
      }
    } catch (e) { setUploadError((e as Error).message); }
    finally { setUploading(false); if (uploadInput.current) uploadInput.current.value = ""; }
  }
  const fail = useCallback((id: string) => { setFailed(old => old.includes(id) ? old : [...old, id]); setSelected(old => old.includes(id) ? old.filter(value => value !== id) : old); },[]);
  return <div className="media-backdrop"><section className="media-dialog" role="dialog" aria-modal="true" aria-labelledby="media-title">
    <header className="panel-heading"><div><p className="eyebrow">CHỌN NỘI DUNG MINH HỌA</p><h2 id="media-title">Chọn ảnh và video</h2><p className="fine">Chọn tối đa 24 mục. Các mục được chọn sẽ xuất hiện theo thứ tự chọn, chia đều thời lượng video. Video nguồn được tắt tiếng, dùng tối đa 90 giây đầu.</p></div><button autoFocus type="button" className="secondary compact" disabled={submitting || uploading} onClick={onCancel}>Đóng ×</button></header>
    <label className="check-label fact-check-option"><input type="checkbox" checked={factCheckEnabled} disabled={submitting} onChange={e => onFactCheckChange(e.target.checked)} />Kiểm chứng dữ kiện trước khi tạo video</label><label className="check-label fact-check-option media-source-option"><input type="checkbox" checked={showMediaSource} disabled={submitting} onChange={e => onMediaSourceChange(e.target.checked)} />Hiển thị nguồn ảnh/video trong video</label><div className="media-body">
      <div className="media-actions"><input ref={uploadInput} type="file" hidden multiple accept=".jpg,.jpeg,.png,.webp,.mp4,.mov,.webm" onChange={e => void uploadFiles(e.target.files)} /><button type="button" className="secondary" disabled={loading || uploading || submitting} onClick={() => uploadInput.current?.click()}>{uploading ? "Đang tải lên…" : "↑ Tải ảnh/video từ máy"}</button><span className="fine">JPG, PNG, WebP (10 MB); MP4, MOV, WebM (100 MB).</span></div>
      {uploadError && <p role="alert" className="source-error">{uploadError}</p>}
      {loading && <p role="status">Đang đọc ảnh và video từ các bài đã chọn… Chưa bắt đầu tạo video.</p>}
      {loadError && <div role="alert"><p>{loadError}</p><button className="secondary" onClick={() => setAttempt(n => n + 1)}>Thử lại</button></div>}
      {warnings.map((warning, i) => <p className="alert" key={i}>{warning}</p>)}
      {!loading && !loadError && <><div className="media-actions"><button type="button" className="text-button" disabled={submitting} onClick={() => setSelected(assets.filter(a => !failed.includes(a.id)).slice(0, 24).map(a => a.id))}>Chọn tất cả{assets.length > 24 ? " (24 mục đầu)" : ""}</button><button type="button" className="text-button" disabled={submitting} onClick={() => setSelected([])}>Bỏ chọn tất cả</button></div>
        {[0, ...articleIDs].map(articleID => { const items = assets.filter(a => articleID === 0 ? a.uploaded : !a.uploaded && a.article_id === articleID); if (articleID === 0 && !items.length) return null; return <section className="media-group" key={articleID}><h3>{articleID === 0 ? "Ảnh/video tải lên" : items[0]?.source_name || `Bài #${articleID}`}</h3><p className="fine">{articleID === 0 ? "Tệp từ máy của bạn" : items[0]?.article_title}</p>{!items.length && <p>Không có ảnh/video hỗ trợ.</p>}<div className="media-grid">{items.map((a, index) => <article className={`media-card ${selected.includes(a.id) ? "chosen" : ""}`} key={a.id}>
          <MediaPreview id={a.id} kind={a.kind} title={`${a.kind==="image" ? "Ảnh" : "Video"} ${index+1} — ${a.article_title}`} active={activeVideo===a.id} onPlay={playVideo} onFail={fail}/>
          {a.kind==="video" && activeVideo===a.id && <button type="button" className="text-button" onClick={()=>setActiveVideo(null)}>Đóng xem trước</button>}
          <label className="check-label"><input type="checkbox" checked={selected.includes(a.id)} disabled={submitting || failed.includes(a.id) || (!selected.includes(a.id) && selected.length >= 24)} onChange={e => {const checked=e.currentTarget.checked;setSelected(old => checked ? (old.includes(a.id) || old.length>=24 ? old : [...old,a.id]) : old.filter(id => id !== a.id));}} />{a.kind === "image" ? "Ảnh" : "Video"} {index + 1}{selected.includes(a.id) && <span className="badge">Thứ tự {selected.indexOf(a.id) + 1}</span>}</label>
          {failed.includes(a.id) && <p className="source-error">Không tải được bản xem trước. Mục này đã được bỏ chọn.</p>}
        </article>)}</div></section>; })}</>}
    </div>
    <footer className="media-footer">{error && <p role="alert" className="source-error">{error}</p>}<div><span><strong>{selected.length}/24</strong> mục được chọn{selected.length === 0 && " · Video sẽ chỉ dùng thẻ chữ"}</span><button type="button" className="primary" disabled={loading || !!loadError || uploading || submitting} onClick={() => onConfirm(selected)}>{submitting ? "Đang gửi…" : "Tạo video với lựa chọn này →"}</button></div></footer>
  </section></div>;
}
