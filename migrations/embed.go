// Package migrations nhúng các file SQL vào binary, chạy tự động khi service khởi động.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
