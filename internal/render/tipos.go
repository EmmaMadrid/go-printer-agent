// Package render traduce los contratos JSON que manda la app a bytes ESC/POS.
//
// Hay cuatro plantillas térmicas, las mismas del agente JS:
//
//	Notas    → nota de venta cobrada (contrato TicketData del ERP)
//	Comanda  → orden de cocina, sin precios y con texto grande
//	Kiosko   → ticket de orden de autoservicio, sin pago
//	Bloques  → ticket genérico descrito por la app, agnóstico del dominio
package render

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/EmmaMadrid/go-printer-agent/internal/config"
	"github.com/EmmaMadrid/go-printer-agent/internal/escpos"
)

// ── Contratos compartidos ──────────────────────────────────────────────────

// Empresa es el encabezado del ticket. `LogoURL` acepta un data URL base64.
type Empresa struct {
	Nombre    string `json:"nombre"`
	Direccion string `json:"direccion"`
	RFC       string `json:"rfc"`
	Tel       string `json:"tel"`
	LogoURL   string `json:"logoUrl"`
}

// Linea es una partida del ticket.
type Linea struct {
	Cantidad    float64 `json:"cantidad"`
	Descripcion string  `json:"descripcion"`
	Importe     float64 `json:"importe"`
}

// Pago es una forma de pago aplicada a la cuenta.
type Pago struct {
	Metodo string  `json:"metodo"`
	Monto  float64 `json:"monto"`
}

// Opciones son los interruptores por trabajo. Se usan punteros porque el
// significado de "ausente" no es el mismo que el de "false": un ticket sin
// `cortar` sí corta, y uno sin `abrirCajon` no abre el cajón.
type Opciones struct {
	Cortar     *bool `json:"cortar"`
	AbrirCajon *bool `json:"abrirCajon"`
	AnchoMm    int   `json:"anchoMm"`
}

func (o Opciones) cortar(n config.Notas) bool {
	return n.Cortar && (o.Cortar == nil || *o.Cortar)
}

func (o Opciones) abrirCajon(n config.Notas) bool {
	return n.AbrirCajon && o.AbrirCajon != nil && *o.AbrirCajon
}

// ── Contratos por plantilla ────────────────────────────────────────────────

// TicketData es la nota de venta cobrada que manda el POS.
type TicketData struct {
	Empresa          Empresa  `json:"empresa"`
	Mesa             string   `json:"mesa"`
	Personas         float64  `json:"personas"`
	Mesero           string   `json:"mesero"`
	Cliente          string   `json:"cliente"`
	Orden            string   `json:"orden"`
	Folio            string   `json:"folio"`
	Tipo             string   `json:"tipo"`
	FechaApertura    string   `json:"fechaApertura"`
	FechaCierre      string   `json:"fechaCierre"`
	Cajero           string   `json:"cajero"`
	Lineas           []Linea  `json:"lineas"`
	NumArticulos     float64  `json:"numArticulos"`
	Subtotal         float64  `json:"subtotal"`
	Descuento        float64  `json:"descuento"`
	IvaIncluido      float64  `json:"ivaIncluido"`
	Propina          float64  `json:"propina"`
	Total            float64  `json:"total"`
	Pagos            []Pago   `json:"pagos"`
	EfectivoRecibido float64  `json:"efectivoRecibido"`
	Cambio           float64  `json:"cambio"`
	Software         string   `json:"software"`
	Opciones         Opciones `json:"opciones"`
}

// ComandaModificador es un "sin cebolla" / "con extra queso" de una partida.
type ComandaModificador struct {
	Tipo        string `json:"tipo"` // quitar | agregar
	Descripcion string `json:"descripcion"`
}

// ComandaLinea es un platillo de la orden de cocina.
type ComandaLinea struct {
	Cantidad      float64              `json:"cantidad"`
	Nombre        string               `json:"nombre"`
	Modificadores []ComandaModificador `json:"modificadores"`
	Nota          string               `json:"nota"`
}

// ComandaData es la orden que sale a cocina en cada ronda.
type ComandaData struct {
	Empresa      Empresa        `json:"empresa"`
	Folio        string         `json:"folio"`
	Orden        string         `json:"orden"`
	TipoVenta    string         `json:"tipoVenta"`
	Mesa         string         `json:"mesa"`
	Cliente      string         `json:"cliente"`
	EnviadoPor   string         `json:"enviadoPor"`
	Ronda        float64        `json:"ronda"`
	Hora         string         `json:"hora"`
	Lineas       []ComandaLinea `json:"lineas"`
	NumArticulos float64        `json:"numArticulos"`
	NotaGeneral  string         `json:"notaGeneral"`
	Opciones     Opciones       `json:"opciones"`
}

// KioskoTicket es el comprobante de orden del autoservicio: sin pago, con el
// número de orden en grande y la indicación de pasar a caja.
type KioskoTicket struct {
	Empresa           Empresa  `json:"empresa"`
	Folio             string   `json:"folio"`
	NumeroOrden       string   `json:"numeroOrden"`
	Fecha             string   `json:"fecha"`
	Lineas            []Linea  `json:"lineas"`
	NumArticulos      float64  `json:"numArticulos"`
	Total             float64  `json:"total"`
	TiempoEstimadoMin float64  `json:"tiempoEstimadoMin"`
	AnchoMm           int      `json:"anchoMm"`
	Opciones          Opciones `json:"opciones"`
}

// Bloque es un elemento del ticket genérico. La app describe el ticket y el
// agente no sabe nada del dominio.
type Bloque struct {
	T      string      `json:"t"`
	TipoAl string      `json:"tipo"` // alias de `t`
	V      any         `json:"v"`
	Valor  any         `json:"valor"`
	Data   string      `json:"data"`
	L      any         `json:"l"`
	R      any         `json:"r"`
	Align  string      `json:"align"`
	Bold   bool        `json:"bold"`
	Grande bool        `json:"grande"`
	Size   float64     `json:"size"`
	Ancho  int         `json:"ancho"`
	N      int         `json:"n"`
	Items  []BloqueCol `json:"items"`
	C      []BloqueCol `json:"c"` // alias de `items`
}

// BloqueCol es una columna dentro de un bloque de tipo `cols`.
type BloqueCol struct {
	V     any      `json:"v"`
	W     *float64 `json:"w"`    // ancho relativo 0–1
	Cols  int      `json:"cols"` // columnas exactas (gana sobre W)
	Align string   `json:"align"`
	Bold  bool     `json:"bold"`
}

// BloquesDoc es el cuerpo del ticket genérico.
type BloquesDoc struct {
	Bloques       []Bloque `json:"bloques"`
	Ancho         int      `json:"ancho"`
	Type          string   `json:"type"`
	CharacterSet  string   `json:"characterSet"`
	LogoAnchoDots int      `json:"logoAnchoDots"`
	Opciones      Opciones `json:"opciones"`
}

// ── Utilidades de formato ──────────────────────────────────────────────────

// mon formatea un importe como lo hacía el agente JS: `$` y dos decimales.
func mon(n float64) string {
	return "$" + strconv.FormatFloat(math.Round(n*100)/100, 'f', 2, 64)
}

// num imprime un número igual que `String(n)` en JavaScript: sin decimales
// cuando es entero, con los que traiga cuando no lo es.
func num(n float64) string {
	return strconv.FormatFloat(n, 'f', -1, 64)
}

// fhora convierte un ISO 8601 a la hora local de la caja. Devuelve "" si la
// fecha no se puede leer, para que el campo simplemente no se imprima.
func fhora(iso string, conSegundos bool) string {
	if iso == "" {
		return ""
	}
	t, err := time.Parse(time.RFC3339, iso)
	if err != nil {
		if t, err = time.Parse("2006-01-02T15:04:05.000Z0700", iso); err != nil {
			return ""
		}
	}
	t = t.Local()
	if conSegundos {
		return t.Format("02/01/2006 15:04:05")
	}
	return t.Format("02/01/06 15:04")
}

// texto convierte a cadena un valor de JSON que puede venir como texto, número
// o booleano (los bloques genéricos no tipan sus valores).
func texto(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case float64:
		return num(x)
	case bool:
		return strconv.FormatBool(x)
	case json.Number:
		return x.String()
	default:
		return fmt.Sprint(x)
	}
}

// primero devuelve el primer valor no vacío (equivalente al `??` encadenado).
func primero(vs ...any) any {
	for _, v := range vs {
		if v != nil {
			if s, ok := v.(string); ok && s == "" {
				continue
			}
			return v
		}
	}
	return nil
}

// kv imprime "etiqueta … valor" y omite el renglón si el valor viene vacío.
func kv(w *escpos.Writer, etiqueta, valor string) {
	if valor == "" {
		return
	}
	w.LeftRight(etiqueta, valor)
}

// nuevoWriter crea el Writer con el dialecto, ancho y juego de caracteres de la
// impresora que toque en este trabajo.
func nuevoWriter(n config.Notas) *escpos.Writer {
	return escpos.New(n.Type, n.Width, n.CharacterSet)
}

// logo devuelve los bytes del logotipo: gana el archivo local configurado en la
// estación y, si no hay, el data URL que mandó la app. Devuelve nil si no hay.
func logo(n config.Notas, e Empresa) []byte {
	if n.LogoPath != "" {
		if b, err := os.ReadFile(n.LogoPath); err == nil {
			return b
		}
	}
	return escpos.DecodificarDataURL(e.LogoURL)
}

// imprimirLogo agrega el logotipo centrado. Un logo ilegible no cancela el
// ticket: se registra el motivo y se sigue, que es lo que importa en una caja.
func imprimirLogo(w *escpos.Writer, n config.Notas, e Empresa) {
	datos := logo(n, e)
	if datos == nil {
		return
	}
	raster, err := escpos.Raster(datos, n.LogoAnchoDots)
	if err != nil {
		log.Printf("[logo] %v", err)
		return
	}
	w.Raw(raster)
	w.NewLine()
}

// columnasPartidas reparte el ancho del papel entre cantidad, descripción e
// importe, que es la tabla que comparten la nota de venta y el ticket de kiosko.
func columnasPartidas(ancho int) (cant, desc, importe int) {
	cant, importe = 4, 10
	desc = ancho - cant - importe
	if desc < 8 {
		desc = 8
	}
	return
}

// encabezadoEmpresa imprime logo, nombre y datos fiscales centrados.
func encabezadoEmpresa(w *escpos.Writer, n config.Notas, e Empresa, conRFC bool) {
	w.AlignCenter()
	imprimirLogo(w, n, e)
	w.Bold(true)
	w.SetTextSize(1, 1)
	nombre := e.Nombre
	if nombre == "" {
		nombre = "Mi empresa"
	}
	w.Println(nombre)
	w.SetTextNormal()
	w.Bold(false)
	if e.Direccion != "" {
		w.Println(e.Direccion)
	}
	if conRFC {
		var partes []string
		if e.RFC != "" {
			partes = append(partes, "RFC: "+e.RFC)
		}
		if e.Tel != "" {
			partes = append(partes, "Tel: "+e.Tel)
		}
		if len(partes) > 0 {
			w.Println(strings.Join(partes, "  ·  "))
		}
	} else if e.Tel != "" {
		w.Println("Tel: " + e.Tel)
	}
	w.DrawLine()
}
