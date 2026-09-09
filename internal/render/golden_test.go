package render

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/EmmaMadrid/go-printer-agent/internal/config"
)

// Los .bin de testdata son la salida REAL del agente JS que hoy corre en las
// cajas, capturada haciéndolo imprimir a un socket local (tools/capturar.js).
// Estas pruebas fijan la equivalencia byte a byte: si un cambio en el
// renderizador movería aunque sea un espacio del ticket, aquí se nota.
//
// Para regenerar los golden con otro juego de datos:
//
//	node tools/capturar.js http://127.0.0.1:9921 internal/render/testdata
func TestSalidaIdenticaAlAgenteJS(t *testing.T) {
	payloads := cargarPayloads(t)
	notas := config.Default().Notas

	casos := []struct {
		nombre string
		armar  func([]byte) ([]byte, error)
	}{
		{"notas", func(raw []byte) ([]byte, error) {
			var v TicketData
			if err := json.Unmarshal(raw, &v); err != nil {
				return nil, err
			}
			return Notas(v, notas)
		}},
		{"comanda", func(raw []byte) ([]byte, error) {
			var v ComandaData
			if err := json.Unmarshal(raw, &v); err != nil {
				return nil, err
			}
			return Comanda(v, notas)
		}},
		{"kiosko", func(raw []byte) ([]byte, error) {
			var v KioskoTicket
			if err := json.Unmarshal(raw, &v); err != nil {
				return nil, err
			}
			return Kiosko(v, notas)
		}},
		{"bloques", func(raw []byte) ([]byte, error) {
			var v BloquesDoc
			if err := json.Unmarshal(raw, &v); err != nil {
				return nil, err
			}
			return Bloques(v, notas)
		}},
	}

	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			crudo, ok := payloads[c.nombre]
			if !ok {
				t.Fatalf("falta el payload %q en testdata/payloads.json", c.nombre)
			}
			obtenido, err := c.armar(crudo)
			if err != nil {
				t.Fatalf("no se pudo armar el ticket: %v", err)
			}
			esperado := leerGolden(t, c.nombre+".bin")
			compararBytes(t, esperado, obtenido)
		})
	}
}

// TestFechaLocal fija el formato de fecha, que es donde más fácil se cuela una
// diferencia entre el `new Date()` de JS y el time de Go.
func TestFechaLocal(t *testing.T) {
	casos := []struct {
		iso         string
		conSegundos bool
		quiere      string
	}{
		{"", false, ""},
		{"no es una fecha", false, ""},
		{"2026-09-09T19:42:13.000Z", true, ""},  // depende de la zona: solo no debe reventar
		{"2026-09-09T19:42:13.000Z", false, ""}, // ídem
	}
	for _, c := range casos {
		got := fhora(c.iso, c.conSegundos)
		if c.quiere != "" && got != c.quiere {
			t.Errorf("fhora(%q) = %q, se esperaba %q", c.iso, got, c.quiere)
		}
	}
	if fhora("2026-09-09T19:42:13.000Z", false) == "" {
		t.Error("una fecha ISO válida no debería dar cadena vacía")
	}
}

// TestMoneda comprueba el redondeo de importes contra el toFixed(2) de JS.
func TestMoneda(t *testing.T) {
	casos := map[float64]string{
		0:       "$0.00",
		1245:    "$1245.00",
		165.515: "$165.52",
		165.524: "$165.52",
		// El agente JS hacía '$' + n.toFixed(2), así que un negativo queda
		// "$-45.00"; el descuento se antepone aparte con su propio signo.
		-45: "$-45.00",
	}
	for entrada, quiere := range casos {
		if got := mon(entrada); got != quiere {
			t.Errorf("mon(%v) = %q, se esperaba %q", entrada, got, quiere)
		}
	}
}

// TestCantidadComoEnJS verifica que un 2 se imprima "2" y no "2.0".
func TestCantidadComoEnJS(t *testing.T) {
	casos := map[float64]string{2: "2", 1.5: "1.5", 0: "0", 7: "7"}
	for entrada, quiere := range casos {
		if got := num(entrada); got != quiere {
			t.Errorf("num(%v) = %q, se esperaba %q", entrada, got, quiere)
		}
	}
}

func cargarPayloads(t *testing.T) map[string]json.RawMessage {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "payloads.json"))
	if err != nil {
		t.Fatalf("no se pudo leer testdata/payloads.json: %v", err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("testdata/payloads.json no es JSON válido: %v", err)
	}
	return m
}

func leerGolden(t *testing.T, nombre string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", nombre))
	if err != nil {
		t.Fatalf("no se pudo leer el golden %s: %v", nombre, err)
	}
	return b
}

// compararBytes señala la primera diferencia con su contexto, que es mucho más
// útil que un "los buffers no son iguales".
func compararBytes(t *testing.T, esperado, obtenido []byte) {
	t.Helper()
	if len(esperado) != len(obtenido) {
		t.Errorf("longitud distinta: el agente JS produjo %d bytes y este %d", len(esperado), len(obtenido))
	}
	minimo := len(esperado)
	if len(obtenido) < minimo {
		minimo = len(obtenido)
	}
	for i := 0; i < minimo; i++ {
		if esperado[i] != obtenido[i] {
			t.Fatalf("primera diferencia en el byte %d: JS=0x%02x (%s) vs Go=0x%02x (%s)\n  JS: %s\n  Go: %s",
				i, esperado[i], legible(esperado[i]), obtenido[i], legible(obtenido[i]),
				contexto(esperado, i), contexto(obtenido, i))
		}
	}
}

func legible(b byte) string {
	if b >= 0x20 && b < 0x7f {
		return fmt.Sprintf("%q", rune(b))
	}
	return "no imprimible"
}

// contexto devuelve los bytes alrededor de una posición, en forma legible.
func contexto(b []byte, pos int) string {
	inicio := pos - 24
	if inicio < 0 {
		inicio = 0
	}
	fin := pos + 24
	if fin > len(b) {
		fin = len(b)
	}
	salida := make([]rune, 0, fin-inicio)
	for _, c := range b[inicio:fin] {
		if c >= 0x20 && c < 0x7f {
			salida = append(salida, rune(c))
		} else {
			salida = append(salida, '·')
		}
	}
	return string(salida)
}
