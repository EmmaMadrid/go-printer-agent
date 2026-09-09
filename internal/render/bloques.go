package render

import (
	"log"
	"math"

	"github.com/EmmaMadrid/go-printer-agent/internal/config"
	"github.com/EmmaMadrid/go-printer-agent/internal/escpos"
)

// Bloques arma un ticket genérico a partir de la lista de bloques que describe
// la app. El agente no sabe de restaurantes ni de tiendas: solo dibuja texto,
// pares etiqueta/valor, columnas, líneas, saltos e imágenes.
func Bloques(doc BloquesDoc, n config.Notas) ([]byte, error) {
	// El cuerpo puede pisar el ancho y el dialecto de la impresora configurada.
	efectiva := n
	if doc.Ancho > 0 {
		efectiva.Width = doc.Ancho
	}
	if doc.Type != "" {
		efectiva.Type = doc.Type
	}
	if doc.CharacterSet != "" {
		efectiva.CharacterSet = doc.CharacterSet
	}
	if efectiva.Width <= 0 {
		efectiva.Width = 48
	}

	w := nuevoWriter(efectiva)
	ancho := w.Ancho()

	for _, b := range doc.Bloques {
		tipo := b.T
		if tipo == "" {
			tipo = b.TipoAl
		}

		switch tipo {
		case "img", "imagen", "logo":
			dato := b.Data
			if dato == "" {
				dato = texto(b.V)
			}
			datos := escpos.DecodificarDataURL(dato)
			if datos == nil {
				break
			}
			w.AlignCenter()
			anchoLogo := primeroPositivo(b.Ancho, doc.LogoAnchoDots, efectiva.LogoAnchoDots)
			raster, err := escpos.Raster(datos, anchoLogo)
			if err != nil {
				log.Printf("[logo] %v", err) // una imagen mala no cancela el ticket
				break
			}
			w.Raw(raster)
			w.NewLine()

		case "txt", "texto":
			w.Align(b.Align)
			if b.Bold {
				w.Bold(true)
			}
			if b.Grande || b.Size >= 2 {
				w.SetTextSize(1, 1)
			}
			w.Println(texto(primero(b.V, b.Valor)))
			w.SetTextNormal()
			w.Bold(false)
			w.AlignLeft()

		case "kv":
			if b.Bold {
				w.Bold(true)
			}
			w.LeftRight(texto(b.L), texto(b.R))
			w.Bold(false)

		case "cols", "tabla":
			items := b.Items
			if len(items) == 0 {
				items = b.C
			}
			if len(items) == 0 {
				break
			}
			anchos := colsEnteros(items, ancho)
			celdas := make([]escpos.Celda, len(items))
			for i, c := range items {
				align := c.Align
				if align == "" {
					align = "LEFT"
				}
				celdas[i] = escpos.Celda{Texto: texto(c.V), Align: align, Cols: anchos[i], Bold: c.Bold}
			}
			w.TableCustom(celdas)

		case "hr", "linea":
			w.DrawLine()

		case "feed", "salto":
			veces := b.N
			if veces <= 0 {
				veces = 1
			}
			for i := 0; i < veces; i++ {
				w.NewLine()
			}
		}
	}

	if doc.Opciones.abrirCajon(efectiva) {
		w.OpenCashDrawer()
	}
	if doc.Opciones.cortar(efectiva) {
		w.Cut()
	}
	return w.Bytes(), nil
}

// colsEnteros reparte columnas enteras que sumen EXACTO el ancho del papel. Si
// cada celda ya trae sus columnas se respetan; si no, se reparte por el peso
// relativo `w` y el sobrante se le da a la última para evitar desbordes de un
// carácter.
func colsEnteros(items []BloqueCol, ancho int) []int {
	todas := true
	for _, c := range items {
		if c.Cols <= 0 {
			todas = false
			break
		}
	}
	if todas {
		out := make([]int, len(items))
		for i, c := range items {
			out[i] = c.Cols
		}
		return out
	}

	n := len(items)
	pesos := make([]float64, n)
	suma := 0.0
	for i, c := range items {
		if c.W != nil {
			pesos[i] = *c.W
		} else {
			pesos[i] = 1 / float64(n)
		}
		suma += pesos[i]
	}
	if suma == 0 {
		suma = 1
	}

	cols := make([]int, n)
	total := 0
	for i, p := range pesos {
		cols[i] = int(math.Floor(p / suma * float64(ancho)))
		total += cols[i]
	}
	cols[n-1] += ancho - total
	return cols
}

// primeroPositivo devuelve el primer valor mayor que cero.
func primeroPositivo(vs ...int) int {
	for _, v := range vs {
		if v > 0 {
			return v
		}
	}
	return 0
}
