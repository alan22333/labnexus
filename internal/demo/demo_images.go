package demo

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"os"
	"path/filepath"
)

// 演示图片直接写到 uploads 根目录:LocalFileStore 以 filepath.Base 打开文件,
// 不支持子目录(与真实上传的文件布局保持一致)。
const (
	demoSamplingImage = "demo-sampling-lineup.png"
	demoCifarImage    = "demo-cifar-samples.png"
)

// demoImage 描述一张运行时生成的演示图片及其对应的资源元数据。
type demoImage struct {
	file         string // 文件名(不含目录)
	title        string
	description  string
	originalName string
	uploader     string
	tags         []string
	postKey      string // 需要内嵌该图片的公开帖子(key),为空则只做资源

	relPath string // 落盘相对路径(uploads/…)
	size    int64  // 文件字节数
}

// demoImageDefs 返回全部演示图片定义。图片内容由代码生成,仓库无需提交二进制素材。
func demoImageDefs() []demoImage {
	return []demoImage{
		{
			file: demoSamplingImage, title: "DDIM vs DPM-Solver 采样质量对比",
			description:  "不同采样步数下的 FID 与采样耗时对比(演示图片)。",
			originalName: "sampling-quality.png", uploader: "student1",
			tags: []string{"实验", "深度学习"}, postKey: "sampling",
		},
		{
			file: demoCifarImage, title: "CIFAR-10 清洗后样例网格",
			description:  "清洗去重后的 CIFAR-10 子集样例(演示图片)。",
			originalName: "cifar10-samples.png", uploader: "student3",
			tags: []string{"数据集"}, postKey: "dataset",
		},
	}
}

// buildDemoImages 生成全部演示图片写入 uploadDir/demo-images,返回带路径与大小的元数据。
func buildDemoImages(uploadDir string) ([]demoImage, error) {
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		return nil, fmt.Errorf("create demo images dir: %w", err)
	}
	defs := demoImageDefs()
	out := make([]demoImage, 0, len(defs))
	for _, def := range defs {
		path := filepath.Join(uploadDir, def.file)
		var err error
		switch def.file {
		case demoSamplingImage:
			err = writeSamplingChart(path)
		default:
			err = writeGridImage(path)
		}
		if err != nil {
			return nil, fmt.Errorf("write demo image %s: %w", def.file, err)
		}
		info, err := os.Stat(path)
		if err != nil {
			return nil, fmt.Errorf("stat demo image %s: %w", def.file, err)
		}
		def.relPath = "uploads/" + def.file
		def.size = info.Size()
		out = append(out, def)
	}
	return out, nil
}

func writeSamplingChart(path string) error {
	const w, h = 960, 600
	img := image.NewRGBA(image.Rect(0, 0, w, h))

	// 背景渐变:浅蓝到浅青
	for y := 0; y < h; y++ {
		t := float64(y) / float64(h-1)
		row := color.RGBA{
			R: uint8(238 - 6*t), G: uint8(245 - 3*t), B: uint8(252 - 2*t), A: 255,
		}
		draw.Draw(img, image.Rect(0, y, w, y+1), &image.Uniform{row}, image.Point{}, draw.Src)
	}

	// 绘图区
	left, right, top, bottom := 96.0, float64(w-64), 72.0, float64(h-72)
	axis := color.RGBA{0x64, 0x74, 0x8b, 0xff}
	drawLine(img, left, top, left, bottom, axis, 2)
	drawLine(img, left, bottom, right, bottom, axis, 2)

	// 网格 + 刻度
	grid := color.RGBA{0xcb, 0xd5, 0xe1, 0xff}
	for i := 0; i <= 5; i++ {
		x := left + (right-left)*float64(i)/5
		drawLine(img, x, top, x, bottom, grid, 1)
		y := top + (bottom-top)*float64(i)/5
		drawLine(img, left, y, right, y, grid, 1)
	}

	// 两条指标曲线(示意 FID,越低越好)
	drawSmoothCurve(img, left, right, top, bottom, []float64{0.92, 0.62, 0.44, 0.35, 0.31, 0.30}, color.RGBA{0x25, 0x63, 0xeb, 0xff}, 4)
	drawSmoothCurve(img, left, right, top, bottom, []float64{0.95, 0.78, 0.66, 0.60, 0.58, 0.57}, color.RGBA{0xf9, 0x73, 0x16, 0xff}, 4)

	// 数据点
	points := []float64{0.92, 0.62, 0.44, 0.35, 0.31, 0.30}
	for i, v := range points {
		x := left + (right-left)*float64(i)/5
		y := bottom - (bottom-top)*((1-v)*0.75+0.1)
		drawCircle(img, int(x), int(y), 7, color.RGBA{0x25, 0x63, 0xeb, 0xff})
	}

	// 图例色块(文字说明见图片标题/帖子正文)
	drawRect(img, 120, 24, 140, 48, color.RGBA{0x25, 0x63, 0xeb, 0xff})
	drawRect(img, 176, 24, 196, 48, color.RGBA{0xf9, 0x73, 0x16, 0xff})

	return writePNG(path, img)
}

func writeGridImage(path string) error {
	const cell, cols, rows = 54, 8, 5
	const pad = 20
	w, h := cols*cell+pad*2, rows*cell+pad*2
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), &image.Uniform{color.RGBA{0xf8, 0xfa, 0xfc, 0xff}}, image.Point{}, draw.Src)

	palette := []color.RGBA{
		{0xef, 0x44, 0x44, 0xff}, {0xf9, 0x73, 0x16, 0xff}, {0xf5, 0x9e, 0x0b, 0xff},
		{0x10, 0xb9, 0x81, 0xff}, {0x06, 0xb6, 0xd4, 0xff}, {0x3b, 0x82, 0xf6, 0xff},
		{0x8b, 0x5c, 0xf6, 0xff}, {0xec, 0x48, 0x99, 0xff}, {0x84, 0xcc, 0x16, 0xff},
		{0x14, 0xb8, 0xa6, 0xff}, {0x63, 0x66, 0xf1, 0xff}, {0xd9, 0x46, 0xef, 0xff},
	}
	border := color.RGBA{0xe2, 0xe8, 0xf0, 0xff}
	for rIdx := 0; rIdx < rows; rIdx++ {
		for cIdx := 0; cIdx < cols; cIdx++ {
			x0 := pad + cIdx*cell
			y0 := pad + rIdx*cell
			base := palette[(rIdx*cols+cIdx)%len(palette)]
			for y := y0 + 1; y < y0+cell-1; y++ {
				for x := x0 + 1; x < x0+cell-1; x++ {
					// 轻微对角渐变,让色块更像真实图像缩略图
					shade := float64((x-x0)+(y-y0)) / float64(2*(cell-2))
					img.Set(x, y, color.RGBA{
						R: uint8(clampF(float64(base.R)*(0.75+0.35*shade), 0, 255)),
						G: uint8(clampF(float64(base.G)*(0.75+0.35*shade), 0, 255)),
						B: uint8(clampF(float64(base.B)*(0.75+0.35*shade), 0, 255)),
						A: 255,
					})
				}
			}
			drawRectOutline(img, x0, y0, x0+cell-1, y0+cell-1, border)
		}
	}
	return writePNG(path, img)
}

func writePNG(path string, img image.Image) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

func drawLine(img *image.RGBA, x0, y0, x1, y1 float64, c color.RGBA, width int) {
	steps := int(math.Max(math.Abs(x1-x0), math.Abs(y1-y0))) * 2
	if steps < 1 {
		steps = 1
	}
	for i := 0; i <= steps; i++ {
		t := float64(i) / float64(steps)
		x := int(math.Round(x0 + (x1-x0)*t))
		y := int(math.Round(y0 + (y1-y0)*t))
		fillBox(img, x-width/2, y-width/2, width, c)
	}
}

func drawSmoothCurve(img *image.RGBA, left, right, top, bottom float64, vals []float64, c color.RGBA, width int) {
	if len(vals) < 2 {
		return
	}
	step := (right - left) / float64(len(vals)-1)
	for i := 0; i < len(vals)-1; i++ {
		x0 := left + step*float64(i)
		x1 := left + step*float64(i+1)
		y0 := bottom - (bottom-top)*((1-vals[i])*0.75+0.1)
		y1 := bottom - (bottom-top)*((1-vals[i+1])*0.75+0.1)
		drawLine(img, x0, y0, x1, y1, c, width)
	}
}

func drawCircle(img *image.RGBA, cx, cy, r int, c color.RGBA) {
	for y := -r; y <= r; y++ {
		for x := -r; x <= r; x++ {
			if x*x+y*y <= r*r {
				fillBox(img, cx+x, cy+y, 1, c)
			}
		}
	}
}

func drawRect(img *image.RGBA, x0, y0, x1, y1 int, c color.RGBA) {
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			fillBox(img, x, y, 1, c)
		}
	}
}

func drawRectOutline(img *image.RGBA, x0, y0, x1, y1 int, c color.RGBA) {
	drawLine(img, float64(x0), float64(y0), float64(x1), float64(y0), c, 1)
	drawLine(img, float64(x0), float64(y1), float64(x1), float64(y1), c, 1)
	drawLine(img, float64(x0), float64(y0), float64(x0), float64(y1), c, 1)
	drawLine(img, float64(x1), float64(y0), float64(x1), float64(y1), c, 1)
}

func fillBox(img *image.RGBA, x, y, size int, c color.RGBA) {
	for dy := 0; dy < size; dy++ {
		for dx := 0; dx < size; dx++ {
			img.Set(x+dx, y+dy, c)
		}
	}
}

func clampF(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
