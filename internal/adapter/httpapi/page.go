package httpapi

import (
	"html/template"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/phuocnguyendev/youpass-share-service/internal/usecase/share"
)

// Trang xem bài tối giản để demo end-to-end trên trình duyệt.
// Production: Next.js SSR render /s/[code] (kèm OG meta) và gọi GET /api/v1/shares/:code.
var pageTmpl = template.Must(template.New("share").Parse(`<!doctype html>
<html lang="vi">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="robots" content="noindex, nofollow">
<title>{{.Title}} · YouPass</title>
{{- if .View}}
<meta property="og:title" content="{{.View.Title}} – Band {{printf "%.1f" .View.BandScore}}">
<meta property="og:description" content="Bài làm của {{.View.OwnerName}} được chia sẻ trên YouPass">
{{- end}}
<style>
  :root { --bg:#f5f6fa; --card:#ffffff; --text:#1c1f2a; --muted:#667085; --accent:#e5484d; --border:#e4e7ec; }
  @media (prefers-color-scheme: dark) {
    :root { --bg:#0e1015; --card:#161920; --text:#e7e9ee; --muted:#98a2b3; --border:#262a34; }
  }
  * { box-sizing: border-box; }
  body { margin:0; background:var(--bg); color:var(--text);
         font-family: system-ui, -apple-system, "Segoe UI", Roboto, sans-serif; }
  main { max-width: 760px; margin: 40px auto; padding: 0 16px; }
  .brand { font-weight: 700; color: var(--accent); margin-bottom: 16px; }
  .card { background:var(--card); border:1px solid var(--border); border-radius:14px; padding:28px; }
  .band { display:inline-block; background:var(--accent); color:#fff; border-radius:999px;
          padding:4px 12px; font-weight:600; font-size:14px; }
  h1 { font-size: 22px; margin: 14px 0 6px; line-height: 1.35; }
  h2 { font-size: 16px; margin: 28px 0 8px; }
  .muted { color:var(--muted); font-size:14px; margin: 0 0 20px; }
  .text { white-space: pre-wrap; line-height: 1.75; }
</style>
</head>
<body>
<main>
  <div class="brand">YouPass</div>
  <div class="card">
  {{- if .View}}
    <span class="band">Band {{printf "%.1f" .View.BandScore}}</span>
    <h1>{{.View.Title}}</h1>
    <p class="muted">{{.View.OwnerName}} · chia sẻ ngày {{.View.SharedAt.Format "02/01/2006"}}</p>
    <div class="text">{{.View.Content}}</div>
    {{- if .View.Feedback}}
    <h2>Nhận xét của giáo viên</h2>
    <div class="text">{{.View.Feedback}}</div>
    {{- end}}
  {{- else}}
    <h1>{{.Title}}</h1>
    <p class="muted">{{.Message}}</p>
  {{- end}}
  </div>
</main>
</body>
</html>`))

type pageData struct {
	Title   string
	Message string
	View    *share.SharedView
}

func renderPage(c *gin.Context, status int, view *share.SharedView) {
	data := pageData{View: view}
	switch status {
	case http.StatusOK:
		data.Title = view.Title
	case http.StatusNotFound:
		data.Title, data.Message = "Không tìm thấy bài làm", "Đường dẫn không tồn tại hoặc đã bị nhập sai."
	case http.StatusGone:
		data.Title, data.Message = "Bài làm không còn được chia sẻ", "Chủ bài làm đã tắt chia sẻ hoặc đã xoá đường dẫn này."
	default:
		data.Title, data.Message = "Có lỗi xảy ra", "Vui lòng thử lại sau ít phút."
	}

	c.Status(status)
	c.Header("Content-Type", "text/html; charset=utf-8")
	_ = pageTmpl.Execute(c.Writer, data)
}
