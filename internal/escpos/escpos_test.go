package escpos

import (
	"bytes"
	"testing"
)

// cuerpo devuelve lo que se escribió después del comando de página de códigos
// (ESC t n) con el que arranca todo Writer.
func cuerpo(w *Writer) []byte {
	return w.Bytes()[3:]
}

func TestTextoEnCP858(t *testing.T) {
	w := New("epson", 42, "PC858_EURO")
	w.Print("Café ñandú €")
	got := cuerpo(w)
	// é=0x82, ñ=0xa4, ú=0xa3, €=0xd5 en CP858.
	want := []byte("Caf\x82 \xa4and\xa3 \xd5")
	if !bytes.Equal(got, want) {
		t.Fatalf("CP858: se obtuvo % x, se esperaba % x", got, want)
	}
}

func TestTransliteraPuntuacionTipografica(t *testing.T) {
	w := New("epson", 42, "PC858_EURO")
	w.Print("PRUEBA — Agente “Go” … ‘ok’ • fin")
	got := string(cuerpo(w))
	want := `PRUEBA - Agente "Go" ... 'ok' * fin`
	if got != want {
		t.Fatalf("transliteración: se obtuvo %q, se esperaba %q", got, want)
	}
}

func TestCaracterSinEquivalenteSaleComoInterrogacion(t *testing.T) {
	w := New("epson", 42, "PC858_EURO")
	w.Print("Sushi 寿司 y más")
	got := string(cuerpo(w))
	want := "Sushi ?? y m\xa0s" // á = 0xa0 en CP858
	if got != want {
		t.Fatalf("sin equivalente: se obtuvo %q, se esperaba %q", got, want)
	}
}

func TestDoblaPorPalabra(t *testing.T) {
	casos := []struct {
		texto string
		ancho int
		want  []string
	}{
		// Igual que el `_fold` del agente JS: se corta en el último espacio
		// DENTRO de la ventana, así que una palabra que termina justo en la
		// columna límite baja al siguiente renglón.
		{"Aguachile de camarón estilo Sinaloa con extra limón", 20,
			[]string{"Aguachile de", "camarón estilo", "Sinaloa con extra", "limón"}},
		{"corto", 20, []string{"corto"}},
		{"sinespaciosmuylargoquenocabe", 10, []string{"sinespacio", "smuylargoq", "uenocabe"}},
		{"dos\nlíneas", 20, []string{"dos", "líneas"}},
		{"exacto de 10", 12, []string{"exacto de 10"}},
	}
	for _, c := range casos {
		got := doblar(c.texto, c.ancho)
		if len(got) != len(c.want) {
			t.Errorf("doblar(%q, %d) = %q, se esperaba %q", c.texto, c.ancho, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("doblar(%q, %d)[%d] = %q, se esperaba %q", c.texto, c.ancho, i, got[i], c.want[i])
			}
		}
	}
}

func TestLeftRightRellenaAlAncho(t *testing.T) {
	w := New("epson", 20, "PC858_EURO")
	w.LeftRight("TOTAL", "$1,320.00")
	got := string(cuerpo(w))
	want := "TOTAL      $1,320.00\n"
	if got != want {
		t.Fatalf("leftRight: se obtuvo %q, se esperaba %q", got, want)
	}
}

func TestCutTerminaConReset(t *testing.T) {
	w := New("epson", 42, "PC858_EURO")
	w.Cut()
	got := cuerpo(w)
	want := []byte{0x1b, 0x64, 0x04, 0x1b, 0x64, 0x04, 0x1d, 0x56, 0x00, 0x1b, 0x40}
	if !bytes.Equal(got, want) {
		t.Fatalf("cut: se obtuvo % x, se esperaba % x", got, want)
	}
}
