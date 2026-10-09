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
2. **Tin đã thu thập:** tìm kiếm theo tiêu đề trên toàn bộ database (chờ 400 ms sau khi ngừng gõ), chọn nguồn bằng danh sách riêng, lọc chủ đề, chọn 1–6 bài cùng sự kiện rồi Tạo video.
3. **Sản xuất video:** theo dõi trạng thái, xem MP4 trực tiếp, đối chiếu kịch bản, dữ kiện, trích dẫn và bài gốc. Duyệt hoặc từ chối khi video ở trạng thái chờ duyệt. Có thể tải MP4.
4. **Sửa kịch bản:** sửa tiêu đề và lời đọc từng cảnh, rồi Lưu và tạo phiên bản mới. Phụ đề đi theo lời đọc. Worker kiểm tra dữ kiện lại trước TTS/render; video trước đó giữ nguyên. Phiên bản mới vẫn cần duyệt.

Danh sách và chi tiết video không tự tải lại theo chu kỳ. Dùng nút **Làm mới** để cập nhật dữ liệu và trạng thái video đang mở; tìm kiếm vẫn chờ 400 ms sau khi ngừng gõ. Danh sách phân trang tin với 100 bài mỗi trang sau khi lọc, hiển thị tối đa 100 video gần nhất. Tổng quan hiển thị số lượng toàn database và nhà cung cấp AI đang dùng. Đổi AI vẫn qua `.env`.

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

## Giao diện video So Híp

Video mới dùng khung dọc 1080×1920: logo So Híp màu vàng chanh, nền đen xanh, đường nhấn cam, tiêu đề ở trên, ảnh lớn ở giữa, phụ đề chia cụm tối đa 7 từ ở dưới. Chữ chính nằm trong vùng x=72–876, phía trên y=1640 để chừa khoảng cho giao diện TikTok; hãy kiểm tra lại trong màn hình đăng vì lớp phủ thay đổi theo thiết bị và độ dài mô tả. Thanh tiến trình và số cảnh giúp người xem theo dõi bản tin.

Renderer ưu tiên ảnh trong nội dung bài có bằng chứng của cảnh, đọc cả ảnh tải trễ (`data-src`, `data-original`) và biến thể độ phân giải cao trong `picture`/`srcset`; dự phòng bằng `og:image`/`twitter:image`. Tải tối đa 6 ảnh mỗi bài, thử địa chỉ khác nếu ảnh lỗi và luân phiên ảnh qua các cảnh của cùng bài, với chuyển động phóng nhẹ. Ảnh minh họa theo bài, chưa tự đối chiếu ý nghĩa từng ảnh với lời đọc. Chỉ nhận JPEG/PNG/WebP hợp lệ, tối đa 10 MiB/24 megapixel; giới hạn thời gian và chặn mạng nội bộ cả khi redirect. Khi bài không có ảnh hoặc tải ảnh thất bại, dùng thẻ dữ kiện có dẫn nguồn. Ảnh lấy từ bài có thể chỉ minh họa toàn bài; người duyệt cần kiểm tra sự phù hợp với từng cảnh và quyền tái sử dụng ảnh. Với Dân trí, chỉ lấy ảnh trong nội dung chính, gộp các kích thước của cùng ảnh và dùng ảnh đại diện khi không có ảnh nội dung tải được. Video Dân trí được đối chiếu `data-video-id` với `VideoObject` trong JSON-LD để lấy đúng URL HLS, thay vì dùng đường dẫn `.mov` tương đối. Ưu tiên video MP4 hoặc HLS trong thẻ `video`/`source` hoặc metadata đã đối chiếu của bài, sau đó mới dùng ảnh và thẻ tin. Hỗ trợ HLS MPEG-TS và AES-128 với khóa công khai của trình phát; chưa hỗ trợ iframe bên thứ ba, DRM, HLS fMP4/byte-range. Tải tối đa 90 giây đầu và 120 MiB mỗi bài, cắt các đoạn nối tiếp theo cảnh, lặp lại nếu clip ngắn hơn lời đọc. Giữ tỷ lệ hình trong khung, tắt tiếng nguồn và dùng giọng đọc bản tin. Mọi playlist, khóa và phân đoạn đều được tải qua HTTP client chặn mạng nội bộ; FFmpeg chỉ đọc bản cục bộ.

Chạy `docker compose up --build -d` rồi tạo video mới hoặc lưu kịch bản thành phiên bản mới để áp dụng giao diện. MP4 đã dựng giữ nguyên. Không cần thêm khóa API ảnh.

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

Chọn 1–6 ID bài **cùng một sự kiện**, thay `[1,2]` bằng ID thực tế. RSS chỉ có mô tả ngắn có thể không đủ dữ kiện. Worker có thể bổ sung nội dung từ các báo đã hỗ trợ (gồm VnExpress, Dân trí) và tải ảnh đại diện của bài được chọn để minh họa video.

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

Từ chối bằng `decision=rejected`. Duyệt chỉ thay trạng thái, **không đăng lên TikTok**. Các trạng thái: `queued → processing → pending_review → approved/rejected`; lỗi tạm thời được thử tối đa 3 lần, sau đó `failed`. Lỗi HTTP 4xx từ AI (trừ 408/429) chuyển ngay sang `failed` vì gửi lại cùng request không khắc phục được lỗi. Xem `last_error` và log worker khi pipeline dừng. Video phải dài hơn 60 giây và tối đa 180 giây; mục tiêu kịch bản là 75–120 giây với 260–480 từ (đếm theo khoảng trắng). Thời lượng được đo từ giọng đọc thực tế; video ngắn hơn yêu cầu sẽ báo lỗi, không chèn khoảng im lặng để kéo dài.

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

Chưa có semantic clustering/pgvector, kiểm chứng ngoài các bài đầu vào, asset có bản quyền, R2, TikTok OAuth/publishing hoặc analytics. Người biên tập vẫn chọn các bài cùng sự kiện. Pipeline dùng template So Híp với ảnh đại diện bài viết chuyển động nhẹ và thẻ tin dự phòng; phụ đề chia cụm từ, thời gian trong cảnh được ước lượng theo số từ, chưa có forced alignment. Mỗi video luôn cần người duyệt.

OpenAI adapter đối chiếu với tài liệu [Structured Outputs](https://developers.openai.com/api/docs/guides/structured-outputs) và [Create speech](https://developers.openai.com/api/reference/resources/audio/subresources/speech/methods/create). JSON schema và kiểm tra quote giúp chặn bằng chứng bịa; chúng không chứng minh thông tin ngoài đời là đúng.

## Khắc phục lỗi Google HTTP 400

Adapter gửi `responseFormat.text.mimeType=APPLICATION_JSON` theo REST API của Google. Nếu worker cũ báo `google AI HTTP 400, request_id=`, cập nhật image bằng `sudo docker compose up --build -d api worker`, rồi tạo video mới từ các bài đã chọn. Video đã `failed` không tự chạy lại.

Log mới và `last_error` hiển thị bước thất bại, endpoint/model, mã và thông báo lỗi của nhà cung cấp; các khóa API và token quản trị được che. Xem bằng `sudo docker compose logs -f worker`. HTTP 408/429 và 5xx vẫn thử lại tối đa 3 lần.

Nếu AI trả `claims=[]` và `scenes=[]`, worker báo nguồn chưa đủ dữ kiện và dừng job thay vì gọi lại cùng đầu vào. Hãy chọn thêm bài liên quan có nội dung đủ dài. Bộ đọc feed ưu tiên RSS `content:encoded` và Atom `content`, rồi mới dùng mô tả/tóm tắt; worker hỗ trợ tải nội dung bài VnExpress được chọn khi nội dung đang lưu dưới 70 từ. Bài đã lưu trước khi cập nhật vẫn giữ nội dung cũ. Kịch bản không đạt kiểm tra được lưu để chẩn đoán và không đưa sang TTS/render.

Lỗi AI HTTP 429/5xx được chờ 2 phút trước lần 2, 4 phút trước lần 3; các lỗi tạm thời khác chờ 5 giây. Log `video retry scheduled` kèm `retry_after` nghĩa là job còn queued; `video failed` chỉ xuất hiện khi kết thúc. Không tự đổi model/provider. Backoff không đảm bảo model hết quá tải.

Tạo video từ một bài VnExpress: worker bổ sung các đoạn nội dung chính từ trang bài gốc nếu RSS đang lưu dưới 70 từ, giữ lại mô tả ban đầu để không mất bằng chứng. Chỉ tải bài được chọn, HTTPS trên vnexpress.net/www.vnexpress.net, chặn redirect ngoài nguồn và IP nội bộ, giới hạn HTML 12 MiB/30 giây. Nếu tải/trích xuất thất bại, log `load full article` và không gọi AI/TTS. Các nguồn được hỗ trợ khác được mô tả ở phần Các nguồn quốc tế bên dưới. Bài có nội dung đủ dài vẫn phải vượt kiểm tra dữ kiện.


### Các nguồn quốc tế

Bộ đọc chi tiết hỗ trợ bài BBC News, Guardian (kể cả live blog), CNN và phần nội dung công khai của NYTimes. Worker tải trang gốc của các nguồn này khi tạo kịch bản thay vì coi mô tả RSS là bài đầy đủ; giữ lại mô tả để bảo toàn bằng chứng. HTML bài có giới hạn riêng 12 MiB, RSS vẫn 4 MiB; URL và các lượt chuyển hướng vẫn bị chặn IP nội bộ. Nếu nguồn chặn truy cập hoặc chỉ có video mà thiếu nội dung đủ dài, job dừng trước AI/TTS và báo lỗi, không suy đoán nội dung.

Quét media loại vùng tin liên quan và thông tin tác giả; nhóm các kích thước/định dạng của cùng ảnh BBC; giữ dấu phẩy trong URL biến đổi ảnh CNN. Video MP4/HLS trong JSON-LD chỉ được nhận khi metadata xác định đúng trang bài đang chọn. Trình phát chỉ cung cấp embed (như BBC mẫu đã kiểm tra) hiển thị cảnh báo trong bước chọn media; chưa hỗ trợ lấy stream qua API trình phát hoặc vượt trang yêu cầu đăng nhập/JavaScript. RSS NYTimes có cả `link` và `atom:link`; bộ đọc giữ URL của thẻ RSS để tránh bỏ mất bài.


ABC News hỗ trợ vùng `prism-article-body`/`Article__Content`, loại các khối `prism-collection` (tin gợi ý) và quảng cáo. Al Jazeera hỗ trợ vùng `wysiwyg--all-content`. Ảnh trong bài và ảnh đại diện được đưa vào bước chọn media. Native video và metadata MP4/HLS khớp trang dùng chung bộ quét; trình phát riêng của ABC chưa được hỗ trợ sẽ hiển thị cảnh báo. Al Jazeera/Brightcove được hỗ trợ qua playback công khai như mô tả bên dưới.

Reuters: adapter nội dung nhận vùng `article-body`/`ArticleBody` trên URL Reuters trực tiếp; chưa xác minh trên trang thực tế vì trang mẫu trả challenge JavaScript. Nguồn Google News RSS hiện lưu URL `news.google.com/rss/articles/...`, không phải URL Reuters. Hệ thống không dùng nội dung hoặc ảnh thẻ Google News làm bài gốc: bước chọn media báo thiếu URL gốc, worker dừng và yêu cầu URL bài gốc. Chưa hỗ trợ phân giải các token Google News hoặc vượt challenge Reuters.


Trang Nguồn tin có tìm kiếm theo tên/URL và lọc trạng thái, khung thêm/thiết lập nguồn cố định khi cuộn. Nút Xóa nguồn yêu cầu xác nhận, dừng thu thập và ẩn nguồn khỏi danh sách; bài và video cũ được giữ để bảo toàn dẫn nguồn. Thêm lại cùng URL RSS sẽ khôi phục nguồn ở trạng thái tắt. Migration `004_source_deletion.sql` bổ sung cột `deleted_at`. API quản lý dùng `DELETE /api/sources/{id}`.

Trang Sản xuất video tìm tiêu đề trong toàn bộ DB qua `GET /api/videos?q=...&status=...`, chờ 400 ms khi nhập và trả tối đa 100 kết quả mới nhất khớp bộ lọc.


Khi có video queued/processing, giao diện kiểm tra trạng thái mỗi 10 giây trong nền và tạm ngừng khi tab trình duyệt bị ẩn. Chỉ cập nhật video/tổng quan; không tải lại danh sách bài viết, không bật loading và không đặt lại bản nháp hay vị trí cuộn. Khi không còn video sản xuất, tự dừng kiểm tra định kỳ. Trang video Al Jazeera có thân bài trống được dùng mô tả công khai ở `article__subhead`; không giả định có bản chép lời hoặc nội dung video chưa được đọc. Kịch bản tổng hợp vẫn phải đạt kiểm tra dữ kiện và độ dài.


Al Jazeera: khi VideoObject trong JSON-LD khớp tiêu đề h1 của bài, bộ quét đọc cấu hình public Brightcove và Playback API của đúng account/videoId để lấy MP4/HLS. Các độ phân giải gộp vào một mục video, ưu tiên MP4. Chỉ dùng HTTPS, loại nguồn DRM và kiểm tra ID trả về; mọi lần tải vẫn qua client chặn IP nội bộ, giới hạn dung lượng/thời gian và FFmpeg chỉ xử lý file cục bộ. Trường hợp cần xác thực hoặc bị giới hạn truy cập vẫn báo cảnh báo, không vượt các giới hạn đó.

Bước chọn media dùng ảnh xem trước JPEG tối đa 640×360, tải trễ và lưu cache; ảnh/video gốc vẫn giữ nguyên cho render. API giới hạn hai tác vụ tạo ảnh xem trước đồng thời. Video chỉ mở trình phát khi bấm Xem video, tối đa một trình phát tại một thời điểm; chọn tất cả hoặc tích checkbox không tải video gốc vào trình duyệt. Kiểm thử tương tác Chrome: sau `npm run build` trong thư mục `web`, chạy `NEWSBOT_BROWSER_TEST=1 node --test tests/media-picker.integration.mjs` (cần Google Chrome).

Thanh Niên/VTV: quét vùng `detail-content[itemprop=articleBody]`, loại khối tin liên quan `RelatedOneNews`/`RelatedNewsBox`; đọc URL MP4/HLS của khối `VideoStream[data-vid]`, bổ sung HTTPS cho URL Thanh Niên thiếu scheme. Trang video VTV có player ngoài thân bài được đối chiếu `data-videoFocus-id` với ID trong URL bài, không lấy các player gợi ý. Kiểm chứng ngày 09/10/2026: ảnh và video Thanh Niên/Dân trí tải và giải mã thành công; ảnh VTV tải được, nhưng hai video mẫu VTV trả HTTP 400 từ CDN, RSS VTV trả 503 tại thời điểm kiểm tra. Phát hiện URL không đồng nghĩa CDN luôn cho tải. Kiểm thử live: `NEWSBOT_VI_MEDIA_URL=<URL> NEWSBOT_VI_MEDIA_VIDEO=1 go test ./internal/newsbot -run '^TestVietnameseLiveMedia$' -v` (cần FFmpeg).

## Chuyển nguồn tin sang máy khác

Ở mục **Nguồn tin**, chọn **Xuất JSON** để tải `newsbot-sources.json`. Trên máy đích, đăng nhập và chọn **Nhập JSON**, rồi chọn file đó. File có định dạng `newsbot-sources`, phiên bản 1; giữ tên, URL RSS, ngôn ngữ, chủ đề, bật/tắt, ghi chú quyền sử dụng và các cờ quyền dùng ảnh/video/quy nguồn.

Nhập sẽ thêm nguồn mới và cập nhật cấu hình nguồn trùng URL RSS, bao gồm trạng thái bật/tắt; nguồn đã xóa có cùng URL được khôi phục. Nguồn ngoài file vẫn được giữ. Không chuyển ID nội bộ, lịch sử thu thập, bài viết, video hay khóa API. File được kiểm tra trước khi nhập toàn bộ trong một giao dịch; giới hạn 1 MiB và 1000 nguồn. Có thể nhập lại cùng file mà không tạo nguồn trùng.

API quản trị: `GET /api/sources/export` tải JSON, `POST /api/sources/import` nhận chính nội dung JSON đó. Cả hai yêu cầu Bearer ADMIN_TOKEN.

## Giọng đọc tự động theo chủ đề (Google 3.8 TTS)

Với `TTS_PROVIDER=google` (hoặc AI_PROVIDER=google và TTS_PROVIDER trống) và `GOOGLE_TTS_MODEL=gemini-3.8-flash-tts` / `gemini-3.8-flash-lite-tts`, mặc định worker phân loại **kịch bản đã vượt kiểm tra dữ kiện** rồi chọn phong cách đọc. Phân loại dùng thêm một request text tới AI_PROVIDER, không chỉ dựa vào chủ đề nguồn RSS.

- HEALTH: giọng nữ tiếng Việt ấm, đồng cảm; đọc chậm vừa, rõ thuật ngữ, không gây hoảng sợ.
- TECHNOLOGY/SCIENCE/AUTO: giọng sáng, rõ, tò mò; nhịp linh hoạt vừa phải.
- SPORTS: có năng lượng, nhấn kết quả, không hét.
- BUSINESS/REAL_ESTATE: điềm tĩnh, rõ số liệu.
- WORLD/VIETNAM/LAW/TOP_NEWS: nghiêm túc, tiết chế.
- CHARITY: nhẹ nhàng, cảm thông; EDUCATION: kiên nhẫn, dễ hiểu.
- ENTERTAINMENT/TRAVEL: gần gũi, tự nhiên; GENERAL: giọng bản tin trung tính.

Voice Design gọi `/voices` để tạo `voice_...`; lưu ID trong PostgreSQL (`005_designed_voices.sql`) và dùng lại giữa các cảnh/video, kể cả khi worker khởi động lại. Cache phụ thuộc model, tài khoản API và prompt; đổi prompt/key/model sẽ tạo giọng khác. Cache làm mới sau 11 tháng. Google có giới hạn số giọng lưu và TTL; request tạo giọng/phân loại có thể phát sinh phí. Không tạo giọng mới cho từng cảnh. Cảm xúc được truyền riêng qua `speech_metadata.style`, giữ nguyên văn narration đã duyệt; mô tả giọng không chứng nhận độ chính xác y khoa.

```dotenv
TTS_AUTO_STYLE=true
GOOGLE_VOICE_DESIGN=true
# Dùng voice đã tạo ở Google AI Studio thì không gọi tạo giọng cho chủ đề đó:
GOOGLE_VOICE_ID_HEALTH=voice_your_health_voice_id
# Hoặc để ID trống để tự tạo; tùy chỉnh prompt và cách thể hiện:
GOOGLE_VOICE_PROMPT_HEALTH=A professional Vietnamese female voice in her mid-30s with a warm, empathetic timbre and clear articulation.
GOOGLE_SPEECH_STYLE_HEALTH=warm, calm and reassuring; moderate pacing and careful pronunciation
```

Các biến `GOOGLE_VOICE_ID_<CATEGORY>`, `GOOGLE_VOICE_PROMPT_<CATEGORY>`, `GOOGLE_SPEECH_STYLE_<CATEGORY>` hỗ trợ mọi category ở trên. `GOOGLE_VOICE_DESIGN=false` dùng GOOGLE_VOICE cùng style tự động, không tạo giọng mới. `TTS_AUTO_STYLE=false` tắt cả phân loại và chọn giọng tự động, trở về GOOGLE_VOICE. OpenAI TTS và model Google cũ giữ hành vi hiện tại. Lỗi tạo giọng dừng pipeline trước khi render; không âm thầm chuyển giọng/provider.

Cập nhật có migration:

```bash
sudo docker compose build api worker migrate
sudo docker compose run --rm migrate
sudo docker compose up -d api worker
```

Log `speech profile selected` hiển thị chủ đề đã chọn. Video đã xuất giữ audio cũ; tạo video/bản sửa mới để áp dụng giọng đọc mới.

Đối chiếu API: [Google Voice Design](https://ai.google.dev/gemini-api/docs/generate-content/voice-design), [Google TTS và speech_metadata](https://ai.google.dev/gemini-api/docs/generate-content/speech-generation).

### Nội dung đầy đủ từ Dân trí

Worker lấy toàn văn bài Dân trí được chọn trước Generate, kể cả bài đã lưu chỉ có RSS hoặc mô tả trên 70 từ. Hỗ trợ URL HTTPS dantri.com.vn/www.dantri.com.vn với đường dẫn bài `.htm`; đọc vùng `#desktop-in-article` (giao diện mới) hoặc `.singular-content` (giao diện cũ). Bỏ quảng cáo, ảnh/chú thích, khối liên quan và trình phát; giới hạn trang 12 MiB, nội dung lưu 12.000 ký tự, timeout 30 giây và chặn IP nội bộ/redirect ngoài Dân trí. Giữ RSS ban đầu để không mất bằng chứng cũ và không thêm lại toàn văn nếu đã có.

Log `full article loaded` có article_id và số từ đã lấy, không ghi toàn văn. Nếu lấy nội dung thất bại, báo `load full article` trước khi gọi AI. Tạo video mới để chạy bước này; video failed không tự chạy lại. Bộ trích xuất đã được kiểm tra trên bài về cà phê và gan trong báo cáo lỗi ngày 09/10/2026; vẫn giữ kiểm tra nguồn và không đảm bảo mọi kịch bản AI sẽ được chấp nhận.

### Chặn audio Google TTS lệch ngôn ngữ

Mọi request Google TTS gửi `speechConfig.languageCode=vi-VN`, kể cả giọng mặc định, Voice Design và khi tắt TTS_AUTO_STYLE. Mặc định `GOOGLE_VERIFY_VIETNAMESE=true`: sau mỗi cảnh, gửi audio WAV thật tới model Google hỗ trợ audio để phiên âm nguyên ngữ và kiểm tra toàn bộ lời kể tiếng Việt (cho phép tên riêng/chữ viết tắt nước ngoài trong câu tiếng Việt). Không gửi narration dự kiến cho checker để giảm nguy cơ nó sao chép kịch bản thay vì nghe audio.

Nếu kiểm tra không đạt, tạo lại riêng cảnh đó tối đa 3 lần với cùng voice và languageCode; hết lượt thì dừng job, không ghép audio bị từ chối. Nếu checker lỗi hoặc không trả kết quả hợp lệ thì dừng lượt xử lý, không bỏ qua kiểm tra. Lỗi chỉ rõ `TTS cảnh 06` (đếm từ 1); log `TTS audio rejected: not Vietnamese` ghi lần tạo bị từ chối. Kiểm tra bằng AI có thể nhận định sai; chưa phải chứng nhận tuyệt đối về ngôn ngữ, nên vẫn nghe video ở bước duyệt.

`GOOGLE_AUDIO_CHECK_MODEL` mặc định theo GOOGLE_TEXT_MODEL, phải hỗ trợ audio đầu vào và structured output. Có thêm một request hiểu audio mỗi cảnh (và các lượt tạo lại nếu sai tiếng), tăng chi phí/thời gian. `GOOGLE_VERIFY_VIETNAMESE=false` tắt checker, chỉ còn languageCode nên không nên dùng nếu yêu cầu chặn audio sai tiếng. Audio/video đã tạo trước đây không tự thay đổi; tạo bản sửa hoặc video mới để áp dụng. Không cần migration mới.
