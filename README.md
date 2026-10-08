# Newsbot

Newsbot biến mô tả RSS thành kịch bản tiếng Việt có dẫn nguồn, giọng đọc AI và video dọc MP4 để biên tập viên duyệt. Backend dùng Go, PostgreSQL, Redis và FFmpeg; dashboard quản trị dùng Next.js. Bản hiện tại có quản lý nguồn, chọn tin, sản xuất video, duyệt và chỉnh sửa kịch bản thành phiên bản mới.

## Chạy bằng Docker

Yêu cầu Docker Engine + Compose v2. Nếu Ubuntu chưa có Docker, cài `docker.io` và `docker-compose-v2`, bật dịch vụ Docker; dùng `sudo docker compose ...` khi tài khoản chưa có quyền truy cập daemon.

Chỉ copy cấu hình nếu bạn **chưa có `.env`**; giữ cấu hình hiện tại khi nâng cấp:

```bash
cp .env.example .env
```

Sửa `.env`: đặt `POSTGRES_PASSWORD`, `ADMIN_TOKEN` (ít nhất 24 ký tự ngẫu nhiên), và khóa API của nhà cung cấp bạn chọn ở phần dưới. Không đưa khóa API vào Git. Mật khẩu PostgreSQL nên dùng chuỗi chữ/số ngẫu nhiên để URL kết nối không cần escape.

```bash
docker compose up --build -d
docker compose logs -f web api collector worker
```

Compose tự chạy migration trước các dịch vụ ứng dụng. PostgreSQL/Redis ở mạng nội bộ; API bind host `127.0.0.1:8080`, dashboard bind `127.0.0.1:3000`. MP4 lưu trong Docker volume `newsbot_videos`. Chưa cần S3/R2.

## Sử dụng dashboard

Mở **http://localhost:3000**, nhập giá trị `ADMIN_TOKEN` trong `.env` để đăng nhập.

1. **Nguồn tin:** thêm RSS, mở Thiết lập để ghi quyền sử dụng và bật nguồn. Chọn Thu thập ngay để lấy tin mà không đợi lịch collector.
2. **Tin đã thu thập:** tìm kiếm theo tiêu đề/nguồn, lọc chủ đề, chọn 1–6 bài cùng sự kiện rồi Tạo video.
3. **Sản xuất video:** theo dõi trạng thái, xem MP4 trực tiếp, đối chiếu kịch bản, dữ kiện, trích dẫn và bài gốc. Duyệt hoặc từ chối khi video ở trạng thái chờ duyệt. Có thể tải MP4.
4. **Sửa kịch bản:** sửa tiêu đề và lời đọc từng cảnh, rồi Lưu và tạo phiên bản mới. Phụ đề đi theo lời đọc. Worker kiểm tra dữ kiện lại trước TTS/render; video trước đó giữ nguyên. Phiên bản mới vẫn cần duyệt.

Danh sách tự cập nhật mỗi 10 giây khi tab trình duyệt đang mở, hiển thị tối đa 100 bài/video gần nhất. Tổng quan hiển thị số lượng toàn database và nhà cung cấp AI đang dùng. Đổi AI vẫn qua `.env`.

`WEB_ORIGIN` mặc định `http://localhost:3000`. Nếu mở bằng `http://127.0.0.1:3000`, hãy đặt `WEB_ORIGIN=http://127.0.0.1:3000` rồi tạo lại container web. Origin phải khớp địa chỉ trên trình duyệt để đăng nhập và gửi thao tác. Khi dùng reverse proxy, đặt WEB_ORIGIN thành địa chỉ HTTPS của dashboard.

Dashboard giữ ADMIN_TOKEN ở server và xác thực bằng cookie HttpOnly có chữ ký, hết hạn sau 12 giờ. Trình duyệt phát MP4 qua proxy có hỗ trợ Range requests; không đưa token vào URL video hoặc localStorage. Đây là một tài khoản quản trị dùng chung, chưa có phân quyền nhiều người.

### Nâng cấp từ bản trước

Lệnh `docker compose up --build -d` sẽ chạy migration mới, giữ bài/video và volumes hiện có. Migration có sổ phiên bản, checksum và khóa PostgreSQL để tránh áp dụng hai lần. Không sửa các file migration đã áp dụng; thêm file mới khi đổi schema.

Nếu container `migrate` cũ vẫn ở trạng thái completed và chưa chạy lại, áp dụng rõ ràng trước khi khởi động ứng dụng:

```bash
docker compose build
docker compose run --rm migrate
docker compose up -d
```

Không dùng `docker compose down -v` khi nâng cấp vì tùy chọn đó xóa volumes.

Các nguồn mẫu **chưa bật**. Xem và bật từng nguồn qua API sau khi kiểm tra quyền sử dụng. RSS VnExpress có điều khoản riêng cho cá nhân/tổ chức phi lợi nhuận; không mặc định nguồn RSS cho phép sản xuất video thương mại. Xem [trang RSS và điều khoản VnExpress](https://vnexpress.net/rss). Thêm nguồn bạn được phép sử dụng thay vì phụ thuộc vào danh sách báo cố định.

## Chọn Google hoặc OpenAI

`AI_PROVIDER` chọn nhà cung cấp tạo kịch bản và kiểm tra dữ kiện. `TTS_PROVIDER` chọn nhà cung cấp giọng đọc; **để trống thì dùng cùng `AI_PROVIDER`**. Giá trị hợp lệ là `openai` hoặc `google` (`gemini` là bí danh của `google`). Không cần khóa của nhà cung cấp không dùng. Đây là cấu hình Gemini Developer API qua Google AI Studio; chưa hỗ trợ Vertex AI/Google Cloud credentials.

OpenAI cho cả hai bước (mặc định, tương thích `.env` cũ):

```dotenv
AI_PROVIDER=openai
TTS_PROVIDER=
OPENAI_API_KEY=your-openai-key
OPENAI_TEXT_MODEL=gpt-4o-mini
OPENAI_TTS_MODEL=gpt-4o-mini-tts
OPENAI_VOICE=coral
```

Google Gemini cho cả hai bước:

```dotenv
AI_PROVIDER=google
TTS_PROVIDER=
GOOGLE_API_KEY=your-google-key
GOOGLE_TEXT_MODEL=gemini-3.8-flash
GOOGLE_TTS_MODEL=gemini-3.8-flash-tts
GOOGLE_VOICE=Kore
```

Cũng nhận `GEMINI_API_KEY`; nếu đặt cả hai, `GOOGLE_API_KEY` được ưu tiên. Model và voice cần có quyền truy cập trong tài khoản tương ứng; bạn có thể thay tên model trong `.env`. Google text model phải hỗ trợ structured output, Google TTS model phải hỗ trợ audio generation.

Kết hợp Gemini viết kịch bản và OpenAI đọc:

```dotenv
AI_PROVIDER=google
TTS_PROVIDER=openai
GOOGLE_API_KEY=your-google-key
OPENAI_API_KEY=your-openai-key
```

Chiều ngược lại dùng `AI_PROVIDER=openai`, `TTS_PROVIDER=google`. Cấu hình model/voice của từng bên giữ riêng, nên đổi provider không cần thay model của bên còn lại. Không tự chuyển sang provider khác khi lỗi; các lỗi tạm thời được thử lại với provider đã chọn.

Sau khi sửa `.env`, tạo lại các container để nhận biến môi trường mới:

```bash
docker compose up --build -d
```

Google sử dụng native `generateContent`, JSON schema và audio WAV/PCM. Adapter đóng gói PCM thành WAV; renderer nhận cả OpenAI MP3 và Google WAV. Cấu trúc kịch bản và các bước chặn dữ kiện không được hỗ trợ giữ chung cho cả hai nhà cung cấp. Định dạng API đối chiếu với [Google Structured Outputs](https://ai.google.dev/gemini-api/docs/generate-content/structured-output) và [Google TTS](https://ai.google.dev/gemini-api/docs/generate-content/speech-generation).

## Thử pipeline

Các lệnh dưới đây dùng Bash và `curl`. Đặt token giống `.env` trong shell của bạn:

```bash
export ADMIN_TOKEN='token-da-dat-trong-env'

curl -sS http://localhost:8080/healthz
curl -sS -H "Authorization: Bearer $ADMIN_TOKEN" http://localhost:8080/api/sources
```

Thêm một nguồn RSS/Atom được phép dùng; nguồn mới mặc định tắt:

```bash
curl -sS -X POST http://localhost:8080/api/sources \
  -H "Authorization: Bearer $ADMIN_TOKEN" -H 'Content-Type: application/json' \
  -d '{"name":"Nguồn của tôi","rss_url":"https://example.com/feed.xml","language":"vi","category":"TECHNOLOGY","license_notes":"Ghi điều khoản hoặc thỏa thuận thực tế ở đây"}'
```

Bật nguồn với ID trả về (thay `1` bằng ID của bạn), ghi rõ căn cứ sử dụng:

```bash
curl -sS -X PATCH http://localhost:8080/api/sources/1 \
  -H "Authorization: Bearer $ADMIN_TOKEN" -H 'Content-Type: application/json' \
  -d '{"active":true,"license_notes":"Ghi căn cứ quyền sử dụng thực tế của nguồn này"}'

docker compose run --rm collector collect
curl -sS -H "Authorization: Bearer $ADMIN_TOKEN" http://localhost:8080/api/articles
```

Chọn 1–6 ID bài **cùng một sự kiện**, thay `[1,2]` bằng ID thực tế. RSS chỉ có mô tả ngắn có thể không đủ dữ kiện. Không tự tải nội dung trang báo, ảnh hoặc video của nguồn.

```bash
curl -sS -X POST http://localhost:8080/api/videos \
  -H "Authorization: Bearer $ADMIN_TOKEN" -H 'Content-Type: application/json' \
  -d '{"article_ids":[1,2]}'
```

API trả `202` và `id`. Theo dõi trạng thái, kịch bản, kết quả kiểm tra và nguồn:

```bash
export VIDEO_ID='id-tra-ve-tu-api'
curl -sS -H "Authorization: Bearer $ADMIN_TOKEN" \
  "http://localhost:8080/api/videos/$VIDEO_ID"
```

Khi `status=pending_review`, tải MP4 để xem, đối chiếu bài gốc và duyệt:

```bash
curl -f -H "Authorization: Bearer $ADMIN_TOKEN" \
  "http://localhost:8080/api/videos/$VIDEO_ID/file" -o video.mp4

curl -sS -X POST "http://localhost:8080/api/videos/$VIDEO_ID/review" \
  -H "Authorization: Bearer $ADMIN_TOKEN" -H 'Content-Type: application/json' \
  -d '{"decision":"approved"}'
```

Từ chối bằng `decision=rejected`. Duyệt chỉ thay trạng thái, **không đăng lên TikTok**. Các trạng thái: `queued → processing → pending_review → approved/rejected`; lỗi tạm thời được thử tối đa 3 lần, sau đó `failed`. Lỗi HTTP 4xx từ AI (trừ 408/429) chuyển ngay sang `failed` vì gửi lại cùng request không khắc phục được lỗi. Xem `last_error` và log worker khi pipeline dừng. Render chỉ cho thời lượng 20–90 giây; mục tiêu prompt là 45–60 giây.

## Cấu trúc

```text
cmd/newsbot/              CLI api, worker, collector, collect, migrate
internal/newsbot/        config, RSS, PostgreSQL jobs, AI, renderer, API
migrations/001_init.sql  nguồn, bài, video, provenance, jobs
migrations/002_video_revisions.sql  lưu bản sửa và liên kết phiên bản trước
web/                     Next.js dashboard, đăng nhập và proxy API
docs/architecture.md     giới hạn và lộ trình
Dockerfile               Go build + FFmpeg + font tiếng Việt
docker-compose.yml       môi trường chạy MVP
.github/workflows/ci.yml unit, PostgreSQL và render integration tests
```

## Phát triển và kiểm thử

Go 1.24+, PostgreSQL 16, Redis 7; worker cần `ffmpeg`, `ffprobe` và font Noto Sans. Load biến `.env` qua IDE hoặc shell trước khi chạy local; chương trình không tự đọc `.env`.

```bash
go mod download
go run ./cmd/newsbot migrate
go run ./cmd/newsbot api
# Terminal khác:
go run ./cmd/newsbot collector
go run ./cmd/newsbot worker

go test ./...
go vet ./...
go build -buildvcs=false -o bin/newsbot ./cmd/newsbot
```

Integration test PostgreSQL cần database kiểm thử riêng: test sẽ **xóa dữ liệu trong database được chỉ định**. Tuyệt đối không dùng database thật.

```bash
NEWSBOT_TEST_DATABASE_URL='postgres://newsbot:test@localhost:5432/newsbot_test?sslmode=disable' \
NEWSBOT_RENDER_TEST=1 go test -race -count=1 ./...
```

Test render dùng âm thanh giả lập, không gọi API tính phí. CI chạy PostgreSQL test và FFmpeg test; unit tests không cần dịch vụ ngoài. Các bài kiểm tra này không đánh giá chất lượng phát âm tiếng Việt hoặc tính đúng đắn của tin thật.

Dashboard local yêu cầu Node.js 24. API Go phải chạy trước; load `ADMIN_TOKEN`, `API_INTERNAL_URL=http://127.0.0.1:8080` và `WEB_ORIGIN=http://localhost:3000` vào môi trường Node. Next.js không đọc `.env` ở thư mục cha; dùng môi trường của shell hoặc file `web/.env.local` được gitignore.

```bash
cd web
npm ci
npm run dev
# Kiểm tra dashboard:
npm test
npm run build
npm run typecheck
npm run test:integration
```

Test integration web dùng Next.js production và Go API giả lập trên localhost, xác nhận đăng nhập/cookie, giới hạn đường dẫn proxy, CSRF, phiên bản kịch bản và MP4 Range streaming. Không đọc khóa thật hoặc gọi AI tính phí.

## Phạm vi tiếp theo

Chưa có semantic clustering/pgvector, kiểm chứng ngoài các bài đầu vào, asset có bản quyền, R2, TikTok OAuth/publishing hoặc analytics. Người biên tập vẫn chọn các bài cùng sự kiện. Pipeline hiện dùng một template chữ + nền màu; phụ đề chia cụm từ, thời gian trong cảnh được ước lượng theo số từ, chưa có forced alignment. Mỗi video luôn cần người duyệt.

OpenAI adapter đối chiếu với tài liệu [Structured Outputs](https://developers.openai.com/api/docs/guides/structured-outputs) và [Create speech](https://developers.openai.com/api/reference/resources/audio/subresources/speech/methods/create). JSON schema và kiểm tra quote giúp chặn bằng chứng bịa; chúng không chứng minh thông tin ngoài đời là đúng.

## Khắc phục lỗi Google HTTP 400

Adapter gửi `responseFormat.text.mimeType=APPLICATION_JSON` theo REST API của Google. Nếu worker cũ báo `google AI HTTP 400, request_id=`, cập nhật image bằng `sudo docker compose up --build -d api worker`, rồi tạo video mới từ các bài đã chọn. Video đã `failed` không tự chạy lại.

Log mới và `last_error` hiển thị bước thất bại, endpoint/model, mã và thông báo lỗi của nhà cung cấp; các khóa API và token quản trị được che. Xem bằng `sudo docker compose logs -f worker`. HTTP 408/429 và 5xx vẫn thử lại tối đa 3 lần.

Nếu AI trả `claims=[]` và `scenes=[]`, worker báo nguồn chưa đủ dữ kiện và dừng job thay vì gọi lại cùng đầu vào. Hãy chọn thêm bài liên quan có nội dung đủ dài. Bộ đọc feed ưu tiên RSS `content:encoded` và Atom `content`, rồi mới dùng mô tả/tóm tắt; worker hỗ trợ tải nội dung bài VnExpress được chọn khi nội dung đang lưu dưới 70 từ. Bài đã lưu trước khi cập nhật vẫn giữ nội dung cũ. Kịch bản không đạt kiểm tra được lưu để chẩn đoán và không đưa sang TTS/render.

Lỗi AI HTTP 429/5xx được chờ 2 phút trước lần 2, 4 phút trước lần 3; các lỗi tạm thời khác chờ 30 giây. Log `video retry scheduled` kèm `retry_after` nghĩa là job còn queued; `video failed` chỉ xuất hiện khi kết thúc. Không tự đổi model/provider. Backoff không đảm bảo model hết quá tải.

Tạo video từ một bài VnExpress: worker bổ sung các đoạn nội dung chính từ trang bài gốc nếu RSS đang lưu dưới 70 từ, giữ lại mô tả ban đầu để không mất bằng chứng. Chỉ tải bài được chọn, HTTPS trên vnexpress.net/www.vnexpress.net, chặn redirect ngoài nguồn và IP nội bộ, giới hạn 4 MiB/30 giây. Nếu tải/trích xuất thất bại, log `load full article` và không gọi AI/TTS. Nguồn khác vẫn dùng nội dung feed. Bài có nội dung đủ dài vẫn phải vượt kiểm tra dữ kiện.
