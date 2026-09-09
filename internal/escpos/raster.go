package escpos

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"math"
	"regexp"

	// Decodificadores registrados por efecto secundario: el logo puede venir en
	// cualquier formato que el usuario haya subido desde el POS.
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"
)

// AnchoLogoPorDefecto son los puntos de ancho de un logo en papel de 80 mm.
const AnchoLogoPorDefecto = 384

var reDataURL = regexp.MustCompile(`^data:image/[a-zA-Z.+-]+;base64,(.+)$`)

// DecodificarDataURL extrae los bytes de una imagen embebida como data URL.
// Devuelve nil si la cadena no es un data URL de imagen.
func DecodificarDataURL(s string) []byte {
	m := reDataURL.FindStringSubmatch(s)
	if m == nil {
		return nil
	}
	b, err := base64.StdEncoding.DecodeString(m[1])
	if err != nil {
		return nil
	}
	return b
}

// Raster convierte CUALQUIER imagen en un bloque ESC/POS `GS v 0`: la reduce al
// ancho de la impresora, la pasa a gris aplanando la transparencia sobre BLANCO
// (así un PNG sin fondo no sale con caja negra) y la difumina a 1 bit con
// Floyd–Steinberg, que es lo que hace legible un logo a color en una térmica.
func Raster(datos []byte, anchoDots int) ([]byte, error) {
	img, _, err := image.Decode(bytes.NewReader(datos))
	if err != nil {
		return nil, fmt.Errorf("no se pudo leer la imagen: %w", err)
	}

	origen := img.Bounds()
	if origen.Dx() <= 0 || origen.Dy() <= 0 {
		return nil, fmt.Errorf("la imagen está vacía")
	}

	if anchoDots <= 0 {
		anchoDots = AnchoLogoPorDefecto
	}
	ancho := anchoDots
	if origen.Dx() < ancho {
		ancho = origen.Dx()
	}
	ancho -= ancho % 8 // el raster se empaqueta en bytes de 8 columnas
	if ancho < 8 {
		ancho = 8
	}
	alto := int(math.Round(float64(origen.Dy()) * float64(ancho) / float64(origen.Dx())))
	if alto < 1 {
		alto = 1
	}

	gris := reducirAGris(img, ancho, alto)
	difuminar(gris, ancho, alto)

	bytesPorFila := ancho / 8
	raster := make([]byte, bytesPorFila*alto)
	for y := 0; y < alto; y++ {
		for x := 0; x < ancho; x++ {
			if gris[y*ancho+x] < 128 {
				raster[y*bytesPorFila+(x>>3)] |= 0x80 >> uint(x&7)
			}
		}
	}

	cab := []byte{
		0x1d, 0x76, 0x30, 0x00,
		byte(bytesPorFila & 0xff), byte(bytesPorFila >> 8 & 0xff),
		byte(alto & 0xff), byte(alto >> 8 & 0xff),
	}
	return append(cab, raster...), nil
}

// reducirAGris aplana la transparencia sobre blanco, calcula la luminancia real
// y promedia por cajas hasta el tamaño destino (promediar antes de difuminar
// conserva mucho más detalle que muestrear un solo píxel).
func reducirAGris(img image.Image, ancho, alto int) []float64 {
	b := img.Bounds()
	origAncho, origAlto := b.Dx(), b.Dy()

	full := make([]float64, origAncho*origAlto)
	for y := 0; y < origAlto; y++ {
		for x := 0; x < origAncho; x++ {
			c := color.NRGBAModel.Convert(img.At(b.Min.X+x, b.Min.Y+y)).(color.NRGBA)
			a := float64(c.A) / 255
			lum := 0.299*float64(c.R) + 0.587*float64(c.G) + 0.114*float64(c.B)
			full[y*origAncho+x] = a*lum + (1-a)*255
		}
	}

	if ancho == origAncho && alto == origAlto {
		return full
	}

	escalaX := float64(origAncho) / float64(ancho)
	escalaY := float64(origAlto) / float64(alto)
	salida := make([]float64, ancho*alto)
	for y := 0; y < alto; y++ {
		y0 := int(float64(y) * escalaY)
		y1 := int(float64(y+1) * escalaY)
		if y1 <= y0 {
			y1 = y0 + 1
		}
		if y1 > origAlto {
			y1 = origAlto
		}
		for x := 0; x < ancho; x++ {
			x0 := int(float64(x) * escalaX)
			x1 := int(float64(x+1) * escalaX)
			if x1 <= x0 {
				x1 = x0 + 1
			}
			if x1 > origAncho {
				x1 = origAncho
			}
			suma, n := 0.0, 0
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					suma += full[sy*origAncho+sx]
					n++
				}
			}
			salida[y*ancho+x] = suma / float64(n)
		}
	}
	return salida
}

// difuminar aplica Floyd–Steinberg in situ: deja cada píxel en 0 o 255 y reparte
// el error a los vecinos, que es lo que simula grises en una impresora de 1 bit.
func difuminar(g []float64, ancho, alto int) {
	for y := 0; y < alto; y++ {
		for x := 0; x < ancho; x++ {
			i := y*ancho + x
			nuevo := 255.0
			if g[i] < 128 {
				nuevo = 0
			}
			err := g[i] - nuevo
			g[i] = nuevo
			if x+1 < ancho {
				g[i+1] += err * 7 / 16
			}
			if y+1 < alto {
				if x > 0 {
					g[i+ancho-1] += err * 3 / 16
				}
				g[i+ancho] += err * 5 / 16
				if x+1 < ancho {
					g[i+ancho+1] += err * 1 / 16
				}
			}
		}
	}
}
