#!/usr/bin/env bash
# Demo end-to-end: tạo link → xem → đếm view → tắt/bật → xoá → 404/410 → rate limit.
# Yêu cầu: đã chạy `make up`. Chạy lại demo: đợi ~60s để cửa sổ rate limit reset.
set -uo pipefail

API="${API:-http://localhost:${API_PORT:-8080}}"
FLUSH_WAIT="${FLUSH_WAIT:-6}"
UA="Mozilla/5.0 (Macintosh; Intel Mac OS X 14_0) AppleWebKit/537.36 Chrome/128.0 Safari/537.36"
RUN_ID="$(date +%s)"
FAILED=0

section() { printf '\n\033[1;34m▶ %s\033[0m\n' "$*"; }

# call METHOD PATH [curl args...] → lưu $STATUS, $BODY
call() {
  local method=$1 path=$2 out
  shift 2
  out=$(curl -sS -X "$method" "$API$path" -A "$UA" -w $'\n%{http_code}' "$@")
  STATUS=${out##*$'\n'}
  BODY=${out%$'\n'*}
  printf '  HTTP %s  %s\n' "$STATUS" "$(printf '%s' "$BODY" | tr -d '\n' | cut -c1-150)"
}

expect() {
  if [[ "$STATUS" == "$1" ]]; then
    printf '  \033[32m✔ %s\033[0m\n' "$2"
  else
    printf '  \033[31m✘ %s (mong đợi %s, nhận %s)\033[0m\n' "$2" "$1" "$STATUS"
    FAILED=1
  fi
}

str_field() { printf '%s' "$BODY" | grep -o "\"$1\":\"[^\"]*\"" | head -1 | cut -d'"' -f4; }
num_field() { printf '%s' "$BODY" | grep -o "\"$1\":[0-9]*" | head -1 | cut -d: -f2; }

auth() { printf 'Authorization: Bearer %s' "$1"; }
json='Content-Type: application/json'

# ---------------------------------------------------------------------------
section "0. Health check"
call GET /readyz
expect 200 "API, PostgreSQL và Redis sẵn sàng"
[[ "$STATUS" == "200" ]] || { echo "API chưa chạy? Hãy chạy: make up"; exit 1; }

section "1. Lấy access token (dev) cho học viên #1 (Minh Anh) và #2 (Hoàng Nam)"
call POST /api/v1/dev/token -H "$json" -d '{"userId":1}'; T1=$(str_field accessToken)
call POST /api/v1/dev/token -H "$json" -d '{"userId":2}'; T2=$(str_field accessToken)

section "2. Học viên #1 chia sẻ bài Writing #101"
call POST /api/v1/shares -H "$(auth "$T1")" -H "$json" -d '{"resourceType":"writing_submission","resourceId":101}'
if [[ "$STATUS" == "200" ]]; then # link từ lần demo trước → xoá để bắt đầu sạch
  call DELETE "/api/v1/shares/$(str_field code)" -H "$(auth "$T1")"
  call POST /api/v1/shares -H "$(auth "$T1")" -H "$json" -d '{"resourceType":"writing_submission","resourceId":101}'
fi
expect 201 "Tạo link mới"
CODE=$(str_field code)
echo "  → URL: $(str_field url)"

section "3. Bấm 'Chia sẻ' lần nữa → idempotent, trả đúng link cũ"
call POST /api/v1/shares -H "$(auth "$T1")" -H "$json" -d '{"resourceType":"writing_submission","resourceId":101}'
expect 200 "Không tạo link trùng"
[[ "$(str_field code)" == "$CODE" ]] && echo "  ✔ Cùng mã $CODE" || { echo "  ✘ Mã khác!"; FAILED=1; }

section "4. Học viên #2 cố chia sẻ bài của học viên #1"
call POST /api/v1/shares -H "$(auth "$T2")" -H "$json" -d '{"resourceType":"writing_submission","resourceId":101}'
expect 403 "Không phải chủ bài → 403"

section "5. Người xem ẩn danh mở link"
call GET "/api/v1/shares/$CODE" -H "X-Visitor-Id: a-$RUN_ID"
expect 200 "Xem được bài làm"

section "6. Cùng người xem F5 thêm 5 lần (dedupe 30 phút)"
for _ in 1 2 3 4 5; do call GET "/api/v1/shares/$CODE" -H "X-Visitor-Id: a-$RUN_ID" >/dev/null; done
echo "  (5 request, chỉ tính 1 view)"

section "7. Thêm 2 người xem khác"
call GET "/api/v1/shares/$CODE" -H "X-Visitor-Id: b-$RUN_ID"
call GET "/api/v1/shares/$CODE" -H "X-Visitor-Id: c-$RUN_ID"

section "8. Crawler Facebook lấy preview + chủ bài tự xem → KHÔNG tính view"
STATUS=$(curl -s -o /dev/null -w '%{http_code}' -A "facebookexternalhit/1.1" "$API/api/v1/shares/$CODE")
expect 200 "Bot vẫn nhận nội dung (để render preview)"
call GET "/api/v1/shares/$CODE" -H "$(auth "$T1")"
expect 200 "Chủ bài xem bài của mình"

section "9. Chờ ${FLUSH_WAIT}s để worker flush view từ Redis xuống PostgreSQL"
sleep "$FLUSH_WAIT"
call GET "/api/v1/shares/$CODE/stats" -H "$(auth "$T1")"
expect 200 "Chủ bài xem thống kê"
[[ "$(num_field totalViews)" == "3" ]] && echo "  ✔ totalViews = 3 (a, b, c)" || { echo "  ✘ totalViews sai"; FAILED=1; }
if command -v docker >/dev/null 2>&1; then
  echo "  view_count trong PostgreSQL:"
  docker compose exec -T postgres psql -U youpass -d youpass -tAc \
    "SELECT '    ' || code || ' → ' || view_count FROM share_links WHERE code = '$CODE'" 2>/dev/null || true
fi

section "10. Học viên #2 cố xem thống kê / tắt link của học viên #1"
call GET "/api/v1/shares/$CODE/stats" -H "$(auth "$T2")"
expect 404 "Không lộ việc link có tồn tại"
call PATCH "/api/v1/shares/$CODE" -H "$(auth "$T2")" -H "$json" -d '{"status":"disabled"}'
expect 404 "Không tắt được link của người khác"

section "11. Chủ bài TẮT chia sẻ → hiệu lực ngay"
call PATCH "/api/v1/shares/$CODE" -H "$(auth "$T1")" -H "$json" -d '{"status":"disabled"}'
expect 200 "Tắt link"
call GET "/api/v1/shares/$CODE"
expect 410 "Link đã tắt → 410 Gone (dù trước đó đã nằm trong cache)"

section "12. Bật lại"
call PATCH "/api/v1/shares/$CODE" -H "$(auth "$T1")" -H "$json" -d '{"status":"active"}'
call GET "/api/v1/shares/$CODE"
expect 200 "Link hoạt động trở lại, mã giữ nguyên"

section "13. Xoá link"
call DELETE "/api/v1/shares/$CODE" -H "$(auth "$T1")"
expect 204 "Xoá link"
call GET "/api/v1/shares/$CODE"
expect 410 "Link đã xoá → 410 Gone"

section "14. Chia sẻ lại → cấp mã MỚI (mã cũ không bao giờ tái sử dụng)"
call POST /api/v1/shares -H "$(auth "$T1")" -H "$json" -d '{"resourceType":"writing_submission","resourceId":101}'
expect 201 "Tạo link mới"
NEW_CODE=$(str_field code)
[[ "$NEW_CODE" != "$CODE" ]] && echo "  ✔ $CODE → $NEW_CODE" || { echo "  ✘ Mã bị tái sử dụng!"; FAILED=1; }

section "15. Mã không tồn tại / sai định dạng"
call GET /api/v1/shares/Zz9Zz9Zz
expect 404 "Mã không tồn tại → 404 (negative cache)"
call GET "/api/v1/shares/abc-123"
expect 404 "Mã sai định dạng → 404, không chạm Redis/DB"

section "16. Rate limit: bắn 70 request liên tục (giới hạn ${RATE_LIMIT_PER_MIN:-60}/phút/IP)"
LIMITED=0
for _ in $(seq 1 70); do
  [[ "$(curl -s -o /dev/null -w '%{http_code}' -A "$UA" "$API/api/v1/shares/Zz9Zz9Zz")" == "429" ]] && LIMITED=$((LIMITED + 1))
done
if (( LIMITED > 0 )); then printf '  \033[32m✔ %d request bị chặn 429\033[0m\n' "$LIMITED"; else echo "  ✘ Không có 429"; FAILED=1; fi

# ---------------------------------------------------------------------------
printf '\n\033[1m👉 Mở trình duyệt: %s/s/%s\033[0m\n' "$API" "$NEW_CODE"
echo "   (đợi ~1 phút cho rate limit reset nếu trang báo 429)"
if (( FAILED == 0 )); then printf '\n\033[1;32mDemo PASS\033[0m\n'; else printf '\n\033[1;31mDemo có bước FAIL\033[0m\n'; exit 1; fi
