package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"

	"cups-web/internal/store"

	"github.com/phpdave11/gofpdf"
	"golang.org/x/image/draw"
)

// ── 扫描项目导出 PDF ───────────────────────────────────────────────────────────
//
// 前端原先用 pdf-lib 在浏览器里把每页 PNG 无损嵌入 PDF，既占内存、大文件也慢，
// 而且无法给用户压缩质量选项。这里改为后端生成，并沿用 /api/scan/stream 的
// 「JSON 事件行 + 二进制数据」流式协议，让前端能实时看到导出进度。

// scanExportEvent 是流式响应里的一行 JSON（以 \n 结尾）。
// type=progress 表示预处理进度；type=ready 表示 PDF 已就绪，其后紧跟 size 字节的 PDF 数据；
// type=error 表示中途失败，message 为可读错误。
type scanExportEvent struct {
	Type    string `json:"type"`
	Current int    `json:"current,omitempty"`
	Total   int    `json:"total,omitempty"`
	Percent int    `json:"percent,omitempty"`
	Size    int64  `json:"size,omitempty"`
	Message string `json:"message,omitempty"`
}

type exportScanPDFRequest struct {
	ImageFormat string `json:"imageFormat"` // "jpeg" | "png"
	Quality     int    `json:"quality"`     // 1-100，仅 JPEG 生效
}

func (r *exportScanPDFRequest) normalize() error {
	if r.ImageFormat == "" {
		r.ImageFormat = "jpeg"
	}
	switch r.ImageFormat {
	case "jpeg", "png":
	default:
		return errors.New("unsupported image format")
	}
	if r.Quality == 0 {
		r.Quality = 85
	}
	if r.Quality < 1 || r.Quality > 100 {
		return errors.New("quality must be between 1 and 100")
	}
	return nil
}

type exportScanPDFOptions struct {
	imageFormat string
	quality     int
}

func exportScanPDFHandler(w http.ResponseWriter, r *http.Request) {
	sess, projectID, ok := scanProjectSessionAndID(w, r)
	if !ok {
		return
	}
	var req exportScanPDFRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := req.normalize(); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	var pages []store.ScanPage
	if err := appStore.WithTx(r.Context(), true, func(tx *sql.Tx) error {
		var err error
		pages, err = store.ListScanPages(r.Context(), tx, sess.UserID, projectID)
		return err
	}); err != nil {
		writeScanProjectError(w, err, "failed to load scan project")
		return
	}
	if len(pages) == 0 {
		writeJSONError(w, http.StatusBadRequest, "project has no pages")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSONError(w, http.StatusInternalServerError, "streaming is not supported by this server")
		return
	}
	w.Header().Set("Cache-Control", "no-store, no-transform")
	w.Header().Set("Content-Type", "application/x-cups-web-pdf-stream")
	w.Header().Set("X-Accel-Buffering", "no")

	writeEvent := func(ev scanExportEvent) bool {
		data, _ := json.Marshal(ev)
		if _, err := w.Write(append(data, '\n')); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}

	pdfPath, cleanup, err := buildScanProjectPDF(r.Context(), pages, exportScanPDFOptions{
		imageFormat: req.ImageFormat,
		quality:     req.Quality,
	}, func(current, total int) {
		writeEvent(scanExportEvent{Type: "progress", Current: current, Total: total, Percent: current * 100 / total})
	})
	if err != nil {
		writeEvent(scanExportEvent{Type: "error", Message: err.Error()})
		return
	}
	defer cleanup()

	stat, err := os.Stat(pdfPath)
	if err != nil {
		writeEvent(scanExportEvent{Type: "error", Message: "failed to stat exported pdf"})
		return
	}
	if !writeEvent(scanExportEvent{Type: "ready", Size: stat.Size()}) {
		return
	}
	file, err := os.Open(pdfPath)
	if err != nil {
		return
	}
	defer file.Close()
	_, _ = io.Copy(w, file)
}

// buildScanProjectPDF 把项目内所有页面合并为单个 A4 PDF。
// progress 在每页图片预处理完成后回调一次（用于流式上报进度）。
// 返回的 cleanup 负责清理临时目录。
func buildScanProjectPDF(ctx context.Context, pages []store.ScanPage, opts exportScanPDFOptions, progress func(current, total int)) (string, func(), error) {
	tmpDir, err := os.MkdirTemp("", "scan-export-")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { _ = os.RemoveAll(tmpDir) }

	total := len(pages)
	type pageImage struct {
		path string
		cfg  image.Config
	}
	images := make([]pageImage, 0, total)
	root := scanStorageDir()
	for index, page := range pages {
		if ctx.Err() != nil {
			cleanup()
			return "", nil, ctx.Err()
		}
		rel := page.EditedPath
		if rel == "" {
			rel = page.OriginalPath
		}
		abs := filepath.Join(root, filepath.FromSlash(rel))
		path, cfg, err := convertScanPageForPDF(abs, tmpDir, opts)
		if err != nil {
			cleanup()
			return "", nil, fmt.Errorf("failed to process page %d: %w", index+1, err)
		}
		images = append(images, pageImage{path: path, cfg: cfg})
		if progress != nil {
			progress(index+1, total)
		}
	}

	pdf := gofpdf.New("P", "mm", "A4", "")
	pdf.SetMargins(pdfPageMarginMM, pdfPageMarginMM, pdfPageMarginMM)
	pdf.SetAutoPageBreak(false, pdfPageMarginMM)
	for _, img := range images {
		pdf.AddPage()
		pageW, pageH := pdf.GetPageSize()
		maxW := pageW - 2*pdfPageMarginMM
		maxH := pageH - 2*pdfPageMarginMM
		scale := math.Min(maxW/float64(img.cfg.Width), maxH/float64(img.cfg.Height))
		if scale <= 0 {
			scale = 1
		}
		w := float64(img.cfg.Width) * scale
		h := float64(img.cfg.Height) * scale
		x := (pageW - w) / 2
		y := (pageH - h) / 2
		imgOpts := gofpdf.ImageOptions{ImageType: "", ReadDpi: true}
		pdf.ImageOptions(img.path, x, y, w, h, false, imgOpts, 0, "")
	}

	outPath := filepath.Join(tmpDir, "export.pdf")
	if err := pdf.OutputFileAndClose(outPath); err != nil {
		cleanup()
		return "", nil, err
	}
	return outPath, cleanup, nil
}

// convertScanPageForPDF 把扫描页 PNG 转成适合嵌入 PDF 的图片：
//   - png 模式：无损，原样返回 PNG 路径（文件较大）；
//   - jpeg 模式：解码后（必要时下采样到 imageDownscaleMaxEdge）以 JPEG 写出，
//     质量由 opts.quality 控制，透明区域以白底合成，大幅减小 PDF 体积。
func convertScanPageForPDF(inputPath, tmpDir string, opts exportScanPDFOptions) (string, image.Config, error) {
	file, err := os.Open(inputPath)
	if err != nil {
		return "", image.Config{}, err
	}
	cfg, format, err := image.DecodeConfig(file)
	_ = file.Close()
	if err != nil || cfg.Width < 1 || cfg.Height < 1 {
		return "", image.Config{}, errors.New("invalid scan page image")
	}
	if format != "png" {
		return "", image.Config{}, errors.New("scan page is not a PNG image")
	}
	if opts.imageFormat == "png" {
		return inputPath, cfg, nil
	}

	srcFile, err := os.Open(inputPath)
	if err != nil {
		return "", image.Config{}, err
	}
	srcImg, _, err := image.Decode(srcFile)
	_ = srcFile.Close()
	if err != nil {
		return "", image.Config{}, err
	}

	longEdge := cfg.Width
	if cfg.Height > longEdge {
		longEdge = cfg.Height
	}
	scale := 1.0
	if longEdge > imageDownscaleMaxEdge {
		scale = float64(imageDownscaleMaxEdge) / float64(longEdge)
	}
	dstW := int(math.Round(float64(cfg.Width) * scale))
	dstH := int(math.Round(float64(cfg.Height) * scale))
	if dstW < 1 {
		dstW = 1
	}
	if dstH < 1 {
		dstH = 1
	}

	// 先铺白底，再用 Over 合成，保证带透明通道的 PNG 输出为白底而非黑底。
	dstImg := image.NewRGBA(image.Rect(0, 0, dstW, dstH))
	draw.Draw(dstImg, dstImg.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)
	if scale == 1.0 {
		draw.Draw(dstImg, dstImg.Bounds(), srcImg, image.Point{}, draw.Over)
	} else {
		draw.CatmullRom.Scale(dstImg, dstImg.Bounds(), srcImg, srcImg.Bounds(), draw.Over, nil)
	}

	seq := atomic.AddUint64(&downscaleSeq, 1)
	outPath := filepath.Join(tmpDir, "page_"+itoa(int(seq))+".jpg")
	outFile, err := os.Create(outPath)
	if err != nil {
		return "", image.Config{}, err
	}
	if err := jpeg.Encode(outFile, dstImg, &jpeg.Options{Quality: opts.quality}); err != nil {
		_ = outFile.Close()
		_ = os.Remove(outPath)
		return "", image.Config{}, err
	}
	if err := outFile.Close(); err != nil {
		_ = os.Remove(outPath)
		return "", image.Config{}, err
	}
	return outPath, image.Config{Width: dstW, Height: dstH}, nil
}
