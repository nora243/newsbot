package newsbot

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct{ DB *pgxpool.Pool }

func NewStore(ctx context.Context, dsn string) (*Store, error) {
	p, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, err
	}
	if err = p.Ping(ctx); err != nil {
		p.Close()
		return nil, err
	}
	return &Store{p}, nil
}
func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}

func (s *Store) Sources(ctx context.Context, activeOnly bool) ([]Source, error) {
	rows, err := s.DB.Query(ctx, `SELECT id,name,rss_url,language,category,active,license_notes,last_error,last_fetched_at FROM news_sources WHERE deleted_at IS NULL AND (NOT $1 OR active) ORDER BY id`, activeOnly)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Source{}
	for rows.Next() {
		var a Source
		if err = rows.Scan(&a.ID, &a.Name, &a.RSSURL, &a.Language, &a.Category, &a.Active, &a.LicenseNotes, &a.LastError, &a.LastFetchedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
func (s *Store) SaveArticle(ctx context.Context, a Article) (bool, error) {
	r, err := s.DB.Exec(ctx, `INSERT INTO news_articles(source_id,canonical_url,title,content,content_hash,published_at) SELECT $1,$2,$3,$4,$5,$6 FROM news_sources WHERE id=$1 AND deleted_at IS NULL ON CONFLICT DO NOTHING`, a.SourceID, a.URL, a.Title, a.Content, a.Hash, a.PublishedAt)
	return err == nil && r.RowsAffected() > 0, err
}
func (s *Store) Articles(ctx context.Context, ids []int64) ([]Article, error) {
	rows, err := s.DB.Query(ctx, `SELECT a.id,a.source_id,s.name,a.canonical_url,a.title,a.content,COALESCE(a.published_at, a.collected_at) FROM news_articles a JOIN news_sources s ON s.id=a.source_id WHERE ($1::bigint[] IS NULL OR a.id=ANY($1)) ORDER BY COALESCE(a.published_at, a.collected_at) DESC, a.collected_at DESC, a.id DESC LIMIT 100`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Article{}
	for rows.Next() {
		var a Article
		if err = rows.Scan(&a.ID, &a.SourceID, &a.SourceName, &a.URL, &a.Title, &a.Content, &a.PublishedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
func (s *Store) SearchArticles(ctx context.Context, query string, sourceIDs []int64, category string, offset int, usage string) ([]Article, error) {
	rows, err := s.DB.Query(ctx, `SELECT a.id,a.source_id,s.name,a.canonical_url,a.title,a.content,COALESCE(a.published_at, a.collected_at), usage.used
 FROM news_articles a JOIN news_sources s ON s.id=a.source_id
 CROSS JOIN LATERAL (SELECT EXISTS(SELECT 1 FROM video_sources vs WHERE vs.article_id=a.id) AS used) usage
 WHERE ($1='' OR strpos(lower(a.title),lower($1))>0)
 AND ($2::bigint[] IS NULL OR cardinality($2)=0 OR a.source_id=ANY($2)) AND ($3='' OR s.category=$3)
 AND ($5='' OR ($5='used' AND usage.used) OR ($5='unused' AND NOT usage.used))
 ORDER BY COALESCE(a.published_at, a.collected_at) DESC, a.collected_at DESC, a.id DESC LIMIT 101 OFFSET $4`, query, sourceIDs, category, offset, usage)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Article{}
	for rows.Next() {
		var a Article
		if err = rows.Scan(&a.ID, &a.SourceID, &a.SourceName, &a.URL, &a.Title, &a.Content, &a.PublishedAt, &a.UsedForVideo); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
func (s *Store) Enqueue(ctx context.Context, ids []int64, media ...[]string) (string, error) {
	return s.enqueue(ctx, ids, nil, nil, false, media...)
}

func (s *Store) EnqueueRevision(ctx context.Context, parentID string, articles []Article, draft Script, media ...[]string) (string, error) {
	return s.enqueueRevision(ctx, parentID, articles, draft, false, media...)
}
func (s *Store) enqueueRevision(ctx context.Context, parentID string, articles []Article, draft Script, repair bool, media ...[]string) (string, error) {
	if err := draft.Validate(articles); err != nil {
		return "", err
	}
	ids := make([]int64, 0, len(articles))
	for _, a := range articles {
		ids = append(ids, a.ID)
	}
	data, err := json.Marshal(draft)
	if err != nil {
		return "", err
	}
	return s.enqueue(ctx, ids, data, &parentID, repair, media...)
}

func (s *Store) enqueue(ctx context.Context, ids []int64, draft []byte, parentID *string, repair bool, media ...[]string) (string, error) {
	if len(ids) == 0 || len(ids) > 6 {
		return "", fmt.Errorf("chọn 1–6 bài viết cùng sự kiện")
	}
	seen := map[int64]bool{}
	for _, id := range ids {
		if id <= 0 || seen[id] {
			return "", fmt.Errorf("article_ids không hợp lệ")
		}
		seen[id] = true
	}
	var mediaJSON []byte
	if len(media) > 0 && media[0] != nil {
		if err := s.ValidateMedia(ctx, ids, media[0]); err != nil {
			return "", err
		}
		mediaJSON, _ = json.Marshal(media[0])
	}
	id := newID()
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `INSERT INTO videos(id,parent_id,draft_script,script,media_ids,repair_failed_scenes) VALUES($1,$2,$3,$3,$4,$5)`, id, parentID, draft, mediaJSON, repair); err != nil {
		return "", err
	}
	for _, aid := range ids {
		if _, err = tx.Exec(ctx, `INSERT INTO video_sources(video_id,article_id) VALUES($1,$2)`, id, aid); err != nil {
			return "", fmt.Errorf("không tìm thấy bài viết: %w", err)
		}
	}
	if _, err = tx.Exec(ctx, `INSERT INTO jobs(video_id) VALUES($1)`, id); err != nil {
		return "", err
	}
	return id, tx.Commit(ctx)
}

func (s *Store) Draft(ctx context.Context, id string) (*Script, error) {
	var raw []byte
	if err := s.DB.QueryRow(ctx, `SELECT draft_script FROM videos WHERE id=$1`, id).Scan(&raw); err != nil {
		return nil, err
	}
	if raw == nil {
		return nil, nil
	}
	var draft Script
	if err := json.Unmarshal(raw, &draft); err != nil {
		return nil, err
	}
	return &draft, nil
}

type Job struct {
	ID                  int64
	VideoID, LeaseToken string
	Attempts            int
}

func (s *Store) ClaimJob(ctx context.Context) (*Job, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	var j Job
	j.LeaseToken = newID()
	err = tx.QueryRow(ctx, `WITH next AS (SELECT id FROM jobs WHERE ((status='queued' AND available_at<=now()) OR (status='running' AND lease_until<now())) AND attempts<3 ORDER BY available_at FOR UPDATE SKIP LOCKED LIMIT 1)
UPDATE jobs SET status='running',attempts=attempts+1,lease_until=now()+interval '2 minutes',lease_token=$1 FROM next WHERE jobs.id=next.id RETURNING jobs.id,video_id,attempts`, j.LeaseToken).Scan(&j.ID, &j.VideoID, &j.Attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, `UPDATE videos SET status='processing',last_error=NULL WHERE id=$1`, j.VideoID); err != nil {
		return nil, err
	}
	return &j, tx.Commit(ctx)
}
func (s *Store) Heartbeat(ctx context.Context, j Job) error {
	r, err := s.DB.Exec(ctx, `UPDATE jobs SET lease_until=now()+interval '2 minutes' WHERE id=$1 AND lease_token=$2 AND status='running'`, j.ID, j.LeaseToken)
	if err != nil {
		return err
	}
	if r.RowsAffected() != 1 {
		return fmt.Errorf("job đã mất lease")
	}
	return nil
}
func (s *Store) JobArticles(ctx context.Context, id string) ([]Article, error) {
	rows, err := s.DB.Query(ctx, `SELECT article_id FROM video_sources WHERE video_id=$1 ORDER BY article_id`, id)
	if err != nil {
		return nil, err
	}
	ids := []int64{}
	for rows.Next() {
		var aid int64
		if err = rows.Scan(&aid); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, aid)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("video không có nguồn")
	}
	return s.Articles(ctx, ids)
}
func (s *Store) SaveScript(ctx context.Context, j Job, script Script, check FactCheck) error {
	a, err := json.Marshal(script)
	if err != nil {
		return err
	}
	b, err := json.Marshal(check)
	if err != nil {
		return err
	}
	r, err := s.DB.Exec(ctx, `UPDATE videos SET script=$1,fact_check=$2 WHERE id=$3 AND EXISTS(SELECT 1 FROM jobs WHERE id=$4 AND lease_token=$5 AND status='running')`, a, b, j.VideoID, j.ID, j.LeaseToken)
	if err != nil {
		return err
	}
	if r.RowsAffected() != 1 {
		return fmt.Errorf("job đã mất lease")
	}
	return nil
}
func (s *Store) Finish(ctx context.Context, j Job, path string, duration float64, jobErr error) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	jobStatus, videoStatus := "done", "pending_review"
	var msg *string
	if jobErr != nil {
		m := jobErr.Error()
		msg = &m
		jobStatus, videoStatus = "queued", "queued"
		if jobRetryDelay(j.Attempts, jobErr) == 0 {
			jobStatus, videoStatus = "failed", "failed"
		}
	}
	r, err := tx.Exec(ctx, `UPDATE jobs SET status=$1,last_error=$2,available_at=now()+($5 * interval '1 second'),lease_until=NULL WHERE id=$3 AND lease_token=$4 AND status='running'`, jobStatus, msg, j.ID, j.LeaseToken, jobRetryDelay(j.Attempts, jobErr).Seconds())
	if err != nil {
		return err
	}
	if r.RowsAffected() != 1 {
		return fmt.Errorf("job đã mất lease")
	}
	if _, err = tx.Exec(ctx, `UPDATE videos SET status=$1,last_error=$2,output_path=$3,duration_seconds=$4 WHERE id=$5`, videoStatus, msg, path, duration, j.VideoID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (s *Store) Reap(ctx context.Context) error {
	_, err := s.DB.Exec(ctx, `WITH dead AS (UPDATE jobs SET status='failed',last_error='worker lease expired after final attempt' WHERE status='running' AND lease_until<now() AND attempts>=3 RETURNING video_id) UPDATE videos SET status='failed',last_error='worker lease expired after final attempt' WHERE id IN(SELECT video_id FROM dead)`)
	return err
}

type Video struct {
	MediaIDs       []string        `json:"media_ids"`
	PostingCaption string          `json:"posting_caption"`
	ParentID       *string         `json:"parent_id"`
	ID             string          `json:"id"`
	Status         string          `json:"status"`
	Script         json.RawMessage `json:"script"`
	FactCheck      json.RawMessage `json:"fact_check"`
	OutputPath     *string         `json:"-"`
	Duration       *float64        `json:"duration_seconds"`
	LastError      *string         `json:"last_error"`
	CreatedAt      time.Time       `json:"created_at"`
	Sources        []Article       `json:"sources,omitempty"`
}

func (s *Store) Videos(ctx context.Context, id string, filters ...string) ([]Video, error) {
	query, status := "", ""
	if len(filters) > 0 {
		query = filters[0]
	}
	if len(filters) > 1 {
		status = filters[1]
	}
	rows, err := s.DB.Query(ctx, `SELECT id,status,script,fact_check,output_path,duration_seconds,last_error,created_at,parent_id,media_ids FROM videos WHERE ($1='' OR id=$1) AND ($2='' OR strpos(lower(COALESCE(script->>'title','')),lower($2))>0) AND ($3='' OR status=$3) ORDER BY created_at DESC LIMIT 100`, id, query, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Video{}
	for rows.Next() {
		var v Video
		if err = rows.Scan(&v.ID, &v.Status, &v.Script, &v.FactCheck, &v.OutputPath, &v.Duration, &v.LastError, &v.CreatedAt, &v.ParentID, &v.MediaIDs); err != nil {
			return nil, err
		}
		var script Script
		if json.Unmarshal(v.Script, &script) == nil {
			v.PostingCaption = postingCaption(script)
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// A repair job uses the completed parent's check to select exactly which scenes to replace.
func (s *Store) RepairCheck(ctx context.Context, id string) (*FactCheck, error) {
	var requested bool
	var raw []byte
	if err := s.DB.QueryRow(ctx, `SELECT v.repair_failed_scenes,p.fact_check FROM videos v LEFT JOIN videos p ON p.id=v.parent_id WHERE v.id=$1`, id).Scan(&requested, &raw); err != nil {
		return nil, err
	}
	if !requested {
		return nil, nil
	}
	if raw == nil {
		return nil, fmt.Errorf("thiếu kết quả kiểm chứng của bản trước")
	}
	var check FactCheck
	if err := json.Unmarshal(raw, &check); err != nil {
		return nil, err
	}
	return &check, nil
}
