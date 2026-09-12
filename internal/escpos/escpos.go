// Package escpos arma el flujo de bytes ESC/POS de un ticket térmico.
//
// Es un port fiel del renderizador que usaba el agente JS (node-thermal-printer):
// mismas secuencias de control, mismo doblado de líneas y mismo reparto de
// columnas, para que un ticket impreso por este agente salga igual que el del
// agente anterior.
package escpos

import (
	"bytes"
	"math"
	"strings"

	"golang.org/x/text/encoding"
)

// dialecto agrupa las secuencias de control que cambian entre fabricantes.
type dialecto struct {
	lf          []byte // avance de línea
	vt          []byte // tabulación vertical (alimenta papel antes del corte)
	init        []byte // reset del hardware
	cutFull     []byte
	alignLeft   []byte
	alignCenter []byte
	alignRight  []byte
	boldOn      []byte
	boldOff     []byte
	textNormal  []byte
	text2Height []byte
	kick2       []byte
	kick5       []byte
	kick        []byte // solo Star: pulso propio del cajón
	codePagePre []byte // prefijo del comando de página de códigos
	textSize    func(alto, ancho int) []byte
}

var epson = dialecto{
	lf:          []byte{0x0a},
	vt:          []byte{0x1b, 0x64, 0x04},
	init:        []byte{0x1b, 0x40},
	cutFull:     []byte{0x1d, 0x56, 0x00},
	alignLeft:   []byte{0x1b, 0x61, 0x00},
	alignCenter: []byte{0x1b, 0x61, 0x01},
	alignRight:  []byte{0x1b, 0x61, 0x02},
	boldOn:      []byte{0x1b, 0x45, 0x01},
	boldOff:     []byte{0x1b, 0x45, 0x00},
	textNormal:  []byte{0x1b, 0x21, 0x00},
	text2Height: []byte{0x1b, 0x21, 0x10},
	kick2:       []byte{0x1b, 0x70, 0x00},
	kick5:       []byte{0x1b, 0x70, 0x01},
	codePagePre: []byte{0x1b, 0x74},
	// GS ! n. El agente JS componía el byte como 0x<alto><ancho> (los nibbles
	// al revés del manual); como todas las llamadas usan valores simétricos
	// —(1,1) y (2,2)— se replica tal cual para no cambiar la salida.
	textSize: func(alto, ancho int) []byte {
		return []byte{0x1d, 0x21, byte(alto<<4 | ancho&0x0f)}
	},
}

var star = dialecto{
	lf:          []byte{0x0a},
	vt:          []byte{0x0b},
	init:        []byte{0x1b, 0x40},
	cutFull:     []byte{0x1b, 0x64, 0x02},
	alignLeft:   []byte{0x1b, 0x1d, 0x61, 0x00},
	alignCenter: []byte{0x1b, 0x1d, 0x61, 0x01},
	alignRight:  []byte{0x1b, 0x1d, 0x61, 0x02},
	boldOn:      []byte{0x1b, 0x45},
	boldOff:     []byte{0x1b, 0x46},
	textNormal:  []byte{0x1b, 0x69, 0x00, 0x00},
	text2Height: []byte{0x1b, 0x69, 0x01, 0x00},
	kick2:       []byte{0x1b, 0x70, 0x00},
	kick5:       []byte{0x1b, 0x70, 0x01},
	kick:        []byte{0x1b, 0x07, 0x0b, 0x37, 0x07},
	codePagePre: []byte{0x1b, 0x74},
	textSize: func(alto, ancho int) []byte {
		return []byte{0x1b, 0x69, byte(alto), byte(ancho)}
	},
}

func resolverDialecto(tipo string) dialecto {
	if strings.EqualFold(tipo, "star") {
		return star
	}
	return epson // epson | tanca | daruma y cualquier valor desconocido
}

// Writer acumula los bytes de un ticket. No es seguro para uso concurrente:
// cada trabajo de impresión crea el suyo.
type Writer struct {
	buf   bytes.Buffer
	d     dialecto
	enc   *encoding.Encoder
	ancho int // columnas de la impresora
}

// New crea un Writer y siembra el buffer con la página de códigos, igual que
// hacía el constructor de node-thermal-printer.
func New(tipo string, ancho int, charset string) *Writer {
	if ancho <= 0 {
		ancho = 48
	}
	d := resolverDialecto(tipo)
	pc := resolverCodigo(charset)
	w := &Writer{
		d:     d,
		ancho: ancho,
		// Codificador estricto a propósito: un carácter fuera de la página
		// debe fallar para que `texto` decida cómo sustituirlo.
		enc: pc.enc.NewEncoder(),
	}
	w.buf.Write(d.codePagePre)
	w.buf.WriteByte(pc.n)
	return w
}

// Ancho son las columnas de texto configuradas.
func (w *Writer) Ancho() int { return w.ancho }

// Bytes devuelve el flujo ESC/POS completo.
func (w *Writer) Bytes() []byte { return w.buf.Bytes() }

// Raw agrega bytes ya listos (por ejemplo el raster de un logo).
func (w *Writer) Raw(b []byte) { w.buf.Write(b) }

// transliteracion cubre la puntuación tipográfica que suele colarse en nombres
// de platillos copiados de un menú o de un documento: no existe en las páginas
// de códigos de las térmicas, pero tiene un equivalente ASCII obvio.
var transliteracion = map[rune]string{
	'—': "-",   // — raya
	'–': "-",   // – semirraya
	'‒': "-",   // ‒ guion de cifra
	'‐': "-",   // ‐ guion
	'‘': "'",   // ‘
	'’': "'",   // ’
	'‚': "'",   // ‚
	'“': "\"",  // “
	'”': "\"",  // ”
	'„': "\"",  // „
	'…': "...", // …
	'•': "*",   // • viñeta
	' ': " ",   // espacio duro
	'™': "TM",  // ™
	'→': "->",  // →
	'←': "<-",  // ←
}

// texto escribe una cadena traducida a la página de códigos activa.
//
// Un carácter que no exista en esa página no rompe el ticket: si tiene un
// equivalente ASCII razonable se translitera, y si no, sale como "?" (que es lo
// que hacía el agente JS y lo que un cajero reconoce como "aquí había algo").
func (w *Writer) texto(s string) {
	if s == "" {
		return
	}
	// Camino rápido: casi todo texto del POS cabe completo en la página.
	if b, err := w.enc.Bytes([]byte(s)); err == nil {
		w.buf.Write(b)
		return
	}
	for _, r := range s {
		if r < 0x80 {
			w.buf.WriteByte(byte(r))
			continue
		}
		if b, err := w.enc.Bytes([]byte(string(r))); err == nil {
			w.buf.Write(b)
			continue
		}
		if t, ok := transliteracion[r]; ok {
			w.buf.WriteString(t)
			continue
		}
		w.buf.WriteByte('?')
	}
}

func (w *Writer) espacios(n int) {
	for i := 0; i < n; i++ {
		w.buf.WriteByte(' ')
	}
}

// ── Formato ────────────────────────────────────────────────────────────────

func (w *Writer) AlignLeft()   { w.buf.Write(w.d.alignLeft) }
func (w *Writer) AlignCenter() { w.buf.Write(w.d.alignCenter) }
func (w *Writer) AlignRight()  { w.buf.Write(w.d.alignRight) }

// Align acepta "center" / "right" y deja cualquier otro valor en izquierda.
func (w *Writer) Align(a string) {
	switch strings.ToLower(a) {
	case "center":
		w.AlignCenter()
	case "right":
		w.AlignRight()
	default:
		w.AlignLeft()
	}
}

// Bold enciende o apaga la negrita.
func (w *Writer) Bold(on bool) {
	if on {
		w.buf.Write(w.d.boldOn)
	} else {
		w.buf.Write(w.d.boldOff)
	}
}

// SetTextNormal vuelve al tamaño de texto base.
func (w *Writer) SetTextNormal() { w.buf.Write(w.d.textNormal) }

// SetTextDoubleHeight duplica solo el alto (platillos de la comanda).
func (w *Writer) SetTextDoubleHeight() { w.buf.Write(w.d.text2Height) }

// SetTextSize fija el multiplicador de alto y ancho (1 = doble, 2 = triple…).
func (w *Writer) SetTextSize(alto, ancho int) { w.buf.Write(w.d.textSize(alto, ancho)) }

// NewLine avanza un renglón.
func (w *Writer) NewLine() { w.buf.Write(w.d.lf) }

// ── Texto ──────────────────────────────────────────────────────────────────

// Print escribe texto doblándolo al ancho de la impresora, cortando en el
// último espacio disponible (mismo algoritmo `_fold` del agente JS).
func (w *Writer) Print(s string) {
	w.texto(strings.Join(doblar(s, w.ancho), "\n"))
}

// Println es Print más un salto de línea.
func (w *Writer) Println(s string) {
	w.Print(s)
	w.buf.WriteByte('\n')
}

// DrawLine dibuja una línea divisoria a lo ancho del papel.
func (w *Writer) DrawLine() {
	for i := 0; i < w.ancho; i++ {
		w.buf.WriteByte('-')
	}
	w.NewLine()
}

// LeftRight imprime una etiqueta a la izquierda y su valor a la derecha,
// rellenando con espacios. Si el par no cabe no se recorta (se desborda al
// renglón siguiente), tal como hacía el agente anterior.
func (w *Writer) LeftRight(izq, der string) {
	w.texto(izq)
	w.espacios(w.ancho - len([]rune(izq)) - len([]rune(der)))
	w.texto(der)
	w.NewLine()
}

// Celda es una columna de una fila de tabla.
type Celda struct {
	Texto string
	Align string // LEFT | CENTER | RIGHT
	Cols  int    // columnas exactas; 0 = reparto equitativo
	Bold  bool
}

// TableCustom imprime una fila repartida en columnas. Si un texto no cabe en su
// columna, el sobrante continúa en una segunda fila (recursivo), igual que el
// `tableCustom` de node-thermal-printer.
func (w *Writer) TableCustom(celdas []Celda) {
	if len(celdas) == 0 {
		return
	}
	segunda := make([]Celda, 0, len(celdas))
	haySegunda := false

	for _, c := range celdas {
		anchoCelda := float64(w.ancho) / float64(len(celdas))
		if c.Cols > 0 {
			anchoCelda = float64(c.Cols)
		}

		runas := []rune(c.Texto)
		var resto string
		if anchoCelda < float64(len(runas)) {
			corte := int(anchoCelda) - 1
			if corte < 0 {
				corte = 0
			}
			resto = string(runas[corte:])
			runas = runas[:corte]
		}
		txt := string(runas)
		largo := len(runas)

		if c.Bold {
			w.Bold(true)
		}
		switch strings.ToUpper(c.Align) {
		case "CENTER":
			// El agente JS calculaba los espacios sin redondear y restaba uno
			// al relleno derecho; se replica para no mover las columnas.
			s := (anchoCelda - float64(largo)) / 2
			w.espacios(int(math.Ceil(s)))
			w.texto(txt)
			w.espacios(int(math.Ceil(s)) - 1)
		case "RIGHT":
			w.espacios(int(anchoCelda) - largo)
			w.texto(txt)
		default:
			w.texto(txt)
			w.espacios(int(anchoCelda) - largo)
		}
		if c.Bold {
			w.Bold(false)
		}

		siguiente := c
		siguiente.Texto = resto
		if resto != "" {
			haySegunda = true
		}
		segunda = append(segunda, siguiente)
	}

	w.NewLine()
	if haySegunda {
		w.TableCustom(segunda)
	}
}

// ── Hardware ───────────────────────────────────────────────────────────────

// OpenCashDrawer manda el pulso al cajón de dinero.
func (w *Writer) OpenCashDrawer() {
	if len(w.d.kick) > 0 {
		w.buf.Write(w.d.kick)
		return
	}
	w.buf.Write(w.d.kick2)
	w.buf.Write(w.d.kick5)
}

// Cut alimenta papel, corta y reinicia la impresora (cierra el ticket).
func (w *Writer) Cut() {
	w.buf.Write(w.d.vt)
	w.buf.Write(w.d.vt)
	w.buf.Write(w.d.cutFull)
	w.buf.Write(w.d.init)
}

// ── Doblado de líneas ──────────────────────────────────────────────────────

// doblar parte un texto en líneas de a lo más `ancho` caracteres, cortando en
// el último espacio disponible cuando lo hay.
func doblar(texto string, ancho int) []string {
	if ancho <= 0 {
		return []string{texto}
	}
	var salida []string
	for _, linea := range strings.Split(texto, "\n") {
		actual := []rune(linea)
		for len(actual) > ancho {
			corte := ancho
			siguiente := ancho
			if idx := ultimoEspacio(actual[:ancho]); idx > 0 {
				corte = idx
				siguiente = idx + 1 // se come el espacio del corte
			}
			salida = append(salida, string(actual[:corte]))
			actual = actual[siguiente:]
		}
		salida = append(salida, string(actual))
	}
	return salida
}

// ultimoEspacio devuelve la posición del último carácter en blanco, o -1.
func ultimoEspacio(r []rune) int {
	for i := len(r) - 1; i >= 0; i-- {
		switch r[i] {
		case ' ', '\t', '\v', '\f', '\r':
			return i
		}
	}
	return -1
}
