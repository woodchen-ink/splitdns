package handler

import (
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/woodchen-ink/go-web-utils/resputil"
	"github.com/woodchen-ink/splitdns/server/service"
)

// ExportDatabase GET /api/export/db 下载一份数据库快照。
//
// 文件里有明文的平台密钥, 所以这个接口跟其它接口一样躺在 Access 后面;
// 下下来之后请当作密钥文件对待, 别随手丢共享盘。
func ExportDatabase(w http.ResponseWriter, r *http.Request) {
	path, cleanup, err := service.ExportDatabase()
	if err != nil {
		resputil.Fail(w, 500, err.Error())
		return
	}
	defer cleanup()

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf(`attachment; filename="%s"`, service.ExportFileName()))
	// 备份文件不该被任何一层缓存留下来
	w.Header().Set("Cache-Control", "no-store")
	http.ServeFile(w, r, path)
}

// maxImportSize 限制上传大小。这个库正常也就几百 KB, 给到 64MB 已经很宽裕,
// 主要是别让一个超大文件把内存和磁盘占满。
const maxImportSize = 64 << 20

// ImportDatabase POST /api/import/db 用上传的库文件整体替换当前数据。
func ImportDatabase(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxImportSize)
	file, header, err := r.FormFile("file")
	if err != nil {
		resputil.Fail(w, 400, "没读到上传的文件: "+err.Error())
		return
	}
	defer file.Close()

	tmp, err := os.CreateTemp("", "splitdns-import-*.db")
	if err != nil {
		resputil.Fail(w, 500, "创建临时文件失败: "+err.Error())
		return
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := io.Copy(tmp, file); err != nil {
		tmp.Close()
		resputil.Fail(w, 400, "接收文件失败: "+err.Error())
		return
	}
	if err := tmp.Close(); err != nil {
		resputil.Fail(w, 500, err.Error())
		return
	}

	backup, err := service.ImportDatabase(tmpPath)
	if err != nil {
		resputil.Fail(w, 400, err.Error())
		return
	}
	resputil.OKMsg(w, map[string]string{"backupPath": backup},
		fmt.Sprintf("已导入 %s, 原有数据备份在 %s", header.Filename, backup))
}
