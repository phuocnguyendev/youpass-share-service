// Package archtest kiểm tra Dependency Rule của Clean Architecture bằng test:
// lớp trong không bao giờ được import lớp ngoài hay framework.
package archtest

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

const module = "github.com/phuocnguyendev/youpass-share-service/internal/"

var frameworks = []string{
	"github.com/gin-gonic/", "github.com/jackc/pgx", "github.com/redis/",
	"github.com/prometheus/", "github.com/golang-jwt/", "net/http", "database/sql",
}

var rules = []struct {
	layer     string   // thư mục (tương đối với internal/)
	forbidden []string // prefix import bị cấm (tương đối với internal/)
}{
	{"domain", []string{"usecase", "adapter", "infrastructure"}},
	{"usecase", []string{"adapter", "infrastructure"}},
	{"adapter", []string{"infrastructure"}},
}

func TestDependencyRule(t *testing.T) {
	for _, rule := range rules {
		dir := filepath.Join("..", rule.layer)
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
			if err != nil {
				return err
			}
			for _, imp := range file.Imports {
				p := strings.Trim(imp.Path.Value, `"`)
				for _, f := range rule.forbidden {
					if strings.HasPrefix(p, module+f) {
						t.Errorf("%s: layer %q must not import %q", path, rule.layer, p)
					}
				}
				if rule.layer != "adapter" {
					for _, fw := range frameworks {
						if strings.HasPrefix(p, fw) {
							t.Errorf("%s: layer %q must not depend on framework %q", path, rule.layer, p)
						}
					}
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
