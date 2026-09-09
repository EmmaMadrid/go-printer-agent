package render

import (
	"strings"

	"github.com/EmmaMadrid/go-printer-agent/internal/config"
)

// Comanda arma la orden de cocina. A diferencia de la nota de venta no lleva
// precios ni cajón: está pensada para leerse de lejos, con folio y platillos en
// grande y los modificadores en renglón propio.
func Comanda(c ComandaData, n config.Notas) ([]byte, error) {
	w := nuevoWriter(n)

	w.AlignCenter()
	w.Bold(true)
	w.Println("*** COMANDA DE COCINA ***")
	w.SetTextSize(2, 2)
	w.Println(c.Folio)
	w.SetTextNormal()
	if c.Ronda > 1 {
		w.Println(">>> RONDA " + num(c.Ronda) + " <<<")
	}
	w.Bold(false)
	w.DrawLine()

	w.AlignLeft()
	kv(w, "Tipo", c.TipoVenta)
	kv(w, "Mesa", c.Mesa)
	kv(w, "Cliente", c.Cliente)
	kv(w, "Orden", c.Orden)
	kv(w, "Envia", c.EnviadoPor)
	kv(w, "Hora", fhora(c.Hora, true))
	w.DrawLine()

	for _, l := range c.Lineas {
		w.Bold(true)
		w.SetTextDoubleHeight()
		w.Println(num(l.Cantidad) + " x " + l.Nombre)
		w.SetTextNormal()
		w.Bold(false)
		for _, m := range l.Modificadores {
			desc := strings.ToUpper(m.Descripcion)
			if m.Tipo == "quitar" {
				w.Println("   - SIN " + desc)
			} else {
				w.Println("   + " + desc)
			}
		}
		if l.Nota != "" {
			w.Println("   >> " + l.Nota)
		}
	}
	w.DrawLine()

	if c.NotaGeneral != "" {
		w.Bold(true)
		w.Println("NOTA DE LA ORDEN:")
		w.Println(c.NotaGeneral)
		w.Bold(false)
		w.DrawLine()
	}

	w.AlignCenter()
	w.Println(num(c.NumArticulos) + " articulo(s)")
	w.NewLine()

	if c.Opciones.cortar(n) {
		w.Cut()
	}
	return w.Bytes(), nil
}
