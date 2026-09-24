package captcha

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"math/big"
)

const (
	imgW    = 310
	imgH    = 155
	pieceW  = 47
	pieceH  = 47
	offsetY = 55
)

// puzzle 生成底图和滑块。缺口的 x 要和校验时的坐标对上。
func puzzle(gapX int) (original, jigsaw string, err error) {
	bg := image.NewRGBA(image.Rect(0, 0, imgW, imgH))
	for y := 0; y < imgH; y++ {
		for x := 0; x < imgW; x++ {
			bg.Set(x, y, color.RGBA{R: uint8(40 + x%180), G: uint8(80 + y%120), B: uint8(160 - x%80), A: 255})
		}
	}
	piece := image.NewRGBA(image.Rect(0, 0, pieceW, pieceH))
	for y := 0; y < pieceH; y++ {
		for x := 0; x < pieceW; x++ {
			c := bg.At(gapX+x, offsetY+y)
			piece.Set(x, y, c)
			bg.Set(gapX+x, offsetY+y, color.RGBA{A: 180})
		}
	}
	original, err = pngBase64(bg)
	if err != nil {
		return "", "", err
	}
	jigsaw, err = pngBase64(piece)
	return original, jigsaw, err
}

func pngBase64(img image.Image) (string, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}

func randomGap() int {
	n, err := rand.Int(rand.Reader, big.NewInt(imgW-pieceW-40))
	if err != nil {
		return 120
	}
	return int(n.Int64()) + 40
}

func randomKey() string {
	const letters = "abcdefghijklmnopqrstuvwxyz0123456789"
	buf := make([]byte, 16)
	raw := make([]byte, 16)
	_, _ = rand.Read(raw)
	for i := range buf {
		buf[i] = letters[int(raw[i])%len(letters)]
	}
	return string(buf)
}
